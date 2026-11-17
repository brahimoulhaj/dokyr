package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"panel/internal/account"
	"panel/internal/auth"
	"panel/internal/brand"
	"panel/internal/store"
)

const githubLoginCallback = "/api/auth/github/callback"
const loginFlowTTL = 5 * time.Minute

type loginFlow struct {
	Kind, Origin, UserID, SessionHash, Name, Verifier, ClientID string
	Session                                                     webauthn.SessionData
	Expires                                                     time.Time
}

// Ephemeral, single-use challenges are browser-bound and disappear on restart.
type loginFlows struct {
	mu        sync.Mutex
	passkeyMu sync.Mutex // serialize counter validation and persistence
	pending   map[string]loginFlow
}

func (s *Server) SetAuthPublicURL(publicURL string) {
	s.authPublicURL = strings.TrimRight(publicURL, "/")
}

// Only configured panel origins are trusted; a supplied Host cannot register an RP
// or redirect OAuth codes to an arbitrary domain.
func (s *Server) loginOrigin(r *http.Request) (string, error) {
	var candidates []string
	domain, _, err := s.store.GetSetting(r.Context(), store.SettingPanelDomain)
	if err != nil {
		return "", err
	}
	if domain != "" {
		// HTTP-only describes the connection from a TLS-terminating CDN to Caddy,
		// not the browser's origin. The configured hostname remains trusted for
		// HTTPS authentication; plain HTTP origins are still refused below.
		candidates = append(candidates, "https://"+domain)
	}
	// The updater preserves the data directory and the install-time environment.
	// Prefer the live panel-domain setting so an old HTTP/IP setup URL does not
	// need an environment-file migration when a new image starts.
	candidates = append(candidates, s.authPublicURL)
	for _, candidate := range candidates {
		u, err := url.Parse(candidate)
		if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			continue
		}
		if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")) {
			continue
		}
		origin := u.Scheme + "://" + u.Host
		if strings.EqualFold(u.Host, r.Host) && (r.Header.Get("Origin") == "" || r.Header.Get("Origin") == origin) {
			return origin, nil
		}
	}
	return "", errors.New("open the HTTPS panel domain configured in Settings > General (localhost is supported for development)")
}

func flowCookieName(kind string) string { return brand.Slug + "_" + strings.ReplaceAll(kind, "-", "_") }
func sessionHash(r *http.Request) string {
	c, err := r.Cookie(brand.SessionCookie())
	if err != nil {
		return ""
	}
	return auth.HashToken(c.Value)
}

func (s *Server) beginLoginFlow(w http.ResponseWriter, f loginFlow) (string, error) {
	token := auth.NewToken()
	f.Expires = time.Now().Add(loginFlowTTL)
	s.loginFlows.mu.Lock()
	defer s.loginFlows.mu.Unlock()
	if s.loginFlows.pending == nil {
		s.loginFlows.pending = map[string]loginFlow{}
	}
	for k, v := range s.loginFlows.pending {
		if time.Now().After(v.Expires) {
			delete(s.loginFlows.pending, k)
		}
	}
	if len(s.loginFlows.pending) >= 1024 {
		return "", errors.New("too many pending sign-in requests")
	}
	s.loginFlows.pending[auth.HashToken(token)] = f
	http.SetCookie(w, &http.Cookie{Name: flowCookieName(f.Kind), Value: token, Path: "/api/auth/", HttpOnly: true, Secure: strings.HasPrefix(f.Origin, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: int(loginFlowTTL.Seconds())})
	return token, nil
}

func (s *Server) takeLoginFlow(w http.ResponseWriter, r *http.Request, kind string) (loginFlow, error) {
	c, err := r.Cookie(flowCookieName(kind))
	if err != nil {
		return loginFlow{}, errors.New("sign-in request expired; try again")
	}
	s.loginFlows.mu.Lock()
	f, ok := s.loginFlows.pending[auth.HashToken(c.Value)]
	delete(s.loginFlows.pending, auth.HashToken(c.Value))
	s.loginFlows.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: flowCookieName(kind), Path: "/api/auth/", MaxAge: -1, HttpOnly: true, Secure: strings.HasPrefix(f.Origin, "https://"), SameSite: http.SameSiteLaxMode})
	origin, originErr := s.loginOrigin(r)
	if !ok || f.Kind != kind || time.Now().After(f.Expires) || originErr != nil || f.Origin != origin {
		return loginFlow{}, errors.New("sign-in request expired; try again")
	}
	if f.UserID != "" && (f.UserID != userFrom(r.Context()).ID || f.SessionHash == "" || f.SessionHash != sessionHash(r)) {
		return loginFlow{}, errors.New("sign-in session changed; try again")
	}
	return f, nil
}

func (s *Server) confirmLoginPassword(w http.ResponseWriter, r *http.Request, password string) bool {
	u := userFrom(r.Context())
	_, err := s.accounts.Login(r.Context(), u.Email, password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "the current password is incorrect")
		return false
	}
	return true
}

func (s *Server) handleLoginMethods(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	origin, originErr := s.loginOrigin(r)
	cfg, err := s.store.GitHubLoginConfig(r.Context())
	if err != nil {
		s.internal(w, "login config", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"passkeys": originErr == nil, "github": originErr == nil && cfg.ClientID != "" && cfg.ClientSecret != "", "origin": origin})
}

func (s *Server) handleSecuritySettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	u := userFrom(r.Context())
	keys, err := s.store.Passkeys(r.Context(), u.ID, "")
	if err != nil {
		s.internal(w, "passkeys", err)
		return
	}
	identity, err := s.store.GitHubIdentity(r.Context(), u.ID)
	if err != nil {
		s.internal(w, "github identity", err)
		return
	}
	cfg, err := s.store.GitHubLoginConfig(r.Context())
	if err != nil {
		s.internal(w, "github login config", err)
		return
	}
	origin, originErr := s.loginOrigin(r)
	callback := ""
	hint := ""
	if originErr == nil {
		callback = origin + githubLoginCallback
	} else {
		hint = originErr.Error()
	}
	writeJSON(w, http.StatusOK, map[string]any{"passkeys": keys, "github_identity": identity, "github_client_id": cfg.ClientID, "github_configured": cfg.ClientSecret != "" && cfg.ClientID != "", "callback_url": callback, "origin_error": hint})
}

func (s *Server) handleSaveGitHubLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		Password     string `json:"password"`
	}
	if !readJSON(w, r, &in) || !s.confirmLoginPassword(w, r, in.Password) {
		return
	}
	in.ClientID = strings.TrimSpace(in.ClientID)
	in.ClientSecret = strings.TrimSpace(in.ClientSecret)
	if len(in.ClientID) > 200 || len(in.ClientSecret) > 500 {
		writeError(w, 422, "invalid", "invalid GitHub application credentials")
		return
	}
	if in.ClientID != "" {
		old, err := s.store.GitHubLoginConfig(r.Context())
		if err != nil {
			s.internal(w, "github config", err)
			return
		}
		if in.ClientSecret == "" && in.ClientID == old.ClientID {
			in.ClientSecret = old.ClientSecret
		}
		if in.ClientSecret == "" {
			writeError(w, 422, "invalid", "GitHub client secret is required")
			return
		}
	}
	if err := s.store.SetGitHubLoginConfig(r.Context(), store.GitHubLoginConfig{ClientID: in.ClientID, ClientSecret: in.ClientSecret}); err != nil {
		s.internal(w, "save github login", err)
		return
	}
	// Invalidate ceremonies begun before a configuration change.
	s.loginFlows.mu.Lock()
	clear(s.loginFlows.pending)
	s.loginFlows.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

type passkeyUser struct {
	store.User
	Credentials []webauthn.Credential
}

func (u passkeyUser) WebAuthnID() []byte                         { return []byte(u.ID) }
func (u passkeyUser) WebAuthnName() string                       { return u.Email }
func (u passkeyUser) WebAuthnDisplayName() string                { return u.Email }
func (u passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.Credentials }

func (s *Server) passkeyUser(ctx context.Context, u store.User, rpID string) (passkeyUser, error) {
	keys, err := s.store.Passkeys(ctx, u.ID, rpID)
	if err != nil {
		return passkeyUser{}, err
	}
	out := passkeyUser{User: u}
	for _, key := range keys {
		var cred webauthn.Credential
		if err := json.Unmarshal(key.Credential, &cred); err != nil {
			return out, err
		}
		out.Credentials = append(out.Credentials, cred)
	}
	return out, nil
}

func passkeyRP(origin string) (*webauthn.WebAuthn, error) {
	u, err := url.Parse(origin)
	if err != nil {
		return nil, err
	}
	return webauthn.New(&webauthn.Config{RPDisplayName: brand.Name, RPID: u.Hostname(), RPOrigins: []string{origin}, AuthenticatorSelection: protocol.AuthenticatorSelection{ResidentKey: protocol.ResidentKeyRequirementRequired, UserVerification: protocol.VerificationRequired}})
}

func (s *Server) handleBeginPasskeyRegistration(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) || !s.confirmLoginPassword(w, r, in.Password) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 64 {
		writeError(w, 422, "invalid", "give the passkey a name (up to 64 characters)")
		return
	}
	origin, err := s.loginOrigin(r)
	if err != nil {
		writeError(w, 422, "origin", err.Error())
		return
	}
	rp, err := passkeyRP(origin)
	if err != nil {
		s.internal(w, "passkey RP", err)
		return
	}
	u, err := s.passkeyUser(r.Context(), userFrom(r.Context()), rp.Config.RPID)
	if err != nil {
		s.internal(w, "passkey user", err)
		return
	}
	if len(u.Credentials) >= 20 {
		writeError(w, 422, "limit", "remove an existing passkey before adding another")
		return
	}
	exclusions := []protocol.CredentialDescriptor{}
	for _, c := range u.Credentials {
		exclusions = append(exclusions, c.Descriptor())
	}
	options, session, err := rp.BeginRegistration(u, webauthn.WithExclusions(exclusions))
	if err != nil {
		s.internal(w, "passkey registration", err)
		return
	}
	if _, err = s.beginLoginFlow(w, loginFlow{Kind: "passkey-register", Origin: origin, UserID: u.ID, SessionHash: sessionHash(r), Name: in.Name, Session: *session}); err != nil {
		writeError(w, 429, "busy", err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, options)
}

func (s *Server) handleFinishPasskeyRegistration(w http.ResponseWriter, r *http.Request) {
	f, err := s.takeLoginFlow(w, r, "passkey-register")
	if err != nil {
		writeError(w, 400, "challenge", err.Error())
		return
	}
	rp, err := passkeyRP(f.Origin)
	if err != nil {
		s.internal(w, "passkey RP", err)
		return
	}
	u, err := s.passkeyUser(r.Context(), userFrom(r.Context()), rp.Config.RPID)
	if err != nil {
		s.internal(w, "passkey user", err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	cred, err := rp.FinishRegistration(u, f.Session, r)
	if err != nil {
		writeError(w, 400, "passkey", "could not verify this passkey; try again")
		return
	}
	data, err := json.Marshal(cred)
	if err != nil {
		s.internal(w, "passkey credential", err)
		return
	}
	if err = s.store.AddPasskey(r.Context(), store.Passkey{UserID: u.ID, RPID: rp.Config.RPID, CredentialID: base64.RawURLEncoding.EncodeToString(cred.ID), Name: f.Name, Credential: data}); err != nil {
		writeError(w, 409, "passkey", "this passkey could not be saved; it may already be registered")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleBeginPasskeyLogin(w http.ResponseWriter, r *http.Request) {
	origin, err := s.loginOrigin(r)
	if err != nil {
		writeError(w, 422, "origin", err.Error())
		return
	}
	rp, err := passkeyRP(origin)
	if err != nil {
		s.internal(w, "passkey RP", err)
		return
	}
	options, session, err := rp.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		s.internal(w, "passkey login", err)
		return
	}
	if _, err = s.beginLoginFlow(w, loginFlow{Kind: "passkey-login", Origin: origin, Session: *session}); err != nil {
		writeError(w, 429, "busy", err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, options)
}

func (s *Server) handleFinishPasskeyLogin(w http.ResponseWriter, r *http.Request) {
	f, err := s.takeLoginFlow(w, r, "passkey-login")
	if err != nil {
		writeError(w, 400, "challenge", err.Error())
		return
	}
	s.loginFlows.passkeyMu.Lock()
	defer s.loginFlows.passkeyMu.Unlock()
	rp, err := passkeyRP(f.Origin)
	if err != nil {
		s.internal(w, "passkey RP", err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	user, cred, err := rp.FinishPasskeyLogin(func(rawID, userHandle []byte) (webauthn.User, error) {
		u, err := s.store.GetUserByID(r.Context(), string(userHandle))
		if err != nil {
			return nil, err
		}
		p, err := s.passkeyUser(r.Context(), u, rp.Config.RPID)
		if err != nil {
			return nil, err
		}
		for _, c := range p.Credentials {
			if bytes.Equal(c.ID, rawID) {
				return p, nil
			}
		}
		return nil, store.ErrNotFound
	}, f.Session, r)
	if err != nil || cred.Authenticator.CloneWarning {
		writeError(w, 401, "invalid_credentials", "could not verify this passkey; use your password or try another passkey")
		return
	}
	u := user.(passkeyUser).User
	data, err := json.Marshal(cred)
	if err != nil {
		s.internal(w, "passkey credential", err)
		return
	}
	if err = s.store.UsePasskey(r.Context(), u.ID, rp.Config.RPID, base64.RawURLEncoding.EncodeToString(cred.ID), data); err != nil {
		writeError(w, 401, "invalid_credentials", "this passkey is no longer registered")
		return
	}
	s.startSession(w, r, u, http.StatusOK)
}

func (s *Server) handleDeletePasskey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) || !s.confirmLoginPassword(w, r, in.Password) {
		return
	}
	if err := s.store.DeletePasskey(r.Context(), userFrom(r.Context()).ID, r.PathValue("id")); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, 404, "not_found", "passkey not found")
		} else {
			s.internal(w, "delete passkey", err)
		}
		return
	}
	w.WriteHeader(204)
}

func (s *Server) handleStartGitHubLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Link     bool   `json:"link"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	f := loginFlow{Kind: "github"}
	if in.Link {
		// Linking is an authenticated operation even though sign-in starts publicly.
		s.requireAuth(sessionOnly(func(w http.ResponseWriter, r *http.Request) {
			if !s.confirmLoginPassword(w, r, in.Password) {
				return
			}
			f.UserID = userFrom(r.Context()).ID
			f.SessionHash = sessionHash(r)
			s.startGitHubFlow(w, r, f)
		}))(w, r)
		return
	}
	s.startGitHubFlow(w, r, f)
}

func (s *Server) startGitHubFlow(w http.ResponseWriter, r *http.Request, f loginFlow) {
	origin, err := s.loginOrigin(r)
	if err != nil {
		writeError(w, 422, "origin", err.Error())
		return
	}
	cfg, err := s.store.GitHubLoginConfig(r.Context())
	if err != nil {
		s.internal(w, "github login config", err)
		return
	}
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		if f.UserID != "" {
			f.Origin = origin
			s.startGitHubSetup(w, r, f)
			return
		}
		writeError(w, 422, "disabled", "GitHub sign-in is not configured")
		return
	}
	f.Origin = origin
	link, err := s.githubAuthorizeURL(w, f, cfg)
	if err != nil {
		writeError(w, 429, "busy", err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"url": link})
}

func (s *Server) githubAuthorizeURL(w http.ResponseWriter, f loginFlow, cfg store.GitHubLoginConfig) (string, error) {
	f.Kind = "github"
	f.Verifier = auth.NewToken()
	f.ClientID = cfg.ClientID
	state, err := s.beginLoginFlow(w, f)
	if err != nil {
		return "", err
	}
	// State is tied to the random browser cookie, not to a callback query alone.
	challenge := sha256.Sum256([]byte(f.Verifier))
	// GitHub Apps use registration permissions instead of OAuth App scopes.
	q := url.Values{"client_id": {cfg.ClientID}, "redirect_uri": {f.Origin + githubLoginCallback}, "state": {state}, "code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}, "code_challenge_method": {"S256"}}
	return "https://github.com/login/oauth/authorize?" + q.Encode(), nil
}

func (s *Server) handleGitHubLoginCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	// Resolve a session for linking only. Login works while signed out.
	if c, err := r.Cookie(brand.SessionCookie()); err == nil {
		if u, err := s.accounts.SessionUser(r.Context(), c.Value); err == nil {
			r = r.WithContext(context.WithValue(r.Context(), userKey, u))
		}
	}
	c, cerr := r.Cookie(flowCookieName("github"))
	state := r.URL.Query().Get("state")
	target := "/login?auth_error=github"
	if userFrom(r.Context()).ID != "" {
		target = "/settings?tab=security&auth_error=github"
	}
	if cerr != nil || state == "" || c.Value != state {
		http.Redirect(w, r, target, http.StatusSeeOther)
		return
	}
	f, err := s.takeLoginFlow(w, r, "github")
	fail := func() { http.Redirect(w, r, target, http.StatusSeeOther) }
	if err != nil || r.URL.Query().Get("error") != "" || r.URL.Query().Get("code") == "" {
		fail()
		return
	}
	cfg, err := s.store.GitHubLoginConfig(r.Context())
	if err != nil || cfg.ClientID != f.ClientID || cfg.ClientSecret == "" {
		fail()
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	subject, login, err := s.githubLoginIdentity(ctx, cfg, f, r.URL.Query().Get("code"))
	if err != nil {
		s.log.Warn("GitHub sign-in failed")
		fail()
		return
	}
	if f.UserID != "" {
		if err = s.store.LinkGitHub(ctx, f.UserID, subject, login); err != nil {
			fail()
			return
		}
		http.Redirect(w, r, "/settings?tab=security", http.StatusSeeOther)
		return
	}
	u, err := s.store.GitHubUser(ctx, subject)
	if err != nil {
		fail()
		return
	}
	token, _, err := s.accounts.NewSession(ctx, u.ID, browserSessionDetails(r))
	if err != nil {
		fail()
		return
	}
	http.SetCookie(w, sessionCookie(r, token, int(account.SessionTTL.Seconds())))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) githubLoginIdentity(ctx context.Context, cfg store.GitHubLoginConfig, f loginFlow, code string) (string, string, error) {
	form := url.Values{"client_id": {cfg.ClientID}, "client_secret": {cfg.ClientSecret}, "code": {code}, "redirect_uri": {f.Origin + githubLoginCallback}, "code_verifier": {f.Verifier}}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://github.com/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	client := s.authHTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	res, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	var token struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&token)
	res.Body.Close()
	if err != nil || res.StatusCode != 200 || token.Error != "" || token.AccessToken == "" {
		return "", "", errors.New("GitHub token exchange failed")
	}
	req, err = http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user", nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	res, err = client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer res.Body.Close()
	var identity struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&identity); err != nil || res.StatusCode != 200 || identity.ID <= 0 || identity.Login == "" {
		return "", "", fmt.Errorf("GitHub identity request failed")
	}
	return strconv.FormatInt(identity.ID, 10), identity.Login, nil
}

func (s *Server) handleUnlinkGitHub(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) || !s.confirmLoginPassword(w, r, in.Password) {
		return
	}
	if err := s.store.UnlinkGitHub(r.Context(), userFrom(r.Context()).ID); err != nil {
		s.internal(w, "unlink github", err)
		return
	}
	w.WriteHeader(204)
}

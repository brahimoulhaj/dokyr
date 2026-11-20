package api

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/fxamacker/cbor/v2"
	"panel/internal/account"
	"panel/internal/auth"
	"panel/internal/secret"
	"panel/internal/store"
)

func loginMethodEnv(t *testing.T) (*env, store.User) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "auth.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err = st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	accounts := account.New(st)
	server := New(st, accounts, slog.New(slog.NewTextHandler(io.Discard, nil)), "test", fstest.MapFS{"index.html": {Data: []byte("app")}})
	server.authRate = newRateLimiter(100, time.Minute)
	jar, _ := cookiejar.New(nil)
	handler := server.Handler()
	client := &http.Client{Jar: jar, Transport: loginRoundTripper(func(r *http.Request) (*http.Response, error) {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, r)
		response := rr.Result()
		response.Request = r
		return response, nil
	})}
	e := &env{t: t, url: "http://localhost:5173", client: client, accounts: accounts, srv: server}
	e.srv.SetAuthPublicURL(e.url)
	u, err := e.accounts.CreateOwner(context.Background(), "owner@example.com", "s3cret-pass")
	if err != nil {
		t.Fatal(err)
	}
	if code, m := e.call("POST", "/api/auth/login", map[string]string{"email": u.Email, "password": "s3cret-pass"}); code != 200 {
		t.Fatalf("login %d %v", code, m)
	}
	return e, u
}

func mustStatus(t *testing.T, e *env, method, path string, body any, want int) map[string]any {
	t.Helper()
	code, m := e.call(method, path, body)
	if code != want {
		t.Fatalf("%s %s = %d %v, want %d", method, path, code, m, want)
	}
	return m
}

func TestPasskeyRegistrationAndLogin(t *testing.T) {
	e, u := loginMethodEnv(t)
	b64 := base64.RawURLEncoding.EncodeToString
	mustStatus(t, e, "POST", "/api/auth/passkeys/register/begin", map[string]string{"name": "Laptop", "password": "wrong"}, 401)
	opts := mustStatus(t, e, "POST", "/api/auth/passkeys/register/begin", map[string]string{"name": "Laptop", "password": "s3cret-pass"}, 200)["publicKey"].(map[string]any)
	if opts["authenticatorSelection"].(map[string]any)["residentKey"] != "required" {
		t.Fatal("passkeys must be discoverable")
	}
	if opts["authenticatorSelection"].(map[string]any)["userVerification"] != "required" {
		t.Fatal("passkeys must require user verification")
	}
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	credID := []byte("test-credential-id-1234567890")
	public, err := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: private.X.FillBytes(make([]byte, 32)), -3: private.Y.FillBytes(make([]byte, 32))})
	if err != nil {
		t.Fatal(err)
	}
	rpID := opts["rp"].(map[string]any)["id"].(string)
	rpHash := sha256.Sum256([]byte(rpID))
	clientData := func(kind, challenge, origin string) []byte {
		b, _ := json.Marshal(map[string]any{"type": kind, "challenge": challenge, "origin": origin, "crossOrigin": false})
		return b
	}
	attData := append([]byte{}, rpHash[:]...)
	attData = append(attData, 0x45, 0, 0, 0, 0) // UP, UV and attested credential data
	attData = append(attData, make([]byte, 16)...)
	attData = binary.BigEndian.AppendUint16(attData, uint16(len(credID)))
	attData = append(attData, credID...)
	attData = append(attData, public...)
	attestation, err := cbor.Marshal(map[string]any{"fmt": "none", "authData": attData, "attStmt": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	registration := map[string]any{"id": b64(credID), "rawId": b64(credID), "type": "public-key", "response": map[string]any{"clientDataJSON": b64(clientData("webauthn.create", opts["challenge"].(string), e.url)), "attestationObject": b64(attestation), "transports": []string{"internal"}}, "clientExtensionResults": map[string]any{}}
	mustStatus(t, e, "POST", "/api/auth/passkeys/register/finish", registration, 204)
	mustStatus(t, e, "POST", "/api/auth/passkeys/register/finish", registration, 400) // consumed challenge
	keys, err := e.srv.store.Passkeys(context.Background(), u.ID, rpID)
	if err != nil || len(keys) != 1 {
		t.Fatalf("keys %v %v", keys, err)
	}
	mustStatus(t, e, "POST", "/api/auth/logout", nil, 204)
	counter := byte(0) // a real authenticator counts its signatures; a repeated count reads as a clone
	assertion := func(options map[string]any, origin string, uv bool) map[string]any {
		counter++
		client := clientData("webauthn.get", options["challenge"].(string), origin)
		flags := byte(0x05)
		if !uv {
			flags = 0x01
		}
		data := append([]byte{}, rpHash[:]...)
		data = append(data, flags, 0, 0, 0, counter)
		ch := sha256.Sum256(client)
		signed := append(append([]byte{}, data...), ch[:]...)
		hash := sha256.Sum256(signed)
		sig, err := ecdsa.SignASN1(rand.Reader, private, hash[:])
		if err != nil {
			t.Fatal(err)
		}
		return map[string]any{"id": b64(credID), "rawId": b64(credID), "type": "public-key", "response": map[string]any{"clientDataJSON": b64(client), "authenticatorData": b64(data), "signature": b64(sig), "userHandle": b64([]byte(u.ID))}, "clientExtensionResults": map[string]any{}}
	}
	// Correct signatures alone cannot bypass the configured origin or user verification.
	for _, test := range []struct {
		origin string
		uv     bool
	}{{"https://evil.example", true}, {e.url, false}} {
		opts = mustStatus(t, e, "POST", "/api/auth/passkeys/login/begin", map[string]any{}, 200)["publicKey"].(map[string]any)
		mustStatus(t, e, "POST", "/api/auth/passkeys/login/finish", assertion(opts, test.origin, test.uv), 401)
	}
	opts = mustStatus(t, e, "POST", "/api/auth/passkeys/login/begin", map[string]any{}, 200)["publicKey"].(map[string]any)
	signed := assertion(opts, e.url, true)
	mustStatus(t, e, "POST", "/api/auth/passkeys/login/finish", signed, 200)
	mustStatus(t, e, "GET", "/api/auth/me", nil, 200)
	mustStatus(t, e, "POST", "/api/auth/passkeys/login/finish", signed, 400)
	// The same passkey confirms host access in place of the password, for this session only.
	if got := mustStatus(t, e, "GET", "/api/host/terminal/settings", nil, 200); got["unlocked"] != false || got["passkey"] != true {
		t.Fatalf("host settings before the confirmation = %v", got)
	}
	for _, test := range []struct {
		origin string
		uv     bool
	}{{"https://evil.example", true}, {e.url, false}} {
		opts = mustStatus(t, e, "POST", "/api/auth/passkeys/host-unlock/begin", map[string]any{}, 200)["publicKey"].(map[string]any)
		mustStatus(t, e, "POST", "/api/auth/passkeys/host-unlock/finish", assertion(opts, test.origin, test.uv), 401)
	}
	opts = mustStatus(t, e, "POST", "/api/auth/passkeys/host-unlock/begin", map[string]any{}, 200)["publicKey"].(map[string]any)
	if allowed, _ := opts["allowCredentials"].([]any); len(allowed) != 1 {
		t.Fatalf("the challenge must name the user's own passkeys: %v", opts["allowCredentials"])
	}
	if opts["userVerification"] != "required" {
		t.Fatal("a host confirmation must require user verification")
	}
	confirmed := assertion(opts, e.url, true)
	mustStatus(t, e, "POST", "/api/auth/passkeys/host-unlock/finish", confirmed, 204)
	mustStatus(t, e, "POST", "/api/auth/passkeys/host-unlock/finish", confirmed, 400) // consumed challenge
	if got := mustStatus(t, e, "GET", "/api/host/terminal/settings", nil, 200); got["unlocked"] != true {
		t.Fatalf("host settings after the confirmation = %v", got)
	}
	keys, err = e.srv.store.Passkeys(context.Background(), u.ID, rpID)
	if err != nil || keys[0].LastUsedAt == nil {
		t.Fatal("last-used timestamp not persisted", err)
	}
	mustStatus(t, e, "DELETE", "/api/auth/passkeys/"+keys[0].ID, map[string]string{"password": "wrong"}, 401)
	mustStatus(t, e, "DELETE", "/api/auth/passkeys/"+keys[0].ID, map[string]string{"password": "s3cret-pass"}, 204)
	mustStatus(t, e, "POST", "/api/auth/logout", nil, 204)
	opts = mustStatus(t, e, "POST", "/api/auth/passkeys/login/begin", map[string]any{}, 200)["publicKey"].(map[string]any)
	mustStatus(t, e, "POST", "/api/auth/passkeys/login/finish", assertion(opts, e.url, true), 401)
}

type loginRoundTripper func(*http.Request) (*http.Response, error)

func (f loginRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGitHubLinkedAccountLogin(t *testing.T) {
	e, u := loginMethodEnv(t)
	ctx := context.Background()
	box, err := secret.New(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.srv.store.UseSecrets(ctx, box); err != nil {
		t.Fatal(err)
	}
	mustStatus(t, e, "PUT", "/api/auth/github/config", map[string]string{"client_id": "client", "client_secret": "secret-value", "password": "wrong"}, 401)
	mustStatus(t, e, "PUT", "/api/auth/github/config", map[string]string{"client_id": "client", "client_secret": "secret-value", "password": "s3cret-pass"}, 204)
	stored, _, err := e.srv.store.GetSetting(ctx, "github_login")
	if err != nil || strings.Contains(stored, "secret-value") {
		t.Fatal("OAuth secret not encrypted")
	}
	security := mustStatus(t, e, "GET", "/api/auth/security", nil, 200)
	raw, _ := json.Marshal(security)
	if strings.Contains(string(raw), "secret-value") {
		t.Fatal("secret leaked")
	}
	var expectedChallenge string
	subject := "12345"
	e.srv.authHTTPClient = &http.Client{Transport: loginRoundTripper(func(r *http.Request) (*http.Response, error) {
		body := ""
		switch r.URL.String() {
		case "https://github.com/login/oauth/access_token":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(hash[:]) != expectedChallenge || r.Form.Get("client_secret") != "secret-value" || r.Form.Get("redirect_uri") != e.url+githubLoginCallback {
				t.Fatal("invalid PKCE/token exchange")
			}
			body = `{"access_token":"github-token"}`
		case "https://api.github.com/user":
			if r.Header.Get("Authorization") != "Bearer github-token" {
				t.Fatal("missing bearer")
			}
			body = `{"id":` + subject + `,"login":"my-github"}`
		default:
			t.Fatal("unexpected outbound request", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}
	e.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	start := func(link bool) string {
		m := mustStatus(t, e, "POST", "/api/auth/github/start", map[string]any{"link": link, "password": "s3cret-pass"}, 200)
		target, err := url.Parse(m["url"].(string))
		if err != nil {
			t.Fatal(err)
		}
		expectedChallenge = target.Query().Get("code_challenge")
		if expectedChallenge == "" || target.Query().Get("code_challenge_method") != "S256" {
			t.Fatal("missing PKCE")
		}
		return target.Query().Get("state")
	}
	callback := func(state string) string {
		res, err := e.client.Get(e.url + githubLoginCallback + "?code=oauth-code&state=" + url.QueryEscape(state))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != 303 {
			t.Fatal("callback status", res.StatusCode)
		}
		return res.Header.Get("Location")
	}
	state := start(true)
	// State must be bound to this browser, not merely a valid pending state.
	req := httptest.NewRequest("GET", e.url+githubLoginCallback+"?code=stolen&state="+state, nil)
	rr := httptest.NewRecorder()
	e.srv.Handler().ServeHTTP(rr, req)
	if !strings.Contains(rr.Header().Get("Location"), "auth_error") {
		t.Fatal("callback accepted without browser cookie")
	}
	if dest := callback(state); dest != "/settings?tab=security" {
		t.Fatal("link failed", dest)
	}
	identity, err := e.srv.store.GitHubIdentity(ctx, u.ID)
	if err != nil || identity == nil || identity.Login != "my-github" {
		t.Fatal("identity not linked", err)
	}
	mustStatus(t, e, "POST", "/api/auth/logout", nil, 204)
	state = start(false)
	if dest := callback(state); dest != "/" {
		t.Fatal("login failed", dest)
	}
	mustStatus(t, e, "GET", "/api/auth/me", nil, 200)
	if dest := callback(state); !strings.Contains(dest, "auth_error") {
		t.Fatal("OAuth replay succeeded")
	}
	// Matching email is deliberately irrelevant: only the linked numeric GitHub ID authorizes access.
	mustStatus(t, e, "POST", "/api/auth/logout", nil, 204)
	subject = "98765"
	state = start(false)
	if dest := callback(state); !strings.Contains(dest, "auth_error") {
		t.Fatal("unlinked GitHub account admitted")
	}
	mustStatus(t, e, "GET", "/api/auth/me", nil, 401)
}

func TestLoginMethodOriginAndFlowBoundaries(t *testing.T) {
	e, u := loginMethodEnv(t)
	req := httptest.NewRequest("POST", "https://evil.example/api/auth/passkeys/login/begin", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()
	e.srv.Handler().ServeHTTP(rr, req)
	if rr.Code != 422 {
		t.Fatalf("untrusted host = %d", rr.Code)
	}
	apiToken := auth.NewToken()
	if _, err := e.srv.store.CreateAPIToken(context.Background(), u.ID, "test", auth.HashToken(apiToken)); err != nil {
		t.Fatal(err)
	}
	if code, _ := e.call("GET", "/api/auth/security", nil, "Authorization", "Bearer "+apiToken); code != 403 {
		t.Fatalf("API token can manage sign-in methods: %d", code)
	}
	// Expired challenges cannot create a session, and are consumed on the attempt.
	browserToken := auth.NewToken()
	e.srv.loginFlows.pending = map[string]loginFlow{auth.HashToken(browserToken): {Kind: "passkey-login", Origin: e.url, Expires: time.Now().Add(-time.Second)}}
	req = httptest.NewRequest("POST", e.url+"/api/auth/passkeys/login/finish", strings.NewReader(`{}`))
	req.AddCookie(&http.Cookie{Name: flowCookieName("passkey-login"), Value: browserToken})
	rr = httptest.NewRecorder()
	e.srv.Handler().ServeHTTP(rr, req)
	if rr.Code != 400 {
		t.Fatalf("expired challenge = %d", rr.Code)
	}
	if len(e.srv.loginFlows.pending) != 0 {
		t.Fatal("expired challenge retained")
	}
}

func TestLoginOriginBehindCDN(t *testing.T) {
	e, _ := loginMethodEnv(t)
	ctx := context.Background()
	// The installer initially records an HTTP IP address for setup links.
	e.srv.SetAuthPublicURL("http://203.0.113.10")
	if err := e.srv.store.SetSetting(ctx, store.SettingPanelDomain, "panel.example.com"); err != nil {
		t.Fatal(err)
	}
	if err := e.srv.store.SetSetting(ctx, store.SettingPanelTLS, store.TLSHTTPOnly); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, host, origin, forwardedProto string
		allowed                            bool
	}{
		{"HTTPS browser through HTTP CDN upstream", "panel.example.com", "https://panel.example.com", "http", true},
		{"OAuth callback without Origin", "panel.example.com", "", "http", true},
		{"plain HTTP browser", "panel.example.com", "http://panel.example.com", "http", false},
		{"foreign Origin", "panel.example.com", "https://evil.example", "https", false},
		{"unconfigured Host", "evil.example", "https://evil.example", "https", false},
		{"setup IP is not a secure sign-in origin", "203.0.113.10", "http://203.0.113.10", "https", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "http://"+test.host+"/api/auth/passkeys/login/begin", nil)
			req.Header.Set("Origin", test.origin)
			req.Header.Set("X-Forwarded-Proto", test.forwardedProto)
			origin, err := e.srv.loginOrigin(req)
			if test.allowed {
				if err != nil || origin != "https://panel.example.com" {
					t.Fatalf("origin = %q, error = %v", origin, err)
				}
			} else if err == nil {
				t.Fatalf("untrusted origin accepted: %q", origin)
			}
		})
	}
}

func TestLoginMethodsAfterImageUpdate(t *testing.T) {
	for _, mode := range []string{store.TLSAuto, store.TLSHTTPOnly} {
		t.Run(mode, func(t *testing.T) {
			e, owner := loginMethodEnv(t)
			ctx := context.Background()
			for key, value := range map[string]string{
				store.SettingPanelDomain: "panel.example.com",
				store.SettingPanelTLS:    mode,
			} {
				if err := e.srv.store.SetSetting(ctx, key, value); err != nil {
					t.Fatal(err)
				}
			}
			if err := e.srv.store.SetGitHubLoginConfig(ctx, store.GitHubLoginConfig{ClientID: "configured-app", ClientSecret: "configured-secret"}); err != nil {
				t.Fatal(err)
			}
			token, _, err := e.accounts.NewSession(ctx, owner.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, installURL := range []string{"", "http://203.0.113.10", "https://old.example.com"} {
				t.Run(installURL, func(t *testing.T) {
					// A replacement image sees the persisted database and the old
					// container environment, just as the self-updater provides them.
					updated := New(e.srv.store, account.New(e.srv.store), e.srv.log, "updated", e.srv.web)
					updated.SetAuthPublicURL(installURL)
					handler := updated.Handler()
					req := httptest.NewRequest("GET", "http://panel.example.com/api/auth/methods", nil)
					req.Header.Set("X-Forwarded-Proto", "http")
					rr := httptest.NewRecorder()
					handler.ServeHTTP(rr, req)
					var methods struct {
						Passkeys bool   `json:"passkeys"`
						GitHub   bool   `json:"github"`
						Origin   string `json:"origin"`
					}
					if err := json.Unmarshal(rr.Body.Bytes(), &methods); err != nil {
						t.Fatal(err)
					}
					if rr.Code != 200 || !methods.Passkeys || !methods.GitHub || methods.Origin != "https://panel.example.com" {
						t.Fatalf("login methods after update: %d %+v", rr.Code, methods)
					}
					req = httptest.NewRequest("GET", "http://panel.example.com/api/auth/security", nil)
					req.AddCookie(&http.Cookie{Name: "dokyr_session", Value: token})
					rr = httptest.NewRecorder()
					handler.ServeHTTP(rr, req)
					var security struct {
						Callback    string `json:"callback_url"`
						OriginError string `json:"origin_error"`
					}
					if err := json.Unmarshal(rr.Body.Bytes(), &security); err != nil {
						t.Fatal(err)
					}
					if rr.Code != 200 || security.OriginError != "" || security.Callback != "https://panel.example.com"+githubLoginCallback {
						t.Fatalf("security after update: %d %+v", rr.Code, security)
					}
					persisted, _, err := e.srv.store.GetSetting(ctx, store.SettingPanelTLS)
					if err != nil || persisted != mode {
						t.Fatalf("TLS setting changed during update: %q, %v", persisted, err)
					}
				})
			}
		})
	}
}

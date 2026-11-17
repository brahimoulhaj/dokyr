package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"panel/internal/auth"
	"panel/internal/brand"
	"panel/internal/secret"
	"panel/internal/store"
)

func beginGitHubSetup(t *testing.T, e *env) string {
	t.Helper()
	m := mustStatus(t, e, "POST", "/api/auth/github/start", map[string]any{"link": true, "password": "s3cret-pass"}, 200)
	target, err := url.Parse(m["action_url"].(string))
	if err != nil || target.Scheme != "https" || target.Host != "github.com" || target.Path != "/settings/apps/new" || target.Query().Get("state") == "" {
		t.Fatalf("invalid app creation target: %v %v", target, err)
	}
	var manifest struct {
		Name         string            `json:"name"`
		URL          string            `json:"url"`
		RedirectURL  string            `json:"redirect_url"`
		CallbackURLs []string          `json:"callback_urls"`
		Public       bool              `json:"public"`
		Permissions  map[string]string `json:"default_permissions"`
		Events       []string          `json:"default_events"`
		Hook         struct {
			Active bool `json:"active"`
		} `json:"hook_attributes"`
	}
	if err := json.Unmarshal([]byte(m["manifest"].(string)), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Name == "" || manifest.URL != e.url || manifest.RedirectURL != e.url+githubSetupCallback || len(manifest.CallbackURLs) != 1 || manifest.CallbackURLs[0] != e.url+githubLoginCallback || manifest.Public || manifest.Hook.Active || len(manifest.Permissions) != 0 || len(manifest.Events) != 0 {
		t.Fatalf("unexpected app manifest: %+v", manifest)
	}
	return target.Query().Get("state")
}

func githubSetupResult(t *testing.T, e *env, state, code string) string {
	t.Helper()
	res, err := e.client.Get(e.url + githubSetupCallback + "?" + url.Values{"state": {state}, "code": {code}}.Encode())
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("setup callback = %d", res.StatusCode)
	}
	return res.Header.Get("Location")
}

func TestGitHubAutomaticSetupAndLogin(t *testing.T) {
	e, u := loginMethodEnv(t)
	ctx := context.Background()
	box, err := secret.New(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.srv.store.UseSecrets(ctx, box); err != nil {
		t.Fatal(err)
	}
	e.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	mustStatus(t, e, "POST", "/api/auth/github/start", map[string]any{"link": true, "password": "wrong"}, 401)
	apiToken := auth.NewToken()
	if _, err := e.srv.store.CreateAPIToken(ctx, u.ID, "test", auth.HashToken(apiToken)); err != nil {
		t.Fatal(err)
	}
	if code, _ := e.call("POST", "/api/auth/github/start", map[string]any{"link": true, "password": "s3cret-pass"}, "Authorization", "Bearer "+apiToken); code != 403 {
		t.Fatalf("API token started setup: %d", code)
	}
	var challenge string
	conversions := 0
	e.srv.authHTTPClient = &http.Client{Transport: loginRoundTripper(func(r *http.Request) (*http.Response, error) {
		status, body := 200, ""
		switch r.URL.String() {
		case "https://api.github.com/app-manifests/setup-code/conversions":
			if r.Method != "POST" {
				t.Fatal("manifest conversion must be POST")
			}
			conversions++
			status, body = 201, `{"id":123,"client_id":"app-client","client_secret":"private-secret","pem":"discarded"}`
		case "https://github.com/login/oauth/access_token":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(hash[:]) != challenge || r.Form.Get("client_id") != "app-client" || r.Form.Get("client_secret") != "private-secret" {
				t.Fatal("invalid OAuth exchange after setup")
			}
			body = `{"access_token":"user-token"}`
		case "https://api.github.com/user":
			if r.Header.Get("Authorization") != "Bearer user-token" {
				t.Fatal("missing user token")
			}
			body = `{"id":456,"login":"my-github"}`
		default:
			t.Fatal("unexpected request", r.URL)
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	state := beginGitHubSetup(t, e)
	target := githubSetupResult(t, e, state, "setup-code")
	authorize, err := url.Parse(target)
	if err != nil || authorize.Host != "github.com" || authorize.Path != "/login/oauth/authorize" || authorize.Query().Get("client_id") != "app-client" || authorize.Query().Get("redirect_uri") != e.url+githubLoginCallback || authorize.Query().Get("scope") != "" || authorize.Query().Get("code_challenge_method") != "S256" {
		t.Fatalf("invalid authorization redirect %s %v", target, err)
	}
	challenge = authorize.Query().Get("code_challenge")
	if challenge == "" {
		t.Fatal("missing PKCE")
	}
	if identity, err := e.srv.store.GitHubIdentity(ctx, u.ID); err != nil || identity != nil {
		t.Fatal("creating an app linked an identity", identity, err)
	}
	stored, _, err := e.srv.store.GetSetting(ctx, "github_login")
	if err != nil || strings.Contains(stored, "private-secret") || strings.Contains(stored, "discarded") {
		t.Fatal("manifest credentials not protected", err)
	}
	security := mustStatus(t, e, "GET", "/api/auth/security", nil, 200)
	raw, _ := json.Marshal(security)
	if strings.Contains(string(raw), "private-secret") || security["github_configured"] != true {
		t.Fatal("invalid security response")
	}
	if dest := githubSetupResult(t, e, state, "setup-code"); !strings.Contains(dest, "auth_error") || conversions != 1 {
		t.Fatal("setup replay accepted")
	}
	complete := func(state string) {
		t.Helper()
		res, err := e.client.Get(e.url + githubLoginCallback + "?code=oauth-code&state=" + url.QueryEscape(state))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 303 || strings.Contains(res.Header.Get("Location"), "auth_error") {
			t.Fatal("GitHub authorization failed", res.StatusCode, res.Header.Get("Location"))
		}
	}
	complete(authorize.Query().Get("state"))
	if identity, err := e.srv.store.GitHubIdentity(ctx, u.ID); err != nil || identity == nil || identity.Login != "my-github" {
		t.Fatal("identity not linked after OAuth", err)
	}
	mustStatus(t, e, "POST", "/api/auth/logout", nil, 204)
	next := mustStatus(t, e, "POST", "/api/auth/github/start", map[string]bool{"link": false}, 200)
	if _, ok := next["manifest"]; ok {
		t.Fatal("subsequent login requires app setup")
	}
	authorize, _ = url.Parse(next["url"].(string))
	challenge = authorize.Query().Get("code_challenge")
	complete(authorize.Query().Get("state"))
	mustStatus(t, e, "GET", "/api/auth/me", nil, 200)
}

func TestGitHubSetupBoundaries(t *testing.T) {
	for _, scenario := range []string{"wrong-state", "missing-cookie", "changed-session", "expired", "invalid-code", "provider-error", "invalid-response", "configuration-race"} {
		t.Run(scenario, func(t *testing.T) {
			e, u := loginMethodEnv(t)
			e.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			state := beginGitHubSetup(t, e)
			calls := 0
			e.srv.authHTTPClient = &http.Client{Transport: loginRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				body, status := `{"id":123,"client_id":"new-client","client_secret":"new-secret"}`, 201
				if scenario == "provider-error" {
					status = 422
				}
				if scenario == "invalid-response" {
					body = `{"id":123,"client_id":"new-client"}`
				}
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			code := "setup-code"
			switch scenario {
			case "wrong-state":
				state = "wrong"
			case "missing-cookie":
				base, _ := url.Parse(e.url + githubSetupCallback)
				e.client.Jar.SetCookies(base, []*http.Cookie{{Name: flowCookieName("github-setup"), Path: "/api/auth/", MaxAge: -1}})
			case "changed-session":
				token, _, err := e.accounts.NewSession(context.Background(), u.ID)
				if err != nil {
					t.Fatal(err)
				}
				base, _ := url.Parse(e.url)
				e.client.Jar.SetCookies(base, []*http.Cookie{{Name: brand.SessionCookie(), Value: token, Path: "/"}})
			case "expired":
				e.srv.loginFlows.mu.Lock()
				f := e.srv.loginFlows.pending[auth.HashToken(state)]
				f.Expires = time.Now().Add(-time.Minute)
				e.srv.loginFlows.pending[auth.HashToken(state)] = f
				e.srv.loginFlows.mu.Unlock()
			case "invalid-code":
				code = "../elsewhere"
			case "configuration-race":
				if err := e.srv.store.SetGitHubLoginConfig(context.Background(), store.GitHubLoginConfig{ClientID: "existing", ClientSecret: "keep"}); err != nil {
					t.Fatal(err)
				}
				if err := e.srv.store.LinkGitHub(context.Background(), u.ID, "456", "existing-user"); err != nil {
					t.Fatal(err)
				}
			}
			if dest := githubSetupResult(t, e, state, code); !strings.Contains(dest, "auth_error") {
				t.Fatal("unsafe setup accepted", dest)
			}
			wantCalls := 0
			if scenario == "provider-error" || scenario == "invalid-response" || scenario == "configuration-race" {
				wantCalls = 1
			}
			if calls != wantCalls {
				t.Fatalf("outbound requests = %d, want %d", calls, wantCalls)
			}
			cfg, err := e.srv.store.GitHubLoginConfig(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "configuration-race" {
				identity, err := e.srv.store.GitHubIdentity(context.Background(), u.ID)
				if cfg.ClientID != "existing" || cfg.ClientSecret != "keep" || err != nil || identity == nil || identity.Login != "existing-user" {
					t.Fatal("existing configuration or link replaced")
				}
			} else if cfg.ClientID != "" || cfg.ClientSecret != "" {
				t.Fatal("failed setup saved credentials")
			}
		})
	}
	t.Run("signed-out", func(t *testing.T) {
		e, _ := loginMethodEnv(t)
		mustStatus(t, e, "POST", "/api/auth/logout", nil, 204)
		mustStatus(t, e, "POST", "/api/auth/github/start", map[string]bool{"link": true}, 401)
		mustStatus(t, e, "POST", "/api/auth/github/start", map[string]bool{"link": false}, 422)
		rr := httptest.NewRecorder()
		e.srv.Handler().ServeHTTP(rr, httptest.NewRequest("GET", e.url+githubSetupCallback+"?state=stolen&code=setup-code", nil))
		if rr.Code != 401 {
			t.Fatal("unauthenticated setup callback accepted", rr.Code)
		}
	})
}

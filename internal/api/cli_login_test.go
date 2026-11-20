package api

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"panel/internal/auth"
	"panel/internal/brand"
)

const cliTestState = "state-0123456789abcdef"

func cliChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// cliApprove approves a CLI sign-in in the browser and returns the code of the redirect.
func (e *env) cliApprove(verifier, device string) string {
	e.t.Helper()
	m := e.must(200, "POST", "/api/cli/authorize", map[string]any{
		"port": 43210, "state": cliTestState, "challenge": cliChallenge(verifier), "device": device,
	})
	u, err := url.Parse(m["redirect"].(string))
	if err != nil {
		e.t.Fatal(err)
	}
	if u.Scheme != "http" || u.Host != "127.0.0.1:43210" || u.Path != "/callback" || u.Query().Get("state") != cliTestState {
		e.t.Fatalf("redirect = %s", u)
	}
	return u.Query().Get("code")
}

func TestCLISignIn(t *testing.T) {
	e := newEnv(t)
	e.srv.authRate.max = 1000 // the routes hold this limiter; the test makes many attempts
	verifier := auth.NewToken()
	ask := map[string]any{"port": 43210, "state": cliTestState, "challenge": cliChallenge(verifier), "device": "laptop"}

	if code, _ := e.call("POST", "/api/cli/authorize", ask); code != 401 {
		t.Fatalf("approve signed out = %d", code)
	}
	e.signIn()

	// Only a loopback port, a state and a SHA-256 challenge are accepted.
	for name, bad := range map[string]map[string]any{
		"low port":        {"port": 80, "state": cliTestState, "challenge": cliChallenge(verifier)},
		"no port":         {"state": cliTestState, "challenge": cliChallenge(verifier)},
		"port too high":   {"port": 70000, "state": cliTestState, "challenge": cliChallenge(verifier)},
		"short state":     {"port": 43210, "state": "x", "challenge": cliChallenge(verifier)},
		"state with &":    {"port": 43210, "state": "aaaaaaaaaaaaaaaa&code=x", "challenge": cliChallenge(verifier)},
		"plain challenge": {"port": 43210, "state": cliTestState, "challenge": verifier + "x"},
	} {
		if code, m := e.call("POST", "/api/cli/authorize", bad); code != 422 {
			t.Errorf("approve with %s = %d %v, want 422", name, code, m)
		}
	}
	// The redirect address is the panel's to choose: a client cannot name one.
	if code, _ := e.call("POST", "/api/cli/authorize", map[string]any{
		"port": 43210, "state": cliTestState, "challenge": cliChallenge(verifier), "redirect": "https://evil.example/callback",
	}); code != 400 {
		t.Errorf("approve with a redirect of its own = %d, want 400", code)
	}

	// A wrong verifier gets nothing, and burns the code.
	code := e.cliApprove(verifier, "laptop")
	if status, body := e.bearer("", "POST", "/api/cli/token", map[string]any{"code": code, "verifier": "not-the-verifier"}); status != 400 {
		t.Fatalf("exchange with a wrong verifier = %d %s", status, body)
	}
	if status, _ := e.bearer("", "POST", "/api/cli/token", map[string]any{"code": code, "verifier": verifier}); status != 400 {
		t.Fatalf("code still good after a wrong verifier: %d", status)
	}
	if status, _ := e.bearer("", "POST", "/api/cli/token", map[string]any{"code": "no-such-code", "verifier": verifier}); status != 400 {
		t.Fatalf("exchange of an unknown code = %d", status)
	}

	// An expired code.
	code = e.cliApprove(verifier, "laptop")
	e.srv.cliGrants.mu.Lock()
	for k, g := range e.srv.cliGrants.m {
		g.expires = time.Now().Add(-time.Second)
		e.srv.cliGrants.m[k] = g
	}
	e.srv.cliGrants.mu.Unlock()
	if status, _ := e.bearer("", "POST", "/api/cli/token", map[string]any{"code": code, "verifier": verifier}); status != 400 {
		t.Fatalf("exchange of an expired code = %d", status)
	}
	if len(e.listTokens()) != 0 {
		t.Fatal("a refused exchange left a token")
	}

	// The real exchange: a token of the user, named after the device, good once.
	code = e.cliApprove(verifier, "  my\x00 laptop\n")
	status, body := e.bearer("", "POST", "/api/cli/token", map[string]any{"code": code, "verifier": verifier})
	if status != 201 {
		t.Fatalf("exchange = %d %s", status, body)
	}
	var out struct {
		Token     string `json:"token"`
		TokenName string `json:"token_name"`
		User      struct{ Email, Role string }
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.Token, brand.Slug+"_") || out.TokenName != "CLI on my laptop" || out.User.Email != "a@b.co" || out.User.Role != "owner" {
		t.Fatalf("exchange = %s", body)
	}
	if status, _ := e.bearer("", "POST", "/api/cli/token", map[string]any{"code": code, "verifier": verifier}); status != 400 {
		t.Fatalf("code used twice = %d", status)
	}
	if status, body := e.bearer(out.Token, "GET", "/api/projects", nil); status != 200 {
		t.Fatalf("projects with the CLI token = %d %s", status, body)
	}

	// Listed with the other tokens, and revocable there.
	list := e.listTokens()
	if len(list) != 1 || list[0]["name"] != "CLI on my laptop" {
		t.Fatalf("tokens = %v", list)
	}
	if !strings.Contains(e.eventActors(), "a@b.co") {
		t.Fatal("the sign-in left no event")
	}

	// A token cannot approve a sign-in: it would mint another token.
	if status, body := e.bearer(out.Token, "POST", "/api/cli/authorize", ask); status != 403 {
		t.Fatalf("approve with a token = %d %s, want 403", status, body)
	}

	e.must(204, "DELETE", "/api/tokens/"+list[0]["id"].(string), nil)
	if status, _ := e.bearer(out.Token, "GET", "/api/projects", nil); status != 401 {
		t.Fatalf("revoked CLI token still works: %d", status)
	}
}

// A maintainer's CLI token is a maintainer: the role comes with the user, not the token.
func TestCLISignInKeepsRole(t *testing.T) {
	e := newEnv(t)
	e.srv.authRate.max = 1000 // the routes hold this limiter; the test makes many attempts
	e.signIn()
	member := e.join("m@b.co", "maintainer")

	verifier := auth.NewToken()
	code := member.cliApprove(verifier, "ci-box")
	status, body := e.bearer("", "POST", "/api/cli/token", map[string]any{"code": code, "verifier": verifier})
	if status != 201 {
		t.Fatalf("exchange = %d %s", status, body)
	}
	var out struct {
		Token string `json:"token"`
		User  struct{ Role string }
	}
	_ = json.Unmarshal(body, &out)
	if out.User.Role != "maintainer" {
		t.Fatalf("role = %q", out.User.Role)
	}
	if status, _ := e.bearer(out.Token, "GET", "/api/projects", nil); status != 200 {
		t.Fatalf("projects as maintainer = %d", status)
	}
	if status, _ := e.bearer(out.Token, "GET", "/api/settings/panel-backup/backups", nil); status != 403 {
		t.Fatalf("admin route with a maintainer's CLI token = %d, want 403", status)
	}
	// The owner's list does not show the maintainer's token.
	if len(e.listTokens()) != 0 {
		t.Fatal("a member's CLI token shows up in the owner's list")
	}
}

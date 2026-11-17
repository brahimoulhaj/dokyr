package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"panel/internal/auth"
	"panel/internal/brand"
	"panel/internal/store"
)

func listedSessions(t *testing.T, e *env) []store.Session {
	t.Helper()
	res, err := e.client.Get(e.url + "/api/auth/sessions")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("sessions = %d %s", res.StatusCode, b)
	}
	var sessions []store.Session
	if err := json.NewDecoder(res.Body).Decode(&sessions); err != nil {
		t.Fatal(err)
	}
	return sessions
}

func TestSessionManagement(t *testing.T) {
	e, owner := loginMethodEnv(t)
	ctx := context.Background()
	mustStatus(t, e, "POST", "/api/auth/logout", nil, http.StatusNoContent)
	// Public peers cannot supply their own session IP through forwarding headers.
	transport := e.client.Transport
	e.client.Transport = loginRoundTripper(func(r *http.Request) (*http.Response, error) {
		r.RemoteAddr = "203.0.113.24:51234"
		return transport.RoundTrip(r)
	})
	code, m := e.call("POST", "/api/auth/login", map[string]string{"email": owner.Email, "password": "s3cret-pass"}, "User-Agent", "Mozilla/5.0 Firefox/135.0", "X-Forwarded-For", "198.51.100.99")
	if code != 200 {
		t.Fatalf("login = %d %v", code, m)
	}
	u, _ := url.Parse(e.url)
	var currentToken string
	for _, cookie := range e.client.Jar.Cookies(u) {
		if cookie.Name == brand.SessionCookie() {
			currentToken = cookie.Value
		}
	}
	if currentToken == "" {
		t.Fatal("no current session")
	}
	otherToken, _, err := e.accounts.NewSession(ctx, owner.ID, store.SessionDetails{IPAddress: "198.51.100.12", UserAgent: "Safari/605.1"})
	if err != nil {
		t.Fatal(err)
	}
	thirdToken, _, err := e.accounts.NewSession(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.srv.store.CreateSession(ctx, owner.ID, auth.HashToken("expired-session"), time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	foreignOwner, err := e.srv.store.CreateUser(ctx, "another-owner@example.com", "unused", "owner")
	if err != nil {
		t.Fatal(err)
	}
	foreignToken, _, err := e.accounts.NewSession(ctx, foreignOwner.ID)
	if err != nil {
		t.Fatal(err)
	}
	foreignSessions, err := e.srv.store.UserSessions(ctx, foreignOwner.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	sessions := listedSessions(t, e)
	if len(sessions) != 3 || !sessions[0].Current {
		t.Fatalf("active sessions = %+v", sessions)
	}
	currentID := sessions[0].ID
	if sessions[0].IPAddress != "203.0.113.24" || sessions[0].UserAgent != "Mozilla/5.0 Firefox/135.0" || sessions[0].LastUsedAt.IsZero() {
		t.Fatalf("metadata = %+v", sessions[0])
	}
	raw, _ := json.Marshal(sessions)
	for _, secret := range []string{currentToken, auth.HashToken(currentToken), otherToken, auth.HashToken(otherToken), "password_hash", "token_hash"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("session list leaked credentials")
		}
	}
	var otherID string
	for _, session := range sessions {
		if session.IPAddress == "198.51.100.12" {
			otherID = session.ID
		}
	}
	if otherID == "" {
		t.Fatal("missing other session")
	}
	mustStatus(t, e, "DELETE", "/api/auth/sessions/"+foreignSessions[0].ID, map[string]string{"password": "s3cret-pass"}, 404)
	mustStatus(t, e, "DELETE", "/api/auth/sessions/"+otherID, map[string]string{"password": "wrong"}, 401)
	if _, err := e.accounts.SessionUser(ctx, otherToken); err != nil {
		t.Fatal("bad password revoked a session")
	}
	mustStatus(t, e, "DELETE", "/api/auth/sessions/"+otherID, map[string]string{"password": "s3cret-pass"}, 204)
	if _, err := e.accounts.SessionUser(ctx, otherToken); err == nil {
		t.Fatal("revoked session still authenticates")
	}
	if _, err := e.accounts.SessionUser(ctx, thirdToken); err != nil {
		t.Fatal("unselected session was revoked")
	}
	mustStatus(t, e, "DELETE", "/api/auth/sessions/others", map[string]string{"password": "wrong"}, 401)
	if _, err := e.accounts.SessionUser(ctx, thirdToken); err != nil {
		t.Fatal("bad password revoked other sessions")
	}
	mustStatus(t, e, "DELETE", "/api/auth/sessions/others", map[string]string{"password": "s3cret-pass"}, 204)
	if _, err := e.accounts.SessionUser(ctx, thirdToken); err == nil {
		t.Fatal("other session still authenticates")
	}
	if _, err := e.accounts.SessionUser(ctx, currentToken); err != nil {
		t.Fatal("current session was revoked", err)
	}
	if _, err := e.accounts.SessionUser(ctx, foreignToken); err != nil {
		t.Fatal("another owner's session was revoked")
	}
	sessions = listedSessions(t, e)
	if len(sessions) != 1 || !sessions[0].Current || sessions[0].ID != currentID {
		t.Fatalf("keep current = %+v", sessions)
	}
	// Idempotently logging out all others always keeps this session.
	mustStatus(t, e, "DELETE", "/api/auth/sessions/others", map[string]string{"password": "s3cret-pass"}, 204)
	mustStatus(t, e, "DELETE", "/api/auth/sessions/"+currentID, nil, 204)
	if _, err := e.accounts.SessionUser(ctx, currentToken); err == nil {
		t.Fatal("current session still authenticates after logout")
	}
	for _, cookie := range e.client.Jar.Cookies(u) {
		if cookie.Name == brand.SessionCookie() {
			t.Fatal("current cookie not cleared")
		}
	}
	mustStatus(t, e, "GET", "/api/auth/me", nil, 401)
	mustStatus(t, e, "GET", "/api/auth/sessions", nil, 401)
}

func TestSessionManagementRequiresBrowserSession(t *testing.T) {
	e, owner := loginMethodEnv(t)
	apiToken := auth.NewToken()
	if _, err := e.srv.store.CreateAPIToken(context.Background(), owner.ID, "test", auth.HashToken(apiToken)); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/auth/sessions", "/api/auth/sessions/others", "/api/auth/sessions/unknown"} {
		method := "DELETE"
		if path == "/api/auth/sessions" {
			method = "GET"
		}
		if code, _ := e.call(method, path, map[string]string{"password": "s3cret-pass"}, "Authorization", "Bearer "+apiToken); code != 403 {
			t.Fatalf("bearer %s = %d", path, code)
		}
	}
	if code, _ := e.call("DELETE", "/api/auth/sessions/others", map[string]string{"password": "s3cret-pass"}, "Origin", "https://foreign.example"); code != 403 {
		t.Fatalf("cross-origin revoke = %d", code)
	}
	mustStatus(t, e, "DELETE", "/api/auth/sessions/others", map[string]string{"password": "s3cret-pass", "keep_id": "attacker-selected"}, 400)
}

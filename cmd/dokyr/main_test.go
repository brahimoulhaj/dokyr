package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakePanel answers what the CLI calls. The exchange checks the verifier against the
// challenge the browser was given, as the panel does.
func fakePanel(t *testing.T, challenge *string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/meta", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "Dokyr", "slug": "dokyr", "version": "test", "setup_required": false})
	})
	mux.HandleFunc("POST /api/cli/token", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Code, Verifier string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		sum := sha256.Sum256([]byte(in.Verifier))
		if in.Code != "the-code" || base64.RawURLEncoding.EncodeToString(sum[:]) != *challenge {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"code":"invalid_grant","message":"this sign-in code is not valid"}}`)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"token":"dokyr_secret","token_name":"CLI on test","user":{"email":"a@b.co","role":"owner"}}`)
	})
	mux.HandleFunc("GET /api/projects", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer dokyr_secret" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"code":"unauthorized","message":"invalid or revoked API token"}}`)
			return
		}
		_, _ = io.WriteString(w, `[{"id":"01P","name":"shop","app_count":3,"apps_running":2,"databases":1,"domains":4,"last_deploy_at":"2026-11-19T10:00:00Z","last_deploy_status":"succeeded"}]`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestLoginThenProjects(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	var challenge string
	panel := fakePanel(t, &challenge)

	// Signed out, a command says how to sign in.
	if err := run(context.Background(), []string{"projects", "list"}, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "not signed in") {
		t.Fatalf("projects signed out = %v", err)
	}

	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := run(context.Background(), []string{"login", "--no-open", panel.URL}, pw, io.Discard)
		pw.Close()
		done <- err
	}()

	// The browser's part: read the link, and come back the way the panel redirects.
	var link *url.URL
	sc := bufio.NewScanner(pr)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); strings.HasPrefix(line, panel.URL+"/cli/authorize?") {
			link, _ = url.Parse(line)
			break
		}
	}
	if link == nil {
		t.Fatalf("no sign-in link printed: %v", <-done)
	}
	go func() { _, _ = io.Copy(io.Discard, pr) }()
	q := link.Query()
	challenge = q.Get("challenge")
	if len(challenge) != 43 || len(q.Get("state")) < 16 || len(q.Get("code")) != 9 || q.Get("port") == "" {
		t.Fatalf("link = %s", link)
	}
	back := "http://127.0.0.1:" + q.Get("port") + "/callback"

	// A request without this run's state is refused and does not end the wait.
	res, err := http.Get(back + "?code=stolen&state=someone-else")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("callback with a foreign state = %d", res.StatusCode)
	}
	res, err = http.Get(back + "?" + url.Values{"code": {"the-code"}, "state": {q.Get("state")}}.Encode())
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !strings.Contains(string(page), "Signed in") {
		t.Fatalf("callback = %d %s", res.StatusCode, page)
	}
	if err := <-done; err != nil {
		t.Fatalf("login = %v", err)
	}

	path := filepath.Join(dir, "dokyr", "config.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %v, want 600", info.Mode().Perm())
	}
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	host := strings.TrimPrefix(panel.URL, "http://")
	if pc := cfg.Contexts[host]; cfg.Current != host || pc.Token != "dokyr_secret" || pc.User != "a@b.co" || pc.URL != panel.URL {
		t.Fatalf("config = %+v", cfg)
	}

	var out bytes.Buffer
	if err := run(context.Background(), []string{"projects", "list"}, &out, io.Discard); err != nil {
		t.Fatalf("projects list = %v", err)
	}
	if got := out.String(); !strings.Contains(got, "NAME") || !strings.Contains(got, "shop") || !strings.Contains(got, "succeeded") || !strings.Contains(got, "01P") {
		t.Fatalf("projects list =\n%s", got)
	}
	out.Reset()
	if err := run(context.Background(), []string{"projects", "list", "--json"}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	var list []map[string]any
	if err := json.Unmarshal(out.Bytes(), &list); err != nil || len(list) != 1 || list[0]["name"] != "shop" {
		t.Fatalf("projects list --json = %s (%v)", out.String(), err)
	}

	// A revoked token: the command says to sign in again.
	cfg.Contexts[host] = panelContext{URL: panel.URL, Token: "dokyr_revoked"}
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"projects", "list"}, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "no longer accepts") {
		t.Fatalf("projects with a revoked token = %v", err)
	}
}

func TestPanelURL(t *testing.T) {
	for in, want := range map[string]string{
		"panel.example.com":             "https://panel.example.com",
		"https://panel.example.com/":    "https://panel.example.com",
		"https://panel.example.com/x?y": "https://panel.example.com",
		"http://localhost:5173":         "http://localhost:5173",
		"http://127.0.0.1:10000/":       "http://127.0.0.1:10000",
		"http://[::1]:10000":            "http://[::1]:10000",
	} {
		u, err := panelURL(in, false)
		if err != nil || u.String() != want {
			t.Errorf("panelURL(%q) = %v, %v; want %s", in, u, err, want)
		}
	}
	for _, bad := range []string{"http://203.0.113.5", "http://panel.example.com", "ftp://panel.example.com", "https://user:pass@panel.example.com", "https://", ""} {
		if u, err := panelURL(bad, false); err == nil {
			t.Errorf("panelURL(%q) = %s, want an error", bad, u)
		}
	}
	if u, err := panelURL("http://203.0.113.5", true); err != nil || u.String() != "http://203.0.113.5" {
		t.Errorf("panelURL with --insecure = %v, %v", u, err)
	}
}

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
func fakePanel(t *testing.T, challenge *string, revoked *[]string) *httptest.Server {
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
	mux.HandleFunc("GET /api/auth/me", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer dokyr_secret" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"code":"unauthorized","message":"invalid or revoked API token"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"01U","email":"a@b.co","role":"admin"}`)
	})
	mux.HandleFunc("POST /api/cli/logout", func(w http.ResponseWriter, r *http.Request) {
		*revoked = append(*revoked, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/projects", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer dokyr_secret" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"code":"unauthorized","message":"invalid or revoked API token"}}`)
			return
		}
		_, _ = io.WriteString(w, `[{"id":"01P","name":"shop","app_count":3,"apps_running":2,"databases":1,"domains":4,"last_deploy_at":"2026-11-19T10:00:00Z","last_deploy_status":"succeeded"},`+
			`{"id":"02P","name":"Blog","app_count":1},{"id":"03P","name":"empty","app_count":0}]`)
	})
	mux.HandleFunc("GET /api/projects/{id}/apps", func(w http.ResponseWriter, r *http.Request) {
		switch r.PathValue("id") {
		case "01P":
			_, _ = io.WriteString(w, `[{"id":"01A","project_id":"01P","name":"api","source_type":"git","git_repository":"https://github.com/org/api.git","git_branch":"main","port":8080,"desired_state":"running","runtime_state":"running","extra":1},`+
				`{"id":"01B","project_id":"01P","name":"web","source_type":"image","image":"nginx:1.27","port":80,"desired_state":"stopped","runtime_state":"exited"}]`)
		case "02P":
			_, _ = io.WriteString(w, `[{"id":"02A","project_id":"02P","name":"api","source_type":"image","image":"ghcr.io/org/api:2","port":0,"runtime_state":"none"}]`)
		default:
			_, _ = io.WriteString(w, `[]`)
		}
	})
	mux.HandleFunc("GET /api/apps/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") != "01A" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":{"code":"not_found","message":"not found"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"01A","project_id":"01P","name":"api","source_type":"git","git_repository":"org/api","git_branch":"main","builder":"dockerfile","port":8080,`+
			`"cpu_limit":0.5,"memory_limit_mb":512,"runtime_state":"running","needs_redeploy":true,"environment":{"A":"1","B":"2"},`+
			`"health":{"status":"healthy","reason":"","restarts":2},"process_states":[{"name":"worker","state":"running"}],"created_at":"2026-11-01T10:00:00Z"}`)
	})
	mux.HandleFunc("GET /api/apps/{id}/routes", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[{"url":"https://api.example.com/"},{"url":"https://example.com/api"}]`)
	})
	mux.HandleFunc("GET /api/registry/settings", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"enabled":true,"domain":"registry.example.com","address":"registry.example.com","state":"running"}`)
	})
	mux.HandleFunc("GET /api/registry/repositories", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer dokyr_secret" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"code":"unauthorized","message":"invalid or revoked API token"}}`)
			return
		}
		_, _ = io.WriteString(w, `[{"name":"shop/api","size":52428800,"tags":[`+
			`{"name":"latest","digest":"sha256:0123456789abcdef0123456789abcdef","size":41943040,"platforms":["linux/amd64","linux/arm64"]},`+
			`{"name":"broken","size":0,"platforms":[]}]},`+
			`{"name":"blog","size":999,"tags":[{"name":"v1","digest":"sha256:fedcba9876543210fedcba9876543210","size":999,"platforms":["linux/amd64"]}]}]`)
	})
	mux.HandleFunc("POST /api/projects", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Name, Description string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		if r.Header.Get("Authorization") != "Bearer dokyr_secret" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"code":"unauthorized","message":"invalid or revoked API token"}}`)
			return
		}
		if in.Name == "shop" {
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"error":{"code":"conflict","message":"a project with this name already exists"}}`)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "01NEW", "name": in.Name, "description": in.Description, "app_count": 0})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// plain is a command's streams when nobody is at a terminal: a pipe, a script, a test.
func plain(out io.Writer) streams {
	return streams{in: strings.NewReader(""), out: out, errOut: io.Discard}
}

func TestLoginThenProjects(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	var challenge string
	var revoked []string
	panel := fakePanel(t, &challenge, &revoked)

	// Signed out: status says so and fails, logout has nothing to do.
	var first bytes.Buffer
	if err := run(context.Background(), []string{"status"}, plain(&first)); err != errStatusShown || !strings.Contains(first.String(), "Not signed in") {
		t.Fatalf("status signed out = %v %q", err, first.String())
	}
	if err := run(context.Background(), []string{"logout"}, plain(&first)); err != nil {
		t.Fatalf("logout signed out = %v", err)
	}
	// Signed out, a command says how to sign in.
	if err := run(context.Background(), []string{"projects", "list"}, plain(io.Discard)); err == nil || !strings.Contains(err.Error(), "not signed in") {
		t.Fatalf("projects signed out = %v", err)
	}

	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := run(context.Background(), []string{"login", "--no-open", panel.URL}, plain(pw))
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
	if err := run(context.Background(), []string{"projects", "list"}, plain(&out)); err != nil {
		t.Fatalf("projects list = %v", err)
	}
	if got := out.String(); !strings.Contains(got, "NAME") || !strings.Contains(got, "shop") || !strings.Contains(got, "succeeded") || !strings.Contains(got, "01P") {
		t.Fatalf("projects list =\n%s", got)
	}
	out.Reset()
	if err := run(context.Background(), []string{"projects", "list", "--json"}, plain(&out)); err != nil {
		t.Fatal(err)
	}
	var list []map[string]any
	if err := json.Unmarshal(out.Bytes(), &list); err != nil || len(list) != 3 || list[0]["name"] != "shop" {
		t.Fatalf("projects list --json = %s (%v)", out.String(), err)
	}

	// Apps: across the projects, or of one; an app by its name or its id.
	out.Reset()
	if err := run(context.Background(), []string{"apps", "list"}, plain(&out)); err != nil {
		t.Fatalf("apps list = %v", err)
	}
	for _, want := range []string{"APP", " github.com/org/api (main)", "nginx:1.27", "stopped", "not deployed", "Blog", "01B", "02A"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("apps list lacks %q:\n%s", want, out.String())
		}
	}
	out.Reset()
	if err := run(context.Background(), []string{"apps", "ls", "-p", "blog"}, plain(&out)); err != nil || strings.Contains(out.String(), "nginx") || !strings.Contains(out.String(), "ghcr.io/org/api:2") {
		t.Fatalf("apps ls -p blog = %v\n%s", err, out.String())
	}
	out.Reset()
	if err := run(context.Background(), []string{"apps", "list", "--project", "empty"}, plain(&out)); err != nil || !strings.Contains(out.String(), "No apps on project empty") {
		t.Fatalf("apps list of an empty project = %v %q", err, out.String())
	}
	if err := run(context.Background(), []string{"apps", "list", "--project", "nope"}, plain(io.Discard)); err == nil || !strings.Contains(err.Error(), "no project") {
		t.Fatalf("apps list of an unknown project = %v", err)
	}
	out.Reset()
	if err := run(context.Background(), []string{"apps", "list", "--json"}, plain(&out)); err != nil {
		t.Fatal(err)
	}
	var listed []map[string]any
	if err := json.Unmarshal(out.Bytes(), &listed); err != nil || len(listed) != 3 || listed[0]["extra"] != float64(1) {
		t.Fatalf("apps list --json = %s (%v)", out.String(), err) // fields the CLI does not know are kept
	}

	if err := run(context.Background(), []string{"apps", "show", "api"}, plain(io.Discard)); err == nil || !strings.Contains(err.Error(), "Blog, shop") {
		t.Fatalf("apps show of a name two projects have = %v", err)
	}
	if err := run(context.Background(), []string{"apps", "show", "nope"}, plain(io.Discard)); err == nil || !strings.Contains(err.Error(), "no app") {
		t.Fatalf("apps show of an unknown app = %v", err)
	}
	for _, args := range [][]string{{"apps", "show", "api", "--project", "shop"}, {"apps", "show", "01A"}} {
		out.Reset()
		if err := run(context.Background(), args, plain(&out)); err != nil {
			t.Fatalf("%v = %v", args, err)
		}
		for _, want := range []string{"api", "shop", "running", "healthy (2 restarts)", "org/api (main)", "dockerfile", "8080", "https://api.example.com/", "https://example.com/api",
			"0.5 CPU, 512 MB", "Process worker", "Variables:   2", "not deployed yet", "01A"} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("%v lacks %q:\n%s", args, want, out.String())
			}
		}
	}
	out.Reset()
	if err := run(context.Background(), []string{"apps", "show", "01A", "--json"}, plain(&out)); err != nil || !strings.Contains(out.String(), `"needs_redeploy":true`) {
		t.Fatalf("apps show --json = %v %s", err, out.String())
	}

	// Registry: one line per tag, named the way it is pulled.
	out.Reset()
	if err := run(context.Background(), []string{"registry", "list"}, plain(&out)); err != nil {
		t.Fatalf("registry list = %v", err)
	}
	for _, want := range []string{"IMAGE", "registry.example.com/shop/api:latest", "41.9 MB", "linux/amd64, linux/arm64", "sha256:0123456789ab\n",
		"registry.example.com/shop/api:broken", "registry.example.com/blog:v1", "999 B"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("registry list lacks %q:\n%s", want, out.String())
		}
	}
	out.Reset()
	if err := run(context.Background(), []string{"registry", "ls", "blog"}, plain(&out)); err != nil || strings.Contains(out.String(), "shop/api") || !strings.Contains(out.String(), "blog:v1") {
		t.Fatalf("registry ls blog = %v\n%s", err, out.String())
	}
	if err := run(context.Background(), []string{"registry", "list", "nope"}, plain(io.Discard)); err == nil || !strings.Contains(err.Error(), "no repository") {
		t.Fatalf("registry list of an unknown repository = %v", err)
	}
	out.Reset()
	if err := run(context.Background(), []string{"registry", "images", "--json"}, plain(&out)); err != nil {
		t.Fatal(err)
	}
	var repos []repository
	if err := json.Unmarshal(out.Bytes(), &repos); err != nil || len(repos) != 2 || repos[0].Name != "shop/api" || len(repos[0].Tags) != 2 {
		t.Fatalf("registry list --json = %s (%v)", out.String(), err)
	}

	// Create: the name before or after the flags, the panel's refusal as it comes.
	out.Reset()
	if err := run(context.Background(), []string{"projects", "create", "blog", "--description", "the company blog"}, plain(&out)); err != nil {
		t.Fatalf("projects create = %v", err)
	}
	if got := out.String(); !strings.Contains(got, "Created project blog") || !strings.Contains(got, "01NEW") {
		t.Fatalf("projects create = %s", got)
	}
	out.Reset()
	if err := run(context.Background(), []string{"projects", "create", "--json", "--description", "notes", "my wiki"}, plain(&out)); err != nil {
		t.Fatal(err)
	}
	var made map[string]any
	if err := json.Unmarshal(out.Bytes(), &made); err != nil || made["name"] != "my wiki" || made["description"] != "notes" {
		t.Fatalf("projects create --json = %s (%v)", out.String(), err)
	}
	if err := run(context.Background(), []string{"projects", "create", "shop"}, plain(io.Discard)); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("projects create of a taken name = %v", err)
	}
	for _, bad := range [][]string{{"projects", "create"}, {"projects", "create", "  "}, {"projects", "create", "a", "b"},
		{"projects", "delete", "a"}, {"projects", "list", "extra"}, {"projects", "list", "--nope"}, {"nope"}, {"login"}} {
		if err := run(context.Background(), bad, plain(io.Discard)); err == nil {
			t.Errorf("%v = no error", bad)
		}
	}

	// Status asks the panel: the role changed there since the sign-in, and it shows.
	out.Reset()
	if err := run(context.Background(), []string{"whoami"}, plain(&out)); err != nil {
		t.Fatalf("whoami = %v", err)
	}
	for _, want := range []string{panel.URL, "a@b.co", "admin", "signed in", "Panel version:  test"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("status lacks %q:\n%s", want, out.String())
		}
	}
	out.Reset()
	if err := run(context.Background(), []string{"status", "--json"}, plain(&out)); err != nil {
		t.Fatal(err)
	}
	var st status
	if err := json.Unmarshal(out.Bytes(), &st); err != nil || !st.SignedIn || st.User != "a@b.co" || st.Role != "admin" || st.Panel != host {
		t.Fatalf("status --json = %s (%v)", out.String(), err)
	}
	if strings.Contains(out.String(), "dokyr_secret") {
		t.Fatal("status prints the token")
	}

	// A revoked token: the command says to sign in again.
	cfg.Contexts[host] = panelContext{URL: panel.URL, Token: "dokyr_revoked"}
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"projects", "list"}, plain(io.Discard)); err == nil || !strings.Contains(err.Error(), "no longer accepts") {
		t.Fatalf("projects with a revoked token = %v", err)
	}
	if err := run(context.Background(), []string{"projects", "create", "blog"}, plain(io.Discard)); err == nil || !strings.Contains(err.Error(), "no longer accepts") {
		t.Fatalf("create with a revoked token = %v", err)
	}
	out.Reset()
	if err := run(context.Background(), []string{"status"}, plain(&out)); err != errStatusShown || !strings.Contains(out.String(), "no longer accepts") {
		t.Fatalf("status with a revoked token = %v\n%s", err, out.String())
	}

	// Logout revokes the token on the panel and forgets it here.
	out.Reset()
	if err := run(context.Background(), []string{"logout"}, plain(&out)); err != nil || !strings.Contains(out.String(), "Signed out of "+host) {
		t.Fatalf("logout = %v %q", err, out.String())
	}
	if len(revoked) != 1 || revoked[0] != "Bearer dokyr_revoked" {
		t.Fatalf("revoked on the panel = %v", revoked)
	}
	if cfg, _ := loadConfig(); len(cfg.Contexts) != 0 || cfg.Current != "" {
		t.Fatalf("config after logout = %+v", cfg)
	}
	if err := run(context.Background(), []string{"projects", "list"}, plain(io.Discard)); err == nil || !strings.Contains(err.Error(), "not signed in") {
		t.Fatalf("projects after logout = %v", err)
	}
}

// Help and completion come from cobra; they must name the commands there are.
func TestHelpAndCompletion(t *testing.T) {
	var out bytes.Buffer
	if err := run(context.Background(), []string{"--help"}, plain(&out)); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"login", "logout", "status", "projects", "apps", "registry", "completion", "version"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help does not name %q:\n%s", want, out.String())
		}
	}
	out.Reset()
	if err := run(context.Background(), []string{"projects", "create", "--help"}, plain(&out)); err != nil || !strings.Contains(out.String(), "--description") {
		t.Fatalf("projects create --help = %v\n%s", err, out.String())
	}
	out.Reset()
	if err := run(context.Background(), []string{"completion", "zsh"}, plain(&out)); err != nil || !strings.Contains(out.String(), "compdef") {
		t.Fatalf("completion zsh = %v", err)
	}
	out.Reset()
	if err := run(context.Background(), []string{"version"}, plain(&out)); err != nil || strings.TrimSpace(out.String()) != version {
		t.Fatalf("version = %v %q", err, out.String())
	}
}

func TestHumanSize(t *testing.T) {
	for n, want := range map[int64]string{0: "0 B", 999: "999 B", 1000: "1.0 kB", 1500: "1.5 kB", 41943040: "41.9 MB", 240_000_000: "240 MB", 3_200_000_000: "3.2 GB"} {
		if got := humanSize(n); got != want {
			t.Errorf("humanSize(%d) = %q, want %q", n, got, want)
		}
	}
	if got := shortDigest("sha256:0123456789abcdef"); got != "sha256:0123456789ab" {
		t.Errorf("shortDigest = %q", got)
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

package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"panel/internal/gitsource"
	"panel/internal/secret"
	"panel/internal/store"
)

// hook calls a deploy webhook the way a Git host or CI does: no cookie, maybe event
// headers, a JSON body.
func (e *env) hook(token string, headers map[string]string, body string) (int, map[string]any) {
	e.t.Helper()
	req, _ := http.NewRequest("POST", e.url+"/api/hooks/deploy/"+token, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(res.Body).Decode(&m)
	return res.StatusCode, m
}

func TestDeployHook(t *testing.T) {
	e := newEnv(t)
	e.signIn()
	pid := e.must(201, "POST", "/api/projects", map[string]any{"name": "Shop"})["id"].(string)
	app := e.must(201, "POST", "/api/projects/"+pid+"/apps", map[string]any{"name": "web", "image": "ghcr.io/org/web:1", "port": 80})
	id := app["id"].(string)
	if app["deploy_hook"] != false {
		t.Fatalf("hook on by default: %v", app)
	}
	if h := e.must(200, "GET", "/api/apps/"+id+"/deploy-hook", nil); h["enabled"] != false || h["token"] != nil {
		t.Fatalf("hook = %v", h)
	}
	token := e.must(200, "POST", "/api/apps/"+id+"/deploy-hook", nil)["token"].(string)
	if len(token) != 48 || e.must(200, "GET", "/api/apps/"+id, nil)["deploy_hook"] != true {
		t.Fatalf("token %q", token)
	}
	if code, _ := e.hook("not-a-token", nil, ""); code != 404 {
		t.Errorf("unknown token = %d", code)
	}

	// A CI call deploys; with a tag it switches the image's tag first.
	if code, m := e.hook(token, nil, ""); code != 202 || m["status"] != "deploying" {
		t.Fatalf("call = %d %v", code, m)
	}
	e.deployer.Wait()
	if code, m := e.hook(token, nil, `{"tag":"2.0.1"}`); code != 202 {
		t.Fatalf("tag = %d %v", code, m)
	}
	e.deployer.Wait()
	if a := e.must(200, "GET", "/api/apps/"+id, nil); a["image"] != "ghcr.io/org/web:2.0.1" {
		t.Errorf("image after tag = %v", a["image"])
	}
	if code, _ := e.hook(token, nil, `{"tag":"../x"}`); code != 422 {
		t.Errorf("bad tag = %d", code)
	}
	if code, _ := e.hook(token, nil, `not json`); code != 422 {
		t.Errorf("bad body = %d", code)
	}
	// An image app ignores Git pushes.
	if code, m := e.hook(token, map[string]string{"X-GitHub-Event": "push"}, `{"ref":"refs/heads/main"}`); code != 200 || m["status"] != "ignored" {
		t.Errorf("push to image app = %d %v", code, m)
	}

	// Pushes while a deploy runs queue one more deploy, however many arrive.
	e.ex.PullDelay = 300 * time.Millisecond
	statuses := []any{}
	for range 3 {
		_, m := e.hook(token, nil, "")
		statuses = append(statuses, m["status"])
	}
	if statuses[0] != "deploying" || statuses[1] != "queued" || statuses[2] != "queued" {
		t.Errorf("statuses = %v", statuses)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(e.list("/api/apps/"+id+"/deployments")) < 4 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	e.deployer.Wait()
	e.ex.PullDelay = 0
	if deps := e.list("/api/apps/" + id + "/deployments"); len(deps) != 4 || deps[0]["trigger"] != "webhook" {
		t.Errorf("deployments after a burst = %d (want 2 + 2): %v", len(deps), deps[0]["trigger"])
	}

	// Git apps: only pushes to their branch deploy.
	e.must(200, "PATCH", "/api/apps/"+id, map[string]any{"source_type": "git", "git_repository": "https://github.com/org/web.git", "git_branch": "main"})
	for _, c := range []struct {
		headers map[string]string
		body    string
		code    int
		status  string
	}{
		{map[string]string{"X-GitHub-Event": "ping"}, `{"zen":"hi"}`, 200, "ok"},
		{map[string]string{"X-GitHub-Event": "push"}, `{"ref":"refs/heads/dev"}`, 200, "ignored"},
		{map[string]string{"X-GitHub-Event": "push"}, `{"ref":"refs/tags/v1"}`, 200, "ignored"},
		{map[string]string{"X-GitHub-Event": "push"}, `{"ref":"refs/heads/main","deleted":true,"after":"0000000000000000000000000000000000000000"}`, 200, "ignored"},
		{map[string]string{"X-GitHub-Event": "issues"}, `{}`, 200, "ignored"},
		{map[string]string{"X-GitHub-Event": "push"}, `{"ref":"refs/heads/main","after":"abc"}`, 202, "deploying"},
		{map[string]string{"X-Gitlab-Event": "Push Hook"}, `{"ref":"refs/heads/main"}`, 202, "deploying"},
		{map[string]string{"X-Gitea-Event": "push"}, `{"ref":"refs/heads/main"}`, 202, "deploying"},
		{nil, `{"ref":"refs/heads/main"}`, 202, "deploying"},
		{map[string]string{"X-GitHub-Event": "push", "Content-Type": "application/x-www-form-urlencoded"},
			"payload=" + url.QueryEscape(`{"ref":"refs/heads/main"}`), 202, "deploying"},
	} {
		code, m := e.hook(token, c.headers, c.body)
		if code != c.code || m["status"] != c.status {
			t.Errorf("%v %s = %d %v, want %d %s", c.headers, c.body, code, m, c.code, c.status)
		}
		e.deployer.Wait()
	}
	if code, _ := e.hook(token, nil, `{"tag":"3"}`); code != 422 {
		t.Errorf("tag on a git app = %d", code)
	}

	// A new URL replaces the old one; turning it off stops it.
	newToken := e.must(200, "POST", "/api/apps/"+id+"/deploy-hook", nil)["token"].(string)
	if code, _ := e.hook(token, nil, ""); code != 404 || newToken == token {
		t.Errorf("old token after regenerate = %d", code)
	}
	e.must(200, "DELETE", "/api/apps/"+id+"/deploy-hook", nil)
	if code, _ := e.hook(newToken, nil, ""); code != 404 {
		t.Errorf("token after off = %d", code)
	}
	found := false
	for _, ev := range e.list("/api/events") {
		if ev["actor"] == "webhook" && ev["action"] == "app.deploy" {
			found = true
		}
	}
	if !found {
		t.Error("no webhook event in the activity")
	}
}

func TestWithTag(t *testing.T) {
	for in, want := range map[string]string{
		"nginx":                           "nginx:2",
		"nginx:1":                         "nginx:2",
		"ghcr.io/org/app:1":               "ghcr.io/org/app:2",
		"registry.example.com:5000/app":   "registry.example.com:5000/app:2",
		"registry.example.com:5000/app:1": "registry.example.com:5000/app:2",
		"ghcr.io/org/app@sha256:abc":      "ghcr.io/org/app:2",
		"ghcr.io/org/app:1@sha256:abc":    "ghcr.io/org/app:2",
	} {
		if got := withTag(in, "2"); got != want {
			t.Errorf("withTag(%q) = %q, want %q", in, got, want)
		}
	}
}

// A Git app with watch paths deploys only on pushes that touch them; when the host does
// not say what changed, it deploys.
func TestDeployHookWatchPaths(t *testing.T) {
	e := newEnv(t)
	e.signIn()
	pid := e.must(201, "POST", "/api/projects", map[string]any{"name": "Mono"})["id"].(string)
	git := map[string]any{"name": "api", "source_type": "git", "git_repository": "https://github.com/org/mono.git", "git_branch": "main", "port": 80}
	git["watch_paths"] = []string{"../outside"}
	e.must(422, "POST", "/api/projects/"+pid+"/apps", git)
	git["watch_paths"] = []string{" ./services/api/ ", "packages/shared", "!**/*.md", ""}
	app := e.must(201, "POST", "/api/projects/"+pid+"/apps", git)
	id := app["id"].(string)
	if got := app["watch_paths"].([]any); len(got) != 3 || got[0] != "services/api/" || got[2] != "!**/*.md" {
		t.Fatalf("watch_paths = %v", got)
	}
	token := e.must(200, "POST", "/api/apps/"+id+"/deploy-hook", nil)["token"].(string)
	push := map[string]string{"X-GitHub-Event": "push"}
	for i, c := range []struct {
		body   string
		status string
	}{
		{`{"ref":"refs/heads/main","after":"a1","commits":[{"modified":["services/web/index.ts"]}]}`, "ignored"},
		{`{"ref":"refs/heads/main","after":"a2","commits":[{"added":["services/api/README.md"]}]}`, "ignored"},
		{`{"ref":"refs/heads/main","after":"a3","commits":[{"added":["x"]},{"removed":["services/api/old.go"]}]}`, "deploying"},
		{`{"ref":"refs/heads/main","after":"a3","commits":[{"removed":["services/api/old.go"]}]}`, "ignored"}, // the same commit again
		{`{"ref":"refs/heads/main","after":"a4","commits":[{"modified":["packages/shared/log.go"]}]}`, "deploying"},
		// Nothing can be said about these: no commit list, a forced push, a new branch, a cut list.
		{`{"ref":"refs/heads/main","after":"a5"}`, "deploying"},
		{`{"ref":"refs/heads/main","after":"a6","forced":true,"commits":[{"modified":["docs/a"]}]}`, "deploying"},
		{`{"ref":"refs/heads/main","after":"a7","created":true,"commits":[{"modified":["docs/a"]}]}`, "deploying"},
		{`{"ref":"refs/heads/main","after":"a8","total_commits_count":30,"commits":[{"modified":["docs/a"]}]}`, "deploying"},
	} {
		_, m := e.hook(token, push, c.body)
		if m["status"] != c.status {
			t.Errorf("push %d = %v, want %s", i, m, c.status)
		}
		e.deployer.Wait()
	}
	// Watch paths are not a setting a deployment applies.
	e.must(200, "PATCH", "/api/apps/"+id, map[string]any{"watch_paths": []string{}})
	if a := e.must(200, "GET", "/api/apps/"+id, nil); len(a["watch_paths"].([]any)) != 0 || a["needs_redeploy"] == true {
		t.Errorf("after clearing: %v %v", a["watch_paths"], a["needs_redeploy"])
	}
}

// The webhook of a GitHub App connection: signed deliveries only, and a push deploys the
// connection's apps on that repository and branch that have deploy on push turned on.
func TestGitHubAppHook(t *testing.T) {
	e := newEnv(t)
	e.signIn()
	post := func(appID, secret, event, body string) (int, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest("POST", e.url+"/api/hooks/github", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-GitHub-Event", event)
		if appID != "" {
			req.Header.Set("X-GitHub-Hook-Installation-Target-ID", appID)
		}
		if secret != "" {
			mac := hmac.New(sha256.New, []byte(secret))
			mac.Write([]byte(body))
			req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var m map[string]any
		_ = json.NewDecoder(res.Body).Decode(&m)
		return res.StatusCode, m
	}
	if code, _ := post("42", "hook-secret", "ping", `{}`); code != 503 {
		t.Fatalf("without Git connections = %d", code)
	}

	// A GitHub that answers the manifest exchange with an app whose webhook has a secret.
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, _ := rsa.GenerateKey(rand.Reader, 2048)
		pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
		json.NewEncoder(w).Encode(map[string]any{"id": 42, "slug": "panel-app", "name": "Panel app", "html_url": "https://github.com/apps/panel-app",
			"client_id": "Iv1", "client_secret": "cs", "webhook_secret": "hook-secret", "pem": string(pemKey)})
	}))
	defer github.Close()
	box, err := secret.LoadOrCreate(filepath.Join(t.TempDir(), "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	svc := gitsource.New(e.srv.store, box)
	svc.GitHubAPI = github.URL
	e.srv.SetGitSources(svc)
	m, err := svc.StartGitHub("https://panel.example", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.Manifest, `"hook_attributes":{"active":true,"url":"https://panel.example/api/hooks/github"}`) || !strings.Contains(m.Manifest, `"default_events":["push"]`) {
		t.Fatalf("the manifest does not ask for the app's push webhook: %s", m.Manifest)
	}
	conn, err := svc.FinishGitHub(context.Background(), "code123", m.State)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.list("/api/git-sources"); len(got) != 1 || got[0]["push_events"] != true {
		t.Fatalf("connection = %v", got)
	}

	pid := e.must(201, "POST", "/api/projects", map[string]any{"name": "Mono"})["id"].(string)
	ids := map[string]string{}
	for name, cfg := range map[string]struct {
		repo, branch string
		watch        []string
	}{
		"api":   {"https://github.com/Org/Mono.git", "main", []string{"services/api"}},
		"web":   {"https://github.com/org/mono", "main", nil},
		"next":  {"https://github.com/org/mono.git", "next", nil},
		"other": {"https://github.com/org/other.git", "main", nil},
	} {
		a := e.must(201, "POST", "/api/projects/"+pid+"/apps", map[string]any{"name": name, "source_type": "git", "git_repository": cfg.repo, "git_branch": cfg.branch, "port": 80, "watch_paths": cfg.watch})
		ids[name] = a["id"].(string)
		// Attached to the connection in the store: the API would ask GitHub about the repository.
		app, err := e.srv.store.GetApp(context.Background(), ids[name])
		if err != nil {
			t.Fatal(err)
		}
		app.Git.ConnectionID = conn.ID
		if _, err := e.srv.store.UpdateApp(context.Background(), app.ID, store.AppInput{Name: app.Name, SourceType: app.SourceType, Git: app.Git, WatchPaths: app.WatchPaths, Port: app.Port,
			Environment: app.Environment, EnvMeta: app.EnvMeta, Mounts: app.Mounts, Processes: app.Processes}); err != nil {
			t.Fatal(err)
		}
	}
	body := `{"ref":"refs/heads/main","after":"c0ffee","repository":{"full_name":"org/mono"},"commits":[{"modified":["services/web/app.ts"]}]}`

	// Nothing happens for a delivery that is unsigned, signed with another secret, or of another app.
	for name, c := range map[string][2]string{"unsigned": {"42", ""}, "wrong secret": {"42", "guess"}, "no app id": {"", "hook-secret"}} {
		if code, _ := post(c[0], c[1], "push", body); code != 401 {
			t.Errorf("%s = %d, want 401", name, code)
		}
	}
	if code, _ := post("43", "hook-secret", "push", body); code != 404 {
		t.Errorf("another app = %d, want 404", code)
	}
	if code, m := post("42", "hook-secret", "ping", `{"zen":"hi"}`); code != 200 || m["status"] != "ok" {
		t.Errorf("ping = %d %v", code, m)
	}
	if code, m := post("42", "hook-secret", "installation", `{}`); code != 200 || m["status"] != "ignored" {
		t.Errorf("installation event = %d %v", code, m)
	}

	// Deploy on push is on for a new app, and can be turned off.
	for _, id := range ids {
		if a := e.must(200, "PUT", "/api/apps/"+id+"/push-deploy", map[string]any{"on": false}); a["push_deploy"] != false {
			t.Fatalf("push_deploy = %v", a["push_deploy"])
		}
	}
	code, got := post("42", "hook-secret", "push", body)
	if code != 202 || len(got["deployed"].([]any)) != 0 || got["ignored"].(map[string]any)["web"] != "deploy on push is off" {
		t.Fatalf("push with deploy on push off = %d %v", code, got)
	}
	for _, name := range []string{"api", "web", "next", "other"} {
		e.must(200, "PUT", "/api/apps/"+ids[name]+"/push-deploy", map[string]any{"on": true})
	}
	code, got = post("42", "hook-secret", "push", body)
	e.deployer.Wait()
	deployed, ignored := got["deployed"].([]any), got["ignored"].(map[string]any)
	if code != 202 || len(deployed) != 1 || deployed[0] != "web" || len(ignored) != 1 || !strings.Contains(ignored["api"].(string), "watch paths") {
		t.Fatalf("push = %d %v", code, got)
	}
	if deps := e.list("/api/apps/" + ids["web"] + "/deployments"); len(deps) != 1 || deps[0]["trigger"] != "push" {
		t.Errorf("web deployments = %v", deps)
	}
	for _, name := range []string{"api", "next", "other"} {
		if deps := e.list("/api/apps/" + ids[name] + "/deployments"); len(deps) != 0 {
			t.Errorf("%s deployed on a push that is not its own: %v", name, deps)
		}
	}
	// GitHub redelivers, or the repository also has the app's deploy webhook: once is enough.
	if _, got = post("42", "hook-secret", "push", body); len(got["deployed"].([]any)) != 0 || !strings.Contains(got["ignored"].(map[string]any)["web"].(string), "already") {
		t.Errorf("the same commit again = %v", got)
	}
	token := e.must(200, "POST", "/api/apps/"+ids["web"]+"/deploy-hook", nil)["token"].(string)
	if _, m := e.hook(token, map[string]string{"X-GitHub-Event": "push"}, body); m["status"] != "ignored" {
		t.Errorf("the repository webhook for the same commit = %v", m)
	}
}

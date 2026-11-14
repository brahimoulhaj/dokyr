package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"panel/internal/registry"
	"panel/internal/store"
)

// fakeRegistry stands in for the built-in registry; it trusts the store's tokens.
type fakeRegistry struct {
	st      *store.Store
	applies int
	tags    map[string][]string
}

func (f *fakeRegistry) Apply(context.Context) error  { f.applies++; return nil }
func (f *fakeRegistry) State(context.Context) string { return "running" }
func (f *fakeRegistry) SystemUser() string           { return "berth" }
func (f *fakeRegistry) CheckEvents(auth string) bool { return auth == "Bearer events" }
func (f *fakeRegistry) Repositories(context.Context) ([]string, error) {
	var out []string
	for r := range f.tags {
		out = append(out, r)
	}
	return out, nil
}
func (f *fakeRegistry) GarbageCollect(context.Context) (string, error) { return "done", nil }
func (f *fakeRegistry) Tags(_ context.Context, repo string) ([]string, error) {
	return f.tags[repo], nil
}
func (f *fakeRegistry) DeleteTag(_ context.Context, repo, tag string) error {
	var keep []string
	for _, t := range f.tags[repo] {
		if t != tag {
			keep = append(keep, t)
		}
	}
	f.tags[repo] = keep
	return nil
}
func (f *fakeRegistry) Token(ctx context.Context, user, password, service string, scopes []string) (string, error) {
	if user != "" {
		if _, err := f.st.CheckRegistryToken(ctx, user, password); err != nil {
			return "", registry.ErrDenied
		}
	}
	return "jwt-for-" + user, nil
}

func TestRegistryAPI(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	reg := &fakeRegistry{st: e.srv.store, tags: map[string][]string{"shop/api": {"1.0", "latest"}}}
	e.srv.SetRegistry(reg)
	e.signIn()

	// Off by default.
	got := e.must(200, "GET", "/api/registry/settings", nil)
	if got["enabled"] != false || got["domain"] != "" {
		t.Fatalf("defaults = %v", got)
	}
	// The domain must be a hostname of its own.
	e.must(422, "PUT", "/api/registry/settings", map[string]any{"enabled": true, "domain": "not a host"})
	e.must(200, "PUT", "/api/settings/panel", map[string]any{"domain": "panel.example.com"})
	e.must(409, "PUT", "/api/registry/settings", map[string]any{"enabled": true, "domain": "panel.example.com"})
	e.must(422, "PUT", "/api/registry/settings", map[string]any{"enabled": true, "domain": "registry.example.com", "s3_connection_id": "nope"})
	got = e.must(200, "PUT", "/api/registry/settings", map[string]any{"enabled": true, "domain": "Registry.Example.com", "build_cache": true})
	if got["domain"] != "registry.example.com" || got["state"] != "running" || reg.applies != 1 {
		t.Fatalf("saved = %v (applies %d)", got, reg.applies)
	}
	// Its domain is then off limits for apps and the panel.
	pid := e.must(201, "POST", "/api/projects", map[string]any{"name": "Shop"})["id"].(string)
	app := e.must(201, "POST", "/api/projects/"+pid+"/apps", map[string]any{"name": "api", "image": "registry.example.com/shop/api:1.0", "port": 80})
	e.must(409, "POST", "/api/apps/"+app["id"].(string)+"/domains", map[string]any{"hostname": "registry.example.com"})
	e.must(409, "PUT", "/api/settings/panel", map[string]any{"domain": "registry.example.com"})

	// Tokens: shown once, then only their name.
	e.must(422, "POST", "/api/registry/tokens", map[string]any{"name": "berth", "scope": "push"})
	e.must(422, "POST", "/api/registry/tokens", map[string]any{"name": "ci", "scope": "admin"})
	tok := e.must(201, "POST", "/api/registry/tokens", map[string]any{"name": "CI", "scope": "push"})
	secret, _ := tok["token"].(string)
	if tok["name"] != "ci" || !strings.HasPrefix(secret, "rgt_") {
		t.Fatalf("token = %v", tok)
	}
	e.must(409, "POST", "/api/registry/tokens", map[string]any{"name": "ci", "scope": "pull"})
	if list := e.list("/api/registry/tokens"); len(list) != 1 || list[0]["token"] != nil {
		t.Fatalf("tokens = %v", list)
	}

	// docker login: Basic auth on the token endpoint, no panel session.
	login := func(user, pass string) int {
		req, _ := http.NewRequest("GET", e.url+"/api/registry/token?service=registry.example.com&scope=repository:shop/api:push,pull", nil)
		req.SetBasicAuth(user, pass)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := login("ci", secret); code != 200 {
		t.Errorf("login = %d", code)
	}
	if code := login("ci", "wrong"); code != 401 {
		t.Errorf("wrong secret = %d", code)
	}

	// Repositories and tags; delete a tag.
	repos := e.list("/api/registry/repositories")
	if len(repos) != 1 || repos[0]["name"] != "shop/api" {
		t.Fatalf("repositories = %v", repos)
	}
	e.must(422, "DELETE", "/api/registry/repositories/shop/api?tag=../x", nil)
	e.must(204, "DELETE", "/api/registry/repositories/shop/api?tag=latest", nil)
	if tags := reg.tags["shop/api"]; len(tags) != 1 || tags[0] != "1.0" {
		t.Errorf("tags after delete = %v", tags)
	}

	// A push report deploys the app on that tag; a wrong secret is refused.
	push := `{"events":[{"action":"push","target":{"repository":"shop/api","tag":"1.0","digest":"sha256:ab"}}]}`
	report := func(auth string) int {
		req, _ := http.NewRequest("POST", e.url+"/api/registry/events", strings.NewReader(push))
		req.Header.Set("Authorization", auth)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := report("Bearer nope"); code != 401 {
		t.Errorf("wrong events secret = %d", code)
	}
	if app["push_deploy"] != true {
		t.Fatalf("push_deploy default = %v", app["push_deploy"])
	}
	if code := report("Bearer events"); code != 200 {
		t.Fatalf("events = %d", code)
	}
	appID := app["id"].(string)
	deadline := time.Now().Add(5 * time.Second)
	for {
		deps, _ := e.srv.store.ListDeployments(ctx, appID, 10)
		if len(deps) == 1 && deps[0].Trigger == "registry" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no registry deployment: %+v", deps)
		}
		time.Sleep(20 * time.Millisecond)
	}
	e.deployer.Wait()

	// Turned off for the app: a push no longer deploys.
	if got := e.must(200, "PUT", "/api/apps/"+appID+"/push-deploy", map[string]any{"on": false}); got["push_deploy"] != false {
		t.Fatalf("push deploy off = %v", got)
	}
	report("Bearer events")
	e.deployer.Wait()
	if deps, _ := e.srv.store.ListDeployments(ctx, appID, 10); len(deps) != 1 {
		t.Errorf("deployments after a push with push deploy off = %d", len(deps))
	}
}

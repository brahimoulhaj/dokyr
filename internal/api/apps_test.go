package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// raw sends a request and returns the undecoded body (list and text endpoints).
func (e *env) raw(method, path string, body any) (int, []byte) {
	e.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.url+path, r)
	res, err := e.client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	return res.StatusCode, out
}

func (e *env) list(path string) []map[string]any {
	e.t.Helper()
	code, body := e.raw("GET", path, nil)
	var out []map[string]any
	if err := json.Unmarshal(body, &out); code != 200 || err != nil {
		e.t.Fatalf("GET %s = %d %s (%v)", path, code, body, err)
	}
	return out
}

// must sends a request and fails the test unless the status matches.
func (e *env) must(want int, method, path string, body any) map[string]any {
	e.t.Helper()
	code, m := e.call(method, path, body)
	if code != want {
		e.t.Fatalf("%s %s = %d %v, want %d", method, path, code, m, want)
	}
	return m
}

func TestAppsRequireAuth(t *testing.T) {
	e := newEnv(t)
	for _, c := range [][2]string{
		{"GET", "/api/projects"}, {"POST", "/api/projects"}, {"GET", "/api/projects/x"}, {"DELETE", "/api/projects/x"},
		{"GET", "/api/apps/x"}, {"POST", "/api/apps/x/deploy"}, {"POST", "/api/apps/x/stop"}, {"DELETE", "/api/apps/x"},
		{"POST", "/api/projects/x/domains"}, {"DELETE", "/api/projects/x/domains/y"}, {"GET", "/api/domains"}, {"POST", "/api/domains"}, {"POST", "/api/domains/x/routes"}, {"DELETE", "/api/domains/x"}, {"GET", "/api/deployments/x/log"}, {"GET", "/api/events"},
	} {
		if code, _ := e.call(c[0], c[1], map[string]any{}); code != 401 {
			t.Errorf("%s %s signed out = %d, want 401", c[0], c[1], code)
		}
	}
}

func TestProjectsAPI(t *testing.T) {
	e := newEnv(t)
	e.signIn()

	if got := e.list("/api/projects"); len(got) != 0 {
		t.Fatalf("initial projects = %v", got)
	}
	for _, bad := range []map[string]any{{"name": ""}, {"name": "   "}, {"name": strings.Repeat("x", 65)}, {"name": "ok", "description": strings.Repeat("d", 501)}} {
		e.must(422, "POST", "/api/projects", bad)
	}
	p := e.must(201, "POST", "/api/projects", map[string]any{"name": " Shop ", "description": "the shop"})
	id := p["id"].(string)
	if p["name"] != "Shop" || p["app_count"] != float64(0) {
		t.Fatalf("created = %v", p)
	}
	e.must(409, "POST", "/api/projects", map[string]any{"name": "shop"})
	e.must(404, "GET", "/api/projects/missing", nil)

	// PATCH changes only what is sent.
	p = e.must(200, "PATCH", "/api/projects/"+id, map[string]any{"name": "Store"})
	if p["name"] != "Store" || p["description"] != "the shop" {
		t.Fatalf("patched = %v", p)
	}
	e.must(422, "PATCH", "/api/projects/"+id, map[string]any{"name": ""})
	e.must(404, "PATCH", "/api/projects/missing", map[string]any{"name": "x"})

	app := e.must(201, "POST", "/api/projects/"+id+"/apps", map[string]any{"name": "web", "image": "nginx:1", "port": 80})
	if c := e.must(409, "DELETE", "/api/projects/"+id, nil); !strings.Contains(c["error"].(map[string]any)["message"].(string), "apps and database instances first") {
		t.Errorf("delete with apps = %v", c)
	}
	if code, _ := e.raw("DELETE", "/api/apps/"+app["id"].(string), nil); code != 204 {
		t.Fatalf("delete app = %d", code)
	}
	if code, _ := e.raw("DELETE", "/api/projects/"+id, nil); code != 204 {
		t.Fatalf("delete project = %d", code)
	}
	e.must(404, "GET", "/api/projects/"+id, nil)

	var actions []string
	for _, ev := range e.list("/api/events") {
		actions = append(actions, ev["action"].(string))
		if ev["actor"] != "a@b.co" {
			t.Errorf("event actor = %v", ev["actor"])
		}
	}
	if got := strings.Join(actions, ","); got != "project.delete,app.delete,app.create,project.update,project.create" {
		t.Errorf("events = %s", got)
	}
	e.must(422, "GET", "/api/events?limit=0", nil)
	if got := e.list("/api/events?limit=2"); len(got) != 2 {
		t.Errorf("limit=2 returned %d", len(got))
	}
}

func TestAppsAPI(t *testing.T) {
	e := newEnv(t)
	e.signIn()
	pid := e.must(201, "POST", "/api/projects", map[string]any{"name": "Shop"})["id"].(string)
	apps := "/api/projects/" + pid + "/apps"

	for name, bad := range map[string]map[string]any{
		"uppercase name":   {"name": "Web", "image": "nginx:1", "port": 80},
		"leading dash":     {"name": "-web", "image": "nginx:1", "port": 80},
		"empty image":      {"name": "web", "image": "", "port": 80},
		"image with flag":  {"name": "web", "image": "--privileged", "port": 80},
		"image with space": {"name": "web", "image": "nginx 1", "port": 80},
		"port zero":        {"name": "web", "image": "nginx:1", "port": 0},
		"port too high":    {"name": "web", "image": "nginx:1", "port": 70000},
		"negative cpu":     {"name": "web", "image": "nginx:1", "port": 80, "cpu_limit": -1},
		"tiny memory":      {"name": "web", "image": "nginx:1", "port": 80, "memory_limit_mb": 2},
	} {
		if code, m := e.call("POST", apps, bad); code != 422 {
			t.Errorf("%s = %d %v, want 422", name, code, m)
		}
	}
	e.must(404, "POST", "/api/projects/missing/apps", map[string]any{"name": "web", "image": "nginx:1", "port": 80})

	a := e.must(201, "POST", apps, map[string]any{"name": "web", "image": "ghcr.io/org/app:1.2", "port": 3000, "cpu_limit": 0.5, "memory_limit_mb": 256})
	id := a["id"].(string)
	if a["source_type"] != "image" || a["desired_state"] != "stopped" || a["current_deployment_id"] != "" || a["memory_limit_mb"] != float64(256) {
		t.Fatalf("created = %v", a)
	}
	e.must(409, "POST", apps, map[string]any{"name": "web", "image": "nginx:1", "port": 80})
	if got := e.list(apps); len(got) != 1 || got[0]["id"] != id {
		t.Fatalf("list = %v", got)
	}
	if got := e.must(200, "GET", "/api/apps/"+id, nil); got["runtime_state"] != "none" {
		t.Errorf("runtime_state before deploy = %v", got["runtime_state"])
	}

	a = e.must(200, "PATCH", "/api/apps/"+id, map[string]any{"image": "ghcr.io/org/app:1.3"})
	if a["image"] != "ghcr.io/org/app:1.3" || a["port"] != float64(3000) || a["cpu_limit"] != 0.5 {
		t.Fatalf("patched = %v", a)
	}
	e.must(422, "PATCH", "/api/apps/"+id, map[string]any{"port": -1})
	e.must(404, "PATCH", "/api/apps/missing", map[string]any{"port": 80})

	// Runtime actions before any deploy.
	for _, action := range []string{"start", "stop", "restart"} {
		if m := e.must(409, "POST", "/api/apps/"+id+"/"+action, nil); m["error"].(map[string]any)["code"] != "not_deployed" {
			t.Errorf("%s before deploy = %v", action, m)
		}
	}
	e.must(404, "POST", "/api/apps/missing/deploy", nil)

	// Deploy: 202 with a queued record, then it succeeds in the background.
	dep := e.must(202, "POST", "/api/apps/"+id+"/deploy", nil)
	depID := dep["id"].(string)
	if dep["status"] != "queued" || dep["image"] != "ghcr.io/org/app:1.3" || dep["started_at"] != nil {
		t.Fatalf("deploy response = %v", dep)
	}
	e.deployer.Wait()
	dep = e.must(200, "GET", "/api/deployments/"+depID, nil)
	if dep["status"] != "succeeded" || dep["container_name"] == "" || dep["finished_at"] == nil || dep["port"] != float64(3000) {
		t.Fatalf("finished deployment = %v", dep)
	}
	// A finished deployment cannot be cancelled; an unknown one is not found.
	if code, m := e.call("POST", "/api/deployments/"+depID+"/cancel", nil); code != 409 || m["error"].(map[string]any)["code"] != "not_cancellable" {
		t.Errorf("cancel finished = %d %v", code, m)
	}
	e.must(404, "POST", "/api/deployments/missing/cancel", nil)
	if got := e.list("/api/apps/" + id + "/deployments"); len(got) != 1 || got[0]["id"] != depID {
		t.Fatalf("deployments = %v", got)
	}
	code, logText := e.raw("GET", "/api/deployments/"+depID+"/log", nil)
	if code != 200 || !strings.Contains(string(logText), "Deployment succeeded") {
		t.Fatalf("log = %d %q", code, logText)
	}
	e.must(404, "GET", "/api/deployments/missing/log", nil)
	a = e.must(200, "GET", "/api/apps/"+id, nil)
	if a["runtime_state"] != "running" || a["desired_state"] != "running" || a["current_deployment_id"] != depID {
		t.Fatalf("app after deploy = %v", a)
	}

	// Stop / start / restart.
	if a = e.must(200, "POST", "/api/apps/"+id+"/stop", nil); a["runtime_state"] != "exited" || a["desired_state"] != "stopped" {
		t.Errorf("after stop = %v", a)
	}
	if a = e.must(200, "POST", "/api/apps/"+id+"/start", nil); a["runtime_state"] != "running" || a["desired_state"] != "running" {
		t.Errorf("after start = %v", a)
	}
	if a = e.must(200, "POST", "/api/apps/"+id+"/restart", nil); a["runtime_state"] != "running" {
		t.Errorf("after restart = %v", a)
	}

	// A failing deploy reports why and leaves the app running.
	e.must(200, "PATCH", "/api/apps/"+id, map[string]any{"image": "registry.example/missing:1"})
	failed := e.must(202, "POST", "/api/apps/"+id+"/deploy", nil)["id"].(string)
	e.deployer.Wait()
	if dep = e.must(200, "GET", "/api/deployments/"+failed, nil); dep["status"] != "failed" || !strings.Contains(dep["error"].(string), "manifest unknown") {
		t.Fatalf("failed deployment = %v", dep)
	}
	if a = e.must(200, "GET", "/api/apps/"+id, nil); a["runtime_state"] != "running" || a["current_deployment_id"] != depID {
		t.Errorf("app after failed deploy = %v", a)
	}

	// Delete removes the container too.
	if code, _ := e.raw("DELETE", "/api/apps/"+id, nil); code != 204 {
		t.Fatalf("delete = %d", code)
	}
	if cs, _ := e.ex.ListContainers(context.Background()); len(cs) != 0 {
		t.Errorf("containers after delete = %+v", cs)
	}
	e.must(404, "GET", "/api/apps/"+id, nil)
	e.must(404, "GET", "/api/deployments/"+depID, nil)
}

func TestDeployBusy(t *testing.T) {
	e := newEnv(t)
	e.signIn()
	pid := e.must(201, "POST", "/api/projects", map[string]any{"name": "Shop"})["id"].(string)
	id := e.must(201, "POST", "/api/projects/"+pid+"/apps", map[string]any{"name": "web", "image": "nginx:1", "port": 80})["id"].(string)
	e.ex.PullDelay = 200_000_000 // 200 ms
	depID := e.must(202, "POST", "/api/apps/"+id+"/deploy", nil)["id"].(string)
	if got := e.list("/api/deployments/active"); len(got) != 1 || got[0]["id"] != depID || got[0]["app_name"] != "web" || got[0]["project_id"] != pid {
		t.Fatalf("active deployments = %v", got)
	}
	if m := e.must(409, "POST", "/api/apps/"+id+"/deploy", nil); m["error"].(map[string]any)["code"] != "busy" {
		t.Errorf("second deploy = %v", m)
	}
	e.must(409, "DELETE", "/api/apps/"+id, nil)
	e.deployer.Wait()
	if got := e.list("/api/deployments/active"); len(got) != 0 {
		t.Fatalf("active deployments after finish = %v", got)
	}
}

func TestDomainsAPI(t *testing.T) {
	e := newEnv(t)
	px := &fakeProxy{}
	e.srv.SetProxy(px)
	e.signIn()
	pid := e.must(201, "POST", "/api/projects", map[string]any{"name": "Shop"})["id"].(string)
	web := e.must(201, "POST", "/api/projects/"+pid+"/apps", map[string]any{"name": "web", "image": "nginx:1", "port": 80})["id"].(string)
	api := e.must(201, "POST", "/api/projects/"+pid+"/apps", map[string]any{"name": "api", "image": "nginx:1", "port": 80})["id"].(string)
	pid2 := e.must(201, "POST", "/api/projects", map[string]any{"name": "Blog"})["id"].(string)
	stranger := e.must(201, "POST", "/api/projects/"+pid2+"/apps", map[string]any{"name": "blog", "image": "nginx:1", "port": 80})["id"].(string)
	domains := "/api/projects/" + pid + "/domains"

	for _, bad := range []string{"", "nodots", "https://shop.example.com", "shop.example.com/path", "*.example.com", "10.0.0.1", "shop.example.com:8443"} {
		e.must(422, "POST", domains, map[string]any{"hostname": bad})
	}
	e.must(404, "POST", "/api/projects/missing/domains", map[string]any{"hostname": "shop.example.com"})
	if px.applied != 0 {
		t.Fatalf("rejected input reached the proxy %d times", px.applied)
	}

	d := e.must(201, "POST", domains, map[string]any{"hostname": " Shop.Example.com "})
	if d["hostname"] != "shop.example.com" || d["tls_mode"] != "auto" || d["project_id"] != pid || px.applied != 1 {
		t.Fatalf("created = %v, applied %d", d, px.applied)
	}
	e.must(409, "POST", domains, map[string]any{"hostname": "shop.example.com"})
	e.must(409, "POST", "/api/projects/"+pid2+"/domains", map[string]any{"hostname": "shop.example.com"})
	did := d["id"].(string)

	// Routes: a path of the domain to an app of the same project.
	routes := "/api/domains/" + did + "/routes"
	e.must(422, "POST", routes, map[string]any{"path": "/a b", "app_id": web})
	e.must(422, "POST", routes, map[string]any{"path": "/", "app_id": stranger})
	e.must(422, "POST", routes, map[string]any{"path": "/", "app_id": "missing"})
	front := e.must(201, "POST", routes, map[string]any{"path": "", "app_id": web, "strip_prefix": true})
	if front["path"] != "/" || front["strip_prefix"] != false || front["url"] != "https://shop.example.com" || front["app_name"] != "web" {
		t.Fatalf("/ route = %v", front)
	}
	back := e.must(201, "POST", routes, map[string]any{"path": "api/", "app_id": api})
	if back["path"] != "/api" || back["strip_prefix"] != false || back["url"] != "https://shop.example.com/api" {
		t.Fatalf("/api route = %v", back)
	}
	e.must(409, "POST", routes, map[string]any{"path": "/api", "app_id": web})
	if m := e.must(200, "PATCH", "/api/routes/"+back["id"].(string), map[string]any{"path": "/api", "app_id": api, "strip_prefix": true}); m["strip_prefix"] != true {
		t.Fatalf("strip = %v", m)
	}
	e.must(409, "PATCH", "/api/routes/"+back["id"].(string), map[string]any{"path": "/", "app_id": api})
	list := e.list(domains)
	if len(list) != 1 || len(list[0]["routes"].([]any)) != 2 {
		t.Fatalf("domains = %v", list)
	}
	if got := e.list("/api/apps/" + api + "/routes"); len(got) != 1 || got[0]["url"] != "https://shop.example.com/api" {
		t.Fatalf("app routes = %v", got)
	}

	// TLS mode: HTTP only for a CDN that terminates TLS; URLs follow.
	e.must(422, "PATCH", "/api/domains/"+did, map[string]any{"tls_mode": "nope"})
	before := px.applied
	if m := e.must(200, "PATCH", "/api/domains/"+did, map[string]any{"tls_mode": "http_only"}); m["tls_mode"] != "http_only" || px.applied != before+1 {
		t.Fatalf("http only = %v", m)
	}
	e.must(200, "PATCH", "/api/domains/"+did, map[string]any{"tls_mode": "http_only"}) // no change, no reload
	if px.applied != before+1 {
		t.Fatal("unchanged mode reloaded the proxy")
	}
	if got := e.list("/api/apps/" + web + "/routes"); got[0]["url"] != "http://shop.example.com" {
		t.Errorf("http-only url = %v", got[0]["url"])
	}

	// The panel's domain and project domains exclude each other, both ways.
	e.must(409, "PUT", "/api/settings/panel", map[string]any{"domain": "shop.example.com"})
	e.must(200, "PUT", "/api/settings/panel", map[string]any{"domain": "panel.example.com"})
	e.must(409, "POST", domains, map[string]any{"hostname": "panel.example.com"})

	// DNS hint.
	dns := "/api/domains/" + did + "/dns"
	e.srv.lookupHost = func(_ context.Context, host string) ([]string, error) { return []string{"203.0.113.5"}, nil }
	e.srv.serverAddrs = func() []string { return []string{"203.0.113.5"} }
	if m := e.must(200, "GET", dns, nil); m["resolves"] != true || m["points_here"] != true {
		t.Errorf("dns match = %v", m)
	}
	e.must(404, "GET", "/api/domains/missing/dns", nil)

	// Proxy down: saved, and the caller is told it is not live.
	px.err = errors.New("down")
	e.must(503, "POST", domains, map[string]any{"hostname": "www.shop.example.com"})
	if got := e.list(domains); len(got) != 2 {
		t.Fatalf("domain not saved while the proxy was down: %v", got)
	}
	px.err = nil

	// Deleting an app removes its routes, not the domain; deleting the domain frees the hostname.
	if code, _ := e.raw("DELETE", "/api/apps/"+api, nil); code != 204 {
		t.Fatal("delete app")
	}
	if got := e.list(domains); len(got[0]["routes"].([]any))+len(got[1]["routes"].([]any)) != 1 {
		t.Errorf("routes after deleting an app = %v", got)
	}
	e.must(204, "DELETE", "/api/routes/"+front["id"].(string), nil)
	e.must(404, "DELETE", "/api/routes/"+front["id"].(string), nil)
	e.must(204, "DELETE", "/api/domains/"+did, nil)
	e.must(404, "DELETE", "/api/domains/"+did, nil)
	e.must(201, "POST", "/api/projects/"+pid2+"/domains", map[string]any{"hostname": "shop.example.com"})
}

func TestTopLevelDomainsAPI(t *testing.T) {
	e := newEnv(t)
	px := &fakeProxy{}
	e.srv.SetProxy(px)
	e.signIn()
	pid := e.must(201, "POST", "/api/projects", map[string]any{"name": "Shop"})["id"].(string)
	web := e.must(201, "POST", "/api/projects/"+pid+"/apps", map[string]any{"name": "web", "image": "nginx:1", "port": 80})["id"].(string)
	pid2 := e.must(201, "POST", "/api/projects", map[string]any{"name": "Blog"})["id"].(string)

	// Added on its own: listed, used by no project, and already known to the proxy.
	e.must(422, "POST", "/api/domains", map[string]any{"hostname": "nodots"})
	e.must(422, "POST", "/api/domains", map[string]any{"hostname": "shop.example.com", "project_id": "missing"})
	d := e.must(201, "POST", "/api/domains", map[string]any{"hostname": " Shop.Example.com ", "tls_mode": "http_only"})
	if d["hostname"] != "shop.example.com" || d["project_id"] != "" || d["tls_mode"] != "http_only" || px.applied != 1 {
		t.Fatalf("created = %v, applied %d", d, px.applied)
	}
	did := d["id"].(string)
	e.must(409, "POST", "/api/domains", map[string]any{"hostname": "shop.example.com"})
	if got := e.list("/api/projects/" + pid + "/domains"); len(got) != 0 {
		t.Fatalf("project domains before attach = %v", got)
	}
	// No routes while no project uses it.
	e.must(422, "POST", "/api/domains/"+did+"/routes", map[string]any{"path": "/", "app_id": web})

	// A project picks it; a second project cannot.
	attach := "/api/projects/" + pid + "/domains"
	e.must(422, "POST", attach, map[string]any{})
	e.must(422, "POST", attach, map[string]any{"domain_id": "missing"})
	if m := e.must(201, "POST", attach, map[string]any{"domain_id": did}); m["project_id"] != pid || m["project_name"] != "Shop" {
		t.Fatalf("attached = %v", m)
	}
	e.must(409, "POST", "/api/projects/"+pid2+"/domains", map[string]any{"domain_id": did})
	e.must(201, "POST", "/api/domains/"+did+"/routes", map[string]any{"path": "/", "app_id": web})
	all := e.list("/api/domains")
	if len(all) != 1 || all[0]["project_name"] != "Shop" || len(all[0]["routes"].([]any)) != 1 {
		t.Fatalf("all domains = %v", all)
	}

	// Created straight into a project.
	if m := e.must(201, "POST", "/api/domains", map[string]any{"hostname": "blog.example.com", "project_id": pid2}); m["project_id"] != pid2 {
		t.Fatalf("created in project = %v", m)
	}
	if got := e.list("/api/projects/" + pid2 + "/domains"); len(got) != 1 || got[0]["hostname"] != "blog.example.com" {
		t.Fatalf("blog domains = %v", got)
	}

	// Detach: only from the project that uses it; routes go, the domain stays and is free.
	e.must(404, "DELETE", "/api/projects/"+pid2+"/domains/"+did, nil)
	e.must(404, "DELETE", "/api/projects/"+pid+"/domains/missing", nil)
	before := px.applied
	e.must(204, "DELETE", "/api/projects/"+pid+"/domains/"+did, nil)
	if px.applied != before+1 {
		t.Fatal("detach did not reload the proxy")
	}
	e.must(404, "DELETE", "/api/projects/"+pid+"/domains/"+did, nil)
	if got := e.list("/api/apps/" + web + "/routes"); len(got) != 0 {
		t.Fatalf("routes after detach = %v", got)
	}
	if all := e.list("/api/domains"); len(all) != 2 || all[1]["hostname"] != "shop.example.com" || all[1]["project_id"] != "" {
		t.Fatalf("after detach = %v", all)
	}
	e.must(201, "POST", "/api/projects/"+pid2+"/domains", map[string]any{"domain_id": did})

	// Deleting a domain a project uses removes it from the project too.
	e.must(204, "DELETE", "/api/domains/"+did, nil)
	if got := e.list("/api/projects/" + pid2 + "/domains"); len(got) != 1 {
		t.Fatalf("blog domains after delete = %v", got)
	}
}

func TestGitAppsAPI(t *testing.T) {
	e := newEnv(t)
	e.signIn()
	pid := e.must(201, "POST", "/api/projects", map[string]any{"name": "Shop"})["id"].(string)
	apps := "/api/projects/" + pid + "/apps"
	git := func(over map[string]any) map[string]any {
		m := map[string]any{"name": "api", "source_type": "git", "git_repository": "https://github.com/org/api.git", "port": 8080}
		for k, v := range over {
			m[k] = v
		}
		return m
	}

	for _, bad := range []map[string]any{
		git(map[string]any{"source_type": "svn"}),
		git(map[string]any{"git_repository": ""}),
		git(map[string]any{"git_repository": "http://github.com/org/api.git"}),
		git(map[string]any{"git_repository": "git@github.com:org/api.git"}),
		git(map[string]any{"git_repository": "file:///etc"}),
		git(map[string]any{"git_repository": "https://user:pw@github.com/org/api.git"}),
		git(map[string]any{"git_repository": "https://github.com/"}),
		git(map[string]any{"git_repository": "https://github.com/org/api --upload-pack=x"}),
		git(map[string]any{"git_branch": "-x"}),
		git(map[string]any{"git_branch": "a..b"}),
		git(map[string]any{"git_branch": "feature/"}),
		git(map[string]any{"builder": "kaniko"}),
		git(map[string]any{"builder": "static", "build_options": map[string]any{"output_dir": "../dist"}}),
		git(map[string]any{"builder": "static", "build_options": map[string]any{"build_command": "a\x00b"}}),
		git(map[string]any{"builder": "buildpacks", "build_options": map[string]any{"image": "not an image"}}),
		git(map[string]any{"build_context": "../x"}),
		git(map[string]any{"build_context": "/etc"}),
		git(map[string]any{"dockerfile_path": "../Dockerfile"}),
		git(map[string]any{"dockerfile_path": "."}),
		git(map[string]any{"start_command": strings.Repeat("x", 4097)}),
		git(map[string]any{"start_command": "a\x00b"}),
	} {
		if code, m := e.call("POST", apps, bad); code != 422 {
			t.Errorf("POST %v = %d %v, want 422", bad, code, m)
		}
	}

	// The new builders keep their options, tidied; saving without them leaves them alone.
	st := e.must(201, "POST", apps, git(map[string]any{"name": "site", "builder": "static",
		"build_options": map[string]any{"build_command": " npm run build ", "output_dir": "./dist/", "spa": true, "image": "node:22-alpine"}}))
	if o := st["build_options"].(map[string]any); st["builder"] != "static" || o["build_command"] != "npm run build" || o["output_dir"] != "dist" || o["spa"] != true || o["image"] != "node:22-alpine" {
		t.Errorf("static app = %v %v", st["builder"], o)
	}
	st = e.must(200, "PATCH", "/api/apps/"+st["id"].(string), map[string]any{"port": 8080})
	if o := st["build_options"].(map[string]any); o["output_dir"] != "dist" {
		t.Errorf("build options after an unrelated save = %v", o)
	}
	for _, b := range []string{"nixpacks", "buildpacks"} {
		e.must(201, "POST", apps, git(map[string]any{"name": "b-" + b, "builder": b}))
	}

	a := e.must(201, "POST", apps, git(map[string]any{"build_context": "./services/api/", "image": "ignored:1"}))
	want := map[string]any{
		"source_type": "git", "image": "", "git_repository": "https://github.com/org/api.git", "git_branch": "main",
		"builder": "auto", "build_context": "services/api", "dockerfile_path": "Dockerfile",
	}
	for k, v := range want {
		if a[k] != v {
			t.Errorf("created %s = %v, want %v", k, a[k], v)
		}
	}
	id := a["id"].(string)

	for _, b := range []string{"railpack", "dockerfile", "auto"} {
		if got := e.must(200, "PATCH", "/api/apps/"+id, map[string]any{"builder": b}); got["builder"] != b {
			t.Errorf("builder %s: %v", b, got["builder"])
		}
	}

	// A start command is trimmed, kept by other updates, and cleared with "".
	start := "java $JAVA_OPTS -jar $(ls -1 build/libs/*jar | grep -v plain)"
	if got := e.must(200, "PATCH", "/api/apps/"+id, map[string]any{"start_command": "  " + start + "\n"}); got["start_command"] != start {
		t.Errorf("start_command = %q", got["start_command"])
	}
	if got := e.must(200, "PATCH", "/api/apps/"+id, map[string]any{"port": 8081}); got["start_command"] != start {
		t.Errorf("start_command lost on update: %q", got["start_command"])
	}
	if got := e.must(200, "PATCH", "/api/apps/"+id, map[string]any{"start_command": ""}); got["start_command"] != "" {
		t.Errorf("start_command not cleared: %q", got["start_command"])
	}

	// Partial update keeps the rest; "." is the repository root.
	a = e.must(200, "PATCH", "/api/apps/"+id, map[string]any{"git_branch": "release/2.0", "build_context": "."})
	if a["git_branch"] != "release/2.0" || a["build_context"] != "" || a["git_repository"] != want["git_repository"] {
		t.Errorf("after update = %v", a)
	}

	// Switching to an image needs an image, and clears the Git settings.
	e.must(422, "PATCH", "/api/apps/"+id, map[string]any{"source_type": "image"})
	a = e.must(200, "PATCH", "/api/apps/"+id, map[string]any{"source_type": "image", "image": "nginx:1.27"})
	if a["source_type"] != "image" || a["git_repository"] != "" || a["builder"] != "" {
		t.Errorf("after switch to image = %v", a)
	}
}

func TestAppEnvironment(t *testing.T) {
	e := newEnv(t)
	e.signIn()
	pid := e.must(201, "POST", "/api/projects", map[string]any{"name": "Environment"})["id"].(string)
	values := map[string]string{"TOKEN": "secret=with#symbols", "EMPTY": "", "MULTILINE": "first\nsecond"}
	a := e.must(201, "POST", "/api/projects/"+pid+"/apps", map[string]any{"name": "env", "image": "nginx:1", "port": 80, "environment": values})
	path := "/api/apps/" + a["id"].(string)
	check := func(a map[string]any) {
		t.Helper()
		got := a["environment"].(map[string]any)
		for k, v := range values {
			if got[k] != v {
				t.Fatalf("environment %s did not round-trip", k)
			}
		}
	}
	check(e.must(200, "GET", path, nil))
	check(e.must(200, "PATCH", path, map[string]any{"port": 8080}))
	for _, bad := range []map[string]string{{"1BAD": "x"}, {"BAD-NAME": "x"}, {"VALID": "x\x00y"}} {
		e.must(422, "PATCH", path, map[string]any{"environment": bad})
	}
	check(e.must(200, "GET", path, nil))
	cleared := e.must(200, "PATCH", path, map[string]any{"environment": map[string]string{}})
	if len(cleared["environment"].(map[string]any)) != 0 {
		t.Fatal("variables were not cleared")
	}
	for _, ev := range e.list("/api/events") {
		if strings.Contains(ev["detail"].(string), "secret=with#symbols") {
			t.Fatal("secret in audit log")
		}
	}
}

func TestDeploymentPhasesAPI(t *testing.T) {
	e := newEnv(t)
	e.signIn()
	pid := e.must(201, "POST", "/api/projects", map[string]any{"name": "Shop"})["id"].(string)
	id := e.must(201, "POST", "/api/projects/"+pid+"/apps", map[string]any{"name": "web", "image": "nginx:1", "port": 80})["id"].(string)
	queued := e.must(202, "POST", "/api/apps/"+id+"/deploy", nil)
	if ps, ok := queued["phases"].([]any); !ok {
		t.Errorf("queued phases = %v, want an array", queued["phases"])
	} else if len(ps) > 0 && queued["status"] == "queued" {
		t.Errorf("queued deployment has phases %v", ps)
	}
	e.deployer.Wait()
	d := e.must(200, "GET", "/api/deployments/"+queued["id"].(string), nil)
	ps, _ := d["phases"].([]any)
	var names []string
	for _, p := range ps {
		m := p.(map[string]any)
		if m["at"] == "" || m["at"] == nil {
			t.Errorf("phase without a time: %v", m)
		}
		names = append(names, m["phase"].(string))
	}
	want := []string{"pulling", "starting", "health_checking", "switching", "draining", "succeeded"}
	if d["phase"] != "draining" || strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("phase %v, phases %v, want draining and %v", d["phase"], names, want)
	}
}

func TestListDeploymentsPaged(t *testing.T) {
	e := newEnv(t)
	e.signIn()
	pid := e.must(201, "POST", "/api/projects", map[string]any{"name": "Shop"})["id"].(string)
	id := e.must(201, "POST", "/api/projects/"+pid+"/apps", map[string]any{"name": "web", "image": "nginx:1", "port": 80})["id"].(string)
	var ids []string
	for range 5 {
		ids = append(ids, e.must(202, "POST", "/api/apps/"+id+"/deploy", nil)["id"].(string))
		e.deployer.Wait()
	}

	page := func(q string) ([]string, string) {
		t.Helper()
		res, err := e.client.Get(e.url + "/api/apps/" + id + "/deployments" + q)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var list []map[string]any
		if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, d := range list {
			out = append(out, d["id"].(string))
		}
		return out, res.Header.Get("X-Total-Count")
	}
	// Newest first: page 1 of 2 is the last two made.
	if got, total := page("?per_page=2"); total != "5" || !slices.Equal(got, []string{ids[4], ids[3]}) {
		t.Errorf("page 1 = %v (total %s)", got, total)
	}
	if got, _ := page("?per_page=2&page=3"); !slices.Equal(got, []string{ids[0]}) {
		t.Errorf("page 3 = %v", got)
	}
	if got, _ := page("?per_page=2&page=9"); len(got) != 0 {
		t.Errorf("past the end = %v", got)
	}
	if got, total := page(""); total != "5" || len(got) != 5 {
		t.Errorf("default = %v (total %s)", got, total)
	}
}

func TestProjectEnvironmentAPI(t *testing.T) {
	e := newEnv(t)
	e.signIn()
	pid := e.must(201, "POST", "/api/projects", map[string]any{"name": "Shop"})["id"].(string)
	got := e.must(200, "GET", "/api/projects/"+pid+"/environment", nil)
	if got["inherit"] != false || len(got["environment"].(map[string]any)) != 0 {
		t.Fatalf("defaults = %v", got)
	}
	e.must(422, "PUT", "/api/projects/"+pid+"/environment", map[string]any{"environment": map[string]string{"1BAD": "x"}})
	e.must(200, "PUT", "/api/projects/"+pid+"/environment", map[string]any{"environment": map[string]string{"REGION": "eu"}, "inherit": true})
	got = e.must(200, "GET", "/api/projects/"+pid+"/environment", nil)
	if got["inherit"] != true || got["environment"].(map[string]any)["REGION"] != "eu" {
		t.Fatalf("saved = %v", got)
	}
	e.must(404, "GET", "/api/projects/nope/environment", nil)
}

// A project routes subdomains of a domain it uses: named ones, and "*" for all the others.
func TestRouteSubdomainsAPI(t *testing.T) {
	e := newEnv(t)
	e.srv.SetProxy(&fakeProxy{})
	e.signIn()
	pid := e.must(201, "POST", "/api/projects", map[string]any{"name": "Shop"})["id"].(string)
	app := e.must(201, "POST", "/api/projects/"+pid+"/apps", map[string]any{"name": "web", "image": "nginx:1", "port": 80})["id"].(string)
	domains := "/api/projects/" + pid + "/domains"
	ask := func(name string) int {
		code, _ := e.raw("GET", "/api/proxy/ask?domain="+name, nil)
		return code
	}

	// The Domains page takes plain hostnames only.
	e.must(422, "POST", "/api/domains", map[string]any{"hostname": "*.example.com"})
	did := e.must(201, "POST", domains, map[string]any{"hostname": "example.com"})["id"].(string)
	other := e.must(201, "POST", domains, map[string]any{"hostname": "blog.example.com"})["id"].(string)
	routes := "/api/domains/" + did + "/routes"

	if got := ask("acme.example.com"); got != 403 {
		t.Errorf("ask before a * route = %d, want 403", got)
	}
	for _, bad := range []string{"a b", "*.a", "a.*", "**", "-a", "https://a"} {
		e.must(422, "POST", routes, map[string]any{"subdomain": bad, "path": "/", "app_id": app})
	}
	e.must(201, "POST", routes, map[string]any{"path": "/", "app_id": app})
	wild := e.must(201, "POST", routes, map[string]any{"subdomain": "*", "path": "/", "app_id": app})
	if wild["hostname"] != "*.example.com" || wild["subdomain"] != "*" || wild["url"] != "https://*.example.com" {
		t.Fatalf("* route = %v", wild)
	}
	admin := e.must(201, "POST", routes, map[string]any{"subdomain": " Admin. ", "path": "/", "app_id": app})
	if admin["hostname"] != "admin.example.com" || admin["subdomain"] != "admin" || admin["url"] != "https://admin.example.com" {
		t.Fatalf("admin route = %v", admin)
	}
	e.must(409, "POST", routes, map[string]any{"subdomain": "admin", "path": "/", "app_id": app})
	e.must(201, "POST", routes, map[string]any{"subdomain": "admin", "path": "/api", "app_id": app})

	// A subdomain that is a domain of its own is routed there, and the other way round.
	e.must(409, "POST", routes, map[string]any{"subdomain": "blog", "path": "/", "app_id": app})
	e.must(409, "POST", "/api/domains", map[string]any{"hostname": "admin.example.com"})
	e.must(409, "PUT", "/api/settings/panel", map[string]any{"domain": "admin.example.com"})
	// The same name through another domain: x.blog under example.com, x under blog.example.com.
	e.must(201, "POST", "/api/domains/"+other+"/routes", map[string]any{"subdomain": "x", "path": "/", "app_id": app})
	e.must(409, "POST", routes, map[string]any{"subdomain": "x.blog", "path": "/", "app_id": app})

	// Certificates on demand: one label under a domain with a * route, while it serves HTTPS.
	for name, want := range map[string]int{
		"acme.example.com": 200, "ACME.example.com": 200, "a.b.example.com": 403, "example.com": 403,
		"acme.blog.example.com": 403, "acme.example.org": 403, "": 403, "*.example.com": 403,
	} {
		if got := ask(name); got != want {
			t.Errorf("ask %q = %d, want %d", name, got, want)
		}
	}
	e.must(200, "PATCH", "/api/domains/"+did, map[string]any{"tls_mode": "http_only"})
	if got := ask("acme.example.com"); got != 403 {
		t.Errorf("ask on an http-only domain = %d, want 403", got)
	}
	e.must(200, "PATCH", "/api/domains/"+did, map[string]any{"tls_mode": "auto"})

	// Editing keeps its own subdomain and can move to another.
	moved := e.must(200, "PATCH", "/api/routes/"+wild["id"].(string), map[string]any{"subdomain": "app", "path": "/", "app_id": app})
	if moved["hostname"] != "app.example.com" {
		t.Fatalf("moved route = %v", moved)
	}
	e.must(200, "PATCH", "/api/routes/"+admin["id"].(string), map[string]any{"subdomain": "admin", "path": "/", "app_id": app})
	if got := ask("acme.example.com"); got != 403 {
		t.Errorf("ask after the * route moved = %d, want 403", got)
	}
}

// An app says its settings moved on since the running deployment, until the next deploy.
func TestNeedsRedeploy(t *testing.T) {
	e := newEnv(t)
	e.signIn()
	_, p := e.call("POST", "/api/projects", map[string]any{"name": "shop"})
	_, a := e.call("POST", "/api/projects/"+p["id"].(string)+"/apps", map[string]any{"name": "web", "image": "nginx:1.27", "port": 80})
	path := "/api/apps/" + a["id"].(string)
	needs := func() bool { _, got := e.call("GET", path, nil); return got["needs_redeploy"].(bool) }
	deploy := func() {
		t.Helper()
		if code, _ := e.call("POST", path+"/deploy", nil); code != 202 {
			t.Fatalf("deploy = %d", code)
		}
		e.deployer.Wait()
	}
	if needs() {
		t.Fatal("an app never deployed has nothing to redeploy")
	}
	deploy()
	if needs() {
		t.Fatal("just deployed: nothing changed")
	}
	e.call("PATCH", path, map[string]any{"name": "site"})
	if needs() {
		t.Fatal("a new name needs no deploy")
	}
	e.call("PATCH", path, map[string]any{"environment": map[string]string{"A": "1"}})
	if !needs() {
		t.Fatal("a new variable needs a deploy")
	}
	deploy()
	if needs() {
		t.Fatal("deployed again: up to date")
	}
	e.call("PATCH", path, map[string]any{"port": 8080})
	if !needs() {
		t.Fatal("a new port needs a deploy")
	}
	e.call("PATCH", path, map[string]any{"port": 80})
	if needs() {
		t.Fatal("back to the deployed settings: nothing to deploy")
	}

	// Values the variables take from elsewhere count too: a project variable the app
	// refers to, one it inherits, another app's port.
	pid := p["id"].(string)
	projectEnv := func(env map[string]string, inherit bool) {
		t.Helper()
		e.must(200, "PUT", "/api/projects/"+pid+"/environment", map[string]any{"environment": env, "inherit": inherit})
	}
	_, other := e.call("POST", "/api/projects/"+pid+"/apps", map[string]any{"name": "api", "image": "nginx:1.27", "port": 3000})
	projectEnv(map[string]string{"REGION": "eu", "UNUSED": "x"}, false)
	e.call("PATCH", path, map[string]any{"environment": map[string]string{"A": "1", "WHERE": "${project.REGION}", "API": "http://${app.api.HOST}:${app.api.PORT}"}})
	deploy()
	if needs() {
		t.Fatal("deployed with references: up to date")
	}
	projectEnv(map[string]string{"REGION": "eu", "UNUSED": "y"}, false)
	if needs() {
		t.Fatal("a project variable the app does not use needs no deploy")
	}
	projectEnv(map[string]string{"REGION": "us", "UNUSED": "y"}, false)
	if !needs() {
		t.Fatal("a referenced project variable changed: needs a deploy")
	}
	projectEnv(map[string]string{"REGION": "eu", "UNUSED": "y"}, false)
	if needs() {
		t.Fatal("the project variable is back: nothing to deploy")
	}
	projectEnv(map[string]string{"REGION": "eu", "UNUSED": "y"}, true)
	if !needs() {
		t.Fatal("the app now inherits the project's variables: needs a deploy")
	}
	deploy()
	e.call("PATCH", "/api/apps/"+other["id"].(string), map[string]any{"port": 4000})
	if !needs() {
		t.Fatal("the port of an app it refers to changed: needs a deploy")
	}
	deploy()
	// A reference that no longer resolves is not a change by itself; the settings still are.
	projectEnv(map[string]string{"UNUSED": "y"}, true)
	if needs() {
		t.Fatal("an unresolvable reference alone must not mark the app")
	}

	// A fingerprint from before the values were recorded keeps comparing the settings only.
	app, err := e.srv.store.GetApp(context.Background(), a["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	check, _, _ := e.srv.store.AppHealthCheck(context.Background(), app.ID)
	if err := e.srv.store.SetAppDeployedConfig(context.Background(), app.ID, app.ConfigHash(check)); err != nil {
		t.Fatal(err)
	}
	projectEnv(map[string]string{"REGION": "ap"}, true)
	if needs() {
		t.Fatal("an old fingerprint knows nothing of the values")
	}
	e.call("PATCH", path, map[string]any{"port": 81})
	if !needs() {
		t.Fatal("an old fingerprint still sees a changed setting")
	}
}

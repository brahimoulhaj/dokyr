package api

import (
	"context"
	"testing"
)

// TestRedirectsAPI: a free hostname can be made to redirect to another; a routed one, a
// redirected one, a wildcard or a chain cannot, and a redirected hostname takes no route.
func TestRedirectsAPI(t *testing.T) {
	e := newEnv(t)
	px := &fakeProxy{}
	e.srv.SetProxy(px)
	e.must(401, "GET", "/api/redirects", nil)
	e.signIn()
	pid := e.must(201, "POST", "/api/projects", map[string]any{"name": "Shop"})["id"].(string)
	web := e.must(201, "POST", "/api/projects/"+pid+"/apps", map[string]any{"name": "web", "image": "nginx:1", "port": 80})["id"].(string)
	e.must(201, "POST", "/api/routes", map[string]any{"hostname": "example.com", "path": "/", "app_id": web})
	const redirects = "/api/redirects"

	for _, bad := range []map[string]any{
		{"hostname": "", "target": "example.com"},
		{"hostname": "*.example.com", "target": "example.com"},
		{"hostname": "www.example.com:8080", "target": "example.com"},
		{"hostname": "www.example.com", "target": ""},
		{"hostname": "www.example.com", "target": "*.example.com"},
		{"hostname": "www.example.com", "target": "www.example.com"},
		{"hostname": "www.example.com", "target": "example.com", "tls_mode": "nope"},
	} {
		e.must(422, "POST", redirects, bad)
	}
	e.must(409, "POST", redirects, map[string]any{"hostname": "example.com", "target": "other.example.com"}) // routed

	before := px.applied
	www := e.must(201, "POST", redirects, map[string]any{"hostname": " https://WWW.Example.com/ ", "target": "example.com"})
	if www["hostname"] != "www.example.com" || www["target"] != "example.com" || www["tls_mode"] != "auto" || px.applied != before+1 {
		t.Fatalf("redirect = %v (applied %d)", www, px.applied-before)
	}
	e.must(409, "POST", redirects, map[string]any{"hostname": "www.example.com", "target": "example.com"})     // taken
	e.must(409, "POST", redirects, map[string]any{"hostname": "old.example.com", "target": "www.example.com"}) // a chain
	e.must(409, "POST", "/api/routes", map[string]any{"hostname": "www.example.com", "path": "/", "app_id": web})
	if list := e.list(redirects); len(list) != 1 || list[0]["id"] != www["id"] {
		t.Fatalf("redirects = %v", list)
	}

	sites, err := e.srv.store.ProxySites(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range sites {
		if s.Host == "www.example.com" {
			found = s.RedirectTo == "example.com" && len(s.Paths) == 0
		}
	}
	if !found {
		t.Fatalf("the proxy is not told of the redirect: %+v", sites)
	}

	e.must(204, "DELETE", redirects+"/"+www["id"].(string), nil)
	e.must(404, "DELETE", redirects+"/"+www["id"].(string), nil)
	e.must(201, "POST", "/api/routes", map[string]any{"hostname": "www.example.com", "path": "/", "app_id": web})
}

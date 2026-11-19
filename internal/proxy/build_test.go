package proxy

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func build(t *testing.T, c Config) map[string]any {
	t.Helper()
	raw, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// dig walks nested objects and arrays: dig(v, "apps", "http", 0, "x").
func dig(t *testing.T, v any, path ...any) any {
	t.Helper()
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, ok := v.(map[string]any)
			if !ok {
				t.Fatalf("dig %v: not an object at %q", path, k)
			}
			v = m[k]
		case int:
			a, ok := v.([]any)
			if !ok || k >= len(a) {
				t.Fatalf("dig %v: no index %d", path, k)
			}
			v = a[k]
		}
	}
	return v
}

func TestBuildFreshInstall(t *testing.T) {
	cfg := build(t, Config{AdminListen: "unix//run/caddy/admin.sock", PanelUpstream: "dokyr:8080"})

	if got := dig(t, cfg, "admin", "listen"); got != "unix//run/caddy/admin.sock" {
		t.Errorf("admin listen = %v", got)
	}
	servers := dig(t, cfg, "apps", "http", "servers").(map[string]any)
	if _, has := servers["https"]; has {
		t.Error("an https server exists without any hostname")
	}
	routes := dig(t, servers, "http", "routes").([]any)
	if len(routes) != 1 {
		t.Fatalf("http routes = %d, want only the panel catch-all", len(routes))
	}
	if _, has := routes[0].(map[string]any)["match"]; has {
		t.Error("the panel catch-all has a matcher")
	}
	if got := dig(t, routes[0], "handle", 0, "upstreams", 0, "dial"); got != "dokyr:8080" {
		t.Errorf("catch-all upstream = %v", got)
	}
	if _, has := dig(t, cfg, "apps").(map[string]any)["tls"]; has {
		t.Error("tls app configured with nothing to configure")
	}
}

func TestBuildRoutes(t *testing.T) {
	cfg := build(t, Config{
		AdminListen:   ":2019",
		PanelUpstream: "dokyr:8080",
		PanelDomain:   "panel.example.com",
		ACMEEmail:     "ops@example.com",
		Routes: []Route{
			{ID: "b", Hosts: []string{"b.example.com", "www.b.example.com"}, Upstreams: []string{"app-b:3000"}},
			{ID: "a", Hosts: []string{"a.test"}}, // stopped app: no upstreams
		},
	})

	https := dig(t, cfg, "apps", "http", "servers", "https", "routes").([]any)
	var ids []string
	for _, r := range https {
		ids = append(ids, r.(map[string]any)["@id"].(string))
	}
	if strings.Join(ids, ",") != "route-panel,route-a,route-b" {
		t.Errorf("https route order = %v", ids)
	}
	if got := dig(t, https[0], "handle", 0, "upstreams", 0, "dial"); got != "dokyr:8080" {
		t.Errorf("panel route upstream = %v", got)
	}
	if ups := dig(t, https[1], "handle", 0, "upstreams").([]any); len(ups) != 0 {
		t.Errorf("stopped app upstreams = %v, want an empty list (not null, so PATCH works)", ups)
	}
	if hosts := dig(t, https[2], "match", 0, "host").([]any); len(hosts) != 2 {
		t.Errorf("route b hosts = %v", hosts)
	}
	if got := dig(t, cfg, "apps", "http", "servers", "https", "automatic_https", "disable_redirects"); got != true {
		t.Error("automatic redirects not disabled")
	}

	// HTTP: one redirect per route, then the panel catch-all last.
	http := dig(t, cfg, "apps", "http", "servers", "http", "routes").([]any)
	if len(http) != 4 {
		t.Fatalf("http routes = %d, want 3 redirects + catch-all", len(http))
	}
	if got := dig(t, http[0], "handle", 0, "status_code"); got != float64(308) {
		t.Errorf("redirect status = %v", got)
	}
	if _, has := http[3].(map[string]any)["match"]; has {
		t.Error("catch-all is not last")
	}

	// TLS: internal names get the internal issuer, the rest ACME with the email.
	policies := dig(t, cfg, "apps", "tls", "automation", "policies").([]any)
	if len(policies) != 2 {
		t.Fatalf("tls policies = %v", policies)
	}
	if subj := dig(t, policies[0], "subjects").([]any); len(subj) != 1 || subj[0] != "a.test" {
		t.Errorf("internal subjects = %v", subj)
	}
	if got := dig(t, policies[0], "issuers", 0, "module"); got != "internal" {
		t.Errorf("internal issuer = %v", got)
	}
	if got := dig(t, policies[1], "issuers", 0, "email"); got != "ops@example.com" {
		t.Errorf("acme email = %v", got)
	}
}

func TestBuildRejects(t *testing.T) {
	base := Config{AdminListen: ":2019", PanelUpstream: "dokyr:8080"}
	for name, mutate := range map[string]func(*Config){
		"no admin":      func(c *Config) { c.AdminListen = "" },
		"bad upstream":  func(c *Config) { c.PanelUpstream = "dokyr" },
		"bad host":      func(c *Config) { c.Routes = []Route{{ID: "a", Hosts: []string{"Not A Host"}}} },
		"no hosts":      func(c *Config) { c.Routes = []Route{{ID: "a"}} },
		"empty id":      func(c *Config) { c.Routes = []Route{{Hosts: []string{"a.example.com"}}} },
		"id with slash": func(c *Config) { c.Routes = []Route{{ID: "a/b", Hosts: []string{"a.example.com"}}} },
		"duplicate id": func(c *Config) {
			c.Routes = []Route{{ID: "a", Hosts: []string{"a.example.com"}}, {ID: "a", Hosts: []string{"b.example.com"}}}
		},
		"duplicate host": func(c *Config) {
			c.Routes = []Route{{ID: "a", Hosts: []string{"a.example.com"}}, {ID: "b", Hosts: []string{"a.example.com"}}}
		},
		"host is panel's": func(c *Config) {
			c.PanelDomain = "a.example.com"
			c.Routes = []Route{{ID: "a", Hosts: []string{"a.example.com"}}}
		},
		"route upstream": func(c *Config) {
			c.Routes = []Route{{ID: "a", Hosts: []string{"a.example.com"}, Upstreams: []string{"nope"}}}
		},
		"panel id reused": func(c *Config) {
			c.PanelDomain = "p.example.com"
			c.Routes = []Route{{ID: "panel", Hosts: []string{"a.example.com"}}}
		},
	} {
		c := base
		mutate(&c)
		if _, err := Build(c); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestValidHost(t *testing.T) {
	for _, ok := range []string{"example.com", "a.b-c.example.com", "localhost", "app.test", "xn--bcher-kva.example"} {
		if err := ValidHost(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "example", "Example.com", "*.example.com", "a..com", "-a.com", "a-.com", "a.com:443", "10.0.0.1", "::1", "a b.com", "https://a.com", strings.Repeat("a", 64) + ".com"} {
		if err := ValidHost(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestBuildHTTPOnlyHosts(t *testing.T) {
	cfg := build(t, Config{
		AdminListen:   "localhost:2019",
		PanelUpstream: "dokyr:8080",
		ACMEEmail:     "ops@example.com",
		Routes: []Route{
			{ID: "app1", Hosts: []string{"secure.example.com", "flex.example.com"}, HTTPOnly: []string{"flex.example.com"}, Upstreams: []string{"app-1:80"}},
			{ID: "app2", Hosts: []string{"cf.example.com"}, HTTPOnly: []string{"cf.example.com"}, Upstreams: []string{"app-2:80"}},
		},
	})
	raw, _ := json.Marshal(cfg)
	s := string(raw)

	https := dig(t, cfg, "apps", "http", "servers", "https", "routes").([]any)
	if len(https) != 1 {
		t.Fatalf("https routes = %d, want only app1's TLS host", len(https))
	}
	if hosts := dig(t, https[0], "match", 0, "host").([]any); len(hosts) != 1 || hosts[0] != "secure.example.com" {
		t.Fatalf("https hosts = %v", hosts)
	}

	// HTTP server: app1 redirect, app1 plain proxy, app2 plain proxy, panel catch-all.
	httpRoutes := dig(t, cfg, "apps", "http", "servers", "http", "routes").([]any)
	if len(httpRoutes) != 4 {
		t.Fatalf("http routes = %d: %s", len(httpRoutes), s)
	}
	byID := map[string]any{}
	for _, r := range httpRoutes {
		if id, ok := r.(map[string]any)["@id"].(string); ok {
			byID[id] = r
		}
	}
	for id, host := range map[string]string{"route-app1-http": "flex.example.com", "route-app2-http": "cf.example.com"} {
		r, ok := byID[id]
		if !ok {
			t.Fatalf("missing %s: %s", id, s)
		}
		if h := dig(t, r, "match", 0, "host", 0); h != host {
			t.Fatalf("%s host = %v", id, h)
		}
		if dig(t, r, "handle", 0, "handler") != "reverse_proxy" {
			t.Fatalf("%s is not a proxy", id)
		}
	}
	// No redirect for the HTTP-only hosts, so a CDN talking HTTP to the origin cannot loop.
	if strings.Count(s, `"status_code":308`) != 1 {
		t.Fatalf("want exactly one redirect (secure.example.com): %s", s)
	}

	// Everything HTTP-only: no HTTPS server at all, so Caddy asks for no certificate.
	only := build(t, Config{AdminListen: "localhost:2019", PanelUpstream: "dokyr:8080",
		Routes: []Route{{ID: "a", Hosts: []string{"cf.example.com"}, HTTPOnly: []string{"cf.example.com"}}}})
	if _, has := dig(t, only, "apps", "http", "servers").(map[string]any)["https"]; has {
		t.Fatal("https server built for HTTP-only hosts")
	}

	if _, err := Build(Config{AdminListen: "x:1", PanelUpstream: "dokyr:8080",
		Routes: []Route{{ID: "a", Hosts: []string{"a.example.com"}, HTTPOnly: []string{"b.example.com"}}}}); err == nil {
		t.Fatal("http-only host outside the route's hosts accepted")
	}
}

func TestBuildPanelHTTPOnly(t *testing.T) {
	cfg := build(t, Config{AdminListen: "localhost:2019", PanelUpstream: "dokyr:8080", PanelDomain: "panel.example.com", PanelHTTPOnly: true})
	if _, has := dig(t, cfg, "apps", "http", "servers").(map[string]any)["https"]; has {
		t.Fatal("https server built for an HTTP-only panel domain")
	}
	r := dig(t, cfg, "apps", "http", "servers", "http", "routes", 0)
	if dig(t, r, "@id") != "route-panel-http" || dig(t, r, "match", 0, "host", 0) != "panel.example.com" {
		t.Fatalf("panel route = %v", r)
	}
}

func TestBuildPanelDomainOnly(t *testing.T) {
	last := func(c Config) any {
		routes := dig(t, build(t, c), "apps", "http", "servers", "http", "routes").([]any)
		return routes[len(routes)-1]
	}
	base := Config{AdminListen: "localhost:2019", PanelUpstream: "dokyr:8080", PanelDomain: "panel.example.com", PanelDomainOnly: true}
	if r := last(base); dig(t, r, "handle", 0, "abort") != true {
		t.Fatalf("catch-all = %v, want abort", r)
	}
	// Without a domain the flag is ignored: the panel must stay reachable.
	base.PanelDomain = ""
	if r := last(base); dig(t, r, "handle", 0, "handler") != "reverse_proxy" {
		t.Fatalf("catch-all without domain = %v", r)
	}
}

func TestBuildPanelPaths(t *testing.T) {
	cfg := build(t, Config{AdminListen: ":2019", PanelUpstream: "dokyr:8080", Routes: []Route{{
		ID: "registry", Hosts: []string{"registry.example.com"}, Upstreams: []string{"dokyr-registry:5000"},
		PanelPaths: []string{"/api/registry/token"},
	}}})
	route := dig(t, cfg, "apps", "http", "servers", "https", "routes", 0)
	sub := dig(t, route, "handle", 0)
	if dig(t, sub, "handler") != "subroute" {
		t.Fatalf("handle = %v, want a subroute", sub)
	}
	if got := dig(t, sub, "routes", 0, "match", 0, "path", 0); got != "/api/registry/token" {
		t.Errorf("panel path = %v", got)
	}
	if got := dig(t, sub, "routes", 0, "handle", 0, "upstreams", 0, "dial"); got != "dokyr:8080" {
		t.Errorf("token endpoint goes to %v, want the panel", got)
	}
	if got := dig(t, sub, "routes", 1, "handle", 0, "upstreams", 0, "dial"); got != "dokyr-registry:5000" {
		t.Errorf("the rest goes to %v, want the registry", got)
	}
}

// An HTTP-only registry (behind Cloudflare "Flexible") is served on the http server, token
// path included, with no redirect to HTTPS (which would loop).
func TestBuildPanelPathsHTTPOnly(t *testing.T) {
	cfg := build(t, Config{AdminListen: ":2019", PanelUpstream: "dokyr:8080", Routes: []Route{{
		ID: "registry", Hosts: []string{"registry.example.com"}, HTTPOnly: []string{"registry.example.com"},
		Upstreams: []string{"dokyr-registry:5000"}, PanelPaths: []string{"/api/registry/token"},
	}}})
	servers := dig(t, cfg, "apps", "http", "servers").(map[string]any)
	if _, ok := servers["https"]; ok {
		t.Errorf("an https server exists for an HTTP-only registry: %v", servers["https"])
	}
	sub := dig(t, servers, "http", "routes", 0, "handle", 0)
	if dig(t, sub, "handler") != "subroute" {
		t.Fatalf("handle = %v, want the registry subroute, not a redirect", sub)
	}
	if got := dig(t, sub, "routes", 0, "handle", 0, "upstreams", 0, "dial"); got != "dokyr:8080" {
		t.Errorf("token endpoint goes to %v, want the panel", got)
	}
}

func TestBuildSites(t *testing.T) {
	cfg := build(t, Config{AdminListen: ":2019", PanelUpstream: "dokyr:8080", Sites: []Site{{Host: "shop.example.com", Paths: []Path{
		{ID: "r1", AppID: "front", Prefix: "/", Upstreams: []string{"front:80"}},
		{ID: "r2", AppID: "back", Prefix: "/api", Strip: true, Upstreams: []string{"back:8080"}},
	}}}})
	sub := dig(t, cfg, "apps", "http", "servers", "https", "routes", 0, "handle", 0, "routes")
	if got := dig(t, sub, 0, "@id"); got != "path-r2" {
		t.Errorf("first path = %v, want the longest (/api)", got)
	}
	if got := dig(t, sub, 0, "match", 0, "path", 1); got != "/api/*" {
		t.Errorf("/api matcher = %v", got)
	}
	if got := dig(t, sub, 0, "handle", 0, "strip_path_prefix"); got != "/api" {
		t.Errorf("strip = %v", got)
	}
	if _, has := dig(t, sub, 1).(map[string]any)["match"]; has {
		t.Error("the / route has a matcher")
	}
	// A hostname both a project domain and a built-in route is refused.
	if _, err := Build(Config{AdminListen: ":2019", PanelUpstream: "dokyr:8080",
		Routes: []Route{{ID: "registry", Hosts: []string{"shop.example.com"}, Upstreams: []string{"r:5000"}}},
		Sites:  []Site{{Host: "shop.example.com"}}}); err == nil {
		t.Error("duplicate hostname accepted")
	}
}

func TestBuildWildcardSite(t *testing.T) {
	cfg := build(t, Config{AdminListen: ":2019", PanelUpstream: "dokyr:8080", ACMEEmail: "ops@example.com", Sites: []Site{
		{Host: "*.example.com", Paths: []Path{{ID: "r1", AppID: "a", Prefix: "/", Upstreams: []string{"a:80"}}}},
		{Host: "shop.example.com", Paths: []Path{{ID: "r2", AppID: "b", Prefix: "/", Upstreams: []string{"b:80"}}}},
		{Host: "*.apps.test"},
		{Host: "*.plain.example.org", HTTPOnly: true},
	}})
	// The exact hostname is matched ahead of the wildcard covering it.
	routes := dig(t, cfg, "apps", "http", "servers", "https", "routes")
	if got := dig(t, routes, 0, "match", 0, "host", 0); got != "shop.example.com" {
		t.Errorf("first https route = %v, want the exact hostname", got)
	}
	if got := dig(t, routes, 1, "match", 0, "host", 0); got != "*.example.com" {
		t.Errorf("second https route = %v", got)
	}
	automation := dig(t, cfg, "apps", "tls", "automation")
	if got := dig(t, automation, "on_demand", "permission", "endpoint"); got != "http://dokyr:8080/api/proxy/ask" {
		t.Errorf("ask endpoint = %v", got)
	}
	// Policies: the exact name first (managed up front), the wildcards on demand, the
	// catch-all last; the HTTP-only wildcard gets none.
	policies := automation.(map[string]any)["policies"].([]any)
	raw, _ := json.Marshal(policies)
	if len(policies) != 4 {
		t.Fatalf("policies = %s", raw)
	}
	if got := dig(t, policies, 0, "subjects", 0); got != "shop.example.com" || dig(t, policies, 0, "on_demand") != nil {
		t.Errorf("policy 0 = %s", raw)
	}
	if dig(t, policies, 1, "subjects", 0) != "*.apps.test" || dig(t, policies, 1, "on_demand") != true || dig(t, policies, 1, "issuers", 0, "module") != "internal" {
		t.Errorf("policy 1 = %s", raw)
	}
	if dig(t, policies, 2, "subjects", 0) != "*.example.com" || dig(t, policies, 2, "on_demand") != true || dig(t, policies, 2, "issuers", 0, "email") != "ops@example.com" {
		t.Errorf("policy 2 = %s", raw)
	}
	if dig(t, policies, 3, "subjects") != nil {
		t.Errorf("policy 3 = %s", raw)
	}
	if strings.Contains(string(raw), "plain.example.org") {
		t.Errorf("http-only wildcard has a policy: %s", raw)
	}

	// No wildcard: no on-demand block.
	cfg = build(t, Config{AdminListen: ":2019", PanelUpstream: "dokyr:8080", ACMEEmail: "ops@example.com", Sites: []Site{{Host: "shop.example.com"}}})
	if _, has := dig(t, cfg, "apps", "tls", "automation").(map[string]any)["on_demand"]; has {
		t.Error("on_demand set without a wildcard site")
	}
	// Wildcards stay refused outside project domains, and deeper than one label.
	if _, err := Build(Config{AdminListen: ":2019", PanelUpstream: "dokyr:8080", PanelDomain: "*.example.com"}); err == nil {
		t.Error("wildcard panel domain accepted")
	}
	for _, bad := range []string{"*.com", "*.*.example.com", "a.*.example.com", "*"} {
		if ValidSiteHost(bad) == nil {
			t.Errorf("ValidSiteHost(%q) accepted", bad)
		}
	}
}

// With a panel token, every request passed on to the panel carries it, and nothing else does.
func TestBuildPanelToken(t *testing.T) {
	c := Config{
		AdminListen: "unix//run/caddy/admin.sock", PanelUpstream: "dokyr:8080", PanelDomain: "panel.example.com", PanelToken: "s3cret",
		Routes: []Route{
			{ID: "app", Hosts: []string{"shop.example.com"}, Upstreams: []string{"app:80"}},
			{ID: "registry", Hosts: []string{"registry.example.com"}, Upstreams: []string{"registry:5000"}, PanelPaths: []string{"/api/registry/*"}},
		},
	}
	raw, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	var cfg any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	toPanel, withToken := 0, 0
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case []any:
			for _, x := range v {
				walk(x)
			}
		case map[string]any:
			if v["handler"] == "reverse_proxy" {
				panel := strings.Contains(fmt.Sprint(v["upstreams"]), "dokyr:8080")
				token := strings.Contains(fmt.Sprint(v["headers"]), "s3cret")
				if panel != token {
					t.Errorf("proxy to %v: token sent = %v", v["upstreams"], token)
				}
				if panel {
					toPanel++
				}
				if token {
					withToken++
				}
			}
			for _, x := range v {
				walk(x)
			}
		}
	}
	walk(cfg)
	if toPanel < 3 || withToken != toPanel { // the panel domain, the registry's panel paths, the IP fallback
		t.Fatalf("%d handlers reach the panel, %d send the token", toPanel, withToken)
	}
	c.PanelToken = ""
	if raw, err = Build(c); err != nil || strings.Contains(string(raw), PanelTokenHeader) {
		t.Fatalf("no token, yet the header is set: %v", err)
	}
}

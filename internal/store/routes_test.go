package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

// A route names its hostname: nothing is added first, and no project owns a hostname.
func TestRoutes(t *testing.T) {
	s, ctx := open(t), context.Background()
	shop, _ := s.CreateProject(ctx, "Shop", "")
	blog, _ := s.CreateProject(ctx, "Blog", "")
	front, _ := s.CreateApp(ctx, shop.ID, AppInput{Name: "front", Image: "nginx", Port: 80})
	back, _ := s.CreateApp(ctx, shop.ID, AppInput{Name: "back", Image: "api", Port: 8080})
	posts, _ := s.CreateApp(ctx, blog.ID, AppInput{Name: "posts", Image: "ghost", Port: 2368})

	root, err := s.CreateRoute(ctx, RouteInput{Hostname: "shop.example.com", Path: "/", AppID: front.ID, TLSMode: TLSHTTPOnly})
	if err != nil || root.Hostname != "shop.example.com" || root.AppName != "front" || root.ProjectName != "Shop" || root.TLSMode != TLSHTTPOnly {
		t.Fatalf("create = %+v, %v", root, err)
	}
	if _, err := s.CreateRoute(ctx, RouteInput{Hostname: "shop.example.com", Path: "/", AppID: back.ID}); !errors.Is(err, ErrConflict) {
		t.Fatalf("the same path twice = %v", err)
	}
	// A second route takes the hostname's TLS mode, whatever it asks for.
	api, err := s.CreateRoute(ctx, RouteInput{Hostname: "shop.example.com", Path: "/api", AppID: back.ID, TLSMode: TLSAuto})
	if err != nil || api.TLSMode != TLSHTTPOnly {
		t.Fatalf("second route = %+v, %v", api, err)
	}
	if _, err := s.CreateRoute(ctx, RouteInput{Hostname: "shop.example.com", Path: "/api/v2", AppID: back.ID, StripPrefix: true}); err != nil {
		t.Fatal(err)
	}
	// Another project's app on the same hostname.
	if _, err := s.CreateRoute(ctx, RouteInput{Hostname: "shop.example.com", Path: "/blog", AppID: posts.ID}); err != nil {
		t.Fatal(err)
	}

	sites, err := s.ProxySites(ctx)
	if err != nil || len(sites) != 1 || !sites[0].HTTPOnly {
		t.Fatalf("sites = %+v, %v", sites, err)
	}
	var order []string
	for _, p := range sites[0].Paths {
		order = append(order, p.Path)
	}
	if got := order; len(got) != 4 || got[0] != "/api/v2" || got[1] != "/blog" || got[2] != "/api" || got[3] != "/" || !sites[0].Paths[0].Strip {
		t.Errorf("paths = %+v", sites[0].Paths)
	}
	if routes, _ := s.AppRoutes(ctx, back.ID); len(routes) != 2 || routes[0].Hostname != "shop.example.com" || routes[0].AppName != "back" {
		t.Errorf("back routes = %+v", routes)
	}
	if routes, _ := s.HostRoutes(ctx, "shop.example.com"); len(routes) != 4 {
		t.Errorf("host routes = %+v", routes)
	}

	// The TLS mode is the hostname's: changing it changes every route there.
	if err := s.SetHostTLS(ctx, "shop.example.com", TLSAuto); err != nil {
		t.Fatal(err)
	}
	if routes, _ := s.HostRoutes(ctx, "shop.example.com"); routes[0].TLSMode != TLSAuto || routes[3].TLSMode != TLSAuto {
		t.Errorf("after SetHostTLS = %+v", routes)
	}
	if err := s.SetHostTLS(ctx, "nothing.example.com", TLSAuto); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetHostTLS on an unrouted hostname = %v", err)
	}

	// Moving a route: alone on a new hostname it keeps its mode, joining one it takes that one's.
	if _, err := s.CreateRoute(ctx, RouteInput{Hostname: "cdn.example.com", Path: "/", AppID: front.ID, TLSMode: TLSHTTPOnly}); err != nil {
		t.Fatal(err)
	}
	moved, err := s.UpdateRoute(ctx, api.ID, RouteInput{Hostname: "cdn.example.com", Path: "/api", AppID: back.ID})
	if err != nil || moved.Hostname != "cdn.example.com" || moved.TLSMode != TLSHTTPOnly {
		t.Fatalf("moved to a routed hostname = %+v, %v", moved, err)
	}
	moved, err = s.UpdateRoute(ctx, api.ID, RouteInput{Hostname: "api.example.com", Path: "/", AppID: back.ID})
	if err != nil || moved.Hostname != "api.example.com" || moved.Path != "/" || moved.TLSMode != TLSHTTPOnly {
		t.Fatalf("moved to a new hostname = %+v, %v", moved, err)
	}
	if _, err := s.UpdateRoute(ctx, api.ID, RouteInput{Hostname: "cdn.example.com", Path: "/", AppID: back.ID}); !errors.Is(err, ErrConflict) {
		t.Fatalf("moved onto a taken path = %v", err)
	}

	// Deleting an app takes its routes; a hostname with no route left is simply gone.
	if err := s.DeleteApp(ctx, posts.ID); err != nil {
		t.Fatal(err)
	}
	if routes, _ := s.HostRoutes(ctx, "shop.example.com"); len(routes) != 2 {
		t.Errorf("after app delete = %+v", routes)
	}
	if err := s.DeleteHost(ctx, "shop.example.com"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.HostRouted(ctx, "shop.example.com"); ok {
		t.Error("HostRouted after DeleteHost")
	}
	if err := s.DeleteHost(ctx, "shop.example.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteHost twice = %v", err)
	}
	if all, _ := s.AllRoutes(ctx); len(all) != 2 {
		t.Errorf("all routes = %+v", all)
	}
}

// A landing page on the domain, a tenant app on every subdomain, the admin on its own.
func TestWildcardRoutes(t *testing.T) {
	s, ctx := open(t), context.Background()
	p, _ := s.CreateProject(ctx, "Shop", "")
	landing, _ := s.CreateApp(ctx, p.ID, AppInput{Name: "landing", Image: "nginx", Port: 80})
	tenants, _ := s.CreateApp(ctx, p.ID, AppInput{Name: "tenants", Image: "app", Port: 80})
	for host, app := range map[string]string{"example.com": landing.ID, "*.example.com": tenants.ID, "admin.example.com": tenants.ID} {
		if _, err := s.CreateRoute(ctx, RouteInput{Hostname: host, Path: "/", AppID: app}); err != nil {
			t.Fatal(err)
		}
	}
	sites, err := s.ProxySites(ctx)
	if err != nil || len(sites) != 3 {
		t.Fatalf("sites = %+v, %v", sites, err)
	}
	// A routed name is taken; the wildcard takes no name.
	for host, want := range map[string]bool{"example.com": true, "admin.example.com": true, "other.example.com": false, "*.example.com": true} {
		if ok, _ := s.HostRouted(ctx, host); ok != want {
			t.Errorf("HostRouted(%s) = %v, want %v", host, ok, want)
		}
	}
	if ok, _ := s.WildcardServesTLS(ctx, "example.com"); !ok {
		t.Error("WildcardServesTLS = false")
	}
	if ok, _ := s.WildcardServesTLS(ctx, "admin.example.com"); ok {
		t.Error("WildcardServesTLS = true under a name without a wildcard")
	}
	// The wildcard is a hostname of its own: the domain's TLS mode is not its mode.
	if err := s.SetHostTLS(ctx, "example.com", TLSHTTPOnly); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.WildcardServesTLS(ctx, "example.com"); !ok {
		t.Error("WildcardServesTLS followed the domain's mode")
	}
	if err := s.SetHostTLS(ctx, "*.example.com", TLSHTTPOnly); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.WildcardServesTLS(ctx, "example.com"); ok {
		t.Error("WildcardServesTLS = true for an HTTP-only wildcard")
	}
}

// Domains and routes from before routes named their hostname keep answering.
func TestRoutesSurviveDomainsMigration(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "old.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.migrate(ctx, 49); err != nil {
		t.Fatal(err)
	}
	p, _ := s.CreateProject(ctx, "Shop", "")
	// Inserted by hand: CreateApp writes columns that later migrations add.
	ts := now()
	web := App{ID: "a1"}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO apps (id, project_id, name, image, port, created_at, updated_at) VALUES ('a1', ?, 'web', 'nginx', 80, ?, ?)`, p.ID, ts, ts); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO domains (id, project_id, hostname, tls_mode, created_at, updated_at) VALUES ('d1', '` + p.ID + `', 'example.com', 'http_only', ?1, ?1)`,
		`INSERT INTO domains (id, project_id, hostname, tls_mode, created_at, updated_at) VALUES ('d2', NULL, 'unused.example.org', 'auto', ?1, ?1)`,
		`INSERT INTO domain_routes (id, domain_id, subdomain, path, app_id, strip_prefix, created_at, updated_at) VALUES ('r1', 'd1', '', '/api', '` + web.ID + `', 1, ?1, ?1)`,
		`INSERT INTO domain_routes (id, domain_id, subdomain, path, app_id, strip_prefix, created_at, updated_at) VALUES ('r2', 'd1', 'shop', '/', '` + web.ID + `', 0, ?1, ?1)`,
		`INSERT INTO domain_routes (id, domain_id, subdomain, path, app_id, strip_prefix, created_at, updated_at) VALUES ('r3', 'd1', '*', '/', '` + web.ID + `', 0, ?1, ?1)`,
	} {
		if _, err := s.db.ExecContext(ctx, q, ts); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for id, host := range map[string]string{"r1": "example.com", "r2": "shop.example.com", "r3": "*.example.com"} {
		r, err := s.GetRoute(ctx, id)
		if err != nil || r.Hostname != host || r.AppID != web.ID || r.TLSMode != TLSHTTPOnly {
			t.Errorf("route %s = %+v, %v", id, r, err)
		}
	}
	if r, _ := s.GetRoute(ctx, "r1"); r.Path != "/api" || !r.StripPrefix {
		t.Errorf("r1 = %+v", r)
	}
	// A domain nothing was routed on served nothing: it is gone, and its name is free.
	if ok, _ := s.HostRouted(ctx, "unused.example.org"); ok {
		t.Error("an unrouted domain became a route")
	}
	// The new table keeps its constraints: a path once per hostname, routes going with their app.
	if _, err := s.CreateRoute(ctx, RouteInput{Hostname: "shop.example.com", Path: "/", AppID: web.ID}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate path = %v", err)
	}
	if err := s.DeleteApp(ctx, web.ID); err != nil {
		t.Fatal(err)
	}
	if all, _ := s.AllRoutes(ctx); len(all) != 0 {
		t.Fatalf("routes after app delete = %+v", all)
	}
}

func TestNormalizePath(t *testing.T) {
	for in, want := range map[string]string{"": "/", "/": "/", "api": "/api", "/api/": "/api", " /api/v2 ": "/api/v2", "/a.b~c_d-e": "/a.b~c_d-e"} {
		if got, ok := NormalizePath(in); !ok || got != want {
			t.Errorf("NormalizePath(%q) = %q %v, want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"/api//x", "/../etc", "/a b", "/api/*", "/a?b"} {
		if got, ok := NormalizePath(bad); ok {
			t.Errorf("NormalizePath(%q) = %q, want refused", bad, got)
		}
	}
}

package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestTopLevelDomains(t *testing.T) {
	s, ctx := open(t), context.Background()
	shop, _ := s.CreateProject(ctx, "Shop", "")
	blog, _ := s.CreateProject(ctx, "Blog", "")
	web, _ := s.CreateApp(ctx, shop.ID, AppInput{Name: "web", Image: "nginx", Port: 80})

	// Added on its own: no project uses it, and the proxy knows the hostname already.
	d, err := s.CreateDomain(ctx, "", "shop.example.com", "")
	if err != nil || d.ProjectID != "" || d.ProjectName != "" {
		t.Fatalf("create = %+v, %v", d, err)
	}
	if list, _ := s.ListDomains(ctx, shop.ID); len(list) != 0 {
		t.Fatalf("project domains before attach = %+v", list)
	}
	if sites, _ := s.ProxySites(ctx); len(sites) != 1 || len(sites[0].Paths) != 0 {
		t.Fatalf("sites = %+v", sites)
	}

	// One project at a time.
	if _, err := s.AttachDomain(ctx, "missing", shop.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("attach missing = %v", err)
	}
	if d, err = s.AttachDomain(ctx, d.ID, shop.ID); err != nil || d.ProjectID != shop.ID || d.ProjectName != "Shop" {
		t.Fatalf("attach = %+v, %v", d, err)
	}
	if _, err := s.AttachDomain(ctx, d.ID, shop.ID); err != nil {
		t.Fatalf("attach again = %v", err)
	}
	if _, err := s.AttachDomain(ctx, d.ID, blog.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("attach to a second project = %v", err)
	}
	if _, err := s.CreateRoute(ctx, d.ID, "/", web.ID, false); err != nil {
		t.Fatal(err)
	}
	all, err := s.AllDomains(ctx)
	if err != nil || len(all) != 1 || all[0].ProjectName != "Shop" {
		t.Fatalf("all = %+v, %v", all, err)
	}
	if routes, _ := s.AllRoutes(ctx); len(routes) != 1 {
		t.Fatalf("all routes = %+v", routes)
	}

	// Detaching drops the routes and keeps the domain, free for another project.
	if err := s.DetachDomain(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	if routes, _ := s.AllRoutes(ctx); len(routes) != 0 {
		t.Fatalf("routes after detach = %+v", routes)
	}
	if d, err = s.AttachDomain(ctx, d.ID, blog.ID); err != nil || d.ProjectID != blog.ID {
		t.Fatalf("attach after detach = %+v, %v", d, err)
	}

	// Deleting the project leaves the domain in the list, unused.
	if err := s.DeleteProject(ctx, blog.ID); err != nil {
		t.Fatal(err)
	}
	if d, err = s.GetDomain(ctx, d.ID); err != nil || d.ProjectID != "" {
		t.Fatalf("after project delete = %+v, %v", d, err)
	}
	if err := s.DetachDomain(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("detach missing = %v", err)
	}
}

// Domains and routes created while domains belonged to projects keep working.
func TestDomainsSurviveTopLevelMigration(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "old.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.migrate(ctx, 35); err != nil {
		t.Fatal(err)
	}
	p, _ := s.CreateProject(ctx, "Shop", "")
	web, _ := s.CreateApp(ctx, p.ID, AppInput{Name: "web", Image: "nginx", Port: 80})
	ts := now()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO domains (id, project_id, hostname, tls_mode, created_at, updated_at) VALUES ('d1', ?, 'shop.example.com', 'http_only', ?, ?)`, p.ID, ts, ts); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO domain_routes (id, domain_id, path, app_id, strip_prefix, created_at, updated_at) VALUES ('r1', 'd1', '/api', ?, 1, ?, ?)`, web.ID, ts, ts); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	d, err := s.GetDomain(ctx, "d1")
	if err != nil || d.ProjectID != p.ID || d.Hostname != "shop.example.com" || d.TLSMode != TLSHTTPOnly {
		t.Fatalf("domain = %+v, %v", d, err)
	}
	r, err := s.GetRoute(ctx, "r1")
	if err != nil || r.DomainID != "d1" || r.Path != "/api" || r.AppID != web.ID || !r.StripPrefix {
		t.Fatalf("route = %+v, %v", r, err)
	}
	// The rebuilt tables keep their constraints: unique hostnames, routes cascading.
	if _, err := s.CreateDomain(ctx, "", "shop.example.com", ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate hostname = %v", err)
	}
	if err := s.DeleteDomain(ctx, "d1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetRoute(ctx, "r1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("route after domain delete = %v", err)
	}
}

func TestRoutesOrderAndPaths(t *testing.T) {
	s, ctx := open(t), context.Background()
	p, _ := s.CreateProject(ctx, "Shop", "")
	front, _ := s.CreateApp(ctx, p.ID, AppInput{Name: "front", Image: "nginx", Port: 80})
	back, _ := s.CreateApp(ctx, p.ID, AppInput{Name: "back", Image: "api", Port: 8080})
	d, err := s.CreateDomain(ctx, p.ID, "shop.example.com", TLSHTTPOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []struct {
		path  string
		app   string
		strip bool
	}{{"/", front.ID, false}, {"/api", back.ID, false}, {"/api/v2", back.ID, true}} {
		if _, err := s.CreateRoute(ctx, d.ID, r.path, r.app, r.strip); err != nil {
			t.Fatal(err)
		}
	}
	sites, err := s.ProxySites(ctx)
	if err != nil || len(sites) != 1 || !sites[0].HTTPOnly {
		t.Fatalf("sites = %+v, %v", sites, err)
	}
	var order []string
	for _, p := range sites[0].Paths {
		order = append(order, p.Path)
	}
	if got := order; len(got) != 3 || got[0] != "/api/v2" || got[1] != "/api" || got[2] != "/" || !sites[0].Paths[0].Strip {
		t.Errorf("paths = %+v", sites[0].Paths)
	}
	routes, _ := s.AppRoutes(ctx, back.ID)
	if len(routes) != 2 || routes[0].Hostname != "shop.example.com" || routes[0].AppName != "back" {
		t.Errorf("back routes = %+v", routes)
	}
	if list, _ := s.ProjectRoutes(ctx, p.ID); len(list) != 3 {
		t.Errorf("project routes = %d", len(list))
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

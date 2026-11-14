package store

import (
	"context"
	"testing"
)

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

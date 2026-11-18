package store

import (
	"context"
	"errors"
	"testing"
)

// A landing page on the domain, a tenant app on every subdomain, the admin on its own.
func TestRouteSubdomains(t *testing.T) {
	s, ctx := open(t), context.Background()
	p, _ := s.CreateProject(ctx, "Shop", "")
	landing, _ := s.CreateApp(ctx, p.ID, AppInput{Name: "landing", Image: "nginx", Port: 80})
	tenants, _ := s.CreateApp(ctx, p.ID, AppInput{Name: "tenants", Image: "app", Port: 80})
	d, err := s.CreateDomain(ctx, p.ID, "example.com", TLSAuto)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRoute(ctx, d.ID, "/", landing.ID, false); err != nil {
		t.Fatal(err)
	}
	wild, err := s.CreateRouteAt(ctx, d.ID, WildcardSubdomain, "/", tenants.ID, false)
	if err != nil || wild.Host() != "*.example.com" {
		t.Fatalf("wildcard route = %+v, %v", wild, err)
	}
	admin, err := s.CreateRouteAt(ctx, d.ID, "admin", "/", tenants.ID, false)
	if err != nil || admin.Host() != "admin.example.com" {
		t.Fatalf("admin route = %+v, %v", admin, err)
	}
	// A path is unique per hostname, not per domain.
	if _, err := s.CreateRouteAt(ctx, d.ID, "admin", "/", landing.ID, false); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate admin / = %v, want ErrConflict", err)
	}
	if _, err := s.CreateRouteAt(ctx, d.ID, "admin", "/api", landing.ID, true); err != nil {
		t.Fatal(err)
	}

	sites, err := s.ProxySites(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, site := range sites {
		got[site.Host] = len(site.Paths)
	}
	if len(sites) != 3 || got["example.com"] != 1 || got["*.example.com"] != 1 || got["admin.example.com"] != 2 {
		t.Errorf("sites = %+v", sites)
	}

	// A routed subdomain is taken; the wildcard takes no name.
	for host, want := range map[string]bool{"example.com": true, "admin.example.com": true, "other.example.com": false, "*.example.com": false} {
		if ok, _ := s.DomainExists(ctx, host); ok != want {
			t.Errorf("DomainExists(%s) = %v, want %v", host, ok, want)
		}
	}
	if ok, _ := s.WildcardServesTLS(ctx, "example.com"); !ok {
		t.Error("WildcardServesTLS = false")
	}
	if _, err := s.SetDomainTLS(ctx, d.ID, TLSHTTPOnly); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.WildcardServesTLS(ctx, "example.com"); ok {
		t.Error("WildcardServesTLS = true for an HTTP-only domain")
	}

	// Moving a route to another subdomain; a domain with only subdomain routes is still a site.
	if _, err := s.UpdateRoute(ctx, wild.ID, "app", "/", tenants.ID, false); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.WildcardServesTLS(ctx, "example.com"); ok {
		t.Error("WildcardServesTLS = true without a * route")
	}
	d2, _ := s.CreateDomain(ctx, p.ID, "example.org", TLSAuto)
	if _, err := s.CreateRouteAt(ctx, d2.ID, WildcardSubdomain, "/", tenants.ID, false); err != nil {
		t.Fatal(err)
	}
	sites, _ = s.ProxySites(ctx)
	got = map[string]int{}
	for _, site := range sites {
		got[site.Host] = len(site.Paths)
	}
	if n, ok := got["example.org"]; !ok || n != 0 || got["*.example.org"] != 1 {
		t.Errorf("sites = %+v", sites)
	}
}

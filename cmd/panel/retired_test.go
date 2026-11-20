package main

import (
	"reflect"
	"testing"

	"panel/internal/proxy"
)

func TestRetiredSites(t *testing.T) {
	app := []proxy.Path{{ID: "r1", AppID: "a1", Prefix: "/"}}
	cfg := proxy.Config{
		AdminListen: "unix//run/caddy/admin.sock", PanelUpstream: "dokyr:8080",
		PanelDomain: "panel.example.org",
		Routes:      []proxy.Route{{ID: "registry", Hosts: []string{"registry.example.com"}}},
		Sites: []proxy.Site{
			{Host: "*.example.com", Paths: app},
			{Host: "admin.example.net", Paths: app}, // an admin routed this retired name
			{Host: "*.example.net", Paths: app},
			{Host: "*.plain.test", HTTPOnly: true, Paths: app},
		},
	}
	got := retiredSites([]string{
		"Panel.example.com",    // covered by a wildcard: taken back
		"panel.example.com",    // once only
		"registry.example.com", // the registry has it now
		"admin.example.net",    // routed on purpose
		"old.example.io",       // nothing serves it
		"panel.example.org",    // the panel has it again
		"old.plain.test",       // keeps the wildcard's plain HTTP
		"localhost",
	}, cfg)
	want := []proxy.Site{{Host: "panel.example.com"}, {Host: "old.plain.test", HTTPOnly: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("retiredSites = %+v, want %+v", got, want)
	}
	// The result must load: an exact site without paths answers 404 ahead of the wildcard.
	cfg.Sites = append(cfg.Sites, got...)
	if _, err := proxy.Build(cfg); err != nil {
		t.Fatalf("config with retired sites: %v", err)
	}
}

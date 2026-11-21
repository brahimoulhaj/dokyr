package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestSearchResources(t *testing.T) {
	s, ctx := open(t), context.Background()
	p, err := s.CreateProject(ctx, "Équipe", "Customer portal")
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.CreateApp(ctx, p.ID, AppInput{Name: "backend", Image: "nginx", Port: 80, Environment: map[string]string{"SECRET": "hidden-environment"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRoute(ctx, RouteInput{Hostname: "portal.example.com", Path: "/", AppID: a.ID}); err != nil {
		t.Fatal(err)
	}
	// Seed intentionally unreadable secrets: search must never attempt decryption.
	for _, query := range []string{
		`INSERT INTO registries VALUES ('registry', 'Images', 'ghcr.io', 'private-user', 'hidden-registry', 'now', 'now')`,
		`INSERT INTO git_sources VALUES ('git', 'gitlab', 'Source', 'https://gitlab.com', 'hidden-git', 'now', 'now')`,
		`INSERT INTO s3_connections (id,name,endpoint,bucket,access_key,secret_key,created_at,updated_at) VALUES ('storage','Backups','s3.example.com','archive','hidden-access','hidden-storage','now','now')`,
	} {
		if _, err := s.db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO db_instances (id,project_id,name,engine,version,database_name,username,credentials,created_at,updated_at) VALUES ('db',?,'main','postgres','18','private-db','private-user','hidden-database','now','now')`, p.ID); err != nil {
		t.Fatal(err)
	}
	all, truncated, err := s.SearchResources(ctx, "")
	if err != nil || len(all) != 7 || truncated {
		t.Fatalf("all = %+v, %v, %v", all, truncated, err)
	}
	body, _ := json.Marshal(all)
	if strings.Contains(string(body), "hidden-") || strings.Contains(string(body), "private-") {
		t.Fatalf("secrets in search: %s", body)
	}
	for _, tc := range []struct{ query, kind, id string }{
		{"EQUIPE BACKEND", "app", a.ID},
		{"customer portal", "project", p.ID},
		{"ghcr", "registry", "registry"},
		{"gitlab", "git", "git"},
		{"archive", "storage", "storage"},
		{"postgres 18", "database", "db"},
		{"portal.example", "domain", ""},
		{a.ID, "app", a.ID},
		{strings.ToLower(a.ID), "app", a.ID},
		{"regis", "registry", "registry"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			got, _, err := s.SearchResources(ctx, tc.query)
			if err != nil || len(got) != 1 || got[0].Kind != tc.kind || (tc.id != "" && got[0].ID != tc.id) {
				t.Fatalf("search = %+v, %v", got, err)
			}
		})
	}
	// "gistry" sits inside the registry's ID, but IDs only match by prefix.
	for _, query := range []string{"hidden-environment", "hidden-database", "%", "' OR 1=1 --", "unmatched", "gistry", a.ID[4:12]} {
		got, _, err := s.SearchResources(ctx, query)
		if err != nil || len(got) != 0 {
			t.Fatalf("%q = %+v, %v", query, got, err)
		}
	}
	for i := 0; i < 12; i++ {
		description := ""
		if i < 8 {
			description = "first batch"
		}
		if _, err := s.CreateProject(ctx, fmt.Sprintf("backend-%02d", i), description); err != nil {
			t.Fatal(err)
		}
	}
	got, truncated, err := s.SearchResources(ctx, "backend")
	if err != nil || len(got) != 9 || got[0].ID != a.ID || !truncated {
		t.Fatalf("ranked and bounded = %+v, %v, %v", got, truncated, err)
	}
	// Exactly searchLimit matches of a kind is not truncated.
	got, truncated, err = s.SearchResources(ctx, "first batch")
	if err != nil || len(got) != 8 || truncated {
		t.Fatalf("exactly full = %+v, %v, %v", got, truncated, err)
	}
	if err := s.DeleteApp(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	got, _, err = s.SearchResources(ctx, a.ID)
	if err != nil || len(got) != 0 {
		t.Fatalf("deleted = %+v, %v", got, err)
	}
}

package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestProjectSummaries(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	p, _ := s.CreateProject(ctx, "shop", "")
	empty, _ := s.CreateProject(ctx, "empty", "")
	web, _ := s.CreateApp(ctx, p.ID, AppInput{Name: "web", Image: "nginx", Port: 80})
	api, _ := s.CreateApp(ctx, p.ID, AppInput{Name: "api", Image: "nginx", Port: 80})
	if _, err := s.CreateApp(ctx, p.ID, AppInput{Name: "new", Image: "nginx", Port: 80}); err != nil {
		t.Fatal(err)
	}
	logPath := func(string) string { return "" }
	d1, _ := s.CreateDeployment(ctx, web.ID, "manual", "nginx", 80, logPath)
	_ = s.FinishDeployment(ctx, d1.ID, DeploySucceeded, "")
	_ = s.SetAppCurrentDeployment(ctx, web.ID, d1.ID, StateRunning)
	d2, _ := s.CreateDeployment(ctx, api.ID, "manual", "nginx", 80, logPath)
	_ = s.FinishDeployment(ctx, d2.ID, DeployFailed, "boom")
	_ = s.SetAppCurrentDeployment(ctx, api.ID, d1.ID, "stopped")
	if _, err := s.CreateRoute(ctx, RouteInput{Hostname: "shop.example.com", Path: "/", AppID: web.ID}); err != nil {
		t.Fatal(err)
	}
	sums, err := s.ProjectSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := sums[p.ID]
	if got.AppsRunning != 1 || got.AppsStopped != 1 || got.AppsNotDeployed != 1 || got.Domains != 1 || got.Databases != 0 ||
		got.LastDeployAt == nil || got.LastDeployStatus != DeployFailed || got.LastDeployApp != "api" {
		t.Fatalf("summary %+v", got)
	}
	if e := sums[empty.ID]; e.LastDeployAt != nil || e.AppsRunning != 0 {
		t.Fatalf("empty project %+v", e)
	}
}

func TestSearchEvents(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	for _, e := range []Event{
		{Actor: "ana@example.com", Action: "app.create", TargetName: "web"},
		{Actor: "system", Action: "deployment.succeeded", TargetName: "web"},
		{Actor: "system", Action: "deployment.failed", TargetName: "api", Detail: "health check 100% failed"},
		{Actor: "webhook", Action: "app.deploy", TargetName: "api"},
		{Actor: "system", Action: "backup.failed", TargetName: "main"},
		{Actor: "bob@example.com", Action: "settings.panel", TargetName: "panel"},
		{Actor: "system", Action: "usage.memory_high", TargetName: "main"},
	} {
		if err := s.AddEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	names := func(q EventQuery) ([]string, int) {
		t.Helper()
		if q.Limit == 0 {
			q.Limit = 50
		}
		list, total, err := s.SearchEvents(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, e := range list {
			out = append(out, e.Action)
		}
		return out, total
	}
	check := func(q EventQuery, want ...string) {
		t.Helper()
		got, total := names(q)
		if strings.Join(got, ",") != strings.Join(want, ",") || total != len(want) {
			t.Fatalf("%+v: got %v (total %d), want %v", q, got, total, want)
		}
	}
	check(EventQuery{Text: "API"}, "app.deploy", "deployment.failed")
	check(EventQuery{Text: "100%"}, "deployment.failed") // % is matched literally
	check(EventQuery{Group: "deploys"}, "app.deploy", "deployment.failed", "deployment.succeeded")
	check(EventQuery{Group: "databases"}, "backup.failed")
	check(EventQuery{Actor: "people"}, "settings.panel", "app.create")
	check(EventQuery{Failures: true}, "usage.memory_high", "backup.failed", "deployment.failed")
	check(EventQuery{Failures: true, Text: "main", Group: "alerts"}, "usage.memory_high", "backup.failed")
	check(EventQuery{Since: time.Now().Add(time.Hour)})
	// Paging: newest first, total of all matches.
	if got, total := names(EventQuery{Limit: 2, Offset: 2}); total != 7 || strings.Join(got, ",") != "backup.failed,app.deploy" {
		t.Fatalf("page 2: %v of %d", got, total)
	}
}

package deploy

import (
	"context"
	"strings"
	"testing"

	"panel/internal/database"
	"panel/internal/store"
)

func TestDeployDatabaseEnvironment(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	rec := &environmentExecutor{Executor: e.ex}
	e.svc.opts.Executor = rec
	d, err := e.st.CreateDBInstance(ctx, store.DBInstance{ProjectID: e.app.ProjectID, Name: "main", Engine: "postgres", Version: "18", DatabaseName: "app", Username: "app"}, map[string]string{"password": "app-secret", "root_password": "admin-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.SetDBInstanceState(ctx, d.ID, "running", "running", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.st.SetProjectEnvironment(ctx, e.app.ProjectID, map[string]string{"DB": "${db.main.URL}"}, false); err != nil {
		t.Fatal(err)
	}
	e.app, err = e.st.UpdateApp(ctx, e.app.ID, appInputWithEnv(e, map[string]string{
		"DATABASE_URL": "${project.DB}", "CUSTOM_URL": "custom://${db.main.HOST}:${db.main.PORT}/${db.main.DATABASE}",
		"DB_USER": "${db.main.USERNAME}", "DB_PASSWORD": "${db.main.PASSWORD}",
	}))
	if err != nil {
		t.Fatal(err)
	}
	dep := e.deploy()
	if dep.Status != "succeeded" {
		t.Fatalf("database deploy = %s\n%s", dep.Status, e.log(dep))
	}
	if rec.environment["DATABASE_URL"] != database.Connection(d, map[string]string{"password": "app-secret"})["URL"] {
		t.Fatal("database reference was not resolved at deploy")
	}
	if rec.environment["CUSTOM_URL"] != "custom://"+database.ContainerName(d.ID)+":5432/app" || rec.environment["DB_USER"] != "app" || rec.environment["DB_PASSWORD"] != "app-secret" {
		t.Fatal("composed database references were not resolved at deploy")
	}
	if strings.Contains(e.log(dep), "app-secret") || strings.Contains(e.log(dep), "admin-secret") {
		t.Fatal("database credentials leaked into deploy log")
	}
	e.st.SetDBInstanceState(ctx, d.ID, "stopped", "stopped", "")
	dep = e.deploy()
	if dep.Status != "failed" || !strings.Contains(dep.Error, "must be running") {
		t.Fatalf("stopped DB deploy = %+v", dep)
	}
}

func TestResolveEnvironment(t *testing.T) {
	project := map[string]string{"DB_HOST": "db.internal", "LOG_LEVEL": "info"}
	app := map[string]string{
		"DATABASE_URL": "postgres://${project.DB_HOST}:5432/app",
		"LOG_LEVEL":    "debug",
		"PLAIN":        "x",
		"LITERAL":      "${not.a.ref} $project.DB_HOST",
	}
	got, err := resolveEnvironment(app, project, false)
	if err != nil {
		t.Fatal(err)
	}
	if got["DATABASE_URL"] != "postgres://db.internal:5432/app" || got["LOG_LEVEL"] != "debug" || got["LITERAL"] != "${not.a.ref} $project.DB_HOST" {
		t.Errorf("resolved = %v", got)
	}
	if _, ok := got["DB_HOST"]; ok {
		t.Error("not inherited, yet DB_HOST is there")
	}
	// Inherit: every project variable, the app's own value winning.
	got, err = resolveEnvironment(app, project, true)
	if err != nil || got["DB_HOST"] != "db.internal" || got["LOG_LEVEL"] != "debug" {
		t.Errorf("inherited = %v, %v", got, err)
	}
	// A reference to nothing fails.
	if _, err := resolveEnvironment(map[string]string{"A": "${project.NOPE}"}, project, false); err == nil || !strings.Contains(err.Error(), "NOPE") {
		t.Errorf("unknown reference: %v", err)
	}
}

func TestDeployProjectEnvironment(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	rec := &environmentExecutor{Executor: e.ex}
	e.svc.opts.Executor = rec
	if err := e.st.SetProjectEnvironment(ctx, e.app.ProjectID, map[string]string{"REGION": "eu", "SHARED": "1"}, true); err != nil {
		t.Fatal(err)
	}
	a, err := e.st.UpdateApp(ctx, e.app.ID, appInputWithEnv(e, map[string]string{"WHERE": "${project.REGION}-1", "SHARED": "app"}))
	if err != nil {
		t.Fatal(err)
	}
	e.app = a
	dep := e.deploy()
	if dep.Status != "succeeded" {
		t.Fatalf("deploy = %+v\n%s", dep, e.log(dep))
	}
	env := rec.environment
	if env["WHERE"] != "eu-1" || env["REGION"] != "eu" || env["SHARED"] != "app" {
		t.Errorf("container env = %v", env)
	}
	if !strings.Contains(e.log(dep), "Inheriting 2 project variables") {
		t.Errorf("log:\n%s", e.log(dep))
	}

	// A typo in a reference fails before anything starts.
	a, _ = e.st.UpdateApp(ctx, e.app.ID, appInputWithEnv(e, map[string]string{"WHERE": "${project.REGOIN}"}))
	e.app = a
	if dep := e.deploy(); dep.Status != "failed" || !strings.Contains(dep.Error, "REGOIN") {
		t.Errorf("typo = %s %q", dep.Status, dep.Error)
	}
}

func appInputWithEnv(e *env, vars map[string]string) store.AppInput {
	return store.AppInput{Name: "web", Image: e.app.Image, Port: e.app.Port, CPULimit: 1, MemoryMB: 64, Environment: vars}
}

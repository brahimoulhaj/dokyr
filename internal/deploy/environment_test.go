package deploy

import (
	"context"
	"errors"
	"panel/internal/builder"
	"reflect"
	"strings"
	"testing"
	"time"

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

// A database still being created (made with the app from a template, deployed right after) is waited for.
func TestDeployWaitsForStartingDatabase(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	d, err := e.st.CreateDBInstance(ctx, store.DBInstance{ProjectID: e.app.ProjectID, Name: "main", Engine: "mariadb", Version: "11.4", DatabaseName: "app", Username: "app"}, map[string]string{"password": "p", "root_password": "r"})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.SetDBInstanceState(ctx, d.ID, "running", "provisioning", ""); err != nil {
		t.Fatal(err)
	}
	if e.app, err = e.st.UpdateApp(ctx, e.app.ID, appInputWithEnv(e, map[string]string{"DB": "${db.main.URL}"})); err != nil {
		t.Fatal(err)
	}
	e.svc.opts.DatabaseWait = 50 * time.Millisecond
	if dep := e.deploy(); dep.Status != "failed" || !strings.Contains(dep.Error, "must be running") {
		t.Fatalf("deploy with a database that never starts = %+v", dep)
	}
	e.svc.opts.DatabaseWait = time.Minute
	go func() {
		time.Sleep(200 * time.Millisecond)
		e.st.SetDBInstanceState(ctx, d.ID, "running", "running", "")
	}()
	dep := e.deploy()
	if dep.Status != "succeeded" || !strings.Contains(e.log(dep), "Waiting for the database") {
		t.Fatalf("deploy = %s\n%s", dep.Status, e.log(dep))
	}
}

func TestResolveAppRefs(t *testing.T) {
	apps := []store.App{{ID: "01ABC", Name: "db", Port: 3306}, {ID: "01DEF", Name: "web", Port: 80}}
	got, err := resolveAppRefs(map[string]string{"A": "${app.db.HOST}:${app.db.PORT}", "B": "plain", "C": "http://${app.web.HOST}/"}, apps)
	if err != nil || got["A"] != "app-01abc:3306" || got["B"] != "plain" || got["C"] != "http://app-01def/" {
		t.Fatalf("resolveAppRefs = %v, %v", got, err)
	}
	if _, err := resolveAppRefs(map[string]string{"A": "${app.gone.HOST}"}, apps); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("unknown app = %v", err)
	}
	if _, err := resolveAppRefs(map[string]string{"A": "${app.db.URL}"}, apps); err == nil || !strings.Contains(err.Error(), "HOST or PORT") {
		t.Fatalf("unknown field = %v", err)
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

// A deployment passes build-scoped variables to the build (secrets apart) and the others
// to the container, and never writes a secret value to the log or the stored error.
func TestDeployBuildVariablesAndSecrets(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	b := &fakeBuilder{commit: strings.Repeat("ab12", 10), output: []string{"#7 0.1 token is npm_s3cretvalue here"}}
	e.useGit(b)
	rec := &environmentExecutor{Executor: e.ex}
	e.svc.opts.Executor = rec
	if err := e.st.SetProjectEnvironment(ctx, e.app.ProjectID, map[string]string{"REGION": "eu", "SHARED_KEY": "pr0ject-secret"}, true); err != nil {
		t.Fatal(err)
	}
	var err error
	e.app, err = e.st.UpdateApp(ctx, e.app.ID, store.AppInput{
		Name: e.app.Name, SourceType: store.SourceGit, Port: e.app.Port, Git: e.app.Git,
		Environment: map[string]string{
			"APP_ENV": "production", "NODE_VERSION": "22", "PUBLIC_URL": "https://${project.REGION}.example.com",
			"NPM_TOKEN": "npm_s3cretvalue", "API_KEY": "${project.SHARED_KEY}", "SESSION_KEY": "runtime-s3cret",
		},
		EnvMeta: map[string]store.EnvVarMeta{
			"NODE_VERSION": {Scope: store.EnvScopeBuild},
			"PUBLIC_URL":   {Scope: store.EnvScopeBoth},
			"NPM_TOKEN":    {Scope: store.EnvScopeBuild, Secret: true},
			"API_KEY":      {Scope: store.EnvScopeBoth, Secret: true},
			"SESSION_KEY":  {Scope: store.EnvScopeRuntime, Secret: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	dep := e.deploy()
	if dep.Status != store.DeploySucceeded {
		t.Fatalf("deployment: %s", dep.Error)
	}
	wantArgs := map[string]string{"NODE_VERSION": "22", "PUBLIC_URL": "https://eu.example.com"}
	wantSecrets := map[string]string{"NPM_TOKEN": "npm_s3cretvalue", "API_KEY": "pr0ject-secret"}
	if !reflect.DeepEqual(b.vars.Args, wantArgs) || !reflect.DeepEqual(b.vars.Secrets, wantSecrets) {
		t.Errorf("build got args %v, secrets %v", b.vars.Args, b.vars.Secrets)
	}
	// Inherited project variables have no scope: runtime only.
	wantRuntime := map[string]string{
		"APP_ENV": "production", "PUBLIC_URL": "https://eu.example.com", "API_KEY": "pr0ject-secret",
		"SESSION_KEY": "runtime-s3cret", "REGION": "eu", "SHARED_KEY": "pr0ject-secret", "PORT": "8080",
	}
	if !reflect.DeepEqual(rec.environment, wantRuntime) {
		t.Errorf("container got %v", rec.environment)
	}
	log := e.log(dep)
	if strings.Contains(log, "npm_s3cretvalue") || !strings.Contains(log, "token is *** here") || !strings.Contains(log, "Build variables: 2 plain, 2 secret") {
		t.Errorf("log:\n%s", log)
	}

	// A failing build's last line becomes the deployment's error: no secret there either.
	b.buildErr = errors.New("docker build: exit status 1: bad token npm_s3cretvalue")
	dep = e.deploy()
	if dep.Status != store.DeployFailed || strings.Contains(dep.Error, "npm_s3cretvalue") || !strings.Contains(dep.Error, "bad token ***") {
		t.Errorf("failed deployment error = %q", dep.Error)
	}
	if strings.Contains(e.log(dep), "npm_s3cretvalue") {
		t.Errorf("log:\n%s", e.log(dep))
	}
}

func TestSecretValues(t *testing.T) {
	env := map[string]string{"A": "abcdef", "B": "abc", "C": "abcdefgh-long", "PEM": "-----BEGIN-----\nMIIBVgIBADANBg\nxy\n", "PLAIN": "visible-value"}
	meta := map[string]store.EnvVarMeta{"A": {Secret: true}, "B": {Secret: true}, "C": {Secret: true}, "PEM": {Secret: true}}
	got := secretValues(env, meta)
	want := []string{"-----BEGIN-----\nMIIBVgIBADANBg\nxy\n", "-----BEGIN-----", "MIIBVgIBADANBg", "abcdefgh-long", "abcdef"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("secretValues = %q", got)
	}
	w := &logWriter{}
	w.hide(got)
	if s := w.redact("x abcdefgh-long y abcdef z MIIBVgIBADANBg visible-value"); s != "x *** y *** z *** visible-value" {
		t.Errorf("redact = %q", s)
	}
}

// TestDeployAddsPHPExtensions: the first Railpack build of a PHP project adds the build
// variable that lists the database drivers, once; what the user makes of it afterwards stays.
func TestDeployAddsPHPExtensions(t *testing.T) {
	const name = builder.PHPExtensionsVariable
	e, ctx := newEnv(t), context.Background()
	b := &fakeBuilder{commit: strings.Repeat("ab12", 10), files: map[string]string{"composer.json": "{}"}}
	e.useGit(b)
	setBuilder := func(kind string, env map[string]string, meta map[string]store.EnvVarMeta) {
		t.Helper()
		git := e.app.Git
		git.Builder = kind
		var err error
		if e.app, err = e.st.UpdateApp(ctx, e.app.ID, store.AppInput{Name: e.app.Name, SourceType: store.SourceGit, Port: e.app.Port, Git: git, Environment: env, EnvMeta: meta}); err != nil {
			t.Fatal(err)
		}
	}
	deploy := func() {
		t.Helper()
		if dep := e.deploy(); dep.Status != store.DeploySucceeded {
			t.Fatalf("deployment: %s", dep.Error)
		}
	}

	// A Dockerfile build is not Railpack's: nothing is added.
	deploy()
	if a, _ := e.st.GetApp(ctx, e.app.ID); len(a.Environment) != 0 || b.vars.Args[name] != "" {
		t.Fatalf("Dockerfile build: env %v, build args %v", a.Environment, b.vars.Args)
	}

	// Automatic without a Dockerfile is Railpack: added to the app as a build-time variable,
	// given to this very build, and the app does not then say it needs a redeploy.
	setBuilder(store.BuilderAuto, map[string]string{"APP_ENV": "production"}, nil)
	deploy()
	a, _ := e.st.GetApp(ctx, e.app.ID)
	if a.Environment[name] != builder.DefaultPHPExtensions || a.EnvMeta[name] != (store.EnvVarMeta{Scope: store.EnvScopeBuild}) || a.Environment["APP_ENV"] != "production" {
		t.Fatalf("app after the first build: env %v, meta %v", a.Environment, a.EnvMeta)
	}
	if got := a.EnvOrder; len(got) == 0 || got[len(got)-1] != name {
		t.Fatalf("order = %v", got)
	}
	if b.vars.Args[name] != builder.DefaultPHPExtensions {
		t.Fatalf("build args = %v", b.vars.Args)
	}
	if e.svc.NeedsRedeploy(ctx, a) {
		t.Fatal("the app says its settings changed since the deployment")
	}

	// Edited to PostgreSQL only: kept, and passed as it is.
	e.app = a
	setBuilder(store.BuilderRailpack, map[string]string{name: "pdo_pgsql"}, map[string]store.EnvVarMeta{name: {Scope: store.EnvScopeBuild}})
	deploy()
	if a, _ := e.st.GetApp(ctx, e.app.ID); a.Environment[name] != "pdo_pgsql" || b.vars.Args[name] != "pdo_pgsql" {
		t.Fatalf("edited value: env %v, build args %v", a.Environment, b.vars.Args)
	}

	// Emptied: still the user's choice, not filled again.
	setBuilder(store.BuilderRailpack, map[string]string{name: ""}, map[string]store.EnvVarMeta{name: {Scope: store.EnvScopeBuild}})
	deploy()
	if a, _ := e.st.GetApp(ctx, e.app.ID); a.Environment[name] != "" {
		t.Fatalf("emptied value filled again: %v", a.Environment)
	}

	// Not a PHP project: nothing is added.
	b.files = map[string]string{"package.json": "{}"}
	setBuilder(store.BuilderRailpack, map[string]string{}, nil)
	deploy()
	if a, _ := e.st.GetApp(ctx, e.app.ID); len(a.Environment) != 0 {
		t.Fatalf("Node project got %v", a.Environment)
	}
}

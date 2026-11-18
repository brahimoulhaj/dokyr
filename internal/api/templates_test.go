package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"panel/internal/account"
	"panel/internal/auth"
	"panel/internal/brand"
	"panel/internal/database"
	"panel/internal/executor/executortest"
	"panel/internal/secret"
	"panel/internal/store"
)

func TestTemplates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	box, err := secret.LoadOrCreate(filepath.Join(dir, "key"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UseSecrets(ctx, box); err != nil {
		t.Fatal(err)
	}
	owner, _ := st.CreateUser(ctx, "owner@example.com", "hash", "owner")
	if err := st.CreateSession(ctx, owner.ID, auth.HashToken("test-session"), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	p, _ := st.CreateProject(ctx, "shop", "")
	svc := database.New(ctx, st, databaseTestHost{executortest.New()})
	defer func() { cancel(); svc.Wait() }()
	server := New(st, account.New(st), slog.New(slog.NewTextHandler(io.Discard, nil)), "test", fstest.MapFS{"index.html": {Data: []byte("app")}})
	server.SetDatabases(svc)
	handler := server.Handler()
	call := func(method, path string, body any, want int) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.AddCookie(&http.Cookie{Name: brand.SessionCookie(), Value: "test-session"})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("%s %s = %d, want %d: %s", method, path, rec.Code, want, rec.Body.String())
		}
		var out map[string]any
		json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}

	req := httptest.NewRequest("GET", "/api/templates", nil)
	req.AddCookie(&http.Cookie{Name: brand.SessionCookie(), Value: "test-session"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var list []map[string]any
	if json.Unmarshal(rec.Body.Bytes(), &list); rec.Code != 200 || len(list) == 0 || list[0]["id"] != "wordpress" {
		t.Fatalf("GET /api/templates = %d %s", rec.Code, rec.Body.String())
	}

	for _, tpl := range list {
		if tpl["variables"] == nil || tpl["volumes"] == nil {
			t.Fatalf("template %v: variables and volumes must be lists, never null", tpl["id"])
		}
	}
	base := "/api/projects/" + p.ID + "/templates/"
	call("POST", base+"missing", map[string]any{}, 404)
	call("POST", "/api/projects/missing/templates/wordpress", map[string]any{}, 404)
	call("POST", base+"wordpress", map[string]any{"name": "Bad Name"}, 422)

	out := call("POST", base+"wordpress", map[string]any{}, 201)
	app := out["app"].(map[string]any)
	appID := app["id"].(string)
	if app["name"] != "wordpress" || app["image"] != "wordpress:6-apache" || app["strategy"] != "recreate" || app["template"] != "wordpress" {
		t.Fatalf("created = %v", out)
	}
	// No database is made, and the variables one fills are there, empty.
	if env := app["environment"].(map[string]any); len(env) != 4 || env["WORDPRESS_DB_PASSWORD"] != "" {
		t.Fatalf("environment = %v", env)
	}
	if dbs, _ := st.ListDBInstances(ctx); len(dbs) != 0 || out["database"] != nil {
		t.Fatalf("a template must not create a database: %d", len(dbs))
	}
	call("POST", base+"wordpress", map[string]any{}, 409)

	// Connect a database: the instance's own one by reference.
	connect := "/api/apps/" + appID + "/database"
	maria, err := svc.Create(ctx, store.DBInstance{ProjectID: p.ID, Name: "main", Engine: "mariadb", Version: "11.4", DatabaseName: "app", Username: "app"})
	if err != nil {
		t.Fatal(err)
	}
	pgdb, err := svc.Create(ctx, store.DBInstance{ProjectID: p.ID, Name: "pg", Engine: "postgres", Version: "17", DatabaseName: "app", Username: "app"})
	if err != nil {
		t.Fatal(err)
	}
	other, _ := st.CreateProject(ctx, "other", "")
	foreign, err := svc.Create(ctx, store.DBInstance{ProjectID: other.ID, Name: "main", Engine: "mariadb", Version: "11.4", DatabaseName: "app", Username: "app"})
	if err != nil {
		t.Fatal(err)
	}
	call("POST", connect, map[string]any{"instance_id": "missing"}, 422)
	call("POST", connect, map[string]any{"instance_id": pgdb.ID}, 422)    // WordPress needs MariaDB
	call("POST", connect, map[string]any{"instance_id": foreign.ID}, 422) // another project's
	got := call("POST", connect, map[string]any{"instance_id": maria.ID}, 200)
	if env := got["environment"].(map[string]any); env["WORDPRESS_DB_HOST"] != "${db.main.HOST}:${db.main.PORT}" || env["WORDPRESS_DB_PASSWORD"] != "${db.main.PASSWORD}" {
		t.Fatalf("connected by reference = %v", env)
	}
	// Another database and user of the instance: the values are written, the password as a secret.
	shopDB, _ := st.AddManagedDatabase(ctx, maria.ID, "shop")
	added, err := st.AddDatabaseUser(ctx, maria.ID, "shopuser", "shop-pass", false)
	if err != nil {
		t.Fatal(err)
	}
	shopUser := added.ID
	got = call("POST", connect, map[string]any{"instance_id": maria.ID, "database_id": shopDB.ID, "user_id": shopUser}, 200)
	env := got["environment"].(map[string]any)
	if env["WORDPRESS_DB_NAME"] != "shop" || env["WORDPRESS_DB_USER"] != "shopuser" || env["WORDPRESS_DB_HOST"] != database.ContainerName(maria.ID)+":3306" {
		t.Fatalf("connected with values = %v", env)
	}
	if _, shown := env["WORDPRESS_DB_PASSWORD"]; shown {
		t.Fatal("the written password must be a secret, never returned")
	}
	if stored, _ := st.GetApp(ctx, appID); stored.Environment["WORDPRESS_DB_PASSWORD"] != "shop-pass" || !stored.EnvMeta["WORDPRESS_DB_PASSWORD"].Secret {
		t.Fatalf("stored password = %q", stored.Environment["WORDPRESS_DB_PASSWORD"])
	}

	// A Compose file becomes one app for each service, after a dry run that creates nothing.
	file := "services:\n  cache:\n    image: redis:7\n    expose: [6379]\n  api:\n    image: ghcr.io/org/api:1\n    ports: ['3000:3000']\n    environment:\n      REDIS_URL: redis://cache:6379\n    volumes: ['uploads:/data']\n"
	composePath := "/api/projects/" + p.ID + "/compose"
	call("POST", composePath, map[string]any{"compose": "services:\n  web:\n    build: ."}, 422)
	call("POST", composePath, map[string]any{"compose": "services:\n  web:\n    image: 'bad image'"}, 422)
	call("POST", composePath, map[string]any{"compose": "services:\n  wordpress:\n    image: nginx"}, 409)
	out = call("POST", composePath, map[string]any{"compose": file, "dry_run": true}, 200)
	if n, _ := st.ListApps(ctx, p.ID); len(out["apps"].([]any)) != 2 || len(n) != 1 {
		t.Fatalf("dry run = %v, %d apps stored", out, len(n))
	}
	out = call("POST", composePath, map[string]any{"compose": file}, 201)
	first := out["apps"].([]any)[1].(map[string]any)
	api, err := st.GetApp(ctx, first["id"].(string))
	if err != nil || api.Name != "api" || api.Port != 3000 || api.Environment["REDIS_URL"] != "redis://${app.cache.HOST}:6379" || len(api.Mounts) != 1 || api.CurrentDeploymentID != "" {
		t.Fatalf("api app = %+v, %v", api, err)
	}
	// A service without a port is healthy when its container stays up; one with a port is probed.
	portless := call("POST", composePath, map[string]any{"compose": "services:\n  jobs:\n    image: busybox"}, 201)["apps"].([]any)[0].(map[string]any)
	if hc, _, _ := st.AppHealthCheck(ctx, portless["id"].(string)); portless["port"] != float64(0) || hc.Type != store.HealthRunning {
		t.Fatalf("portless app = %v, health check %q", portless, hc.Type)
	}
	if hc, _, _ := st.AppHealthCheck(ctx, api.ID); hc.Type != store.HealthTCP {
		t.Fatalf("app with a port: health check %q", hc.Type)
	}
	call("POST", composePath, map[string]any{"compose": file}, 409)
	if n, _ := st.ListApps(ctx, p.ID); len(n) != 4 {
		t.Fatalf("%d apps after the compose import", len(n))
	}

	// A template without a database has nothing to connect.
	out = call("POST", base+"jenkins", map[string]any{"name": "ci"}, 201)
	call("POST", "/api/apps/"+out["app"].(map[string]any)["id"].(string)+"/database", map[string]any{"instance_id": maria.ID}, 422)
	if out["app"].(map[string]any)["port"] != float64(8080) {
		t.Fatalf("jenkins = %v", out)
	}
	if hc, _, _ := st.AppHealthCheck(ctx, out["app"].(map[string]any)["id"].(string)); hc.StartPeriod != 180 {
		t.Fatalf("start period = %d", hc.StartPeriod)
	}
}

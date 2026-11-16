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
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"panel/internal/account"
	"panel/internal/auth"
	"panel/internal/brand"
	"panel/internal/database"
	"panel/internal/executor"
	"panel/internal/executor/executortest"
	"panel/internal/secret"
	"panel/internal/store"
)

type databaseTestHost struct{ *executortest.Fake }

func (h databaseTestHost) ExecStream(context.Context, string, executor.Command, io.Reader, io.Writer) error {
	return nil
}

func (h databaseTestHost) Exec(context.Context, string, executor.Command) error { return nil }

// Exercise the real router and auth middleware in memory, without opening a port.
func TestDatabaseAPI(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
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
	owner, err := st.CreateUser(ctx, "owner@example.com", "hash", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(ctx, owner.ID, auth.HashToken("test-session"), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAPIToken(ctx, owner.ID, "api", auth.HashToken("api-token")); err != nil {
		t.Fatal(err)
	}
	p, _ := st.CreateProject(ctx, "shop", "")
	svc := database.New(ctx, st, databaseTestHost{executortest.New()})
	defer func() { cancel(); svc.Wait() }()
	server := New(st, account.New(st), slog.New(slog.NewTextHandler(io.Discard, nil)), "test", fstest.MapFS{"index.html": {Data: []byte("app")}})
	server.SetDatabases(svc)
	handler := server.Handler()
	call := func(method, path string, body any, mode string, want int) *httptest.ResponseRecorder {
		t.Helper()
		var raw []byte
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		if mode == "session" {
			req.AddCookie(&http.Cookie{Name: brand.SessionCookie(), Value: "test-session"})
		}
		if mode == "token" {
			req.Header.Set("Authorization", "Bearer api-token")
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("%s %s = %d, want %d: %s", method, path, rec.Code, want, rec.Body.String())
		}
		return rec
	}
	call("GET", "/api/databases", nil, "", 401)
	call("POST", "/api/databases", nil, "", 401)
	call("GET", "/api/database-operations/active", nil, "", 401)
	call("GET", "/api/database-operations/missing", nil, "", 401)
	call("GET", "/api/database-engines", nil, "session", 200)
	in := map[string]any{"project_id": p.ID, "name": "main", "engine": "postgres", "version": "18", "database_name": "app", "username": "app", "memory_mb": 512}
	in["version"] = "latest"
	call("POST", "/api/databases", in, "session", 422)
	in["version"] = "18"
	rec := call("POST", "/api/databases", in, "session", 202)
	var d databaseJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	svc.Wait()
	if d.Operation == nil || d.Operation.Action != "create" {
		t.Fatal("creation has no tracked operation")
	}
	result := call("GET", "/api/database-operations/"+d.Operation.ID, nil, "session", 200)
	if !strings.Contains(result.Body.String(), `"status":"succeeded"`) {
		t.Fatal("creation operation did not finish")
	}
	if d.ID == "" || d.Host == "" || d.Volume == "" {
		t.Fatalf("create = %+v", d)
	}
	call("GET", "/api/projects/"+p.ID+"/databases", nil, "", 401)
	call("GET", "/api/projects/missing/databases", nil, "session", 404)
	other, err := st.CreateProject(ctx, "other", "")
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := st.CreateDBInstance(ctx, store.DBInstance{ProjectID: other.ID, Name: "main", Engine: "mariadb", Version: "11.8", DatabaseName: "private_database", Username: "private_user"}, map[string]string{"password": "foreign-secret", "root_password": "foreign-root"})
	if err != nil {
		t.Fatal(err)
	}
	emptyProject, err := st.CreateProject(ctx, "empty", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(call("GET", "/api/projects/"+emptyProject.ID+"/databases", nil, "session", 200).Body.String()) != "[]" {
		t.Fatal("empty project does not return an empty list")
	}
	checkProjectDatabases := func(wantNames int) {
		t.Helper()
		body := call("GET", "/api/projects/"+p.ID+"/databases", nil, "session", 200).Body.Bytes()
		var instances []projectDatabaseJSON
		if err := json.Unmarshal(body, &instances); err != nil {
			t.Fatal(err)
		}
		if len(instances) != 1 || instances[0].ID != d.ID || instances[0].ProjectID != p.ID || len(instances[0].DatabaseNames) != wantNames {
			t.Fatalf("project instances = %+v", instances)
		}
		for _, private := range []string{foreign.ID, "private_database", "private_user", "foreign-secret", "foreign-root"} {
			if strings.Contains(string(body), private) {
				t.Fatal("project listing exposed another project's database")
			}
		}
	}
	// The initial database is visible before its access tab seeds any mirror rows.
	checkProjectDatabases(1)
	creds, _ := st.DBInstanceCredentials(ctx, d.ID)
	for _, path := range []string{"/api/databases", "/api/databases/" + d.ID} {
		rec := call("GET", path, nil, "session", 200)
		if strings.Contains(rec.Body.String(), creds["password"]) || strings.Contains(rec.Body.String(), creds["root_password"]) || strings.Contains(rec.Body.String(), "credentials") {
			t.Fatal("ordinary response exposed credentials")
		}
	}
	call("POST", "/api/databases", in, "session", 409)
	call("DELETE", "/api/projects/"+p.ID, nil, "session", 409)
	path := "/api/databases/" + d.ID
	call("GET", path+"/connection", nil, "", 401)
	call("GET", path+"/connection", nil, "token", 403)
	rec = call("GET", path+"/connection", nil, "session", 200)
	if !strings.Contains(rec.Body.String(), creds["password"]) || strings.Contains(rec.Body.String(), creds["root_password"]) || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("connection response is unsafe")
	}
	call("GET", path+"/adminer/", nil, "", 401)
	call("GET", path+"/adminer/", nil, "token", 403)
	call("POST", path+"/adminer/", nil, "", 401)
	call("GET", "/api/databases/missing/adminer/", nil, "session", 404)
	call("GET", path+"/backup-policies", nil, "", 401)
	call("PUT", path+"/backup-policies", map[string]any{}, "token", 403)
	call("GET", path+"/backup-policies", nil, "session", 200)
	scheduleStorage, err := st.CreateS3Connection(ctx, store.S3Input{S3Connection: store.S3Connection{Name: "schedule-store", Endpoint: "s3.example", Bucket: "backups"}, SecretKey: "schedule-secret"})
	if err != nil {
		t.Fatal(err)
	}
	scheduleInput := map[string]any{"database": "app", "s3_connection_id": scheduleStorage.ID, "enabled": true, "frequency": "daily", "hour": 2, "minute": 30, "weekday": 0, "keep_count": 7}
	scheduleResponse := call("PUT", path+"/backup-policies", scheduleInput, "session", 200)
	if strings.Contains(scheduleResponse.Body.String(), "schedule-secret") || !strings.Contains(scheduleResponse.Body.String(), "next_run_at") {
		t.Fatal("unsafe policy response")
	}
	scheduleInput["keep_count"] = 0
	call("PUT", path+"/backup-policies", scheduleInput, "session", 422)
	scheduleInput["keep_count"] = 7
	scheduleInput["frequency"] = "invalid"
	call("PUT", path+"/backup-policies", scheduleInput, "session", 422)
	scheduleInput["frequency"] = "weekly"
	scheduleInput["enabled"] = false
	call("PUT", path+"/backup-policies", scheduleInput, "session", 200)
	policyList, err := st.BackupPolicies(ctx, d.ID)
	if err != nil || len(policyList) != 1 || policyList[0].Enabled || policyList[0].Frequency != "weekly" {
		t.Fatal("policy upsert did not preserve one policy per database", policyList, err)
	}
	call("GET", path+"/backups", nil, "", 401)
	call("POST", path+"/backups", map[string]string{}, "token", 403)
	call("POST", path+"/restore", map[string]string{}, "token", 403)
	call("GET", path+"/backups", nil, "session", 200)
	call("GET", "/api/databases/missing/backups", nil, "session", 404)
	call("POST", path+"/backups", map[string]string{"database": "missing", "s3_connection_id": "missing"}, "session", 404)
	call("POST", path+"/restore", map[string]string{"backup_id": "missing", "name": "restored"}, "session", 404)
	call("GET", path+"/metrics", nil, "", 401)
	call("GET", path+"/metrics?range=2y", nil, "session", 422)
	call("GET", "/api/databases/missing/metrics", nil, "session", 404)
	now := time.Now().Unix()
	if err := st.AddMetricSamples(ctx, now-now%10, []store.MetricSample{{Kind: "database", ResourceID: d.ID, CPU: .25, Memory: 4096}}); err != nil {
		t.Fatal(err)
	}
	var series struct {
		Range  string               `json:"range"`
		Step   int64                `json:"step"`
		Points []store.MetricSample `json:"points"`
	}
	if err := json.Unmarshal(call("GET", path+"/metrics", nil, "token", 200).Body.Bytes(), &series); err != nil || series.Range != "1h" || series.Step != 10 || len(series.Points) != 1 || series.Points[0].CPU != .25 || series.Points[0].Memory != 4096 {
		t.Fatalf("metrics %+v %v", series, err)
	}
	if call("GET", path+"/metrics?range=30d", nil, "session", 200).Body.String() == "" {
		t.Fatal("empty 30d answer")
	}
	orphans := "/api/database-backups/orphaned"
	call("GET", orphans, nil, "", 401)
	if got := call("GET", orphans, nil, "session", 200); strings.TrimSpace(got.Body.String()) != "[]" {
		t.Fatalf("orphaned backups: %s", got.Body.String())
	}
	call("DELETE", orphans+"/missing", nil, "token", 403)
	call("DELETE", orphans+"/missing", nil, "session", 404)
	call("POST", orphans+"/missing/restore", map[string]string{"name": "copy"}, "token", 403)
	call("POST", orphans+"/missing/restore", map[string]string{"name": "copy"}, "session", 404)
	call("GET", path+"/catalog", nil, "", 401)
	call("POST", path+"/users", map[string]string{"name": "reader"}, "token", 403)
	call("POST", path+"/databases", map[string]string{"name": "analytics"}, "session", 202)
	svc.Wait()
	call("POST", path+"/users", map[string]string{"name": "reader"}, "session", 202)
	svc.Wait()
	var catalog store.DatabaseCatalog
	if err := json.Unmarshal(call("GET", path+"/catalog", nil, "session", 200).Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	var userID, databaseID string
	for _, u := range catalog.Users {
		if u.Name == "reader" {
			userID = u.ID
		}
	}
	for _, db := range catalog.Databases {
		if db.Name == "analytics" {
			databaseID = db.ID
		}
	}
	if userID == "" || databaseID == "" {
		t.Fatal("management catalog omitted created resources")
	}
	checkProjectDatabases(2)
	call("PUT", path+"/grants", map[string]string{"user_id": userID, "database_id": databaseID, "access": "read_only"}, "session", 202)
	svc.Wait()
	call("PUT", path+"/grants", map[string]string{"user_id": userID, "database_id": "foreign", "access": "full"}, "session", 404)
	userConnection := path + "/users/" + userID + "/connection?database_id=" + databaseID
	call("GET", userConnection, nil, "token", 403)
	call("GET", path+"/users/"+userID+"/connection?database_id=foreign", nil, "session", 404)
	password, err := st.DatabaseUserPassword(ctx, d.ID, "reader")
	if err != nil {
		t.Fatal(err)
	}
	result = call("GET", userConnection, nil, "session", 200)
	if !strings.Contains(result.Body.String(), password) || strings.Contains(result.Body.String(), creds["root_password"]) || result.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("user credential endpoint is unsafe")
	}
	if strings.Contains(call("GET", path+"/catalog", nil, "session", 200).Body.String(), password) {
		t.Fatal("catalog exposed a password")
	}
	move := map[string]string{"project_id": emptyProject.ID}
	call("PATCH", path, move, "", 401)
	call("PATCH", path, map[string]string{"project_id": ""}, "session", 422)
	call("PATCH", path, map[string]string{"project_id": "missing"}, "session", 404)
	call("PATCH", "/api/databases/missing", move, "session", 404)
	call("PATCH", path, map[string]string{"project_id": other.ID}, "session", 409)
	// A persisted job must block movement, including during panel recovery.
	current, err := st.GetDBInstance(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := st.CreateDBOperationPayload(ctx, current, "grant_update", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	call("PATCH", path, move, "session", 409)
	if err := st.FinishDBOperation(ctx, pending.ID, ""); err != nil {
		t.Fatal(err)
	}
	checkProjectDatabases(2)
	moved := call("PATCH", path, move, "session", 200)
	var updated databaseJSON
	if err := json.Unmarshal(moved.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.ProjectID != emptyProject.ID || updated.Host != d.Host || updated.Volume != d.Volume || updated.Status != current.Status {
		t.Fatalf("move changed instance identity/state: %+v", updated)
	}
	if strings.Contains(moved.Body.String(), creds["password"]) || strings.Contains(moved.Body.String(), password) {
		t.Fatal("move exposed credentials")
	}
	if strings.TrimSpace(call("GET", "/api/projects/"+p.ID+"/databases", nil, "session", 200).Body.String()) != "[]" {
		t.Fatal("moved instance still listed in old project")
	}
	var movedList []projectDatabaseJSON
	if err := json.Unmarshal(call("GET", "/api/projects/"+emptyProject.ID+"/databases", nil, "session", 200).Body.Bytes(), &movedList); err != nil || len(movedList) != 1 || movedList[0].ID != d.ID || len(movedList[0].DatabaseNames) != 2 {
		t.Fatalf("destination listing lost managed databases: %+v, %v", movedList, err)
	}
	if got := call("GET", path+"/catalog", nil, "session", 200).Body.String(); !strings.Contains(got, userID) || !strings.Contains(got, databaseID) {
		t.Fatal("move lost users or databases")
	}
	if got, err := st.DatabaseUserPassword(ctx, d.ID, "reader"); err != nil || got != password {
		t.Fatal("move changed managed credentials", err)
	}
	call("PATCH", path, map[string]string{"project_id": p.ID}, "session", 200)
	checkProjectDatabases(2)
	call("POST", path+"/stop", nil, "session", 202)
	svc.Wait()
	call("GET", path+"/adminer/", nil, "session", 409)
	call("POST", path+"/start", nil, "session", 202)
	svc.Wait()
	call("POST", path+"/restart", nil, "session", 202)
	svc.Wait()
	call("GET", path+"/logs", nil, "session", 200)
	call("POST", path+"/upgrade", nil, "session", 422)
	call("DELETE", path, map[string]string{"name": "wrong"}, "session", 422)
	removed := call("DELETE", path, map[string]string{"name": "main"}, "session", 202)
	var response struct {
		Operation store.DBOperation `json:"operation"`
	}
	if err := json.Unmarshal(removed.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	svc.Wait()
	result = call("GET", "/api/database-operations/"+response.Operation.ID, nil, "session", 200)
	if !strings.Contains(result.Body.String(), `"status":"succeeded"`) {
		t.Fatal("removal operation lost after instance deletion")
	}
	call("GET", path, nil, "session", 404)
	call("DELETE", "/api/projects/"+p.ID, nil, "session", 204)
}

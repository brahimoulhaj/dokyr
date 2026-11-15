package api

import (
	"context"
	"errors"
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
	"panel/internal/store"
)

type appProblemDeployer struct {
	Deployer
	problems []store.AppRuntimeTarget
	err      error
}

func (d *appProblemDeployer) AppProblems(context.Context) ([]store.AppRuntimeTarget, error) {
	return d.problems, d.err
}

func TestAppProblemsAPI(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if _, err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.CreateUser(ctx, "test@example.com", "hash", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(ctx, owner.ID, auth.HashToken("session"), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	server := New(st, account.New(st), slog.New(slog.NewTextHandler(io.Discard, nil)), "test", fstest.MapFS{})
	runtime := &appProblemDeployer{problems: []store.AppRuntimeTarget{{AppID: "app", AppName: "inertia", ProjectID: "project", ProjectName: "Demo", ContainerName: "private-container", RuntimeState: "restarting"}}}
	server.SetDeployer(runtime)
	handler := server.Handler()
	check := func(signedIn bool, status int) string {
		t.Helper()
		req := httptest.NewRequest("GET", "/api/apps/problems", nil)
		if signedIn {
			req.AddCookie(&http.Cookie{Name: brand.SessionCookie(), Value: "session"})
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != status {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}
	check(false, 401)
	body := check(true, 200)
	if !strings.Contains(body, `"runtime_state":"restarting"`) || !strings.Contains(body, `"project_name":"Demo"`) || strings.Contains(body, "private-container") {
		t.Fatal("incorrect app problem response", body)
	}
	runtime.err = errors.New("private Docker diagnostic")
	if strings.Contains(check(true, 503), "private Docker diagnostic") {
		t.Fatal("runtime diagnostics leaked")
	}
	runtime.err, runtime.problems = nil, []store.AppRuntimeTarget{}
	if strings.TrimSpace(check(true, 200)) != "[]" {
		t.Fatal("empty result is not an array")
	}
}

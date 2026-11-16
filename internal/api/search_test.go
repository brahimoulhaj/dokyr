package api

import (
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
	"panel/internal/store"
)

func TestSearchAPI(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if _, err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	server := New(st, account.New(st), slog.New(slog.NewTextHandler(io.Discard, nil)), "test", fstest.MapFS{})
	u, err := st.CreateUser(ctx, "search@example.com", "hash", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(ctx, u.ID, auth.HashToken("search-session"), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	p, err := st.CreateProject(ctx, "Search project", "")
	if err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()
	for _, tc := range []struct {
		path     string
		signedIn bool
		status   int
	}{
		{"/api/search?q=Search", false, 401},
		{"/api/search?q=Search", true, 200},
		{"/api/search?q=missing", true, 200},
		{"/api/search?q=" + strings.Repeat("x", 257), true, 400},
	} {
		r := httptest.NewRequest(http.MethodGet, tc.path, nil)
		if tc.signedIn {
			r.AddCookie(&http.Cookie{Name: brand.SessionCookie(), Value: "search-session"})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
		if tc.status != 200 {
			continue
		}
		var body struct {
			Items     []store.SearchResult `json:"items"`
			Truncated bool                 `json:"truncated"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(tc.path, "missing") {
			if body.Items == nil || len(body.Items) != 0 || body.Truncated {
				t.Fatalf("empty = %s", w.Body.String())
			}
		} else if len(body.Items) != 1 || body.Items[0].ID != p.ID || body.Truncated {
			t.Fatalf("results = %s", w.Body.String())
		}
	}
}

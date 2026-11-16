package api

import (
	"bufio"
	"context"
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

func TestChangeStream(t *testing.T) {
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
	defer server.StopStreams()
	u, err := st.CreateUser(ctx, "changes@example.com", "hash", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(ctx, u.ID, auth.HashToken("changes-session"), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	res, err := http.Get(ts.URL + "/api/changes")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("signed out: %d", res.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/changes", nil)
	req.AddCookie(&http.Cookie{Name: brand.SessionCookie(), Value: "changes-session"})
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("stream: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	lines := make(chan string)
	go func() {
		scanner := bufio.NewScanner(res.Body)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()

	// Two writes close together arrive as one event.
	p, err := st.CreateProject(ctx, "Shop", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateApp(ctx, p.ID, store.AppInput{Name: "web", Image: "nginx", Port: 80}); err != nil {
		t.Fatal(err)
	}
	var got []string
	timeout := time.After(3 * time.Second)
	for len(got) < 2 {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatalf("stream closed after %v", got)
			}
			if line != "" && !strings.HasPrefix(line, ":") { // comments: the opening line, pings
				got = append(got, line)
			}
		case <-timeout:
			t.Fatalf("no change event, got %v", got)
		}
	}
	if got[0] != "event: change" || got[1] != `data: {"tables":["apps","projects"]}` {
		t.Fatalf("event = %v", got)
	}
}

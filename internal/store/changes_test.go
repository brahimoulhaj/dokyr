package store

import (
	"context"
	"slices"
	"testing"
	"time"
)

func TestChanges(t *testing.T) {
	s, ctx := open(t), context.Background()
	sub, stop := s.Changes().Subscribe()
	defer stop()
	wait := func(want ...string) {
		t.Helper()
		select {
		case <-sub.Ready:
		case <-time.After(2 * time.Second):
			t.Fatalf("no change announced, want %v", want)
		}
		got := sub.Drain()
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Fatalf("changed = %v, want %v", got, want)
		}
	}
	quiet := func() {
		t.Helper()
		select {
		case <-sub.Ready:
			t.Fatalf("unexpected change: %v", sub.Drain())
		case <-time.After(50 * time.Millisecond):
		}
	}

	p, err := s.CreateProject(ctx, "Shop", "")
	if err != nil {
		t.Fatal(err)
	}
	wait("projects")
	if _, err := s.CreateApp(ctx, p.ID, AppInput{Name: "web", Image: "nginx", Port: 80}); err != nil {
		t.Fatal(err)
	}
	wait("apps")

	// Several writes in one transaction arrive together, once committed.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE projects SET description = 'a' WHERE id = ?`, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE apps SET name = 'api' WHERE project_id = ?`, p.ID); err != nil {
		t.Fatal(err)
	}
	quiet()
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	wait("apps", "projects")

	// A rolled-back write announces nothing.
	tx, err = s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE projects SET description = 'b' WHERE id = ?`, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	quiet()

	// Session changes update the Security list; normal activity is throttled.
	user, err := s.CreateUser(ctx, "sessions@example.com", "hash", "owner")
	if err != nil {
		t.Fatal(err)
	}
	quiet()
	if err := s.CreateSession(ctx, user.ID, "test-session", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	wait("sessions")
	if err := s.TouchSession(ctx, "test-session", SessionDetails{}); err != nil {
		t.Fatal(err)
	}
	quiet()
	if err := s.DeleteUserSessions(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	wait("sessions")

	stop()
	if _, err := s.CreateProject(ctx, "After", ""); err != nil {
		t.Fatal(err)
	}
	quiet()
}

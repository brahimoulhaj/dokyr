package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionsSurviveDetailsMigration(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "old.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.migrate(ctx, 34); err != nil {
		t.Fatal(err)
	}
	user, err := s.CreateUser(ctx, "owner@example.com", "hash", "owner")
	if err != nil {
		t.Fatal(err)
	}
	ts := now()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO sessions (id, user_id, token_hash, expires_at, created_at, updated_at) VALUES ('old-session', ?, 'legacy-hash', ?, ?, ?)`, user.ID, fmtTime(time.Now().Add(time.Hour)), ts, ts); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionUser(ctx, "legacy-hash"); err != nil {
		t.Fatal("migration signed out a session", err)
	}
	sessions, err := s.UserSessions(ctx, user.ID, "legacy-hash")
	if err != nil || len(sessions) != 1 || !sessions[0].Current || sessions[0].LastUsedAt.IsZero() {
		t.Fatalf("legacy list = %+v %v", sessions, err)
	}
	meta := SessionDetails{IPAddress: "198.51.100.42", UserAgent: "Test browser"}
	if err := s.TouchSession(ctx, "legacy-hash", meta); err != nil {
		t.Fatal(err)
	}
	sessions, _ = s.UserSessions(ctx, user.ID, "legacy-hash")
	if sessions[0].IPAddress != meta.IPAddress || sessions[0].UserAgent != meta.UserAgent {
		t.Fatalf("legacy metadata not filled: %+v", sessions[0])
	}
	if err := s.TouchSession(ctx, "legacy-hash", SessionDetails{IPAddress: "192.0.2.50", UserAgent: "Changed immediately"}); err != nil {
		t.Fatal(err)
	}
	sessions, _ = s.UserSessions(ctx, user.ID, "legacy-hash")
	if sessions[0].IPAddress != meta.IPAddress {
		t.Fatal("session activity is not throttled")
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE sessions SET last_used_at = ? WHERE id = 'old-session'`, fmtTime(time.Now().Add(-2*time.Minute))); err != nil {
		t.Fatal(err)
	}
	if err := s.TouchSession(ctx, "legacy-hash", SessionDetails{IPAddress: "192.0.2.50"}); err != nil {
		t.Fatal(err)
	}
	sessions, _ = s.UserSessions(ctx, user.ID, "legacy-hash")
	if sessions[0].IPAddress != "192.0.2.50" {
		t.Fatal("session activity was not refreshed after the throttle window")
	}
}

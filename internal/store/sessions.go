package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"panel/internal/id"
)

type SessionDetails struct {
	IPAddress string
	UserAgent string
}

type Session struct {
	ID         string    `json:"id"`
	IPAddress  string    `json:"ip_address"`
	UserAgent  string    `json:"user_agent"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Current    bool      `json:"current"`
}

func (s *Store) CreateSession(ctx context.Context, userID, tokenHash string, expiresAt time.Time, details ...SessionDetails) error {
	ts := now()
	var meta SessionDetails
	if len(details) > 0 {
		meta = details[0]
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, user_id, token_hash, expires_at, created_at, updated_at, ip_address, user_agent, last_used_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id.New(), userID, tokenHash, fmtTime(expiresAt), ts, ts, meta.IPAddress, meta.UserAgent, ts)
	return err
}

// Only opaque row IDs are returned. The browser never receives session tokens or hashes.
func (s *Store) UserSessions(ctx context.Context, userID, currentHash string) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, ip_address, user_agent, created_at, COALESCE(last_used_at, created_at), expires_at, token_hash = ? FROM sessions WHERE user_id = ? AND expires_at > ? ORDER BY (token_hash = ?) DESC, COALESCE(last_used_at, created_at) DESC, id DESC`, currentHash, userID, now(), currentHash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		var session Session
		var created, used, expires string
		if err := rows.Scan(&session.ID, &session.IPAddress, &session.UserAgent, &created, &used, &expires, &session.Current); err != nil {
			return nil, err
		}
		session.CreatedAt = parseTime(created)
		session.LastUsedAt = parseTime(used)
		session.ExpiresAt = parseTime(expires)
		out = append(out, session)
	}
	return out, rows.Err()
}

// TouchSession records browser activity at most once per minute, including filling
// metadata for sessions retained across the migration.
func (s *Store) TouchSession(ctx context.Context, tokenHash string, meta SessionDetails) error {
	ts := now()
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET ip_address = ?, user_agent = ?, last_used_at = ?, updated_at = ? WHERE token_hash = ? AND expires_at > ? AND (last_used_at IS NULL OR last_used_at < ?)`, meta.IPAddress, meta.UserAgent, ts, ts, tokenHash, ts, fmtTime(time.Now().Add(-time.Minute)))
	return err
}

func (s *Store) IsCurrentSession(ctx context.Context, userID, sessionID, currentHash string) (bool, error) {
	var current bool
	err := s.db.QueryRowContext(ctx, `SELECT token_hash = ? FROM sessions WHERE id = ? AND user_id = ? AND expires_at > ?`, currentHash, sessionID, userID, now()).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	return current, err
}

func (s *Store) DeleteSessionByID(ctx context.Context, userID, sessionID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ? AND user_id = ? AND expires_at > ?`, sessionID, userID, now())
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}

func (s *Store) DeleteOtherSessions(ctx context.Context, userID, currentHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND token_hash <> ?`, userID, currentHash)
	return err
}

// SessionUser returns the user owning an unexpired session, or ErrNotFound.
func (s *Store) SessionUser(ctx context.Context, tokenHash string) (User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `
		SELECT u.id, u.email, u.password_hash, u.role, u.created_at, u.updated_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ?`, tokenHash, now()))
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

// DeleteUserSessions signs a user out everywhere (used after a password reset).
func (s *Store) DeleteUserSessions(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}

func (s *Store) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

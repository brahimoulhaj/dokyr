package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"panel/internal/id"
)

// Redirect sends every request for a hostname to another hostname, keeping the path and
// the query: "www.example.com" to "example.com". The hostname serves nothing else.
type Redirect struct {
	ID        string
	Hostname  string
	Target    string // the hostname requests are sent to, always over HTTPS
	TLSMode   string // how Hostname itself is served: TLSAuto or TLSHTTPOnly
	CreatedAt time.Time
}

const redirectSelect = `SELECT id, hostname, target, tls_mode, created_at FROM redirects`

func scanRedirect(row scanner) (Redirect, error) {
	var r Redirect
	var created string
	err := row.Scan(&r.ID, &r.Hostname, &r.Target, &r.TLSMode, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Redirect{}, ErrNotFound
	}
	r.CreatedAt = parseTime(created)
	return r, err
}

// ListRedirects lists every redirect, by hostname.
func (s *Store) ListRedirects(ctx context.Context) ([]Redirect, error) {
	rows, err := s.db.QueryContext(ctx, redirectSelect+` ORDER BY hostname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Redirect{}
	for rows.Next() {
		r, err := scanRedirect(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CreateRedirect adds a redirect; ErrConflict when the hostname already redirects.
func (s *Store) CreateRedirect(ctx context.Context, hostname, target, tlsMode string) (Redirect, error) {
	if tlsMode == "" {
		tlsMode = TLSAuto
	}
	rid, ts := id.New(), now()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO redirects (id, hostname, target, tls_mode, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		rid, hostname, target, tlsMode, ts, ts); err != nil {
		return Redirect{}, conflict(err)
	}
	return s.GetRedirect(ctx, rid)
}

func (s *Store) GetRedirect(ctx context.Context, redirectID string) (Redirect, error) {
	return scanRedirect(s.db.QueryRowContext(ctx, redirectSelect+` WHERE id = ?`, redirectID))
}

// HostRedirect returns the redirect of a hostname; ErrNotFound when it has none.
func (s *Store) HostRedirect(ctx context.Context, hostname string) (Redirect, error) {
	return scanRedirect(s.db.QueryRowContext(ctx, redirectSelect+` WHERE hostname = ?`, hostname))
}

func (s *Store) DeleteRedirect(ctx context.Context, redirectID string) error {
	return affected(s.db.ExecContext(ctx, `DELETE FROM redirects WHERE id = ?`, redirectID))
}

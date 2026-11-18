package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"panel/internal/id"
)

type User struct {
	ID           string
	Email        string
	PasswordHash string
	Role         string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

const userCols = `id, email, password_hash, role, created_at, updated_at`

func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var u User
	var created, updated string
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	u.CreatedAt, u.UpdatedAt = parseTime(created), parseTime(updated)
	return u, err
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(ctx context.Context, email, passwordHash, role string) (User, error) {
	u := User{ID: id.New(), Email: email, PasswordHash: passwordHash, Role: role}
	ts := now()
	u.CreatedAt, u.UpdatedAt = parseTime(ts), parseTime(ts)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users (`+userCols+`) VALUES (?, ?, ?, ?, ?, ?)`,
		u.ID, u.Email, u.PasswordHash, u.Role, ts, ts)
	return u, err
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE email = ?`, email))
}

func (s *Store) GetUserByID(ctx context.Context, userID string) (User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, userID))
}

// FirstOwner returns the earliest-created owner.
func (s *Store) FirstOwner(ctx context.Context) (User, error) {
	return scanUser(s.db.QueryRowContext(ctx,
		`SELECT `+userCols+` FROM users WHERE role = 'owner' ORDER BY id LIMIT 1`))
}

func (s *Store) UpdateUserPassword(ctx context.Context, userID, passwordHash string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`, passwordHash, now(), userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListUsers returns every user, oldest first.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+userCols+` FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) UpdateUserRole(ctx context.Context, userID, role string) error {
	return affected(s.db.ExecContext(ctx, `UPDATE users SET role = ?, updated_at = ? WHERE id = ?`, role, now(), userID))
}

// DeleteUser removes a user; their sessions, API tokens and login methods go with them.
func (s *Store) DeleteUser(ctx context.Context, userID string) error {
	return affected(s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, userID))
}

// Invite is a pending invitation to join the panel with a role.
type Invite struct {
	ID        string
	Email     string
	Role      string
	ExpiresAt time.Time
	CreatedAt time.Time
}

const inviteCols = `id, email, role, expires_at, created_at`

func scanInvite(row interface{ Scan(...any) error }) (Invite, error) {
	var i Invite
	var expires, created string
	err := row.Scan(&i.ID, &i.Email, &i.Role, &expires, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Invite{}, ErrNotFound
	}
	i.ExpiresAt, i.CreatedAt = parseTime(expires), parseTime(created)
	return i, err
}

// SaveInvite stores an invite for the email, replacing an earlier one (and its link).
func (s *Store) SaveInvite(ctx context.Context, email, role, tokenHash, invitedBy string, expiresAt time.Time) (Invite, error) {
	ts := now()
	return scanInvite(s.db.QueryRowContext(ctx, `
		INSERT INTO user_invites (id, email, role, token_hash, invited_by, expires_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?)
		ON CONFLICT(email) DO UPDATE SET role = excluded.role, token_hash = excluded.token_hash,
			invited_by = excluded.invited_by, expires_at = excluded.expires_at, updated_at = excluded.updated_at
		RETURNING `+inviteCols,
		id.New(), email, role, tokenHash, invitedBy, fmtTime(expiresAt), ts, ts))
}

// ListInvites returns pending invites, newest first; expired ones are included.
func (s *Store) ListInvites(ctx context.Context) ([]Invite, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+inviteCols+` FROM user_invites ORDER BY updated_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invite{}
	for rows.Next() {
		i, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// InviteByToken returns the unexpired invite with this token hash, or ErrNotFound.
func (s *Store) InviteByToken(ctx context.Context, tokenHash string) (Invite, error) {
	return scanInvite(s.db.QueryRowContext(ctx,
		`SELECT `+inviteCols+` FROM user_invites WHERE token_hash = ? AND expires_at > ?`, tokenHash, now()))
}

// DeleteInvite revokes an invite and returns it.
func (s *Store) DeleteInvite(ctx context.Context, inviteID string) (Invite, error) {
	return scanInvite(s.db.QueryRowContext(ctx, `DELETE FROM user_invites WHERE id = ? RETURNING `+inviteCols, inviteID))
}

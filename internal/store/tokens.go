package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"panel/internal/id"
	"panel/internal/store/sqlcgen"
)

// APIToken is a long-lived bearer credential. Only the token's hash is stored.
type APIToken struct {
	ID         string
	UserID     string
	Name       string
	LastUsedAt *time.Time
	CreatedAt  time.Time
}

func (s *Store) CreateAPIToken(ctx context.Context, userID, name, tokenHash string) (APIToken, error) {
	t := APIToken{ID: id.New(), UserID: userID, Name: name}
	ts := now()
	t.CreatedAt = parseTime(ts)
	err := s.q.CreateAPIToken(ctx, sqlcgen.CreateAPITokenParams{ID: t.ID, UserID: userID, Name: name, TokenHash: tokenHash, CreatedAt: ts, UpdatedAt: ts})
	return t, err
}

// ListAPITokens returns a user's tokens, newest first.
func (s *Store) ListAPITokens(ctx context.Context, userID string) ([]APIToken, error) {
	rows, err := s.q.ListAPITokens(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]APIToken, 0, len(rows))
	for _, r := range rows {
		out = append(out, apiToken(r.ID, r.UserID, r.Name, r.LastUsedAt, r.CreatedAt))
	}
	return out, nil
}

// DeleteAPIToken revokes one of a user's tokens; ErrNotFound when the user has no such token.
func (s *Store) DeleteAPIToken(ctx context.Context, userID, tokenID string) (APIToken, error) {
	r, err := s.q.DeleteAPIToken(ctx, sqlcgen.DeleteAPITokenParams{ID: tokenID, UserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return APIToken{}, ErrNotFound
	}
	if err != nil {
		return APIToken{}, err
	}
	return apiToken(r.ID, r.UserID, r.Name, r.LastUsedAt, r.CreatedAt), nil
}

// APITokenUser returns the token with this hash and its owner, or ErrNotFound.
func (s *Store) APITokenUser(ctx context.Context, tokenHash string) (APIToken, User, error) {
	r, err := s.q.APITokenUser(ctx, tokenHash)
	if errors.Is(err, sql.ErrNoRows) {
		return APIToken{}, User{}, ErrNotFound
	}
	if err != nil {
		return APIToken{}, User{}, err
	}
	u := User{ID: r.OwnerID, Email: r.Email, PasswordHash: r.PasswordHash, Role: r.Role,
		CreatedAt: parseTime(r.OwnerCreatedAt), UpdatedAt: parseTime(r.OwnerUpdatedAt)}
	return apiToken(r.ID, r.UserID, r.Name, r.LastUsedAt, r.CreatedAt), u, nil
}

// TouchAPIToken records a use, at most once a minute per token so busy clients do not
// turn every request into a write.
func (s *Store) TouchAPIToken(ctx context.Context, tokenID string) error {
	return s.q.TouchAPIToken(ctx, sqlcgen.TouchAPITokenParams{
		Now: sql.NullString{String: now(), Valid: true}, ID: tokenID,
		Before: sql.NullString{String: fmtTime(time.Now().Add(-time.Minute)), Valid: true},
	})
}

func apiToken(id, userID, name string, lastUsed sql.NullString, created string) APIToken {
	t := APIToken{ID: id, UserID: userID, Name: name, CreatedAt: parseTime(created)}
	if lastUsed.Valid {
		lu := parseTime(lastUsed.String)
		t.LastUsedAt = &lu
	}
	return t
}

// DeleteUserAPITokens revokes every API token of a user.
func (s *Store) DeleteUserAPITokens(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM api_tokens WHERE user_id = ?`, userID)
	return err
}

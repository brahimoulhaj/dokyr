package store

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"panel/internal/id"
)

// RegistrySettings configure the built-in registry. It is off until enabled, and needs a
// domain of its own: docker login and image names use it.
type RegistrySettings struct {
	Enabled    bool   `json:"enabled"`
	Domain     string `json:"domain"`
	BuildCache bool   `json:"build_cache"` // push every Git build to the registry too
}

// GetRegistrySettings returns the saved settings; the zero value (off) when never saved.
func (s *Store) GetRegistrySettings(ctx context.Context) (RegistrySettings, error) {
	var out RegistrySettings
	v, ok, err := s.GetSetting(ctx, SettingRegistry)
	if err != nil || !ok {
		return out, err
	}
	return out, json.Unmarshal([]byte(v), &out)
}

func (s *Store) SetRegistrySettings(ctx context.Context, rs RegistrySettings) error {
	raw, err := json.Marshal(rs)
	if err != nil {
		return err
	}
	return s.SetSetting(ctx, SettingRegistry, string(raw))
}

// Scopes of a registry token.
const (
	RegistryPull = "pull"
	RegistryPush = "push" // push and pull
)

// RegistryToken is a docker login for the built-in registry, without its secret.
type RegistryToken struct {
	ID         string
	Name       string
	Scope      string
	LastUsedAt time.Time // zero: never used
	CreatedAt  time.Time
}

const registryTokenCols = `id, name, scope, COALESCE(last_used_at, ''), created_at`

func scanRegistryToken(row scanner) (RegistryToken, error) {
	var t RegistryToken
	var used, created string
	err := row.Scan(&t.ID, &t.Name, &t.Scope, &used, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return RegistryToken{}, ErrNotFound
	}
	t.LastUsedAt, t.CreatedAt = parseTime(used), parseTime(created)
	return t, err
}

// CreateRegistryToken stores a token by its hash; ErrConflict when the name is taken.
func (s *Store) CreateRegistryToken(ctx context.Context, name, scope, token string) (RegistryToken, error) {
	tid, ts := id.New(), now()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO registry_tokens (id, name, token_hash, scope, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		tid, name, hookHash(token), scope, ts, ts)
	if err != nil {
		return RegistryToken{}, conflict(err)
	}
	return scanRegistryToken(s.db.QueryRowContext(ctx, `SELECT `+registryTokenCols+` FROM registry_tokens WHERE id = ?`, tid))
}

func (s *Store) ListRegistryTokens(ctx context.Context) ([]RegistryToken, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+registryTokenCols+` FROM registry_tokens ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RegistryToken{}
	for rows.Next() {
		t, err := scanRegistryToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) DeleteRegistryToken(ctx context.Context, tokenID string) error {
	return affected(s.db.ExecContext(ctx, `DELETE FROM registry_tokens WHERE id = ?`, tokenID))
}

// CheckRegistryToken returns the token named name when token is its secret, and records
// the use; ErrNotFound otherwise.
func (s *Store) CheckRegistryToken(ctx context.Context, name, token string) (RegistryToken, error) {
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT token_hash FROM registry_tokens WHERE name = ?`, name).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && subtle.ConstantTimeCompare([]byte(hash), []byte(hookHash(token))) != 1) {
		return RegistryToken{}, ErrNotFound
	}
	if err != nil {
		return RegistryToken{}, err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE registry_tokens SET last_used_at = ? WHERE name = ?`, now(), name); err != nil {
		return RegistryToken{}, err
	}
	return scanRegistryToken(s.db.QueryRowContext(ctx, `SELECT `+registryTokenCols+` FROM registry_tokens WHERE name = ?`, name))
}

// SetAppPushDeploy turns deploy-on-push on or off for an image app on the built-in registry.
func (s *Store) SetAppPushDeploy(ctx context.Context, appID string, on bool) error {
	return affected(s.db.ExecContext(ctx, `UPDATE apps SET push_deploy = ?, updated_at = ? WHERE id = ?`, on, now(), appID))
}

// AppsByImage lists the image apps whose image is exactly image.
func (s *Store) AppsByImage(ctx context.Context, image string) ([]App, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+appCols+` FROM apps WHERE source_type = ? AND image = ? ORDER BY id`, SourceImage, image)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []App{}
	for rows.Next() {
		a, err := s.scanApp(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

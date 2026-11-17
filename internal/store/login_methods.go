package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"panel/internal/id"
)

type Passkey struct {
	ID           string     `json:"id"`
	UserID       string     `json:"-"`
	RPID         string     `json:"rp_id"`
	CredentialID string     `json:"-"`
	Name         string     `json:"name"`
	Credential   []byte     `json:"-"`
	CreatedAt    time.Time  `json:"created_at"`
	LastUsedAt   *time.Time `json:"last_used_at"`
}

func (s *Store) Passkeys(ctx context.Context, userID, rpID string) ([]Passkey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, user_id, rp_id, credential_id, name, credential, created_at, last_used_at FROM passkeys WHERE user_id = ? AND (? = '' OR rp_id = ?) ORDER BY created_at, id`, userID, rpID, rpID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Passkey{}
	for rows.Next() {
		var p Passkey
		var created string
		var used sql.NullString
		if err := rows.Scan(&p.ID, &p.UserID, &p.RPID, &p.CredentialID, &p.Name, &p.Credential, &created, &used); err != nil {
			return nil, err
		}
		p.CreatedAt = parseTime(created)
		if used.Valid {
			t := parseTime(used.String)
			p.LastUsedAt = &t
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) AddPasskey(ctx context.Context, p Passkey) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO passkeys (id, user_id, rp_id, credential_id, name, credential, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, id.New(), p.UserID, p.RPID, p.CredentialID, p.Name, p.Credential, now())
	return err
}

func (s *Store) UsePasskey(ctx context.Context, userID, rpID, credentialID string, credential []byte) error {
	res, err := s.db.ExecContext(ctx, `UPDATE passkeys SET credential = ?, last_used_at = ? WHERE user_id = ? AND rp_id = ? AND credential_id = ?`, credential, now(), userID, rpID, credentialID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}

func (s *Store) DeletePasskey(ctx context.Context, userID, keyID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM passkeys WHERE id = ? AND user_id = ?`, keyID, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}

type LoginIdentity struct {
	Subject   string    `json:"-"`
	Login     string    `json:"login"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) GitHubIdentity(ctx context.Context, userID string) (*LoginIdentity, error) {
	var i LoginIdentity
	var created string
	err := s.db.QueryRowContext(ctx, `SELECT subject, login, created_at FROM login_identities WHERE user_id = ? AND provider = 'github'`, userID).Scan(&i.Subject, &i.Login, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	i.CreatedAt = parseTime(created)
	return &i, err
}

func (s *Store) GitHubUser(ctx context.Context, subject string) (User, error) {
	var userID string
	if err := s.db.QueryRowContext(ctx, `SELECT user_id FROM login_identities WHERE provider = 'github' AND subject = ?`, subject).Scan(&userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, err
	}
	return s.GetUserByID(ctx, userID)
}

func (s *Store) LinkGitHub(ctx context.Context, userID, subject, login string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO login_identities (user_id, provider, subject, login, created_at) VALUES (?, 'github', ?, ?, ?)`, userID, subject, login, now())
	return err
}

func (s *Store) UnlinkGitHub(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM login_identities WHERE user_id = ? AND provider = 'github'`, userID)
	return err
}

type GitHubLoginConfig struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

func (s *Store) GitHubLoginConfig(ctx context.Context) (GitHubLoginConfig, error) {
	value, exists, err := s.GetSetting(ctx, "github_login")
	if err != nil || !exists {
		return GitHubLoginConfig{}, err
	}
	env, err := s.openEnvironment(value)
	return GitHubLoginConfig{ClientID: env["client_id"], ClientSecret: env["client_secret"]}, err
}

// Config uses the same encrypted-at-rest format as application secrets.
func (s *Store) SetGitHubLoginConfig(ctx context.Context, cfg GitHubLoginConfig) error {
	value, err := s.sealEnvironment(map[string]string{"client_id": cfg.ClientID, "client_secret": cfg.ClientSecret})
	if err != nil {
		return err
	}
	// Changing providers invalidates existing links; IDs are scoped to this configuration.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM login_identities WHERE provider = 'github'`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO settings (id, key, value, created_at, updated_at) VALUES (?, 'github_login', ?, ?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, id.New(), value, now(), now()); err != nil {
		return err
	}
	return tx.Commit()
}

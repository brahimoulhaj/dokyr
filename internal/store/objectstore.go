package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"panel/internal/id"
)

const settingObjectStore = "object_store"

// ObjectStoreSettings configure the built-in S3 storage (RustFS). Off until enabled. The
// root credentials are made once and kept when it is turned off, so the data stays
// reachable; SecretKey is sealed with the panel key at rest.
type ObjectStoreSettings struct {
	Enabled bool   `json:"enabled"`
	Domain  string `json:"domain"`   // optional: serves the S3 API outside the server, through the proxy
	TLSMode string `json:"tls_mode"` // TLSAuto ("" too) or TLSHTTPOnly
	// ConnectionID is the S3 connection the panel made for it (backups, the registry).
	ConnectionID string `json:"connection_id"`
	AccessKey    string `json:"access_key"`
	SecretKey    string `json:"-"`
}

type storedObjectStore struct {
	ObjectStoreSettings
	SealedSecret string `json:"secret_key"`
}

// GetObjectStoreSettings returns the saved settings; the zero value (off) when never saved.
func (s *Store) GetObjectStoreSettings(ctx context.Context) (ObjectStoreSettings, error) {
	raw, ok, err := s.GetSetting(ctx, settingObjectStore)
	if err != nil || !ok {
		return ObjectStoreSettings{}, err
	}
	var st storedObjectStore
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		return ObjectStoreSettings{}, err
	}
	out := st.ObjectStoreSettings
	if st.SealedSecret != "" {
		if s.box == nil {
			return out, errors.New("object storage: no encryption key loaded")
		}
		plain, err := s.box.Decrypt(st.SealedSecret)
		if err != nil {
			return out, err
		}
		out.SecretKey = string(plain)
	}
	return out, nil
}

func (s *Store) SetObjectStoreSettings(ctx context.Context, in ObjectStoreSettings) error {
	st := storedObjectStore{ObjectStoreSettings: in}
	if in.SecretKey != "" {
		if s.box == nil {
			return errors.New("object storage: no encryption key loaded")
		}
		sealed, err := s.box.Encrypt([]byte(in.SecretKey))
		if err != nil {
			return err
		}
		st.SealedSecret = sealed
	}
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return s.SetSetting(ctx, settingObjectStore, string(raw))
}

// Permissions of an access key of the built-in storage.
const (
	S3KeyRead  = "read"
	S3KeyWrite = "write" // read, write and delete objects
)

// S3AccessKey is an access key of the built-in storage limited to one bucket, without its
// secret, which is never stored.
type S3AccessKey struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Bucket     string    `json:"bucket"`
	AccessKey  string    `json:"access_key"`
	Permission string    `json:"permission"`
	CreatedAt  time.Time `json:"created_at"`
}

const s3KeyCols = `id, name, bucket, access_key, permission, created_at`

func scanS3Key(row scanner) (S3AccessKey, error) {
	var k S3AccessKey
	var created string
	err := row.Scan(&k.ID, &k.Name, &k.Bucket, &k.AccessKey, &k.Permission, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return S3AccessKey{}, ErrNotFound
	}
	k.CreatedAt = parseTime(created)
	return k, err
}

func (s *Store) CreateS3AccessKey(ctx context.Context, k S3AccessKey) (S3AccessKey, error) {
	kid, ts := id.New(), now()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO s3_access_keys (id, name, bucket, access_key, permission, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		kid, k.Name, k.Bucket, k.AccessKey, k.Permission, ts, ts)
	if err != nil {
		return S3AccessKey{}, conflict(err)
	}
	return s.GetS3AccessKey(ctx, kid)
}

func (s *Store) GetS3AccessKey(ctx context.Context, keyID string) (S3AccessKey, error) {
	return scanS3Key(s.db.QueryRowContext(ctx, `SELECT `+s3KeyCols+` FROM s3_access_keys WHERE id = ?`, keyID))
}

// ListS3AccessKeys lists the keys, by bucket then name; bucket "" lists all of them.
func (s *Store) ListS3AccessKeys(ctx context.Context, bucket string) ([]S3AccessKey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+s3KeyCols+` FROM s3_access_keys WHERE ? = '' OR bucket = ? ORDER BY bucket, name, id`, bucket, bucket)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []S3AccessKey{}
	for rows.Next() {
		k, err := scanS3Key(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) DeleteS3AccessKey(ctx context.Context, keyID string) error {
	return affected(s.db.ExecContext(ctx, `DELETE FROM s3_access_keys WHERE id = ?`, keyID))
}

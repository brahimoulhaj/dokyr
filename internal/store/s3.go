package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"panel/internal/id"
	"panel/internal/secret"
)

// S3Connection is an S3-compatible bucket the panel stores data in, without its secret key.
type S3Connection struct {
	ID        string
	Name      string
	Endpoint  string // host[:port], no scheme
	Region    string // "" lets the client find it
	Bucket    string
	Prefix    string // folder inside the bucket; "" = the root
	AccessKey string
	UseSSL    bool // https to the endpoint
	PathStyle bool // bucket in the path, not the host name (MinIO, most self-hosted)
	CreatedAt time.Time
	UpdatedAt time.Time
}

// S3Input is what creating a connection takes; SecretKey in clear.
type S3Input struct {
	S3Connection
	SecretKey string
}

const s3Cols = `id, name, endpoint, region, bucket, prefix, access_key, use_ssl, path_style, created_at, updated_at`

func scanS3(row scanner) (S3Connection, error) {
	var c S3Connection
	var created, updated string
	err := row.Scan(&c.ID, &c.Name, &c.Endpoint, &c.Region, &c.Bucket, &c.Prefix, &c.AccessKey, &c.UseSSL, &c.PathStyle, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return S3Connection{}, ErrNotFound
	}
	c.CreatedAt, c.UpdatedAt = parseTime(created), parseTime(updated)
	return c, err
}

// CreateS3Connection stores a connection; the secret key is encrypted with the panel key.
func (s *Store) CreateS3Connection(ctx context.Context, in S3Input) (S3Connection, error) {
	sealed := in.SecretKey
	if s.box != nil {
		var err error
		if sealed, err = s.box.Encrypt([]byte(in.SecretKey)); err != nil {
			return S3Connection{}, err
		}
	}
	cid, ts := id.New(), now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO s3_connections
		(id, name, endpoint, region, bucket, prefix, access_key, secret_key, use_ssl, path_style, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cid, in.Name, in.Endpoint, in.Region, in.Bucket, in.Prefix, in.AccessKey, sealed, in.UseSSL, in.PathStyle, ts, ts)
	if err != nil {
		return S3Connection{}, conflict(err)
	}
	return s.GetS3Connection(ctx, cid)
}

func (s *Store) GetS3Connection(ctx context.Context, connID string) (S3Connection, error) {
	return scanS3(s.db.QueryRowContext(ctx, `SELECT `+s3Cols+` FROM s3_connections WHERE id = ?`, connID))
}

func (s *Store) ListS3Connections(ctx context.Context) ([]S3Connection, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+s3Cols+` FROM s3_connections ORDER BY name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []S3Connection{}
	for rows.Next() {
		c, err := scanS3(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// S3Secret returns a connection with its decrypted secret key.
func (s *Store) S3Secret(ctx context.Context, connID string) (S3Input, error) {
	c, err := s.GetS3Connection(ctx, connID)
	if err != nil {
		return S3Input{}, err
	}
	var stored string
	if err := s.db.QueryRowContext(ctx, `SELECT secret_key FROM s3_connections WHERE id = ?`, connID).Scan(&stored); err != nil {
		return S3Input{}, err
	}
	in := S3Input{S3Connection: c, SecretKey: stored}
	if secret.IsSealed(stored) {
		if s.box == nil {
			return in, errors.New("S3 secret key is encrypted, and the store has no secret key")
		}
		plain, err := s.box.Decrypt(stored)
		if err != nil {
			return in, err
		}
		in.SecretKey = string(plain)
	}
	return in, nil
}

// DeleteS3Connection refuses with ErrConflict while the registry stores its images there.
func (s *Store) DeleteS3Connection(ctx context.Context, connID string) error {
	if _, err := s.GetS3Connection(ctx, connID); err != nil {
		return err
	}
	if v, _, err := s.GetSetting(ctx, SettingRegistryS3); err != nil {
		return err
	} else if v == connID {
		return ErrConflict
	}
	var references int
	if err := s.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM db_backups WHERE s3_connection_id=?) + (SELECT COUNT(*) FROM backup_policies WHERE s3_connection_id=?)`, connID, connID).Scan(&references); err != nil {
		return err
	}
	if references > 0 {
		return ErrConflict
	}
	return affected(s.db.ExecContext(ctx, `DELETE FROM s3_connections WHERE id = ?`, connID))
}

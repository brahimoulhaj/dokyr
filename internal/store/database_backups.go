package store

import (
	"context"
	"encoding/json"
	"time"
)

type DBBackup struct {
	PolicyID       string    `json:"policy_id"`
	ID             string    `json:"id"`
	InstanceID     string    `json:"instance_id"`
	DatabaseName   string    `json:"database_name"`
	S3ConnectionID string    `json:"s3_connection_id"`
	ObjectKey      string    `json:"object_key"`
	SizeBytes      int64     `json:"size_bytes"`
	Ready          bool      `json:"ready"`
	CreatedAt      time.Time `json:"created_at"`
	Status         string    `json:"status"`
	Error          string    `json:"error"`
	// Scope is "database" (one dump) or "instance" (a dump per database and a
	// manifest; ObjectKey is the manifest). Files lists every object written.
	Scope string   `json:"scope"`
	Files []string `json:"-"`
}

func (s *Store) AddDBBackup(ctx context.Context, b DBBackup) error {
	scope := b.Scope
	if scope == "" {
		scope = "database"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO db_backups(id,instance_id,database_name,s3_connection_id,object_key,created_at,policy_id,scope) VALUES(?,?,?,?,?,?,NULLIF(?,''),?) ON CONFLICT(id) DO NOTHING`, b.ID, b.InstanceID, b.DatabaseName, b.S3ConnectionID, b.ObjectKey, now(), b.PolicyID, scope)
	return err
}

// AddDBBackupFile records an object key before it is uploaded, so cleanup can
// always find it even when the upload fails half-way.
func (s *Store) AddDBBackupFile(ctx context.Context, id, key string) error {
	return affected(s.db.ExecContext(ctx, `UPDATE db_backups SET files=json_insert(files,'$[#]',?) WHERE id=?`, key, id))
}
func (s *Store) CompleteDBBackup(ctx context.Context, id string, size int64) error {
	return affected(s.db.ExecContext(ctx, `UPDATE db_backups SET size_bytes=?,ready=1 WHERE id=?`, size, id))
}
func (s *Store) ListDBBackups(ctx context.Context, instanceID string) ([]DBBackup, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT b.id,b.instance_id,b.database_name,b.s3_connection_id,b.object_key,b.size_bytes,b.ready,b.created_at,o.status,o.error,COALESCE(b.policy_id,''),b.scope,b.files FROM db_backups b JOIN db_operations o ON o.id=b.id WHERE b.instance_id=? ORDER BY b.id DESC`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []DBBackup{}
	for rows.Next() {
		var b DBBackup
		var created, files string
		if err := rows.Scan(&b.ID, &b.InstanceID, &b.DatabaseName, &b.S3ConnectionID, &b.ObjectKey, &b.SizeBytes, &b.Ready, &created, &b.Status, &b.Error, &b.PolicyID, &b.Scope, &files); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(files), &b.Files); err != nil {
			return nil, err
		}
		b.CreatedAt = parseTime(created)
		result = append(result, b)
	}
	return result, rows.Err()
}
func (s *Store) GetDBBackup(ctx context.Context, instanceID, id string) (DBBackup, error) {
	rows, err := s.ListDBBackups(ctx, instanceID)
	if err != nil {
		return DBBackup{}, err
	}
	for _, b := range rows {
		if b.ID == id {
			return b, nil
		}
	}
	return DBBackup{}, ErrNotFound
}

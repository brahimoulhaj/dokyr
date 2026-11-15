package store

import (
	"context"
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
}

func (s *Store) AddDBBackup(ctx context.Context, b DBBackup) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO db_backups(id,instance_id,database_name,s3_connection_id,object_key,created_at,policy_id) VALUES(?,?,?,?,?,?,NULLIF(?,'')) ON CONFLICT(id) DO NOTHING`, b.ID, b.InstanceID, b.DatabaseName, b.S3ConnectionID, b.ObjectKey, now(), b.PolicyID)
	return err
}
func (s *Store) CompleteDBBackup(ctx context.Context, id string, size int64) error {
	return affected(s.db.ExecContext(ctx, `UPDATE db_backups SET size_bytes=?,ready=1 WHERE id=?`, size, id))
}
func (s *Store) ListDBBackups(ctx context.Context, instanceID string) ([]DBBackup, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT b.id,b.instance_id,b.database_name,b.s3_connection_id,b.object_key,b.size_bytes,b.ready,b.created_at,o.status,o.error,COALESCE(b.policy_id,'') FROM db_backups b JOIN db_operations o ON o.id=b.id WHERE b.instance_id=? ORDER BY b.id DESC`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []DBBackup{}
	for rows.Next() {
		var b DBBackup
		var created string
		if err := rows.Scan(&b.ID, &b.InstanceID, &b.DatabaseName, &b.S3ConnectionID, &b.ObjectKey, &b.SizeBytes, &b.Ready, &created, &b.Status, &b.Error, &b.PolicyID); err != nil {
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

package store

import (
	"context"
	"database/sql"
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

const dbBackupCols = `b.id,b.instance_id,b.database_name,b.s3_connection_id,b.object_key,b.size_bytes,b.ready,b.created_at,o.status,o.error,COALESCE(b.policy_id,''),b.scope,b.files`

func scanDBBackup(rows *sql.Rows, extra ...any) (DBBackup, error) {
	var b DBBackup
	var created, files string
	dest := append([]any{&b.ID, &b.InstanceID, &b.DatabaseName, &b.S3ConnectionID, &b.ObjectKey, &b.SizeBytes, &b.Ready, &created, &b.Status, &b.Error, &b.PolicyID, &b.Scope, &files}, extra...)
	if err := rows.Scan(dest...); err != nil {
		return b, err
	}
	if err := json.Unmarshal([]byte(files), &b.Files); err != nil {
		return b, err
	}
	b.CreatedAt = parseTime(created)
	return b, nil
}

func (s *Store) ListDBBackups(ctx context.Context, instanceID string) ([]DBBackup, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+dbBackupCols+` FROM db_backups b JOIN db_operations o ON o.id=b.id WHERE b.instance_id=? ORDER BY b.id DESC`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []DBBackup{}
	for rows.Next() {
		b, err := scanDBBackup(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, rows.Err()
}

// OrphanDBBackup is a backup whose instance was deleted. The instance's name,
// project, engine and version come from the backup operation's snapshot.
type OrphanDBBackup struct {
	DBBackup
	InstanceName string `json:"instance_name"`
	ProjectID    string `json:"project_id"`
	Engine       string `json:"engine"`
	Version      string `json:"version"`
}

// ListOrphanDBBackups returns the backups of deleted instances, newest first.
// Unfinished ones are left out: their operation died with the instance.
func (s *Store) ListOrphanDBBackups(ctx context.Context) ([]OrphanDBBackup, error) {
	return s.orphanDBBackups(ctx, "")
}

// GetOrphanDBBackup returns ErrNotFound unless the backup exists and its instance does not.
func (s *Store) GetOrphanDBBackup(ctx context.Context, id string) (OrphanDBBackup, error) {
	list, err := s.orphanDBBackups(ctx, id)
	if err != nil {
		return OrphanDBBackup{}, err
	}
	if len(list) == 0 {
		return OrphanDBBackup{}, ErrNotFound
	}
	return list[0], nil
}

func (s *Store) orphanDBBackups(ctx context.Context, id string) ([]OrphanDBBackup, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+dbBackupCols+`,o.instance_name,o.project_id,o.engine,o.version FROM db_backups b JOIN db_operations o ON o.id=b.id
WHERE NOT EXISTS (SELECT 1 FROM db_instances i WHERE i.id=b.instance_id) AND o.status IN ('succeeded','failed') AND (?='' OR b.id=?) ORDER BY b.id DESC`, id, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []OrphanDBBackup{}
	for rows.Next() {
		var o OrphanDBBackup
		b, err := scanDBBackup(rows, &o.InstanceName, &o.ProjectID, &o.Engine, &o.Version)
		if err != nil {
			return nil, err
		}
		o.DBBackup = b
		result = append(result, o)
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

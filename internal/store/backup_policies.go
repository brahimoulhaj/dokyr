package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"panel/internal/id"
)

type BackupPolicy struct {
	ID              string    `json:"id"`
	InstanceID      string    `json:"instance_id"`
	DatabaseName    string    `json:"database_name"`
	S3ConnectionID  string    `json:"s3_connection_id"`
	Enabled         bool      `json:"enabled"`
	Frequency       string    `json:"frequency"`
	Hour            int       `json:"hour"`
	Minute          int       `json:"minute"`
	Weekday         int       `json:"weekday"`
	KeepCount       int       `json:"keep_count"`
	NextRunAt       time.Time `json:"next_run_at"`
	LastOperationID string    `json:"last_operation_id"`
	LastError       string    `json:"last_error"`
}

const policyColumns = `id,instance_id,database_name,s3_connection_id,enabled,frequency,hour,minute,weekday,keep_count,next_run_at,last_operation_id,last_error`

func scanBackupPolicy(row scanner) (BackupPolicy, error) {
	var p BackupPolicy
	var next string
	e := row.Scan(&p.ID, &p.InstanceID, &p.DatabaseName, &p.S3ConnectionID, &p.Enabled, &p.Frequency, &p.Hour, &p.Minute, &p.Weekday, &p.KeepCount, &next, &p.LastOperationID, &p.LastError)
	if errors.Is(e, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	p.NextRunAt = parseTime(next)
	return p, e
}
func (s *Store) BackupPolicies(ctx context.Context, instance string) ([]BackupPolicy, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT `+policyColumns+` FROM backup_policies WHERE instance_id=? ORDER BY database_name`, instance)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	result := []BackupPolicy{}
	for rows.Next() {
		p, e := scanBackupPolicy(rows)
		if e != nil {
			return nil, e
		}
		result = append(result, p)
	}
	return result, rows.Err()
}
func (s *Store) GetBackupPolicy(ctx context.Context, id string) (BackupPolicy, error) {
	return scanBackupPolicy(s.db.QueryRowContext(ctx, `SELECT `+policyColumns+` FROM backup_policies WHERE id=?`, id))
}
func (s *Store) SaveBackupPolicy(ctx context.Context, p BackupPolicy) (BackupPolicy, error) {
	_, e := s.db.ExecContext(ctx, `INSERT INTO backup_policies(id,instance_id,database_name,s3_connection_id,enabled,frequency,hour,minute,weekday,keep_count,next_run_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(instance_id,database_name) DO UPDATE SET s3_connection_id=excluded.s3_connection_id,enabled=excluded.enabled,frequency=excluded.frequency,hour=excluded.hour,minute=excluded.minute,weekday=excluded.weekday,keep_count=excluded.keep_count,next_run_at=excluded.next_run_at,last_error='',updated_at=excluded.updated_at`, id.New(), p.InstanceID, p.DatabaseName, p.S3ConnectionID, p.Enabled, p.Frequency, p.Hour, p.Minute, p.Weekday, p.KeepCount, p.NextRunAt.UTC().Format(time.RFC3339Nano), now())
	if e != nil {
		return p, conflict(e)
	}
	return scanBackupPolicy(s.db.QueryRowContext(ctx, `SELECT `+policyColumns+` FROM backup_policies WHERE instance_id=? AND database_name=?`, p.InstanceID, p.DatabaseName))
}
func (s *Store) DueBackupPolicies(ctx context.Context, at time.Time) ([]BackupPolicy, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT `+policyColumns+` FROM backup_policies WHERE enabled=1 ORDER BY next_run_at`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	result := []BackupPolicy{}
	for rows.Next() {
		p, e := scanBackupPolicy(rows)
		if e != nil {
			return nil, e
		}
		if !p.NextRunAt.After(at) {
			result = append(result, p)
		}
	}
	return result, rows.Err()
}

// QueuePolicyBackup advances the schedule and persists its job in one transaction.
// A failed transaction leaves the policy due, so busy/stopped instances retry later.
func (s *Store) QueuePolicyBackup(ctx context.Context, p BackupPolicy, d DBInstance, at, next time.Time) (DBOperation, error) {
	payload, e := s.sealEnvironment(map[string]string{"database": p.DatabaseName, "s3_connection_id": p.S3ConnectionID, "policy_id": p.ID})
	if e != nil {
		return DBOperation{}, e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return DBOperation{}, e
	}
	defer tx.Rollback()
	current, e := scanBackupPolicy(tx.QueryRowContext(ctx, `SELECT `+policyColumns+` FROM backup_policies WHERE id=?`, p.ID))
	if e != nil {
		return DBOperation{}, e
	}
	if !current.Enabled || current.NextRunAt.After(at) || !current.NextRunAt.Equal(p.NextRunAt) || current.S3ConnectionID != p.S3ConnectionID || current.KeepCount != p.KeepCount {
		return DBOperation{}, ErrConflict
	}
	oid, ts := id.New(), now()
	_, e = tx.ExecContext(ctx, `INSERT INTO db_operations(id,instance_id,project_id,instance_name,engine,version,action,status,payload,detail,created_at,updated_at) VALUES(?,?,?,?,?,?,'backup','queued',?,?,?,?)`, oid, d.ID, d.ProjectID, d.Name, d.Engine, d.Version, payload, p.DatabaseName, ts, ts)
	if e != nil {
		return DBOperation{}, conflict(e)
	}
	_, e = tx.ExecContext(ctx, `UPDATE backup_policies SET next_run_at=?,last_operation_id=?,last_error='',updated_at=? WHERE id=?`, next.UTC().Format(time.RFC3339Nano), oid, ts, p.ID)
	if e != nil {
		return DBOperation{}, e
	}
	if e = tx.Commit(); e != nil {
		return DBOperation{}, e
	}
	return s.GetDBOperation(ctx, oid)
}
func (s *Store) BackupPolicyError(ctx context.Context, id, reason string) error {
	_, e := s.db.ExecContext(ctx, `UPDATE backup_policies SET last_error=?,updated_at=? WHERE id=?`, reason, now(), id)
	return e
}
func (s *Store) RemovePrunedBackup(ctx context.Context, id string) error {
	return affected(s.db.ExecContext(ctx, `DELETE FROM db_backups WHERE id=?`, id))
}

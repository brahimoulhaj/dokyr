package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"panel/internal/id"
)

type DBOperation struct {
	ID           string     `json:"id"`
	InstanceID   string     `json:"instance_id"`
	ProjectID    string     `json:"project_id"`
	InstanceName string     `json:"instance_name"`
	Engine       string     `json:"engine"`
	Version      string     `json:"version"`
	Action       string     `json:"action"`
	Status       string     `json:"status"`
	Error        string     `json:"error"`
	Detail       string     `json:"detail"`
	StartedAt    *time.Time `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

const dbOperationCols = `id, instance_id, project_id, instance_name, engine, version, action, status, error, started_at, finished_at, created_at, updated_at, detail`

func scanDBOperation(row scanner) (DBOperation, error) {
	var o DBOperation
	var started, finished sql.NullString
	var created, updated string
	err := row.Scan(&o.ID, &o.InstanceID, &o.ProjectID, &o.InstanceName, &o.Engine, &o.Version, &o.Action, &o.Status, &o.Error, &started, &finished, &created, &updated, &o.Detail)
	if errors.Is(err, sql.ErrNoRows) {
		return o, ErrNotFound
	}
	if started.Valid {
		t := parseTime(started.String)
		o.StartedAt = &t
	}
	if finished.Valid {
		t := parseTime(finished.String)
		o.FinishedAt = &t
	}
	o.CreatedAt, o.UpdatedAt = parseTime(created), parseTime(updated)
	return o, err
}

// CreateDBOperation atomically records the job and its instance's intent before
// returning an accepted response. A partial unique index serializes each instance.
func (s *Store) CreateDBOperation(ctx context.Context, d DBInstance, action string) (DBOperation, error) {
	return s.CreateDBOperationPayload(ctx, d, action, "", nil)
}

func (s *Store) CreateDBOperationPayload(ctx context.Context, d DBInstance, action, detail string, payload map[string]string) (DBOperation, error) {
	sealed, err := s.sealEnvironment(payload)
	if err != nil {
		return DBOperation{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return DBOperation{}, err
	}
	defer tx.Rollback()
	oid, ts := id.New(), now()
	_, err = tx.ExecContext(ctx, `INSERT INTO db_operations
 (id, instance_id, project_id, instance_name, engine, version, action, status, created_at, updated_at, payload, detail)
 VALUES (?, ?, ?, ?, ?, ?, ?, 'queued', ?, ?, ?, ?)`, oid, d.ID, d.ProjectID, d.Name, d.Engine, d.Version, action, ts, ts, sealed, detail)
	if err != nil {
		return DBOperation{}, conflict(err)
	}
	desired, status := StateRunning, "provisioning"
	switch action {
	case "database_create", "user_create", "grant_update", "backup", "restore", "database_delete", "user_delete", "password_rotate", "catalog_sync":
		// SQL work on a running instance leaves its lifecycle state alone.
		desired, status = d.DesiredState, d.Status
	}
	if action == "stop" || action == "delete" || action == "restart" {
		desired, status = StateStopped, "stopping"
	}
	if err := affected(tx.ExecContext(ctx, `UPDATE db_instances SET desired_state = ?, status = ?, error = '', updated_at = ? WHERE id = ?`, desired, status, ts, d.ID)); err != nil {
		return DBOperation{}, err
	}
	if err := tx.Commit(); err != nil {
		return DBOperation{}, err
	}
	return s.GetDBOperation(ctx, oid)
}

// SetDBOperationPayload replaces an operation's encrypted input, e.g. to record
// which phase a long job reached before a restart.
func (s *Store) SetDBOperationPayload(ctx context.Context, oid string, payload map[string]string) error {
	sealed, err := s.sealEnvironment(payload)
	if err != nil {
		return err
	}
	return affected(s.db.ExecContext(ctx, `UPDATE db_operations SET payload = ?, updated_at = ? WHERE id = ?`, sealed, now(), oid))
}

// DBOperationPayload is never part of an operation's public JSON.
func (s *Store) DBOperationPayload(ctx context.Context, oid string) (map[string]string, error) {
	var sealed string
	if err := s.db.QueryRowContext(ctx, `SELECT payload FROM db_operations WHERE id = ?`, oid).Scan(&sealed); err != nil {
		return nil, err
	}
	return s.openEnvironment(sealed)
}

func (s *Store) GetDBOperation(ctx context.Context, oid string) (DBOperation, error) {
	return scanDBOperation(s.db.QueryRowContext(ctx, `SELECT `+dbOperationCols+` FROM db_operations WHERE id = ?`, oid))
}

func (s *Store) ActiveDBOperations(ctx context.Context) ([]DBOperation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+dbOperationCols+` FROM db_operations WHERE status IN ('queued', 'running') ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DBOperation{}
	for rows.Next() {
		o, err := scanDBOperation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *Store) StartDBOperation(ctx context.Context, oid string) error {
	return affected(s.db.ExecContext(ctx, `UPDATE db_operations SET status = 'running', started_at = COALESCE(started_at, ?), updated_at = ? WHERE id = ? AND status IN ('queued', 'running')`, now(), now(), oid))
}

func (s *Store) FinishDBOperation(ctx context.Context, oid, reason string) error {
	status := "succeeded"
	if reason != "" {
		status = "failed"
	}
	return affected(s.db.ExecContext(ctx, `UPDATE db_operations SET status = ?, error = ?, finished_at = ?, updated_at = ? WHERE id = ? AND status IN ('queued', 'running')`, status, reason, now(), now(), oid))
}

// SetDBOperationDetail replaces an operation's detail: what it turned out to do.
func (s *Store) SetDBOperationDetail(ctx context.Context, opID, detail string) error {
	return affected(s.db.ExecContext(ctx, `UPDATE db_operations SET detail = ?, updated_at = ? WHERE id = ?`, detail, now(), opID))
}

package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"panel/internal/id"
)

// DBInstance contains no credentials. Passwords are read separately and encrypted
// with the same panel key as app environments.
type DBInstance struct {
	ID           string    `json:"id"`
	ProjectID    string    `json:"project_id"`
	Name         string    `json:"name"`
	Engine       string    `json:"engine"`
	Version      string    `json:"version"`
	DatabaseName string    `json:"database_name"`
	Username     string    `json:"username"`
	CPULimit     float64   `json:"cpu_limit"`
	MemoryMB     int64     `json:"memory_mb"`
	DesiredState string    `json:"desired_state"`
	Status       string    `json:"status"`
	Error        string    `json:"error"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	// PublicPort is the host port of the public gate; 0 means internal only.
	PublicPort  int    `json:"public_port"`
	PublicAllow string `json:"public_allow"` // comma-separated IPs/CIDRs; empty = anyone
}

const dbInstanceCols = `id, project_id, name, engine, version, database_name, username,
 cpu_limit, memory_mb, desired_state, status, error, created_at, updated_at, public_port, public_allow`

func scanDBInstance(row scanner) (DBInstance, error) {
	var d DBInstance
	var created, updated string
	err := row.Scan(&d.ID, &d.ProjectID, &d.Name, &d.Engine, &d.Version, &d.DatabaseName, &d.Username,
		&d.CPULimit, &d.MemoryMB, &d.DesiredState, &d.Status, &d.Error, &created, &updated, &d.PublicPort, &d.PublicAllow)
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	d.CreatedAt, d.UpdatedAt = parseTime(created), parseTime(updated)
	return d, err
}

func (s *Store) CreateDBInstance(ctx context.Context, in DBInstance, credentials map[string]string) (DBInstance, error) {
	if _, err := s.GetProject(ctx, in.ProjectID); err != nil {
		return DBInstance{}, err
	}
	sealed, err := s.sealEnvironment(credentials)
	if err != nil {
		return DBInstance{}, err
	}
	did, ts := id.New(), now()
	_, err = s.db.ExecContext(ctx, `INSERT INTO db_instances
 (id, project_id, name, engine, version, database_name, username, credentials, cpu_limit, memory_mb, created_at, updated_at)
 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, did, in.ProjectID, in.Name, in.Engine, in.Version,
		in.DatabaseName, in.Username, sealed, in.CPULimit, in.MemoryMB, ts, ts)
	if err != nil {
		return DBInstance{}, conflict(err)
	}
	return s.GetDBInstance(ctx, did)
}

func (s *Store) GetDBInstance(ctx context.Context, did string) (DBInstance, error) {
	return scanDBInstance(s.db.QueryRowContext(ctx, `SELECT `+dbInstanceCols+` FROM db_instances WHERE id = ?`, did))
}

var ErrDBInstanceBusy = errors.New("store: database operation in progress")

// MoveDBInstance changes project membership only. Instance-owned resources and
// historical operation snapshots remain attached to the same instance ID.
func (s *Store) MoveDBInstance(ctx context.Context, did, projectID string) (DBInstance, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return DBInstance{}, err
	}
	defer tx.Rollback()
	d, err := scanDBInstance(tx.QueryRowContext(ctx, `SELECT `+dbInstanceCols+` FROM db_instances WHERE id = ?`, did))
	if err != nil {
		return DBInstance{}, err
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM projects WHERE id = ?)`, projectID).Scan(&exists); err != nil {
		return DBInstance{}, err
	}
	if !exists {
		return DBInstance{}, ErrNotFound
	}
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM db_operations WHERE instance_id = ? AND status IN ('queued', 'running'))`, did).Scan(&exists); err != nil {
		return DBInstance{}, err
	}
	if exists {
		return DBInstance{}, ErrDBInstanceBusy
	}
	if d.ProjectID != projectID {
		if err := affected(tx.ExecContext(ctx, `UPDATE db_instances SET project_id = ?, updated_at = ? WHERE id = ?`, projectID, now(), did)); err != nil {
			return DBInstance{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return DBInstance{}, err
	}
	return s.GetDBInstance(ctx, did)
}

func (s *Store) ListDBInstances(ctx context.Context) ([]DBInstance, error) {
	return s.listDBInstances(ctx, "")
}

func (s *Store) ListProjectDBInstances(ctx context.Context, projectID string) ([]DBInstance, error) {
	if projectID == "" {
		return nil, ErrNotFound
	}
	return s.listDBInstances(ctx, projectID)
}

func (s *Store) listDBInstances(ctx context.Context, projectID string) ([]DBInstance, error) {
	query := `SELECT ` + dbInstanceCols + ` FROM db_instances`
	var args []any
	if projectID != "" {
		query += ` WHERE project_id = ?`
		args = append(args, projectID)
	}
	rows, err := s.db.QueryContext(ctx, query+` ORDER BY name, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DBInstance{}
	for rows.Next() {
		d, err := scanDBInstance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DBInstanceDatabaseNames includes the original database even when its management
// tab has never been opened, plus additional databases successfully created there.
func (s *Store) DBInstanceDatabaseNames(ctx context.Context, instanceID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name FROM db_databases WHERE instance_id = ?
 UNION SELECT database_name FROM db_instances WHERE id = ? ORDER BY 1`, instanceID, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func (s *Store) DBInstanceCredentials(ctx context.Context, did string) (map[string]string, error) {
	var sealed string
	err := s.db.QueryRowContext(ctx, `SELECT credentials FROM db_instances WHERE id = ?`, did).Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.openEnvironment(sealed)
}

func (s *Store) SetDBInstanceState(ctx context.Context, did, desired, status, reason string) error {
	return affected(s.db.ExecContext(ctx, `UPDATE db_instances SET desired_state = ?, status = ?, error = ?, updated_at = ? WHERE id = ?`,
		desired, status, reason, now(), did))
}

func (s *Store) DeleteDBInstance(ctx context.Context, did string) error {
	return affected(s.db.ExecContext(ctx, `DELETE FROM db_instances WHERE id = ?`, did))
}

// SetDBInstancePublic saves public access settings; port 0 turns it off.
func (s *Store) SetDBInstancePublic(ctx context.Context, did string, port int, allow string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE db_instances SET public_port = ?, public_allow = ?, updated_at = ? WHERE id = ?`, port, allow, now(), did)
	return conflict(err)
}

// UsedPublicPorts lists public ports of other instances.
func (s *Store) UsedPublicPorts(ctx context.Context, except string) (map[int]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT public_port FROM db_instances WHERE public_port > 0 AND id <> ?`, except)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	used := map[int]bool{}
	for rows.Next() {
		var p int
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		used[p] = true
	}
	return used, rows.Err()
}

// SetDBInstanceCredentials replaces the encrypted credentials (e.g. to add the
// public gate's certificate).
func (s *Store) SetDBInstanceCredentials(ctx context.Context, did string, credentials map[string]string) error {
	sealed, err := s.sealEnvironment(credentials)
	if err != nil {
		return err
	}
	return affected(s.db.ExecContext(ctx, `UPDATE db_instances SET credentials = ?, updated_at = ? WHERE id = ?`, sealed, now(), did))
}

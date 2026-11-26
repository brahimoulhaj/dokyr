package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"panel/internal/id"
)

// Volume backup states.
const (
	VolumeBackupRunning = "running"
	VolumeBackupReady   = "ready"
	VolumeBackupFailed  = "failed"
)

// VolumeBackup is one backup of a named volume of an app, in S3 storage.
type VolumeBackup struct {
	ID             string
	AppID          string // "" once the app is deleted
	AppName        string
	Volume         string
	S3ConnectionID string
	ObjectKey      string
	Size           int64
	Status         string
	Error          string
	CreatedAt      time.Time
	FinishedAt     *time.Time
}

const volumeBackupCols = `id, COALESCE(app_id, ''), app_name, volume, s3_connection_id, object_key, size_bytes, status, error, created_at, finished_at`

func scanVolumeBackup(row scanner) (VolumeBackup, error) {
	var b VolumeBackup
	var created string
	var finished sql.NullString
	err := row.Scan(&b.ID, &b.AppID, &b.AppName, &b.Volume, &b.S3ConnectionID, &b.ObjectKey, &b.Size, &b.Status, &b.Error, &created, &finished)
	if errors.Is(err, sql.ErrNoRows) {
		return VolumeBackup{}, ErrNotFound
	}
	b.CreatedAt = parseTime(created)
	if finished.Valid {
		t := parseTime(finished.String)
		b.FinishedAt = &t
	}
	return b, err
}

// CreateVolumeBackup records a backup that has just started.
func (s *Store) CreateVolumeBackup(ctx context.Context, appID, appName, volume, s3ConnectionID, objectKey string) (VolumeBackup, error) {
	bid := id.New()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO volume_backups (id, app_id, app_name, volume, s3_connection_id, object_key, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		bid, appID, appName, volume, s3ConnectionID, objectKey, now()); err != nil {
		return VolumeBackup{}, err
	}
	return s.GetVolumeBackup(ctx, bid)
}

// FinishVolumeBackup records how a backup ended: ready with its size, or failed with why.
func (s *Store) FinishVolumeBackup(ctx context.Context, backupID string, size int64, failure string) error {
	status := VolumeBackupReady
	if failure != "" {
		status = VolumeBackupFailed
	}
	return affected(s.db.ExecContext(ctx, `UPDATE volume_backups SET status = ?, size_bytes = ?, error = ?, finished_at = ? WHERE id = ?`,
		status, size, failure, now(), backupID))
}

// FailRunningVolumeBackups marks the backups a restart interrupted; it returns how many.
func (s *Store) FailRunningVolumeBackups(ctx context.Context, failure string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE volume_backups SET status = ?, error = ?, finished_at = ? WHERE status = ?`,
		VolumeBackupFailed, failure, now(), VolumeBackupRunning)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) GetVolumeBackup(ctx context.Context, backupID string) (VolumeBackup, error) {
	return scanVolumeBackup(s.db.QueryRowContext(ctx, `SELECT `+volumeBackupCols+` FROM volume_backups WHERE id = ?`, backupID))
}

// AppVolumeBackups lists the backups of an app's volumes, newest first.
func (s *Store) AppVolumeBackups(ctx context.Context, appID string) ([]VolumeBackup, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+volumeBackupCols+` FROM volume_backups WHERE app_id = ? ORDER BY id DESC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []VolumeBackup{}
	for rows.Next() {
		b, err := scanVolumeBackup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) DeleteVolumeBackup(ctx context.Context, backupID string) error {
	return affected(s.db.ExecContext(ctx, `DELETE FROM volume_backups WHERE id = ?`, backupID))
}

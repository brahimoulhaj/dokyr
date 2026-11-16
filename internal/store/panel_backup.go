package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	settingPanelBackup       = "panel_backup"
	settingPanelBackupStatus = "panel_backup_status"
)

// PanelBackupConfig is where and when the panel backs up its own database and key.
// Passphrase encrypts each backup; it is sealed with the panel key at rest and never
// sent back to the browser.
type PanelBackupConfig struct {
	Enabled        bool   `json:"enabled"`
	S3ConnectionID string `json:"s3_connection_id"`
	Schedule       string `json:"schedule"` // 5-field cron in UTC
	Keep           int    `json:"keep"`     // newest backups kept in storage
	Passphrase     string `json:"-"`
}

// PanelBackupStatus is the outcome of the latest run.
type PanelBackupStatus struct {
	LastRunAt     *time.Time `json:"last_run_at"`
	LastSuccessAt *time.Time `json:"last_success_at"`
	LastError     string     `json:"last_error"`
	LastKey       string     `json:"last_key"`
	LastSize      int64      `json:"last_size"`
}

type storedPanelBackup struct {
	PanelBackupConfig
	SealedPassphrase string `json:"passphrase"`
}

// ErrNoBackupConfig: the panel backup was never set up.
var ErrNoBackupConfig = errors.New("panel backup is not set up")

func (s *Store) GetPanelBackupConfig(ctx context.Context) (PanelBackupConfig, error) {
	raw, ok, err := s.GetSetting(ctx, settingPanelBackup)
	if err != nil {
		return PanelBackupConfig{}, err
	}
	if !ok {
		return PanelBackupConfig{}, ErrNoBackupConfig
	}
	var stored storedPanelBackup
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return PanelBackupConfig{}, fmt.Errorf("panel backup settings: %w", err)
	}
	cfg := stored.PanelBackupConfig
	if stored.SealedPassphrase != "" {
		if s.box == nil {
			return PanelBackupConfig{}, errors.New("panel backup: no encryption key loaded")
		}
		plain, err := s.box.Decrypt(stored.SealedPassphrase)
		if err != nil {
			return PanelBackupConfig{}, fmt.Errorf("panel backup passphrase: %w", err)
		}
		cfg.Passphrase = string(plain)
	}
	return cfg, nil
}

// SetPanelBackupConfig saves the settings. An empty Passphrase keeps the saved one.
func (s *Store) SetPanelBackupConfig(ctx context.Context, cfg PanelBackupConfig) error {
	stored := storedPanelBackup{PanelBackupConfig: cfg}
	if cfg.Passphrase == "" {
		if old, err := s.GetPanelBackupConfig(ctx); err == nil {
			cfg.Passphrase = old.Passphrase
		} else if !errors.Is(err, ErrNoBackupConfig) {
			return err
		}
	}
	if cfg.Passphrase != "" {
		if s.box == nil {
			return errors.New("panel backup: no encryption key loaded")
		}
		sealed, err := s.box.Encrypt([]byte(cfg.Passphrase))
		if err != nil {
			return err
		}
		stored.SealedPassphrase = sealed
	}
	raw, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	return s.SetSetting(ctx, settingPanelBackup, string(raw))
}

func (s *Store) GetPanelBackupStatus(ctx context.Context) (PanelBackupStatus, error) {
	var st PanelBackupStatus
	raw, ok, err := s.GetSetting(ctx, settingPanelBackupStatus)
	if err != nil || !ok {
		return st, err
	}
	return st, json.Unmarshal([]byte(raw), &st)
}

func (s *Store) SetPanelBackupStatus(ctx context.Context, st PanelBackupStatus) error {
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return s.SetSetting(ctx, settingPanelBackupStatus, string(raw))
}

// Snapshot writes a consistent copy of the database to dest (which must not exist),
// while the panel keeps running. VACUUM INTO also folds in what still sits in the WAL.
func (s *Store) Snapshot(ctx context.Context, dest string) error {
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("snapshot: %s already exists", dest)
	}
	_, err := s.db.ExecContext(ctx, "VACUUM INTO '"+strings.ReplaceAll(dest, "'", "''")+"'")
	return err
}

// SchemaVersion is the newest migration applied.
func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var v int
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&v)
	return v, err
}

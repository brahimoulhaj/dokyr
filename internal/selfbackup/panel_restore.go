package selfbackup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"panel/internal/brand"
	"panel/internal/store"
)

var (
	// ErrNewer: the backup's database is newer than this panel understands.
	ErrNewer = errors.New("this backup was made by a newer version of the panel; update the panel first")
	// ErrForeign: the backup says another product made it.
	ErrForeign = errors.New("this backup was not made by this panel")
	// ErrRestoring: a restore is staged or being staged; the panel is about to restart.
	ErrRestoring = errors.New("a restore is already in progress")
)

const restoredName = "restored.json"

// Restored is what a restore started from the panel leaves for the next start, to be
// recorded in the restored database (the one it was started from is replaced).
type Restored struct {
	Backup    string    `json:"backup"`
	Actor     string    `json:"actor"`
	CreatedAt time.Time `json:"created_at"` // when the backup was made
	// Status is the backup status at the restore: the backup of the state that was
	// replaced is the newest one, and the restored database predates it (a backup is
	// even snapshotted before its own success is saved).
	Status *store.PanelBackupStatus `json:"status,omitempty"`
}

// TakeRestored returns the note left by Restore, once.
func TakeRestored(dataDir string) (Restored, bool) {
	p := filepath.Join(dataDir, restoredName)
	raw, err := os.ReadFile(p)
	if err != nil {
		return Restored{}, false
	}
	os.Remove(p)
	var r Restored
	return r, json.Unmarshal(raw, &r) == nil
}

// Inspect downloads a backup from storage, decrypts and checks it, and tells what it
// holds. Nothing is kept.
func (s *Service) Inspect(ctx context.Context, name, passphrase string) (Manifest, error) {
	tmp, err := os.MkdirTemp(s.dataDir, ".panel-inspect-")
	if err != nil {
		return Manifest{}, err
	}
	defer os.RemoveAll(tmp)
	return s.fetch(ctx, name, passphrase, filepath.Join(tmp, "backup"))
}

// fetch extracts the backup into dir (which must not exist) and checks that this panel
// can run on it.
func (s *Service) fetch(ctx context.Context, name, passphrase, dir string) (Manifest, error) {
	rc, err := s.Download(ctx, name)
	if err != nil {
		return Manifest{}, err
	}
	defer rc.Close()
	m, err := Extract(rc, passphrase, dir)
	if err != nil {
		return m, err
	}
	if err := checkDatabase(ctx, filepath.Join(dir, dbName)); err != nil {
		return m, err
	}
	if m.Product != "" && m.Product != brand.Slug {
		return m, ErrForeign
	}
	schema, err := s.st.SchemaVersion(ctx)
	if err != nil {
		return m, err
	}
	if m.SchemaVersion > schema {
		return m, ErrNewer
	}
	return m, nil
}

// Restore stages a backup from storage to replace the database and key at the next
// start; the caller restarts the panel. The backup is checked first, then the state
// about to be replaced is backed up, so there is always a way forward again: a failure
// at either step changes nothing.
func (s *Service) Restore(ctx context.Context, name, passphrase, actor string) (Manifest, error) {
	if !s.restoring.CompareAndSwap(false, true) {
		return Manifest{}, ErrRestoring
	}
	staged := false
	defer func() {
		if !staged { // once staged it stays set: the panel restarts
			s.restoring.Store(false)
		}
	}()
	staging := filepath.Join(s.dataDir, stagingDir)
	if err := os.RemoveAll(staging); err != nil {
		return Manifest{}, err
	}
	defer os.RemoveAll(staging) // gone already once renamed
	m, err := s.fetch(ctx, name, passphrase, staging)
	if err != nil {
		return m, err
	}
	if _, err := s.Run(ctx, actor); err != nil {
		return m, fmt.Errorf("the current state could not be backed up first, nothing was restored: %w", err)
	}
	restored := Restored{Backup: name, Actor: actor, CreatedAt: m.CreatedAt}
	if status, err := s.st.GetPanelBackupStatus(ctx); err == nil {
		restored.Status = &status
	}
	note, err := json.Marshal(restored)
	if err != nil {
		return m, err
	}
	if err := os.WriteFile(filepath.Join(s.dataDir, restoredName), note, 0o600); err != nil {
		return m, err
	}
	pending := filepath.Join(s.dataDir, pendingDir)
	if err := os.RemoveAll(pending); err != nil {
		return m, err
	}
	if err := os.Rename(staging, pending); err != nil {
		return m, err
	}
	staged = true
	return m, nil
}

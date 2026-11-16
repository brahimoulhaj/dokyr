package selfbackup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // checks the restored database before it is staged
)

const (
	pendingDir = "restore-pending"
	stagingDir = "restore-staging"
)

// Stage decrypts and checks a backup, and leaves it in dataDir to replace the database
// and key the next time the panel starts. The running panel is not touched, so this is
// safe to run next to it (`docker exec`), followed by a restart.
func Stage(ctx context.Context, r io.Reader, passphrase, dataDir string) (Manifest, error) {
	staging := filepath.Join(dataDir, stagingDir)
	if err := os.RemoveAll(staging); err != nil {
		return Manifest{}, err
	}
	m, err := Extract(r, passphrase, staging)
	if err == nil {
		err = checkDatabase(ctx, filepath.Join(staging, dbName))
	}
	if err != nil {
		os.RemoveAll(staging)
		return m, err
	}
	pending := filepath.Join(dataDir, pendingDir)
	if err := os.RemoveAll(pending); err != nil {
		return m, err
	}
	return m, os.Rename(staging, pending)
}

func checkDatabase(ctx context.Context, path string) error {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()
	var result string
	if err := db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&result); err != nil {
		return fmt.Errorf("backup database: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("backup database is damaged: %s", result)
	}
	var tables int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('schema_migrations', 'projects', 'apps')`).Scan(&tables); err != nil || tables != 3 {
		return errors.New("backup database is not a panel database")
	}
	return nil
}

// Pending reports whether a staged restore waits for the next start.
func Pending(dataDir string) bool {
	_, err := os.Stat(filepath.Join(dataDir, pendingDir, dbName))
	return err == nil
}

// ApplyPending swaps a staged restore in, before the database or key is opened. The
// files it replaces move to backupDir/pre-restore-<time>, so a mistaken restore can be
// undone by hand. It returns where they went, or "" when nothing was staged.
func ApplyPending(dataDir, dbPath, keyPath, backupDir string) (string, error) {
	pending := filepath.Join(dataDir, pendingDir)
	if !Pending(dataDir) {
		return "", nil
	}
	aside := filepath.Join(backupDir, "pre-restore-"+time.Now().UTC().Format("20060102T150405Z"))
	if err := os.MkdirAll(aside, 0o700); err != nil {
		return "", err
	}
	// The WAL and shared-memory files belong to the old database: keep them with it,
	// never next to the restored one.
	for _, p := range []string{dbPath, dbPath + "-wal", dbPath + "-shm", keyPath} {
		if err := os.Rename(p, filepath.Join(aside, filepath.Base(p))); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("move %s aside: %w", filepath.Base(p), err)
		}
	}
	if err := os.Rename(filepath.Join(pending, keyName), keyPath); err != nil {
		return "", fmt.Errorf("restore key: %w", err)
	}
	if err := os.Rename(filepath.Join(pending, dbName), dbPath); err != nil {
		return "", fmt.Errorf("restore database: %w", err)
	}
	return aside, os.RemoveAll(pending)
}

package selfbackup

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"panel/internal/secret"
	"panel/internal/store"
)

const passphrase = "correct horse battery staple"

// memObjects is an in-memory bucket.
type memObjects struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (m *memObjects) Put(_ context.Context, key string, r io.Reader) (int64, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = b
	return int64(len(b)), nil
}

func (m *memObjects) Get(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.objects[key]
	if !ok {
		return nil, store.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (m *memObjects) List(_ context.Context, prefix string) ([]Object, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Object{}
	for k, b := range m.objects {
		if strings.HasPrefix(k, prefix) && validName(filepath.Base(k)) {
			out = append(out, Object{Key: k, Name: filepath.Base(k), Size: int64(len(b))})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out, nil
}

func (m *memObjects) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

func setup(t *testing.T) (*Service, *store.Store, *memObjects, string) {
	t.Helper()
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "secret.key")
	box, err := secret.LoadOrCreate(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "panel.db"), filepath.Join(dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	if _, err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UseSecrets(ctx, box); err != nil {
		t.Fatal(err)
	}
	bucket := &memObjects{objects: map[string][]byte{}}
	svc := New(st, slog.New(slog.NewTextHandler(io.Discard, nil)), "v-test", dir, keyPath)
	svc.Open = func(context.Context, string) (Objects, string, error) { return bucket, "prefix/panel-backups", nil }
	return svc, st, bucket, dir
}

func TestBackupAndRestore(t *testing.T) {
	svc, st, bucket, dir := setup(t)
	ctx := context.Background()

	if _, err := svc.Run(ctx, "test"); !errors.Is(err, store.ErrNoBackupConfig) {
		t.Fatalf("unconfigured run = %v", err)
	}
	if err := st.SetPanelBackupConfig(ctx, store.PanelBackupConfig{S3ConnectionID: "s3", Schedule: "0 3 * * *", Keep: 2, Passphrase: passphrase}); err != nil {
		t.Fatal(err)
	}
	// The passphrase is sealed at rest and kept when saving without one.
	raw, _, _ := st.GetSetting(ctx, "panel_backup")
	if strings.Contains(raw, passphrase) {
		t.Fatalf("passphrase stored in clear: %s", raw)
	}
	if err := st.SetPanelBackupConfig(ctx, store.PanelBackupConfig{S3ConnectionID: "s3", Schedule: "0 3 * * *", Keep: 2}); err != nil {
		t.Fatal(err)
	}
	if cfg, err := st.GetPanelBackupConfig(ctx); err != nil || cfg.Passphrase != passphrase {
		t.Fatalf("config = %+v, %v", cfg, err)
	}

	p, err := st.CreateProject(ctx, "Kept", "in the backup")
	if err != nil {
		t.Fatal(err)
	}
	obj, err := svc.Run(ctx, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(obj.Key, "prefix/panel-backups/") || !strings.HasSuffix(obj.Key, Extension) || obj.Size == 0 {
		t.Fatalf("object = %+v", obj)
	}
	if bytes.Contains(bucket.objects[obj.Key], []byte("in the backup")) {
		t.Fatal("backup is not encrypted")
	}
	status, _ := st.GetPanelBackupStatus(ctx)
	if status.LastSuccessAt == nil || status.LastError != "" || status.LastKey != obj.Key {
		t.Fatalf("status = %+v", status)
	}

	// Wrong passphrase.
	if _, err := Extract(bytes.NewReader(bucket.objects[obj.Key]), "not the passphrase", t.TempDir()); !errors.Is(err, ErrPassphrase) {
		t.Fatalf("wrong passphrase = %v", err)
	}

	// Stage and apply on a "new server": a fresh data dir with its own key and database.
	target := t.TempDir()
	dbPath, keyPath := filepath.Join(target, "panel.db"), filepath.Join(target, "secret.key")
	os.WriteFile(dbPath, []byte("old database"), 0o600)
	os.WriteFile(dbPath+"-wal", []byte("old wal"), 0o600)
	os.WriteFile(keyPath, []byte("old key"), 0o600)
	m, err := Stage(ctx, bytes.NewReader(bucket.objects[obj.Key]), passphrase, target)
	if err != nil {
		t.Fatal(err)
	}
	if m.PanelVersion != "v-test" || m.SchemaVersion == 0 || !Pending(target) {
		t.Fatalf("manifest = %+v, pending %v", m, Pending(target))
	}
	aside, err := ApplyPending(target, dbPath, keyPath, filepath.Join(target, "backups"))
	if err != nil || aside == "" {
		t.Fatalf("apply = %q, %v", aside, err)
	}
	if Pending(target) {
		t.Fatal("still pending after apply")
	}
	if b, _ := os.ReadFile(filepath.Join(aside, "panel.db-wal")); string(b) != "old wal" {
		t.Fatal("old WAL not moved aside")
	}
	if _, err := os.Stat(dbPath + "-wal"); !os.IsNotExist(err) {
		t.Fatal("old WAL left next to the restored database")
	}
	origKey, _ := os.ReadFile(filepath.Join(dir, "secret.key"))
	if got, _ := os.ReadFile(keyPath); !bytes.Equal(got, origKey) {
		t.Fatal("key not restored")
	}
	restored, err := store.Open(dbPath, "")
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if got, err := restored.GetProject(ctx, p.ID); err != nil || got.Description != "in the backup" {
		t.Fatalf("restored project = %+v, %v", got, err)
	}
	box, _ := secret.LoadOrCreate(keyPath)
	if _, err := restored.UseSecrets(ctx, box); err != nil {
		t.Fatal(err)
	}
	if cfg, err := restored.GetPanelBackupConfig(ctx); err != nil || cfg.Passphrase != passphrase {
		t.Fatalf("restored key opens its secrets: %+v, %v", cfg, err)
	}

	// Retention keeps the newest two.
	for i := 0; i < 2; i++ {
		time.Sleep(1100 * time.Millisecond) // names have one-second resolution
		if _, err := svc.Run(ctx, "test"); err != nil {
			t.Fatal(err)
		}
	}
	list, err := svc.List(ctx)
	if err != nil || len(list) != 2 || list[0].Name <= list[1].Name {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if _, ok := bucket.objects[obj.Key]; ok {
		t.Fatal("oldest backup not pruned")
	}
	rc, err := svc.Download(ctx, list[0].Name)
	if err != nil {
		t.Fatal(err)
	}
	rc.Close()
	for _, bad := range []string{"../secret.key", "other.tar.gz.age", list[0].Name + "/x"} {
		if _, err := svc.Download(ctx, bad); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("download %q = %v", bad, err)
		}
	}
}

func TestStageRejectsDamage(t *testing.T) {
	svc, st, bucket, _ := setup(t)
	ctx := context.Background()
	if err := st.SetPanelBackupConfig(ctx, store.PanelBackupConfig{S3ConnectionID: "s3", Schedule: "0 3 * * *", Passphrase: passphrase}); err != nil {
		t.Fatal(err)
	}
	obj, err := svc.Run(ctx, "test")
	if err != nil {
		t.Fatal(err)
	}
	data := bucket.objects[obj.Key]
	target := t.TempDir()
	if _, err := Stage(ctx, bytes.NewReader(data[:len(data)/2]), passphrase, target); err == nil {
		t.Fatal("truncated backup staged")
	}
	if Pending(target) {
		t.Fatal("failed stage left a pending restore")
	}
}

func TestSchedule(t *testing.T) {
	svc, st, _, _ := setup(t)
	ctx := context.Background()
	for _, bad := range []string{"", "@daily", "* * *", "61 * * * *", "0 0 30 2 *"} {
		if _, err := ParseSchedule(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
	every := store.PanelBackupConfig{Enabled: true, S3ConnectionID: "s3", Schedule: "* * * * *", Passphrase: passphrase}
	if err := st.SetPanelBackupConfig(ctx, every); err != nil {
		t.Fatal(err)
	}
	svc.started = time.Now().UTC().Add(-2 * time.Minute)
	if !svc.due(ctx) {
		t.Fatal("not due after a missed slot")
	}
	now := time.Now().UTC()
	st.SetPanelBackupStatus(ctx, store.PanelBackupStatus{LastRunAt: &now})
	if svc.due(ctx) {
		t.Fatal("due right after a run")
	}
	every.Enabled = false
	st.SetPanelBackupConfig(ctx, every)
	svc.started = time.Now().UTC().Add(-time.Hour)
	if svc.due(ctx) || svc.NextRun(ctx) != nil {
		t.Fatal("disabled schedule runs")
	}
}

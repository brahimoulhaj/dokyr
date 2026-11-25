package api

import (
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"panel/internal/selfbackup"
	"panel/internal/store"
)

// restoreStub stands in for the panel backups: one backup, opened by one passphrase.
type restoreStub struct {
	PanelBackups
	restored atomic.Int32
}

func (b *restoreStub) check(name, passphrase string) (selfbackup.Manifest, error) {
	switch {
	case name != "dokyr-1.tar.gz.age":
		return selfbackup.Manifest{}, store.ErrNotFound
	case passphrase != "the passphrase":
		return selfbackup.Manifest{}, selfbackup.ErrPassphrase
	}
	return selfbackup.Manifest{PanelVersion: "v1", SchemaVersion: 3, CreatedAt: time.Unix(1700000000, 0).UTC()}, nil
}

func (b *restoreStub) Inspect(_ context.Context, name, passphrase string) (selfbackup.Manifest, error) {
	return b.check(name, passphrase)
}

func (b *restoreStub) Restore(_ context.Context, name, passphrase, _ string) (selfbackup.Manifest, error) {
	m, err := b.check(name, passphrase)
	if err == nil {
		b.restored.Add(1)
	}
	return m, err
}

func (b *restoreStub) Download(context.Context, string) (io.ReadCloser, error) {
	return nil, store.ErrNotFound
}

// TestPanelRestore: only the owner, signed in, with the backup's passphrase, rolls the
// panel back; the panel then restarts.
func TestPanelRestore(t *testing.T) {
	e := newEnv(t)
	stub := &restoreStub{}
	e.srv.SetPanelBackups(stub)
	restarted := make(chan struct{}, 1)
	e.srv.SetRestart(func() { restarted <- struct{}{} })
	const base = "/api/settings/panel-backup/backups/dokyr-1.tar.gz.age"
	good := map[string]any{"passphrase": "the passphrase"}

	e.must(401, "POST", base+"/restore", good)
	e.signIn()
	for _, role := range []string{"admin", "maintainer"} {
		member := e.join(role+"@b.co", role)
		member.must(403, "POST", base+"/inspect", good)
		member.must(403, "POST", base+"/restore", good)
	}
	e.must(422, "POST", base+"/restore", map[string]any{"passphrase": ""})
	e.must(422, "POST", base+"/restore", map[string]any{"passphrase": "wrong"})
	e.must(404, "POST", "/api/settings/panel-backup/backups/dokyr-2.tar.gz.age/restore", good)
	if stub.restored.Load() != 0 {
		t.Fatal("a refused restore went through")
	}

	if m := e.must(200, "POST", base+"/inspect", good); m["panel_version"] != "v1" || m["restarting"] != false {
		t.Fatalf("inspect = %v", m)
	}
	if m := e.must(202, "POST", base+"/restore", good); m["restarting"] != true || m["name"] != "dokyr-1.tar.gz.age" {
		t.Fatalf("restore = %v", m)
	}
	select {
	case <-restarted:
	case <-time.After(5 * time.Second):
		t.Fatal("the panel did not restart after the restore")
	}
	if stub.restored.Load() != 1 {
		t.Fatalf("restores = %d", stub.restored.Load())
	}
}

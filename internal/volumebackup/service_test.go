package volumebackup

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"panel/internal/executor"
	"panel/internal/secret"
	"panel/internal/store"
)

// fakeHost stands in for Docker: a helper container "holds" the volume as a byte slice,
// tar writes it out and the restore command reads it back.
type fakeHost struct {
	mu       sync.Mutex
	content  map[string][]byte // by volume
	helpers  map[string]executor.ContainerSpec
	removed  int
	running  []executor.Container
	failTar  error
	readOnly []bool
}

func (h *fakeHost) PullImage(context.Context, string, *executor.RegistryAuth, func(executor.PullProgress)) error {
	return nil
}
func (h *fakeHost) CreateContainer(_ context.Context, spec executor.ContainerSpec) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.helpers[spec.Name] = spec
	h.readOnly = append(h.readOnly, spec.Mounts[0].ReadOnly)
	return spec.Name, nil
}
func (h *fakeHost) StartContainer(context.Context, string) error { return nil }
func (h *fakeHost) RemoveContainer(_ context.Context, id string, _ bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.helpers, id)
	h.removed++
	return nil
}
func (h *fakeHost) ListContainers(context.Context) ([]executor.Container, error) {
	return h.running, nil
}
func (h *fakeHost) ExecStream(_ context.Context, id string, cmd executor.Command, in io.Reader, out io.Writer) error {
	h.mu.Lock()
	volume := h.helpers[id].Mounts[0].Volume
	h.mu.Unlock()
	if cmd.Args[0] == "tar" {
		if h.failTar != nil {
			return h.failTar
		}
		_, err := out.Write(h.content[volume])
		return err
	}
	data, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	h.mu.Lock()
	h.content[volume] = data
	h.mu.Unlock()
	return nil
}

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
		return nil, errors.New("no such object")
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}
func (m *memObjects) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

func setup(t *testing.T) (*Service, *store.Store, *fakeHost, *memObjects, store.App, string) {
	t.Helper()
	dir := t.TempDir()
	box, err := secret.LoadOrCreate(filepath.Join(dir, "secret.key"))
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
	p, err := st.CreateProject(ctx, "Shop", "")
	if err != nil {
		t.Fatal(err)
	}
	app, err := st.CreateApp(ctx, p.ID, store.AppInput{Name: "web", SourceType: "image", Image: "nginx:1", Port: 80, Mounts: []store.AppMount{
		{Type: "volume", Source: "web-data", Target: "/data"},
		{Type: "bind", Source: "/srv/files", Target: "/files"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := st.CreateS3Connection(ctx, store.S3Input{S3Connection: store.S3Connection{Name: "s3", Endpoint: "s3.test", Bucket: "b"}, SecretKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	host := &fakeHost{content: map[string][]byte{"web-data": []byte("the files of the volume")}, helpers: map[string]executor.ContainerSpec{}}
	bucket := &memObjects{objects: map[string][]byte{}}
	svc := New(st, host, "helper:1", slog.New(slog.NewTextHandler(io.Discard, nil)), func(context.Context, string) (Objects, string, error) {
		return bucket, "prefix", nil
	})
	return svc, st, host, bucket, app, conn.ID
}

// TestBackupAndRestore: a volume the app mounts is archived, gzip-compressed, to storage
// from a read-only helper; a restore puts it back while the app is stopped, and nothing
// else can be named, restored under a running app, or restored from a failed backup.
func TestBackupAndRestore(t *testing.T) {
	svc, st, host, bucket, app, conn := setup(t)
	ctx := context.Background()

	for _, name := range []string{"", "other-volume", "/srv/files", "dokyr-data"} {
		if _, err := svc.Start(ctx, app, name, conn, "a@b.co"); !errors.Is(err, ErrNotVolume) {
			t.Fatalf("backup of %q = %v, want ErrNotVolume", name, err)
		}
	}
	if got := Volumes(app); len(got) != 1 || got[0] != "web-data" {
		t.Fatalf("volumes = %v", got)
	}

	b, err := svc.Start(ctx, app, "web-data", conn, "a@b.co")
	if err != nil || b.Status != store.VolumeBackupRunning || !strings.HasPrefix(b.ObjectKey, "prefix/volume-backups/web-data/web-data-") {
		t.Fatalf("start = %+v, %v", b, err)
	}
	svc.Wait()
	b, err = st.GetVolumeBackup(ctx, b.ID)
	if err != nil || b.Status != store.VolumeBackupReady || b.Size == 0 || b.FinishedAt == nil {
		t.Fatalf("finished = %+v, %v", b, err)
	}
	z, err := gzip.NewReader(bytes.NewReader(bucket.objects[b.ObjectKey]))
	if err != nil {
		t.Fatal(err)
	}
	if plain, _ := io.ReadAll(z); string(plain) != "the files of the volume" {
		t.Fatalf("stored = %q", plain)
	}
	if len(host.helpers) != 0 || host.removed != 1 || !host.readOnly[0] {
		t.Fatalf("helper left behind or not read-only: %d left, %d removed, read-only %v", len(host.helpers), host.removed, host.readOnly)
	}

	// A restore under a running app is refused; stopped, it replaces the content.
	host.content["web-data"] = []byte("changed since")
	host.running = []executor.Container{{Kind: executor.KindApp, ResourceID: app.ID, Running: true}}
	if _, err := svc.Restore(ctx, b.ID, "a@b.co"); !errors.Is(err, ErrRunning) {
		t.Fatalf("restore under a running app = %v", err)
	}
	host.running = []executor.Container{{Kind: executor.KindApp, ResourceID: "another-app", Running: true}}
	if _, err := svc.Restore(ctx, b.ID, "a@b.co"); err != nil {
		t.Fatal(err)
	}
	svc.Wait()
	if string(host.content["web-data"]) != "the files of the volume" || host.readOnly[1] {
		t.Fatalf("restored = %q (read-only helper: %v)", host.content["web-data"], host.readOnly[1])
	}
	if _, err := svc.Restore(ctx, "missing", "a@b.co"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("restore of a missing backup = %v", err)
	}

	// A failed backup leaves a record that says why, no object, and cannot be restored.
	host.failTar = errors.New("exit 2")
	failed, err := svc.Start(ctx, app, "web-data", conn, "a@b.co")
	if err != nil {
		t.Fatal(err)
	}
	svc.Wait()
	failed, _ = st.GetVolumeBackup(ctx, failed.ID)
	if failed.Status != store.VolumeBackupFailed || failed.Error == "" || len(bucket.objects) != 1 {
		t.Fatalf("failed backup = %+v, %d objects", failed, len(bucket.objects))
	}
	if _, err := svc.Restore(ctx, failed.ID, "a@b.co"); !errors.Is(err, ErrNotReady) {
		t.Fatalf("restore of a failed backup = %v", err)
	}

	if _, err := svc.Delete(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	if len(bucket.objects) != 0 {
		t.Fatal("the object was not deleted")
	}
	if list, _ := st.AppVolumeBackups(ctx, app.ID); len(list) != 1 || list[0].ID != failed.ID {
		t.Fatalf("left = %+v", list)
	}
}

package objectstore

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"panel/internal/executor"
	"panel/internal/executor/executortest"
	"panel/internal/secret"
	"panel/internal/store"
)

func setup(t *testing.T) (*Service, *store.Store, *executortest.Fake) {
	t.Helper()
	dir := t.TempDir()
	box, err := secret.LoadOrCreate(filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "panel.db"), "")
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
	ex := executortest.New()
	svc, err := New(Options{Executor: ex, Store: st, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Name: "panel-s3", Image: "rustfs/rustfs:test", DataVolume: "panel-s3-data"})
	if err != nil {
		t.Fatal(err)
	}
	return svc, st, ex
}

func TestLifecycle(t *testing.T) {
	svc, st, ex := setup(t)
	ctx := context.Background()

	// Off: nothing runs, no client.
	if err := svc.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := ex.InspectContainer(ctx, "panel-s3"); !errors.Is(err, executor.ErrNotFound) {
		t.Fatalf("container while off: %v", err)
	}
	if svc.State(ctx) != "off" {
		t.Fatalf("state = %s", svc.State(ctx))
	}
	if _, err := svc.Buckets(ctx); !errors.Is(err, ErrOff) {
		t.Fatalf("buckets while off = %v", err)
	}

	// On: credentials made, sealed at rest, container running with them and the volume.
	cfg, err := svc.Configure(ctx, true, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.AccessKey) != 20 || len(cfg.SecretKey) != 40 {
		t.Fatalf("credentials = %q / %d chars", cfg.AccessKey, len(cfg.SecretKey))
	}
	raw, _, _ := st.GetSetting(ctx, "object_store")
	if strings.Contains(raw, cfg.SecretKey) {
		t.Fatal("secret key stored in clear")
	}
	if err := svc.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	c, err := ex.InspectContainer(ctx, "panel-s3")
	if err != nil || !c.Running || !IsObjectStore(c.Kind, c.ResourceID) {
		t.Fatalf("container = %+v, %v", c, err)
	}
	spec := svc.spec(cfg)
	if spec.Env["RUSTFS_SECRET_KEY"] != cfg.SecretKey || spec.Mounts[0].Volume != "panel-s3-data" || len(spec.Ports) != 0 {
		t.Fatalf("spec = %+v", spec)
	}

	// Applying again changes nothing; a domain change keeps the credentials and container.
	if err := svc.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	again, err := svc.Configure(ctx, true, "s3.example.com", "")
	if err != nil || again.SecretKey != cfg.SecretKey || again.AccessKey != cfg.AccessKey {
		t.Fatalf("reconfigure = %+v, %v", again, err)
	}
	if err := svc.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if c2, _ := ex.InspectContainer(ctx, "panel-s3"); c2.ID != c.ID {
		t.Fatal("container recreated without a spec change")
	}

	// Off: container removed, credentials kept for the data still in the volume.
	if _, err := svc.Configure(ctx, false, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := ex.InspectContainer(ctx, "panel-s3"); !errors.Is(err, executor.ErrNotFound) {
		t.Fatalf("container after off: %v", err)
	}
	if kept, _ := svc.Settings(ctx); kept.SecretKey != cfg.SecretKey {
		t.Fatal("credentials lost when turned off")
	}
}

func TestForeignContainer(t *testing.T) {
	svc, _, ex := setup(t)
	ctx := context.Background()
	if _, err := ex.CreateContainer(ctx, executor.ContainerSpec{Name: "panel-s3", Image: "x", Kind: executor.KindApp, ResourceID: "app"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Configure(ctx, true, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.Apply(ctx); err == nil {
		t.Fatal("took over a container it does not manage")
	}
}

func TestBucketNames(t *testing.T) {
	for _, ok := range []string{"panel", "my-bucket.2", "abc"} {
		if !bucketName.MatchString(ok) {
			t.Fatalf("%q refused", ok)
		}
	}
	for _, bad := range []string{"ab", "Upper", "under_score", "-start", "end-", "a/b", string(make([]byte, 64))} {
		if bucketName.MatchString(bad) {
			t.Fatalf("%q accepted", bad)
		}
	}
}

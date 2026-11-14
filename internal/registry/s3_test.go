package registry

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"

	"panel/internal/executor"
	"panel/internal/storage"
	"panel/internal/store"
)

// TestRealRegistryS3 stores the registry's images in S3 (a throwaway RustFS): the
// connection check passes, a pushed image lands in the bucket under the prefix.
func TestRealRegistryS3(t *testing.T) {
	if testing.Short() {
		t.Skip("needs Docker; skipped with -short")
	}
	ctx := context.Background()
	name := fmt.Sprintf("paneltest-%d", time.Now().UnixNano())
	d, err := executor.NewDocker(executor.Options{Network: name, LabelPrefix: name})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Ping(ctx); err != nil {
		t.Skipf("no Docker daemon: %v", err)
	}
	s3Port := freePort(t)
	t.Cleanup(func() {
		list, _ := d.ListContainers(ctx)
		for _, c := range list {
			_ = d.RemoveContainer(ctx, c.ID, true)
		}
		exec.Command("docker", "network", "rm", name).Run()
		exec.Command("docker", "volume", "rm", name+"-data").Run()
		d.Close()
	})
	if err := d.EnsureNetwork(ctx); err != nil {
		t.Fatal(err)
	}
	// RustFS: S3-compatible, the panel's planned built-in storage. Skipped when not pulled.
	const s3Image = "rustfs/rustfs:latest"
	if out, err := exec.Command("docker", "image", "inspect", s3Image).CombinedOutput(); err != nil {
		t.Skipf("no %s image locally: %s", s3Image, out)
	}
	mid, err := d.CreateContainer(ctx, executor.ContainerSpec{
		Name: name + "-s3", Image: s3Image, Kind: executor.KindService, ResourceID: "s3",
		Env:   map[string]string{"RUSTFS_ACCESS_KEY": "panelkey", "RUSTFS_SECRET_KEY": "panelsecret"},
		Ports: []executor.PortBinding{{HostIP: "127.0.0.1", HostPort: s3Port, ContainerPort: 9000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.StartContainer(ctx, mid); err != nil {
		t.Fatal(err)
	}
	// Seen from the host for the check and the bucket; from the registry by container name.
	conn := store.S3Input{S3Connection: store.S3Connection{Endpoint: fmt.Sprintf("127.0.0.1:%d", s3Port), Bucket: "images",
		Prefix: "panel/registry", AccessKey: "panelkey", PathStyle: true}, SecretKey: "panelsecret"}
	cl, err := storage.Client(conn)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		if err = cl.MakeBucket(ctx, "images", minio.MakeBucketOptions{}); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("S3 never came up: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err := storage.Test(ctx, conn); err != nil {
		t.Fatalf("storage.Test: %v", err)
	}
	bad := conn
	bad.SecretKey = "wrong"
	if err := storage.Test(ctx, bad); err == nil || !strings.Contains(err.Error(), "secret key") {
		t.Errorf("a wrong secret key: %v", err)
	}

	st := newStore(t)
	inNet := conn
	inNet.Endpoint = name + "-s3:9000"
	c, err := st.CreateS3Connection(ctx, inNet)
	if err != nil {
		t.Fatal(err)
	}
	st.SetSetting(ctx, store.SettingRegistryS3, c.ID)
	st.SetRegistrySettings(ctx, store.RegistrySettings{Enabled: true, Domain: "registry.example.com"})
	port := freePort(t)
	s, err := New(Options{Executor: d, Store: st, Log: slog.New(slog.DiscardHandler), Name: name + "-registry", Image: "registry:3.0.0",
		Dir: t.TempDir(), DataVolume: name + "-data", Issuer: "berth", PublishPort: port, PanelUpstream: "host.docker.internal:1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	base := s.baseURL()
	waitUp(t, base+"/v2/")
	tok, _ := s.Token(ctx, "berth", s.system, "registry.example.com", []string{"repository:shop/api:pull,push"})
	config := []byte(`{"architecture":"amd64","os":"linux","rootfs":{"type":"layers","diff_ids":[]}}`)
	upload(t, base, tok, config)

	found := false
	for obj := range cl.ListObjects(ctx, "images", minio.ListObjectsOptions{Prefix: "panel/registry/", Recursive: true}) {
		if obj.Err == nil && strings.Contains(obj.Key, "docker/registry/v2/blobs") {
			found = true
		}
	}
	if !found {
		t.Error("the pushed blob is not in the bucket under panel/registry/")
	}
}

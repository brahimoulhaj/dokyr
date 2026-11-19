package objectstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"panel/internal/executor/executortest"
	"panel/internal/secret"
	"panel/internal/store"
)

func TestBucketPolicy(t *testing.T) {
	read, _ := bucketPolicy("photos", store.S3KeyRead)
	write, _ := bucketPolicy("photos", store.S3KeyWrite)
	for _, want := range []string{`"arn:aws:s3:::photos"`, `"arn:aws:s3:::photos/*"`, `"s3:GetObject"`, `"s3:ListBucket"`} {
		if !strings.Contains(string(read), want) || !strings.Contains(string(write), want) {
			t.Fatalf("policy misses %s: %s", want, read)
		}
	}
	if strings.Contains(string(read), "PutObject") || strings.Contains(string(read), "DeleteObject") {
		t.Fatalf("read policy can write: %s", read)
	}
	if !strings.Contains(string(write), `"s3:PutObject"`) || !strings.Contains(string(write), `"s3:DeleteObject"`) {
		t.Fatalf("write policy cannot write: %s", write)
	}
	if !json.Valid(read) {
		t.Fatal("invalid JSON")
	}
}

func TestCreateAccessKeyValidation(t *testing.T) {
	svc, _, _ := setup(t)
	ctx := context.Background()
	if _, _, err := svc.CreateAccessKey(ctx, "", "b", store.S3KeyRead); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("empty name: %v", err)
	}
	if _, _, err := svc.CreateAccessKey(ctx, "ci", "b", "admin"); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("bad permission: %v", err)
	}
	if _, _, err := svc.CreateAccessKey(ctx, "ci", "b", store.S3KeyRead); !errors.Is(err, ErrOff) {
		t.Fatalf("while off: %v", err)
	}
}

// TestRealAccessKeys makes keys in a real RustFS and checks what each can reach.
func TestRealAccessKeys(t *testing.T) {
	if testing.Short() {
		t.Skip("needs Docker; skipped with -short")
	}
	const image = "rustfs/rustfs:1.0.0-rc.6"
	if out, err := exec.Command("docker", "image", "inspect", image).CombinedOutput(); err != nil {
		t.Skipf("no %s image locally: %s", image, out)
	}
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
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	svc, err := New(Options{Executor: executortest.New(), Store: st, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Name: "unused", Image: image, DataVolume: "unused", PublishPort: port})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := svc.Configure(ctx, true, "", "")
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("panel-keys-test-%d", time.Now().UnixNano())
	if out, err := exec.Command("docker", "run", "-d", "--rm", "--name", name, "-p", fmt.Sprintf("127.0.0.1:%d:9000", port),
		"-e", "RUSTFS_ACCESS_KEY="+cfg.AccessKey, "-e", "RUSTFS_SECRET_KEY="+cfg.SecretKey, "-e", "RUSTFS_VOLUMES=/data",
		"-e", "RUSTFS_ADDRESS=:9000", "-e", "RUSTFS_CONSOLE_ENABLE=false", image).CombinedOutput(); err != nil {
		t.Fatalf("docker run: %s", out)
	}
	t.Cleanup(func() { exec.Command("docker", "rm", "-f", name).Run() })
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := svc.Buckets(ctx); err == nil {
			break
		} else if time.Now().After(deadline) {
			t.Fatalf("storage not answering: %v", err)
		}
		time.Sleep(300 * time.Millisecond)
	}
	for _, b := range []string{"photos", "other"} {
		if err := svc.CreateBucket(ctx, b); err != nil {
			t.Fatal(err)
		}
	}
	root, _ := minio.New(svc.Endpoint(), &minio.Options{Creds: credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""), BucketLookup: minio.BucketLookupPath, Region: "us-east-1"})
	for _, b := range []string{"photos", "other"} {
		if _, err := root.PutObject(ctx, b, "a.txt", bytes.NewReader([]byte("hi")), 2, minio.PutObjectOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	as := func(access, secret string) *minio.Client {
		c, _ := minio.New(svc.Endpoint(), &minio.Options{Creds: credentials.NewStaticV4(access, secret, ""), BucketLookup: minio.BucketLookupPath, Region: "us-east-1"})
		return c
	}
	put := func(c *minio.Client, bucket string) error {
		_, err := c.PutObject(ctx, bucket, "b.txt", bytes.NewReader([]byte("x")), 1, minio.PutObjectOptions{})
		return err
	}
	get := func(c *minio.Client, bucket string) error {
		_, err := c.StatObject(ctx, bucket, "a.txt", minio.StatObjectOptions{})
		return err
	}

	if _, _, err := svc.CreateAccessKey(ctx, "x", "missing", store.S3KeyRead); !errors.Is(err, ErrNoBucket) {
		t.Fatalf("missing bucket: %v", err)
	}
	ro, roSecret, err := svc.CreateAccessKey(ctx, "site", "photos", store.S3KeyRead)
	if err != nil {
		t.Fatal(err)
	}
	rw, rwSecret, err := svc.CreateAccessKey(ctx, "uploader", "photos", store.S3KeyWrite)
	if err != nil {
		t.Fatal(err)
	}
	if len(ro.AccessKey) != 20 || len(roSecret) != 40 || ro.Permission != "read" {
		t.Fatalf("key = %+v", ro)
	}
	r, w := as(ro.AccessKey, roSecret), as(rw.AccessKey, rwSecret)
	if err := get(r, "photos"); err != nil {
		t.Fatalf("read key cannot read its bucket: %v", err)
	}
	if err := put(r, "photos"); err == nil {
		t.Fatal("read key wrote")
	}
	if err := get(r, "other"); err == nil {
		t.Fatal("read key reached another bucket")
	}
	if err := put(w, "photos"); err != nil {
		t.Fatalf("write key cannot write: %v", err)
	}
	if err := w.RemoveObject(ctx, "photos", "b.txt", minio.RemoveObjectOptions{}); err != nil {
		t.Fatalf("write key cannot delete: %v", err)
	}
	if err := put(w, "other"); err == nil {
		t.Fatal("write key reached another bucket")
	}

	// A bucket with keys cannot be deleted.
	if err := svc.DeleteBucket(ctx, "photos"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("delete bucket with keys: %v", err)
	}
	keys, _ := svc.AccessKeys(ctx, "photos")
	if len(keys) != 2 {
		t.Fatalf("keys = %+v", keys)
	}

	// Deleting a key revokes it at once; deleting it again is not found.
	if _, err := svc.DeleteAccessKey(ctx, ro.ID); err != nil {
		t.Fatal(err)
	}
	if err := get(r, "photos"); err == nil {
		t.Fatal("deleted key still reads")
	}
	if _, err := svc.DeleteAccessKey(ctx, ro.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}
	// A key already gone from the storage is still forgotten.
	adm, _ := svc.admin(ctx)
	if err := adm.deleteServiceAccount(ctx, rw.AccessKey); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DeleteAccessKey(ctx, rw.ID); err != nil {
		t.Fatalf("delete key missing in storage: %v", err)
	}
	if keys, _ := svc.AccessKeys(ctx, ""); len(keys) != 0 {
		t.Fatalf("keys left = %+v", keys)
	}
}

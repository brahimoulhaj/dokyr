// Package objectstore runs the built-in S3 storage (RustFS) in a container of its own,
// like the registry: off until enabled, its data in a Docker volume, optionally served on
// a domain through the proxy. The panel keeps its root credentials and makes an S3
// connection to it, so backups and the registry can use it like any other bucket.
//
// RustFS is still a release candidate; the UI labels this feature experimental.
package objectstore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"panel/internal/executor"
	"panel/internal/store"
)

const (
	resourceID = "object-store"
	specLabel  = "object-store.spec" // hash of the container spec; a change recreates it
	// Port is the S3 API port inside the container.
	Port = 9000
	// DefaultBucket is made on first start for the panel's own use (backups).
	DefaultBucket = "panel"
	// ConnectionName names the S3 connection the panel makes for the built-in storage.
	ConnectionName = "Built-in storage"
)

// IsObjectStore reports whether a managed container is the built-in storage.
func IsObjectStore(kind executor.Kind, id string) bool {
	return kind == executor.KindService && id == resourceID
}

type Options struct {
	Executor   executor.Executor
	Store      *store.Store
	Log        *slog.Logger
	Name       string // container name; also its host name on the shared network
	Image      string
	DataVolume string // named volume the objects are kept in
	// PublishPort publishes the S3 port on 127.0.0.1 of the host, for a panel that runs on
	// the host (development) and cannot reach the shared network; 0 = not published.
	PublishPort int
}

type Service struct {
	opts Options
	mu   sync.Mutex // serialises Apply
}

func New(opts Options) (*Service, error) {
	if opts.Executor == nil || opts.Store == nil || opts.Log == nil || opts.Name == "" || opts.Image == "" || opts.DataVolume == "" {
		return nil, errors.New("objectstore: executor, store, log, name, image and data volume are required")
	}
	return &Service{opts: opts}, nil
}

// Upstream is where the proxy reaches the S3 API, on the shared network.
func (s *Service) Upstream() string { return fmt.Sprintf("%s:%d", s.opts.Name, Port) }

// Endpoint is where the panel (and containers on the shared network) reach the S3 API.
func (s *Service) Endpoint() string {
	if s.opts.PublishPort > 0 {
		return fmt.Sprintf("127.0.0.1:%d", s.opts.PublishPort)
	}
	return s.Upstream()
}

func (s *Service) Settings(ctx context.Context) (store.ObjectStoreSettings, error) {
	return s.opts.Store.GetObjectStoreSettings(ctx)
}

// Configure turns the storage on or off and sets its domain. Root credentials are made
// on first enable and kept afterwards.
func (s *Service) Configure(ctx context.Context, enabled bool, domain, tlsMode string) (store.ObjectStoreSettings, error) {
	cur, err := s.Settings(ctx)
	if err != nil {
		return cur, err
	}
	cur.Enabled, cur.Domain, cur.TLSMode = enabled, domain, tlsMode
	if cur.AccessKey == "" || cur.SecretKey == "" {
		if cur.AccessKey, err = randomKey(10); err != nil {
			return cur, err
		}
		if cur.SecretKey, err = randomKey(20); err != nil {
			return cur, err
		}
	}
	return cur, s.opts.Store.SetObjectStoreSettings(ctx, cur)
}

// randomKey is n random bytes, hex: letters and digits only, safe in any client.
func randomKey(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Apply makes the container match the settings: removed when off (the volume and its data
// stay), created or recreated when its spec changed, started when stopped.
func (s *Service) Apply(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ex := s.opts.Executor
	cfg, err := s.Settings(ctx)
	if err != nil {
		return err
	}
	c, err := ex.InspectContainer(ctx, s.opts.Name)
	found := err == nil
	if err != nil && !errors.Is(err, executor.ErrNotFound) {
		return err
	}
	if found && !IsObjectStore(c.Kind, c.ResourceID) {
		return fmt.Errorf("object storage: a container named %s exists and is not managed by this panel; remove or rename it", s.opts.Name)
	}
	if !cfg.Enabled || cfg.SecretKey == "" {
		if found {
			s.opts.Log.Info("object storage turned off; removing its container (the data volume stays)")
			return ex.RemoveContainer(ctx, c.ID, true)
		}
		return nil
	}
	spec := s.spec(cfg)
	if found {
		if c.Labels[specLabel] == spec.Labels[specLabel] {
			if c.Running {
				return nil
			}
			return ex.StartContainer(ctx, c.ID)
		}
		s.opts.Log.Info("object storage configuration changed; recreating its container")
		if err := ex.RemoveContainer(ctx, c.ID, true); err != nil {
			return err
		}
	}
	if err := ex.EnsureNetwork(ctx); err != nil {
		return err
	}
	id, err := ex.CreateContainer(ctx, spec)
	if errors.Is(err, executor.ErrNotFound) {
		s.opts.Log.Info("pulling object storage image", "image", spec.Image)
		if err := ex.PullImage(ctx, spec.Image, nil, nil); err != nil {
			return err
		}
		id, err = ex.CreateContainer(ctx, spec)
	}
	if err != nil {
		return err
	}
	if err := ex.StartContainer(ctx, id); err != nil {
		_ = ex.RemoveContainer(context.WithoutCancel(ctx), id, true)
		return err
	}
	return nil
}

func (s *Service) spec(cfg store.ObjectStoreSettings) executor.ContainerSpec {
	o := s.opts
	spec := executor.ContainerSpec{
		Name:       o.Name,
		Image:      o.Image,
		Kind:       executor.KindService,
		ResourceID: resourceID,
		Env: map[string]string{
			"RUSTFS_ACCESS_KEY":     cfg.AccessKey,
			"RUSTFS_SECRET_KEY":     cfg.SecretKey,
			"RUSTFS_VOLUMES":        "/data",
			"RUSTFS_ADDRESS":        fmt.Sprintf(":%d", Port),
			"RUSTFS_CONSOLE_ENABLE": "false",
		},
		Mounts: []executor.Mount{{Volume: o.DataVolume, Target: "/data"}},
	}
	if o.PublishPort > 0 {
		spec.Ports = []executor.PortBinding{{HostIP: "127.0.0.1", HostPort: o.PublishPort, ContainerPort: Port}}
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%+v", spec)))
	spec.Labels = map[string]string{specLabel: hex.EncodeToString(sum[:8])}
	return spec
}

// State is what the container is doing: "off", "missing", or Docker's state (running, …).
func (s *Service) State(ctx context.Context) string {
	cfg, err := s.Settings(ctx)
	if err != nil || !cfg.Enabled {
		return "off"
	}
	c, err := s.opts.Executor.InspectContainer(ctx, s.opts.Name)
	switch {
	case errors.Is(err, executor.ErrNotFound):
		return "missing"
	case err != nil:
		return "unknown"
	}
	return c.State
}

// client signs in with the root credentials.
func (s *Service) client(ctx context.Context) (*minio.Client, error) {
	cfg, err := s.Settings(ctx)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled || cfg.SecretKey == "" {
		return nil, ErrOff
	}
	return minio.New(s.Endpoint(), &minio.Options{
		Creds:        credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		BucketLookup: minio.BucketLookupPath,
		Region:       "us-east-1",
	})
}

// ErrOff: the built-in storage is not enabled.
var ErrOff = errors.New("built-in storage is off")

// EnsureConnection makes the default bucket and an S3 connection to it once the storage
// answers, so it shows up wherever a storage connection can be picked. A connection
// the user deleted is made again only when the storage is turned on again.
func (s *Service) EnsureConnection(ctx context.Context) error {
	cfg, err := s.Settings(ctx)
	if err != nil || !cfg.Enabled {
		return err
	}
	cl, err := s.client(ctx)
	if err != nil {
		return err
	}
	exists, err := cl.BucketExists(ctx, DefaultBucket)
	if err != nil {
		return fmt.Errorf("object storage not answering yet: %w", err)
	}
	if !exists {
		if err := cl.MakeBucket(ctx, DefaultBucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("create bucket %s: %w", DefaultBucket, err)
		}
	}
	if cfg.ConnectionID != "" {
		return nil // made already, possibly deleted since on purpose
	}
	conn, err := s.opts.Store.CreateS3Connection(ctx, store.S3Input{
		S3Connection: store.S3Connection{Name: ConnectionName, Endpoint: s.Endpoint(), Region: "us-east-1", Bucket: DefaultBucket, AccessKey: cfg.AccessKey, PathStyle: true},
		SecretKey:    cfg.SecretKey,
	})
	if errors.Is(err, store.ErrConflict) {
		return nil // a connection already has that name; leave the user's alone
	}
	if err != nil {
		return err
	}
	cfg.ConnectionID = conn.ID
	return s.opts.Store.SetObjectStoreSettings(ctx, cfg)
}

// Supervise keeps the container as the settings say and makes the connection once it
// answers. It returns when ctx ends.
func (s *Service) Supervise(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	var lastErr string
	for {
		applyCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		err := s.Apply(applyCtx)
		if err == nil {
			err = s.EnsureConnection(applyCtx)
		}
		cancel()
		switch {
		case ctx.Err() != nil:
			return
		case err == nil:
			lastErr = ""
		case err.Error() != lastErr:
			s.opts.Log.Warn("object storage", "error", err)
			lastErr = err.Error()
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Bucket is one bucket with its usage.
type Bucket struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	Size      int64     `json:"size"`
	Objects   int64     `json:"objects"`
	// Truncated: the bucket holds more objects than were counted.
	Truncated bool `json:"truncated"`
}

// maxCounted bounds the objects counted per bucket, so a large bucket cannot stall the page.
const maxCounted = 10_000

func (s *Service) Buckets(ctx context.Context) ([]Bucket, error) {
	cl, err := s.client(ctx)
	if err != nil {
		return nil, err
	}
	infos, err := cl.ListBuckets(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Bucket, 0, len(infos))
	for _, b := range infos {
		bucket := Bucket{Name: b.Name, CreatedAt: b.CreationDate}
		for o := range cl.ListObjects(ctx, b.Name, minio.ListObjectsOptions{Recursive: true}) {
			if o.Err != nil {
				return nil, o.Err
			}
			if bucket.Objects == maxCounted {
				bucket.Truncated = true
				break
			}
			bucket.Objects++
			bucket.Size += o.Size
		}
		out = append(out, bucket)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

var bucketName = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)

// ErrInvalidName: not a valid S3 bucket name.
var ErrInvalidName = errors.New("bucket names are 3–63 characters: lowercase letters, digits, dots and hyphens, starting and ending with a letter or digit")

// ErrNotEmpty: a bucket still holds objects.
var ErrNotEmpty = errors.New("the bucket is not empty")

func (s *Service) CreateBucket(ctx context.Context, name string) error {
	if !bucketName.MatchString(name) || strings.Contains(name, "..") {
		return ErrInvalidName
	}
	cl, err := s.client(ctx)
	if err != nil {
		return err
	}
	if ok, err := cl.BucketExists(ctx, name); err != nil {
		return err
	} else if ok {
		return store.ErrConflict
	}
	return cl.MakeBucket(ctx, name, minio.MakeBucketOptions{})
}

// DeleteBucket removes an empty bucket; it refuses one an S3 connection points at.
func (s *Service) DeleteBucket(ctx context.Context, name string) error {
	cl, err := s.client(ctx)
	if err != nil {
		return err
	}
	if ok, err := cl.BucketExists(ctx, name); err != nil {
		return err
	} else if !ok {
		return store.ErrNotFound
	}
	conns, err := s.opts.Store.ListS3Connections(ctx)
	if err != nil {
		return err
	}
	for _, c := range conns {
		if c.Bucket == name && c.Endpoint == s.Endpoint() {
			return fmt.Errorf("%w: the storage connection %q uses it", store.ErrConflict, c.Name)
		}
	}
	for o := range cl.ListObjects(ctx, name, minio.ListObjectsOptions{Recursive: true, MaxKeys: 1}) {
		if o.Err != nil {
			return o.Err
		}
		return ErrNotEmpty
	}
	return cl.RemoveBucket(ctx, name)
}

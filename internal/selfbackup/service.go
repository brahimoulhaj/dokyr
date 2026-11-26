package selfbackup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/robfig/cron/v3"

	"panel/internal/brand"
	"panel/internal/storage"
	"panel/internal/store"
)

// Folder holds panel backups under the storage connection's prefix.
const Folder = "panel-backups"

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// ParseSchedule checks a 5-field cron expression in UTC.
func ParseSchedule(expr string) (cron.Schedule, error) {
	if len(strings.Fields(expr)) != 5 {
		return nil, errors.New(`use a 5-field cron expression in UTC, like "0 3 * * *"`)
	}
	schedule, err := cronParser.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression: %w", err)
	}
	if schedule.Next(time.Now().UTC()).IsZero() {
		return nil, errors.New("this cron expression never runs")
	}
	return schedule, nil
}

// Objects is the part of S3 a backup needs; tests replace it.
type Objects interface {
	Put(ctx context.Context, key string, r io.Reader) (int64, error)
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	List(ctx context.Context, prefix string) ([]Object, error)
	Delete(ctx context.Context, key string) error
}

// Object is one backup in storage.
type Object struct {
	Key          string    `json:"key"`
	Name         string    `json:"name"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"last_modified"`
}

type Service struct {
	st      *store.Store
	log     *slog.Logger
	version string
	dataDir string
	keyPath string
	started time.Time
	mu      sync.Mutex // one backup at a time
	running atomic.Bool
	// restoring: a restore is being staged, or is staged and waits for the restart.
	restoring atomic.Bool

	// Open returns the objects of a storage connection and the folder for backups;
	// tests replace it.
	Open func(ctx context.Context, connectionID string) (Objects, string, error)
}

func New(st *store.Store, log *slog.Logger, version, dataDir, keyPath string) *Service {
	s := &Service{st: st, log: log, version: version, dataDir: dataDir, keyPath: keyPath, started: time.Now().UTC()}
	s.Open = s.openS3
	return s
}

// Running reports whether a backup is in progress.
func (s *Service) Running() bool { return s.running.Load() }

// ErrBusy: a backup is already running.
var ErrBusy = errors.New("a panel backup is already running")

// Run makes one backup now, uploads it and prunes old ones. The outcome is saved as the
// status and recorded as an event; a failure also feeds the "backup.failed" alert.
func (s *Service) Run(ctx context.Context, actor string) (Object, error) {
	return s.backup(ctx, actor, true)
}

// backup is Run; without prune no older backup is removed, whatever the number kept.
func (s *Service) backup(ctx context.Context, actor string, prune bool) (Object, error) {
	if !s.mu.TryLock() {
		return Object{}, ErrBusy
	}
	defer s.mu.Unlock()
	s.running.Store(true)
	defer s.running.Store(false)
	started := time.Now().UTC()
	obj, err := s.run(ctx, started, prune)

	status, _ := s.st.GetPanelBackupStatus(ctx)
	status.LastRunAt = &started
	event := store.Event{Actor: actor, TargetType: "panel", TargetName: brand.Name}
	if err != nil {
		status.LastError = err.Error()
		event.Action, event.Detail = "backup.failed", err.Error()
		s.log.Error("panel backup", "error", err)
	} else {
		status.LastSuccessAt, status.LastError, status.LastKey, status.LastSize = &started, "", obj.Key, obj.Size
		event.Action, event.Detail = "panel.backup", obj.Name
	}
	// The run's context may be gone (shutdown); still record what happened.
	save, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if serr := s.st.SetPanelBackupStatus(save, status); serr != nil {
		s.log.Error("panel backup status", "error", serr)
	}
	_ = s.st.AddEvent(save, event)
	return obj, err
}

func (s *Service) run(ctx context.Context, started time.Time, prune bool) (Object, error) {
	cfg, err := s.st.GetPanelBackupConfig(ctx)
	if err != nil {
		return Object{}, err
	}
	if cfg.S3ConnectionID == "" {
		return Object{}, errors.New("choose a storage connection for panel backups")
	}
	if len(cfg.Passphrase) < MinPassphrase {
		return Object{}, errors.New("set a backup passphrase first")
	}
	objects, folder, err := s.Open(ctx, cfg.S3ConnectionID)
	if err != nil {
		return Object{}, err
	}

	// The snapshot stays inside the data dir: same disk, same permissions as the original.
	tmp, err := os.MkdirTemp(s.dataDir, ".panel-backup-")
	if err != nil {
		return Object{}, err
	}
	defer os.RemoveAll(tmp)
	snapshot := filepath.Join(tmp, dbName)
	if err := s.st.Snapshot(ctx, snapshot); err != nil {
		return Object{}, fmt.Errorf("snapshot: %w", err)
	}
	schema, err := s.st.SchemaVersion(ctx)
	if err != nil {
		return Object{}, err
	}
	m := Manifest{Product: brand.Slug, PanelVersion: s.version, SchemaVersion: schema, CreatedAt: started}

	name := brand.Slug + "-" + started.Format("20060102T150405Z") + Extension
	key := path.Join(folder, name)
	pr, pw := io.Pipe()
	go func() { pw.CloseWithError(Write(pw, cfg.Passphrase, m, snapshot, s.keyPath)) }()
	size, err := objects.Put(ctx, key, pr)
	pr.CloseWithError(err) // stops the writer if the upload gave up
	if err != nil {
		return Object{}, fmt.Errorf("upload: %w", err)
	}
	if prune {
		if err := s.prune(ctx, objects, folder, cfg.Keep); err != nil {
			// The backup itself is safe; old ones just linger until the next run.
			s.log.Warn("panel backup retention", "error", err)
		}
	}
	return Object{Key: key, Name: name, Size: size, LastModified: started}, nil
}

// prune keeps the newest `keep` backups. Names carry a UTC timestamp, so they sort by age.
func (s *Service) prune(ctx context.Context, objects Objects, folder string, keep int) error {
	if keep <= 0 {
		return nil
	}
	list, err := objects.List(ctx, folder+"/")
	if err != nil {
		return err
	}
	if len(list) <= keep {
		return nil
	}
	for _, o := range list[keep:] {
		if err := objects.Delete(ctx, o.Key); err != nil {
			return err
		}
	}
	return nil
}

// List returns the panel backups in storage, newest first.
func (s *Service) List(ctx context.Context) ([]Object, error) {
	cfg, err := s.st.GetPanelBackupConfig(ctx)
	if err != nil || cfg.S3ConnectionID == "" {
		return []Object{}, nil
	}
	objects, folder, err := s.Open(ctx, cfg.S3ConnectionID)
	if err != nil {
		return nil, err
	}
	return objects.List(ctx, folder+"/")
}

// Download opens one backup from the configured storage. Only names in the backups
// folder are accepted.
func (s *Service) Download(ctx context.Context, name string) (io.ReadCloser, error) {
	if !validName(name) {
		return nil, store.ErrNotFound
	}
	cfg, err := s.st.GetPanelBackupConfig(ctx)
	if err != nil {
		return nil, err
	}
	objects, folder, err := s.Open(ctx, cfg.S3ConnectionID)
	if err != nil {
		return nil, err
	}
	return objects.Get(ctx, path.Join(folder, name))
}

func validName(name string) bool {
	return strings.HasPrefix(name, brand.Slug+"-") && strings.HasSuffix(name, Extension) &&
		!strings.ContainsAny(name, `/\`) && len(name) < 128
}

// Watch runs scheduled backups until ctx ends. It checks once a minute; a missed slot
// (panel stopped) runs once at start-up, not once per slot.
func (s *Service) Watch(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		if s.due(ctx) {
			if _, err := s.Run(ctx, "system"); err != nil && !errors.Is(err, ErrBusy) && ctx.Err() == nil {
				s.log.Warn("scheduled panel backup failed", "error", err)
			}
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			return
		}
	}
}

func (s *Service) due(ctx context.Context) bool {
	cfg, err := s.st.GetPanelBackupConfig(ctx)
	if err != nil || !cfg.Enabled {
		return false
	}
	schedule, err := ParseSchedule(cfg.Schedule)
	if err != nil {
		return false
	}
	status, err := s.st.GetPanelBackupStatus(ctx)
	if err != nil {
		return false
	}
	from := s.started
	if status.LastRunAt != nil {
		from = *status.LastRunAt
	}
	return !schedule.Next(from).After(time.Now().UTC())
}

// NextRun is when the schedule runs next, or nil when off.
func (s *Service) NextRun(ctx context.Context) *time.Time {
	cfg, err := s.st.GetPanelBackupConfig(ctx)
	if err != nil || !cfg.Enabled {
		return nil
	}
	schedule, err := ParseSchedule(cfg.Schedule)
	if err != nil {
		return nil
	}
	next := schedule.Next(time.Now().UTC())
	return &next
}

type s3Objects struct {
	client *minio.Client
	bucket string
}

func (s *Service) openS3(ctx context.Context, connectionID string) (Objects, string, error) {
	c, err := s.st.S3Secret(ctx, connectionID)
	if err != nil {
		return nil, "", err
	}
	client, err := storage.Client(c)
	if err != nil {
		return nil, "", err
	}
	return s3Objects{client, c.Bucket}, path.Join(strings.Trim(c.Prefix, "/"), Folder), nil
}

func (o s3Objects) Put(ctx context.Context, key string, r io.Reader) (int64, error) {
	info, err := o.client.PutObject(ctx, o.bucket, key, r, -1, minio.PutObjectOptions{ContentType: "application/octet-stream"})
	return info.Size, err
}

func (o s3Objects) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := o.client.GetObject(ctx, o.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	if _, err := obj.Stat(); err != nil {
		obj.Close()
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	return obj, nil
}

func (o s3Objects) List(ctx context.Context, prefix string) ([]Object, error) {
	out := []Object{}
	for info := range o.client.ListObjects(ctx, o.bucket, minio.ListObjectsOptions{Prefix: prefix}) {
		if info.Err != nil {
			return nil, info.Err
		}
		name := path.Base(info.Key)
		if !validName(name) {
			continue
		}
		out = append(out, Object{Key: info.Key, Name: name, Size: info.Size, LastModified: info.LastModified})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out, nil
}

func (o s3Objects) Delete(ctx context.Context, key string) error {
	return o.client.RemoveObject(ctx, o.bucket, key, minio.RemoveObjectOptions{})
}

// Package volumebackup backs up and restores the named volumes of apps. A backup is a
// gzip-compressed tar of the volume's content in S3 storage, readable without the panel:
//
//	gunzip < backup.tar.gz | tar -x -C /somewhere
//
// The volume is read and written from a short-lived helper container that mounts it, so
// neither the app's image nor its state matters: no tar in the image is needed, and a
// stopped app is backed up like a running one.
package volumebackup

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"strings"
	"sync"
	"time"

	"panel/internal/brand"
	"panel/internal/executor"
	"panel/internal/id"
	"panel/internal/store"
)

// Folder holds the volume backups under an S3 connection's prefix.
const Folder = "volume-backups"

var (
	// ErrBusy: the volume is being backed up or restored right now.
	ErrBusy = errors.New("this volume is being backed up or restored")
	// ErrNotVolume: the app mounts no named volume of that name.
	ErrNotVolume = errors.New("the app has no volume of that name")
	// ErrRunning: a restore replaces the content under a running app.
	ErrRunning = errors.New("stop the app before restoring one of its volumes")
	// ErrNotReady: the backup did not complete, so there is nothing to restore.
	ErrNotReady = errors.New("this backup did not complete")
	// ErrAppGone: the backup's app no longer exists, so its volume cannot be restored into.
	ErrAppGone = errors.New("the app of this backup no longer exists")
)

// Host is what the service needs from the container host.
type Host interface {
	PullImage(ctx context.Context, ref string, auth *executor.RegistryAuth, onProgress func(executor.PullProgress)) error
	CreateContainer(ctx context.Context, spec executor.ContainerSpec) (string, error)
	StartContainer(ctx context.Context, id string) error
	RemoveContainer(ctx context.Context, id string, force bool) error
	ListContainers(ctx context.Context) ([]executor.Container, error)
	ExecStream(ctx context.Context, id string, cmd executor.Command, in io.Reader, out io.Writer) error
}

// Objects is the storage a backup goes to.
type Objects interface {
	Put(ctx context.Context, key string, r io.Reader) (int64, error)
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

// Service backs up and restores volumes. Operations run in the background and end in
// the backup's status and the activity log.
type Service struct {
	st    *store.Store
	host  Host
	image string // of the helper container; it must have sh, tar and find (BusyBox does)
	log   *slog.Logger
	// Open returns the storage of an S3 connection and the folder prefix inside it.
	Open func(ctx context.Context, connectionID string) (Objects, string, error)

	mu   sync.Mutex
	busy map[string]bool // by volume name
	wg   sync.WaitGroup
}

func New(st *store.Store, host Host, image string, log *slog.Logger, open func(ctx context.Context, connectionID string) (Objects, string, error)) *Service {
	return &Service{st: st, host: host, image: image, log: log, Open: open, busy: map[string]bool{}}
}

// Wait blocks until the operations in flight have ended (tests, shutdown).
func (s *Service) Wait() { s.wg.Wait() }

// Volumes names the named volumes an app mounts: what can be backed up.
func Volumes(app store.App) []string {
	out := []string{}
	for _, m := range app.Mounts {
		if m.Type == "volume" && m.Source != "" && !contains(out, m.Source) {
			out = append(out, m.Source)
		}
	}
	return out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func (s *Service) lock(volume string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy[volume] {
		return false
	}
	s.busy[volume] = true
	return true
}

func (s *Service) unlock(volume string) {
	s.mu.Lock()
	delete(s.busy, volume)
	s.mu.Unlock()
}

// Start begins a backup of one of the app's volumes to an S3 connection and returns its
// record, still running. Only a volume the app mounts can be named, so a backup never
// reaches a volume of another app or of the panel.
func (s *Service) Start(ctx context.Context, app store.App, volume, connectionID, actor string) (store.VolumeBackup, error) {
	if !contains(Volumes(app), volume) {
		return store.VolumeBackup{}, ErrNotVolume
	}
	objects, prefix, err := s.Open(ctx, connectionID)
	if err != nil {
		return store.VolumeBackup{}, err
	}
	if !s.lock(volume) {
		return store.VolumeBackup{}, ErrBusy
	}
	started := time.Now().UTC()
	// The time for people, the id so two backups of one second never share an object:
	// a failed backup deletes its object, which must not be another backup's.
	key := path.Join(prefix, Folder, volume, volume+"-"+started.Format("20060102T150405Z")+"-"+strings.ToLower(id.New())+".tar.gz")
	b, err := s.st.CreateVolumeBackup(ctx, app.ID, app.Name, volume, connectionID, key)
	if err != nil {
		s.unlock(volume)
		return store.VolumeBackup{}, err
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.unlock(volume)
		// Outlives the request; a backup that never finishes gives up after six hours.
		run, cancel := context.WithTimeout(context.WithoutCancel(ctx), 6*time.Hour)
		defer cancel()
		size, err := s.archive(run, objects, key, volume)
		failure := ""
		event := store.Event{Actor: actor, Action: "volume.backup", TargetType: "app", TargetID: app.ID, TargetName: app.Name, Detail: volume}
		if err != nil {
			failure = err.Error()
			event.Action, event.Detail = "volume.backup_failed", volume+": "+failure
			s.log.Error("volume backup", "volume", volume, "error", err)
			_ = objects.Delete(run, key) // a partial upload is of no use
		}
		save, cancelSave := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancelSave()
		if err := s.st.FinishVolumeBackup(save, b.ID, size, failure); err != nil {
			s.log.Error("volume backup status", "error", err)
		}
		_ = s.st.AddEvent(save, event)
	}()
	return b, nil
}

// helper runs fn with a helper container that mounts the volume at /data, then removes it.
func (s *Service) helper(ctx context.Context, volume string, readOnly bool, fn func(container string) error) error {
	if err := s.host.PullImage(ctx, s.image, nil, nil); err != nil {
		return fmt.Errorf("helper image: %w", err)
	}
	name := brand.Slug + "-volume-" + strings.ToLower(id.New())
	cid, err := s.host.CreateContainer(ctx, executor.ContainerSpec{
		Name: name, Image: s.image, Kind: executor.KindService, ResourceID: "volume-backup",
		Entrypoint: []string{"sleep"}, Command: []string{"21600"},
		Mounts: []executor.Mount{{Volume: volume, Target: "/data", ReadOnly: readOnly}},
	})
	if err != nil {
		return fmt.Errorf("helper container: %w", err)
	}
	defer func() {
		rm, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if err := s.host.RemoveContainer(rm, cid, true); err != nil {
			s.log.Warn("remove volume helper", "container", name, "error", err)
		}
	}()
	if err := s.host.StartContainer(ctx, cid); err != nil {
		return fmt.Errorf("helper container: %w", err)
	}
	return fn(cid)
}

// archive streams the volume as tar through gzip into storage and returns the stored size.
func (s *Service) archive(ctx context.Context, objects Objects, key, volume string) (int64, error) {
	var size int64
	err := s.helper(ctx, volume, true, func(cid string) error {
		pr, pw := io.Pipe()
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		done := make(chan error, 1)
		go func() {
			z := gzip.NewWriter(pw)
			err := s.host.ExecStream(ctx, cid, executor.Command{Args: []string{"tar", "-cf", "-", "-C", "/data", "."}}, nil, z)
			if cerr := z.Close(); err == nil {
				err = cerr
			}
			pw.CloseWithError(err)
			done <- err
		}()
		n, putErr := objects.Put(ctx, key, pr)
		pr.CloseWithError(putErr)
		if putErr != nil {
			cancel()
		}
		if tarErr := <-done; tarErr != nil {
			return fmt.Errorf("read the volume: %w", tarErr)
		}
		if putErr != nil {
			return fmt.Errorf("upload: %w", putErr)
		}
		size = n
		return nil
	})
	return size, err
}

// Restore replaces the content of the backup's volume with the backup, in the background.
// The app must still exist, still mount the volume, and have no running container: the
// volume is emptied first, which would pull the data from under a running app.
func (s *Service) Restore(ctx context.Context, backupID, actor string) (store.VolumeBackup, error) {
	b, err := s.st.GetVolumeBackup(ctx, backupID)
	if err != nil {
		return b, err
	}
	if b.Status != store.VolumeBackupReady {
		return b, ErrNotReady
	}
	if b.AppID == "" {
		return b, ErrAppGone
	}
	app, err := s.st.GetApp(ctx, b.AppID)
	if errors.Is(err, store.ErrNotFound) {
		return b, ErrAppGone
	} else if err != nil {
		return b, err
	}
	if !contains(Volumes(app), b.Volume) {
		return b, ErrNotVolume
	}
	list, err := s.host.ListContainers(ctx)
	if err != nil {
		return b, err
	}
	for _, c := range list {
		if c.Kind == executor.KindApp && c.ResourceID == app.ID && c.Running {
			return b, ErrRunning
		}
	}
	objects, _, err := s.Open(ctx, b.S3ConnectionID)
	if err != nil {
		return b, err
	}
	if !s.lock(b.Volume) {
		return b, ErrBusy
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.unlock(b.Volume)
		run, cancel := context.WithTimeout(context.WithoutCancel(ctx), 6*time.Hour)
		defer cancel()
		err := s.extract(run, objects, b.ObjectKey, b.Volume)
		event := store.Event{Actor: actor, Action: "volume.restore", TargetType: "app", TargetID: app.ID, TargetName: app.Name, Detail: b.Volume}
		if err != nil {
			event.Action, event.Detail = "volume.restore_failed", b.Volume+": "+err.Error()
			s.log.Error("volume restore", "volume", b.Volume, "error", err)
		}
		save, cancelSave := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancelSave()
		_ = s.st.AddEvent(save, event)
	}()
	return b, nil
}

// extract empties the volume and unpacks the backup into it. The backup is read to its
// end, gzip checksum included, before the result counts: a truncated object fails.
func (s *Service) extract(ctx context.Context, objects Objects, key, volume string) error {
	obj, err := objects.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer obj.Close()
	z, err := gzip.NewReader(obj)
	if err != nil {
		return fmt.Errorf("not a volume backup: %w", err)
	}
	defer z.Close()
	return s.helper(ctx, volume, false, func(cid string) error {
		cmd := executor.Command{Args: []string{"sh", "-c", "find /data -mindepth 1 -delete && tar -xf - -C /data"}}
		if err := s.host.ExecStream(ctx, cid, cmd, z, io.Discard); err != nil {
			return fmt.Errorf("write the volume: %w", err)
		}
		return nil
	})
}

// Delete removes a backup: its object, then its record. A missing object is not an error.
func (s *Service) Delete(ctx context.Context, backupID string) (store.VolumeBackup, error) {
	b, err := s.st.GetVolumeBackup(ctx, backupID)
	if err != nil {
		return b, err
	}
	if b.Status == store.VolumeBackupRunning {
		return b, ErrBusy
	}
	if objects, _, err := s.Open(ctx, b.S3ConnectionID); err != nil {
		return b, err
	} else if err := objects.Delete(ctx, b.ObjectKey); err != nil {
		return b, fmt.Errorf("delete from storage: %w", err)
	}
	return b, s.st.DeleteVolumeBackup(ctx, b.ID)
}

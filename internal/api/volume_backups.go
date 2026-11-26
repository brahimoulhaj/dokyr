package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"panel/internal/store"
	"panel/internal/volumebackup"
)

// VolumeBackups backs up and restores the named volumes of apps (see volumebackup.Service).
type VolumeBackups interface {
	Start(ctx context.Context, app store.App, volume, connectionID, actor string) (store.VolumeBackup, error)
	Restore(ctx context.Context, backupID, actor string) (store.VolumeBackup, error)
	Delete(ctx context.Context, backupID string) (store.VolumeBackup, error)
}

// SetVolumeBackups gives the server the volume backups.
func (s *Server) SetVolumeBackups(v VolumeBackups) { s.volumeBackups = v }

type volumeBackupJSON struct {
	ID             string     `json:"id"`
	AppID          string     `json:"app_id"`
	Volume         string     `json:"volume"`
	S3ConnectionID string     `json:"s3_connection_id"`
	ObjectKey      string     `json:"object_key"`
	Size           int64      `json:"size"`
	Status         string     `json:"status"` // "running", "ready" or "failed"
	Error          string     `json:"error"`
	CreatedAt      time.Time  `json:"created_at"`
	FinishedAt     *time.Time `json:"finished_at"`
}

func toVolumeBackupJSON(b store.VolumeBackup) volumeBackupJSON {
	return volumeBackupJSON{ID: b.ID, AppID: b.AppID, Volume: b.Volume, S3ConnectionID: b.S3ConnectionID, ObjectKey: b.ObjectKey,
		Size: b.Size, Status: b.Status, Error: b.Error, CreatedAt: b.CreatedAt, FinishedAt: b.FinishedAt}
}

func (s *Server) requireVolumeBackups(w http.ResponseWriter) bool {
	if s.volumeBackups == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "volume backups are not available")
		return false
	}
	return true
}

// volumeBackupError answers the service's refusals; false when err is nil.
func (s *Server) volumeBackupError(w http.ResponseWriter, what string, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, volumebackup.ErrNotVolume):
		invalid(w, err.Error())
	case errors.Is(err, volumebackup.ErrBusy), errors.Is(err, volumebackup.ErrRunning),
		errors.Is(err, volumebackup.ErrNotReady), errors.Is(err, volumebackup.ErrAppGone):
		writeError(w, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "not found")
	default:
		s.log.Error(what, "error", err)
		writeError(w, http.StatusBadGateway, "storage_error", "the storage or the container host could not be reached")
	}
	return true
}

// handleAppVolumeBackups answers the app's named volumes and their backups, newest first.
func (s *Server) handleAppVolumeBackups(w http.ResponseWriter, r *http.Request) {
	app, err := s.store.GetApp(r.Context(), r.PathValue("id"))
	if s.storeError(w, "get app", err, "") {
		return
	}
	list, err := s.store.AppVolumeBackups(r.Context(), app.ID)
	if err != nil {
		s.internal(w, "list volume backups", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"volumes": volumebackup.Volumes(app),
		"backups": mapSlice(list, toVolumeBackupJSON),
	})
}

// handleStartVolumeBackup starts a backup of one of the app's volumes; it answers the
// record while the backup runs on.
func (s *Server) handleStartVolumeBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requireVolumeBackups(w) {
		return
	}
	var in struct {
		Volume         string `json:"volume"`
		S3ConnectionID string `json:"s3_connection_id"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	app, err := s.store.GetApp(r.Context(), r.PathValue("id"))
	if s.storeError(w, "get app", err, "") {
		return
	}
	if _, err := s.store.GetS3Connection(r.Context(), in.S3ConnectionID); errors.Is(err, store.ErrNotFound) {
		invalid(w, "s3_connection_id must be an existing storage connection")
		return
	} else if err != nil {
		s.internal(w, "get s3 connection", err)
		return
	}
	b, err := s.volumeBackups.Start(r.Context(), app, in.Volume, in.S3ConnectionID, userFrom(r.Context()).Email)
	if s.volumeBackupError(w, "start volume backup", err) {
		return
	}
	writeJSON(w, http.StatusAccepted, toVolumeBackupJSON(b))
}

// handleRestoreVolumeBackup replaces the volume's content with a backup. The app must be
// stopped; the outcome lands in the activity log.
func (s *Server) handleRestoreVolumeBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requireVolumeBackups(w) {
		return
	}
	b, err := s.volumeBackups.Restore(r.Context(), r.PathValue("id"), userFrom(r.Context()).Email)
	if s.volumeBackupError(w, "restore volume backup", err) {
		return
	}
	writeJSON(w, http.StatusAccepted, toVolumeBackupJSON(b))
}

func (s *Server) handleDeleteVolumeBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requireVolumeBackups(w) {
		return
	}
	b, err := s.volumeBackups.Delete(r.Context(), r.PathValue("id"))
	if s.volumeBackupError(w, "delete volume backup", err) {
		return
	}
	s.audit(r, "volume.backup_delete", "app", b.AppID, b.AppName, b.Volume)
	w.WriteHeader(http.StatusNoContent)
}

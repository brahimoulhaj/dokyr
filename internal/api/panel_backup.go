package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"panel/internal/brand"
	"panel/internal/selfbackup"
	"panel/internal/store"
)

// PanelBackups backs up the panel's own database and key.
type PanelBackups interface {
	Run(ctx context.Context, actor string) (selfbackup.Object, error)
	List(ctx context.Context) ([]selfbackup.Object, error)
	Download(ctx context.Context, name string) (io.ReadCloser, error)
	NextRun(ctx context.Context) *time.Time
	Running() bool
}

func (s *Server) SetPanelBackups(b PanelBackups) { s.panelBackups = b }

type panelBackupJSON struct {
	Enabled        bool                    `json:"enabled"`
	S3ConnectionID string                  `json:"s3_connection_id"`
	Schedule       string                  `json:"schedule"`
	Keep           int                     `json:"keep"`
	HasPassphrase  bool                    `json:"has_passphrase"`
	Configured     bool                    `json:"configured"`
	NextRunAt      *time.Time              `json:"next_run_at"`
	Status         store.PanelBackupStatus `json:"status"`
	Running        bool                    `json:"running"`
}

func (s *Server) requirePanelBackups(w http.ResponseWriter) bool {
	if s.panelBackups == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "panel backups are not available")
		return false
	}
	return true
}

func (s *Server) handleGetPanelBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requirePanelBackups(w) {
		return
	}
	ctx := r.Context()
	out := panelBackupJSON{Schedule: "0 3 * * *", Keep: 14}
	cfg, err := s.store.GetPanelBackupConfig(ctx)
	switch {
	case err == nil:
		out.Configured = true
		out.Enabled, out.S3ConnectionID, out.Schedule, out.Keep = cfg.Enabled, cfg.S3ConnectionID, cfg.Schedule, cfg.Keep
		out.HasPassphrase = cfg.Passphrase != ""
	case !errors.Is(err, store.ErrNoBackupConfig):
		s.internal(w, "panel backup settings", err)
		return
	}
	if out.Status, err = s.store.GetPanelBackupStatus(ctx); err != nil {
		s.internal(w, "panel backup status", err)
		return
	}
	out.NextRunAt = s.panelBackups.NextRun(ctx)
	out.Running = s.panelBackups.Running()
	writeJSON(w, http.StatusOK, out)
}

type panelBackupInput struct {
	Enabled        bool   `json:"enabled"`
	S3ConnectionID string `json:"s3_connection_id"`
	Schedule       string `json:"schedule"`
	Keep           int    `json:"keep"`
	// Passphrase: empty keeps the saved one.
	Passphrase string `json:"passphrase"`
}

func (s *Server) handlePutPanelBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requirePanelBackups(w) {
		return
	}
	var in panelBackupInput
	if !readJSON(w, r, &in) {
		return
	}
	ctx := r.Context()
	in.Schedule = strings.TrimSpace(in.Schedule)
	if _, err := selfbackup.ParseSchedule(in.Schedule); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_schedule", err.Error())
		return
	}
	if in.Keep < 1 || in.Keep > 365 {
		writeError(w, http.StatusUnprocessableEntity, "invalid_keep", "keep between 1 and 365 backups")
		return
	}
	conn, err := s.store.GetS3Connection(ctx, in.S3ConnectionID)
	if s.storeError(w, "panel backup storage", err, "") {
		return
	}
	if in.Passphrase != "" && len(in.Passphrase) < selfbackup.MinPassphrase {
		writeError(w, http.StatusUnprocessableEntity, "weak_passphrase", "use a passphrase of at least 12 characters")
		return
	}
	old, err := s.store.GetPanelBackupConfig(ctx)
	if err != nil && !errors.Is(err, store.ErrNoBackupConfig) {
		s.internal(w, "panel backup settings", err)
		return
	}
	if in.Passphrase == "" && old.Passphrase == "" {
		writeError(w, http.StatusUnprocessableEntity, "passphrase_required", "set a passphrase: it is needed to restore a backup")
		return
	}
	cfg := store.PanelBackupConfig{Enabled: in.Enabled, S3ConnectionID: conn.ID, Schedule: in.Schedule, Keep: in.Keep, Passphrase: in.Passphrase}
	if err := s.store.SetPanelBackupConfig(ctx, cfg); err != nil {
		s.internal(w, "save panel backup settings", err)
		return
	}
	detail := "storage " + conn.Name + ", " + in.Schedule
	if !in.Enabled {
		detail = "schedule off, storage " + conn.Name
	}
	if in.Passphrase != "" && old.Passphrase != "" && in.Passphrase != old.Passphrase {
		detail += ", passphrase changed"
	}
	s.audit(r, "panel.backup_settings", "panel", "", brand.Name, detail)
	s.handleGetPanelBackup(w, r)
}

// handleRunPanelBackup starts a backup and answers at once; the change stream and the
// status say when it is done.
func (s *Server) handleRunPanelBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requirePanelBackups(w) {
		return
	}
	if s.panelBackups.Running() {
		writeError(w, http.StatusConflict, "busy", selfbackup.ErrBusy.Error())
		return
	}
	actor := userFrom(r.Context()).Email
	go func() {
		// Outlives the request; a backup that never finishes gives up after an hour.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), time.Hour)
		defer cancel()
		_, _ = s.panelBackups.Run(ctx, actor) // outcome lands in the status and the activity log
	}()
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleListPanelBackups(w http.ResponseWriter, r *http.Request) {
	if !s.requirePanelBackups(w) {
		return
	}
	list, err := s.panelBackups.List(r.Context())
	if err != nil {
		s.log.Warn("list panel backups", "error", err)
		writeError(w, http.StatusBadGateway, "storage_error", "the backups could not be listed from storage: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleDownloadPanelBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requirePanelBackups(w) {
		return
	}
	name := r.PathValue("name")
	rc, err := s.panelBackups.Download(r.Context(), name)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such backup")
		return
	}
	if err != nil {
		s.log.Warn("download panel backup", "error", err)
		writeError(w, http.StatusBadGateway, "storage_error", "the backup could not be read from storage")
		return
	}
	defer rc.Close()
	s.audit(r, "panel.backup_download", "panel", "", brand.Name, name)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.Copy(w, rc)
}

package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"panel/internal/account"
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
	// Inspect decrypts and checks a backup in storage without keeping anything.
	Inspect(ctx context.Context, name, passphrase string) (selfbackup.Manifest, error)
	// Restore stages a backup to replace the database and key at the next start.
	Restore(ctx context.Context, name, passphrase, actor string) (selfbackup.Manifest, error)
}

func (s *Server) SetPanelBackups(b PanelBackups) { s.panelBackups = b }

// SetRestart gives the server a way to restart the panel, which finishes a restore.
// Without one, a restore is staged and the answer says a restart is still needed.
func (s *Server) SetRestart(restart func()) { s.restart = restart }

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

type panelRestoreJSON struct {
	Name          string    `json:"name"`
	CreatedAt     time.Time `json:"created_at"`
	PanelVersion  string    `json:"panel_version"`
	SchemaVersion int       `json:"schema_version"`
	// Restarting: the panel restarts on its own to finish; false = restart it by hand.
	Restarting bool `json:"restarting"`
}

// panelRestoreInput reads the passphrase of a restore request. Rolling the panel back
// replaces every account, secret and setting, so only the owner may, signed in (no
// token), and only with the passphrase of that backup.
func (s *Server) panelRestoreInput(w http.ResponseWriter, r *http.Request) (string, bool) {
	if !s.requirePanelBackups(w) {
		return "", false
	}
	if userFrom(r.Context()).Role != account.RoleOwner {
		writeError(w, http.StatusForbidden, "forbidden", "only the owner can restore the panel")
		return "", false
	}
	var in struct {
		Passphrase string `json:"passphrase"`
	}
	if !readJSON(w, r, &in) {
		return "", false
	}
	if in.Passphrase == "" {
		writeError(w, http.StatusUnprocessableEntity, "passphrase_required", "enter the passphrase of this backup")
		return "", false
	}
	return in.Passphrase, true
}

func (s *Server) panelRestoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "no such backup")
	case errors.Is(err, selfbackup.ErrPassphrase):
		writeError(w, http.StatusUnprocessableEntity, "wrong_passphrase", "wrong passphrase, or not a panel backup")
	case errors.Is(err, selfbackup.ErrNewer), errors.Is(err, selfbackup.ErrForeign):
		writeError(w, http.StatusUnprocessableEntity, "incompatible", err.Error())
	case errors.Is(err, selfbackup.ErrRestoring), errors.Is(err, selfbackup.ErrBusy):
		writeError(w, http.StatusConflict, "busy", err.Error())
	default:
		s.log.Warn("panel restore", "error", err)
		writeError(w, http.StatusBadGateway, "restore_failed", "the backup could not be restored: "+err.Error())
	}
}

// handleInspectPanelBackup checks a backup in storage with its passphrase and tells what
// it holds, before the owner decides to restore it.
func (s *Server) handleInspectPanelBackup(w http.ResponseWriter, r *http.Request) {
	passphrase, ok := s.panelRestoreInput(w, r)
	if !ok {
		return
	}
	name := r.PathValue("name")
	m, err := s.panelBackups.Inspect(r.Context(), name, passphrase)
	if err != nil {
		s.panelRestoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, panelRestoreJSON{Name: name, CreatedAt: m.CreatedAt, PanelVersion: m.PanelVersion, SchemaVersion: m.SchemaVersion})
}

// handleRestorePanelBackup rolls the panel back to a backup in storage: the backup is
// checked, the current state is backed up, the backup is staged, and the panel restarts
// on it. Refused while a deployment runs, which the restart would cut off.
func (s *Server) handleRestorePanelBackup(w http.ResponseWriter, r *http.Request) {
	passphrase, ok := s.panelRestoreInput(w, r)
	if !ok {
		return
	}
	if n, err := s.store.CountActiveDeployments(r.Context()); err != nil {
		s.internal(w, "panel restore", err)
		return
	} else if n > 0 {
		writeError(w, http.StatusConflict, "busy", "a deployment is in progress; restore once it has ended")
		return
	}
	name := r.PathValue("name")
	// Not cut short by a closed tab: it ends staged, or with nothing changed.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), time.Hour)
	defer cancel()
	m, err := s.panelBackups.Restore(ctx, name, passphrase, userFrom(r.Context()).Email)
	if err != nil {
		s.panelRestoreError(w, err)
		return
	}
	s.log.Warn("panel restore staged", "backup", name, "by", userFrom(r.Context()).Email)
	out := panelRestoreJSON{Name: name, CreatedAt: m.CreatedAt, PanelVersion: m.PanelVersion, SchemaVersion: m.SchemaVersion, Restarting: s.restart != nil}
	writeJSON(w, http.StatusAccepted, out)
	if s.restart != nil {
		go func() {
			time.Sleep(time.Second) // let the answer reach the browser
			s.restart()
		}()
	}
}

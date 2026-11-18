package api

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"panel/internal/hostcron"
	"panel/internal/store"
)

// CronJobs starts runs of cron jobs and knows which are in progress.
type CronJobs interface {
	Start(ctx context.Context, j store.CronJob, source, actor string) (store.CronRun, error)
	Running(jobID string) bool
}

func (s *Server) SetCronJobs(c CronJobs) { s.cronJobs = c }

type cronRunJSON struct {
	ID         string     `json:"id"`
	Source     string     `json:"source"`
	Actor      string     `json:"actor"`
	Status     string     `json:"status"` // running, ok or failed
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	ExitCode   int        `json:"exit_code"`
	Error      string     `json:"error"`
	Output     string     `json:"output"`
}

func toCronRunJSON(r store.CronRun) cronRunJSON {
	status := "running"
	if r.FinishedAt != nil {
		status = "ok"
		if r.ExitCode != 0 || r.Error != "" {
			status = "failed"
		}
	}
	return cronRunJSON{ID: r.ID, Source: r.Source, Actor: r.Actor, Status: status, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt, ExitCode: r.ExitCode, Error: r.Error, Output: r.Output}
}

type cronJobJSON struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Schedule  string       `json:"schedule"`
	Command   string       `json:"command"`
	User      string       `json:"user"`
	Enabled   bool         `json:"enabled"`
	NextRunAt *time.Time   `json:"next_run_at"` // null while the job or the feature is off
	LastRun   *cronRunJSON `json:"last_run"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

func (s *Server) cronJobJSON(j store.CronJob, last *store.CronRun, on bool) cronJobJSON {
	out := cronJobJSON{ID: j.ID, Name: j.Name, Schedule: j.Schedule, Command: j.Command, User: j.User, Enabled: j.Enabled, CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt}
	if on && j.Enabled {
		out.NextRunAt = hostcron.Next(j.Schedule, time.Now())
	}
	if last != nil {
		run := toCronRunJSON(*last)
		out.LastRun = &run
	}
	return out
}

// cronGate is what every cron job handler needs first: the service, and the host terminal
// settings, whose off switch and run-as user these jobs share. With change it also refuses
// while host access is off or the password is not confirmed: a job is a command on the host.
func (s *Server) cronGate(w http.ResponseWriter, r *http.Request, change bool) (hostTerminalSettings, bool) {
	if s.cronJobs == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "cron jobs are unavailable")
		return hostTerminalSettings{}, false
	}
	cfg, err := s.hostTerminalSettings(r.Context())
	if err != nil {
		s.internal(w, "host terminal settings", err)
		return cfg, false
	}
	if !change {
		return cfg, true
	}
	if !cfg.Enabled {
		writeError(w, http.StatusForbidden, "disabled", "host access is switched off in Settings; cron jobs do not run")
		return cfg, false
	}
	if !s.hostUnlocks.ok(sessionHash(r)) {
		writeError(w, http.StatusForbidden, "confirm_required", "confirm your password to change or run cron jobs")
		return cfg, false
	}
	return cfg, true
}

func (s *Server) handleListCronJobs(w http.ResponseWriter, r *http.Request) {
	cfg, ok := s.cronGate(w, r, false)
	if !ok {
		return
	}
	jobs, err := s.store.ListCronJobs(r.Context())
	if err != nil {
		s.internal(w, "list cron jobs", err)
		return
	}
	last, err := s.store.LastCronRuns(r.Context())
	if err != nil {
		s.internal(w, "last cron runs", err)
		return
	}
	out := make([]cronJobJSON, 0, len(jobs))
	for _, j := range jobs {
		var run *store.CronRun
		if l, ok := last[j.ID]; ok {
			run = &l
		}
		out = append(out, s.cronJobJSON(j, run, cfg.Enabled))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"jobs": out, "enabled": cfg.Enabled, "default_user": cfg.User,
		"unlocked": s.hostUnlocks.ok(sessionHash(r)),
	})
}

type cronJobInput struct {
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
	User     string `json:"user"` // empty: the run-as user of Settings; root has to be named
	Enabled  *bool  `json:"enabled"`
}

// cronJobFrom validates the input over base (the saved job on an update).
func cronJobFrom(w http.ResponseWriter, in cronJobInput, base store.CronJob, cfg hostTerminalSettings) (store.CronJob, bool) {
	base.Name, base.Schedule, base.Command = in.Name, in.Schedule, in.Command
	// Never root by omission: with no run-as user in Settings, the job has to name one.
	base.User = cmp.Or(strings.TrimSpace(in.User), cfg.User)
	if base.User == "" {
		invalid(w, "choose the user this job runs as; type root to run it as root")
		return base, false
	}
	if in.Enabled != nil {
		base.Enabled = *in.Enabled
	}
	j, err := hostcron.Check(base)
	if err != nil {
		invalid(w, strings.TrimPrefix(err.Error(), hostcron.ErrInvalid.Error()+": "))
		return j, false
	}
	return j, true
}

func (s *Server) handleCreateCronJob(w http.ResponseWriter, r *http.Request) {
	var in cronJobInput
	if !readJSON(w, r, &in) {
		return
	}
	cfg, ok := s.cronGate(w, r, true)
	if !ok {
		return
	}
	j, ok := cronJobFrom(w, in, store.CronJob{Enabled: true}, cfg)
	if !ok {
		return
	}
	j, err := s.store.CreateCronJob(r.Context(), j)
	if err != nil {
		s.internal(w, "create cron job", err)
		return
	}
	s.audit(r, "cron_job.create", "cron_job", j.ID, hostcron.Label(j), cronDetail(j))
	writeJSON(w, http.StatusCreated, s.cronJobJSON(j, nil, true))
}

func cronDetail(j store.CronJob) string {
	detail := j.Schedule + ", as " + j.User
	if !j.Enabled {
		detail += ", off"
	}
	return detail
}

func (s *Server) handleUpdateCronJob(w http.ResponseWriter, r *http.Request) {
	var in cronJobInput
	if !readJSON(w, r, &in) {
		return
	}
	cfg, ok := s.cronGate(w, r, true)
	if !ok {
		return
	}
	saved, err := s.store.GetCronJob(r.Context(), r.PathValue("id"))
	if s.storeError(w, "get cron job", err, "") {
		return
	}
	j, ok := cronJobFrom(w, in, saved, cfg)
	if !ok {
		return
	}
	j, err = s.store.UpdateCronJob(r.Context(), j)
	if s.storeError(w, "update cron job", err, "") {
		return
	}
	s.audit(r, "cron_job.update", "cron_job", j.ID, hostcron.Label(j), cronDetail(j))
	writeJSON(w, http.StatusOK, s.cronJobJSON(j, nil, true))
}

// handleEnableCronJob switches a job on or off. Off needs no password and works while
// host access is off: it only takes power away.
func (s *Server) handleEnableCronJob(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	cfg, ok := s.cronGate(w, r, in.Enabled)
	if !ok {
		return
	}
	j, err := s.store.GetCronJob(r.Context(), r.PathValue("id"))
	if s.storeError(w, "get cron job", err, "") {
		return
	}
	j.Enabled = in.Enabled
	j, err = s.store.UpdateCronJob(r.Context(), j)
	if s.storeError(w, "update cron job", err, "") {
		return
	}
	action := "cron_job.disable"
	if j.Enabled {
		action = "cron_job.enable"
	}
	s.audit(r, action, "cron_job", j.ID, hostcron.Label(j), "")
	writeJSON(w, http.StatusOK, s.cronJobJSON(j, nil, cfg.Enabled))
}

// handleDeleteCronJob needs no password either. A run in progress finishes.
func (s *Server) handleDeleteCronJob(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.cronGate(w, r, false); !ok {
		return
	}
	j, err := s.store.DeleteCronJob(r.Context(), r.PathValue("id"))
	if s.storeError(w, "delete cron job", err, "") {
		return
	}
	s.audit(r, "cron_job.delete", "cron_job", j.ID, hostcron.Label(j), "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRunCronJob(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.cronGate(w, r, true); !ok {
		return
	}
	j, err := s.store.GetCronJob(r.Context(), r.PathValue("id"))
	if s.storeError(w, "get cron job", err, "") {
		return
	}
	run, err := s.cronJobs.Start(r.Context(), j, hostcron.SourceManual, userFrom(r.Context()).Email)
	if errors.Is(err, hostcron.ErrBusy) {
		writeError(w, http.StatusConflict, "busy", "this job is still running")
		return
	}
	if err != nil {
		s.internal(w, "run cron job", err)
		return
	}
	s.audit(r, "cron_job.run", "cron_job", j.ID, hostcron.Label(j), "as "+j.User)
	writeJSON(w, http.StatusAccepted, toCronRunJSON(run))
}

func (s *Server) handleListCronRuns(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.cronGate(w, r, false); !ok {
		return
	}
	j, err := s.store.GetCronJob(r.Context(), r.PathValue("id"))
	if s.storeError(w, "get cron job", err, "") {
		return
	}
	runs, err := s.store.ListCronRuns(r.Context(), j.ID)
	if err != nil {
		s.internal(w, "list cron runs", err)
		return
	}
	out := make([]cronRunJSON, 0, len(runs))
	for _, run := range runs {
		out = append(out, toCronRunJSON(run))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCronSchedule checks a schedule and returns its next runs, for the form's preview.
func (s *Server) handleCronSchedule(w http.ResponseWriter, r *http.Request) {
	expr := strings.Join(strings.Fields(r.URL.Query().Get("expr")), " ")
	if _, err := hostcron.Check(store.CronJob{Schedule: expr, Command: "true", User: "root"}); err != nil {
		invalid(w, strings.TrimPrefix(err.Error(), hostcron.ErrInvalid.Error()+": "))
		return
	}
	next := make([]time.Time, 0, 3)
	for at := time.Now(); len(next) < 3; {
		n := hostcron.Next(expr, at)
		if n == nil || n.IsZero() {
			break
		}
		next = append(next, *n)
		at = *n
	}
	writeJSON(w, http.StatusOK, map[string]any{"schedule": expr, "next": next})
}

// Package hostcron runs commands on the host on a schedule: the panel's cron jobs. The
// panel keeps the schedule itself rather than writing the host's crontab, so every run
// has a record and its output, and no cron daemon is needed on the host.
package hostcron

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"panel/internal/executor"
	"panel/internal/selfbackup"
	"panel/internal/store"
)

const (
	// MaxRun is how long one run may take before it is stopped.
	MaxRun = 6 * time.Hour
	// maxOutput is how much of a run's output is kept: its end, where errors are.
	maxOutput = 64 * 1024

	SourceSchedule = "schedule"
	SourceManual   = "manual"
)

var (
	ErrInvalid = errors.New("invalid cron job")
	// ErrBusy: the job's previous run has not finished.
	ErrBusy = errors.New("cron job is already running")
)

// Runner runs command on the host as user and writes what it prints to out.
type Runner func(ctx context.Context, user, command string, out io.Writer) (exitCode int, err error)

type Service struct {
	st   *store.Store
	log  *slog.Logger
	run  Runner
	base context.Context // ends with the panel; runs do not outlive it

	mu      sync.Mutex
	running map[string]bool // by job ID
	wg      sync.WaitGroup

	now func() time.Time // replaced by tests
}

// New returns a service whose runs end when ctx does.
func New(ctx context.Context, st *store.Store, log *slog.Logger, run Runner) *Service {
	return &Service{st: st, log: log, run: run, base: ctx, running: map[string]bool{}, now: time.Now}
}

// Check validates a job and returns it normalised. User must already be chosen.
func Check(j store.CronJob) (store.CronJob, error) {
	j.Name = strings.TrimSpace(j.Name)
	j.Schedule = strings.Join(strings.Fields(j.Schedule), " ")
	j.Command = strings.TrimSpace(j.Command)
	j.User = strings.TrimSpace(j.User)
	switch {
	case utf8.RuneCountInString(j.Name) > 80:
		return j, fmt.Errorf("%w: the name is at most 80 characters", ErrInvalid)
	case j.Command == "":
		return j, fmt.Errorf("%w: a script is required", ErrInvalid)
	case len(j.Command) > MaxScript || strings.ContainsRune(j.Command, 0) || !utf8.ValidString(j.Command):
		return j, fmt.Errorf("%w: the script is at most %d bytes of text", ErrInvalid, MaxScript)
	case j.User == "" || !executor.ValidHostUser(j.User):
		return j, fmt.Errorf("%w: run as a user name of this server: lowercase letters, digits, - and _", ErrInvalid)
	}
	if _, err := selfbackup.ParseSchedule(j.Schedule); err != nil {
		return j, fmt.Errorf("%w: %s", ErrInvalid, err.Error())
	}
	return j, nil
}

// Next is when schedule runs next after t, or nil when it is not a valid schedule.
func Next(schedule string, t time.Time) *time.Time {
	s, err := selfbackup.ParseSchedule(schedule)
	if err != nil {
		return nil
	}
	next := s.Next(t.UTC())
	return &next
}

// Running reports whether a run of the job is in progress.
func (s *Service) Running(jobID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running[jobID]
}

// Watch starts due jobs until ctx ends. A slot missed while the panel was stopped is not
// caught up, like cron. Nothing runs while the host terminal is switched off.
func (s *Service) Watch(ctx context.Context) {
	if err := s.st.FailUnfinishedCronRuns(ctx, "the panel restarted during this run"); err != nil {
		s.log.Warn("close unfinished cron runs", "error", err)
	}
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	last := s.now()
	for {
		select {
		case <-ctx.Done():
			s.wg.Wait()
			return
		case <-tick.C:
		}
		now := s.now()
		s.startDue(ctx, last, now)
		last = now
	}
}

// startDue starts every enabled job with a slot in (last, now].
func (s *Service) startDue(ctx context.Context, last, now time.Time) {
	if off, _, err := s.st.GetSetting(ctx, store.SettingHostTermOff); err != nil || off == "true" {
		return
	}
	jobs, err := s.st.ListCronJobs(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.log.Warn("list cron jobs", "error", err)
		}
		return
	}
	for _, j := range jobs {
		if !j.Enabled {
			continue
		}
		if next := Next(j.Schedule, last); next == nil || next.After(now) {
			continue
		}
		if _, err := s.Start(ctx, j, SourceSchedule, "system"); errors.Is(err, ErrBusy) {
			s.log.Warn("cron job skipped: its previous run is still going", "job", j.ID)
		} else if err != nil {
			s.log.Warn("start cron job", "job", j.ID, "error", err)
		}
	}
}

// Start begins a run of the job in the background and returns its record.
func (s *Service) Start(ctx context.Context, j store.CronJob, source, actor string) (store.CronRun, error) {
	s.mu.Lock()
	if s.running[j.ID] {
		s.mu.Unlock()
		return store.CronRun{}, ErrBusy
	}
	s.running[j.ID] = true
	s.mu.Unlock()
	run, err := s.st.StartCronRun(ctx, j.ID, source, actor)
	if err != nil {
		s.finished(j.ID)
		return run, err
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.finished(j.ID)
		s.execute(j, run)
	}()
	return run, nil
}

func (s *Service) finished(jobID string) {
	s.mu.Lock()
	delete(s.running, jobID)
	s.mu.Unlock()
}

func (s *Service) execute(j store.CronJob, run store.CronRun) {
	ctx, cancel := context.WithTimeout(s.base, MaxRun)
	defer cancel()
	out := &tail{max: maxOutput}
	code, err := s.run(ctx, j.User, j.Command, out)
	failure := ""
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		failure = "stopped: it ran longer than " + MaxRun.String()
	case errors.Is(err, context.Canceled):
		failure = "stopped: the panel shut down"
	case err != nil:
		failure = "could not run: " + err.Error()
	}
	if err != nil {
		code = -1
	}
	// Recorded even while the panel shuts down.
	saveCtx, stop := context.WithTimeout(context.WithoutCancel(s.base), 10*time.Second)
	defer stop()
	if err := s.st.FinishCronRun(saveCtx, run.ID, code, out.String(), failure); err != nil {
		s.log.Error("record cron run", "job", j.ID, "error", err)
	}
	// Activity gets the runs that failed, and (from the API) the ones someone asked for:
	// every run of an every-minute job would bury the rest. The job's history has them all.
	if code != 0 {
		detail := failure
		if detail == "" {
			detail = fmt.Sprintf("exit %d", code)
		}
		if err := s.st.AddEvent(saveCtx, store.Event{Actor: "system", Action: "cron_job.run_failed", TargetType: "cron_job", TargetID: j.ID, TargetName: Label(j), Detail: detail}); err != nil {
			s.log.Error("record event", "action", "cron_job.run_failed", "error", err)
		}
	}
}

// MaxScript is the size limit of a job's script. It reaches the host in one environment
// variable, which Linux caps at 128 KiB.
const MaxScript = 32 * 1024

// Label names a job in Activity: its name, or the first line of its script that does
// something (not the shebang, a comment or an empty line).
func Label(j store.CronJob) string {
	if j.Name != "" {
		return j.Name
	}
	line, _, _ := strings.Cut(j.Command, "\n")
	for _, l := range strings.Split(j.Command, "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			line = l
			break
		}
	}
	if r := []rune(line); len(r) > 60 {
		return string(r[:60]) + "…"
	}
	return line
}

// tail keeps the last max bytes written to it.
type tail struct {
	mu      sync.Mutex
	max     int
	buf     bytes.Buffer
	dropped bool
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf.Write(p)
	if t.buf.Len() > 2*t.max {
		t.buf.Next(t.buf.Len() - t.max)
		t.dropped = true
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	b := t.buf.Bytes()
	dropped := t.dropped
	if len(b) > t.max {
		b, dropped = b[len(b)-t.max:], true
	}
	out := strings.ToValidUTF8(string(b), "")
	if dropped {
		// Start at a whole line.
		if i := strings.IndexByte(out, '\n'); i >= 0 && i < len(out)-1 {
			out = out[i+1:]
		}
		out = "[earlier output dropped]\n" + out
	}
	return out
}

package deploy

import (
	"context"
	"fmt"
	"sync"
	"time"

	"panel/internal/store"
)

// Health statuses of a running app, as the monitor last saw it.
const (
	HealthUnknown   = "unknown"   // not checked yet since the panel started
	HealthHealthy   = "healthy"   // running, and its health check passes (or it has none)
	HealthUnhealthy = "unhealthy" // running, but its health check failed Retries times in a row
	HealthDown      = "down"      // exited, restarting in a loop, or missing
)

// MonitorInterval is how often running apps are checked after their deploy.
const MonitorInterval = 30 * time.Second

// AppHealth is an app's last observed health.
type AppHealth struct {
	Status    string    `json:"status"`
	Reason    string    `json:"reason"`
	Since     time.Time `json:"since"`      // when Status last changed
	CheckedAt time.Time `json:"checked_at"` // last check
	Probe     string    `json:"probe"`      // "tcp", "http", or "" when only the container state is watched
	Restarts  int       `json:"restarts"`   // restarts of the current container
}

type appWatch struct {
	AppHealth
	container string // the container these counters belong to
	failures  int    // health check failures in a row
	restarts  int
}

type monitor struct {
	mu   sync.Mutex
	apps map[string]*appWatch
}

// AppHealth returns the app's last observed health; false when it is not watched
// (stopped, never deployed, or deploying).
func (s *Service) AppHealth(appID string) (AppHealth, bool) {
	s.mon.mu.Lock()
	defer s.mon.mu.Unlock()
	w, ok := s.mon.apps[appID]
	if !ok {
		return AppHealth{}, false
	}
	return w.AppHealth, true
}

// Monitor checks every running app each interval until ctx ends: the container's
// state and restarts, then the app's own TCP or HTTP health check. Changes are
// recorded as events (app.health_failing, app.health_recovered, app.crashed),
// which alert rules can send to notification channels.
func (s *Service) Monitor(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		s.CheckHealth(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// CheckHealth runs one round of checks.
func (s *Service) CheckHealth(ctx context.Context) {
	targets, err := s.opts.Store.AppRuntimeTargets(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.opts.Log.Warn("health monitor", "error", err)
		}
		return
	}
	watched := map[string]bool{}
	var wg sync.WaitGroup
	limit := make(chan struct{}, 8)
	for _, t := range targets {
		watched[t.AppID] = true
		wg.Add(1)
		go func() {
			defer wg.Done()
			limit <- struct{}{}
			defer func() { <-limit }()
			s.checkApp(ctx, t)
		}()
	}
	wg.Wait()
	s.mon.mu.Lock()
	for id := range s.mon.apps {
		if !watched[id] {
			delete(s.mon.apps, id) // stopped, removed or deploying: its deploy reports on it
		}
	}
	s.mon.mu.Unlock()
}

func (s *Service) checkApp(ctx context.Context, t store.AppRuntimeTarget) {
	now := time.Now().UTC()
	s.mon.mu.Lock()
	w := s.mon.apps[t.AppID]
	if w == nil {
		w = &appWatch{AppHealth: AppHealth{Status: HealthUnknown, Since: now}, restarts: -1}
		s.mon.apps[t.AppID] = w
	}
	if w.container != t.ContainerName { // a new deploy: new counters
		w.container, w.failures, w.restarts = t.ContainerName, 0, -1
	}
	s.mon.mu.Unlock()

	app := store.App{ID: t.AppID, Name: t.AppName, ProjectID: t.ProjectID}
	status, reason, probe := HealthHealthy, "", ""
	c, err := s.opts.Executor.InspectContainer(ctx, t.ContainerName)
	crashed := ""
	switch {
	case ctx.Err() != nil:
		return
	case err != nil:
		status, reason = HealthDown, "the container is missing"
	default:
		s.mon.mu.Lock()
		if w.restarts >= 0 && c.Restarts > w.restarts {
			crashed = fmt.Sprintf("the container restarted (exit code %d)", c.ExitCode)
			if c.OOMKilled {
				crashed = "the container was killed for exceeding its memory limit and restarted"
			}
		}
		w.restarts, w.Restarts = c.Restarts, c.Restarts
		s.mon.mu.Unlock()
		switch {
		case c.State == "restarting":
			status, reason = HealthDown, "the container keeps crashing and restarting"
		case !c.Running:
			status, reason = HealthDown, fmt.Sprintf("the container stopped (exit code %d)", c.ExitCode)
			if c.OOMKilled {
				reason = "the container was killed for exceeding its memory limit"
			}
		default:
			h, _, herr := s.opts.Store.AppHealthCheck(ctx, t.AppID)
			if herr == nil && h.Type != store.HealthRunning && !s.opts.OutsideNetwork && t.Port > 0 {
				probe = h.Type
				if perr := s.probe(ctx, h, t.ContainerName, t.Port); perr != nil {
					if ctx.Err() != nil {
						return
					}
					s.mon.mu.Lock()
					w.failures++
					failures := w.failures
					s.mon.mu.Unlock()
					reason = fmt.Sprintf("health check failed %d time(s) in a row: %v", failures, perr)
					retries := max(1, h.Retries)
					if failures >= retries {
						status = HealthUnhealthy
					} else {
						s.mon.mu.Lock()
						status = w.Status // not yet: keep the last status
						if status == HealthUnknown || status == HealthDown {
							status = HealthHealthy
						}
						s.mon.mu.Unlock()
					}
				} else {
					s.mon.mu.Lock()
					w.failures = 0
					s.mon.mu.Unlock()
				}
			}
		}
	}

	s.mon.mu.Lock()
	prev := w.Status
	w.CheckedAt, w.Probe = now, probe
	if status != prev {
		w.Since = now
	}
	w.Status, w.Reason = status, reason // healthy keeps the reason of a failure that is not counted yet
	s.mon.mu.Unlock()

	if crashed != "" {
		s.event(ctx, "app.crashed", app, crashed)
	}
	failing := status == HealthUnhealthy || status == HealthDown
	switch {
	case failing && prev != HealthUnhealthy && prev != HealthDown:
		s.event(ctx, "app.health_failing", app, reason)
	case !failing && (prev == HealthUnhealthy || prev == HealthDown):
		s.event(ctx, "app.health_recovered", app, "")
	}
}

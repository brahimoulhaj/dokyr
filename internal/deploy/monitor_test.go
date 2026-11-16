package deploy

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"

	"panel/internal/executor"
	"panel/internal/store"
)

func TestHealthMonitor(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	var failing atomic.Bool
	e.svc.opts.Dial = func(context.Context, string, string) (net.Conn, error) {
		if failing.Load() {
			return nil, errors.New("connection refused")
		}
		a, b := net.Pipe()
		go b.Close()
		return a, nil
	}
	h := store.HealthCheck{Type: store.HealthTCP, Interval: 1, Timeout: 1, Retries: 2, StartPeriod: 0, Drain: 0}
	if err := e.st.SetAppHealthCheck(ctx, e.app.ID, h); err != nil {
		t.Fatal(err)
	}
	dep := e.deploy()
	if dep.Status != store.DeploySucceeded {
		t.Fatalf("deploy %s: %s", dep.Status, dep.Error)
	}
	events := func() []string {
		list, _ := e.st.ListEvents(ctx, 50)
		var out []string
		for i := len(list) - 1; i >= 0; i-- {
			if strings.HasPrefix(list[i].Action, "app.") {
				out = append(out, list[i].Action+":"+list[i].Detail)
			}
		}
		return out
	}
	status := func() AppHealth {
		t.Helper()
		got, ok := e.svc.AppHealth(e.app.ID)
		if !ok {
			t.Fatal("app not watched")
		}
		return got
	}

	e.svc.CheckHealth(ctx)
	if s := status(); s.Status != HealthHealthy || s.Probe != "tcp" || len(events()) != 0 {
		t.Fatalf("first check %+v %v", s, events())
	}
	// One failure is not enough (Retries 2); the second makes it unhealthy, once.
	failing.Store(true)
	e.svc.CheckHealth(ctx)
	if s := status(); s.Status != HealthHealthy || !strings.Contains(s.Reason, "1 time") {
		t.Fatalf("after one failure %+v", s)
	}
	e.svc.CheckHealth(ctx)
	e.svc.CheckHealth(ctx)
	if s := status(); s.Status != HealthUnhealthy || len(events()) != 1 || !strings.HasPrefix(events()[0], "app.health_failing:health check failed") {
		t.Fatalf("unhealthy %+v %v", s, events())
	}
	failing.Store(false)
	e.svc.CheckHealth(ctx)
	if s := status(); s.Status != HealthHealthy || s.Reason != "" || events()[1] != "app.health_recovered:" {
		t.Fatalf("recovered %+v %v", s, events())
	}

	// A restart is a crash; an OOM kill says so.
	e.ex.Mutate(dep.ContainerName, func(c *executor.Container) { c.Restarts, c.OOMKilled = 1, true })
	e.svc.CheckHealth(ctx)
	if ev := events(); len(ev) != 3 || !strings.Contains(ev[2], "app.crashed:the container was killed for exceeding its memory limit") {
		t.Fatalf("crash events %v", ev)
	}
	// A crash loop is down; a stopped container too.
	e.ex.Mutate(dep.ContainerName, func(c *executor.Container) { c.State = "restarting" })
	e.svc.CheckHealth(ctx)
	if s := status(); s.Status != HealthDown || !strings.Contains(s.Reason, "keeps crashing") {
		t.Fatalf("restart loop %+v", s)
	}
	e.ex.Mutate(dep.ContainerName, func(c *executor.Container) { c.State, c.Running, c.ExitCode, c.OOMKilled = "exited", false, 3, false })
	e.svc.CheckHealth(ctx)
	if s := status(); s.Status != HealthDown || !strings.Contains(s.Reason, "exit code 3") || len(events()) != 4 {
		t.Fatalf("stopped %+v %v (down after down: no new event)", s, events())
	}

	// A stopped app is no longer watched.
	if err := e.st.SetAppDesiredState(ctx, e.app.ID, "stopped"); err != nil {
		t.Fatal(err)
	}
	e.svc.CheckHealth(ctx)
	if _, ok := e.svc.AppHealth(e.app.ID); ok {
		t.Fatal("stopped app still watched")
	}
}

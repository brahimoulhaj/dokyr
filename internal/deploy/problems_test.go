package deploy

import (
	"context"
	"errors"
	"testing"

	"panel/internal/executor"
	"panel/internal/store"
)

type runtimeSnapshot struct {
	executor.Executor
	containers []executor.Container
	err        error
	calls      int
}

func (r *runtimeSnapshot) ListContainers(context.Context) ([]executor.Container, error) {
	r.calls++
	return r.containers, r.err
}

func TestAppProblems(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	// Never-deployed apps have no crash alert.
	if list, err := e.svc.AppProblems(ctx); err != nil || len(list) != 0 {
		t.Fatalf("undeployed = %+v, %v", list, err)
	}
	dep := e.deploy()
	if dep.Status != store.DeploySucceeded {
		t.Fatal("setup deployment failed", dep.Error)
	}
	c, err := e.ex.InspectContainer(ctx, dep.ContainerName)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := &runtimeSnapshot{Executor: e.ex, containers: []executor.Container{c}}
	e.svc.opts.Executor = snapshot
	if list, err := e.svc.AppProblems(ctx); err != nil || len(list) != 0 {
		t.Fatalf("healthy = %+v, %v", list, err)
	}
	for _, state := range []string{"restarting", "exited", "dead", "paused"} {
		snapshot.containers[0].State, snapshot.containers[0].Running = state, false
		before := snapshot.calls
		list, err := e.svc.AppProblems(ctx)
		if err != nil || len(list) != 1 || list[0].RuntimeState != state || list[0].AppID != e.app.ID || list[0].ProjectName != "Shop" {
			t.Fatalf("%s = %+v, %v", state, list, err)
		}
		if snapshot.calls != before+1 {
			t.Fatal("more than one Docker list call per check")
		}
	}
	snapshot.containers = nil
	if list, err := e.svc.AppProblems(ctx); err != nil || len(list) != 1 || list[0].RuntimeState != "missing" {
		t.Fatalf("missing = %+v, %v", list, err)
	}
	snapshot.err = errors.New("Docker offline")
	if _, err := e.svc.AppProblems(ctx); err == nil {
		t.Fatal("Docker outage was reported as healthy")
	}
	snapshot.err = nil
	if err := e.st.SetAppDesiredState(ctx, e.app.ID, store.StateStopped); err != nil {
		t.Fatal(err)
	}
	if list, err := e.svc.AppProblems(ctx); err != nil || len(list) != 0 {
		t.Fatalf("intentional stop = %+v, %v", list, err)
	}
	e.st.SetAppDesiredState(ctx, e.app.ID, store.StateRunning)
	next, err := e.st.CreateDeployment(ctx, e.app.ID, "manual", "nginx:2", 80, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.MarkDeploymentRunning(ctx, next.ID); err != nil {
		t.Fatal(err)
	}
	if list, err := e.svc.AppProblems(ctx); err != nil || len(list) != 0 {
		t.Fatalf("active recreate = %+v, %v", list, err)
	}
}

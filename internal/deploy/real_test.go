package deploy

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"panel/internal/executor"
	"panel/internal/store"
)

// realImage is small, starts fast and stays up; a start command that exits makes it fail.
const realImage = "caddy:2.10.2-alpine"

// TestRealDeploy deploys an image app on a real Docker daemon, end to end: the first
// deployment, a rolling redeploy that replaces the container, a deployment that fails and
// leaves the running one alone, then stop, start and removal. Skipped with -short or
// without a daemon.
func TestRealDeploy(t *testing.T) {
	if testing.Short() {
		t.Skip("needs Docker; skipped with -short")
	}
	name := fmt.Sprintf("deploytest-%d", time.Now().UnixNano())
	ex, err := executor.NewDocker(executor.Options{Network: name, LabelPrefix: name})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	ping, stop := context.WithTimeout(ctx, 5*time.Second)
	err = ex.Ping(ping)
	stop()
	if err != nil {
		ex.Close()
		t.Skipf("no Docker daemon: %v", err)
	}
	t.Cleanup(func() {
		clean, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		list, _ := ex.ListContainers(clean)
		for _, c := range list {
			_ = ex.RemoveContainer(clean, c.ID, true)
		}
		_ = exec.CommandContext(clean, "docker", "network", "rm", name).Run()
		ex.Close()
	})
	if err := ex.EnsureNetwork(ctx); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "panel.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	// The test runs on the host, off the apps' network: a deployment waits StartGrace for
	// the container to stay up, as the panel does in development.
	svc := New(ctx, Options{
		Store: st, Executor: ex, DataDir: dir, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		StartGrace: 2 * time.Second, OutsideNetwork: true, MaxDrain: time.Second, StopGrace: 2 * time.Second,
	})
	p, err := st.CreateProject(ctx, "real", "")
	if err != nil {
		t.Fatal(err)
	}
	app, err := st.CreateApp(ctx, p.ID, store.AppInput{Name: "web", SourceType: store.SourceImage, Image: realImage, Port: 80, Environment: map[string]string{"RELEASE": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	running := func() []executor.Container {
		t.Helper()
		list, err := ex.ListContainers(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var out []executor.Container
		for _, c := range list {
			if c.Kind == executor.KindApp && c.ResourceID == app.ID && c.Running {
				out = append(out, c)
			}
		}
		return out
	}
	deploy := func(want string) store.Deployment {
		t.Helper()
		dep, err := svc.Deploy(ctx, app.ID, "test")
		if err != nil {
			t.Fatal(err)
		}
		svc.Wait()
		dep, err = st.GetDeployment(ctx, dep.ID)
		if err != nil {
			t.Fatal(err)
		}
		if dep.Status != want {
			log, _ := exec.Command("cat", svc.LogPath(dep)).Output()
			t.Fatalf("deployment %s, want %s: %s\n%s", dep.Status, want, dep.Error, log)
		}
		return dep
	}

	// 1. The first deployment: one container of the image, running, and the app points at it.
	first := deploy(store.DeploySucceeded)
	one := running()
	if len(one) != 1 || one[0].Image != realImage {
		t.Fatalf("after the first deployment: %+v", one)
	}
	if a, _ := st.GetApp(ctx, app.ID); a.CurrentDeploymentID != first.ID || a.DesiredState != store.StateRunning {
		t.Fatalf("app after the first deployment: %+v", a)
	}

	// 2. A redeploy with a changed variable: a new container takes over, the old one goes.
	a, _ := st.GetApp(ctx, app.ID)
	in := store.AppInput{Name: a.Name, SourceType: a.SourceType, Image: a.Image, Port: a.Port, Environment: map[string]string{"RELEASE": "2"}}
	if _, err := st.UpdateApp(ctx, app.ID, in); err != nil {
		t.Fatal(err)
	}
	second := deploy(store.DeploySucceeded)
	two := running()
	if len(two) != 1 || two[0].ID == one[0].ID {
		t.Fatalf("after the redeploy: %+v (was %s)", two, one[0].ID)
	}
	if _, err := ex.InspectContainer(ctx, one[0].ID); err == nil {
		t.Fatal("the old container was not removed")
	}

	// 3. A version that exits at once fails its deployment; the running one is untouched.
	in.StartCommand = "exit 3"
	if _, err := st.UpdateApp(ctx, app.ID, in); err != nil {
		t.Fatal(err)
	}
	failed := deploy(store.DeployFailed)
	if failed.Error == "" {
		t.Fatal("a failed deployment says nothing of why")
	}
	still := running()
	if len(still) != 1 || still[0].ID != two[0].ID {
		t.Fatalf("after the failed deployment: %+v, want only %s", still, two[0].ID)
	}
	if a, _ := st.GetApp(ctx, app.ID); a.CurrentDeploymentID != second.ID {
		t.Fatalf("the app points at %s after a failed deployment, want %s", a.CurrentDeploymentID, second.ID)
	}

	// 4. Stop, start, remove.
	if err := svc.Stop(ctx, app.ID); err != nil {
		t.Fatal(err)
	}
	if left := running(); len(left) != 0 {
		t.Fatalf("running after stop: %+v", left)
	}
	if err := svc.Start(ctx, app.ID); err != nil {
		t.Fatal(err)
	}
	svc.Wait()
	if back := running(); len(back) != 1 {
		t.Fatalf("running after start: %+v", back)
	}
	if err := svc.Remove(ctx, app.ID); err != nil {
		t.Fatal(err)
	}
	list, _ := ex.ListContainers(ctx)
	for _, c := range list {
		if c.ResourceID == app.ID {
			t.Fatalf("a container is left after the removal: %+v", c)
		}
	}
}

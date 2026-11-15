package database

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"panel/internal/executor"
	"panel/internal/executor/executortest"
	"panel/internal/store"
)

type testHost struct {
	*executortest.Fake
	specs     []executor.ContainerSpec
	commands  []executor.Command
	initError bool
}

func (h *testHost) CreateContainer(ctx context.Context, spec executor.ContainerSpec) (string, error) {
	h.specs = append(h.specs, spec)
	return h.Fake.CreateContainer(ctx, spec)
}
func (h *testHost) Exec(_ context.Context, _ string, cmd executor.Command) error {
	h.commands = append(h.commands, cmd)
	if h.initError && cmd.Input != "" {
		return errors.New("SQL contains secret-password")
	}
	return nil
}

func testService(t *testing.T) (*Service, *testHost, store.Project) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	if _, err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := st.CreateProject(ctx, "shop", "")
	if err != nil {
		t.Fatal(err)
	}
	h := &testHost{Fake: executortest.New()}
	svc := New(ctx, st, h)
	svc.Poll = time.Millisecond
	t.Cleanup(func() { cancel(); svc.Wait() })
	return svc, h, p
}

func input(p store.Project, engine, version string) store.DBInstance {
	return store.DBInstance{ProjectID: p.ID, Name: "main", Engine: engine, Version: version, DatabaseName: "app", Username: "app", CPULimit: .5, MemoryMB: 256}
}

func TestProvisionLifecycleAndRecovery(t *testing.T) {
	for _, tc := range []struct{ engine, version, target string }{{"postgres", "18", "/var/lib/postgresql"}, {"postgres", "17", "/var/lib/postgresql/data"}, {"mariadb", "11.8", "/var/lib/mysql"}} {
		t.Run(tc.engine+tc.version, func(t *testing.T) {
			svc, h, p := testService(t)
			ctx := context.Background()
			d, err := svc.Create(ctx, input(p, tc.engine, tc.version))
			if err != nil {
				t.Fatal(err)
			}
			svc.Wait()
			got, _ := svc.st.GetDBInstance(ctx, d.ID)
			if got.Status != "running" || svc.Runtime(ctx, d.ID) != "running" {
				t.Fatalf("provision = %+v", got)
			}
			spec := h.specs[0]
			if len(spec.Ports) != 0 || len(spec.Mounts) != 1 || spec.Mounts[0].Target != tc.target || spec.Mounts[0].Volume != VolumeName(d.ID) || spec.Kind != executor.KindDatabase || spec.CPUs != .5 || spec.MemoryMB != 256 {
				t.Fatalf("bad container spec: %+v", spec)
			}
			creds, _ := svc.st.DBInstanceCredentials(ctx, d.ID)
			if len(creds["password"]) < 40 || creds["password"] == creds["root_password"] {
				t.Fatal("credentials are not separate random passwords")
			}
			if tc.engine == "postgres" && !strings.Contains(h.commands[1].Input, "NOSUPERUSER NOCREATEDB NOCREATEROLE") {
				t.Fatal("application role is not restricted")
			}
			if err := svc.Action(ctx, d.ID, "stop"); err != nil {
				t.Fatal(err)
			}
			if err := svc.Recover(ctx); err != nil {
				t.Fatal(err)
			}
			svc.Wait()
			if svc.Runtime(ctx, d.ID) != "exited" {
				t.Fatal("recovery restarted an intentionally stopped database")
			}
			if err := svc.Action(ctx, d.ID, "start"); err != nil {
				t.Fatal(err)
			}
			svc.Wait()
			if err := svc.Action(ctx, d.ID, "restart"); err != nil {
				t.Fatal(err)
			}
			svc.Wait()
			if len(h.specs) != 1 {
				t.Fatal("start/restart recreated the data container")
			}
			// A missing container is recreated with the exact same persistent volume.
			h.RemoveContainer(ctx, ContainerName(d.ID), true)
			if err := svc.Recover(ctx); err != nil {
				t.Fatal(err)
			}
			svc.Wait()
			if len(h.specs) != 2 || h.specs[1].Mounts[0].Volume != spec.Mounts[0].Volume {
				t.Fatal("recovery lost the persistent volume")
			}
			// Stop intent survives interruption before Docker has stopped the workload.
			svc.st.SetDBInstanceState(ctx, d.ID, "stopped", "stopping", "")
			svc.Recover(ctx)
			svc.Wait()
			if svc.Runtime(ctx, d.ID) != "exited" {
				t.Fatal("interrupted stop was not enforced")
			}
			if err := svc.Delete(ctx, d.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.st.GetDBInstance(ctx, d.ID); !errors.Is(err, store.ErrNotFound) {
				t.Fatal("instance record remains", err)
			}
			if svc.Runtime(ctx, d.ID) != "missing" {
				t.Fatal("container remains")
			}
		})
	}
}

func TestProvisionFailureRetryAndBusy(t *testing.T) {
	svc, h, p := testService(t)
	ctx := context.Background()
	h.initError = true
	d, err := svc.Create(ctx, input(p, "postgres", "18"))
	if err != nil {
		t.Fatal(err)
	}
	svc.Wait()
	got, _ := svc.st.GetDBInstance(ctx, d.ID)
	if got.Status != "failed" || strings.Contains(got.Error, "secret-password") {
		t.Fatalf("failure = %+v", got)
	}
	h.initError = false
	if err := svc.Action(ctx, d.ID, "start"); err != nil {
		t.Fatal(err)
	}
	svc.Wait()
	if got, _ := svc.st.GetDBInstance(ctx, d.ID); got.Status != "running" {
		t.Fatalf("retry = %+v", got)
	}
	svc.acquire(d.ID)
	if err := svc.Action(ctx, d.ID, "stop"); !errors.Is(err, ErrBusy) {
		t.Fatalf("busy stop = %v", err)
	}
	if err := svc.Delete(ctx, d.ID); !errors.Is(err, ErrBusy) {
		t.Fatalf("busy delete = %v", err)
	}
	svc.release(d.ID)
}

func TestDatabaseCrashBeforeReady(t *testing.T) {
	svc, h, p := testService(t)
	h.CrashOnStart["postgres:18"] = true
	d, err := svc.Create(context.Background(), input(p, "postgres", "18"))
	if err != nil {
		t.Fatal(err)
	}
	svc.Wait()
	got, err := svc.st.GetDBInstance(context.Background(), d.ID)
	if err != nil || got.Status != "failed" || !strings.Contains(got.Error, "exited before becoming ready") {
		t.Fatalf("crashed instance = %+v, %v", got, err)
	}
	if len(h.commands) != 0 {
		t.Fatal("SQL ran in an exited container")
	}
}

func TestDatabaseReferences(t *testing.T) {
	svc, h, p := testService(t)
	ctx := context.Background()
	d, _ := svc.Create(ctx, input(p, "postgres", "18"))
	svc.Wait()
	creds, _ := svc.st.DBInstanceCredentials(ctx, d.ID)
	want := Connection(d, creds)
	env := map[string]string{"DATABASE_URL": "${db.main.URL}", "HOST": "prefix-${db.main.HOST}", "LITERAL": "${other.X}"}
	got, err := ResolveEnvironment(ctx, svc.st, p.ID, env)
	if err != nil || got["DATABASE_URL"] != want["URL"] || got["HOST"] != "prefix-"+want["HOST"] || got["LITERAL"] != env["LITERAL"] {
		t.Fatalf("references failed: %v", err)
	}
	for _, bad := range []string{"${db.other.URL}", "${db.main.NOPE}", "${db.main.url}", "${db.main}"} {
		if _, err := ResolveEnvironment(ctx, svc.st, p.ID, map[string]string{"X": bad}); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	other, _ := svc.st.CreateProject(ctx, "other", "")
	if _, err := ResolveEnvironment(ctx, svc.st, other.ID, env); err == nil {
		t.Fatal("cross-project reference exposed a credential")
	}
	before, err := svc.st.GetDBInstance(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	commands, specs := len(h.commands), len(h.specs)
	moved, err := svc.MoveProject(ctx, d.ID, other.ID)
	if err != nil || moved.ProjectID != other.ID || moved.Status != before.Status || moved.DesiredState != before.DesiredState || moved.ID != d.ID {
		t.Fatalf("move = %+v, %v", moved, err)
	}
	if len(h.commands) != commands || len(h.specs) != specs || svc.Runtime(ctx, d.ID) != "running" {
		t.Fatal("move modified the running database")
	}
	if _, err := ResolveEnvironment(ctx, svc.st, p.ID, env); err == nil {
		t.Fatal("old project still resolves moved database")
	}
	if got, err := ResolveEnvironment(ctx, svc.st, other.ID, env); err != nil || got["DATABASE_URL"] != want["URL"] {
		t.Fatal("moving changed connection details or new project cannot resolve", err)
	}
	svc.Action(ctx, d.ID, "stop")
	if _, err := ResolveEnvironment(ctx, svc.st, other.ID, env); err == nil {
		t.Fatal("stopped database reference accepted")
	}
}

func TestDatabaseValidation(t *testing.T) {
	valid := input(store.Project{ID: "p"}, "postgres", "18")
	for _, mutate := range []func(*store.DBInstance){
		func(d *store.DBInstance) { d.Name = "UPPER" }, func(d *store.DBInstance) { d.DatabaseName = "a;DROP DATABASE" },
		func(d *store.DBInstance) { d.Username = "postgres" }, func(d *store.DBInstance) { d.Version = "latest" },
		func(d *store.DBInstance) { d.MemoryMB = 64 }, func(d *store.DBInstance) { d.CPULimit = -1 },
	} {
		d := valid
		mutate(&d)
		if !errors.Is(Validate(d), ErrInvalid) {
			t.Fatalf("accepted %+v", d)
		}
	}
}

func TestDatabaseOperationRecovery(t *testing.T) {
	svc, _, p := testService(t)
	ctx := context.Background()
	d, err := svc.st.CreateDBInstance(ctx, input(p, "postgres", "18"), map[string]string{"password": "app-secret", "root_password": "root-secret"})
	if err != nil {
		t.Fatal(err)
	}
	op, err := svc.st.CreateDBOperation(ctx, d, "create")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.st.CreateDBOperation(ctx, d, "start"); !errors.Is(err, store.ErrConflict) {
		t.Fatal("concurrent operation was accepted", err)
	}
	if err := svc.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	svc.Wait()
	got, err := svc.st.GetDBOperation(ctx, op.ID)
	if err != nil || got.Status != "succeeded" || got.StartedAt == nil || got.FinishedAt == nil {
		t.Fatalf("recovered operation = %+v, %v", got, err)
	}
	list, err := svc.st.ActiveDBOperations(ctx)
	if err != nil || len(list) != 0 {
		t.Fatalf("finished job remains active: %+v, %v", list, err)
	}
	// Simulate shutdown between deleting the instance and recording success.
	op, err = svc.st.CreateDBOperation(ctx, d, "delete")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ex.RemoveContainer(ctx, ContainerName(d.ID), true); err != nil {
		t.Fatal(err)
	}
	if err := svc.st.DeleteDBInstance(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	svc.Wait()
	got, err = svc.st.GetDBOperation(ctx, op.ID)
	if err != nil || got.Status != "succeeded" || got.InstanceName != d.Name {
		t.Fatalf("deleted instance result = %+v, %v", got, err)
	}
}

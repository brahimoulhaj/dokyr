package api

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"panel/internal/hostcron"
	"panel/internal/store"
)

func TestCronJobs(t *testing.T) {
	e := newEnv(t)
	e.signIn()

	// Not configured: nothing to manage.
	e.must(503, "GET", "/api/cron-jobs", nil)

	type ran struct{ user, command string }
	runs := make(chan ran, 4)
	ctx, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	e.srv.SetCronJobs(hostcron.New(ctx, e.srv.store, log, func(_ context.Context, user, command string, out io.Writer) (int, error) {
		fmt.Fprintln(out, "hello from "+user)
		runs <- ran{user, command}
		if command == "false" {
			return 1, nil
		}
		return 0, nil
	}))

	// Commands on the host: not for maintainers, API tokens or signed-out visitors.
	maint := e.join("maint@b.co", "maintainer")
	job := map[string]any{"name": "Prune", "schedule": "0  3 * * *", "command": "docker system prune -f"}
	for _, c := range [][2]string{
		{"GET", "/api/cron-jobs"}, {"POST", "/api/cron-jobs"}, {"PUT", "/api/cron-jobs/x"}, {"PUT", "/api/cron-jobs/x/enabled"},
		{"DELETE", "/api/cron-jobs/x"}, {"POST", "/api/cron-jobs/x/run"}, {"GET", "/api/cron-jobs/x/runs"},
	} {
		maint.must(403, c[0], c[1], job)
		e.as().must(401, c[0], c[1], job)
	}
	tok := e.must(201, "POST", "/api/tokens", map[string]any{"name": "ci"})
	if code, body := e.bearer(tok["token"].(string), "GET", "/api/cron-jobs", nil); code != 403 {
		t.Fatalf("token list = %d %s", code, body)
	}

	// The session alone does not add a job: the password is asked again, as for the host terminal.
	if got := e.must(200, "GET", "/api/cron-jobs", nil); got["unlocked"] != false || got["enabled"] != true || got["default_user"] != "" {
		t.Fatalf("list = %v", got)
	}
	if got := e.must(403, "POST", "/api/cron-jobs", job); got["error"].(map[string]any)["code"] != "confirm_required" {
		t.Fatalf("locked create = %v", got)
	}
	e.must(204, "POST", "/api/host/terminal/unlock", map[string]any{"password": "s3cret-pass"})

	for _, bad := range []map[string]any{
		{"schedule": "0 3 * * *", "command": "  "},
		{"schedule": "nope", "command": "true"},
		{"schedule": "@daily", "command": "true"},
		{"schedule": "CRON_TZ=UTC 0 3 * * *", "command": "true"},
		{"schedule": "* * * * * *", "command": "true"},
		{"schedule": "0 3 * * *", "command": "true", "user": "bad name; id"},
		{"schedule": "0 3 * * *", "command": "true"}, // no run-as user in Settings: never root by omission
	} {
		e.must(422, "POST", "/api/cron-jobs", bad)
	}

	if got := e.must(200, "GET", "/api/cron-jobs/schedule?expr=*/15+*+*+*+*", nil); len(got["next"].([]any)) != 3 {
		t.Fatalf("schedule preview = %v", got)
	}
	if got := e.must(422, "GET", "/api/cron-jobs/schedule?expr=nope", nil); got["error"].(map[string]any)["message"] != `use a 5-field cron expression in UTC, like "0 3 * * *"` {
		t.Fatalf("invalid schedule = %v", got)
	}
	maint.must(403, "GET", "/api/cron-jobs/schedule?expr=*+*+*+*+*", nil)

	// The default user is the run-as user of Settings; root has to be asked for.
	e.must(200, "PUT", "/api/host/terminal/settings", map[string]any{"enabled": true, "user": "deploy"})
	created := e.must(201, "POST", "/api/cron-jobs", job)
	id := created["id"].(string)
	if created["user"] != "deploy" || created["schedule"] != "0 3 * * *" || created["enabled"] != true || created["next_run_at"] == nil {
		t.Fatalf("created = %v", created)
	}
	asRoot := e.must(201, "POST", "/api/cron-jobs", map[string]any{"schedule": "*/5 * * * *", "command": "false", "user": "root"})
	if asRoot["user"] != "root" {
		t.Fatalf("root job = %v", asRoot)
	}

	updated := e.must(200, "PUT", "/api/cron-jobs/"+id, map[string]any{"name": "Prune images", "schedule": "30 4 * * 0", "command": "docker image prune -af", "user": "ops"})
	if updated["name"] != "Prune images" || updated["user"] != "ops" || updated["schedule"] != "30 4 * * 0" {
		t.Fatalf("updated = %v", updated)
	}
	e.must(404, "PUT", "/api/cron-jobs/missing", job)
	e.must(404, "POST", "/api/cron-jobs/missing/run", nil)

	// Run now: as the job's user, recorded with its output.
	started := e.must(202, "POST", "/api/cron-jobs/"+id+"/run", nil)
	if started["status"] != "running" || started["source"] != "manual" || started["actor"] != "a@b.co" {
		t.Fatalf("started = %v", started)
	}
	select {
	case got := <-runs:
		if got != (ran{"ops", "docker image prune -af"}) {
			t.Fatalf("ran %v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("job did not run")
	}
	history := e.waitCronRun(id)
	if len(history) != 1 || history[0]["status"] != "ok" || history[0]["output"] != "hello from ops\n" {
		t.Fatalf("history = %v", history)
	}
	e.must(202, "POST", "/api/cron-jobs/"+asRoot["id"].(string)+"/run", nil)
	if failed := e.waitCronRun(asRoot["id"].(string)); failed[0]["status"] != "failed" || failed[0]["exit_code"] != float64(1) {
		t.Fatalf("failed run = %v", failed)
	}
	list := e.must(200, "GET", "/api/cron-jobs", nil)["jobs"].([]any)
	if len(list) != 2 || list[0].(map[string]any)["last_run"].(map[string]any)["status"] != "ok" {
		t.Fatalf("list = %v", list)
	}

	// Host access switched off: nothing is added, changed or run; a job can still be
	// switched off or deleted.
	e.must(200, "PUT", "/api/host/terminal/settings", map[string]any{"enabled": false, "user": "deploy"})
	e.must(403, "POST", "/api/cron-jobs", job)
	e.must(403, "PUT", "/api/cron-jobs/"+id, job)
	e.must(403, "POST", "/api/cron-jobs/"+id+"/run", nil)
	e.must(403, "PUT", "/api/cron-jobs/"+id+"/enabled", map[string]any{"enabled": true})
	if off := e.must(200, "PUT", "/api/cron-jobs/"+id+"/enabled", map[string]any{"enabled": false}); off["enabled"] != false || off["next_run_at"] != nil {
		t.Fatalf("switched off = %v", off)
	}
	if got := e.must(200, "GET", "/api/cron-jobs", nil); got["enabled"] != false {
		t.Fatalf("list = %v", got)
	}
	e.must(204, "DELETE", "/api/cron-jobs/"+id, nil)
	e.must(404, "DELETE", "/api/cron-jobs/"+id, nil)
	e.must(404, "GET", "/api/cron-jobs/"+id+"/runs", nil)

	// Every change and run is in Activity.
	events, err := e.srv.store.ListEvents(context.Background(), 50)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]store.Event{}
	for _, ev := range events {
		seen[ev.Action] = ev
	}
	for _, action := range []string{"cron_job.create", "cron_job.update", "cron_job.run", "cron_job.run_failed", "cron_job.disable", "cron_job.delete"} {
		if _, ok := seen[action]; !ok {
			t.Errorf("no %s event", action)
		}
	}
	if ev := seen["cron_job.update"]; ev.TargetName != "Prune images" || ev.Detail != "30 4 * * 0, as ops" || ev.Actor != "a@b.co" {
		t.Fatalf("update event = %+v", ev)
	}
}

// waitCronRun returns a job's runs once none is in progress.
func (e *env) waitCronRun(jobID string) []map[string]any {
	e.t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		runs := e.list("/api/cron-jobs/" + jobID + "/runs")
		if len(runs) > 0 && runs[0]["status"] != "running" {
			return runs
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("run did not finish: %v", runs)
		}
	}
}

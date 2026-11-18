package hostcron

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"panel/internal/store"
)

func TestCheck(t *testing.T) {
	ok := store.CronJob{Name: " Nightly ", Schedule: " 0   3 * * * ", Command: " echo hi ", User: "deploy"}
	j, err := Check(ok)
	if err != nil || j.Name != "Nightly" || j.Schedule != "0 3 * * *" || j.Command != "echo hi" {
		t.Fatalf("Check = %+v %v", j, err)
	}
	for name, change := range map[string]func(*store.CronJob){
		"no command":   func(j *store.CronJob) { j.Command = "" },
		"NUL":          func(j *store.CronJob) { j.Command = "a\x00b" },
		"long command": func(j *store.CronJob) { j.Command = strings.Repeat("a", MaxScript+1) },
		"long name":    func(j *store.CronJob) { j.Name = strings.Repeat("n", 81) },
		"no user":      func(j *store.CronJob) { j.User = "" },
		"bad user":     func(j *store.CronJob) { j.User = "root; id" },
		"descriptor":   func(j *store.CronJob) { j.Schedule = "@hourly" },
		"six fields":   func(j *store.CronJob) { j.Schedule = "0 0 3 * * *" },
		"bad field":    func(j *store.CronJob) { j.Schedule = "61 3 * * *" },
	} {
		bad := ok
		change(&bad)
		if _, err := Check(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestSchedule(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if _, err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	ran := make(chan string, 8)
	release := make(chan struct{})
	svc := New(ctx, st, slog.New(slog.NewTextHandler(io.Discard, nil)), func(_ context.Context, user, command string, out io.Writer) (int, error) {
		io.WriteString(out, strings.Repeat("x", 3*maxOutput)+"\nlast line\n")
		ran <- command
		<-release
		return 0, nil
	})
	hourly, _ := st.CreateCronJob(ctx, store.CronJob{Schedule: "0 * * * *", Command: "hourly", User: "root", Enabled: true})
	st.CreateCronJob(ctx, store.CronJob{Schedule: "0 * * * *", Command: "off", User: "root", Enabled: false})
	st.CreateCronJob(ctx, store.CronJob{Schedule: "30 * * * *", Command: "half past", User: "root", Enabled: true})

	at := func(clock string) time.Time {
		v, _ := time.Parse(time.RFC3339, "2026-01-01T"+clock+"Z")
		return v
	}
	none := func() {
		t.Helper()
		select {
		case c := <-ran:
			t.Fatalf("ran %q", c)
		case <-time.After(50 * time.Millisecond):
		}
	}
	svc.startDue(ctx, at("09:59:30"), at("09:59:45"))
	none()
	svc.startDue(ctx, at("09:59:45"), at("10:00:00"))
	if c := <-ran; c != "hourly" {
		t.Fatalf("ran %q", c)
	}
	none()
	// Still running at its next slot: skipped, not stacked.
	if !svc.Running(hourly.ID) {
		t.Fatal("not marked running")
	}
	svc.startDue(ctx, at("10:59:50"), at("11:00:05"))
	none()
	close(release)
	var runs []store.CronRun
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if runs, _ = st.ListCronRuns(ctx, hourly.ID); len(runs) == 1 && runs[0].FinishedAt != nil {
			break
		}
	}
	if len(runs) != 1 || runs[0].FinishedAt == nil || runs[0].Source != SourceSchedule {
		t.Fatalf("runs = %+v", runs)
	}
	// Only the end of a long output is kept.
	if out := runs[0].Output; len(out) > maxOutput+64 || !strings.HasPrefix(out, "[earlier output dropped]\n") || !strings.HasSuffix(out, "\nlast line\n") {
		t.Fatalf("output: %d bytes, %q…%q", len(out), out[:40], out[len(out)-20:])
	}

	// Host access switched off: nothing runs.
	if err := st.SetSetting(ctx, store.SettingHostTermOff, "true"); err != nil {
		t.Fatal(err)
	}
	svc.startDue(ctx, at("11:59:50"), at("12:00:05"))
	none()
}

func TestLabelSkipsShebangAndComments(t *testing.T) {
	j := store.CronJob{Command: "#!/bin/bash\n# nightly cleanup\n\nset -euo pipefail\ndocker system prune -f"}
	if got := Label(j); got != "set -euo pipefail" {
		t.Fatalf("label = %q", got)
	}
	j.Name = "cleanup"
	if got := Label(j); got != "cleanup" {
		t.Fatalf("label = %q", got)
	}
}

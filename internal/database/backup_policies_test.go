package database

import (
	"context"
	"errors"
	"testing"
	"time"

	"panel/internal/store"
)

func TestNextBackupRun(t *testing.T) {
	for _, c := range []struct {
		frequency, after, want string
		weekday                int
	}{
		{"daily", "2026-11-15T01:00:00Z", "2026-11-15T02:30:00Z", 0},
		{"daily", "2026-11-15T02:30:00Z", "2026-11-16T02:30:00Z", 0},
		{"weekly", "2026-11-15T03:00:00Z", "2026-11-22T02:30:00Z", 0},
		{"weekly", "2026-11-15T01:00:00Z", "2026-11-16T02:30:00Z", 1},
	} {
		at, _ := time.Parse(time.RFC3339, c.after)
		got := NextBackupRun(store.BackupPolicy{Frequency: c.frequency, Hour: 2, Minute: 30, Weekday: c.weekday}, at)
		if got.Format(time.RFC3339) != c.want {
			t.Fatalf("%+v: %s", c, got)
		}
	}
	// Input location must never change the UTC wall-clock schedule.
	at, _ := time.Parse(time.RFC3339, "2026-11-15T03:00:00+01:00")
	if got := NextBackupRun(store.BackupPolicy{Frequency: "daily", Hour: 2, Minute: 30}, at); got.Format(time.RFC3339) != "2026-11-15T02:30:00Z" {
		t.Fatal(got)
	}
}
func TestScheduledBackupsAndRetention(t *testing.T) {
	s, h, project := testService(t)
	host := &backupTestHost{testHost: h}
	s.ex = host
	objects := &memoryBackups{data: map[string][]byte{}}
	s.BackupTarget = func(store.S3Input) (BackupObjects, error) { return objects, nil }
	ctx := context.Background()
	d, e := s.Create(ctx, input(project, "postgres", "18"))
	if e != nil {
		t.Fatal(e)
	}
	s.Wait()
	connection, e := s.st.CreateS3Connection(ctx, store.S3Input{S3Connection: store.S3Connection{Name: "schedule", Endpoint: "s3.example", Bucket: "bucket"}, SecretKey: "secret"})
	if e != nil {
		t.Fatal(e)
	}
	policy := store.BackupPolicy{InstanceID: d.ID, DatabaseName: "app", S3ConnectionID: connection.ID, Enabled: true, Frequency: "daily", Hour: 2, KeepCount: 2}
	invalid := policy
	invalid.KeepCount = 0
	if _, e = s.SaveBackupPolicy(ctx, invalid); !errors.Is(e, ErrInvalid) {
		t.Fatal("zero retention accepted", e)
	}
	invalid = policy
	invalid.DatabaseName = "other"
	if _, e = s.SaveBackupPolicy(ctx, invalid); !errors.Is(e, store.ErrNotFound) {
		t.Fatal("foreign database accepted", e)
	}
	policy, e = s.SaveBackupPolicy(ctx, policy)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.st.DeleteS3Connection(ctx, connection.ID); !errors.Is(e, store.ErrConflict) {
		t.Fatal("policy storage deletion allowed", e)
	}
	// The manual backup must survive all retention runs.
	manual, e := s.BackupAction(ctx, d.ID, "backup", map[string]string{"database": "app", "s3_connection_id": connection.ID})
	if e != nil {
		t.Fatal(e)
	}
	s.Wait()
	at := time.Now().UTC().Add(time.Hour)
	policy.NextRunAt = at.Add(-48 * time.Hour)
	policy, e = s.st.SaveBackupPolicy(ctx, policy)
	if e != nil {
		t.Fatal(e)
	}
	tick := func() {
		t.Helper()
		if e := s.runDuePolicies(ctx, at); e != nil {
			t.Fatal(e)
		}
		s.Wait()
	}
	tick()
	first, _ := s.st.GetBackupPolicy(ctx, policy.ID)
	firstID := first.LastOperationID
	if firstID == "" || !first.NextRunAt.After(at) {
		t.Fatal("schedule not advanced", first)
	}
	tick()
	same, _ := s.st.GetBackupPolicy(ctx, policy.ID)
	if same.LastOperationID != firstID {
		t.Fatal("duplicate backup for same schedule slot")
	}
	at = at.AddDate(0, 0, 1)
	tick()
	at = at.AddDate(0, 0, 1)
	tick()
	list, _ := s.st.ListDBBackups(ctx, d.ID)
	if len(list) != 3 {
		t.Fatalf("retention expected manual + two scheduled, got %d", len(list))
	}
	if _, e = s.st.GetDBBackup(ctx, d.ID, firstID); !errors.Is(e, store.ErrNotFound) {
		t.Fatal("old scheduled backup retained", e)
	}
	if _, e = s.st.GetDBBackup(ctx, d.ID, manual.ID); e != nil {
		t.Fatal("manual backup pruned", e)
	}
	// Failed backups preserve the two previous recovery points.
	host.failDump = true
	at = at.AddDate(0, 0, 1)
	tick()
	p, _ := s.st.GetBackupPolicy(ctx, policy.ID)
	if p.LastError == "" {
		t.Fatal("backup failure not shown on policy")
	}
	list, _ = s.st.ListDBBackups(ctx, d.ID)
	successful := 0
	for _, b := range list {
		if b.Ready {
			successful++
		}
	}
	if successful != 3 {
		t.Fatal("failure pruned successful backups")
	}
	host.failDump = false
	objects.failDelete = true
	at = at.AddDate(0, 0, 1)
	tick()
	p, _ = s.st.GetBackupPolicy(ctx, policy.ID)
	if p.LastError == "" {
		t.Fatal("retention failure hidden")
	}
	op, _ := s.st.GetDBOperation(ctx, p.LastOperationID)
	if op.Status != "succeeded" {
		t.Fatal("cleanup failure falsely failed valid backup")
	}
	objects.failDelete = false
	at = at.AddDate(0, 0, 1)
	tick()
	p, _ = s.st.GetBackupPolicy(ctx, policy.ID)
	if p.LastError != "" {
		t.Fatal("cleanup not retried", p.LastError)
	}
	// Paused policies never run.
	p.Enabled = false
	p.NextRunAt = at.Add(-time.Hour)
	p, e = s.st.SaveBackupPolicy(ctx, p)
	if e != nil {
		t.Fatal(e)
	}
	tick()
	paused, _ := s.st.GetBackupPolicy(ctx, p.ID)
	if paused.LastOperationID != p.LastOperationID {
		t.Fatal("paused policy ran")
	}
	// Stopped instances defer the due slot without losing it.
	p.Enabled = true
	p.NextRunAt = at.Add(-time.Hour)
	p, e = s.st.SaveBackupPolicy(ctx, p)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Enqueue(ctx, d.ID, "stop"); e != nil {
		t.Fatal(e)
	}
	s.Wait()
	tick()
	stopped, _ := s.st.GetBackupPolicy(ctx, p.ID)
	if !stopped.NextRunAt.Equal(p.NextRunAt) {
		t.Fatal("stopped instance lost due slot")
	}
	if _, e = s.Enqueue(ctx, d.ID, "start"); e != nil {
		t.Fatal(e)
	}
	s.Wait()
	tick()
	resumed, _ := s.st.GetBackupPolicy(ctx, p.ID)
	if !resumed.NextRunAt.After(at) {
		t.Fatal("missed slot not caught up")
	}
}

func TestBackupSchedulerShutdown(t *testing.T) {
	s, _, _ := testService(t)
	ctx, cancel := context.WithCancel(context.Background())
	s.StartBackupScheduler(ctx)
	s.StartBackupScheduler(ctx)
	cancel()
	finished := make(chan struct{})
	go func() { s.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not stop")
	}
}

func TestCronBackupPolicies(t *testing.T) {
	at, _ := time.Parse(time.RFC3339, "2026-11-15T03:10:00+01:00")
	if got := NextBackupRun(store.BackupPolicy{Frequency: "cron", Cron: "30 2 * * 1-5"}, at); got.Format(time.RFC3339) != "2026-11-16T02:30:00Z" {
		t.Fatal("cron runs in UTC on the next weekday", got)
	}
	if got := NextBackupRun(store.BackupPolicy{Frequency: "cron", Cron: "0 */6 * * *"}, at); got.Format(time.RFC3339) != "2026-11-15T06:00:00Z" {
		t.Fatal(got)
	}
	for _, expr := range []string{"", "* * * * *", "*/30 * * * *", "0,30 2 * * *", "@every 1s", "@daily", "CRON_TZ=Europe/Paris 0 2 * * *", "0 2 * *", "0 2 * * * *", "61 2 * * *", "0 0 30 2 *"} {
		if _, err := parseBackupCron(expr); !errors.Is(err, ErrInvalid) {
			t.Errorf("accepted %q: %v", expr, err)
		}
	}
	for _, expr := range []string{"0 * * * *", "15 3 1 * *", "0 4 * * 0", "0 0 29 2 *"} {
		if _, err := parseBackupCron(expr); err != nil {
			t.Errorf("rejected %q: %v", expr, err)
		}
	}

	s, _, project := testService(t)
	ctx := context.Background()
	d, e := s.Create(ctx, input(project, "postgres", "18"))
	if e != nil {
		t.Fatal(e)
	}
	s.Wait()
	connection, e := s.st.CreateS3Connection(ctx, store.S3Input{S3Connection: store.S3Connection{Name: "cron", Endpoint: "s3.example", Bucket: "bucket"}, SecretKey: "secret"})
	if e != nil {
		t.Fatal(e)
	}
	p, e := s.SaveBackupPolicy(ctx, store.BackupPolicy{InstanceID: d.ID, DatabaseName: "app", S3ConnectionID: connection.ID, Enabled: true, Frequency: "cron", Cron: "  0   */6 * * * ", KeepCount: 3})
	if e != nil || p.Cron != "0 */6 * * *" || p.Frequency != "cron" || p.NextRunAt.Minute() != 0 || p.NextRunAt.Hour()%6 != 0 {
		t.Fatalf("cron policy %+v %v", p, e)
	}
	// Switching back to daily clears the expression.
	p, e = s.SaveBackupPolicy(ctx, store.BackupPolicy{InstanceID: d.ID, DatabaseName: "app", S3ConnectionID: connection.ID, Enabled: true, Frequency: "daily", Hour: 1, Cron: "0 * * * *", KeepCount: 3})
	if e != nil || p.Cron != "" || p.Frequency != "daily" {
		t.Fatalf("daily policy %+v %v", p, e)
	}
}

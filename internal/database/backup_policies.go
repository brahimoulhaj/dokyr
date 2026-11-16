package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	"panel/internal/store"
)

// cronParser accepts the standard 5 fields only: no descriptors such as @every,
// and no CRON_TZ prefix, so every schedule is plain UTC like daily/weekly ones.
var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// minCronInterval keeps a custom schedule from monopolising the instance lock
// (backups block SQL management and lifecycle work while they run).
const minCronInterval = time.Hour

// parseBackupCron validates a custom schedule and its spacing over the next runs.
func parseBackupCron(expr string) (cron.Schedule, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" || strings.ContainsAny(expr, "@=") || len(strings.Fields(expr)) != 5 {
		return nil, fmt.Errorf("%w: use a 5-field cron expression in UTC, like \"30 2 * * 1-5\"", ErrInvalid)
	}
	schedule, err := cronParser.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid cron expression: %s", ErrInvalid, err.Error())
	}
	at := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	prev := schedule.Next(at)
	if prev.IsZero() {
		return nil, fmt.Errorf("%w: this cron expression never runs", ErrInvalid)
	}
	// Check a few years so sparse fields (days of month, months) are covered.
	for i := 0; i < 2000; i++ {
		next := schedule.Next(prev)
		if next.IsZero() || next.Year() > 2004 {
			break
		}
		if next.Sub(prev) < minCronInterval {
			return nil, fmt.Errorf("%w: custom schedules must run at most once per hour", ErrInvalid)
		}
		prev = next
	}
	return schedule, nil
}

// NextBackupRun uses UTC wall-clock schedules; a missed slot runs once on startup,
// then advances to a future slot instead of replaying every missed backup.
func NextBackupRun(p store.BackupPolicy, after time.Time) time.Time {
	at := after.UTC()
	if p.Frequency == "cron" {
		schedule, err := parseBackupCron(p.Cron)
		if err != nil {
			// Saved policies are validated; never spin on a corrupt row.
			return at.AddDate(0, 0, 1)
		}
		return schedule.Next(at).UTC()
	}
	next := time.Date(at.Year(), at.Month(), at.Day(), p.Hour, p.Minute, 0, 0, time.UTC)
	if !next.After(at) {
		next = next.AddDate(0, 0, 1)
	}
	if p.Frequency == "weekly" {
		for int(next.Weekday()) != p.Weekday {
			next = next.AddDate(0, 0, 1)
		}
	}
	return next
}
func (s *Service) SaveBackupPolicy(ctx context.Context, p store.BackupPolicy) (store.BackupPolicy, error) {
	if p.Frequency == "cron" {
		if _, err := parseBackupCron(p.Cron); err != nil {
			return p, err
		}
		p.Cron = strings.Join(strings.Fields(p.Cron), " ")
		if p.KeepCount < 1 || p.KeepCount > 365 {
			return p, fmt.Errorf("%w: keep between 1 and 365 backups", ErrInvalid)
		}
	} else {
		p.Cron = ""
	}
	if p.Frequency != "daily" && p.Frequency != "weekly" && p.Frequency != "cron" || p.Hour < 0 || p.Hour > 23 || p.Minute < 0 || p.Minute > 59 || p.Weekday < 0 || p.Weekday > 6 || p.KeepCount < 1 || p.KeepCount > 365 {
		return p, fmt.Errorf("%w: choose daily, weekly or a cron schedule, a valid UTC time, and retention between 1 and 365 backups", ErrInvalid)
	}
	if p.Scope == "" {
		p.Scope = "database"
	}
	if p.Scope != "database" && p.Scope != "instance" {
		return p, fmt.Errorf("%w: choose a database or the whole instance", ErrInvalid)
	}
	if !s.acquire(p.InstanceID) {
		return p, ErrBusy
	}
	defer s.release(p.InstanceID)
	if _, e := s.st.GetDBInstance(ctx, p.InstanceID); e != nil {
		return p, e
	}
	names, e := s.st.DBInstanceDatabaseNames(ctx, p.InstanceID)
	if e != nil {
		return p, e
	}
	// An instance policy has no database; its empty name keeps one per instance.
	if p.Scope == "instance" {
		p.DatabaseName = ""
		names = append(names, "")
	}
	found := false
	for _, n := range names {
		if n == p.DatabaseName {
			found = true
		}
	}
	if !found {
		return p, store.ErrNotFound
	}
	if _, e = s.st.GetS3Connection(ctx, p.S3ConnectionID); e != nil {
		return p, e
	}
	p.NextRunAt = NextBackupRun(p, time.Now())
	return s.st.SaveBackupPolicy(ctx, p)
}
func (s *Service) StartBackupScheduler(ctx context.Context) {
	s.schedulerOnce.Do(func() {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				if ctx.Err() != nil {
					return
				}
				_ = s.runDuePolicies(ctx, time.Now())
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	})
}
func (s *Service) runDuePolicies(ctx context.Context, at time.Time) error {
	policies, e := s.st.DueBackupPolicies(ctx, at)
	if e != nil {
		return e
	}
	for _, p := range policies {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !s.acquire(p.InstanceID) {
			continue
		}
		d, e := s.st.GetDBInstance(ctx, p.InstanceID)
		if e != nil || d.Status != "running" || d.DesiredState != store.StateRunning || s.Runtime(ctx, d.ID) != "running" {
			s.release(p.InstanceID)
			continue
		}
		op, e := s.st.QueuePolicyBackup(ctx, p, d, at, NextBackupRun(p, at))
		if e != nil {
			s.release(p.InstanceID)
			if !errors.Is(e, store.ErrConflict) {
				return e
			}
			continue
		}
		s.launch(op)
	}
	return nil
}

// finishPolicyBackup runs while the instance operation lock is still held, so
// retention can never remove a backup currently being restored by this service.
func (s *Service) finishPolicyBackup(ctx context.Context, op store.DBOperation, jobErr error) {
	if op.Action != "backup" {
		return
	}
	payload, e := s.st.DBOperationPayload(ctx, op.ID)
	if e != nil || payload["policy_id"] == "" {
		return
	}
	policyID := payload["policy_id"]
	if jobErr != nil {
		_ = s.st.BackupPolicyError(ctx, policyID, jobErr.Error())
		return
	}
	if e = s.prunePolicyBackups(ctx, policyID); e != nil {
		_ = s.st.BackupPolicyError(ctx, policyID, "Backup succeeded, but retention cleanup failed. Older backups are kept for retry after the next successful scheduled backup.")
	} else {
		_ = s.st.BackupPolicyError(ctx, policyID, "")
	}
}
func (s *Service) prunePolicyBackups(ctx context.Context, policyID string) error {
	p, e := s.st.GetBackupPolicy(ctx, policyID)
	if e != nil {
		return e
	}
	if !p.Enabled {
		return nil
	}
	backups, e := s.st.ListDBBackups(ctx, p.InstanceID)
	if e != nil {
		return e
	}
	kept := 0
	for _, b := range backups {
		if b.PolicyID != p.ID || !b.Ready || b.Status != "succeeded" {
			continue
		}
		kept++
		if kept <= p.KeepCount {
			continue
		}
		// Delete exactly the recorded objects, never list/remove an entire S3 prefix.
		if e = s.deleteBackupObjects(ctx, b); e != nil {
			return e
		}
		if e = s.st.RemovePrunedBackup(ctx, b.ID); e != nil {
			return e
		}
	}
	return nil
}

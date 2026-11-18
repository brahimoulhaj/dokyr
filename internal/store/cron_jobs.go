package store

import (
	"context"
	"database/sql"
	"time"

	"panel/internal/id"
)

// CronJob is a command the panel runs on the host on a schedule.
type CronJob struct {
	ID        string
	Name      string
	Schedule  string // 5-field cron expression, UTC
	Command   string
	User      string // a user of the host; never empty
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// CronRun is one execution of a job. FinishedAt is nil while it runs.
type CronRun struct {
	ID         string
	JobID      string
	Source     string // "schedule" or "manual"
	Actor      string
	StartedAt  time.Time
	FinishedAt *time.Time
	ExitCode   int
	Output     string
	Error      string // why it could not run or was stopped; empty when the command itself ran
}

// cronRunsKept is how many runs of a job stay in its history.
const cronRunsKept = 20

const cronJobColumns = `id, name, schedule, command, run_as, enabled, created_at, updated_at`

func scanCronJob(row scanner) (CronJob, error) {
	var j CronJob
	var created, updated string
	err := row.Scan(&j.ID, &j.Name, &j.Schedule, &j.Command, &j.User, &j.Enabled, &created, &updated)
	if err == sql.ErrNoRows {
		return j, ErrNotFound
	}
	j.CreatedAt, j.UpdatedAt = parseTime(created), parseTime(updated)
	return j, err
}

// ListCronJobs returns every job, oldest first.
func (s *Store) ListCronJobs(ctx context.Context) ([]CronJob, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+cronJobColumns+` FROM cron_jobs ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CronJob{}
	for rows.Next() {
		j, err := scanCronJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *Store) GetCronJob(ctx context.Context, jobID string) (CronJob, error) {
	return scanCronJob(s.db.QueryRowContext(ctx, `SELECT `+cronJobColumns+` FROM cron_jobs WHERE id = ?`, jobID))
}

func (s *Store) CreateCronJob(ctx context.Context, j CronJob) (CronJob, error) {
	j.ID = id.New()
	ts := now()
	j.CreatedAt, j.UpdatedAt = parseTime(ts), parseTime(ts)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO cron_jobs (id, name, schedule, command, run_as, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		j.ID, j.Name, j.Schedule, j.Command, j.User, j.Enabled, ts, ts)
	return j, err
}

// UpdateCronJob saves everything but the ID; ErrNotFound when the job is gone.
func (s *Store) UpdateCronJob(ctx context.Context, j CronJob) (CronJob, error) {
	return scanCronJob(s.db.QueryRowContext(ctx,
		`UPDATE cron_jobs SET name = ?, schedule = ?, command = ?, run_as = ?, enabled = ?, updated_at = ? WHERE id = ? RETURNING `+cronJobColumns,
		j.Name, j.Schedule, j.Command, j.User, j.Enabled, now(), j.ID))
}

// DeleteCronJob removes a job and its run history.
func (s *Store) DeleteCronJob(ctx context.Context, jobID string) (CronJob, error) {
	return scanCronJob(s.db.QueryRowContext(ctx, `DELETE FROM cron_jobs WHERE id = ? RETURNING `+cronJobColumns, jobID))
}

// StartCronRun records a run that has just begun, and drops the job's oldest runs.
func (s *Store) StartCronRun(ctx context.Context, jobID, source, actor string) (CronRun, error) {
	run := CronRun{ID: id.New(), JobID: jobID, Source: source, Actor: actor}
	ts := now()
	run.StartedAt = parseTime(ts)
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO cron_job_runs (id, job_id, source, actor, started_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		run.ID, jobID, source, actor, ts, ts, ts); err != nil {
		return run, err
	}
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM cron_job_runs WHERE job_id = ? AND id NOT IN (SELECT id FROM cron_job_runs WHERE job_id = ? ORDER BY id DESC LIMIT ?)`,
		jobID, jobID, cronRunsKept)
	return run, err
}

// FinishCronRun stores how a run ended. A run whose job was deleted meanwhile is gone; that is not an error.
func (s *Store) FinishCronRun(ctx context.Context, runID string, exitCode int, output, failure string) error {
	ts := now()
	_, err := s.db.ExecContext(ctx,
		`UPDATE cron_job_runs SET finished_at = ?, exit_code = ?, output = ?, error = ?, updated_at = ? WHERE id = ?`,
		ts, exitCode, output, failure, ts, runID)
	return err
}

// FailUnfinishedCronRuns closes runs a previous panel process left open.
func (s *Store) FailUnfinishedCronRuns(ctx context.Context, failure string) error {
	ts := now()
	_, err := s.db.ExecContext(ctx,
		`UPDATE cron_job_runs SET finished_at = ?, exit_code = -1, error = ?, updated_at = ? WHERE finished_at IS NULL`, ts, failure, ts)
	return err
}

func scanCronRun(row scanner, withOutput bool) (CronRun, error) {
	var run CronRun
	var started string
	var finished sql.NullString
	dst := []any{&run.ID, &run.JobID, &run.Source, &run.Actor, &started, &finished, &run.ExitCode, &run.Error}
	if withOutput {
		dst = append(dst, &run.Output)
	}
	if err := row.Scan(dst...); err != nil {
		return run, err
	}
	run.StartedAt = parseTime(started)
	if finished.Valid {
		t := parseTime(finished.String)
		run.FinishedAt = &t
	}
	return run, nil
}

// ListCronRuns returns a job's runs with their output, newest first.
func (s *Store) ListCronRuns(ctx context.Context, jobID string) ([]CronRun, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, job_id, source, actor, started_at, finished_at, exit_code, error, output FROM cron_job_runs WHERE job_id = ? ORDER BY id DESC`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CronRun{}
	for rows.Next() {
		run, err := scanCronRun(rows, true)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

// LastCronRuns returns the newest run of every job, by job ID, without output.
func (s *Store) LastCronRuns(ctx context.Context) (map[string]CronRun, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, job_id, source, actor, started_at, finished_at, exit_code, error FROM cron_job_runs
		 WHERE id IN (SELECT MAX(id) FROM cron_job_runs GROUP BY job_id)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]CronRun{}
	for rows.Next() {
		run, err := scanCronRun(rows, false)
		if err != nil {
			return nil, err
		}
		out[run.JobID] = run
	}
	return out, rows.Err()
}

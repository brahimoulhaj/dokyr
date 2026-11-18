-- Scheduled commands on the host. The panel runs them itself (it does not write the
-- host's crontab), each as run_as through the same helper the host terminal uses.
CREATE TABLE cron_jobs (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL DEFAULT '',
    schedule   TEXT NOT NULL,          -- 5-field cron expression, UTC
    command    TEXT NOT NULL,
    run_as     TEXT NOT NULL,          -- a user of the host; "root" is always explicit
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- The last runs of each job, with what they printed. finished_at is NULL while running.
CREATE TABLE cron_job_runs (
    id          TEXT PRIMARY KEY,
    job_id      TEXT NOT NULL REFERENCES cron_jobs(id) ON DELETE CASCADE,
    source      TEXT NOT NULL,         -- "schedule" or "manual"
    actor       TEXT NOT NULL,         -- who pressed Run now, or "system"
    started_at  TEXT NOT NULL,
    finished_at TEXT,
    exit_code   INTEGER NOT NULL DEFAULT 0,
    output      TEXT NOT NULL DEFAULT '',
    error       TEXT NOT NULL DEFAULT '', -- why it could not run or was stopped; empty when the command itself ran
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);
CREATE INDEX cron_job_runs_job ON cron_job_runs(job_id, id);

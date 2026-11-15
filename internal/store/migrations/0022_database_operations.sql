-- Snapshots survive instance removal so every client can observe its final result.
CREATE TABLE db_operations (
    id TEXT PRIMARY KEY,
    instance_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    instance_name TEXT NOT NULL,
    engine TEXT NOT NULL,
    version TEXT NOT NULL,
    action TEXT NOT NULL CHECK(action IN ('create', 'start', 'stop', 'restart', 'delete')),
    status TEXT NOT NULL CHECK(status IN ('queued', 'running', 'succeeded', 'failed')),
    error TEXT NOT NULL DEFAULT '',
    started_at TEXT,
    finished_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX db_operations_active_instance ON db_operations(instance_id)
    WHERE status IN ('queued', 'running');
CREATE INDEX db_operations_status ON db_operations(status, id);

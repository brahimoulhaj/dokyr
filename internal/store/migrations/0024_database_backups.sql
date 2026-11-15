-- Snapshots survive instance removal so every client can observe its final result.
ALTER TABLE db_operations RENAME TO db_operations_old;

CREATE TABLE db_operations (
    id TEXT PRIMARY KEY,
    instance_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    instance_name TEXT NOT NULL,
    engine TEXT NOT NULL,
    version TEXT NOT NULL,
    action TEXT NOT NULL CHECK(action IN ('create', 'start', 'stop', 'restart', 'delete', 'database_create', 'user_create', 'grant_update', 'backup', 'restore')),
    status TEXT NOT NULL CHECK(status IN ('queued', 'running', 'succeeded', 'failed')),
    error TEXT NOT NULL DEFAULT '',
    payload TEXT NOT NULL DEFAULT '{}',
    detail TEXT NOT NULL DEFAULT '',
    started_at TEXT,
    finished_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
INSERT INTO db_operations SELECT * FROM db_operations_old;
DROP TABLE db_operations_old;
CREATE UNIQUE INDEX db_operations_active_instance ON db_operations(instance_id)
    WHERE status IN ('queued', 'running');
CREATE INDEX db_operations_status ON db_operations(status, id);

CREATE TABLE db_backups (
 id TEXT PRIMARY KEY REFERENCES db_operations(id),
 instance_id TEXT NOT NULL,
 database_name TEXT NOT NULL,
 s3_connection_id TEXT NOT NULL REFERENCES s3_connections(id) ON DELETE RESTRICT,
 object_key TEXT NOT NULL,
 size_bytes INTEGER NOT NULL DEFAULT 0,
 ready INTEGER NOT NULL DEFAULT 0,
 created_at TEXT NOT NULL
);
CREATE INDEX db_backups_instance ON db_backups(instance_id, id);

-- Snapshots survive instance removal so every client can observe its final result.
ALTER TABLE db_operations RENAME TO db_operations_old;

CREATE TABLE db_operations (
    id TEXT PRIMARY KEY,
    instance_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    instance_name TEXT NOT NULL,
    engine TEXT NOT NULL,
    version TEXT NOT NULL,
    action TEXT NOT NULL CHECK(action IN ('create', 'start', 'stop', 'restart', 'delete', 'database_create', 'user_create', 'grant_update')),
    status TEXT NOT NULL CHECK(status IN ('queued', 'running', 'succeeded', 'failed')),
    error TEXT NOT NULL DEFAULT '',
    payload TEXT NOT NULL DEFAULT '{}',
    detail TEXT NOT NULL DEFAULT '',
    started_at TEXT,
    finished_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
INSERT INTO db_operations (id, instance_id, project_id, instance_name, engine, version, action, status, error, started_at, finished_at, created_at, updated_at)
SELECT id, instance_id, project_id, instance_name, engine, version, action, status, error, started_at, finished_at, created_at, updated_at FROM db_operations_old;
DROP TABLE db_operations_old;
CREATE UNIQUE INDEX db_operations_active_instance ON db_operations(instance_id)
    WHERE status IN ('queued', 'running');
CREATE INDEX db_operations_status ON db_operations(status, id);

CREATE TABLE db_databases (
 id TEXT PRIMARY KEY,
 instance_id TEXT NOT NULL REFERENCES db_instances(id) ON DELETE CASCADE,
 name TEXT NOT NULL,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 UNIQUE(instance_id, name)
);
CREATE TABLE db_users (
 id TEXT PRIMARY KEY,
 instance_id TEXT NOT NULL REFERENCES db_instances(id) ON DELETE CASCADE,
 name TEXT NOT NULL,
 credentials TEXT NOT NULL,
 is_initial INTEGER NOT NULL DEFAULT 0,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 UNIQUE(instance_id, name)
);
CREATE TABLE db_user_grants (
 user_id TEXT NOT NULL REFERENCES db_users(id) ON DELETE CASCADE,
 database_id TEXT NOT NULL REFERENCES db_databases(id) ON DELETE CASCADE,
 access TEXT NOT NULL CHECK(access IN ('read_only','read_write','full')),
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 PRIMARY KEY(user_id, database_id)
);

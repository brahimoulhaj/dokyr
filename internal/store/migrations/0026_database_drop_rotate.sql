-- Allow deleting databases/users and rotating passwords. db_backups references
-- db_operations(id), and renaming db_operations would rewrite that reference to the
-- renamed table. Instead copy the rows aside, drop and recreate db_operations under
-- the same name; deferred foreign keys are re-satisfied before the commit.
PRAGMA defer_foreign_keys = ON;
CREATE TEMP TABLE db_operations_copy AS SELECT * FROM db_operations;
DROP TABLE db_operations;

CREATE TABLE db_operations (
    id TEXT PRIMARY KEY,
    instance_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    instance_name TEXT NOT NULL,
    engine TEXT NOT NULL,
    version TEXT NOT NULL,
    action TEXT NOT NULL CHECK(action IN ('create', 'start', 'stop', 'restart', 'delete', 'database_create', 'user_create', 'grant_update', 'backup', 'restore', 'database_delete', 'user_delete', 'password_rotate')),
    status TEXT NOT NULL CHECK(status IN ('queued', 'running', 'succeeded', 'failed')),
    error TEXT NOT NULL DEFAULT '',
    payload TEXT NOT NULL DEFAULT '{}',
    detail TEXT NOT NULL DEFAULT '',
    started_at TEXT,
    finished_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
INSERT INTO db_operations SELECT * FROM temp.db_operations_copy;
DROP TABLE temp.db_operations_copy;
CREATE UNIQUE INDEX db_operations_active_instance ON db_operations(instance_id)
    WHERE status IN ('queued', 'running');
CREATE INDEX db_operations_status ON db_operations(status, id);

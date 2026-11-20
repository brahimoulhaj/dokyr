-- A database user found on the server that the panel did not create: its password is not
-- known here until it is rotated from the panel.
ALTER TABLE db_users ADD COLUMN external INTEGER NOT NULL DEFAULT 0 CHECK (external IN (0, 1));

-- Allow the catalog_sync operation (reading the server's databases, users and permissions
-- into the panel's lists). Rebuilt in place for the reason given in 0026.
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
    action TEXT NOT NULL CHECK(action IN ('create', 'start', 'stop', 'restart', 'delete', 'database_create', 'user_create', 'grant_update', 'backup', 'restore', 'database_delete', 'user_delete', 'password_rotate', 'catalog_sync')),
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

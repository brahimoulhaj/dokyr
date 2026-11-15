CREATE TABLE db_instances (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    name TEXT NOT NULL COLLATE NOCASE,
    engine TEXT NOT NULL CHECK(engine IN ('postgres', 'mariadb')),
    version TEXT NOT NULL,
    database_name TEXT NOT NULL,
    username TEXT NOT NULL,
    credentials TEXT NOT NULL,
    cpu_limit REAL NOT NULL DEFAULT 0,
    memory_mb INTEGER NOT NULL DEFAULT 0,
    desired_state TEXT NOT NULL DEFAULT 'running',
    status TEXT NOT NULL DEFAULT 'provisioning',
    error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(project_id, name)
);
CREATE INDEX db_instances_project ON db_instances(project_id);

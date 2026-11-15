CREATE TABLE backup_policies (
 id TEXT PRIMARY KEY,
 instance_id TEXT NOT NULL REFERENCES db_instances(id) ON DELETE CASCADE,
 database_name TEXT NOT NULL,
 s3_connection_id TEXT NOT NULL REFERENCES s3_connections(id) ON DELETE RESTRICT,
 enabled INTEGER NOT NULL DEFAULT 1,
 frequency TEXT NOT NULL CHECK(frequency IN ('daily','weekly')),
 hour INTEGER NOT NULL CHECK(hour BETWEEN 0 AND 23),
 minute INTEGER NOT NULL CHECK(minute BETWEEN 0 AND 59),
 weekday INTEGER NOT NULL CHECK(weekday BETWEEN 0 AND 6),
 keep_count INTEGER NOT NULL CHECK(keep_count BETWEEN 1 AND 365),
 next_run_at TEXT NOT NULL,
 last_operation_id TEXT NOT NULL DEFAULT '',
 last_error TEXT NOT NULL DEFAULT '',
 updated_at TEXT NOT NULL,
 UNIQUE(instance_id,database_name)
);
ALTER TABLE db_backups ADD COLUMN policy_id TEXT REFERENCES backup_policies(id) ON DELETE SET NULL;
CREATE INDEX db_backups_policy ON db_backups(policy_id,id);
CREATE INDEX backup_policies_due ON backup_policies(enabled,next_run_at);

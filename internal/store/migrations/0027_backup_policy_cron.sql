-- Custom 5-field UTC cron schedules for backup policies. Widening the frequency
-- CHECK needs a rebuild; dropping backup_policies fires db_backups.policy_id's
-- ON DELETE SET NULL, so the links are saved first and restored afterwards.
CREATE TEMP TABLE backup_policy_links AS SELECT id, policy_id FROM db_backups WHERE policy_id IS NOT NULL;
CREATE TEMP TABLE backup_policies_copy AS SELECT * FROM backup_policies;
DROP TABLE backup_policies;

CREATE TABLE backup_policies (
 id TEXT PRIMARY KEY,
 instance_id TEXT NOT NULL REFERENCES db_instances(id) ON DELETE CASCADE,
 database_name TEXT NOT NULL,
 s3_connection_id TEXT NOT NULL REFERENCES s3_connections(id) ON DELETE RESTRICT,
 enabled INTEGER NOT NULL DEFAULT 1,
 frequency TEXT NOT NULL CHECK(frequency IN ('daily','weekly','cron')),
 hour INTEGER NOT NULL CHECK(hour BETWEEN 0 AND 23),
 minute INTEGER NOT NULL CHECK(minute BETWEEN 0 AND 59),
 weekday INTEGER NOT NULL CHECK(weekday BETWEEN 0 AND 6),
 keep_count INTEGER NOT NULL CHECK(keep_count BETWEEN 1 AND 365),
 next_run_at TEXT NOT NULL,
 last_operation_id TEXT NOT NULL DEFAULT '',
 last_error TEXT NOT NULL DEFAULT '',
 updated_at TEXT NOT NULL,
 cron_expr TEXT NOT NULL DEFAULT '',
 CHECK(frequency <> 'cron' OR cron_expr <> ''),
 UNIQUE(instance_id,database_name)
);
INSERT INTO backup_policies (id,instance_id,database_name,s3_connection_id,enabled,frequency,hour,minute,weekday,keep_count,next_run_at,last_operation_id,last_error,updated_at)
SELECT id,instance_id,database_name,s3_connection_id,enabled,frequency,hour,minute,weekday,keep_count,next_run_at,last_operation_id,last_error,updated_at FROM temp.backup_policies_copy;
UPDATE db_backups SET policy_id = (SELECT l.policy_id FROM temp.backup_policy_links l WHERE l.id = db_backups.id)
 WHERE id IN (SELECT id FROM temp.backup_policy_links);
DROP TABLE temp.backup_policy_links;
DROP TABLE temp.backup_policies_copy;
CREATE INDEX backup_policies_due ON backup_policies(enabled,next_run_at);

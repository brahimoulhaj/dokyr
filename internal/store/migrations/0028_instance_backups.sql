-- Whole-instance backups: one dump object per database plus a manifest. "files"
-- records every object key as it is written, so failed or pruned backups can be
-- removed completely without listing the bucket. Instance policies use an empty
-- database name, so UNIQUE(instance_id, database_name) allows one per instance.
ALTER TABLE db_backups ADD COLUMN scope TEXT NOT NULL DEFAULT 'database' CHECK(scope IN ('database','instance'));
ALTER TABLE db_backups ADD COLUMN files TEXT NOT NULL DEFAULT '[]';
ALTER TABLE backup_policies ADD COLUMN scope TEXT NOT NULL DEFAULT 'database' CHECK(scope IN ('database','instance'));

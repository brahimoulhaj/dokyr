-- A backup of one named volume of an app: a gzip-compressed tar of its content, kept in
-- S3 storage. The row outlives the app (app_id is cleared), since the object does too.
CREATE TABLE volume_backups (
    id               TEXT PRIMARY KEY,
    app_id           TEXT REFERENCES apps(id) ON DELETE SET NULL,
    app_name         TEXT NOT NULL,
    volume           TEXT NOT NULL,
    s3_connection_id TEXT NOT NULL REFERENCES s3_connections(id) ON DELETE RESTRICT,
    object_key       TEXT NOT NULL,
    size_bytes       INTEGER NOT NULL DEFAULT 0,
    status           TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'ready', 'failed')),
    error            TEXT NOT NULL DEFAULT '',
    created_at       TEXT NOT NULL,
    finished_at      TEXT
);
CREATE INDEX volume_backups_app ON volume_backups(app_id, id);

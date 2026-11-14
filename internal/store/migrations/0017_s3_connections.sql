-- External S3-compatible storage (AWS, R2, MinIO, RustFS…), used by the built-in registry
-- and later by backups. endpoint is a host[:port] ("s3.amazonaws.com"); secret_key is
-- encrypted with the panel key. prefix is a folder inside the bucket ("" = its root).
CREATE TABLE s3_connections (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    endpoint   TEXT NOT NULL,
    region     TEXT NOT NULL DEFAULT '',
    bucket     TEXT NOT NULL,
    prefix     TEXT NOT NULL DEFAULT '',
    access_key TEXT NOT NULL,
    secret_key TEXT NOT NULL,
    use_ssl    INTEGER NOT NULL DEFAULT 1 CHECK (use_ssl IN (0, 1)),
    path_style INTEGER NOT NULL DEFAULT 0 CHECK (path_style IN (0, 1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

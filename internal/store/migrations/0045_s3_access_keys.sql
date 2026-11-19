-- Access keys of the built-in S3 storage, each limited to one bucket. The key lives in the
-- storage (a RustFS service account with an inline policy); the panel keeps what it is for.
-- The secret key is shown once when it is made and not stored.
CREATE TABLE s3_access_keys (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    bucket     TEXT NOT NULL,
    access_key TEXT NOT NULL UNIQUE,
    permission TEXT NOT NULL,          -- "read" or "write" (read, write and delete objects)
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX s3_access_keys_bucket ON s3_access_keys(bucket);

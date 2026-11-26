-- A hostname that serves nothing itself and sends every request to another hostname,
-- path and query kept: www.example.com to example.com, or an old name to the new one.
-- A hostname is either routed or redirected, never both (the API keeps them apart).
CREATE TABLE redirects (
    id         TEXT PRIMARY KEY,
    hostname   TEXT NOT NULL UNIQUE,
    target     TEXT NOT NULL,
    tls_mode   TEXT NOT NULL DEFAULT 'auto',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- Credentials for the built-in registry (docker login): username = name, password = the
-- token, stored as its SHA-256. scope is "pull" or "push" (push includes pull).
CREATE TABLE registry_tokens (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL UNIQUE,
    token_hash   TEXT NOT NULL UNIQUE,
    scope        TEXT NOT NULL CHECK (scope IN ('pull', 'push')),
    last_used_at TEXT,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);

-- An image app on the built-in registry deploys when its tag is pushed again (1, the
-- default), or only when asked (0).
ALTER TABLE apps ADD COLUMN push_deploy INTEGER NOT NULL DEFAULT 1 CHECK (push_deploy IN (0, 1));

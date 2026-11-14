-- Domains belong to projects now, and routes send a path of a domain to an app: one
-- hostname can serve a frontend at / and a backend at /api. The old per-app domains are
-- dropped, not converted (no installation relies on them yet).
DROP INDEX IF EXISTS domains_one_primary;
DROP INDEX IF EXISTS domains_app_id;
DROP TABLE domains;

CREATE TABLE domains (
    id         TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    hostname   TEXT NOT NULL UNIQUE,
    tls_mode   TEXT NOT NULL DEFAULT 'auto',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX domains_project_id ON domains(project_id);

-- path is "/" or a prefix like "/api" (no trailing slash). The longest matching path
-- wins. strip_prefix = 1 sends /api/users as /users.
CREATE TABLE domain_routes (
    id           TEXT PRIMARY KEY,
    domain_id    TEXT NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    path         TEXT NOT NULL,
    app_id       TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    strip_prefix INTEGER NOT NULL DEFAULT 0 CHECK (strip_prefix IN (0, 1)),
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    UNIQUE (domain_id, path)
);
CREATE INDEX domain_routes_app_id ON domain_routes(app_id);

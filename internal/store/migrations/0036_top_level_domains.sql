-- Domains are added once, on their own page, and a project picks the ones it uses. So a
-- domain no longer needs a project: project_id becomes optional, and deleting a project
-- leaves its domains in the list, unused. Existing domains stay attached to their project.
--
-- SQLite cannot drop NOT NULL, so the table is rebuilt. Dropping it would cascade into
-- domain_routes, so the routes are set aside and put back.
CREATE TEMP TABLE domain_routes_keep AS SELECT * FROM domain_routes;
DROP TABLE domain_routes;

CREATE TABLE domains_new (
    id         TEXT PRIMARY KEY,
    project_id TEXT REFERENCES projects(id) ON DELETE SET NULL,
    hostname   TEXT NOT NULL UNIQUE,
    tls_mode   TEXT NOT NULL DEFAULT 'auto',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
INSERT INTO domains_new (id, project_id, hostname, tls_mode, created_at, updated_at)
    SELECT id, project_id, hostname, tls_mode, created_at, updated_at FROM domains;
DROP TABLE domains;
ALTER TABLE domains_new RENAME TO domains;
CREATE INDEX domains_project_id ON domains(project_id);

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
INSERT INTO domain_routes (id, domain_id, path, app_id, strip_prefix, created_at, updated_at)
    SELECT id, domain_id, path, app_id, strip_prefix, created_at, updated_at FROM domain_routes_keep;
DROP TABLE domain_routes_keep;
CREATE INDEX domain_routes_app_id ON domain_routes(app_id);

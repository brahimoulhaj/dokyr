-- A route may sit on a subdomain of its domain: the project that uses example.com sends
-- shop.example.com and api.example.com to its apps without adding each name as a domain,
-- and "*" sends every other subdomain (tenants of a multi-tenant app) to one.
-- '' is the domain itself. A path is unique per hostname, so the table is rebuilt for the
-- wider UNIQUE.
--
-- Wildcard domains (*.example.com on the Domains page, v0.22.0) are replaced by the "*"
-- route and removed.
DELETE FROM domain_routes WHERE domain_id IN (SELECT id FROM domains WHERE hostname LIKE '*.%');
DELETE FROM domains WHERE hostname LIKE '*.%';

CREATE TABLE domain_routes_new (
    id           TEXT PRIMARY KEY,
    domain_id    TEXT NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    subdomain    TEXT NOT NULL DEFAULT '',
    path         TEXT NOT NULL,
    app_id       TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    strip_prefix INTEGER NOT NULL DEFAULT 0 CHECK (strip_prefix IN (0, 1)),
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    UNIQUE (domain_id, subdomain, path)
);
INSERT INTO domain_routes_new (id, domain_id, path, app_id, strip_prefix, created_at, updated_at)
    SELECT id, domain_id, path, app_id, strip_prefix, created_at, updated_at FROM domain_routes;
DROP TABLE domain_routes;
ALTER TABLE domain_routes_new RENAME TO domain_routes;
CREATE INDEX domain_routes_app_id ON domain_routes(app_id);

-- The panel routes traffic; it does not manage domains. A route now names its hostname
-- itself: hostname + path -> app. There is no list of domains to add first, and a hostname
-- no longer belongs to a project.
--
-- Each route takes the hostname it answered on (the domain, "shop." + the domain, or
-- "*." + the domain) and its domain's TLS mode. Routes of one hostname share a TLS mode.
-- Domains without a route served nothing (404) and are dropped.
CREATE TABLE routes (
    id           TEXT PRIMARY KEY,
    hostname     TEXT NOT NULL,
    path         TEXT NOT NULL,
    app_id       TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    strip_prefix INTEGER NOT NULL DEFAULT 0 CHECK (strip_prefix IN (0, 1)),
    tls_mode     TEXT NOT NULL DEFAULT 'auto',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    UNIQUE (hostname, path)
);
INSERT OR IGNORE INTO routes (id, hostname, path, app_id, strip_prefix, tls_mode, created_at, updated_at)
    SELECT r.id, CASE r.subdomain WHEN '' THEN d.hostname ELSE r.subdomain || '.' || d.hostname END,
        r.path, r.app_id, r.strip_prefix, d.tls_mode, r.created_at, r.updated_at
    FROM domain_routes r JOIN domains d ON d.id = r.domain_id
    ORDER BY r.created_at, r.id;
CREATE INDEX routes_app_id ON routes(app_id);

DROP TABLE domain_routes;
DROP TABLE domains;

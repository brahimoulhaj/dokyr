-- Generated from the migrations by TestSchemaSnapshot (UPDATE_SCHEMA=1). Do not edit.

CREATE TABLE alert_rules (
 id TEXT PRIMARY KEY,
 event TEXT NOT NULL,
 target_id TEXT NOT NULL DEFAULT '',
 channel_id TEXT NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
 enabled INTEGER NOT NULL DEFAULT 1,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL, threshold REAL NOT NULL DEFAULT 0, duration_s INTEGER NOT NULL DEFAULT 0,
 UNIQUE(event,target_id,channel_id)
);

CREATE TABLE api_tokens (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    token_hash   TEXT NOT NULL UNIQUE,
    last_used_at TEXT,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);

CREATE TABLE app_health_checks (
    app_id          TEXT PRIMARY KEY REFERENCES apps(id) ON DELETE CASCADE,
    type            TEXT NOT NULL,              -- tcp, http or running
    path            TEXT NOT NULL DEFAULT '',   -- http: request path
    expected_status INTEGER NOT NULL DEFAULT 0, -- http: 0 = any 2xx or 3xx
    interval_s      INTEGER NOT NULL,
    timeout_s       INTEGER NOT NULL,
    retries         INTEGER NOT NULL,           -- failures in a row, after the start period, that fail the deploy
    start_period_s  INTEGER NOT NULL,           -- failures before it do not count; running: how long to stay up
    updated_at      TEXT NOT NULL
, drain_s INTEGER NOT NULL DEFAULT 10);

CREATE TABLE apps (
    id                    TEXT PRIMARY KEY,
    project_id            TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    name                  TEXT NOT NULL,                    -- slug, unique inside the project
    source_type           TEXT NOT NULL DEFAULT 'image',
    image                 TEXT NOT NULL,                    -- e.g. nginx:1.27 or ghcr.io/org/app:tag
    port                  INTEGER NOT NULL,                 -- port the container listens on
    cpu_limit             REAL NOT NULL DEFAULT 0,          -- cores, 0 = unlimited
    memory_limit_mb       INTEGER NOT NULL DEFAULT 0,       -- MiB, 0 = unlimited
    desired_state         TEXT NOT NULL DEFAULT 'stopped',  -- running | stopped
    current_deployment_id TEXT REFERENCES deployments(id) ON DELETE SET NULL,
    created_at            TEXT NOT NULL,
    updated_at            TEXT NOT NULL, git_repository  TEXT NOT NULL DEFAULT '', git_branch      TEXT NOT NULL DEFAULT '', builder         TEXT NOT NULL DEFAULT '', build_context   TEXT NOT NULL DEFAULT '', dockerfile_path TEXT NOT NULL DEFAULT '', git_source_id TEXT REFERENCES git_sources(id), environment TEXT NOT NULL DEFAULT '{}', mounts TEXT NOT NULL DEFAULT '[]', start_command TEXT NOT NULL DEFAULT '', registry_id TEXT REFERENCES registries(id), deploy_hook_hash TEXT, deploy_hook TEXT, push_deploy INTEGER NOT NULL DEFAULT 1 CHECK (push_deploy IN (0, 1)), processes TEXT NOT NULL DEFAULT '[]', environment_meta TEXT NOT NULL DEFAULT '{}', environment_order TEXT NOT NULL DEFAULT '[]', template TEXT NOT NULL DEFAULT '', deployed_config TEXT NOT NULL DEFAULT '', build_options TEXT NOT NULL DEFAULT '{}', watch_paths TEXT NOT NULL DEFAULT '[]',
    UNIQUE (project_id, name)
);

CREATE TABLE backup_policies (
 id TEXT PRIMARY KEY,
 instance_id TEXT NOT NULL REFERENCES db_instances(id) ON DELETE CASCADE,
 database_name TEXT NOT NULL,
 s3_connection_id TEXT NOT NULL REFERENCES s3_connections(id) ON DELETE RESTRICT,
 enabled INTEGER NOT NULL DEFAULT 1,
 frequency TEXT NOT NULL CHECK(frequency IN ('daily','weekly','cron')),
 hour INTEGER NOT NULL CHECK(hour BETWEEN 0 AND 23),
 minute INTEGER NOT NULL CHECK(minute BETWEEN 0 AND 59),
 weekday INTEGER NOT NULL CHECK(weekday BETWEEN 0 AND 6),
 keep_count INTEGER NOT NULL CHECK(keep_count BETWEEN 1 AND 365),
 next_run_at TEXT NOT NULL,
 last_operation_id TEXT NOT NULL DEFAULT '',
 last_error TEXT NOT NULL DEFAULT '',
 updated_at TEXT NOT NULL,
 cron_expr TEXT NOT NULL DEFAULT '', scope TEXT NOT NULL DEFAULT 'database' CHECK(scope IN ('database','instance')),
 CHECK(frequency <> 'cron' OR cron_expr <> ''),
 UNIQUE(instance_id,database_name)
);

CREATE TABLE cron_job_runs (
    id          TEXT PRIMARY KEY,
    job_id      TEXT NOT NULL REFERENCES cron_jobs(id) ON DELETE CASCADE,
    source      TEXT NOT NULL,         -- "schedule" or "manual"
    actor       TEXT NOT NULL,         -- who pressed Run now, or "system"
    started_at  TEXT NOT NULL,
    finished_at TEXT,
    exit_code   INTEGER NOT NULL DEFAULT 0,
    output      TEXT NOT NULL DEFAULT '',
    error       TEXT NOT NULL DEFAULT '', -- why it could not run or was stopped; empty when the command itself ran
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE TABLE cron_jobs (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL DEFAULT '',
    schedule   TEXT NOT NULL,          -- 5-field cron expression, UTC
    command    TEXT NOT NULL,
    run_as     TEXT NOT NULL,          -- a user of the host; "root" is always explicit
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE db_backups (
 id TEXT PRIMARY KEY REFERENCES db_operations(id),
 instance_id TEXT NOT NULL,
 database_name TEXT NOT NULL,
 s3_connection_id TEXT NOT NULL REFERENCES s3_connections(id) ON DELETE RESTRICT,
 object_key TEXT NOT NULL,
 size_bytes INTEGER NOT NULL DEFAULT 0,
 ready INTEGER NOT NULL DEFAULT 0,
 created_at TEXT NOT NULL
, policy_id TEXT REFERENCES backup_policies(id) ON DELETE SET NULL, scope TEXT NOT NULL DEFAULT 'database' CHECK(scope IN ('database','instance')), files TEXT NOT NULL DEFAULT '[]');

CREATE TABLE db_databases (
 id TEXT PRIMARY KEY,
 instance_id TEXT NOT NULL REFERENCES db_instances(id) ON DELETE CASCADE,
 name TEXT NOT NULL,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 UNIQUE(instance_id, name)
);

CREATE TABLE db_instances (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    name TEXT NOT NULL COLLATE NOCASE,
    engine TEXT NOT NULL CHECK(engine IN ('postgres', 'mariadb')),
    version TEXT NOT NULL,
    database_name TEXT NOT NULL,
    username TEXT NOT NULL,
    credentials TEXT NOT NULL,
    cpu_limit REAL NOT NULL DEFAULT 0,
    memory_mb INTEGER NOT NULL DEFAULT 0,
    desired_state TEXT NOT NULL DEFAULT 'running',
    status TEXT NOT NULL DEFAULT 'provisioning',
    error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL, public_port INTEGER NOT NULL DEFAULT 0 CHECK(public_port BETWEEN 0 AND 65535), public_allow TEXT NOT NULL DEFAULT '',
    UNIQUE(project_id, name)
);

CREATE TABLE db_operations (
    id TEXT PRIMARY KEY,
    instance_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    instance_name TEXT NOT NULL,
    engine TEXT NOT NULL,
    version TEXT NOT NULL,
    action TEXT NOT NULL CHECK(action IN ('create', 'start', 'stop', 'restart', 'delete', 'database_create', 'user_create', 'grant_update', 'backup', 'restore', 'database_delete', 'user_delete', 'password_rotate', 'catalog_sync')),
    status TEXT NOT NULL CHECK(status IN ('queued', 'running', 'succeeded', 'failed')),
    error TEXT NOT NULL DEFAULT '',
    payload TEXT NOT NULL DEFAULT '{}',
    detail TEXT NOT NULL DEFAULT '',
    started_at TEXT,
    finished_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE db_user_grants (
 user_id TEXT NOT NULL REFERENCES db_users(id) ON DELETE CASCADE,
 database_id TEXT NOT NULL REFERENCES db_databases(id) ON DELETE CASCADE,
 access TEXT NOT NULL CHECK(access IN ('read_only','read_write','full')),
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 PRIMARY KEY(user_id, database_id)
);

CREATE TABLE db_users (
 id TEXT PRIMARY KEY,
 instance_id TEXT NOT NULL REFERENCES db_instances(id) ON DELETE CASCADE,
 name TEXT NOT NULL,
 credentials TEXT NOT NULL,
 is_initial INTEGER NOT NULL DEFAULT 0,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL, external INTEGER NOT NULL DEFAULT 0 CHECK (external IN (0, 1)),
 UNIQUE(instance_id, name)
);

CREATE TABLE deployments (
    id             TEXT PRIMARY KEY,
    app_id         TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    status         TEXT NOT NULL,
    trigger        TEXT NOT NULL DEFAULT 'manual',
    image          TEXT NOT NULL,              -- the image this attempt deployed
    port           INTEGER NOT NULL DEFAULT 0, -- the app port at the time; the proxy dials this one
    container_id   TEXT NOT NULL DEFAULT '',
    container_name TEXT NOT NULL DEFAULT '',   -- also the container's DNS name on the shared network
    error          TEXT NOT NULL DEFAULT '',
    log_path       TEXT NOT NULL DEFAULT '',   -- relative to the data dir
    started_at     TEXT,
    finished_at    TEXT,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL
, commit_sha TEXT NOT NULL DEFAULT '', attempts INTEGER NOT NULL DEFAULT 0, rollback_of TEXT NOT NULL DEFAULT '', phase TEXT NOT NULL DEFAULT '', phases TEXT NOT NULL DEFAULT '[]', commit_message TEXT NOT NULL DEFAULT '');

CREATE TABLE events (
    id          TEXT PRIMARY KEY,
    user_id     TEXT REFERENCES users(id) ON DELETE SET NULL,
    actor       TEXT NOT NULL DEFAULT '',   -- email at the time, or 'system'
    action      TEXT NOT NULL,              -- e.g. app.create, deployment.succeeded
    target_type TEXT NOT NULL DEFAULT '',
    target_id   TEXT NOT NULL DEFAULT '',
    target_name TEXT NOT NULL DEFAULT '',
    detail      TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE TABLE git_sources (
    id         TEXT PRIMARY KEY,
    type       TEXT NOT NULL,
    name       TEXT NOT NULL,
    base_url   TEXT NOT NULL,   -- https://github.com, https://gitlab.com or a self-hosted GitLab
    secret     TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE login_identities (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    subject TEXT NOT NULL,
    login TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY(user_id, provider),
    UNIQUE(provider, subject)
);

CREATE TABLE "metrics_samples" (
 kind TEXT NOT NULL CHECK(kind IN ('app','database','host')),
 resource_id TEXT NOT NULL,
 resolution INTEGER NOT NULL CHECK(resolution IN (10,60,3600)),
 ts INTEGER NOT NULL,
 cpu REAL NOT NULL,
 cpu_max REAL NOT NULL,
 memory INTEGER NOT NULL,
 memory_max INTEGER NOT NULL,
 memory_limit INTEGER NOT NULL,
 net_rx REAL NOT NULL,
 net_tx REAL NOT NULL,
 disk_read REAL NOT NULL,
 disk_write REAL NOT NULL,
 PRIMARY KEY(kind,resource_id,resolution,ts)
) WITHOUT ROWID;

CREATE TABLE notification_channels (
 id TEXT PRIMARY KEY,
 name TEXT NOT NULL UNIQUE,
 kind TEXT NOT NULL CHECK(kind IN ('email','slack','discord','telegram','webhook')),
 config TEXT NOT NULL,
 last_sent_at TEXT,
 last_error TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);

CREATE TABLE passkeys (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    rp_id TEXT NOT NULL,
    credential_id TEXT NOT NULL,
    name TEXT NOT NULL,
    credential BLOB NOT NULL,
    created_at TEXT NOT NULL,
    last_used_at TEXT,
    UNIQUE(rp_id, credential_id)
);

CREATE TABLE projects (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE COLLATE NOCASE,
    description TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
, environment TEXT NOT NULL DEFAULT '{}', env_inherit INTEGER NOT NULL DEFAULT 0 CHECK (env_inherit IN (0, 1)), environment_order TEXT NOT NULL DEFAULT '[]');

CREATE TABLE redirects (
    id         TEXT PRIMARY KEY,
    hostname   TEXT NOT NULL UNIQUE,
    target     TEXT NOT NULL,
    tls_mode   TEXT NOT NULL DEFAULT 'auto',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE registries (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    server     TEXT NOT NULL,
    username   TEXT NOT NULL,
    secret     TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE registry_tokens (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL UNIQUE,
    token_hash   TEXT NOT NULL UNIQUE,
    scope        TEXT NOT NULL CHECK (scope IN ('pull', 'push')),
    last_used_at TEXT,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);

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

CREATE TABLE s3_access_keys (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    bucket     TEXT NOT NULL,
    access_key TEXT NOT NULL UNIQUE,
    permission TEXT NOT NULL,          -- "read" or "write" (read, write and delete objects)
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

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

CREATE TABLE sessions (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
, ip_address TEXT NOT NULL DEFAULT '', user_agent TEXT NOT NULL DEFAULT '', last_used_at TEXT);

CREATE TABLE settings (
    id         TEXT PRIMARY KEY,
    key        TEXT NOT NULL UNIQUE,
    value      TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE user_invites (
    id         TEXT PRIMARY KEY,
    email      TEXT NOT NULL UNIQUE COLLATE NOCASE,
    role       TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    invited_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'owner',
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

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

CREATE INDEX alert_rules_event ON alert_rules(event);

CREATE INDEX api_tokens_user_id ON api_tokens(user_id);

CREATE UNIQUE INDEX apps_deploy_hook_hash ON apps(deploy_hook_hash);

CREATE INDEX apps_project_id ON apps(project_id);

CREATE INDEX backup_policies_due ON backup_policies(enabled,next_run_at);

CREATE INDEX cron_job_runs_job ON cron_job_runs(job_id, id);

CREATE INDEX db_backups_instance ON db_backups(instance_id, id);

CREATE INDEX db_backups_policy ON db_backups(policy_id,id);

CREATE INDEX db_instances_project ON db_instances(project_id);

CREATE UNIQUE INDEX db_instances_public_port ON db_instances(public_port) WHERE public_port > 0;

CREATE UNIQUE INDEX db_operations_active_instance ON db_operations(instance_id)
    WHERE status IN ('queued', 'running');

CREATE INDEX db_operations_status ON db_operations(status, id);

CREATE INDEX deployments_app_id ON deployments(app_id, id);

CREATE INDEX events_target ON events(target_type, target_id);

CREATE INDEX metrics_samples_age ON metrics_samples(resolution,ts);

CREATE INDEX passkeys_user ON passkeys(user_id);

CREATE INDEX routes_app_id ON routes(app_id);

CREATE INDEX s3_access_keys_bucket ON s3_access_keys(bucket);

CREATE INDEX sessions_expires_at ON sessions(expires_at);

CREATE INDEX sessions_user_id ON sessions(user_id);

CREATE INDEX volume_backups_app ON volume_backups(app_id, id);

CREATE TRIGGER alert_rules_app_deleted AFTER DELETE ON apps
BEGIN DELETE FROM alert_rules WHERE target_id = OLD.id; END;

CREATE TRIGGER alert_rules_db_instance_deleted AFTER DELETE ON db_instances
BEGIN DELETE FROM alert_rules WHERE target_id = OLD.id; END;

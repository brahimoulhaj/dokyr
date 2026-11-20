# Database instances (M4, first increment)

Back to the [docs index](README.md).

## Instances

Databases → New instance creates a PostgreSQL or MariaDB instance in a project, with
an initial database and application user, separate generated administrator credentials,
a persistent Docker volume, and optional CPU/memory caps. Credentials are encrypted with
the panel key. Instances have no published host ports; applications on the same Docker
network connect internally. PostgreSQL's application user is not a superuser; MariaDB's
application user has privileges on its initial database only.

## Adminer

**Open Adminer** on a running instance opens a new tab, automatically signed in as
its initial application user. Existing SQL grants apply; this is not a server root login.
The panel starts one shared `adminer:5.5.1-standalone` gateway on first use and
checks the browser's panel session on every request. Database passwords stay on
the backend. The gateway has no public port when the panel runs in Docker;
native development publishes it only on `127.0.0.1:18081`, behind a random
internal gateway key. API tokens cannot open Adminer.

## Backups and restore

The instance **Backups** tab creates manual, per-database gzip SQL dumps in an
existing S3 connection and lists status, compressed size and failures. Restore
creates a **new database in the same instance** and grants its initial application
user access; it never overwrites an existing database. PostgreSQL dumps omit
ownership and ACLs; database users, passwords and their grants are not backed up.
MariaDB uses a single-transaction dump (transactional tables should use InnoDB;
avoid schema changes during a backup). S3 connections with backup records cannot
be removed. Daily and weekly backup schedules are configurable per database in the Backups
tab, using an explicit UTC time and (for weekly schedules) day of week. Pause a
schedule by clearing Enabled. Schedules survive panel restarts; one missed run is
queued when the instance becomes available, then the next future slot is used.
Busy or stopped instances keep their due slot for retry. The next slot and its
backup job are saved atomically. A failed backup is shown on the policy and is
retried at the next scheduled slot.

## Retention

Retention keeps the latest 1–365 successful backups belonging to that policy.
After each successful scheduled backup, older successful copies are permanently
removed from S3 before their backup rows are removed. Manual backups and failed
backup rows are retained. Cleanup failures are shown separately from backup
success and retried after the next successful scheduled backup. Paused policies
are not pruned. S3 connections referenced by a policy cannot be removed. Arbitrary
cron expressions, age-based retention and manual backup deletion are not available
yet. Jobs have a ten-minute timeout. Restore verifies gzip integrity using a 0600
temporary SQL file, limited to 20 GiB uncompressed, and removes it afterward;
allow sufficient free temporary disk. Interrupted restores are marked failed and
may leave a partial **new** database for inspection. Restore again under a new name.

## Instances in projects

Each project's **Databases** tab lists only its own instances, with engine/version,
current status and panel-managed database names. Open an instance to manage its databases,
users and permissions, or use **New instance** to create one in that project.
`GET /api/projects/{id}/databases` provides the same scoped list without credentials.
An instance's **Settings → Project** selector moves it between projects, preserving its
data, users, credentials, container and volume. Moves are rejected while an operation is
active or if the destination already has an instance with the same name. App references
then resolve in the destination project; old-project references must be updated before
deploying again. Running apps keep their existing connections. This immediate metadata
save uses loading/success/error toasts and `PATCH /api/databases/{id}` with
`{"project_id":"…"}`; container and SQL operations continue to use the durable queue.

## Connecting an app

In an app's environment, set `DATABASE_URL=${db.main.URL}` (for an instance named `main`),
or use `${db.main.HOST}`, `${db.main.PORT}`, `${db.main.DATABASE}`,
`${db.main.USERNAME}` and `${db.main.PASSWORD}`. References are limited to that app's
project and applied on its next deployment; a missing or stopped instance fails the deploy.
Connection → Reveal shows only the application credentials and requires a browser session.

The connection screen is framework-neutral. Use the environment variable names and URL
format expected by your app, composing a client-specific URL from the individual
connection references when needed. Save and redeploy to apply changes. Client-specific
prefixes and options belong in the app's environment, alongside generic database references.

## Lifecycle operations

Creation, Start, Stop, Restart and Remove run in the background, appear in the navbar's
queue and the existing toast stack, and keep their progress when the user navigates away.
Success toasts appear only on completion; failed operations stay visible until dismissed.
Creation and Start/Restart report ready only after SQL readiness and user initialization.
Operations resume after a panel restart with the same operation ID; intentionally stopped
instances stay stopped. Remove requires the instance name and retains the named data
volume (shown in the UI); it removes the panel record and credentials, so retain connection
details separately if needed. Public ports, backups and restore are
not implemented yet. **Database data has no automatic backup in this increment.**

## Instance API

The API is authenticated: `GET/POST /api/databases`, `GET/DELETE /api/databases/{id}`,
`POST /api/databases/{id}/start|stop|restart`, `GET /api/databases/{id}/connection`,
`GET /api/databases/{id}/logs`, and `GET /api/database-engines`. Mutations return 202 with
an `operation` record. Poll `GET /api/database-operations/{operation-id}` for its result or
`GET /api/database-operations/active` for ongoing work. The final result remains available
after instance removal. Poll the instance for its `status` and observed container `runtime`. Delete takes
`{"name":"main"}`. The logs endpoint returns the last 200 container lines.

## Databases, users and grants

The instance's **Databases & users** tab lists panel-managed databases, users and grants,
including the initial application database and user. It creates additional databases and
users and assigns each user **No access**, **Read only**, **Read & write** (SELECT, INSERT,
UPDATE, DELETE), or **Full access** (also table creation) on each database. Administrator
accounts are excluded. User passwords are generated, stored encrypted and revealed only
through a browser session; creation/grant inputs in operation records are encrypted too.
These operations use the shared queue/toasts and the same per-instance lifecycle lock.
An SQL management failure does not change the healthy instance's Running status.

PostgreSQL presets cover `public`, existing tables/sequences and future objects created by
managed users. Full access permits creation in `public`, without server administration or
arbitrary schema creation. A user owning objects must transfer ownership before access can
be reduced. Initial application access to its original database is protected. MariaDB
grants are scoped to one database, with underscore wildcards escaped.

**Sync with server** reads the instance and makes these lists say what is there: databases,
users and permissions made outside the panel (in Adminer, over SQL) are added, the ones
dropped there are forgotten, and each permission is set to the level the server grants
(a level is read from what the user can do: connect, read tables, write them, create in
the database). It runs as a queued operation, only reads, and never changes the server.
A user found this way is marked **Made outside the panel**: the panel has no password for
it, so its connection details are unavailable until its password is rotated here. The
instance's initial database, initial user and that user's access are never touched. Names
the panel cannot handle safely (unusual characters) and administrator accounts are left out.

## Databases, users and grants API

Endpoints: `GET /api/databases/{id}/catalog`, `POST /api/databases/{id}/databases` and
`POST /api/databases/{id}/users` (body `{"name":"analytics"}`), and
`PUT /api/databases/{id}/grants` (body `{"database_id":"…","user_id":"…","access":"read_only"}`).
Writes require a browser session and return a tracked operation (202). User credentials:
`GET /api/databases/{id}/users/{user-id}/connection?database_id={database-id}`, session only,
with a no-store response (409 `external_user` for a user made outside the panel whose
password was not rotated yet). `POST /api/databases/{id}/catalog/sync` starts a sync.

## Testing against real engines

Real SQL permissions, restart and volume persistence can be checked with an isolated
Docker network and temporary volumes (removed by the tests):

```sh
go test ./internal/database -run TestRealDatabaseInstances -v
```

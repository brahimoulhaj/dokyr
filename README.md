# Dokyr

A lightweight, single-node PaaS panel: one Go binary with an embedded React app, SQLite for
state, Docker for workloads and Caddy in front. The name lives in `internal/brand/brand.go`;
nothing else hard-codes it.

Progress is tracked in [Roadmap.md](Roadmap.md).

## Install

On a fresh Ubuntu or Debian server (amd64 or arm64) with ports 80 and 443 open:

```sh
curl -fsSL https://github.com/brahimoulhaj/paas/releases/latest/download/install.sh | sudo sh
```

It installs Docker if needed, starts the panel and prints a one-time setup link
(`http://<server-ip>/setup?token=…`). Options go between `sudo` and `sh`, e.g.
`… | sudo DOKYR_PUBLIC_URL=http://203.0.113.10 sh`. A specific version:
`https://github.com/brahimoulhaj/paas/releases/download/v0.1.1/install.sh`.

Later versions install from the panel (Settings → Updates) or over SSH with `dokyr update`
(`--check` only reports, `--yes` skips the prompt).

## Requirements

Go 1.26+, Node 22+, pnpm 10+, and a running Docker daemon (the panel starts without one, but
cannot deploy; the executor tests skip themselves).

## First run

```sh
make deps      # go mod tidy (creates go.sum) + pnpm install
make dev-api   # Go server on :8090, data in ./.data — prints a one-time setup link;
               # also starts Caddy in Docker on :8880 (HTTP) and :8443 (HTTPS).
               # Rebuilds and restarts on Go changes when `air` is installed (.air.toml)
make dev-web   # in a second terminal: Vite on :5173, proxies /api to :8090
```

Open the setup link from the `dev-api` output, create the owner, and you are in.

## Development with Docker (live reload)

```sh
make dev        # builds two dev images, starts them with `docker compose watch`
```

Open http://localhost:5173. Edits show up on their own:

- `web/**` is synced into the Vite container and hot-swapped in the browser (about 1 s).
- `cmd/` and `internal/` are synced into the API container, which restarts and recompiles
  (about 2 s). The browser keeps working after the restart.
- `go.mod`, `go.sum`, `web/package.json` and the lockfile rebuild the images.

Stop it with Ctrl-C, or `make dev-down` from another terminal. Caddy (`dokyr-proxy`) and
deployed apps keep running, as they would during a panel update; the next start adopts them.

Panel data (database, key, certificates, deployment logs) lives in the Docker volume
`dokyr-data`, not in `./.data`. The panel is told so (`DOKYR_DATA_VOLUME`) and mounts Caddy's
folders from that volume. It survives `make dev-down`; `docker compose down -v` deletes it.
`make dev-import-data` replaces its content with a copy of `./.data` (from `make dev-api`).

Don't run `make dev` and `make dev-api` at the same time: two panels on one Docker host
fight over the proxy container.

## Build

```sh
make build              # bin/dokyr, web app embedded
make test               # go vet, go test, web typecheck
make image VERSION=0.1.0
```

## Releases

Push a tag `vX.Y.Z`: the Release workflow publishes `ghcr.io/brahimoulhaj/paas:X.Y.Z` (and
`:latest`) for amd64 and arm64 and attaches the matching `install.sh` to a GitHub release.
Installed panels see the new tag in Settings → Updates.

## Install on a server

```sh
make image VERSION=0.1.0                       # local build, tagged ghcr.io/brahimoulhaj/paas:0.1.0
make installer VERSION=0.1.0 [IMAGE=...]       # writes bin/install.sh from install/install.sh.tmpl
```

On the server (Linux, amd64 or arm64), as root: `curl -fsSL <url>/install.sh | sh`. It checks
ports 80/443, installs Docker from get.docker.com when missing, creates `/var/lib/dokyr` and the
`dokyr` network, writes `/var/lib/dokyr/compose.yaml` and starts the panel, then prints the
setup link. It also installs a `dokyr` host command that runs the CLI inside the container
(`dokyr setup-token`, `dokyr owner reset-password`). Running it again keeps the data.

Overrides: `DOKYR_IMAGE`, `DOKYR_PUBLIC_URL`, and `DOKYR_PULL=never` to use an image already
loaded with `docker load` (no registry needed for testing).

## CLI

```
dokyr serve
dokyr setup-token                                  new one-time setup link (15 min)
dokyr owner create --email E [--password P]
dokyr owner reset-password [--email E] [--password P]
dokyr version
```

Configuration is by environment: `DOKYR_DATA_DIR` (default `/var/lib/dokyr`), `DOKYR_ADDR`
(default `:8080`), `DOKYR_PUBLIC_URL` (base URL for printed links), `DOKYR_MAX_BUILDS` (Git builds
run at once, default 1; further Git deploys wait, queued, and can be cancelled; queued and interrupted deploys
start again after a panel restart, at most 3 times),
`DOKYR_BUILD_CPUS` and `DOKYR_BUILD_MEMORY_MB` (default CPU and memory all builds share, e.g.
`1.5` and `2048`; unset means no cap; Settings → Builds overrides them without a restart). Capped builds run in a BuildKit container of their own
(`moby/buildkit`, created on the first build, about 100 MB to download) because Docker's
built-in BuildKit ignores per-build limits; a build step over the memory cap is killed.

The dashboard samples host CPU, RAM, swap, load, uptime, data-disk capacity and network
traffic every five seconds, keeping ten minutes of history in memory. The disk is the
filesystem holding `DOKYR_DATA_DIR`; available space excludes filesystem reservations.
For host network readings inside Docker, mount `/proc:/host/proc:ro` on the panel
container (included in the installer and development Compose file). Existing installations
can re-run the installer or add that mount and recreate the panel container. Image-only
updates preserve existing mounts. `DOKYR_HOST_PROC` overrides the proc path for custom
setups. On Docker Desktop these are the Linux VM's metrics. Unavailable readings are
shown explicitly, and loopback, container bridges and tunnels are excluded from network
totals. Metrics are available only through the authenticated `/api/host/metrics` endpoint.

The Docker dashboard card uses the connected daemon's read-only API to show container
states, images, volumes, networks, engine configuration and per-resource storage usage.
Counts refresh every 15 seconds; aggregate disk scans run at most once a minute.
Sizes and reclaimable space are Docker's estimates, exclude bind mounts and container
logs, and are shown by category because image and build-cache layers may overlap.
If Docker becomes unavailable, the card labels the last known readings. The endpoint
is `/api/docker/metrics` and requires authentication.

Proxy: `DOKYR_PROXY_HTTP_PORT` / `DOKYR_PROXY_HTTPS_PORT` (default 80 / 443; `make dev-api` uses
8880 / 8443), `DOKYR_PROXY_IMAGE`, `DOKYR_PROXY_PANEL_UPSTREAM` (how Caddy reaches the panel),
`DOKYR_PROXY_ADMIN` (`socket` on Linux, `tcp` elsewhere) and `DOKYR_PROXY_ADMIN_PORT` (tcp mode,
default 2019 on 127.0.0.1). Socket mode needs the panel to run as root and the data dir mounted
at the same path inside the panel container as on the host.

One panel per Docker host: the network, the proxy container and the container labels are all
named after the product slug, so two panels on the same daemon would fight over them.

## Layout

```
cmd/panel/            main: serve, owner, setup-token
internal/brand/       product name and slug — the only place they live
internal/config/      environment configuration
internal/store/       SQLite queries and embedded migrations
internal/secret/      AES-GCM encryption with a key file in the data dir
internal/auth/        password hashing, opaque tokens
internal/account/     owner setup, login, recovery (shared by API and CLI)
internal/executor/    container host seam; local Docker implementation
internal/proxy/       Caddy container, admin API client, config builder
internal/deploy/      deploy pipeline: pull, start, switch route, remove old; start/stop/restart
internal/database/    project-scoped Postgres/MariaDB provisioning, lifecycle, connection references
internal/api/         HTTP handlers, middleware, embedded SPA
internal/webui/       go:embed target for the built web app
web/                  React + TypeScript + Vite + Tailwind
```

## Database instances (M4, first increment)

Databases → New instance creates a PostgreSQL or MariaDB instance in a project, with
an initial database and application user, separate generated administrator credentials,
a persistent Docker volume, and optional CPU/memory caps. Credentials are encrypted with
the panel key. Instances have no published host ports; applications on the same Docker
network connect internally. PostgreSQL's application user is not a superuser; MariaDB's
application user has privileges on its initial database only.

**Open Adminer** on a running instance opens a new tab, automatically signed in as
its initial application user. Existing SQL grants apply; this is not a server root login.
The panel starts one shared `adminer:5.5.1-standalone` gateway on first use and
checks the browser's panel session on every request. Database passwords stay on
the backend. The gateway has no public port when the panel runs in Docker;
native development publishes it only on `127.0.0.1:18081`, behind a random
internal gateway key. API tokens cannot open Adminer.

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

In an app's environment, set `DATABASE_URL=${db.main.URL}` (for an instance named `main`),
or use `${db.main.HOST}`, `${db.main.PORT}`, `${db.main.DATABASE}`,
`${db.main.USERNAME}` and `${db.main.PASSWORD}`. References are limited to that app's
project and applied on its next deployment; a missing or stopped instance fails the deploy.
Connection → Reveal shows only the application credentials and requires a browser session.

The connection screen is framework-neutral. Use the environment variable names and URL
format expected by your app, composing a client-specific URL from the individual
connection references when needed. Save and redeploy to apply changes. Client-specific
prefixes and options belong in the app's environment, alongside generic database references.

Creation, Start, Stop, Restart and Remove run in the background, appear in the navbar's
queue and the existing toast stack, and keep their progress when the user navigates away.
Success toasts appear only on completion; failed operations stay visible until dismissed.
Creation and Start/Restart report ready only after SQL readiness and user initialization.
Operations resume after a panel restart with the same operation ID; intentionally stopped
instances stay stopped. Remove requires the instance name and retains the named data
volume (shown in the UI); it removes the panel record and credentials, so retain connection
details separately if needed. Public ports, backups and restore are
not implemented yet. **Database data has no automatic backup in this increment.**

The API is authenticated: `GET/POST /api/databases`, `GET/DELETE /api/databases/{id}`,
`POST /api/databases/{id}/start|stop|restart`, `GET /api/databases/{id}/connection`,
`GET /api/databases/{id}/logs`, and `GET /api/database-engines`. Mutations return 202 with
an `operation` record. Poll `GET /api/database-operations/{operation-id}` for its result or
`GET /api/database-operations/active` for ongoing work. The final result remains available
after instance removal. Poll the instance for its `status` and observed container `runtime`. Delete takes
`{"name":"main"}`. The logs endpoint returns the last 200 container lines.

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
grants are scoped to one database, with underscore wildcards escaped. Custom users and
grants made outside the panel are not discovered or reconciled in this increment.

Endpoints: `GET /api/databases/{id}/catalog`, `POST /api/databases/{id}/databases` and
`POST /api/databases/{id}/users` (body `{"name":"analytics"}`), and
`PUT /api/databases/{id}/grants` (body `{"database_id":"…","user_id":"…","access":"read_only"}`).
Writes require a browser session and return a tracked operation (202). User credentials:
`GET /api/databases/{id}/users/{user-id}/connection?database_id={database-id}`, session only,
with a no-store response. Deletion of individual databases/users and password rotation
remain for a later increment.

Real SQL permissions, restart and volume persistence can be checked with an isolated
Docker network and temporary volumes (removed by the tests):

```sh
go test ./internal/database -run TestRealDatabaseInstances -v
```

The dashboard polls `/api/apps/problems` every five seconds to show crashing, down or
missing app containers, with project names and links to their logs. Intentionally stopped
apps and apps currently deploying are excluded. Recovery removes the alert on the next
poll; an unavailable Docker daemon is reported separately from an app crash.

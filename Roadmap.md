# Dokyr — Roadmap

Implementation tracker for the single-node PaaS panel. Source of truth for the design is the
architecture doc ("Single-node PaaS panel: architecture and data model"). The product is named
Dokyr (dokyr.com, earlier working name Berth); the name lives only in `internal/brand/brand.go`.

**How to use:** tick a box only when the item is built *and* verified (test or manual run).
`[ ]` not started · `[~]` in progress · `[x]` done.

**Progress:** M0 ◐ · M1 ◐ · M2 ◐ (nearly done) · M3 ◐ · M4 ◐ (instances, users/grants with drop and password rotation, Adminer, S3 backups/restore/deletion, daily/weekly/cron retention policies, whole-instance backups, backups of deleted instances, public ports; checked on the VPS) · M5 ◐ (registry done and checked on the VPS) · M6 ◐ (container metrics with history and charts; notifications and alert rules; continuous health monitoring; usage threshold alerts; more engines remain)

**Where we are (2026-11-17, v0.20.0):** The navbar and the command palette open a host terminal: root shells on the server, in tabs, docked under the page and resizable (`GET /api/host/terminal`, `web/src/components/host-terminal.tsx`). A shell runs in a short-lived privileged helper container that enters the host's namespaces (`nsenter`), is removed when the session ends, and is limited to the owner's browser session, with open and close in Activity. Checked in the browser against OrbStack, where the host is Docker's Linux VM; not yet checked on the VPS. Settings → Security now lists active browser sessions, marks the current one, and lets the owner sign out one session or all other sessions while keeping the current browser. Other-session revocation requires password confirmation. GitHub sign-in now registers a private app automatically through the manifest flow, stores credentials encrypted, and continues to account authorization without manual Client ID or secret fields. Tests cover setup, linking/login, callback replay, session binding, provider errors, and concurrent configuration changes; authentication tests pass with the race detector, store/auth tests, Go vet and the web build pass, and the controls were checked in the browser. First-time GitHub app creation and authorization require the owner’s confirmation on GitHub. v0.19.1: sign-in automatically reads the saved HTTPS panel domain after an update, including HTTP-only CDN upstreams and installations whose original public URL is an HTTP IP address; no environment-file edits or manual container recreation are needed. Regression tests cover replacement-image startup, stale/missing installer URLs, callback URLs and rejection of untrusted origins. v0.19.0: passkeys and GitHub sign-in are available in Settings → Security, with password confirmation in an action-specific dialog. Passkeys use discoverable WebAuthn credentials and device verification; GitHub accounts must be explicitly linked, with OAuth state and PKCE protection. OAuth credentials are encrypted, password recovery remains available, and the UI is translated in all four languages. Authentication handler tests, Go vet, store/auth tests, all-package compilation, and the web production build pass; the dialog was checked in the browser. Live device enrollment needs an authenticator; first-time GitHub sign-in now registers a private app automatically and needs the owner to confirm app creation and account authorization on GitHub; the broader API suite could not run in the local sandbox because it cannot bind test ports. v0.18.0: the product is renamed Dokyr (slug `dokyr`: command, `/var/lib/dokyr`, `DOKYR_*`, network and container names); no migration from `berth`, installs are removed and installed again. v0.17.3: the sidebar is grouped (Workloads, Infrastructure, System) with counts, a status dot per recent project, storage state and the Settings sections, and a + to create a project or an app in a listed project (`web/src/components/sidebar.tsx`). v0.17.2: the New app sheet takes environment variables (.env text) and CPU and memory limits in optional sections; every sheet keeps its action buttons at the bottom, outside the scrolling content (`SheetActions` in `web/src/components/sheet.tsx`). v0.17.1: host and Docker metrics are pushed over SSE (`GET /api/metrics/stream`) and queries poll only while their stream is down, so an idle Overview sends no requests. v0.17.0: the panel backs itself up: Settings → Backup sends an encrypted copy of its database and key (age, passphrase) to S3 on a schedule or on demand, and `panel restore FILE` stages one for the next start (round-trip checked against RustFS). Built-in S3 storage (M5, Experimental): Storage → Turn on runs RustFS in a container with a volume, makes a "panel" bucket and a storage connection to it, and manages buckets. v0.16.2 added live updates: the panel announces committed changes over one server-sent event stream (`GET /api/changes`) and the web UI refetches only what changed, instead of polling every 1–15 s (an idle tab went from about 10 requests in 12 s to none). v0.16.1 added a command palette (Ctrl/Cmd+K) searches every resource and runs common actions (`GET /api/search`). v0.16.0 added CPU and memory threshold alerts (rules with a threshold and a 1–60 minute duration, per app, per database or all; `0033_usage_alerts.sql`); searchable, paged projects and databases lists with richer rows; Activity search with filters and readable labels for every action; outlined and solid sidebar icons. Before that, v0.15.0 added continuous health monitoring: every 30 s running apps get their container inspected and their own TCP/HTTP check run; health shows on the app Overview, and unhealthy/recovered/crashed events feed alert rules (checked on real Docker with Mailpit); rules go with their app or instance (`0032_alert_rule_targets.sql`). v0.14.0 added notifications: channels (email over SMTP, Slack, Discord, Telegram, a signed webhook) with a test send, and alert rules for deploys started/succeeded/failed, failed backups and a nearly full disk (Settings → Notifications; `0031_notifications.sql`); email checked against Mailpit. Before that, v0.13.0 added container metrics: every 10 s the panel reads Docker stats of each running app and database container (`internal/metrics`; the public-port gate is left out) and stores one sample per app or instance (containers of one app are added together during a rolling deploy): CPU in cores, memory without reclaimable cache, memory limit, network and disk rates. Samples roll up to minutes and hours with average and peak; raw samples are kept 24 hours, minutes 7 days, hours 30 days (`0030_metrics.sql`, `metrics_samples`). `GET /api/apps/{id}/metrics` and `GET /api/databases/{id}/metrics` take `range=1h|6h|24h|7d|30d` and answer at most about 360 points. App and database pages have a Metrics tab with four Recharts charts (CPU, memory with its limit, network, disk), time axis across the whole range, gaps where no samples exist and a hover tooltip, in four languages; Recharts 3.10.1 is pinned and loaded only when the tab opens (its own chunk, about 107 kB gzipped). A resource's first reading (new container, panel restart) only primes the counters, so charts never show a false drop to zero. Checked in the browser against a real PostgreSQL instance, at desktop and phone width. The VPS checks of v0.12.3–v0.12.4 are done (public-port allowlist with real IPs, Adminer edits, retention on S3, the Backups tab controls) and backups of deleted instances can be restored or deleted from the Databases page (`TestOrphanBackups`). `TestRealPublicPort/mariadb` sometimes loses the TLS handshake when the whole suite runs at once; it passes on its own.

**Earlier milestones:** recreate with a volume is checked on the VPS (data kept across a deploy, a short gap, a failed health check rolls back). v0.12.0 adds project databases (M4): Postgres and MariaDB instances per project with encrypted credentials, lifecycle controls, an internal data volume, `${db.main.URL}` references, management operations and the Databases pages; deploys report problems per app. Real Docker checks for MariaDB and the VPS are still pending. v0.11.5 adds a queue button to the navbar (a count and a list of the deployments running or waiting; each opens its app's Deployments tab) and turns the deployment toasts into a stack, one card per deployment, so several in parallel are all in view (ding shows one toast at a time). v0.11.4: the registry page has Images (how to push and pull, a table of image, tag, platform and size, with several tags in one collapsible row) and Settings (settings, turn off, tokens); the registry address carries the proxy's port in development. v0.11.3: the token endpoint accepts the space-separated scopes of containerd's login (Docker Desktop), which fixed `insufficient_scope` on push. v0.11.2: an HTTP-only option for the registry domain, for Cloudflare "Flexible" (it looped on redirects). v0.11.0 checked on the VPS (2026-11-15): the built-in registry on a real domain (docker login and push from a laptop, push redeploys, S3 storage, build cache) and a project domain with `/` → frontend and `/api` → backend with shared variables; recreate with a volume checked on the VPS too (2026-11-15: data kept, short gap, failed check rolls back). v0.11.0 moves domains and shared variables to the project: a project domain's paths go to apps (`/` → frontend, `/api` → backend, prefix kept unless stripped), and project variables are used as `${project.NAME}` or inherited by every app when the project says so (off by default); forms open in side sheets; the registry page lists every registry in one list. v0.10.0 added the built-in registry. Earlier: M2 is done except pushing builds to a registry, more builders (Nixpacks, Buildpacks, Static), a Gitea token, a build-time env flag and cron. M3 has push/CI webhooks, health checks before the traffic switch (v0.8.0), rollback (v0.8.0), the deploy state machine with its timeline (v0.8.0), the Deployments tab as a paged accordion (v0.8.1), and Git deployments named by their commit message (v0.8.2, `0015_deployment_commit_message.sql`); a check-for-updates button beside the version in the navbar offers Update now / Dismiss in a toast (v0.8.2); the rolling strategy now drains the old container (default 10 s) before removing it (v0.8.3); apps with volumes deploy with recreate (v0.9.0). The built-in registry (M5, v0.10.0): off by default, its own domain, Docker volume or S3 storage, registry tokens, deploy on push, build cache, repositories with delete and garbage collection; checked against real Docker, Distribution and RustFS, and on the VPS with a real domain. v0.8.2 checked on the VPS (2026-11-14): update from the navbar toast (v0.8.1 → v0.8.2), commit messages with the hover tooltip, HTTP and TCP health checks (a failing check keeps the old container), Git rollback without a build, the live timeline and paging. v0.6.0 checked on the VPS (2026-11-14): build caps, resume after restart, deploy keys, private registries, deploy webhooks, Railpack without a Dockerfile, cancel in the browser.

**Next up:**
1. M6 is done except more database engines (MongoDB, ClickHouse). Next candidates: M3 TLS (DNS-01 wildcards, custom certificates, redirects, Certificates page), M2 builders (Nixpacks, Buildpacks, Static). Built-in S3 left: per-bucket access keys, a real domain, and checking database backups and the registry against it on the VPS.
2. `TestRealPublicPort` fails intermittently on this Mac (passed on 2026-11-16 15:05), also at v0.13.0 (Docker is OrbStack; `host.docker.internal` resolves to 0.250.250.254): find out how the gate is reached there and make the test wait for it. Check whether "Build log stream on the deployment page" is already covered by the deployment log (Git build output streams there with `|` lines) and tick it if so.
3. Record the fresh-install time with image download (M1, "Under 60 seconds").
---

## M0 — Project skeleton

Everything needed before the first feature: repo layout, tooling, and the shared plumbing.

### Repository and tooling
- [x] Go module (`panel`, brand-neutral so a rename never touches imports), `cmd/panel` entrypoint, package layout from the doc
- [x] `internal/brand` — `Name` and `Slug` constants; every path, network, env prefix derives from them
- [x] `internal/config` — data dir, listen address, env vars with the `DOKYR_` prefix
- [x] Makefile: `deps`, `dev-api`, `dev-web`, `web`, `build`, `test`, `image`
- [ ] Linting (golangci-lint, eslint)
- [x] Multi-stage `Dockerfile` (web build → Go build → slim runtime image) — `make image`, 36 MB, runs on a fresh Ubuntu 24.04 arm64 VM
- [x] `.gitignore`, `.editorconfig`, README with dev setup
- [x] CI: vet, test (`-short -race`), web typecheck and build, installer renders — `.github/workflows/ci.yml`, green on `main`
- [ ] Decide license (open question in the doc)

### Store (`internal/store`)
- [x] SQLite via `modernc.org/sqlite` (pure Go), WAL mode, foreign keys on, busy timeout — `make test` green with the real driver (2026-11-10)
- [x] Migrations embedded in the binary, run at startup (small built-in runner, `internal/store/migrate.go`; goose can replace it if down-migrations are ever needed)
- [~] Pre-migration backup with `VACUUM INTO` — runs when migration 0002 is applied to an existing database; not yet checked by restoring one
- [x] ULID ids (strictly increasing within a process), `created_at` / `updated_at` on every table
- [ ] sqlc for typed queries
- [x] `internal/secret` — AES-GCM encryption with a key file in the data dir

### HTTP API (`internal/api`)
- [x] Router (std `net/http`), request logging, panic recovery, security headers, JSON error format
- [x] `GET /api/health`
- [x] `GET /api/meta` — product name, slug, version, setup state (the UI hard-codes no product string)
- [x] Serve the embedded SPA with client-side-routing fallback
- [x] SSE helper for log and status streams (`internal/api/sse.go`); open streams end on shutdown
- [x] Live updates (v0.16.2, 2026-11-16): SQLite commit hooks record which watched tables a transaction wrote (`internal/store/changes.go`); `GET /api/changes` streams the table names (never row data), coalesced over 150 ms, and ends once the session is signed out; the web UI invalidates the matching queries (`web/src/lib/live.ts`) and polls only while the stream is down. Host and Docker metrics are pushed whole over `GET /api/metrics/stream` (v0.17.1); every stream writes a comment line first, since Node's proxy (Vite) held headers until body bytes arrived. The metrics charts, the update check and database logs still poll

### Web app (`web/`)
- [x] Vite + React + TypeScript + Tailwind
- [x] TanStack Router and TanStack Query
- [~] ReUI / shadcn components copied into the repo — searchable combobox (`web/src/components/combobox.tsx`, shadcn's combobox on Base UI, the one ReUI builds on, restyled with the theme tokens); used for repository access, repository and branch; other components as they are needed
- [x] Theme tokens: zero radius, 1 px borders, monospace body, pixel font for headings and badges, one accent on a dark base
- [x] App shell: sidebar, sign-out, overview page with projects, empty state and recent activity
- [x] API client with typed errors; product name read from `/api/meta`
- [x] Dev proxy to the Go server; production build embedded with `go:embed`
- [~] Languages: English, French, Arabic (right to left) and Spanish — typed dictionaries per screen in `web/src/i18n`, `web/src/lib/i18n.tsx` (choice kept in the browser, defaults to the browser's language), searchable language menu on the globe button of the navbar and on the sign-in screens; messages written by the server (API errors, deployment logs) stay in English

---

## M1 — Foundation

Goal: install on a real VPS, create the owner, deploy a public image, attach a domain with automatic TLS, watch live logs.

### Auth and owner
- [x] Migration: `users`, `sessions`, `api_tokens`, `settings`
- [x] Password hashing — PBKDF2-HMAC-SHA256, 600k iterations (stdlib); the stored format carries an algorithm id so argon2id can be added later
- [x] One-time setup token, valid 15 minutes, stored hashed
- [x] `POST /api/setup` — create the owner with a valid setup token
- [x] `POST /api/auth/login`, `POST /api/auth/logout`, `GET /api/auth/me`
- [x] Cookie sessions (HttpOnly, SameSite, Secure when on HTTPS), expiry and cleanup
- [x] Auth middleware; CSRF protection for cookie-authenticated writes (same-origin check)
- [x] Login and setup rate limiting (per IP, in memory; the client IP comes from Caddy's `X-Forwarded-For` when the peer is private)
- [x] Passkey sign-in and management in Settings → Security: discoverable WebAuthn credentials, required user verification, browser-bound single-use challenges, origin validation, persistent counters and revocation; password confirmation for registration/removal. HTTP handler tests cover registration/sign-in, origin and UV rejection, replay and deletion; device enrollment still needs a real browser authenticator check.
- [x] GitHub sign-in in Settings → Security: password-confirmed **Link GitHub account** automatically prepares a private GitHub App with the manifest flow, stores its credentials encrypted, then continues to account authorization. No manual Client ID/secret fields. State and PKCE, explicit numeric account identity (no email-based signup), session-only management. Tests cover manifest conversion, linking/login, browser and session binding, replay, provider errors, and preservation of an existing configuration. Live app creation/authorization still needs the owner's confirmation on GitHub.
- [x] Active sessions in Settings → Security: browser/device, IP, sign-in and last activity, current-session badge, individual sign-out and password-confirmed sign-out of all other sessions while retaining the current cookie. Owner-scoped browser-session-only API, throttled metadata, live list updates, and automatic migration preserving existing logins. Migration/store tests and authentication handler tests (race detector) pass; session list and confirmation dialog checked in the browser.
- [x] API tokens: create, list, revoke, bearer auth (`/api/tokens`, `Authorization: Bearer dokyr_…`); token management needs the browser session; last use recorded at most once a minute; events name the token. Checked by `internal/api/tokens_test.go` and in a browser against the dev stack

### CLI (same binary)
- [x] `dokyr serve`
- [x] `dokyr owner create`
- [x] `dokyr owner reset-password`
- [x] `dokyr setup-token`
- [x] `dokyr version`

### Executor (`internal/executor`)
Verified by `internal/executor/docker_test.go` against a real daemon (skipped with `-short` or when no daemon answers).

- [x] `Executor` interface — the seam for multi-server later
- [x] Local Docker implementation over the mounted socket (`github.com/moby/moby/client`, API version negotiated)
- [x] Ensure the `dokyr` Docker network exists (at `serve` startup)
- [x] Image pull with progress
- [x] Container create / start / stop / remove / inspect, with labels identifying panel-managed containers (`dokyr.managed`, `dokyr.kind`, `dokyr.resource`); json-file log rotation 10 MB × 3
- [x] Container log streaming (follow, tail, since), stdout and stderr demultiplexed
- [x] CPU and memory limits
- [x] Reconcile on startup: adopt running managed containers (proxy and apps), log orphans; unfinished deployments are resumed (see Jobs and queue)

### Proxy (`internal/proxy`)
Verified by `internal/proxy/manager_test.go` against a real Caddy container (skipped with `-short` or when no daemon answers).

- [x] Start and supervise the Caddy container (ports 80/443, data under `<data dir>/proxy`); recreated when its spec changes, checked every 30 s
- [x] Caddy admin API client — unix socket in the data dir on Linux, TCP on 127.0.0.1 for development on macOS/Windows
- [x] Config builder: full Caddy JSON derived from SQLite (panel domain and one route per app with domains)
- [x] Rebuild and push the full config on startup (also written to the file Caddy boots from)
- [x] Route `http://<server-ip>` to the panel UI
- [x] Panel domain with automatic TLS — `GET/PUT /api/settings/panel`, verified with Caddy's internal CA; on a real VPS as `http_only` behind Cloudflare's proxy (`ops.azayr.com`) and with a Let's Encrypt certificate on a direct (unproxied) domain (2026-11-11); settings screen and onboarding step done
- [x] Per-app route with automatic TLS (`tls_mode = auto`) — verified end to end with Caddy's internal CA on a `.test` name, and on a real VPS with a public domain and a Let's Encrypt certificate (2026-11-11)
- [x] Atomic upstream patch for a single route
- [x] Socket admin mode end to end on a Linux host — installed panel on an Ubuntu 24.04 VM: `proxy ready admin=socket`, deploy and route switch work (2026-11-11)

### Projects and apps (image source only)
Verified by unit tests (fake container host) and one manual end-to-end run against real Docker and Caddy: deploy, redeploy with zero failed requests, failed deploy keeps the old container, stop/start, panel restart, delete.

- [x] Migration: `projects`, `apps`, `domains`, `deployments`, `events` (`0002_projects_apps.sql`)
- [x] Projects CRUD — `/api/projects`
- [x] Apps CRUD — source type `image`, public registry, port, CPU and memory limits — `/api/projects/{id}/apps`, `/api/apps/{id}`
- [x] Domains: add, remove, hostname validation, DNS check hint — `/api/apps/{id}/domains`, `/api/domains/{id}/dns`
- [x] Primary domain per app (`0007_primary_domain.sql`): the first domain added is primary, can be switched (`PATCH /api/domains/{id}`), and is handed to the oldest remaining domain when the primary is removed; one primary per app enforced by a unique index
- [x] Minimal deploy: pull → start → switch route → remove old (no health checks yet; a container that exits or restarts within 3 s fails the deploy) — `internal/deploy`
- [x] Deployment record with status and log file path — `/api/deployments/{id}`, `/api/deployments/{id}/log`
- [x] Start / stop / restart an app
- [x] `events` rows for every mutating action (audit log, activity feed) — `/api/events`
- [ ] Automated integration test of a deploy against a real Docker daemon (today's end-to-end run was a throwaway script)

### Live logs
Verified by API tests (with the race detector) and in a browser against a real nginx container: lines appear as requests arrive, search, pause, wrap, follow, stop and start.

- [x] SSE endpoint for container runtime logs — `/api/apps/{id}/logs/stream`; resumes after the last line on reconnect
- [x] SSE endpoint for deployment logs, persisted per deployment — `/api/deployments/{id}/log/stream`; follows the file, resumes by byte offset
- [x] Log viewer in the UI (follow, pause, search, wrap); keeps the last 5000 lines

### Installer
- [x] `install.sh` generated from the brand constants — `make installer` fills `install/install.sh.tmpl`; shellcheck clean (hooking it into a release pipeline is under Cross-cutting)
- [x] Checks: root, OS, architecture, ports 80 and 443 free (skipped on a re-run, which finds its own proxy there)
- [x] Docker Engine check and install from Docker's official script — verified on a VM without Docker (installed 29.9.3 with Compose)
- [x] Create `/var/lib/dokyr` and the Docker network
- [x] Pull and start the panel image with the socket and data dir mounted — Compose project in `/var/lib/dokyr/compose.yaml`; installed on a real VPS from `ghcr.io/brahimoulhaj/paas` with the one-command install (2026-11-11)
- [x] Print the setup URL with the one-time token; `dokyr` host command wraps the CLI in the container
- [~] Under 60 seconds on a server that already has Docker — 2 s with the images already local; image download time from a registry still to measure

### Updates
- [x] Short-lived updater container that pulls the new image and recreates the panel — `internal/update` (registry check by semver tag or digest, helper runs `update-apply` to swap the container, result recorded as an event); registry and compose-rewrite logic unit-tested; on a real VPS the panel updated itself 0.1.x → 0.2.0 → 0.2.1 from GHCR (2026-11-11)
- [x] `dokyr update` and an update button with the available version — Settings → Updates: check, update now, hourly automatic updates (wait while a deploy runs); `dokyr update [--check] [--yes]` (same check and helper; refuses while a helper is already swapping); on the VPS the CLI swapped the panel to 0.2.4 (2026-11-11)

### UI
Checked in a browser against a real deploy (create project and app, add domain, deploy, failed redeploy, stop/start, delete), desktop and phone width.

- [x] Setup screen (token → owner email and password)
- [x] Login screen
- [~] Onboarding stepper — `/onboarding`, shown after setup and until finished or skipped (saved on the server: `/api/onboarding`), reachable again from Settings. Steps today: panel domain, first deploy. S3, registry and notifications steps arrive with M4, M5 and M6
- [x] Projects list and project page (create, rename, delete)
- [x] Create-app flow (image)
- [x] App page: overview, deployments (live log), logs (live container output), domains (with DNS hint), settings
- [x] Activity feed
- [x] API tokens section on the Settings page: create (secret shown once, copy, curl example), list with last use, revoke
- [x] Panel domain options: HTTP only (CDN terminates TLS) and "disable access by IP" (unknown hosts dropped; needs a domain; recovery through `127.0.0.1:8080` over SSH); both on in production on a real VPS (2026-11-11)
- [x] Settings page with the panel domain section: set, change, remove, certificate email, DNS hint (`/api/settings/panel/dns`); checked in a browser with `panel.test` through Caddy's internal CA

---

## M2 — Git and builds

Goal: push-to-deploy from a repository with any of the builders.

### Git sources
- [~] Migration: `git_sources`, `registries`, `env_vars` — `0003_git_source.sql` adds the Git columns to `apps` and `commit_sha` to `deployments`; `0004_git_sources.sql` adds `git_sources` (secrets encrypted) and `apps.git_source_id`; `0005_app_environment.sql` keeps env vars as a JSON column on `apps`; `registries` comes with its feature
- [x] Public Git URL — app `source_type = git` with an https repository and a branch (`/api/projects/{id}/apps`, `PATCH /api/apps/{id}`; URL, branch and path validation tested in `internal/api/apps_test.go`); create-app flow and settings offer Image or Git repository; deployed end to end on a real VPS (2026-11-13)
- [x] Private repo by deploy key (generate key pair, show public key) — Settings → Git connections → Add deploy key (`POST /api/git-sources/deploy-key`): an ed25519 key pair, private half encrypted with the panel key, public half shown with Copy and where to paste it on GitHub, GitLab and Gitea; apps using it give an SSH URL (`git@host:org/app.git` or `ssh://git@host:port/…`), validated against option injection (`TestSSHRepo`). The clone runs over SSH only, with the key in a private temp folder, no user config or agent, and the host key trusted on first use and pinned in `<data dir>/ssh/known_hosts`; a changed host key or a key the server refuses gives a clear error. `openssh-client` added to the images. `TestFetchOverSSH` clones from a throwaway sshd in Docker (wrong key and changed host key refused); `TestDeployKey`, API test; a private GitHub repository deployed by deploy key on a real VPS with v0.6.0 (2026-11-14)
- [x] GitHub App (first provider) — created from Settings with GitHub's manifest flow (`POST /api/git-sources/github`, callback `/api/git-sources/github/callback`), read-only on code and metadata; app JWT and cached installation tokens; `internal/gitsource` tested against a fake GitHub; created, installed and used to deploy a private repository on a real VPS (2026-11-13). Webhooks come with M3
- [~] GitLab token — gitlab.com or self-hosted over https, token checked when added (`POST /api/git-sources/gitlab`); tested against a fake GitLab; not yet tried against GitLab itself
- [ ] Gitea token
- [x] Repo and branch picker in the UI — `GET /api/git-sources/{id}/repos` and `/branches`; the app form lists the connection's repositories and the chosen one's branches; used with a real GitHub connection (2026-11-13)
- [x] Clone / fetch at a commit into a build workspace, with cleanup — shallow clone of the branch tip into `<data dir>/builds/<deployment>` (no credential prompt or host credential helper, https only), removed after the build and at startup; commit recorded on the deployment; a private repository is fetched with the connection's token, passed to git through the environment and scoped to the repository's host

### Builders (`internal/builder`)
- [x] Builder interface: source dir in, tagged local image out — `internal/builder` (`Fetch`, `Build`); the deploy pipeline builds instead of pulling for Git apps (`internal/deploy`, fake builder tests)
- [x] Auto-detect: Dockerfile if present, otherwise Railpack — builder `auto`, the default for new Git apps; checks the configured Dockerfile path inside the build context and says in the deploy log which builder it picked (`TestBuildAutoPicksBuilder`)
- [x] Dockerfile — `docker buildx build --load` with build context and Dockerfile path checked to stay inside the repository (symlinks followed); image labelled `<slug>.build=<app id>`; checked against a real daemon in `internal/builder/builder_test.go` and on a real VPS (2026-11-13)
- [x] Railpack — `railpack prepare` writes a plan beside the workspace, `docker buildx build` runs it through the pinned `railpack-frontend` on the same daemon (no separate BuildKit); the frontend drops `--label`, so a one-line `FROM` build re-tags with the build label that pruning relies on; Git apps get `PORT` set to the app port unless their env sets it. Real build in `TestBuildRailpack` (needs `railpack` on PATH or `DOKYR_TEST_RAILPACK`); an app without a Dockerfile built by Railpack on a real VPS with v0.6.0 (2026-11-14)
- [x] Start command override — per app (image or Git), replaces the image's entrypoint and command with `/bin/sh -c <command>` so `$PORT` and `$(…)` expand; empty keeps the image's own; shown on the overview, set in Settings (`0008_app_start_command.sql`, `TestStartCommand`, `TestDockerEntrypointOverride`). Needed for single-module Gradle apps: Railpack 0.43.0 starts them with `*/build/libs/*jar`, which only matches subprojects (seen on a Spring Boot app from GitLab, 2026-11-13)
- [ ] Nixpacks — `nixpacks build`
- [ ] Cloud Native Buildpacks — `pack build --builder <image>` (Heroku / Paketo)
- [ ] Static — build command, output served by a small Caddy image
- [~] Builder CLIs shipped in the panel image, pinned versions — `git`, `docker-cli` and `docker-cli-buildx` from Alpine in the runtime and dev images (not pinned); Railpack 0.43.0 pinned and checksummed in both (+22.5 MB), kept equal to `builder.RailpackVersion` by `TestRailpackPinMatchesImages`
- [x] Image tag `app-<id>:<commit-sha>`; keep the last 5, prune older — after each successful deploy the newest 5 tagged build images stay (the live one always), older and dangling ones (a commit rebuilt under the same tag) are removed; Docker refuses any image a container still uses; deleting an app removes all its build images. `internal/deploy/deploy_test.go` and `internal/executor/docker_test.go` (real daemon)
- [x] Push to the configured registry after a build — the built-in registry's build cache (off by default, Registry page → Keep Git builds): after a successful Git deploy the image is pushed as `<registry>/builds/app-<id>:<commit>` and only its local name kept (pruning works by id); a rollback to a build pruned from the server pulls it back instead of building, and such builds count as rollback targets. A failed push is a warning. `TestBuildCache`

### Jobs and queue (`internal/jobs`)
- [x] SQLite-backed job queue with retries and resume after restart — the `deployments` table is the queue (a generic jobs table can come with backups and cron). On startup, queued deploys start again; a running one that had not switched traffic starts over, its half-made container removed; one that had switched is finished (old containers removed, succeeded); at most 3 starts per deploy (`attempts`, `0009_deployment_attempts.sql`). A shutdown puts running and waiting deploys back in the queue instead of failing them. Resumed deploys wait for the proxy. Clone, pull and Git credentials are retried twice (2 s, 5 s) on network errors; builds are not. `TestRecoverResumesDeployments`, `TestShutdownRequeues`, `TestRetryTemporaryFailures`, `TestTemporary`; checked against real Docker and Caddy: panel stopped mid-clone, deploy resumed at attempt 2 and succeeded (2026-11-13); panel restarted mid-deploy, deploy resumed and succeeded on a real VPS with v0.6.0 (2026-11-14)
- [x] One build at a time by default, configurable — `DOKYR_MAX_BUILDS`; a Git deploy waits, still `queued`, for a build slot and says so in its log; the slot is released once the image exists (pulls take none). Queued deploys survive a restart (below) (`TestBuildsRunOneAtATime`, `TestMaxBuildsRunsInParallel`)
- [x] CPU and memory caps on builds — Settings → Builds (`GET/PUT/DELETE /api/settings/builds`, `TestBuildSettings`; checked in a browser: save, invalid memory, reset), defaults from `DOKYR_BUILD_CPUS`, `DOKYR_BUILD_MEMORY_MB` (unset = no cap); read at the start of every build, shared by all builds; Docker's built-in BuildKit ignores per-build limits, so capped builds run on a buildx `docker-container` builder (`moby/buildkit:v0.34.2`) created with the limits on first use and replaced when they change (an old one is removed once no build runs on it); Railpack's label step stays on the daemon's builder, which holds the loaded image. `TestBuildLimits` (stub), `TestBuildMemoryCapEnforced` (real daemon: a 400 MB step killed under a 256 MB cap); a Git app deployed under a build cap on a real VPS with v0.6.0 (2026-11-14)
- [x] Cancel a queued or running build — `POST /api/deployments/{id}/cancel` and a Cancel button on the deployment log; works until traffic switches (then 409 `not_cancellable`); the build or pull is killed, a new container removed, the live version keeps serving; status `cancelled`, event `deployment.cancelled` (`TestCancelDeployment`); build cancelled from the browser on a real VPS with v0.6.0 (2026-11-14)
- [ ] Cron scheduler (`robfig/cron`)

### Env vars and registries
- [~] Env vars CRUD, encrypted at rest, build-time flag — per-app variables passed to the container on deploy (`0005_app_environment.sql`, `internal/store/environment_test.go`, `internal/deploy/deploy_test.go`); encrypted at rest with the panel key (AES-GCM, `internal/store/environment.go`); environments stored in clear by earlier versions are encrypted at startup (`TestEnvironmentEncryptedAtRest`); no build-time flag yet (no build arguments exist)
- [x] Bulk edit / `.env` paste — `web/src/lib/environment.ts` parses and writes `.env` text (comments, `export`, quoted values)
- [x] External private registries: add, test credentials, pull with auth — the Registry page in the sidebar (`/registry`; `GET/POST/DELETE /api/registries`); adding signs in through the Docker daemon (`RegistryLogin`), a refused password is reported; the password is encrypted with the panel key and never returned; Docker Hub names are normalized to `docker.io`. A registry in use cannot be removed. `0010_registries.sql`, `TestRegistries`, `TestRegistriesAPI`, `TestPrivateRegistry` (real `registry:2` with password auth: wrong password refused, pull without credentials refused, with them works); checked in a browser against a local registry: refused password, then add, deploy and remove (2026-11-13)
- [x] App source: image from a private registry — image apps choose Registry access (public or a registry); credentials are only sent when the image is on that registry's host, checked on save and again at deploy (`TestDeployFromPrivateRegistry`); the deploy log names the account, never the password; a private image deployed from GHCR or Docker Hub on a real VPS with v0.6.0 (2026-11-14)

### UI
- [~] Git sources settings — Settings → Git connections: connect GitHub, connect GitLab, remove (refused while an app uses it); GitHub and GitLab connected for real (2026-11-13)
- [x] Create-app flow (Git) with builder choice — Automatic, Dockerfile or Railpack; the Dockerfile field hides for Railpack; the app overview shows the builder
- [ ] Build log stream on the deployment page — probably done already (Git build output streams into the deployment log); check and tick
- [x] App settings in sections — General, Source, Resources, Environment, Volumes, Webhook, Danger zone, in a side menu (a strip on phones), the section in the URL; each saves only its fields
- [x] Env vars editor — `web/src/components/environment.tsx`, with a configurable log line limit in the log viewer
- [x] Registries page — its own page (sidebar → Registry), not in Settings: add (server, user, password or read-only token), list, remove

---

## M3 — Deploy quality

Goal: zero-downtime deploys you can trust, with rollback and every trigger.

### State machine (`internal/deploy`)
- [x] States: `queued → building → starting → health_checking → switching → draining → succeeded` — `status` stays queued / running / succeeded / failed / cancelled; while running, `phase` is the step (`pulling` for an image app instead of `building`; a Git rollback goes straight to `starting`) and `phases` records each step entered with its time, closed by the final status (`0014_deployment_phases.sql`, `EnterDeploymentPhase`). A failed deployment keeps the phase it failed in. `TestDeployPhases`, `TestDeployPhasesGit`, `TestDeploymentPhasesAPI`; checked in the browser with an image app (2026-11-14). Draining is still an immediate remove (graceful drain comes with the rolling strategy)
- [x] Exits: `failed`, `cancelled`, `rolled_back` — `rolled_back` is a recreate deploy whose new container failed after the old one was stopped: the old one started again and serves (phase `rolling_back`, event `deployment.rolled_back`, a red toast saying the previous version runs again). `TestDeployRecreateRollsBack`
- [x] Resume or fail cleanly after a panel restart mid-deploy; no orphan containers — see Jobs and queue (M2); per-state resume comes with the state machine

### Health checks
- [~] Migration: `app_health_checks`, `volumes`, `certificates` — `0012_app_health_checks.sql` (one row per app, removed with it); volumes live in `apps.mounts` (`0006`); `certificates` comes with custom TLS
- [x] `tcp` (default) — what new apps get (port accepts a connection, start period 30 s, every 2 s, 3 failures in a row). Apps created before keep the old 3-second stay-up wait until a check is saved (`LegacyHealthCheck`); the settings say so
- [x] `http` — path, expected status, interval, timeout, retries, start period — GET on the container, redirects not followed, expected status 0 = any 2xx or 3xx; failures during the start period do not count, one success passes; a crash fails at once whatever the check (`TestHealthCheck`, `TestHealthCheckStartPeriod`, `TestHealthCheckAPI`)
- [x] `running` — stays up for N seconds — for workers that do not listen
- [x] Checks run by the panel from inside the Docker network — by container name and port, before the traffic switch; a failed check removes the new container and leaves the route. A panel run on the host (`make dev-api`) cannot reach containers, so it only waits 3 s and says so in the log (`TestHealthCheckOutsideNetwork`, checked in the browser (2026-11-14)). On a real VPS with v0.8.2 (2026-11-14): an HTTP check passed, a wrong path failed and kept the old container, a new app got the TCP check
- [x] Continuous health monitoring after deploy (feeds alerts in M6) — `internal/deploy/monitor.go`: every 30 s each running, deployed app not mid-deploy gets its container inspected (stopped, restart loop, restarts, OOM kill) and its own TCP/HTTP check run (Retries failures in a row = unhealthy; skipped when the panel runs outside Docker); health on the app's Overview; events `app.health_failing`, `app.health_recovered`, `app.crashed` (`TestHealthMonitor`). Checked on real Docker in development with Mailpit: stopping the container sent "Unhealthy", starting it "Recovered" (2026-11-16). Health is kept in memory: a panel restart starts over from "checking"

### Strategies
- [x] Rolling: start new → health check → patch Caddy upstream → drain (default 10 s) → remove old — after the switch the old container keeps running for the app's drain (Health check settings → Drain, 0–300 s, default 10; `0016_health_check_drain.sql`; an API call without `drain` gets the default), then gets SIGTERM and `StopGrace` (10 s) before it is killed and removed. No drain on a first deploy (nothing old running); a shutdown cuts it short and the old container goes. The timeline's draining step shows the wait (`TestDeployDrains`, `TestHealthCheckAPI`); checked against local Docker (2026-11-14)
- [x] Rolling failure path: remove new, leave route untouched, mark failed — what every failed deploy already does (`TestDeployFailureKeepsOldContainer`)
- [x] Recreate: stop old → start new → health check → roll back to previous image on failure — after the pull or build (and the mount check), phase `stopping` stops every running container of the app (kept, `StopGrace`), then start and health check as usual; no drain. Any failure from there (crash, failed check, cancel, failed switch, shutdown) removes the new container and starts the live deployment's container again, route pointed back. A first deploy or an app stopped on purpose has nothing to start: plain failed. A deployment given up after too many panel restarts starts the old one too (`TestDeployRecreate`, `TestDeployRecreateRollsBack`, `soloExecutor` asserts the two never run at once). Checked against real Docker (2026-11-14, busybox with a named volume, run by a throwaway test on its own network): two good deploys went stopping → starting → health_checking → switching with the volume's content carried over and never two containers at once; a crashing start command ended `rolled_back` with the old container running again and still current. A start command run by `/bin/sh -c` does not pass SIGTERM on, so its old container takes the full `StopGrace` (10 s) to stop: that is the gap
- [x] Recreate forced for apps with volumes; UI states the short gap — `App.Strategy()`: a named volume or a bind mount means recreate (inline files are per deployment, so no). The app overview shows the strategy (hover for why), the Volumes settings warn about the gap as soon as a volume or host path is added, the timeline shows stopping and rolling back (`TestStrategy`). Apps cannot publish host ports yet; when they can, they join this rule
- [x] Volumes CRUD and mounts — named volumes, bind mounts and inline files per app (`0006_app_mounts.sql`, `internal/deploy/mounts.go`); file content written per deployment so editing never changes the live container; tested in `internal/deploy/mounts_test.go` and `internal/store/mounts_test.go`

### Rollback and triggers
- [x] Rollback to a kept image through the same pipeline, without a build — `POST /api/deployments/{id}/rollback` makes a new deployment (trigger `rollback`, `rollback_of`, `0013_deployment_rollback.sql`) with the earlier image, commit and port; start, health check and switch as usual. A Git app returns to the images still kept (5); an image app pulls the image again and its image setting follows, so a redeploy stays on it. Offered for succeeded deployments whose image is not the live one (`TestRollbackGit`, `TestRollbackImage`, `TestRollbackAPI`); checked in the browser with an image app (2026-11-14) and with a Git app on a real VPS with v0.8.2 (no build). An image app on a moving tag (`latest`) gets whatever the tag points to now
- [ ] Manual deploy (button and API)
- [~] Git push webhooks, filtered by branch and optional path — one URL per app takes push events from GitHub (JSON or its default form encoding), GitLab, Gitea and Forgejo: a push to the app's branch deploys; other branches, tags, deleted branches and other events are ignored with the reason; ping answers OK. Pushes during a deploy queue one follow-up deploy, so a burst ships the last commit (`DeployOrQueue`). `TestDeployHook`; checked against real Docker and Caddy (ping, other branch ignored, three pushes → two deploys, 2026-11-13). No path filter yet; the GitHub App's own webhook is not used (a repository webhook is). A push to a GitHub repository with the webhook deployed on a real VPS with v0.6.0 (2026-11-14)
- [x] Deploy webhook `POST /api/hooks/deploy/<token>` with optional image tag or commit — CI calls it with no event; an image app can pass `{"tag": "…"}` (or `?tag=`), which swaps only the tag of its image (`TestWithTag`). The token (24 random bytes) is the only credential: stored hashed for lookup and encrypted for display (`0011_deploy_hooks.sql`); 60 calls a minute per IP; webhook deploys show as `webhook` / `push` triggers and in the activity as actor `webhook`. Commits: a Git app always builds its branch tip
- [x] Auto-deploy toggle per app; regenerate deploy token — the webhook is off until turned on; New URL replaces the token, Turn off removes it

### TLS modes
- [x] `http_only` (behind Cloudflare proxy) — per domain, chosen when adding it or switched later (`PATCH /api/domains/{id}`); served on :80 with no certificate and no redirect; verified against Caddy in dev and in production: panel domain `ops.azayr.com` behind Cloudflare (SSL Flexible) on a real VPS (2026-11-11)
- [ ] `dns01` (wildcards; provider credentials)
- [ ] `custom` — upload certificate and key, expiry tracking
- [ ] Domain redirects (`redirect_to`), www / apex

### UI
- [x] Health check settings — app Settings → Health check: type, path, expected status, start period, interval, timeout, failures in a row; checked in a browser (2026-11-14)
- [x] Deployment timeline with per-state timing — above the log of the selected deployment: one bar per step on a shared scale, with its duration, the running step live, the step it failed or was cancelled in marked; the list shows a running deployment's step. Deployments from before have none ("No steps recorded"). The Deployments tab is an accordion: each row opens its actions, timeline and log in place, the latest open by default; 10 a page with Newer / Older (`?page`, `?per_page`, `X-Total-Count`; `TestListDeploymentsPaged`); checked on a real VPS with v0.8.2 (2026-11-14)
- [x] Rollback picker — select a deployment in the Deployments tab, "Roll back to this" with a confirmation in its log panel; rollbacks are marked ↩ in the list and get the usual deploy toasts
- [x] Webhook and auto-deploy settings — Deploy webhook panel in the app's Settings: URL with Copy, how to add it on GitHub, GitLab and Gitea (or a curl line for image apps), a warning when the panel is reached on a local address
- [ ] Certificates page
- [x] Container terminal (WebSocket + xterm.js) — `internal/api/terminal.go`, `internal/executor/terminal.go`, `web/src/components/container-terminal.tsx`; tested in `internal/api/terminal_test.go` and on a real VPS (2026-11-13)
- [x] Host terminal (root shell on the server, tabs, docked under the page) — `OpenHostTerminal` in `internal/executor/terminal.go`, `handleHostTerminal` in `internal/api/terminal.go`, `web/src/components/host-terminal.tsx`; `TestHostTerminal`. VPS check pending.

---

## Projects: domains, routes and shared variables (v0.11.0)

- [x] Domains belong to projects — `0020_project_domains.sql` drops the per-app domains (not converted: no install relied on them) and adds `domains` (project, hostname, TLS mode) and `domain_routes` (domain, path, app, strip prefix). Project → Domains tab: add a domain (sheet), HTTPS or HTTP only, DNS hint, remove; the app page loses its Domains tab and shows its URLs on the overview, with a link to the project. `TestDomainsAPI`, `TestRoutesOrderAndPaths`
- [x] Path routes — a path of a domain (`/` or a prefix like `/api`, which also matches `/api/...`) goes to an app of the same project; the longest path wins; the prefix is kept by default and can be stripped per route (`/api/users` → `/users`); a domain without `/` answers 404 elsewhere. Add and edit in a sheet. Caddy gets one subroute per hostname; a deploy switches the app's upstreams on every path that serves it, atomically. `TestBuildSites`; real Caddy in `TestSitesWithRealCaddy` (stripped and kept prefixes, longest match, switch)
- [x] Shared project variables — Project → Environment tab (same editor as an app's, encrypted, `0019_project_environment.sql`). An app uses one with `${project.NAME}` in a value; "Every app inherits all of these" (off by default: secrets stay in apps) gives every app all of them, the app's own values winning. Resolved at deploy, before anything stops; an unknown reference fails the deploy with its name. `TestResolveEnvironment`, `TestDeployProjectEnvironment`, `TestProjectEnvironmentAPI`
- [ ] "Needs redeploy" mark on apps after a shared variable changes

## UI (v0.10.1–v0.11.5)
- [x] Forms in side sheets — new project, new app, add domain, add or edit a route, add an external registry, enable the built-in registry and its settings, create a registry token, add an S3 connection (`web/src/components/sheet.tsx`, Base UI dialog from the inline end)
- [x] Project page tabs — Apps, Domains, Environment, Settings (name, description, delete)
- [x] Registries as one list — the built-in registry first, then external ones; an empty state offers both; the built-in one has its own page (Images, Settings)
- [x] Queue button in the navbar — a count and a popover list of the deployments running or queued (any app), each opening its app's Deployments tab (`web/src/components/queue-menu.tsx`)
- [x] Stacked deployment toasts — one card per deployment in progress, centred at the bottom; an ended one keeps its place (success 8 s, failure until dismissed); ding still shows the other toasts and plays its chime (`web/src/components/deploy-toasts.tsx`)
- [x] Database operations in the same queue and toast stack — create/start/stop/restart/remove persist as `db_operations` (`0022`), queued/running/succeeded/failed with snapshots kept after removal. The HTTP request returns 202 immediately; navigation never cancels the worker. Active jobs are discovered across clients, local submissions are tracked immediately so fast jobs still produce a completion toast, successes expire after 8 s and failures stay until dismissed. `GET /api/database-operations/active`, `GET /api/database-operations/{id}`; `TestDatabaseOperationRecovery`, `TestDatabaseAPI`. Real PostgreSQL 18 creation and queue count → success toast verified in the browser (2026-11-15)
- [x] Dashboard app runtime alerts — `/api/apps/problems`, authenticated, polled every 5 s, one Docker container-list call. Crashing/down/missing containers show the app and project with a link to Logs; intentional stops and in-progress deploys are excluded; Docker failures have a distinct unavailable message. Alerts clear after recovery. `TestAppProblems`, `TestAppProblemsAPI`; checked in the browser that the intentionally stopped inertia app does not alert (2026-11-15)
- [ ] Other forms still inline: app settings sections, Git connections, API tokens, build limits, panel domain

## M4 — Databases

Goal: managed Postgres and MariaDB with scheduled S3 backups and restore.

### Instances (`internal/database`)
- [x] Migrations: `db_instances` (`0021`), `db_operations` (`0022`), managed databases/users/grants (`0023`), `db_backups` and backup/restore actions (`0024`), `backup_policies` and backup policy references (`0025`), plus S3 connections (`0017`). Store/database/API tests run through the migrations.
- [~] Driver support: container spec, SQL readiness, initialization, extra databases/users/grants and per-database dump/restore commands implemented. Live backup/restore verification and whole-instance backup remain
- [~] Postgres driver — versions 18/17/16, a separate non-superuser application role; version-specific volume mount. Fake-host lifecycle and role SQL tests pass; real Docker check passes for 18 and 17 (2026-11-15: provisioning, write access, no superuser, database/user management); 16 not run
- [~] MariaDB driver — versions 11.8/11.4/10.11, initial database and scoped application user, administrator login limited to localhost. Fake-host lifecycle tests pass; real Docker check passes for 11.8 (2026-11-15: provisioning, write access, no admin rights, database/user management); 11.4 and 10.11 not run
- [~] Version picker, persistent named volume, CPU and memory limits — implemented; volume reuse checked with a fake host, `TestRealDatabaseInstances` passes on real Docker (2026-11-15)
- [x] Internal-only by default, with an opt-in public port (v0.12.4, 2026-11-16, `0029_database_public_port.sql`): a gate container (`<instance>-public`, the panel's own image running `panel db-gate`) publishes the chosen host port, drops addresses outside an optional IP/CIDR allowlist and terminates TLS itself with a per-instance self-signed certificate (downloadable for verify-full); clients that do not start TLS are refused before sending a password (PostgreSQL SSLRequest/direct TLS; MariaDB SSL handshake with sequence rewrite), so the database container is unchanged and MariaDB 10.11 works too. The gate follows the instance (removed on stop, recreated on start and after a panel update). Settings → Public access with confirmations; Connection tab shows a client example. `TestPostgresGate`, `TestMariaDBGate`, `TestGateAllowlist`, `TestPublicAccess`, and `TestRealPublicPort` (real psql/mariadb clients: TLS, verify-full, plain refused, allowlist, close). Docker Desktop rewrites client addresses, so allowlists only match real IPs on Linux; not yet checked on the VPS. In development (panel on the host) set `<PREFIX>GATE_IMAGE` to an image containing the panel binary.
- [x] Start / stop / restart / remove with confirmation — start/restart wait for SQL readiness asynchronously, durable provisioning resumes after a panel restart; interrupted Stop is enforced on recovery. Remove gracefully stops and removes the container and record, retaining its named data volume. `TestProvisionLifecycleAndRecovery`, `TestProvisionFailureRetryAndBusy`, `TestDatabaseAPI`
- [x] Connection strings; inject into an app as env vars — `${db.<instance-name>.URL}` (also HOST, PORT, DATABASE, USERNAME, PASSWORD), restricted to the app's project, resolved before the deployment starts. The database layer exposes generic URI and individual field references; apps compose any client-specific URL format themselves. Generated administrator/application passwords are stored encrypted; only the application connection can be revealed, through a browser session and a no-store response. Unknown/stopped references fail deployment. `TestDatabasePersistence`, `TestDatabaseReferences`, `TestConnectionFormats`, `TestDatabaseAPI`, `TestDeployDatabaseEnvironment`

### Databases and users
- [~] Create / drop databases — creation implemented for PostgreSQL and MariaDB; drop remains. Names validated and quoted, operation tracked globally, metadata recorded after SQL succeeds
- [~] Create / drop users, grants, password rotation — creation and per-database No access / Read only / Read & write / Full access implemented. Generated user passwords and operation payloads encrypted; session-only credential reveal. PostgreSQL existing/future public-schema table and sequence grants, owner downgrade guard, protected initial access; MariaDB grants scoped to the database with wildcard escaping. SQL errors do not falsely fail the running instance. Drop and rotation implemented (2026-11-16): deleting a database drops it and its backup schedule (backups stay restorable); deleting a user reassigns its PostgreSQL objects to the administrator, then drops it; rotation stores the new password (the initial user's updates `${db.<name>.URL}`). Initial database/user are protected; real-engine checks remain. `TestManageDatabasesUsersAndGrants`, `TestPostgresPermissionSQL`, `TestMariaDBPermissionSQL`, `TestFailedPermissionDoesNotReportSuccess`, `TestDatabaseAPI`; real permission tests included in `TestRealDatabaseInstances`, skipped here (Docker socket denied)
- [~] Mirror tables kept in sync with panel operations — original resources seeded for existing instances, new rows/grants saved after successful SQL. External SQL users/databases/grants are not discovered or reconciled yet

### Storage connection (`internal/storage`)
- [x] S3 client — `internal/storage` (minio-go): path-style or virtual-host, HTTPS or not, S3 errors in plain words
- [x] External S3 connections CRUD (AWS, R2, others) with a connection test — Settings → Storage (`GET/POST /api/s3-connections`, `POST …/{id}/test`, `DELETE`); the check writes, reads and deletes a small object under the folder; the secret key is encrypted and never returned; a connection the registry stores in cannot be removed (`0017_s3_connections.sql`, `TestS3ConnectionsAPI`; real check against RustFS in `TestRealRegistryS3`)

### Backups
- [~] Manual backup: per-database gzip SQL dumps to an existing S3 connection, operation queue/history, compressed size and sanitized failures implemented; simulated PostgreSQL/MariaDB round-trip and failure tests pass. Real Docker/S3 round trip verified (2026-11-16). Finished backups can be deleted (S3 object first, then the record; never while the instance is busy; `TestDeleteBackup`). Whole-instance backups (2026-11-16, `0028_instance_backups.sql`): one gzip dump per managed database plus `manifest.json` (databases, users, grants, PostgreSQL object owners; no passwords), each object key recorded before upload so deletion and retention remove exactly those objects. PostgreSQL dumps keep object owners so restored apps can still alter their tables. Restore creates a new instance (engine, version and initial names from the manifest) in the same project: users get new passwords, owners outside the panel become NOLOGIN roles, grants are reapplied; the source is untouched and an interrupted restore fails instead of replaying. Instance policies sit beside per-database ones. Backups of deleted instances (2026-11-16): kept with their S3 objects and listed under `GET /api/database-backups/orphaned` with the instance's name, project, engine and version from the backup operation; restore (`POST …/{backup}/restore`) into a new instance or into a new database on another running instance of the same engine, delete (`DELETE …/{backup}`). A backup being read by a restore cannot be deleted, including a restore into another instance (which holds a different lock). Backup and restore jobs may run up to 6 hours (other jobs 10 minutes). `TestInstanceBackupAndRestore`, `TestInterruptedInstanceRestoreIsNotReplayed`, `TestInstanceBackupPolicy`; real run `TestRealInstanceBackupRestore` passes (PostgreSQL 18, MariaDB 11.8, RustFS).
- [~] Backup policies: durable per-database daily/weekly UTC schedules with pause/resume, atomic enqueue/next-slot persistence, one missed-slot catch-up, busy/stopped deferral, policy editor and count-based retention (1–365). Schedule, retention, failure/retry and API tests pass; scheduled runs after a restart and pause/resume verified on real Docker/S3 (2026-11-16). Custom 5-field UTC cron schedules, at most one run per hour, no descriptors or time zones (`TestCronBackupPolicies`).
- [~] Streamed, gzip-compressed upload to S3 implemented through Docker exec stdout (stderr discarded); real S3 round-trip pending.
- [~] Retention pruning: only successful scheduled backups belonging to the same policy, after a successful replacement; manual backups preserved. S3 object removed before metadata; cleanup failures shown separately and retried after the next successful scheduled backup. Tests pass; real S3 deletion verification pending.
- [~] Restore from a saved backup into a new database on the same instance, gzip validation before SQL, 20 GiB temporary-file limit, initial-user grants, interrupted-restore guard. Simulated round-trip/corrupt-file tests pass; real Docker/S3 verification pending.
- [x] Backup status, compressed size and sanitized failure reason, listed in the instance Backups tab; backend tests and frontend build/typecheck pass.

### UI
- [x] Database instances list and create flow — Databases navigation, project/engine/version/initial database/application user and resource limits in a side sheet; English, French, Arabic and Spanish. Typecheck and production build pass; PostgreSQL 18 creation, Running state and completion toast verified in the browser (2026-11-15)
- [x] Project Databases tab — scoped instance count/list with engine/version, observed status and panel-managed database names, management links and creation bound to the current project. Authenticated `GET /api/projects/{id}/databases`; project isolation, empty list and additional database names covered by `TestDatabaseAPI`. Populated/empty projects and project-bound creation form verified in the browser (2026-11-15)
- [x] Change an instance's project in Settings — authenticated PATCH, atomic metadata move, duplicate-name and active-operation guards; preserves data/users/credentials/container/volume and refreshes project lists. Save toasts and reference-impact explanation in four languages. `TestDatabaseAPI`, `TestDatabaseReferences`, race checks, typecheck/build pass; project selector verified in the browser (2026-11-15)
- [~] Instance page — overview, Databases & users (create forms, per-user grant editor and credential reveal), connection references, recent logs (200 lines, polling), settings/removal and lifecycle controls. Databases & users tab, creation form and protected initial permission editor checked in browser (2026-11-15). Backups tab now includes manual backup/restore, history and daily/weekly policy sheets; the header keeps status beside the instance name and groups lifecycle controls separately from Adminer. Additional-user/grant real SQL checks were previously recorded above; live backup/restore and Adminer verification remain pending
- [~] Adminer: shared private container, panel-session authentication on every request, managed-user automatic login, no passwords in URLs. PHP bootstrap session conflict fixed; `TestAdminerSessionBootstrap` checks both drivers, warning-free startup, persistent CSRF/session state and secure cookie scope. Gateway/proxy tests pass. Live check (2026-11-16): login and browsing work; edits/deletes were rejected as cross-origin because of `Referrer-Policy: no-referrer` (browsers send `Origin: null`), fixed with `same-origin` — recheck in the browser.
- [x] S3 connections settings — Settings → Storage
- [~] Manual backup and restore sheets with storage/database pickers, empty states and queue notifications in four languages implemented; daily/weekly UTC policy editor and retention count implemented in four languages; cron option, backup deletion, and database/user delete and password rotation buttons added in four languages; live check of the new controls remains.

---

## M5 — Storage and registry

Goal: optional built-in S3 and image registry.

### Internal S3
- [x] RustFS stability check; ship behind an "experimental" label if needed — shipped as Experimental: RustFS is at `1.0.0-rc.6` (a release candidate); the Storage page and its setup sheet say so (v0.17.0)
- [x] RustFS container lifecycle (start, stop, upgrade, data volume) — `internal/objectstore`: off by default; Storage → Turn on runs `rustfs/rustfs:1.0.0-rc.6` (`DOKYR_S3_IMAGE`) as `<slug>-s3` with its data in the `<slug>-s3-data` volume, console off, root credentials made once and sealed at rest; supervised every 30 s, recreated when its spec (image included) changes, removed when turned off with the data and credentials kept; published on 127.0.0.1:9006 for a panel on the host (`DOKYR_S3_PORT`). `TestLifecycle`, `TestForeignContainer`; checked on real Docker (off and on again keeps the objects)
- [~] Buckets and access keys management — buckets: list with object count and size (counting stops at 10 000 objects), create (S3 name rules), delete (empty only, refused while a storage connection uses it), checked against real RustFS. Access keys: only the root credentials, revealed in a sheet (session only, audited); per-bucket keys remain
- [~] Routed through Caddy with TLS — an optional domain of its own adds a proxy route to `<slug>-s3:9000`, kept apart from the panel, registry and app domains, with the HTTP-only option; not yet tried on a real domain
- [~] Usable as a backup target and as the registry backend — turning it on makes the `panel` bucket and a "Built-in storage" S3 connection (path-style), so it can be picked wherever a storage connection can; the panel backup into it is checked. Database backups and the registry use the same connection code but are not checked against it yet (the registry needs the panel in its container, where the endpoint is `<slug>-s3:9000`)

### Internal registry (`internal/registry`)
- [x] Registry container lifecycle, backed by an S3 connection — `internal/registry`: off by default; Registry page → Built-in registry turns it on with a domain of its own and a storage (a Docker volume, or an S3 connection with its folder). CNCF Distribution `registry:3.0.0` (`DOKYR_REGISTRY_IMAGE`) in `<slug>-registry`, configuration (0600, may hold the S3 key) and token certificate under `<data>/registry`; recreated when the settings change, removed when turned off (images stay), supervised every 30 s. `TestRealRegistry`, `TestRealRegistryS3` (pushed blobs land in the bucket under the folder)
- [x] Auth for push and pull — the panel is the token server (`/api/registry/token`, on the registry's domain): registry tokens (`0018_registry.sql`, Registry page → Tokens, shown once, stored hashed) with push+pull or pull-only scope; docker login user = token name. JWTs signed ES256 with a key in the data dir, certificate in x5c. Both docker's Basic auth GET and containerd's OAuth2 password POST are accepted; failed logins rate limited. The panel's own account pulls for deploys and pushes builds. Real `docker login` / `push` / pull in `TestDockerCLI` (a pull token cannot push); `TestTokenGrants`, `TestRegistryAPI`
- [x] Routed through Caddy with TLS — a route for the registry domain, `/api/registry/token` to the panel (`PanelPaths`, `TestBuildPanelPaths`); the domain is kept apart from app domains and the panel's. An HTTP-only option (like the panel's and project domains') serves it behind Cloudflare "Flexible" without a redirect loop; the form warns about the CDN's upload cap (100 MB per request on Cloudflare Free). Tried on a real domain behind Cloudflare (2026-11-15): docker login, push, and deploy on push work
- [x] Push notification endpoint → auto-deploy on a new tag — the registry reports pushes to `/api/registry/events` (shared secret); image apps whose image is that tag on the built-in registry deploy (trigger `registry`) unless deploy on push is off (app Source settings, `PUT /api/apps/{id}/push-deploy`). Real push report in `TestRealRegistry`; `TestRegistryAPI`
- [x] Repositories and tags browser, delete, garbage collection — Registry page → Images: a table of image, tag, platform and size (digest on hover), an image with several tags in one collapsible row whose size counts shared layers once; clicking an image copies the full name, Delete per tag, Free space runs `garbage-collect --delete-untagged` in a one-off container while the registry is stopped, then starts it again (`TestRealRegistry`)
- [x] App source: image from the internal registry — the app form's Registry access offers Built-in registry with a picker of pushed images; deploys pull with the panel's own account

### UI
- [~] Storage page (buckets, keys, usage) — `/storage` (the nav item is live): state, endpoint, public URL, access key, its storage connection; buckets table with usage, New bucket sheet, Delete in the Actions menu with a confirmation; Credentials sheet; Turn off asks first; four languages. Per-bucket keys remain
- [x] Registry page (images, push and pull instructions) — built-in registry on top (tabs Images and Settings; settings, turn off and tokens are in Settings; the header shows the address docker uses), external registries below

---

## M6 — Insights and alerts

Goal: see what the server is doing and get told when something breaks.

### Metrics (`internal/metrics`)
- [x] Migration: `metrics_samples` (`0030_metrics.sql`), `notification_channels` and `alert_rules` (`0031_notifications.sql`)
- [x] Docker stats sampler, 10 s samples (CPU, memory, network, disk) — `internal/metrics`, one sample per app or database instance (its containers added together, the public-port gate left out), rates from the previous reading, `TestSampler`; checked against a real PostgreSQL container (2026-11-16); engine-wide counts and storage usage already shown by `internal/dockermetrics` (`/api/docker/metrics`, 15 s refresh, disk scan at most once a minute)
- [x] Rollups to 1 min and 1 h; prune after 30 days — average and peak per bucket; raw samples kept 24 h, minutes 7 days, hours 30 days; idempotent, run every minute (`TestMetricRollupsAndRetention`)
- [x] Host metrics: CPU, memory, disk, load — plus swap, uptime and network; `internal/hostmetrics`, 5 s samples, ten minutes kept in memory, `/api/host/metrics`; reads `/proc` through a read-only `/host/proc` mount on Linux (installer and dev Compose); on a real VPS (2026-11-13)
- [x] Metrics query API — `GET /api/apps/{id}/metrics`, `GET /api/databases/{id}/metrics`, `range=1h|6h|24h|7d|30d`, at most about 360 points

### Notifications (`internal/notify`)
- [x] Channel interface with a test-send — `internal/notify`: settings sealed with the panel key, secrets never sent back (an empty secret keeps the saved one), last delivery and last error kept per channel; Settings → Notifications → Send test (2026-11-16)
- [x] Email (SMTP) — STARTTLS (default, required when chosen), implicit TLS or none; optional login; several recipients; checked against Mailpit
- [x] Slack — incoming webhook
- [x] Discord — webhook embed coloured by level
- [x] Telegram — Bot API sendMessage; errors never echo the bot token
- [x] Generic webhook — JSON (title, text, level, url, at), optional `X-Signature-256: sha256=<HMAC>`; checked against a local receiver
- [x] Alert rules: event, target, channel — every recorded event passes through the store's event hook; rules for one app or database, or all; one message per channel; pause/resume (`TestDispatch`)
- [x] Usage alerts: CPU above N cores or memory above N% of the limit for 1–60 minutes, per app or instance or for all (`0033_usage_alerts.sql`); fire once when every 10-second sample of the period is at or above the threshold (80% of the samples must exist), send "back to normal" below it, each rule to its own channel (`TestUsageRules`); checked end to end with Mailpit (2026-11-16)
- [x] Events: deploy started / succeeded / failed (rolled back counts as failed), app unhealthy or down / recovered / crashed, backup failed, disk high (fires at 90%, re-arms under 85%); rules for one app or instance are removed with it (`0032_alert_rule_targets.sql`)

### More engines
- [ ] MongoDB driver
- [ ] ClickHouse driver

### UI
- [~] Global dashboard — host and Docker cards on the overview page (`web/src/components/host-metrics.tsx`, `docker-metrics.tsx`); the host CPU, RAM and network sparklines use Recharts (lazy-loaded, hover readout); host history is ten minutes in memory, per-app and per-database history is on their Metrics tabs
- [x] Per-app and per-database charts — Metrics tab on app and database pages: CPU, memory (with limit), network and disk, range picker, hover readout; Recharts (lazy-loaded)
- [x] Notification channels settings — Settings → Notifications, four languages
- [x] Alert rules editor — same tab: event, app/database or all, channel

---

## UI (v0.13–v0.16.1)
- [x] Sidebar icons — outlined, solid and accent-coloured on the open page (Phosphor 2.1.7, per-icon imports; about 9 kB gzipped added to the main bundle) (2026-11-16)
- [x] Projects list — search (name and description, accents ignored), filters (needs attention, all good, no apps), sorting (name, last deploy, newest, most apps), paging of 10; rows show running, down and stopped counts, databases, domains and the last deploy (`GET /api/projects` returns one summary per project)
- [x] Databases list — search (instance, project, database, engine), filters (engine, project, status, needs attention), sorting (name, project, newest, last backup), paging of 10; rows show status, engine and version, public port, databases, users, limits and the last backup (`GET /api/databases` returns one summary per instance)
- [x] Activity — server-side search, filters (type, problems only, people or panel and webhooks, period), paging of 25 with totals (`GET /api/events/search`); readable labels in four languages for every recorded action; problem and success markers (2026-11-16)
- [x] Shared list controls: search box, selects, pager and debounced search (`web/src/components/list-controls.tsx`)
- [x] Command palette (Ctrl/Cmd+K): go to pages and to projects, apps, databases, domains, registries, Git sources and storage; create an app, project or database, connect a registry, switch theme or language, sign out (`GET /api/search`, up to 8 matches per kind with a `truncated` flag; IDs match by prefix only) (v0.16.1, 2026-11-16)

## Cross-cutting

- [ ] Unit tests for store, secret, deploy state machine, Caddy config builder
- [ ] Integration tests against a real Docker daemon
- [ ] End-to-end test: install script on a fresh VM
- [ ] Security review: socket exposure, secrets at rest, webhook token handling, headers
- [x] Panel self-backup (SQLite file + key) and restore procedure (2026-11-16): Settings → Backup sends an encrypted copy (`age` with a passphrase, sealed at rest with the panel key; `gzip`+`tar` of a `VACUUM INTO` snapshot, the key and a manifest) to an S3 connection on a cron schedule or on demand, keeps the newest N, lists and downloads them; failures feed the `backup.failed` alert. `panel restore FILE` checks the file (passphrase, key fingerprint, size, SQLite integrity) and stages it; the next start moves the old database, WAL and key to `backups/pre-restore-<time>` and swaps the backup in (`internal/selfbackup`; real round-trip against RustFS checked)
- [ ] Docs: install, first deploy, backups, updates, recovery CLI
- [x] Release pipeline: versioned image (amd64 + arm64), generated `install.sh`, changelog — push a `vX.Y.Z` tag; `.github/workflows/release.yml` publishes `ghcr.io/brahimoulhaj/paas:X.Y.Z` and `:latest` and a GitHub release with generated notes (v0.1.0 → v0.6.0 published)

## Out of v1

Multiple servers · teams and roles beyond a single owner · Docker Compose stacks · preview environments · database replication.

## Open questions

- [ ] Final product name and install domain
- [ ] License: MIT, Apache 2.0, or AGPL
- [x] RustFS: stable enough, or "experimental" label — "Experimental" while RustFS is a release candidate
- [x] Database instances: internal-only by default with an opt-in public port — decided: optional allowlist, TLS required, user-picked port (2026-11-16)
- [ ] Docker Compose stacks in v1?
- [x] First Git provider: GitHub App or GitLab token — both: a GitHub App and a GitLab token (2026-11-11)

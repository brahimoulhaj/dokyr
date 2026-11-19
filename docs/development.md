# Development

Back to the [docs index](README.md).

A lightweight, single-node PaaS panel: one Go binary with an embedded React app, SQLite for
state, Docker for workloads and Caddy in front. The name lives in `internal/brand/brand.go`;
nothing else hard-codes it.

## Requirements

Go 1.26+, Node 22+, pnpm 10+, and a running Docker daemon (the panel starts without one, but
cannot deploy; the executor tests skip themselves).

## First run (native)

```sh
make deps      # go mod tidy (creates go.sum) + pnpm install
make dev-api   # Go server on :10000, data in ./.data — prints a one-time setup link;
               # also starts Caddy in Docker on :8880 (HTTP) and :8443 (HTTPS).
               # Rebuilds and restarts on Go changes when `air` is installed (.air.toml)
make dev-web   # in a second terminal: Vite on :5173, proxies /api to :10000
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

## CLI in development

See [CLI → In development](cli.md#in-development).

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

To build an image and installer locally, see [Install on a server](install.md#install-on-a-server-custom-image).

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
internal/hostcron/    cron jobs: schedule and runs of commands on the host
internal/database/    project-scoped Postgres/MariaDB provisioning, lifecycle, connection references
internal/api/         HTTP handlers, middleware, embedded SPA
internal/webui/       go:embed target for the built web app
web/                  React + TypeScript + Vite + Tailwind
```

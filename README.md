# Dokyr

A lightweight, single-node PaaS panel: one Go binary with an embedded React app, SQLite for
state, Docker for workloads and Caddy in front. Deploy apps from images or Git, put domains
and HTTPS in front of them, run databases, and manage it all from one web panel.

Progress is tracked in [Roadmap.md](Roadmap.md). Detailed guides live in [docs/](docs/README.md).

## Use Dokyr

### Install

On a fresh Ubuntu or Debian server (amd64 or arm64) with ports 80 and 443 open:

```sh
curl -fsSL https://github.com/brahimoulhaj/paas/releases/latest/download/install.sh | sudo sh
```

It installs Docker if needed, starts the panel and prints a one-time setup link
(`http://<server-ip>/setup?token=…`). Options, specific versions and custom images:
[Install and update](docs/install.md).

### First steps

1. Open the setup link and create the owner account.
2. Point a domain at the server and save it in Settings → General, so the panel and
   passkeys run over HTTPS ([Sign-in methods](docs/sign-in.md)).
3. Invite your team from Settings → Users ([Users and roles](docs/users-and-roles.md)).
4. Create a project, then deploy apps ([First deploy](docs/first-deploy.md)) and add
   [databases](docs/databases.md) to it.
5. Set up the panel backup in Settings → Backup ([Backups and restore](docs/backups.md)).

Lost the setup link or the owner password? Use the `dokyr` host command
([CLI](docs/cli.md)).

### Update

Later versions install from the panel (Settings → Updates) or over SSH with `dokyr update`
(`--check` only reports, `--yes` skips the prompt).

### Guides

| Guide | What it covers |
|---|---|
| [Install and update](docs/install.md) | installer options, custom images, updates |
| [First deploy](docs/first-deploy.md) | project, app, variables, domain and HTTPS |
| [Configuration](docs/configuration.md) | `DOKYR_*` environment variables, builds, proxy |
| [CLI](docs/cli.md) | `dokyr` commands |
| [Users and roles](docs/users-and-roles.md) | invites, permissions, host terminal |
| [Sign-in methods](docs/sign-in.md) | passkeys, GitHub, active sessions |
| [Cron jobs](docs/cron-jobs.md) | scheduled commands on the host |
| [Databases](docs/databases.md) | instances, Adminer, backups, references, API |
| [Monitoring](docs/monitoring.md) | host and Docker metrics, app problems |
| [Backups and restore](docs/backups.md) | panel backup, restore, what is not covered |
| [Recovery](docs/recovery.md) | lost link or password, unreachable panel, lost server |
| [Security](docs/security.md) | security model and hardening |

## Develop Dokyr

Requirements: Go 1.26+, Node 22+, pnpm 10+, and a running Docker daemon.

```sh
make deps      # go mod tidy + pnpm install
make dev       # everything in Docker with live reload; open http://localhost:5173
```

Or run natively in two terminals:

```sh
make dev-api   # Go server on :10000, data in ./.data, prints a setup link; Caddy on :8880/:8443
make dev-web   # Vite on :5173, proxies /api to :10000
```

Don't run `make dev` and `make dev-api` at the same time: two panels on one Docker host
fight over the proxy container.

Build and test:

```sh
make build              # bin/dokyr, web app embedded
make test               # go vet, go test, web typecheck
make image VERSION=0.1.0
```

Releases: push a tag `vX.Y.Z`; the Release workflow publishes the image and `install.sh`.

The [development guide](docs/development.md) covers the Docker dev loop, data volume, CLI in
development, releases and the code layout.

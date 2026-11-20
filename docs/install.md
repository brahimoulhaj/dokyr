# Install and update

Back to the [docs index](README.md).

## Requirements

- Linux, amd64 or arm64. Ubuntu and Debian are what the installer is tried on.
- Root access, and ports 80 and 443 free and open to the internet (the proxy needs them
  for HTTP and for certificates).
- Docker with the Compose plugin; the installer installs Docker when it is missing.
- One panel per server.

## Quick install

On a fresh Ubuntu or Debian server (amd64 or arm64) with ports 80 and 443 open:

```sh
curl -fsSL https://github.com/brahimoulhaj/paas/releases/latest/download/install.sh | sudo sh
```

It installs Docker if needed, starts the panel and prints a one-time setup link
(`http://<server-ip>/setup?token=…`). Options go between `sudo` and `sh`, e.g.
`… | sudo DOKYR_PUBLIC_URL=http://203.0.113.10 sh`. A specific version:
`https://github.com/brahimoulhaj/paas/releases/download/v0.1.1/install.sh`.

The link is valid for 15 minutes and works once; `dokyr setup-token` prints a new one.
Next: [First deploy](first-deploy.md).

What the installer leaves on the server:

| Path | What |
|---|---|
| `/var/lib/dokyr/` | all panel data: `dokyr.db`, `secret.key`, certificates, deployment logs (mode 700) |
| `/var/lib/dokyr/compose.yaml` | the panel's Compose project; rewritten by the installer and by updates |
| `/var/lib/dokyr/panel.env` | `DOKYR_*` settings ([Configuration](configuration.md)); kept across runs |
| `/usr/local/bin/dokyr` | host command that runs the CLI inside the panel container |
| `127.0.0.1:8080` | the panel without the proxy, from the server itself ([Recovery](recovery.md)) |

## Updating

Two ways, same mechanism:

- **In the panel**: Settings → Updates shows the running version and whether a newer one
  exists (**Check now**), and installs it with **Update now**. **Update automatically**
  checks every hour and installs what it finds; an update waits while a deployment runs.
- **Over SSH**: `dokyr update` (`--check` only reports, `--yes` skips the prompt).

What happens: the new image is pulled, then a short-lived helper container
(`dokyr-updater`) replaces the panel container with one from the new image and the same
configuration (mounts, environment, networks, ports). The panel is away for a few
seconds; the proxy, apps and databases keep running and serving traffic. If the new panel
does not answer its health check within 90 seconds, it is removed and the previous one
comes back; Settings → Updates and the activity log say why.

The installer's `/var/lib/dokyr/compose.yaml` is rewritten to the new image, so running
the installer again later keeps the updated version.

Database migrations run when the new version starts and are not undone by going back to
an older image. Before an update that matters, take a panel backup
([Backups and restore](backups.md)).

When an update seems stuck:

```sh
docker logs -f dokyr-updater   # the helper's own log
docker logs dokyr              # the panel
dokyr version
```

More in [Recovery](recovery.md).

## Install on a server (custom image)

To install a build of your own instead of a published release:

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

Other environment variables are listed in [Configuration](configuration.md).

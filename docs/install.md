# Install and update

Back to the [docs index](README.md).

## Quick install

On a fresh Ubuntu or Debian server (amd64 or arm64) with ports 80 and 443 open:

```sh
curl -fsSL https://github.com/brahimoulhaj/paas/releases/latest/download/install.sh | sudo sh
```

It installs Docker if needed, starts the panel and prints a one-time setup link
(`http://<server-ip>/setup?token=…`). Options go between `sudo` and `sh`, e.g.
`… | sudo DOKYR_PUBLIC_URL=http://203.0.113.10 sh`. A specific version:
`https://github.com/brahimoulhaj/paas/releases/download/v0.1.1/install.sh`.

## Updating

Later versions install from the panel (Settings → Updates) or over SSH with `dokyr update`
(`--check` only reports, `--yes` skips the prompt).

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

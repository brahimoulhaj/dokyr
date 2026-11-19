# Configuration

Back to the [docs index](README.md).

## Environment variables

Configuration is by environment: `DOKYR_DATA_DIR` (default `/var/lib/dokyr`), `DOKYR_ADDR`
(default `:8080`), `DOKYR_PUBLIC_URL` (base URL for printed links), `DOKYR_MAX_BUILDS` (Git builds
run at once, default 1; further Git deploys wait, queued, and can be cancelled; queued and interrupted deploys
start again after a panel restart, at most 3 times),
`DOKYR_BUILD_CPUS` and `DOKYR_BUILD_MEMORY_MB` (default CPU and memory all builds share, e.g.
`1.5` and `2048`; unset means no cap; Settings → Builds overrides them without a restart). Capped builds run in a BuildKit container of their own
(`moby/buildkit`, created on the first build, about 100 MB to download) because Docker's
built-in BuildKit ignores per-build limits; a build step over the memory cap is killed.

Host metrics also read `DOKYR_HOST_PROC`; see [Monitoring](monitoring.md).

## Proxy

Proxy: `DOKYR_PROXY_HTTP_PORT` / `DOKYR_PROXY_HTTPS_PORT` (default 80 / 443; `make dev-api` uses
8880 / 8443), `DOKYR_PROXY_IMAGE`, `DOKYR_PROXY_PANEL_UPSTREAM` (how Caddy reaches the panel),
`DOKYR_PROXY_ADMIN` (`socket` on Linux, `tcp` elsewhere) and `DOKYR_PROXY_ADMIN_PORT` (tcp mode,
default 2019 on 127.0.0.1). Socket mode needs the panel to run as root and the data dir mounted
at the same path inside the panel container as on the host.

One panel per Docker host: the network, the proxy container and the container labels are all
named after the product slug, so two panels on the same daemon would fight over them.

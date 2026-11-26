# Dokyr docs

Detailed guides behind the [main README](../README.md). Progress is tracked in
[Roadmap.md](../Roadmap.md).

## Using Dokyr

- [Install and update](install.md): requirements, one-line install, what it leaves on the server, updates, custom images.
- [First deploy](first-deploy.md): project, app from an image or Git, variables, domain and HTTPS.
- [Configuration](configuration.md): `DOKYR_*` environment variables, builds, proxy settings.
- [CLI](cli.md): `dokyr` commands, setup links, owner password recovery.
- [Remote CLI](remote-cli.md): sign in from your own machine through the browser, list projects.
- [HTTP API](api.md): API tokens, the API version and what counts as a breaking change, errors.
- [Users and roles](users-and-roles.md): invites, the roles table, host terminal confirmation.
- [Sign-in methods](sign-in.md): passkeys, GitHub sign-in, active sessions.
- [Cron jobs](cron-jobs.md): scheduled commands on the host.
- [Deploy on push](deploy-on-push.md): the deploy webhook, the GitHub App's webhook, watch paths.
- [Databases](databases.md): PostgreSQL/MariaDB instances, Adminer, backups, retention, `${db.*}` references, API.
- [Monitoring](monitoring.md): host and Docker metrics, app problems on the dashboard.
- [Backups and restore](backups.md): what to back up, the panel backup, volume backups, restoring on the same or a new server.
- [Recovery](recovery.md): lost setup link or password, unreachable panel, failed start, lost server.
- [Security](security.md): how the panel protects the host, secrets and sessions; what stays your job.

## Working on Dokyr

- [Development](development.md): requirements, native and Docker dev loops, build, releases, code layout.

# Dokyr docs

Detailed guides behind the [main README](../README.md). Progress is tracked in
[Roadmap.md](../Roadmap.md).

## Using Dokyr

- [Install and update](install.md): one-line install, installer overrides, custom images, updates.
- [Configuration](configuration.md): `DOKYR_*` environment variables, builds, proxy settings.
- [CLI](cli.md): `dokyr` commands, setup links, owner password recovery.
- [Users and roles](users-and-roles.md): invites, the roles table, host terminal confirmation.
- [Sign-in methods](sign-in.md): passkeys, GitHub sign-in, active sessions.
- [Cron jobs](cron-jobs.md): scheduled commands on the host.
- [Deploy on push](deploy-on-push.md): the deploy webhook, the GitHub App's webhook, watch paths.
- [Databases](databases.md): PostgreSQL/MariaDB instances, Adminer, backups, retention, `${db.*}` references, API.
- [Monitoring](monitoring.md): host and Docker metrics, app problems on the dashboard.

## Working on Dokyr

- [Development](development.md): requirements, native and Docker dev loops, build, releases, code layout.

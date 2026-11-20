# CLI

Back to the [docs index](README.md).

```
dokyr serve
dokyr setup-token                                  new one-time setup link (15 min)
dokyr owner create --email E [--password P]
dokyr owner reset-password [--email E] [--password P]   new password, all sessions signed out
dokyr update [--check] [--yes]                     update the panel to the newest release
dokyr restore [--passphrase-file F] FILE           stage a panel backup; applied on the next start
dokyr version
```

These commands run on the server and work on the data directory. To manage a panel from your
own machine through its API, see the [Remote CLI](remote-cli.md).

On a server, the installer adds a `dokyr` host command that runs the CLI inside the panel container (see [Install](install.md)).
Because it runs inside the container, a file given to `dokyr restore` must be inside it too
(`docker cp FILE dokyr:/tmp/`).

`owner create` and `owner reset-password` ask for the password on standard input when
`--password` is left out, which keeps it out of the shell history. `--email` on
`reset-password` is only needed to name the account explicitly.

What each command is for: [Install and update](install.md), [Backups and restore](backups.md),
[Recovery](recovery.md).

## In development

For local development, CLI commands must use the same data directory as the API:
`DOKYR_DATA_DIR=./.data ./tmp/panel owner reset-password` (no `sudo`).
With `make dev`, use `docker compose exec api go run ./cmd/panel owner reset-password`.

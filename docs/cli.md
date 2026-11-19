# CLI

Back to the [docs index](README.md).

```
dokyr serve
dokyr setup-token                                  new one-time setup link (15 min)
dokyr owner create --email E [--password P]
dokyr owner reset-password [--email E] [--password P]
dokyr version
```

On a server, the installer adds a `dokyr` host command that runs the CLI inside the panel container (see [Install](install.md)).

## In development

For local development, CLI commands must use the same data directory as the API:
`DOKYR_DATA_DIR=./.data ./tmp/panel owner reset-password` (no `sudo`).
With `make dev`, use `docker compose exec api go run ./cmd/panel owner reset-password`.

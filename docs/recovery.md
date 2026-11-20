# Recovery

Back to the [docs index](README.md).

What to do over SSH when the panel cannot be used from the browser. Everything here
needs root on the server; the `dokyr` host command runs the CLI inside the panel
container ([CLI](cli.md)).

## The setup link is lost or expired

```sh
dokyr setup-token
```

Prints a new one-time link, valid 15 minutes. It only works while no owner exists.

## The owner password is lost

```sh
dokyr owner reset-password
```

Asks for a new password, sets it for the owner and signs out every session of that
account. `--email E` names another account instead. Passkeys and a linked GitHub account
are left as they are.

If the account itself is gone (a database restored from before setup, for example),
`dokyr owner create --email E` makes an owner.

## The panel's domain does not answer

The panel also listens on `127.0.0.1:8080` on the server, without the proxy. From your
own machine:

```sh
ssh -L 8080:127.0.0.1:8080 root@<server>
```

then open `http://localhost:8080` and sign in with the password (passkeys are tied to
the panel's hostname and do not work here). From there, fix the panel domain in
Settings → General, or look at what the proxy reports.

Things to check on the server:

```sh
docker ps --filter name=dokyr          # dokyr (panel) and dokyr-proxy (Caddy) should be up
docker logs --tail 100 dokyr
docker logs --tail 100 dokyr-proxy     # certificate and DNS errors show here
```

- **DNS**: the panel's hostname must resolve to this server, and ports 80 and 443 must be
  reachable, or no certificate can be issued.
- **Proxy container missing or stopped**: restart the panel (`docker restart dokyr`); it
  creates the proxy again and loads every route into it.

## The panel does not start

```sh
docker logs --tail 200 dokyr
cd /var/lib/dokyr && docker compose up -d
```

- **After an update**: a failed update puts the previous version back by itself; see
  `docker logs dokyr-updater`. To pin a version by hand, set the `image:` line in
  `/var/lib/dokyr/compose.yaml` and run `docker compose up -d` there. An older image
  cannot open a database a newer one has migrated: restore a panel backup made before the
  update instead ([Backups and restore](backups.md)).
- **Disk full**: free space first (`docker system df`, `docker image prune`); SQLite needs
  room to write.
- **`secret.key` missing or replaced**: the database's secrets cannot be decrypted without
  the key they were sealed with. Put the right key back, or restore a panel backup, which
  carries both.

Apps, databases and the proxy keep running while the panel is down: they are ordinary
Docker containers with a restart policy. Only deploys, the interface and scheduled jobs
(backups, cron jobs, health alerts) stop.

## The server is lost

Install the panel on a new server and restore the latest panel backup, then the
databases: [Backups and restore](backups.md#restore-the-panel).

## Running the installer again

Safe at any time: it keeps `/var/lib/dokyr`, keeps the image the panel runs now (it may
have updated itself since) and recreates the panel container. Use it when the container
or the `dokyr` host command was removed by accident.

## Removing a user's access in a hurry

Settings → Users → remove the user: their sessions and API tokens end at once. For your
own account, Settings → Security → **Log out other sessions**, then change the password.
A deploy webhook URL that leaked is replaced with **New URL** in the app's Deploy on push
panel; a leaked API token is revoked in Settings → API tokens.

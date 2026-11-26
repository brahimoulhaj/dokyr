# Backups and restore

Back to the [docs index](README.md).

Three things hold state on a Dokyr server, and each is backed up differently:

| What | Where it lives | How it is backed up |
|---|---|---|
| The panel: projects, apps, settings, users, encrypted secrets | `/var/lib/dokyr/dokyr.db` and `/var/lib/dokyr/secret.key` | Panel backup, below |
| Database contents | each instance's Docker volume | Database backups ([Databases](databases.md#backups-and-restore)) |
| App volumes and host paths | Docker volumes, host folders | Not backed up by the panel: use your own tool (restic, snapshots of the disk) |

The database file is useless without `secret.key`: every stored secret (tokens,
passwords, environment values) is encrypted with it. A panel backup always carries both.

## Panel backup

Settings → Backup (owner and admins). It needs an S3 storage connection first
(Settings → Storage): AWS S3, Cloudflare R2, Backblaze B2 or any S3-compatible service.
Prefer a bucket outside the server; the built-in storage runs on the same disk as the
panel and does not survive losing it.

- **Passphrase**: at least 12 characters. Every backup is encrypted with it before it
  leaves the server, so the bucket holds nothing readable. Keep it somewhere safe outside
  the server: without it no backup can be restored, and it cannot be recovered. After a
  change, older backups still open with the old passphrase only.
- **Schedule**: off (manual only), a preset (daily at 03:00, every 6 hours, weekly) or a
  five-field cron expression. Times are UTC. A slot missed while the panel was stopped
  runs once at the next start.
- **Backups to keep**: older ones are deleted from storage after each successful backup.
- **Back up now** runs one at once. The list shows what is in storage, with a download
  for each file.

Files are named `dokyr-<UTC time>.tar.gz.age` and sit in the `panel-backups` folder of the
storage connection. A failed backup raises the `backup.failed` alert when a notification
rule sends it somewhere (Settings → Notifications).

A backup is a consistent snapshot of the database (`VACUUM INTO`), the key and a small
manifest, packed as `age(passphrase) → gzip → tar`. Not included: Caddy's certificates
(issued again on demand), deployment logs, images, volumes.

## Restore the panel

### From the panel

For a backup still listed in Settings → Backup, on a panel that works: open the backup's
menu and choose **Restore…**. Only the owner sees it. Enter the passphrase and check the
backup: the panel downloads it, verifies it and shows when it was made and by which
version. Confirming takes a backup of the current state first (it stays in the bucket, so
the restore can be undone by restoring that one), stages the chosen backup and restarts
the panel, which takes a few seconds. Everything changed in the panel since the backup
was made is lost; apps and databases keep running, and those created since stay on the
server without the panel knowing them.

After a restore, the Backup page lists the containers the panel no longer knows, under
"Containers the panel does not know": remove the ones you do not need (their volumes
stay), or create the app or database again.

A backup made by a newer version is refused, and so is a restore while a deployment is
running. Backups in the bucket are not touched: newer ones stay listed after the restore.

### From the command line

On the same server or a new one. The current database and key are moved aside first,
never deleted.

1. Install the panel if it is not there ([Install and update](install.md)), the same
   version as the backup or a newer one. No need to finish the setup link.
2. Copy the backup file to the server (download it from Settings → Backup, or from the
   bucket), then into the container:

   ```sh
   docker cp dokyr-20261120T030000Z.tar.gz.age dokyr:/tmp/
   ```

3. Stage it. The passphrase is asked for; the file is checked (passphrase, key
   fingerprint, SQLite integrity) before anything changes:

   ```sh
   dokyr restore /tmp/dokyr-20261120T030000Z.tar.gz.age
   ```

   Without a terminal, give the passphrase with `--passphrase-file F` or the
   `DOKYR_BACKUP_PASSPHRASE` environment variable. `-` as the file reads the backup from
   standard input.

4. Restart the panel to finish:

   ```sh
   docker restart dokyr
   ```

On start the panel moves the database, its WAL files and the key to
`/var/lib/dokyr/backups/pre-restore-<time>/` and puts the backup in their place. To undo a
mistaken restore, stop the panel, move those files back and start it again.

### After a restore on a new server

The panel knows its apps and databases, but the new server has none of their containers
or data:

- Point the DNS records of the panel and of every app domain at the new server.
- Redeploy each app (images are pulled or built again).
- Start each database instance, then restore its databases from their backups
  ([Databases](databases.md#backups-and-restore)).
- App volumes come back empty: restore their content with the tool that backed them up.
- Sessions in the backup are still valid; sign in as before. Passkeys keep working when
  the panel keeps its hostname.

### Without the panel

A backup opens with `age` and `tar` alone:

```sh
age -d dokyr-20261120T030000Z.tar.gz.age | tar -xz
```

It yields `manifest.json`, `panel.db` and `secret.key`.

## What to check now and then

- The last backup's time on Settings → Backup, or a notification rule for `backup.failed`.
- That the passphrase is written down somewhere that is not this server.
- A test restore on a throw-away server: the only proof a backup works.

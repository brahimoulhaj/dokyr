# Users and roles

Back to the [docs index](README.md).

Settings → Users (owner only) invites people with a one-time link, valid 7 days. Copy the
link and send it yourself; the invited person opens it, picks a password and is signed in.

| | Owner | Admin | Maintainer |
|---|---|---|---|
| Create, deploy and configure apps, databases, routes | yes | yes | yes |
| Delete apps, databases, routes, projects, backups and other resources | yes | yes | no |
| Host terminal, cron jobs, panel backups, GitHub sign-in configuration | yes | yes | no |
| Invite and remove users, change roles | yes | no | no |

There is one owner, who cannot be changed or removed. A role change applies at once;
removing a user ends their sessions and API tokens, and what they created stays. An API
token carries the role of its user. A maintainer cannot reach the
host through an app either: mounting a host path and choosing a Buildpacks builder image
need an admin (a maintainer keeps what an admin set). Inside an app the roles are no
sandbox: a maintainer can open the container's terminal and read its variables.

## Host terminal confirmation

The host terminal asks for the password again before a shell opens (valid 15 minutes for
that browser session). An account with a passkey registered on the panel's domain can
confirm with it instead: the dialog offers **Use a passkey instead**, and the challenge is
for that user's own passkeys, with user verification. Settings → Security chooses the user
the terminal opens as (root by default), switches it off for everyone, and sets after how
many minutes without input a shell is closed (30 by default, 0 for never, 480 at most);
what a shell prints does not count as activity, so a `tail -f` left open still closes.

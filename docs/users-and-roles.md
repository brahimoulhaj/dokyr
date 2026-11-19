# Users and roles

Back to the [docs index](README.md).

Settings → Users (owner only) invites people with a one-time link, valid 7 days. Copy the
link and send it yourself; the invited person opens it, picks a password and is signed in.

| | Owner | Admin | Maintainer |
|---|---|---|---|
| Create, deploy and configure apps, databases, domains | yes | yes | yes |
| Delete apps, databases, domains, projects, backups and other resources | yes | yes | no |
| Host terminal, cron jobs, panel backups, GitHub sign-in configuration | yes | yes | no |
| Invite and remove users, change roles | yes | no | no |

There is one owner, who cannot be changed or removed. A role change applies at once;
removing a user ends their sessions and API tokens, and what they created stays. An API
token carries the role of its user. Roles are guardrails, not a sandbox: a maintainer can
still open an app's container terminal or mount a host path into an app.

## Host terminal confirmation

The host terminal asks for the password again before a shell opens (valid 15 minutes for
that browser session). Settings → Security chooses the user it opens as (root by default)
or switches it off for everyone.

# Deploy on push

A Git app deploys when its branch is pushed, in two ways. Both follow the same rules, and
a commit announced twice is deployed once.

## The app's deploy webhook

App → Settings → Deploy webhook gives a secret URL (`/api/hooks/deploy/<token>`) to add to
the repository on GitHub, GitLab, Gitea or Forgejo, or to call from CI. A push to the
app's branch deploys; other branches, tags and deleted branches are ignored, with the
reason in the answer. An image app takes a `tag` to switch to.

## The GitHub App's own webhook

A GitHub App connection made from Git connections registers its own webhook
(`/api/hooks/github`, push events): GitHub tells the panel about pushes to every repository
the app is installed on, so no webhook is added per repository. Each delivery is checked
against the app's webhook secret (`X-Hub-Signature-256`) before anything is read from it.
The apps of that connection on the pushed repository and branch deploy, unless
**Deploy when <branch> is pushed (GitHub App)** is turned off for the app (it is on by
default).

A connection made before this (its app was created without a webhook) keeps working as it
did and reports no pushes: the panel never received a webhook secret for it. Connect GitHub
again to get a connection with the webhook and point the apps at it, or keep using the
deploy webhook above.

## Only when these paths change

For an app that is one part of a repository, **Only when these paths change** lists the
paths a push must touch, one pattern per line, from the repository's root:

| Pattern | Matches |
| --- | --- |
| `services/api` | that file, or everything under that folder |
| `services/api/` | everything under the folder |
| `*.md` | `*` and `?` stay inside one name |
| `docs/**/*.png` | `**` crosses folders |
| `!**/*.md` | leaves paths out; with only such lines, every other path deploys |

Empty means every push to the branch deploys. The files come from the commits the Git host
lists in its push event; when it does not list them all (a forced push, a new branch, more
commits than the event carries), the push deploys. A deploy started by hand, from CI
without a push event, or by a rollback is never filtered.

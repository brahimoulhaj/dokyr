# First deploy

Back to the [docs index](README.md).

From a fresh install to an app answering on its own domain over HTTPS. It assumes the
panel is installed ([Install and update](install.md)) and the owner account exists.

## Before you start

- **A domain for the panel (recommended).** Settings → General → panel domain. Until then
  the panel stays on `http://<server-ip>`: fine to look around, but sign-in travels in
  clear text and passkeys need HTTPS ([Sign-in methods](sign-in.md)).
- **DNS for the app.** An `A` record (and `AAAA` if the server has IPv6) for the app's
  hostname pointing at the server. The certificate is requested when the hostname is first
  used, so the record has to resolve by then.
- **Ports 80 and 443** open to the internet: Let's Encrypt checks the hostname through them.

The onboarding stepper shown after setup (also under Settings) walks through the panel
domain, storage, a registry and notifications. All of them can be skipped and done later.

## 1. Create a project

Projects → **Create a project**. A project is a folder for related apps: it holds their
routes, shared variables and databases.

## 2. Add an app

In the project, add an app and pick its source.

### From an image

- **Image**: a reference like `nginx:1.27` or `ghcr.io/org/app:tag`. It is pulled on every
  deploy.
- **Registry access**: *Public image*, or a registry added on the Registry page for private
  images.
- **Container port**: the port the process listens on inside the container.

### From a Git repository

- **Repository access**: *Public repository, by URL* (an https URL), or a Git connection
  from Settings → Git connections (GitHub App, GitLab token, or a deploy key with an SSH
  URL).
- **Branch**, and a **build context** when the app lives in a sub-folder.
- **Builder**: *Automatic* uses the repository's Dockerfile when there is one and Railpack
  otherwise. Nixpacks, Buildpacks and Static (a folder served by Caddy, after an optional
  build command) can be chosen explicitly. Images built without a Dockerfile listen on
  `$PORT`, which the panel sets to the app's container port.

Builds run on the server, two at a time by default; see `DOKYR_MAX_BUILDS` and the build
limits in [Configuration](configuration.md).

### From a template or a Compose file

Templates create a ready-made app (and its database when it needs one). A pasted Compose
file is converted into one app per service; the panel does not run Compose itself.

## 3. Set variables and storage

On the app page, before the first deploy:

- **Environment**: runtime variables. `${project.NAME}` uses a shared variable of the
  project, `${db.main.URL}` the connection string of a project database
  ([Databases](databases.md)). A value marked *Secret* is never shown again (type a new
  one to replace it); *Build time* makes it available to the build.
- **Mounts**: a named volume for data that must survive a deploy. An app with a volume
  deploys with *recreate* (the old container stops before the new one starts, a gap of a
  few seconds) instead of *rolling*.
- **Health check**: HTTP (a request answers), TCP (the port accepts a connection) or
  Running (the container stays up). A new version only receives traffic once the check
  passes; if it fails, the new container is removed and the current one keeps serving.

## 4. Deploy

Press **Deploy**. The deployment page shows each step with its log: pull or build, start,
health check, switch. If a step fails, the previous version keeps serving and the log
says why.

Settings changed afterwards are flagged as **Changes not deployed** until the next deploy.

## 5. Give it an address

The panel routes traffic; DNS stays with your DNS provider. Point the record of the
hostname at the server, then tell the panel which app answers on it:

1. On the app, **Routing** → **Add route**.
2. Type the address: `example.com` for the whole hostname, `example.com/api` for one path
   of it, `*.example.com` for every other name under `example.com`. There is nothing to add
   first: the hostname is part of the route.
3. Save. The card of the hostname shows whether its DNS record points at this server.

The longest matching path wins, so `/` can go to a frontend and `/api` to a backend, in
the same project or not. **Routing** in the main menu lists every route of the panel.

The certificate is issued on the first request; plain HTTP is redirected to HTTPS. Behind
a CDN that terminates TLS itself, switch the hostname to **HTTP only** from its Actions.

## 6. Keep it deployed

- **Deploy on push**: a webhook URL per app, or the GitHub App's own webhook, with optional
  watch paths ([Deploy on push](deploy-on-push.md)).
- **Roll back**: Deployments → **Roll back to this** deploys an earlier image again without
  a build. For a Git app the next deploy or push builds the branch again.
- **Alerts**: Settings → Notifications sends failed deploys, apps that go down, failed
  backups and resource thresholds to a channel ([Monitoring](monitoring.md)).

## When the deploy fails

| Symptom | Usual cause |
|---|---|
| Health check never passes | wrong container port, or the app listens on `127.0.0.1` instead of `0.0.0.0` |
| Build is killed | over the build memory cap (Settings → Builds) |
| Private image or repository refused | no registry or Git connection chosen for the app |
| Hostname answers 404 | no route matches the path: the hostname has routes, but none for `/` |
| Certificate error in the browser | the DNS record does not point at the server yet, or port 80/443 is closed |

The app's **Logs** tab shows the running container's output; a container that keeps
restarting is reported on the dashboard as an app problem.

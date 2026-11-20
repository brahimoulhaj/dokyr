# Security

Back to the [docs index](README.md).

How Dokyr protects the server, the secrets it stores and the people signed in to it; what
a review of the code found; and what remains the operator's job. Report a vulnerability
privately to the maintainers rather than in a public issue.

## The model

- **The panel is root on the server.** It holds the Docker socket; whoever controls the
  panel controls the host. Everything below is about who gets to control the panel.
- **Three roles** ([Users and roles](users-and-roles.md)). The owner and admins may reach
  the host on purpose (host terminal, cron jobs, host paths in apps). A **maintainer must
  not** be able to: a way from maintainer to the host, to the panel's secrets or to another
  user's session is a vulnerability.
- **Inside an app there is no sandbox between roles.** A maintainer can open an app's
  terminal and read its variables. Projects do not separate users either.
- **App containers are not trusted.** An app may be compromised from the internet; it
  shares a Docker network with the proxy and the panel.

## What the panel does

### Host and Docker socket

- Every API route needs a session or an API token; role checks are on the server. Host
  terminal, cron jobs, panel settings, panel backup, registry and storage settings,
  notification channels and updates are admin-only; users and invites are owner-only.
- App containers get no privileged mode, no host network, no added capabilities and no
  published ports; `NET_RAW` is dropped. Compose import keeps only image, ports,
  environment, volumes, command and limits.
- A maintainer cannot add or change a host-path mount, mount a volume the panel owns
  (`dokyr-*`, BuildKit state), or choose a Buildpacks builder image. An app that already
  mounts a host path is the admin's: a maintainer cannot change its source, image, start
  command or processes, nor open its terminal.
- The host terminal asks for the password or a passkey again, closes when idle, and closes
  when its user stops being an admin.
- Dockerfile and Static builds run in BuildKit without entitlements. Git clones restrict
  protocols and never put credentials on a command line.
- Caddy's admin API is a unix socket inside the data directory, not a TCP port.

### Secrets at rest

- `/var/lib/dokyr/secret.key`: 32 random bytes, mode 600 in a mode-700 directory. Values
  are sealed with AES-256-GCM and a random nonce.
- Sealed: app and project variables, database credentials, Git tokens and deploy keys, the
  GitHub App key and webhook secret, registry passwords, S3 secret keys, notification
  channel settings, the backup passphrase, deploy webhook tokens.
- Stored as a SHA-256 hash only: sessions, API tokens, invites, the setup token, registry
  tokens. Passwords: PBKDF2-SHA256, 600 000 iterations.
- Secret variables are never returned by the API, are masked in deployment logs, and reach
  builds as BuildKit secret files. The request log keeps no query strings and masks the
  webhook token.
- Panel backups are encrypted with a passphrase (age, scrypt) before they leave the server
  ([Backups and restore](backups.md)).

### Webhooks

- A deploy webhook URL carries a 192-bit random token, checked (by hash) before the body
  is read. It can only deploy that app's configured branch or image tag.
- The GitHub App's webhook is verified with HMAC-SHA256 over the raw body, compared in
  constant time, before anything is parsed.
- Calls are limited per address; repeated pushes of the same commit are ignored for two
  minutes.

### Browser, sessions and headers

- `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy:
  same-origin`, HSTS over HTTPS, a Content-Security-Policy with hashed inline scripts and
  `frame-ancestors 'none'`. No CORS headers.
- Cross-site writes are refused (`Sec-Fetch-Site` and `Origin`), cookies are `HttpOnly`,
  `SameSite=Lax`, host-only and `Secure` over HTTPS. Terminals require a same-origin
  WebSocket.
- A new session token on every sign-in; sessions last 30 days and are listed and revocable
  in Settings → Security.
- The client address is taken from `X-Forwarded-For` only when the request comes from the
  proxy and carries a token made at each start.
- A hostname the panel used to have cannot be given to an app by a maintainer, and a
  wildcard route does not serve it: browsers still send the old session cookie there.
- Sign-in, setup and invite endpoints are rate-limited per address.

## Review, v0.31.0 (2026-11-20)

A read of the code against four areas: Docker socket exposure, secrets at rest, webhook
token handling, headers and sessions. Nothing was exploited live.

### Fixed

| Finding | Was | Fix |
|---|---|---|
| A maintainer could mount any named volume, including the built-in storage's data (every bucket, database dumps), a database instance's files, and the panel's data volume where one is used | medium; high with `DOKYR_DATA_VOLUME` | volumes the panel owns need an admin |
| A maintainer could change the image or command of, or open a terminal in, an app an admin gave a host path (the Docker socket, for example) | medium | such an app's source, image, command, processes and terminal need an admin |
| A `*` route made before the panel's domain moved served the old hostname afterwards, receiving session cookies | medium | a retired hostname covered by a wildcard answers 404 |
| App containers could forge ARP on the shared network and read proxy-to-panel traffic | high | `NET_RAW` dropped from every container the panel creates |
| A demoted admin kept an open host shell | low | the shell closes within 30 seconds |
| `dokyr owner reset-password` left API tokens valid | low | tokens are revoked with the sessions |

`NET_RAW` is dropped when a container is created: apps get it on their next deploy,
database instances and the proxy when they are next recreated.

### Open

Ordered by how much they matter.

1. **Panel, proxy and apps share one Docker network** over plain HTTP. Dropping `NET_RAW`
   closes the known way to intercept it; a network of their own for panel, proxy and
   Adminer would close the class.
2. **Railpack and Nixpacks read the repository inside the panel container** (root, Docker
   socket) to plan a build. Not confirmed exploitable: it depends on whether those tools
   ever execute files from the repository (mise configuration, for example). Planning in
   a throw-away container without the socket would settle it. Until then, treat push
   access to a deployed repository as trusted.
3. **Buildpacks** run the build in a container that has the Docker socket mounted, after
   dropping to the builder's user. Safe while that user cannot open the socket.
4. **Cron jobs** run as root on the host without the re-confirmation the host terminal
   asks for: a stolen admin session is enough.
5. **Internal addresses.** A maintainer cannot point an https Git URL at the server's own
   network, but SSH repository URLs and registry servers are not checked, and the check
   resolves a name once.
6. **File mounts** are stored unsealed and written mode 644; do not put secrets in them.
7. **The session cookie has no `__Host-` prefix**: an app on a sibling subdomain of the
   panel can set a cookie for the parent domain and sign a visitor in to another account.
8. **Revocation.** Registry tokens and deploy webhook URLs are not tied to a user and
   outlive them. A password reset leaves passkeys and a linked GitHub account.
9. **Rate limits are per address only.** Behind a CDN every visitor shares the CDN's
   address: limits are shared and the addresses in sessions and the activity log are the
   CDN's.
10. **The GitHub App webhook** reads up to 25 MiB before it checks the signature.
11. Smaller: maintainers can change build limits; Adminer is served on the panel's origin
    without the panel's Content-Security-Policy; pending sign-in flows are capped globally
    (a flood can block passkey and GitHub sign-in); sealed values are not bound to their
    row.

## What stays your job

- **Keep the server itself safe**: SSH keys only, updates, a firewall that leaves 80, 443
  and SSH. Published database ports are open to the addresses you allow.
- **Give the panel a domain**, so sign-in runs over HTTPS, and do not host apps on
  subdomains of the panel's own hostname (see 7 above).
- **Hand out roles sparingly.** Admins are root. Give maintainers no app with the Docker
  socket or a sensitive host path mounted.
- **One panel per server, and nothing else on its Docker.** Volumes are named host-wide:
  a maintainer can mount a volume of another stack running on the same Docker daemon.
- **Back up the panel** and keep the passphrase elsewhere ([Backups and restore](backups.md)).
- **Replace what leaks**: a webhook URL with **New URL**, an API token by revoking it, a
  password with `dokyr owner reset-password` ([Recovery](recovery.md)).

# Sign-in methods

Back to the [docs index](README.md).

Settings → Security manages passkeys and GitHub sign-in for the signed-in user.
Enter the current password to register/remove a passkey, link/unlink GitHub, or
disable GitHub sign-in. Password sign-in and CLI password recovery
remain available.

## Active sessions

The **Active sessions** section shows the current session first, with browser/device,
IP address, sign-in time and last activity. Sign out an individual session or use
**Log out other sessions** to keep the current browser signed in and revoke every
other session. Revoking another session requires password confirmation; signing
out the current browser uses the normal logout flow. Session lists are scoped to
the signed-in user and never expose tokens or token hashes. Existing sessions
survive the schema update and acquire device information on their next request.

## Passkeys and GitHub

- **Passkeys:** choose a name, click **Add passkey**, and complete the browser's
  fingerprint, face, device PIN or security-key prompt. Sign out and choose
  **Sign in with a passkey**. Passkeys are tied to the hostname where they were
  registered; register another passkey when moving to a different domain.
- **GitHub:** choose **Link GitHub account**, confirm your password, and continue
  on GitHub. On first use, GitHub asks you to create a private sign-in app for this
  panel, then authorize your account. Dokyr registers the callbacks and stores
  the credentials automatically; there are no Client ID or secret fields to fill in.
  Later sign-ins go directly to GitHub authorization. Existing OAuth App
  configurations continue to work without creating a new app.
  Once linked, **Sign in with GitHub** accepts that numeric GitHub account ID only;
  email matching never creates or links accounts. **Disable GitHub sign-in** clears
  the panel's credentials and account links; delete the app separately in GitHub
  if it is no longer needed.

## Requirements and security

Both methods require a configured HTTPS panel domain or `DOKYR_PUBLIC_URL` matching
the browser origin; HTTP is accepted for localhost/loopback development. GitHub app
credentials for repository access in Git connections are separate from sign-in.
The OAuth client secret is encrypted with the panel key and never returned to the
browser; user OAuth access tokens are used only to look up the GitHub identity and
are not stored. App setup and account authorization are separate, single-use,
browser-session-bound steps; creating an app alone never links an account. The
sign-in app requests no repository permissions or webhook events. Pending
sign-in challenges expire after five minutes and are lost
on an API restart; registered passkeys and account links persist.

## Panel domain

Sign-in automatically uses the HTTPS domain saved in Settings → General. The
self-updater preserves that setting and the new image reads it on startup, even
if the installer originally set `DOKYR_PUBLIC_URL` to an HTTP IP address. No edit
to `panel.env`, installer rerun, or manual container recreation is needed. HTTP-only
mode describes the CDN-to-Caddy connection; browsers still use HTTPS for passkeys.
`DOKYR_PUBLIC_URL` remains an optional fallback for external proxy deployments
without a saved panel domain and for localhost development.

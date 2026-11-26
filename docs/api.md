# HTTP API

Back to the [docs index](README.md).

Everything the web app does goes through the panel's HTTP API under `/api`, and the same
API is open to scripts with an API token (Settings → API tokens, sent as
`Authorization: Bearer <token>`). A token carries the role of its user. Some endpoints
answer a browser session only, because they ask for a password, a passkey or a
passphrase: restoring the panel, handing over ownership, the host terminal.

## API version

`GET /api/meta` needs no sign-in and reports, beside the release (`version`), an
`api_version`:

```json
{ "name": "Dokyr", "version": "1.0.0", "api_version": 1, "setup_required": false }
```

`api_version` is a number of its own, so a client can check that a panel speaks the API
it was written for without knowing which release changed what.

It is raised only by a breaking change:

- an endpoint removed or moved,
- a field removed or renamed, or its type or meaning changed,
- a request that was accepted now refused,
- a permission tightened for API tokens.

It is not raised when an endpoint, an optional request field or a response field is
added. Write clients to ignore fields they do not know.

`1` is the API as of v1.0.0. While releases are numbered 0.x, a breaking change may still
land without raising it. From 1.0.0 on, a breaking change raises the number and the
release notes say what changed.

## Errors

A refused request answers JSON with a stable `code` and a message for people, in the
language of the `Accept-Language` header when the panel has it:

```json
{ "error": { "code": "conflict", "message": "this hostname already has routes or a redirect" } }
```

Branch on the status and the `code`, never on the message.

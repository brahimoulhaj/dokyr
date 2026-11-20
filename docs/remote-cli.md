# Remote CLI

Back to the [docs index](README.md).

The remote CLI manages a panel from your own machine. It only speaks the panel's HTTP API,
as you, with your role. It is not the [server CLI](cli.md) (`setup-token`, `owner`,
`update`, `restore`), which works on the data directory and runs on the server.

It is a first slice: sign-in and the project list. The rest (deploy, logs, contexts,
releases for each platform) is planned under M7 in the [Roadmap](../Roadmap.md).

## Build

There are no released binaries yet. From a checkout:

```
make cli          # writes bin/cli/dokyr
```

## Sign in

```
dokyr login https://panel.example.com
```

The command prints a short code and opens the panel in your browser. Sign in there as
usual (password, passkey or GitHub), check that the page shows the same code and device,
and approve. The terminal then says who you are signed in as. The CLI never sees your
password.

- `--no-open` prints the link instead of opening a browser. The browser must still run on
  the same machine as the CLI: the approval comes back to a port on `127.0.0.1`.
- A panel on plain HTTP (`http://<server-ip>`) is refused unless you add `--insecure`,
  because the token would travel unencrypted. A panel on this machine
  (`http://localhost:5173` in development) needs no flag.

What you get is an API token of your account, named `CLI on <device>`. It is listed under
**API tokens** with its last use, and revoking it there signs the CLI out. Like every API
token, it cannot create or revoke tokens.

The sign-in is kept in `~/.config/dokyr/config.json` (`$XDG_CONFIG_HOME/dokyr` when set;
the AppData folder on Windows), readable by your user only. Signing in to another panel
adds it and makes it the current one.

## Projects

```
dokyr projects list           # a table: name, apps, running, databases, domains, last deploy, id
dokyr projects list --json    # the list as the API returns it
```

## How the sign-in works

An authorization code with PKCE, between three parties:

1. The CLI listens on a free port on `127.0.0.1`, makes a random secret (the verifier) and
   opens `/cli/authorize` on the panel with the port, a state and the SHA-256 of the
   secret (the challenge).
2. You approve in a signed-in browser session. The panel makes a one-time code, valid
   5 minutes, and sends the browser to `http://127.0.0.1:<port>/callback`. The panel builds
   that address from the port alone: a code cannot be sent to another host.
3. The CLI checks the state, then exchanges the code and the verifier at
   `POST /api/cli/token` for the token. A code works once, and not without the verifier,
   so another program on the machine that sees the redirect gains nothing.

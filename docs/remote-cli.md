# Remote CLI

Back to the [docs index](README.md).

The remote CLI manages a panel from your own machine. It only speaks the panel's HTTP API,
as you, with your role. It is not the [server CLI](cli.md) (`setup-token`, `owner`,
`update`, `restore`), which works on the data directory and runs on the server.

It is a first slice: sign-in and sign-out, who is signed in, the project list and creating a project. The rest (deploy, logs, contexts,
releases for each platform) is planned under M7 in the [Roadmap](../Roadmap.md).

## Build

There are no released binaries yet. From a checkout:

```
make cli          # writes bin/cli/dokyr
```

## In a terminal and in a script

In a terminal the CLI shows frames, colours, a spinner while it waits, and asks for what
is missing: `dokyr login` without a URL asks for the panel, `dokyr projects create`
without a name asks for the name and the description.

In a pipe or a script (no terminal on standard input and output) it never asks and prints
plain text: columns separated by spaces, no frame, no colour. `--json` gives the API's own
JSON. `NO_COLOR=1` turns colours off in a terminal.

`dokyr help`, `dokyr COMMAND --help` and `dokyr completion bash|zsh|fish|powershell`
(the script to load for tab completion) come with every command.

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

## Who am I, and signing out

```
dokyr status          # or: dokyr whoami
dokyr status --json
dokyr logout
```

`status` shows the panel, the user, the role, whether the sign-in still holds, and the
versions of the panel and of the CLI. It asks the panel, so a token revoked there shows as
no longer accepted, and a role changed there shows as it is now. It ends with a failing
exit code when you are not signed in, which makes it usable as a check in a script. The
token is never printed.

`logout` revokes the token on the panel and forgets it on this machine. The token is
forgotten here even when the panel cannot be reached; the command then says to revoke
`CLI on <device>` under API tokens. A panel older than the one that brought this command
cannot revoke for the CLI either, and gets the same message.

## Projects

```
dokyr projects list           # a table: name, apps, running, databases, domains, last deploy, id
dokyr projects list --json    # the list as the API returns it
```

```
dokyr projects create blog --description "The company blog"
dokyr projects create "My wiki" --json     # the new project as the API returns it
dokyr projects create                      # in a terminal: asks for the name and the description
```

A project's name is unique on the panel; the panel's refusal (a taken name, a name too
long) is printed as it comes. Every role can create a project.

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

Signing out is `POST /api/cli/logout` with the token: it revokes that token and no other.
It is the one thing a token may do to a token; creating and revoking tokens otherwise
needs the browser session.

// Command dokyr is the remote CLI: it manages a panel from a personal machine through
// the HTTP API, as a user, with that user's role. It holds no store, no Docker client
// and no web app; the server and its recovery commands are cmd/panel.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"

	"panel/internal/brand"
)

// version is set at build time: -ldflags "-X main.version=1.2.3".
var version = "dev"

const usage = `Manage a ` + brand.Name + ` panel from this machine.

Usage:
  ` + brand.Slug + ` login URL [--insecure] [--no-open]   sign in through the browser
  ` + brand.Slug + ` projects list [--json]               list the projects of the panel
  ` + brand.Slug + ` projects create NAME [--description TEXT] [--json]
                                             create a project
  ` + brand.Slug + ` version

Sign-in is kept in %s.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, brand.Slug+":", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out, errOut io.Writer) error {
	if len(args) == 0 {
		printUsage(errOut)
		return errors.New("a command is required")
	}
	switch args[0] {
	case "login":
		return runLogin(ctx, args[1:], out, errOut)
	case "projects":
		return runProjects(ctx, args[1:], out, errOut)
	case "version", "--version":
		fmt.Fprintln(out, version)
		return nil
	case "help", "-h", "--help":
		printUsage(out)
		return nil
	}
	return fmt.Errorf("unknown command %q; see `%s help`", args[0], brand.Slug)
}

func printUsage(w io.Writer) {
	path, err := configPath()
	if err != nil {
		path = "the user's config directory"
	}
	fmt.Fprintf(w, usage, path)
}

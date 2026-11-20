// Command dokyr is the remote CLI: it manages a panel from a personal machine through
// the HTTP API, as a user, with that user's role. It holds no store, no Docker client
// and no web app; the server and its recovery commands are cmd/panel.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"panel/internal/brand"
)

// version is set at build time: -ldflags "-X main.version=1.2.3".
var version = "dev"

// errCancelled ends a command the user walked away from; there is nothing to explain.
var errCancelled = errors.New("cancelled")

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	s := terminalStreams()
	err := run(ctx, os.Args[1:], s)
	stop()
	if err != nil {
		if err != errStatusShown { // the command has already said what is wrong
			s.fail(brand.Slug, err)
		}
		os.Exit(1)
	}
}

// run executes one command line. Errors come back to the caller, which prints them:
// cobra prints neither the error nor the usage on a failure of the command itself.
func run(ctx context.Context, args []string, s streams) error {
	root := newRootCmd(s)
	root.SetArgs(args)
	return root.ExecuteContext(ctx)
}

func newRootCmd(s streams) *cobra.Command {
	path, err := configPath()
	if err != nil {
		path = "the user's config directory"
	}
	root := &cobra.Command{
		Use:   brand.Slug,
		Short: "Manage a " + brand.Name + " panel from this machine",
		Long: "Manage a " + brand.Name + " panel from this machine, through its API, as you.\n\n" +
			"Sign in first with `" + brand.Slug + " login https://your-panel`. The sign-in is kept in\n" + path + ".",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE:          func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	root.SetIn(s.in)
	root.SetOut(s.out)
	root.SetErr(s.errOut)
	root.SetVersionTemplate("{{.Version}}\n")
	root.CompletionOptions.HiddenDefaultCmd = false
	root.AddCommand(
		newLoginCmd(s),
		newStatusCmd(s),
		newLogoutCmd(s),
		newProjectsCmd(s),
		newAppsCmd(s),
		newDeployCmd(s),
		newDeploymentsCmd(s),
		newRegistryCmd(s),
		&cobra.Command{
			Use:   "version",
			Short: "Print the version of this CLI",
			Args:  cobra.NoArgs,
			Run:   func(*cobra.Command, []string) { fmt.Fprintln(s.out, version) },
		},
	)
	return root
}

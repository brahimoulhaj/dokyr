package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"panel/internal/brand"
)

// errStatusShown ends `status` with a failing exit code after it has said why itself.
var errStatusShown = errors.New("")

// status is what `dokyr status` knows: the saved sign-in, and what the panel says of it now.
type status struct {
	SignedIn     bool   `json:"signed_in"`
	Panel        string `json:"panel,omitempty"`
	URL          string `json:"url,omitempty"`
	User         string `json:"user,omitempty"`
	Role         string `json:"role,omitempty"`
	PanelVersion string `json:"panel_version,omitempty"`
	CLIVersion   string `json:"cli_version"`
	// Problem says why the sign-in could not be confirmed: refused, or the panel unreachable.
	Problem string `json:"problem,omitempty"`
}

func newStatusCmd(s streams) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "status",
		Aliases: []string{"whoami"},
		Short:   "Show which panel you are signed in to, and as whom",
		Long: "Show which panel you are signed in to, as whom and with which role.\n\n" +
			"The panel is asked, so the answer is what holds now: a token revoked on the panel\n" +
			"shows as no longer accepted. The command fails when you are not signed in.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStatus(cmd.Context(), s, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the status as JSON")
	return cmd
}

func runStatus(ctx context.Context, s streams, asJSON bool) error {
	st := status{CLIVersion: version}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	pc, err := cfg.current()
	if err == nil {
		st.Panel, st.URL, st.User, st.Role = cfg.Current, pc.URL, pc.User, pc.Role
		client := newClient(pc.URL, pc.Token)
		var me struct{ Email, Role string }
		switch err := client.do(ctx, http.MethodGet, "/api/auth/me", nil, &me); {
		case err == nil:
			st.SignedIn = true
			// The role may have changed on the panel since the sign-in.
			if me.Email != pc.User || me.Role != pc.Role {
				st.User, st.Role = me.Email, me.Role
				pc.User, pc.Role = me.Email, me.Role
				cfg.Contexts[cfg.Current] = pc
				_ = saveConfig(cfg) // the next run asks again anyway
			}
		case signedOut(err):
			st.Problem = "the panel no longer accepts this sign-in"
		default:
			st.Problem = err.Error()
		}
		var meta struct{ Version string }
		if client.do(ctx, http.MethodGet, "/api/meta", nil, &meta) == nil {
			st.PanelVersion = meta.Version
		}
	}

	if asJSON {
		raw, err := json.MarshalIndent(st, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(s.out, string(raw))
	} else {
		printStatus(s, st)
	}
	if !st.SignedIn {
		return errStatusShown
	}
	return nil
}

func printStatus(s streams, st status) {
	if st.Panel == "" {
		s.say("Not signed in. Run `%s login https://your-panel`.", brand.Slug)
		return
	}
	state := "signed in"
	switch {
	case st.Problem != "" && strings.Contains(st.Problem, "no longer accepts"):
		state = "signed out: " + st.Problem
	case st.Problem != "":
		state = "unknown: " + st.Problem
	}
	rows := [][2]string{{"Panel", st.URL}, {"User", st.User}, {"Role", st.Role}, {"Sign-in", state}}
	if st.PanelVersion != "" {
		rows = append(rows, [2]string{"Panel version", st.PanelVersion})
	}
	rows = append(rows, [2]string{"CLI version", st.CLIVersion})

	if !s.interactive {
		for _, r := range rows {
			fmt.Fprintf(s.out, "%-15s %s\n", r[0]+":", r[1])
		}
	} else {
		p := s.palette()
		lines := make([]string, 0, len(rows))
		for _, r := range rows {
			value := r[1]
			switch {
			case r[0] == "Sign-in" && st.SignedIn:
				value = lipgloss.NewStyle().Foreground(p.accent).Bold(true).Render("✓ " + value)
			case r[0] == "Sign-in":
				value = lipgloss.NewStyle().Foreground(p.danger).Bold(true).Render("✗ " + value)
			case r[0] == "User", r[0] == "Panel":
				value = lipgloss.NewStyle().Bold(true).Render(value)
			}
			lines = append(lines, lipgloss.NewStyle().Foreground(p.muted).Width(15).Render(r[0])+value)
		}
		s.say("%s", s.card(lines...))
	}
	if !st.SignedIn && strings.Contains(st.Problem, "no longer accepts") {
		s.say("Sign in again with `%s login %s`.", brand.Slug, st.URL)
	}
}

func newLogoutCmd(s streams) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Sign out: revoke the token on the panel and forget it here",
		Long: "Sign out of the current panel. The token is revoked on the panel, then forgotten on\n" +
			"this machine. When the panel cannot be reached, or is too old to know the command, the token is\n" +
			"still forgotten here and the command says to revoke it under API tokens.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLogout(cmd.Context(), s)
		},
	}
}

func runLogout(ctx context.Context, s streams) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	pc, err := cfg.current()
	if err != nil {
		s.say("Not signed in.")
		return nil
	}
	name := cfg.Current
	revokeErr := newClient(pc.URL, pc.Token).do(ctx, http.MethodPost, "/api/cli/logout", nil, nil)

	// Forgotten here whatever the panel said: signing out must not depend on the network.
	delete(cfg.Contexts, name)
	cfg.Current = ""
	others := make([]string, 0, len(cfg.Contexts)) // another panel signed in to earlier, if any
	for other := range cfg.Contexts {
		others = append(others, other)
	}
	if len(others) > 0 {
		sort.Strings(others)
		cfg.Current = others[0]
	}
	if err := saveConfig(cfg); err != nil {
		return fmt.Errorf("the sign-in could not be removed from this machine: %w", err)
	}

	var e *apiError
	switch {
	case revokeErr == nil, signedOut(revokeErr): // revoked now, or already gone
		s.done("Signed out of %s. The token is revoked.", s.accent(name))
	case errors.As(revokeErr, &e) && (e.Status == http.StatusNotFound || e.Status == http.StatusMethodNotAllowed):
		s.done("Signed out of %s on this machine.", s.accent(name))
		s.say("This panel is too old to revoke the token for the CLI. Revoke \"CLI on …\" under API tokens at %s.", pc.URL)
	default:
		s.done("Signed out of %s on this machine.", s.accent(name))
		s.say("The token could not be revoked on the panel (%v). Revoke \"CLI on …\" under API tokens at %s.", revokeErr, pc.URL)
	}
	if cfg.Current != "" {
		s.say("%s", s.muted("Now using "+cfg.Current+"."))
	}
	return nil
}

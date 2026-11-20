package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"text/tabwriter"
	"time"

	"panel/internal/brand"
)

// project is a row of GET /api/projects: the project and its counts.
type project struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	Description      string     `json:"description"`
	AppCount         int        `json:"app_count"`
	AppsRunning      int        `json:"apps_running"`
	Databases        int        `json:"databases"`
	Domains          int        `json:"domains"`
	LastDeployAt     *time.Time `json:"last_deploy_at"`
	LastDeployStatus string     `json:"last_deploy_status"`
}

func runProjects(ctx context.Context, args []string, out, errOut io.Writer) error {
	if len(args) > 0 && args[0] == "list" {
		args = args[1:]
	} else if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		return fmt.Errorf("unknown command \"projects %s\"; there is `%s projects list`", args[0], brand.Slug)
	}
	fs := flag.NewFlagSet("projects list", flag.ContinueOnError)
	fs.SetOutput(errOut)
	asJSON := fs.Bool("json", false, "print the list as JSON, as the API returns it")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("projects list takes no argument, got %q", fs.Arg(0))
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	pc, err := cfg.current()
	if err != nil {
		return err
	}
	var raw json.RawMessage
	if err := newClient(pc.URL, pc.Token).do(ctx, http.MethodGet, "/api/projects", nil, &raw); err != nil {
		if signedOut(err) {
			return fmt.Errorf("%s no longer accepts this sign-in; run `%s login %s`", cfg.Current, brand.Slug, pc.URL)
		}
		return err
	}
	if *asJSON {
		_, err := fmt.Fprintln(out, string(raw))
		return err
	}
	var list []project
	if err := json.Unmarshal(raw, &list); err != nil {
		return fmt.Errorf("%s answered with something that is not a project list", cfg.Current)
	}
	if len(list) == 0 {
		fmt.Fprintf(out, "No projects on %s yet.\n", cfg.Current)
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tAPPS\tRUNNING\tDATABASES\tDOMAINS\tLAST DEPLOY\tID")
	for _, p := range list {
		last := "-"
		if p.LastDeployAt != nil {
			last = p.LastDeployAt.Local().Format("2006-01-02 15:04")
			if p.LastDeployStatus != "" {
				last += " (" + p.LastDeployStatus + ")"
			}
		}
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%s\t%s\n", p.Name, p.AppCount, p.AppsRunning, p.Databases, p.Domains, last, p.ID)
	}
	return tw.Flush()
}

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
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
	// `projects` alone, or with flags only, is the list.
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return runProjectsList(ctx, args, out, errOut)
	}
	switch args[0] {
	case "list":
		return runProjectsList(ctx, args[1:], out, errOut)
	case "create":
		return runProjectsCreate(ctx, args[1:], out, errOut)
	}
	return fmt.Errorf("unknown command \"projects %s\"; there are `%s projects list` and `%s projects create`", args[0], brand.Slug, brand.Slug)
}

// signedIn is the current sign-in and a client for it.
func signedIn() (string, panelContext, *apiClient, error) {
	cfg, err := loadConfig()
	if err != nil {
		return "", panelContext{}, nil, err
	}
	pc, err := cfg.current()
	if err != nil {
		return "", panelContext{}, nil, err
	}
	return cfg.Current, pc, newClient(pc.URL, pc.Token), nil
}

// again turns the panel's refusal of the token into what to do about it.
func again(err error, name string, pc panelContext) error {
	if signedOut(err) {
		return fmt.Errorf("%s no longer accepts this sign-in; run `%s login %s`", name, brand.Slug, pc.URL)
	}
	return err
}

func runProjectsList(ctx context.Context, args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("projects list", flag.ContinueOnError)
	fs.SetOutput(errOut)
	asJSON := fs.Bool("json", false, "print the list as JSON, as the API returns it")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("projects list takes no argument, got %q", fs.Arg(0))
	}
	name, pc, client, err := signedIn()
	if err != nil {
		return err
	}
	var raw json.RawMessage
	if err := client.do(ctx, http.MethodGet, "/api/projects", nil, &raw); err != nil {
		return again(err, name, pc)
	}
	if *asJSON {
		_, err := fmt.Fprintln(out, string(raw))
		return err
	}
	var list []project
	if err := json.Unmarshal(raw, &list); err != nil {
		return fmt.Errorf("%s answered with something that is not a project list", name)
	}
	if len(list) == 0 {
		fmt.Fprintf(out, "No projects on %s yet.\n", name)
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

// runProjectsCreate makes a project. The panel checks the name (its length, one of a
// kind) and its refusal is shown as it comes.
func runProjectsCreate(ctx context.Context, args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("projects create", flag.ContinueOnError)
	fs.SetOutput(errOut)
	description := fs.String("description", "", "what the project is for")
	asJSON := fs.Bool("json", false, "print the new project as JSON, as the API returns it")
	// Flags may come before or after the name.
	var projectName string
	named := false
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return err
		}
		args = fs.Args()
		if len(args) > 0 {
			if named {
				return errors.New("projects create takes one name; quote a name with spaces")
			}
			projectName, named, args = args[0], true, args[1:]
		}
	}
	if strings.TrimSpace(projectName) == "" {
		return fmt.Errorf("usage: %s projects create NAME [--description TEXT] [--json]", brand.Slug)
	}
	name, pc, client, err := signedIn()
	if err != nil {
		return err
	}
	var raw json.RawMessage
	body := map[string]string{"name": projectName, "description": *description}
	if err := client.do(ctx, http.MethodPost, "/api/projects", body, &raw); err != nil {
		return again(err, name, pc)
	}
	if *asJSON {
		_, err := fmt.Fprintln(out, string(raw))
		return err
	}
	var p project
	if err := json.Unmarshal(raw, &p); err != nil || p.ID == "" {
		return fmt.Errorf("%s answered with something that is not a project", name)
	}
	fmt.Fprintf(out, "Created project %s on %s (%s).\n", p.Name, name, p.ID)
	return nil
}

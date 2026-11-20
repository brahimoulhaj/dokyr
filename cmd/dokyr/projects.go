package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"net/http"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"

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

func newProjectsCmd(s streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "projects",
		Aliases: []string{"project"},
		Short:   "List and create projects",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}

	var listJSON bool
	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the projects of the panel",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runProjectsList(cmd.Context(), s, listJSON)
		},
	}
	list.Flags().BoolVar(&listJSON, "json", false, "print the list as JSON, as the API returns it")

	var description string
	var createJSON bool
	create := &cobra.Command{
		Use:   "create [NAME]",
		Short: "Create a project",
		Long: "Create a project. Without a name, in a terminal, it asks for the name and the\n" +
			"description. The panel checks the name: it is unique, and at most 64 characters.",
		Example: "  " + brand.Slug + " projects create blog --description \"The company blog\"\n  " + brand.Slug + " projects create",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 1 {
				return errors.New("projects create takes one name; quote a name with spaces")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			return runProjectsCreate(cmd.Context(), s, name, description, len(args) == 1, createJSON)
		},
	}
	create.Flags().StringVarP(&description, "description", "d", "", "what the project is for")
	create.Flags().BoolVar(&createJSON, "json", false, "print the new project as JSON, as the API returns it")

	cmd.AddCommand(list, create)
	return cmd
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

func runProjectsList(ctx context.Context, s streams, asJSON bool) error {
	name, pc, client, err := signedIn()
	if err != nil {
		return err
	}
	var raw json.RawMessage
	if err := client.do(ctx, http.MethodGet, "/api/projects", nil, &raw); err != nil {
		return again(err, name, pc)
	}
	if asJSON {
		_, err := fmt.Fprintln(s.out, string(raw))
		return err
	}
	var list []project
	if err := json.Unmarshal(raw, &list); err != nil {
		return fmt.Errorf("%s answered with something that is not a project list", name)
	}
	if len(list) == 0 {
		s.say("No projects on %s yet. Create one with `%s projects create`.", name, brand.Slug)
		return nil
	}
	headers := []string{"NAME", "APPS", "RUNNING", "DATABASES", "DOMAINS", "LAST DEPLOY", "ID"}
	rows := make([][]string, 0, len(list))
	for _, p := range list {
		last := "-"
		if p.LastDeployAt != nil {
			last = p.LastDeployAt.Local().Format("2006-01-02 15:04")
			if p.LastDeployStatus != "" {
				last += " (" + p.LastDeployStatus + ")"
			}
		}
		rows = append(rows, []string{p.Name, strconv.Itoa(p.AppCount), strconv.Itoa(p.AppsRunning),
			strconv.Itoa(p.Databases), strconv.Itoa(p.Domains), last, p.ID})
	}
	if !s.interactive { // a pipe or a script: plain columns, no frame
		tw := tabwriter.NewWriter(s.out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, strings.Join(headers, "\t"))
		for _, r := range rows {
			fmt.Fprintln(tw, strings.Join(r, "\t"))
		}
		return tw.Flush()
	}
	p := s.palette()
	s.say("%s", s.table(headers, rows, func(col int, value string) color.Color {
		switch {
		case col == 0:
			return nil
		case col == 5 && strings.Contains(value, "(failed)"):
			return p.danger
		case col == 5 && strings.Contains(value, "(succeeded)"):
			return p.accent
		case col == 6, value == "0", value == "-":
			return p.muted
		}
		return nil
	}))
	s.say("%s", s.muted(fmt.Sprintf("%d on %s", len(list), name)))
	return nil
}

// projectName is the panel's own rule, checked while the user types.
func projectName(v string) error {
	v = strings.TrimSpace(v)
	if v == "" {
		return errors.New("a name is required")
	}
	if len(v) > 64 {
		return errors.New("at most 64 characters")
	}
	return nil
}

// runProjectsCreate makes a project. The panel checks the name (its length, one of a
// kind) and its refusal is shown as it comes.
func runProjectsCreate(ctx context.Context, s streams, projName, description string, named, asJSON bool) error {
	// Signed out is said before asking for anything.
	name, pc, client, err := signedIn()
	if err != nil {
		return err
	}
	if !named {
		if !s.interactive {
			return fmt.Errorf("usage: %s projects create NAME [--description TEXT] [--json]", brand.Slug)
		}
		err := s.ask(
			huh.NewInput().Title("Project name").Description("Unique on "+name+".").CharLimit(64).Value(&projName).Validate(projectName),
			huh.NewInput().Title("Description").Description("Optional: what the project is for.").CharLimit(500).Value(&description).
				Validate(func(v string) error {
					if utf8.RuneCountInString(v) > 500 {
						return errors.New("at most 500 characters")
					}
					return nil
				}),
		)
		if err != nil {
			return err
		}
	}
	if strings.TrimSpace(projName) == "" {
		return fmt.Errorf("usage: %s projects create NAME [--description TEXT] [--json]", brand.Slug)
	}
	var raw json.RawMessage
	body := map[string]string{"name": projName, "description": description}
	if err := client.do(ctx, http.MethodPost, "/api/projects", body, &raw); err != nil {
		return again(err, name, pc)
	}
	if asJSON {
		_, err := fmt.Fprintln(s.out, string(raw))
		return err
	}
	var p project
	if err := json.Unmarshal(raw, &p); err != nil || p.ID == "" {
		return fmt.Errorf("%s answered with something that is not a project", name)
	}
	s.done("Created project %s on %s (%s).", s.accent(p.Name), name, p.ID)
	return nil
}

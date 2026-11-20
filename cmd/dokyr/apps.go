package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"panel/internal/brand"
)

// app is what the CLI reads of an app from the API (GET /api/projects/{id}/apps and
// GET /api/apps/{id}; the last four fields come only with the second).
type app struct {
	ID                  string    `json:"id"`
	ProjectID           string    `json:"project_id"`
	Name                string    `json:"name"`
	SourceType          string    `json:"source_type"` // image or git
	Image               string    `json:"image"`
	GitRepository       string    `json:"git_repository"`
	GitBranch           string    `json:"git_branch"`
	Builder             string    `json:"builder"`
	Port                int       `json:"port"`
	CPULimit            float64   `json:"cpu_limit"`
	MemoryLimitMB       int64     `json:"memory_limit_mb"`
	DesiredState        string    `json:"desired_state"`
	RuntimeState        string    `json:"runtime_state"` // the container: running, exited, …; none before a first deploy
	CurrentDeploymentID string    `json:"current_deployment_id"`
	Strategy            string    `json:"strategy"`
	StartCommand        string    `json:"start_command"`
	Template            string    `json:"template"`
	CreatedAt           time.Time `json:"created_at"`

	Environment   map[string]string `json:"environment"`
	NeedsRedeploy bool              `json:"needs_redeploy"`
	Health        *struct {
		Status   string `json:"status"`
		Reason   string `json:"reason"`
		Restarts int    `json:"restarts"`
	} `json:"health"`
	Processes []struct {
		Name string `json:"name"`
	} `json:"processes"`
	ProcessStates []struct {
		Name  string `json:"name"`
		State string `json:"state"`
	} `json:"process_states"`
}

// source is where the app's image comes from, in one line.
func (a app) source() string {
	if a.SourceType == "git" {
		if a.GitBranch != "" {
			return a.GitRepository + " (" + a.GitBranch + ")"
		}
		return a.GitRepository
	}
	return a.Image
}

// shortSource is source for a table: a repository without its scheme and ".git".
func (a app) shortSource() string {
	if a.SourceType != "git" {
		return a.Image
	}
	repo := strings.TrimSuffix(a.GitRepository, ".git")
	for _, scheme := range []string{"https://", "http://", "ssh://"} {
		repo = strings.TrimPrefix(repo, scheme)
	}
	if a.GitBranch != "" {
		return repo + " (" + a.GitBranch + ")"
	}
	return repo
}

// state is the app's state in the words of the interface.
func (a app) state() string {
	switch a.RuntimeState {
	case "", "none":
		return "not deployed"
	case "running":
		return "running"
	case "exited":
		if a.DesiredState == "stopped" {
			return "stopped"
		}
		return "exited"
	}
	return a.RuntimeState
}

func newAppsCmd(s streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "apps",
		Aliases: []string{"app"},
		Short:   "List, show, create and delete apps",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}

	var listProject string
	var listJSON bool
	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the apps of the panel, or of one project",
		Example: "  " + brand.Slug + " apps list\n  " + brand.Slug + " apps list --project Demo",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAppsList(cmd.Context(), s, listProject, listJSON)
		},
	}
	list.Flags().StringVarP(&listProject, "project", "p", "", "only the apps of this project (name or id)")
	list.Flags().BoolVar(&listJSON, "json", false, "print the apps as JSON, as the API returns them")

	var showProject string
	var showJSON bool
	show := &cobra.Command{
		Use:   "show APP",
		Short: "Show one app: state, source, addresses, limits",
		Long: "Show one app. APP is its name or its id. When two projects have an app of that\n" +
			"name, say which with --project.",
		Example: "  " + brand.Slug + " apps show api\n  " + brand.Slug + " apps show api --project Demo",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppsShow(cmd.Context(), s, args[0], showProject, showJSON)
		},
	}
	show.Flags().StringVarP(&showProject, "project", "p", "", "the project the app is in (name or id)")
	show.Flags().BoolVar(&showJSON, "json", false, "print the app as JSON, as the API returns it")

	cmd.AddCommand(list, show, newAppsCreateCmd(s), newAppsDeleteCmd(s))
	return cmd
}

// projectsOf returns the panel's projects, or the one named by ref (its id, or its name
// whatever the case).
func projectsOf(ctx context.Context, client *apiClient, ref string) ([]project, error) {
	var all []project
	if err := client.do(ctx, http.MethodGet, "/api/projects", nil, &all); err != nil {
		return nil, err
	}
	if ref == "" {
		return all, nil
	}
	for _, p := range all {
		if p.ID == ref {
			return []project{p}, nil
		}
	}
	var named []project
	for _, p := range all {
		if strings.EqualFold(p.Name, ref) {
			named = append(named, p)
		}
	}
	if len(named) == 0 {
		return nil, fmt.Errorf("no project %q; see `%s projects list`", ref, brand.Slug)
	}
	return named, nil
}

// listedApp is an app with the project it is in, and the JSON the API sent for it.
type listedApp struct {
	app
	project project
	raw     json.RawMessage
}

// appsOf lists the apps of the given projects, in the projects' order.
func appsOf(ctx context.Context, client *apiClient, projects []project) ([]listedApp, error) {
	var out []listedApp
	for _, p := range projects {
		var raws []json.RawMessage
		if err := client.do(ctx, http.MethodGet, "/api/projects/"+url.PathEscape(p.ID)+"/apps", nil, &raws); err != nil {
			return nil, err
		}
		for _, raw := range raws {
			la := listedApp{project: p, raw: raw}
			if err := json.Unmarshal(raw, &la.app); err != nil {
				return nil, errors.New("the panel answered with something that is not a list of apps")
			}
			out = append(out, la)
		}
	}
	return out, nil
}

func runAppsList(ctx context.Context, s streams, projectRef string, asJSON bool) error {
	name, pc, client, err := signedIn()
	if err != nil {
		return err
	}
	projects, err := projectsOf(ctx, client, projectRef)
	if err != nil {
		return again(err, name, pc)
	}
	apps, err := appsOf(ctx, client, projects)
	if err != nil {
		return again(err, name, pc)
	}
	if asJSON {
		raws := make([]json.RawMessage, 0, len(apps))
		for _, a := range apps {
			raws = append(raws, a.raw)
		}
		out, err := json.Marshal(raws)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(s.out, string(out))
		return err
	}
	if len(apps) == 0 {
		where := name
		if projectRef != "" {
			where = "project " + projects[0].Name
		}
		s.say("No apps on %s yet.", where)
		return nil
	}
	headers := []string{"APP", "PROJECT", "STATE", "SOURCE", "PORT", "ID"}
	rows := make([][]string, 0, len(apps))
	for _, a := range apps {
		port := "-"
		if a.Port > 0 {
			port = strconv.Itoa(a.Port)
		}
		rows = append(rows, []string{a.Name, a.project.Name, a.state(), a.shortSource(), port, a.ID})
	}
	if !s.interactive {
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
		case col == 2:
			return stateColor(p, value)
		case col == 5, value == "-":
			return p.muted
		}
		return nil
	}))
	running := 0
	for _, a := range apps {
		if a.RuntimeState == "running" {
			running++
		}
	}
	s.say("%s", s.muted(fmt.Sprintf("%d apps on %s, %d running", len(apps), name, running)))
	return nil
}

// stateColor is green for what runs, red for what should and does not, quiet otherwise.
func stateColor(p palette, state string) color.Color {
	switch state {
	case "running", "healthy":
		return p.accent
	case "exited", "dead", "missing", "unhealthy":
		return p.danger
	case "restarting", "created", "paused", "unknown", "starting":
		return p.warn
	}
	return p.muted
}

// findApp resolves APP to one app: an id, or a name that one app has (in the project
// named by projectRef when given).
func findApp(ctx context.Context, client *apiClient, ref, projectRef string) (listedApp, error) {
	projects, err := projectsOf(ctx, client, projectRef)
	if err != nil {
		return listedApp{}, err
	}
	apps, err := appsOf(ctx, client, projects)
	if err != nil {
		return listedApp{}, err
	}
	var found []listedApp
	for _, a := range apps {
		if a.ID == ref {
			return a, nil
		}
		if strings.EqualFold(a.Name, ref) {
			found = append(found, a)
		}
	}
	switch len(found) {
	case 0:
		return listedApp{}, fmt.Errorf("no app %q; see `%s apps list`", ref, brand.Slug)
	case 1:
		return found[0], nil
	}
	in := make([]string, 0, len(found))
	for _, a := range found {
		in = append(in, a.project.Name)
	}
	sort.Strings(in)
	return listedApp{}, fmt.Errorf("%d apps are named %q, in projects %s; add --project, or use the app's id", len(found), ref, strings.Join(in, ", "))
}

func runAppsShow(ctx context.Context, s streams, ref, projectRef string, asJSON bool) error {
	name, pc, client, err := signedIn()
	if err != nil {
		return err
	}
	listed, err := findApp(ctx, client, ref, projectRef)
	if err != nil {
		return again(err, name, pc)
	}
	// The app on its own carries what the list leaves out: health, processes, pending changes.
	var raw json.RawMessage
	if err := client.do(ctx, http.MethodGet, "/api/apps/"+url.PathEscape(listed.ID), nil, &raw); err != nil {
		return again(err, name, pc)
	}
	if asJSON {
		_, err := fmt.Fprintln(s.out, string(raw))
		return err
	}
	var a app
	if err := json.Unmarshal(raw, &a); err != nil {
		return fmt.Errorf("%s answered with something that is not an app", name)
	}
	var routes []struct {
		URL string `json:"url"`
	}
	// Addresses are a nicety: an app without them is still shown.
	_ = client.do(ctx, http.MethodGet, "/api/apps/"+url.PathEscape(a.ID)+"/routes", nil, &routes)

	rows := [][2]string{
		{"App", a.Name},
		{"Project", listed.project.Name},
		{"State", a.state()},
	}
	if a.Health != nil && a.Health.Status != "" {
		health := a.Health.Status
		if a.Health.Reason != "" {
			health += ": " + a.Health.Reason
		}
		if a.Health.Restarts > 0 {
			health += fmt.Sprintf(" (%d restarts)", a.Health.Restarts)
		}
		rows = append(rows, [2]string{"Health", health})
	}
	if a.SourceType == "git" {
		rows = append(rows, [2]string{"Repository", a.source()})
		if a.Builder != "" {
			rows = append(rows, [2]string{"Builder", a.Builder})
		}
	} else {
		rows = append(rows, [2]string{"Image", a.Image})
	}
	if a.Port > 0 {
		rows = append(rows, [2]string{"Port", strconv.Itoa(a.Port)})
	}
	for i, r := range routes {
		label := "Address"
		if i > 0 {
			label = ""
		}
		rows = append(rows, [2]string{label, r.URL})
	}
	if a.StartCommand != "" {
		rows = append(rows, [2]string{"Command", a.StartCommand})
	}
	limits := []string{}
	if a.CPULimit > 0 {
		limits = append(limits, strconv.FormatFloat(a.CPULimit, 'f', -1, 64)+" CPU")
	}
	if a.MemoryLimitMB > 0 {
		limits = append(limits, strconv.FormatInt(a.MemoryLimitMB, 10)+" MB")
	}
	if len(limits) > 0 {
		rows = append(rows, [2]string{"Limits", strings.Join(limits, ", ")})
	}
	for _, ps := range a.ProcessStates {
		rows = append(rows, [2]string{"Process " + ps.Name, ps.State})
	}
	if n := len(a.Environment); n > 0 {
		rows = append(rows, [2]string{"Variables", strconv.Itoa(n)})
	}
	if a.NeedsRedeploy {
		rows = append(rows, [2]string{"Changes", "not deployed yet: deploy to apply them"})
	}
	rows = append(rows, [2]string{"Created", a.CreatedAt.Local().Format("2006-01-02 15:04")}, [2]string{"ID", a.ID})

	if !s.interactive {
		for _, r := range rows {
			label := r[0]
			if label != "" {
				label += ":"
			}
			fmt.Fprintf(s.out, "%-12s %s\n", label, r[1])
		}
		return nil
	}
	p := s.palette()
	width := 0
	for _, r := range rows {
		width = max(width, len(r[0])+2)
	}
	lines := make([]string, 0, len(rows))
	for _, r := range rows {
		value := r[1]
		switch {
		case r[0] == "App":
			value = lipgloss.NewStyle().Bold(true).Render(value)
		case r[0] == "State":
			value = lipgloss.NewStyle().Foreground(stateColor(p, value)).Bold(true).Render(value)
		case r[0] == "Health":
			value = lipgloss.NewStyle().Foreground(stateColor(p, a.Health.Status)).Render(value)
		case strings.HasPrefix(r[0], "Process "):
			value = lipgloss.NewStyle().Foreground(stateColor(p, value)).Render(value)
		case r[0] == "Changes":
			value = lipgloss.NewStyle().Foreground(p.warn).Render(value)
		case r[0] == "ID":
			value = lipgloss.NewStyle().Foreground(p.muted).Render(value)
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(p.muted).Width(width).Render(r[0])+value)
	}
	s.say("%s", s.card(lines...))
	return nil
}

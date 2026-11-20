package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/huh/v2"
	"charm.land/huh/v2/spinner"
	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"panel/internal/brand"
)

// dbInstance is what the CLI reads of a database instance from the API. The password is
// not among it: connection details need the browser session.
type dbInstance struct {
	ID           string    `json:"id"`
	ProjectID    string    `json:"project_id"`
	Name         string    `json:"name"`
	Engine       string    `json:"engine"`
	Version      string    `json:"version"`
	DatabaseName string    `json:"database_name"`
	Username     string    `json:"username"`
	CPULimit     float64   `json:"cpu_limit"`
	MemoryMB     int64     `json:"memory_mb"`
	DesiredState string    `json:"desired_state"`
	Status       string    `json:"status"`
	Error        string    `json:"error"`
	PublicPort   int       `json:"public_port"`
	Host         string    `json:"host"`    // its name on the panel's network
	Volume       string    `json:"volume"`  // where its data lives
	Runtime      string    `json:"runtime"` // the container: running, exited, missing, …
	CreatedAt    time.Time `json:"created_at"`

	// Only in the list.
	Databases        int        `json:"database_count"`
	Users            int        `json:"user_count"`
	BackupPolicies   int        `json:"backup_policies"`
	LastBackupAt     *time.Time `json:"last_backup_at"`
	LastBackupStatus string     `json:"last_backup_status"`

	Operation *dbOperation `json:"operation"` // what a create or an action started
}

// dbOperation is work the panel does on an instance in the background.
type dbOperation struct {
	ID     string `json:"id"`
	Action string `json:"action"`
	Status string `json:"status"` // queued, running, succeeded, failed
	Error  string `json:"error"`
}

type dbEngine struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Versions []string `json:"versions"` // newest first
	Port     int      `json:"port"`
}

// state is the instance's state in the words of the interface.
func (d dbInstance) state() string {
	switch {
	case d.Status == "failed":
		return "failed"
	case d.Runtime == "running":
		return "running"
	case d.Runtime == "exited" && d.DesiredState == "stopped":
		return "stopped"
	case d.Runtime == "" || d.Runtime == "missing":
		if d.Status != "" && d.Status != "running" {
			return d.Status
		}
		return "missing"
	}
	return d.Runtime
}

func newDatabasesCmd(s streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "databases",
		Aliases: []string{"database", "db"},
		Short:   "List, show, create, start, stop and delete database instances",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}

	var listProject string
	var listJSON bool
	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the database instances of the panel, or of one project",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDatabasesList(cmd.Context(), s, listProject, listJSON)
		},
	}
	list.Flags().StringVarP(&listProject, "project", "p", "", "only the instances of this project (name or id)")
	list.Flags().BoolVar(&listJSON, "json", false, "print the instances as JSON, as the API returns them")

	var showProject string
	var showJSON bool
	show := &cobra.Command{
		Use:   "show DATABASE",
		Short: "Show one instance: engine, state, where apps reach it, limits",
		Long: "Show one database instance, by its name or its id. The password is never shown:\n" +
			"connection details are read in the panel, in a signed-in browser.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDatabasesShow(cmd.Context(), s, args[0], showProject, showJSON)
		},
	}
	show.Flags().StringVarP(&showProject, "project", "p", "", "the project the instance is in (name or id)")
	show.Flags().BoolVar(&showJSON, "json", false, "print the instance as JSON, as the API returns it")

	cmd.AddCommand(list, show, newDatabasesCreateCmd(s), newDatabasesDeleteCmd(s))
	for _, action := range []string{"start", "stop", "restart"} {
		cmd.AddCommand(newDatabasesActionCmd(s, action))
	}
	return cmd
}

// databasesOf lists the panel's instances with their projects' names, or those of one project.
func databasesOf(ctx context.Context, client *apiClient, projectRef string) ([]dbInstance, []json.RawMessage, map[string]string, error) {
	projects, err := projectsOf(ctx, client, "")
	if err != nil {
		return nil, nil, nil, err
	}
	names := map[string]string{}
	for _, p := range projects {
		names[p.ID] = p.Name
	}
	only := ""
	if projectRef != "" {
		one, err := projectsOf(ctx, client, projectRef)
		if err != nil {
			return nil, nil, nil, err
		}
		only = one[0].ID
	}
	var raws []json.RawMessage
	if err := client.do(ctx, http.MethodGet, "/api/databases", nil, &raws); err != nil {
		return nil, nil, nil, err
	}
	var list []dbInstance
	var kept []json.RawMessage
	for _, raw := range raws {
		var d dbInstance
		if err := json.Unmarshal(raw, &d); err != nil {
			return nil, nil, nil, errors.New("the panel answered with something that is not a list of databases")
		}
		if only == "" || d.ProjectID == only {
			list, kept = append(list, d), append(kept, raw)
		}
	}
	return list, kept, names, nil
}

func runDatabasesList(ctx context.Context, s streams, projectRef string, asJSON bool) error {
	name, pc, client, err := signedIn()
	if err != nil {
		return err
	}
	list, raws, projects, err := databasesOf(ctx, client, projectRef)
	if err != nil {
		return again(err, name, pc)
	}
	if asJSON {
		if raws == nil {
			raws = []json.RawMessage{}
		}
		out, err := json.Marshal(raws)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(s.out, string(out))
		return err
	}
	if len(list) == 0 {
		s.say("No database instances on %s yet. Create one with `%s databases create`.", name, brand.Slug)
		return nil
	}
	headers := []string{"DATABASE", "PROJECT", "ENGINE", "STATE", "DBS", "USERS", "LAST BACKUP", "ID"}
	rows := make([][]string, 0, len(list))
	running := 0
	for _, d := range list {
		if d.Runtime == "running" {
			running++
		}
		backup := "-"
		if d.LastBackupAt != nil {
			backup = d.LastBackupAt.Local().Format("2006-01-02 15:04")
			if d.LastBackupStatus != "" && d.LastBackupStatus != "succeeded" {
				backup += " (" + d.LastBackupStatus + ")"
			}
		}
		rows = append(rows, []string{d.Name, projects[d.ProjectID], d.Engine + " " + d.Version, d.state(),
			strconv.Itoa(d.Databases), strconv.Itoa(d.Users), backup, d.ID})
	}
	return printTable(s, headers, rows, 3, 7, fmt.Sprintf("%d instances on %s, %d running", len(list), name, running))
}

// findDatabase resolves DATABASE to one instance: an id, or a name one instance has.
func findDatabase(ctx context.Context, client *apiClient, ref, projectRef string) (dbInstance, string, error) {
	list, _, projects, err := databasesOf(ctx, client, projectRef)
	if err != nil {
		return dbInstance{}, "", err
	}
	var found []dbInstance
	for _, d := range list {
		if d.ID == ref {
			return d, projects[d.ProjectID], nil
		}
		if strings.EqualFold(d.Name, ref) {
			found = append(found, d)
		}
	}
	switch len(found) {
	case 0:
		return dbInstance{}, "", fmt.Errorf("no database instance %q; see `%s databases list`", ref, brand.Slug)
	case 1:
		return found[0], projects[found[0].ProjectID], nil
	}
	in := make([]string, 0, len(found))
	for _, d := range found {
		in = append(in, projects[d.ProjectID])
	}
	sort.Strings(in)
	return dbInstance{}, "", fmt.Errorf("%d instances are named %q, in projects %s; add --project, or use the instance's id", len(found), ref, strings.Join(in, ", "))
}

func runDatabasesShow(ctx context.Context, s streams, ref, projectRef string, asJSON bool) error {
	name, pc, client, err := signedIn()
	if err != nil {
		return err
	}
	d, project, err := findDatabase(ctx, client, ref, projectRef)
	if err != nil {
		return again(err, name, pc)
	}
	if asJSON {
		var raw json.RawMessage
		if err := client.do(ctx, http.MethodGet, "/api/databases/"+url.PathEscape(d.ID), nil, &raw); err != nil {
			return again(err, name, pc)
		}
		_, err := fmt.Fprintln(s.out, string(raw))
		return err
	}
	port := 0
	var engines []dbEngine
	if client.do(ctx, http.MethodGet, "/api/database-engines", nil, &engines) == nil {
		for _, e := range engines {
			if e.ID == d.Engine {
				port = e.Port
			}
		}
	}
	host := d.Host
	if port > 0 {
		host += ":" + strconv.Itoa(port)
	}
	rows := [][2]string{
		{"Database", d.Name},
		{"Project", project},
		{"Engine", d.Engine + " " + d.Version},
		{"State", d.state()},
	}
	if d.Error != "" {
		rows = append(rows, [2]string{"Error", d.Error})
	}
	rows = append(rows, [2]string{"Host", host + "  (from apps on this panel)"},
		[2]string{"Name", d.DatabaseName}, [2]string{"User", d.Username},
		[2]string{"Password", "in the panel, on the instance's page"})
	if d.PublicPort > 0 {
		rows = append(rows, [2]string{"Public port", strconv.Itoa(d.PublicPort)})
	}
	limits := []string{}
	if d.CPULimit > 0 {
		limits = append(limits, strconv.FormatFloat(d.CPULimit, 'f', -1, 64)+" CPU")
	}
	if d.MemoryMB > 0 {
		limits = append(limits, strconv.FormatInt(d.MemoryMB, 10)+" MB")
	}
	if len(limits) > 0 {
		rows = append(rows, [2]string{"Limits", strings.Join(limits, ", ")})
	}
	rows = append(rows, [2]string{"Databases", strconv.Itoa(d.Databases)}, [2]string{"Users", strconv.Itoa(d.Users)})
	backup := "never"
	if d.LastBackupAt != nil {
		backup = d.LastBackupAt.Local().Format("2006-01-02 15:04") + " (" + d.LastBackupStatus + ")"
	}
	if d.BackupPolicies > 0 {
		backup += fmt.Sprintf(", %d schedules", d.BackupPolicies)
	} else {
		backup += ", no schedule"
	}
	rows = append(rows, [2]string{"Backups", backup}, [2]string{"Volume", d.Volume},
		[2]string{"Created", d.CreatedAt.Local().Format("2006-01-02 15:04")}, [2]string{"ID", d.ID})
	printCard(s, rows, map[string]string{"State": d.state()})
	return nil
}

// printCard prints label and value pairs: plain lines in a script, a framed card in a
// terminal. states names the rows whose value is coloured as a state.
func printCard(s streams, rows [][2]string, states map[string]string) {
	width := 0
	for _, r := range rows {
		width = max(width, len(r[0])+2)
	}
	if !s.interactive {
		for _, r := range rows {
			label := r[0]
			if label != "" {
				label += ":"
			}
			fmt.Fprintf(s.out, "%-*s %s\n", width, label, r[1])
		}
		return
	}
	p := s.palette()
	lines := make([]string, 0, len(rows))
	for i, r := range rows {
		value := r[1]
		switch state, isState := states[r[0]]; {
		case isState:
			value = lipgloss.NewStyle().Foreground(stateColor(p, state)).Bold(true).Render(value)
		case i == 0:
			value = lipgloss.NewStyle().Bold(true).Render(value)
		case r[0] == "ID", r[0] == "Volume":
			value = lipgloss.NewStyle().Foreground(p.muted).Render(value)
		case r[0] == "Error":
			value = lipgloss.NewStyle().Foreground(p.danger).Render(value)
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(p.muted).Width(width).Render(r[0])+value)
	}
	s.say("%s", s.card(lines...))
}

// waitOperation waits for background work on an instance to end, behind a spinner in a
// terminal. The work is the panel's: leaving does not stop it.
func waitOperation(ctx context.Context, s streams, client *apiClient, op dbOperation, title string) error {
	wait := func(ctx context.Context) error {
		deadline := time.After(15 * time.Minute)
		for {
			if op.Status == "succeeded" {
				return nil
			}
			if op.Status == "failed" {
				if op.Error != "" {
					return errors.New(op.Error)
				}
				return errors.New("the panel reports a failure without a reason; see the instance's page")
			}
			select {
			case <-time.After(time.Second):
			case <-deadline:
				return errors.New("still not done after 15 minutes; it goes on on the server")
			case <-ctx.Done():
				return errors.New("stopped waiting; it goes on on the server")
			}
			if err := client.do(ctx, http.MethodGet, "/api/database-operations/"+url.PathEscape(op.ID), nil, &op); err != nil && ctx.Err() == nil {
				return err
			}
		}
	}
	if !s.interactive {
		return wait(ctx)
	}
	var waitErr error
	finished := false
	_ = spinner.New().Title(" " + title).
		WithTheme(spinner.ThemeFunc(func(isDark bool) *spinner.Styles {
			return &spinner.Styles{Spinner: lipgloss.NewStyle().Foreground(streams{dark: isDark}.palette().accent), Title: lipgloss.NewStyle()}
		})).
		Context(ctx).
		ActionWithErr(func(ctx context.Context) error {
			waitErr = wait(ctx)
			finished = true
			return waitErr
		}).Run()
	if !finished { // Ctrl+C reaches the spinner as a key press
		return errors.New("stopped waiting; it goes on on the server")
	}
	return waitErr
}

var (
	dbInstanceNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)
	dbSQLNameRe      = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)
)

func newDatabasesCreateCmd(s streams) *cobra.Command {
	var projectRef, engine, version, dbName, user string
	var cpu float64
	var memory int64
	var noWait, asJSON bool
	cmd := &cobra.Command{
		Use:   "create [NAME]",
		Short: "Create a database instance and wait until it is ready",
		Long: "Create a database instance in a project: its own container and data volume, a\n" +
			"first database and a user with a generated password. In a terminal, what is missing\n" +
			"is asked for.\n\n" +
			"The password is not printed. Read the connection details in the panel, or connect\n" +
			"the instance to an app there.",
		Example: "  " + brand.Slug + " databases create main --project Demo --engine postgres\n" +
			"  " + brand.Slug + " db create shop-db -p Demo --engine mariadb --version 11.4 --database shop --user shop --memory 512",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			return runDatabasesCreate(cmd.Context(), s, name, projectRef, engine, version, dbName, user, cpu, memory, noWait, asJSON)
		},
	}
	f := cmd.Flags()
	f.StringVarP(&projectRef, "project", "p", "", "the project to create the instance in (name or id)")
	f.StringVar(&engine, "engine", "", "postgres or mariadb")
	f.StringVar(&version, "version", "", "the engine's version (default: the newest the panel offers)")
	f.StringVar(&dbName, "database", "", "the first database (default: from the instance's name)")
	f.StringVar(&user, "user", "", "the first user (default: app)")
	f.Float64Var(&cpu, "cpu", 0, "CPU limit in cores (default unlimited)")
	f.Int64Var(&memory, "memory", 0, "memory limit in MB, at least 128 (default unlimited)")
	f.BoolVar(&noWait, "no-wait", false, "return once the creation is accepted")
	f.BoolVar(&asJSON, "json", false, "print the new instance as JSON, as the API returns it")
	return cmd
}

func runDatabasesCreate(ctx context.Context, s streams, instance, projectRef, engine, version, dbName, user string, cpu float64, memory int64, noWait, asJSON bool) error {
	name, pc, client, err := signedIn()
	if err != nil {
		return err
	}
	projects, err := projectsOf(ctx, client, projectRef)
	if err != nil {
		return again(err, name, pc)
	}
	if len(projects) == 0 {
		return fmt.Errorf("%s has no project yet; create one with `%s projects create`", name, brand.Slug)
	}
	var engines []dbEngine
	if err := client.do(ctx, http.MethodGet, "/api/database-engines", nil, &engines); err != nil {
		return again(err, name, pc)
	}
	target := projects[0]
	if projectRef == "" && len(projects) > 1 {
		if !s.interactive {
			return errors.New("which project? add --project NAME")
		}
		options := make([]huh.Option[string], 0, len(projects))
		for _, p := range projects {
			options = append(options, huh.NewOption(p.Name, p.ID))
		}
		id := projects[0].ID
		if err := s.ask(huh.NewSelect[string]().Title("Project").Options(options...).Value(&id)); err != nil {
			return err
		}
		for _, p := range projects {
			if p.ID == id {
				target = p
			}
		}
	}
	if instance == "" || engine == "" {
		if !s.interactive {
			return fmt.Errorf("usage: %s databases create NAME --project P --engine postgres|mariadb", brand.Slug)
		}
		var fields []huh.Field
		if instance == "" {
			fields = append(fields, huh.NewInput().Title("Instance name").
				Description("Starts with a letter; lowercase letters, digits and dashes. Unique in "+target.Name+".").
				CharLimit(48).Value(&instance).
				Validate(func(v string) error {
					if !dbInstanceNameRe.MatchString(strings.TrimSpace(v)) {
						return errors.New("a letter first, then up to 47 lowercase letters, digits or dashes")
					}
					return nil
				}))
		}
		if engine == "" && len(engines) > 0 {
			options := make([]huh.Option[string], 0, len(engines))
			for _, e := range engines {
				options = append(options, huh.NewOption(e.Name, e.ID))
			}
			engine = engines[0].ID
			fields = append(fields, huh.NewSelect[string]().Title("Engine").Options(options...).Value(&engine))
		}
		if err := s.ask(fields...); err != nil {
			return err
		}
		// The version is asked once the engine is known, and only when the flag left it out.
		for _, e := range engines {
			if e.ID == engine && version == "" && len(e.Versions) > 1 {
				version = e.Versions[0]
				if err := s.ask(huh.NewSelect[string]().Title(e.Name + " version").Options(huh.NewOptions(e.Versions...)...).Value(&version)); err != nil {
					return err
				}
			}
		}
	}
	instance = strings.TrimSpace(instance)
	if version == "" {
		for _, e := range engines {
			if e.ID == engine && len(e.Versions) > 0 {
				version = e.Versions[0]
			}
		}
	}
	if dbName == "" { // main-db gives the database main_db
		dbName = strings.ReplaceAll(instance, "-", "_")
	}
	if user == "" {
		user = "app"
	}
	if !dbSQLNameRe.MatchString(dbName) || !dbSQLNameRe.MatchString(user) {
		return errors.New("--database and --user start with a letter and hold up to 48 lowercase letters, digits or underscores")
	}

	body := map[string]any{"project_id": target.ID, "name": instance, "engine": engine, "version": version,
		"database_name": dbName, "username": user, "cpu_limit": cpu, "memory_mb": memory}
	var raw json.RawMessage
	if err := client.do(ctx, http.MethodPost, "/api/databases", body, &raw); err != nil {
		return again(err, name, pc)
	}
	var d dbInstance
	if err := json.Unmarshal(raw, &d); err != nil || d.ID == "" {
		return fmt.Errorf("%s answered with something that is not a database instance", name)
	}
	if !noWait && d.Operation != nil {
		if err := waitOperation(ctx, s, client, *d.Operation, "Creating "+d.Name+" ("+engine+" "+version+")"); err != nil {
			return fmt.Errorf("creating %s: %w", d.Name, err)
		}
	}
	if asJSON {
		_, err := fmt.Fprintln(s.out, string(raw))
		return err
	}
	if noWait {
		s.done("Creating database instance %s in project %s (%s).", s.accent(d.Name), target.Name, d.ID)
		return nil
	}
	s.done("Created database instance %s in project %s (%s).", s.accent(d.Name), target.Name, d.ID)
	s.say("%s", s.muted(fmt.Sprintf("%s %s · database %s · user %s · host %s", engine, version, dbName, user, d.Host)))
	s.say("%s", s.muted("The password is on the instance's page in the panel."))
	return nil
}

func newDatabasesActionCmd(s streams, action string) *cobra.Command {
	var projectRef string
	var noWait bool
	short := map[string]string{"start": "Start a stopped instance", "stop": "Stop an instance; its data stays", "restart": "Restart an instance"}[action]
	cmd := &cobra.Command{
		Use:   action + " DATABASE",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, pc, client, err := signedIn()
			if err != nil {
				return err
			}
			d, _, err := findDatabase(cmd.Context(), client, args[0], projectRef)
			if err != nil {
				return again(err, name, pc)
			}
			var out dbInstance
			if err := client.do(cmd.Context(), http.MethodPost, "/api/databases/"+url.PathEscape(d.ID)+"/"+action, nil, &out); err != nil {
				return again(err, name, pc)
			}
			doing := map[string]string{"start": "Starting", "stop": "Stopping", "restart": "Restarting"}[action]
			did := map[string]string{"start": "Started", "stop": "Stopped", "restart": "Restarted"}[action]
			if !noWait && out.Operation != nil {
				if err := waitOperation(cmd.Context(), s, client, *out.Operation, doing+" "+d.Name); err != nil {
					return fmt.Errorf("%s %s: %w", strings.ToLower(doing), d.Name, err)
				}
				s.done("%s database instance %s.", did, s.accent(d.Name))
				return nil
			}
			s.done("%s database instance %s.", doing, s.accent(d.Name))
			return nil
		},
	}
	cmd.Flags().StringVarP(&projectRef, "project", "p", "", "the project the instance is in (name or id)")
	cmd.Flags().BoolVar(&noWait, "no-wait", false, "return once the "+action+" is accepted")
	return cmd
}

func newDatabasesDeleteCmd(s streams) *cobra.Command {
	var projectRef string
	var yes, noWait bool
	cmd := &cobra.Command{
		Use:     "delete DATABASE",
		Aliases: []string{"rm"},
		Short:   "Remove an instance from the panel; its data volume stays on the server",
		Long: "Remove a database instance: its container is stopped and removed, and the instance\n" +
			"leaves the panel with its databases, users and backup schedules. Its data volume\n" +
			"stays on the server. Apps that use its references fail their next deployment.\n\n" +
			"In a terminal you confirm by typing the instance's name. In a script, --yes is\n" +
			"required. The panel refuses a maintainer.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDatabasesDelete(cmd.Context(), s, args[0], projectRef, yes, noWait)
		},
	}
	cmd.Flags().StringVarP(&projectRef, "project", "p", "", "the project the instance is in (name or id)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "delete without asking")
	cmd.Flags().BoolVar(&noWait, "no-wait", false, "return once the removal is accepted")
	return cmd
}

func runDatabasesDelete(ctx context.Context, s streams, ref, projectRef string, yes, noWait bool) error {
	name, pc, client, err := signedIn()
	if err != nil {
		return err
	}
	d, project, err := findDatabase(ctx, client, ref, projectRef)
	if err != nil {
		return again(err, name, pc)
	}
	if !yes {
		if !s.interactive {
			return fmt.Errorf("removing %s takes it and its users out of the panel; add --yes to do it without a prompt", d.Name)
		}
		p := s.palette()
		s.say("%s", s.card(
			s.styled(p.danger, true, "Remove database instance "+d.Name+"?"),
			"",
			"Project  "+project,
			"Engine   "+d.Engine+" "+d.Version,
			"State    "+d.state(),
			"",
			"Its container is removed, and the instance leaves the panel with",
			"its databases, users and backup schedules.",
			"Its data volume stays on the server: "+d.Volume,
			"Apps using its references fail their next deployment.",
		))
		typed := ""
		err := s.ask(huh.NewInput().Title("Type the instance's name to remove it").Placeholder(d.Name).Value(&typed).
			Validate(func(v string) error {
				if strings.TrimSpace(v) != d.Name {
					return errors.New("that is not " + d.Name + " (Ctrl+C to keep the instance)")
				}
				return nil
			}))
		if err != nil {
			return err
		}
	}
	// The panel wants the name too: the same confirmation, on its side.
	var out struct {
		Operation *dbOperation `json:"operation"`
	}
	if err := client.do(ctx, http.MethodDelete, "/api/databases/"+url.PathEscape(d.ID), map[string]string{"name": d.Name}, &out); err != nil {
		return again(err, name, pc)
	}
	if !noWait && out.Operation != nil {
		if err := waitOperation(ctx, s, client, *out.Operation, "Removing "+d.Name); err != nil {
			return fmt.Errorf("removing %s: %w", d.Name, err)
		}
	}
	s.done("Removed database instance %s from project %s.", s.accent(d.Name), project)
	s.say("%s", s.muted("Its data volume stays on the server: "+d.Volume))
	return nil
}

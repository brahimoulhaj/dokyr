package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"panel/internal/brand"
)

// deployment is what the CLI reads of a deployment from the API.
type deployment struct {
	ID            string     `json:"id"`
	AppID         string     `json:"app_id"`
	AppName       string     `json:"app_name"`   // only in the list of active deployments
	ProjectID     string     `json:"project_id"` // idem
	Status        string     `json:"status"`     // queued, running, succeeded, failed, cancelled, rolled_back
	Trigger       string     `json:"trigger"`
	Image         string     `json:"image"`
	CommitSHA     string     `json:"commit_sha"`
	CommitMessage string     `json:"commit_message"`
	Error         string     `json:"error"`
	Phase         string     `json:"phase"`
	RollbackOf    string     `json:"rollback_of"`
	StartedAt     *time.Time `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at"`
	CreatedAt     time.Time  `json:"created_at"`
}

// took is how long the deployment ran, or has been running.
func (d deployment) took() string {
	if d.StartedAt == nil {
		return "-"
	}
	end := time.Now()
	if d.FinishedAt != nil {
		end = *d.FinishedAt
	}
	return shortDuration(end.Sub(*d.StartedAt))
}

// what is the thing deployed, short: a commit with its message, or an image.
func (d deployment) what() string {
	if d.CommitSHA != "" {
		sha := d.CommitSHA
		if len(sha) > 7 {
			sha = sha[:7]
		}
		if msg, _, _ := strings.Cut(strings.TrimSpace(d.CommitMessage), "\n"); msg != "" {
			return sha + " " + msg
		}
		return sha
	}
	if d.Image != "" {
		return d.Image
	}
	return "-"
}

func shortDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
}

func newDeployCmd(s streams) *cobra.Command {
	var projectRef string
	var noWait, attach, asJSON bool
	cmd := &cobra.Command{
		Use:   "deploy APP",
		Short: "Deploy an app and follow its log",
		Long: "Deploy an app: the panel pulls its image or builds it from its repository, starts\n" +
			"the new container and checks it answers. The deployment's log is printed as it\n" +
			"is written, and the command ends with the deployment: it fails when the deployment\n" +
			"did not succeed, so a script can rely on it.\n\n" +
			"Ctrl+C stops following, not the deployment: it goes on on the server. Pick it up\n" +
			"again with --attach, which follows the app's deployment in progress and never\n" +
			"starts one.",
		Example: "  " + brand.Slug + " deploy api\n  " + brand.Slug + " deploy api --project Demo --no-wait\n  " + brand.Slug + " deploy api --attach",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeploy(cmd.Context(), s, args[0], projectRef, noWait, attach, asJSON)
		},
	}
	cmd.Flags().StringVarP(&projectRef, "project", "p", "", "the project the app is in (name or id)")
	cmd.Flags().BoolVar(&noWait, "no-wait", false, "start the deployment and return at once")
	cmd.Flags().BoolVar(&attach, "attach", false, "follow the deployment in progress instead of starting one")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the deployment as JSON instead of its log: as started with --no-wait, as ended otherwise")
	cmd.MarkFlagsMutuallyExclusive("no-wait", "attach")
	return cmd
}

func runDeploy(ctx context.Context, s streams, ref, projectRef string, noWait, attach, asJSON bool) error {
	name, pc, client, err := signedIn()
	if err != nil {
		return err
	}
	target, err := findApp(ctx, client, ref, projectRef)
	if err != nil {
		return again(err, name, pc)
	}

	var dep deployment
	joined := attach
	if !attach {
		err := client.do(ctx, http.MethodPost, "/api/apps/"+url.PathEscape(target.ID)+"/deploy", nil, &dep)
		var e *apiError
		switch {
		case err == nil:
		case errors.As(err, &e) && e.Status == http.StatusConflict && !noWait:
			joined = true // something is already under way on this app: follow that
		default:
			return again(err, name, pc)
		}
	}
	if joined {
		active, err := activeDeployments(ctx, client)
		if err != nil {
			return again(err, name, pc)
		}
		found := false
		for _, d := range active {
			if d.AppID == target.ID {
				dep, found = d, true
			}
		}
		switch {
		case !found && attach:
			return fmt.Errorf("no deployment of %s is in progress; start one with `%s deploy %s`", target.Name, brand.Slug, target.Name)
		case !found:
			return fmt.Errorf("%s is busy with another operation (a start, stop or restart); try again in a moment", target.Name)
		case !attach:
			fmt.Fprintf(s.errOut, "A deployment of %s is already in progress; following it.\n", target.Name)
		}
	}
	if noWait {
		if asJSON {
			return printJSON(s, dep)
		}
		s.done("Started deployment %s of %s.", dep.ID, s.accent(target.Name))
		s.say("%s", s.muted("Follow it with `"+brand.Slug+" deploy "+target.Name+" --attach`."))
		return nil
	}

	if !asJSON {
		s.say("%s %s %s", s.accent("Deploying "+target.Name), s.muted("in "+target.project.Name), s.muted("· deployment "+dep.ID))
	}
	logTo := s.out
	if asJSON {
		logTo = io.Discard
	}
	status, reason, err := followDeployment(ctx, s, client, dep.ID, logTo)
	if err != nil {
		if ctx.Err() != nil {
			// The user left; the deployment did not.
			fmt.Fprintf(s.errOut, "\nStopped following. The deployment goes on: `%s deploy %s --attach` follows it again.\n", brand.Slug, target.Name)
			return errStatusShown // said above; nothing failed
		}
		return fmt.Errorf("lost the deployment's log (%w); it may still be running: `%s deploy %s --attach`", err, brand.Slug, target.Name)
	}
	// The record has what the stream does not: when it started and ended.
	final := dep
	if err := client.do(ctx, http.MethodGet, "/api/deployments/"+url.PathEscape(dep.ID), nil, &final); err != nil {
		final.Status, final.Error = status, reason
	}
	if asJSON {
		if err := printJSON(s, final); err != nil {
			return err
		}
	}
	if final.Status != "succeeded" {
		why := final.Error
		if why == "" {
			why = reason
		}
		msg := "deployment " + strings.ReplaceAll(final.Status, "_", " ")
		if final.Status == "rolled_back" {
			msg = "deployment failed, the previous version was put back"
		}
		if why != "" {
			msg += ": " + why
		}
		return errors.New(msg)
	}
	if asJSON {
		return nil
	}
	var routes []struct {
		URL string `json:"url"`
	}
	_ = client.do(ctx, http.MethodGet, "/api/apps/"+url.PathEscape(target.ID)+"/routes", nil, &routes)
	where := ""
	if len(routes) > 0 {
		where = " → " + routes[0].URL
	}
	s.done("Deployed %s in %s%s", s.accent(target.Name), final.took(), where)
	return nil
}

func printJSON(s streams, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(s.out, string(raw))
	return err
}

func activeDeployments(ctx context.Context, client *apiClient) ([]deployment, error) {
	var out []deployment
	err := client.do(ctx, http.MethodGet, "/api/deployments/active", nil, &out)
	return out, err
}

// followDeployment prints the deployment's log as the panel streams it (Server-Sent
// Events) and returns the status the stream ends with. A connection that drops is
// taken up again where it stopped, which the panel supports through Last-Event-ID.
func followDeployment(ctx context.Context, s streams, client *apiClient, id string, out io.Writer) (status, reason string, err error) {
	lastID := ""
	failures := 0
	for {
		ended, st, why, progressed, err := readLogStream(ctx, s, client, id, &lastID, out)
		if ended {
			return st, why, nil
		}
		if ctx.Err() != nil {
			return "", "", ctx.Err()
		}
		var e *apiError
		if errors.As(err, &e) { // the panel said no: asking again will not change it
			return "", "", err
		}
		if progressed {
			failures = 0
		}
		failures++
		if failures > 5 {
			if err == nil {
				err = errors.New("the stream keeps closing")
			}
			return "", "", err
		}
		select {
		case <-time.After(time.Duration(failures) * time.Second):
		case <-ctx.Done():
			return "", "", ctx.Err()
		}
	}
}

// readLogStream reads one connection of the log stream until its "end" event or until
// it breaks. lastID is kept up to date for the next connection.
func readLogStream(ctx context.Context, s streams, client *apiClient, id string, lastID *string, out io.Writer) (ended bool, status, reason string, progressed bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, client.base+"/api/deployments/"+url.PathEscape(id)+"/log/stream", nil)
	if err != nil {
		return false, "", "", false, err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", brand.Slug+"-cli/"+version)
	req.Header.Set("Authorization", "Bearer "+client.token)
	if *lastID != "" {
		req.Header.Set("Last-Event-ID", *lastID)
	}
	// No overall timeout: a build takes as long as it takes. The panel pings a quiet stream.
	streaming := &http.Client{CheckRedirect: client.http.CheckRedirect}
	res, err := streaming.Do(req)
	if err != nil {
		return false, "", "", false, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<16))
		var e struct {
			Error struct{ Code, Message string }
		}
		msg := "the panel answered " + res.Status
		if json.Unmarshal(raw, &e) == nil && e.Error.Message != "" {
			msg = e.Error.Message
		}
		return false, "", "", false, &apiError{Status: res.StatusCode, Code: e.Error.Code, Message: msg}
	}

	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20) // a build can print very long lines
	var event, data, eventID string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "": // the blank line that ends an event
			switch event {
			case "log":
				var l struct {
					Text string `json:"text"`
				}
				if json.Unmarshal([]byte(data), &l) == nil {
					fmt.Fprintln(out, logStyle(s, l.Text))
				}
				if eventID != "" {
					*lastID = eventID
				}
				progressed = true
			case "end":
				var end struct{ Status, Error string }
				_ = json.Unmarshal([]byte(data), &end)
				return true, end.Status, end.Error, progressed, nil
			}
			event, data, eventID = "", "", ""
		case strings.HasPrefix(line, ":"): // a comment: the panel's ping
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(line[len("event:"):])
		case strings.HasPrefix(line, "data:"):
			data += strings.TrimPrefix(line[len("data:"):], " ")
		case strings.HasPrefix(line, "id:"):
			eventID = strings.TrimSpace(line[len("id:"):])
		}
	}
	return false, "", "", progressed, sc.Err()
}

// logStyle sets the panel's own lines apart from what a build or a container printed
// (those are indented), in a terminal.
func logStyle(s streams, text string) string {
	if !s.interactive {
		return text
	}
	if strings.HasPrefix(text, "  ") {
		return s.muted(text)
	}
	return text
}

func newDeploymentsCmd(s streams) *cobra.Command {
	var projectRef string
	var limit int
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "deployments [APP]",
		Aliases: []string{"deployment"},
		Short:   "Show the deployments in progress, or an app's history",
		Long: "Without an app: the deployments queued or running now, on the whole panel.\n" +
			"With an app: its latest deployments, newest first, the one that runs marked.",
		Example: "  " + brand.Slug + " deployments\n  " + brand.Slug + " deployments api\n  " + brand.Slug + " deployments api --limit 30",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return runActiveDeployments(cmd.Context(), s, asJSON)
			}
			return runAppDeployments(cmd.Context(), s, args[0], projectRef, limit, asJSON)
		},
	}
	cmd.Flags().StringVarP(&projectRef, "project", "p", "", "the project the app is in (name or id)")
	cmd.Flags().IntVarP(&limit, "limit", "n", 10, "how many of the app's deployments to show (at most 100)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the deployments as JSON, as the API returns them")
	return cmd
}

func runActiveDeployments(ctx context.Context, s streams, asJSON bool) error {
	name, pc, client, err := signedIn()
	if err != nil {
		return err
	}
	var raw json.RawMessage
	if err := client.do(ctx, http.MethodGet, "/api/deployments/active", nil, &raw); err != nil {
		return again(err, name, pc)
	}
	if asJSON {
		_, err := fmt.Fprintln(s.out, string(raw))
		return err
	}
	var active []deployment
	if err := json.Unmarshal(raw, &active); err != nil {
		return fmt.Errorf("%s answered with something that is not a list of deployments", name)
	}
	if len(active) == 0 {
		s.say("No deployment in progress on %s.", name)
		return nil
	}
	projects, _ := projectsOf(ctx, client, "") // for their names; ids do when it fails
	projectName := func(id string) string {
		for _, p := range projects {
			if p.ID == id {
				return p.Name
			}
		}
		return id
	}
	headers := []string{"APP", "PROJECT", "STATUS", "STEP", "TRIGGER", "RUNNING FOR", "ID"}
	rows := make([][]string, 0, len(active))
	for _, d := range active {
		rows = append(rows, []string{d.AppName, projectName(d.ProjectID), d.Status, dash(strings.ReplaceAll(d.Phase, "_", " ")), dash(d.Trigger), d.took(), d.ID})
	}
	return printTable(s, headers, rows, 2, 6, fmt.Sprintf("%d in progress on %s", len(active), name))
}

func runAppDeployments(ctx context.Context, s streams, ref, projectRef string, limit int, asJSON bool) error {
	name, pc, client, err := signedIn()
	if err != nil {
		return err
	}
	target, err := findApp(ctx, client, ref, projectRef)
	if err != nil {
		return again(err, name, pc)
	}
	limit = min(max(limit, 1), 100)
	var raw json.RawMessage
	path := "/api/apps/" + url.PathEscape(target.ID) + "/deployments?per_page=" + strconv.Itoa(limit)
	if err := client.do(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return again(err, name, pc)
	}
	if asJSON {
		_, err := fmt.Fprintln(s.out, string(raw))
		return err
	}
	var list []deployment
	if err := json.Unmarshal(raw, &list); err != nil {
		return fmt.Errorf("%s answered with something that is not a list of deployments", name)
	}
	if len(list) == 0 {
		s.say("%s has never been deployed. Deploy it with `%s deploy %s`.", target.Name, brand.Slug, target.Name)
		return nil
	}
	headers := []string{"", "STATUS", "WHAT", "TRIGGER", "WHEN", "TOOK", "ID"}
	rows := make([][]string, 0, len(list))
	for _, d := range list {
		mark := ""
		if d.ID == target.CurrentDeploymentID {
			mark = "●" // the deployment that runs now
		}
		what := d.what()
		if d.RollbackOf != "" {
			what = "rollback: " + what
		}
		rows = append(rows, []string{mark, strings.ReplaceAll(d.Status, "_", " "), what, dash(d.Trigger), d.CreatedAt.Local().Format("2006-01-02 15:04"), d.took(), d.ID})
	}
	return printTable(s, headers, rows, 1, 6, fmt.Sprintf("%s in %s · ● runs now", target.Name, target.project.Name))
}

func dash(v string) string {
	if v == "" {
		return "-"
	}
	return v
}

// printTable prints rows as plain columns in a script and as a framed table in a
// terminal, where the status column is coloured and the id column is quiet.
func printTable(s streams, headers []string, rows [][]string, statusCol, idCol int, footer string) error {
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
		case col == statusCol:
			switch value {
			case "succeeded":
				return p.accent
			case "failed", "rolled back":
				return p.danger
			case "running", "queued":
				return p.warn
			}
			return p.muted
		case value == "●":
			return p.accent
		case col == idCol, value == "-":
			return p.muted
		}
		return nil
	}))
	if footer != "" {
		s.say("%s", s.muted(footer))
	}
	return nil
}

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"

	"panel/internal/brand"
)

// appNameRe is the panel's rule for an app's name, checked while the user types. The
// panel checks again and has the last word.
var appNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?$`)

// appCreate is what `apps create` was given on the command line.
type appCreate struct {
	project, image, git, branch, gitSource, builder, command string
	port                                                     int
	cpu                                                      float64
	memory                                                   int64
	env                                                      []string
	asJSON                                                   bool
}

func newAppsCreateCmd(s streams) *cobra.Command {
	var in appCreate
	cmd := &cobra.Command{
		Use:   "create [NAME]",
		Short: "Create an app from an image or a Git repository",
		Long: "Create an app in a project, from a ready image (--image) or built from a Git\n" +
			"repository (--git). In a terminal, what is missing is asked for.\n\n" +
			"Creating an app does not start it: it runs after its first deploy.",
		Example: "  " + brand.Slug + " apps create web --project Demo --image nginx:1.27 --port 80\n" +
			"  " + brand.Slug + " apps create api -p Demo --git https://github.com/org/api.git --branch main --port 8080\n" +
			"  " + brand.Slug + " apps create",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			return runAppsCreate(cmd.Context(), s, name, in, cmd.Flags().Changed("port"))
		},
	}
	f := cmd.Flags()
	f.StringVarP(&in.project, "project", "p", "", "the project to create the app in (name or id)")
	f.StringVar(&in.image, "image", "", "run this image, like nginx:1.27 or ghcr.io/org/app:tag")
	f.StringVar(&in.git, "git", "", "build from this repository, like https://github.com/org/app.git")
	f.StringVar(&in.branch, "branch", "", "the branch to build (default main)")
	f.StringVar(&in.gitSource, "git-source", "", "the Git connection that can read the repository (name or id); none for a public one")
	f.StringVar(&in.builder, "builder", "", "how to build: auto, dockerfile, railpack, nixpacks, buildpacks or static (default auto)")
	f.IntVar(&in.port, "port", 0, "the port the app listens on")
	f.StringVar(&in.command, "command", "", "the start command, instead of the image's own")
	f.Float64Var(&in.cpu, "cpu", 0, "CPU limit in cores, like 0.5 (default unlimited)")
	f.Int64Var(&in.memory, "memory", 0, "memory limit in MB (default unlimited)")
	f.StringArrayVarP(&in.env, "env", "e", nil, "a variable as KEY=VALUE; repeat for more")
	f.BoolVar(&in.asJSON, "json", false, "print the new app as JSON, as the API returns it")
	cmd.MarkFlagsMutuallyExclusive("image", "git")
	return cmd
}

func runAppsCreate(ctx context.Context, s streams, appName string, in appCreate, portGiven bool) error {
	name, pc, client, err := signedIn()
	if err != nil {
		return err
	}
	environment := map[string]string{}
	for _, kv := range in.env {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || strings.TrimSpace(k) == "" {
			return fmt.Errorf("--env takes KEY=VALUE, got %q", kv)
		}
		environment[strings.TrimSpace(k)] = v
	}
	projects, err := projectsOf(ctx, client, in.project)
	if err != nil {
		return again(err, name, pc)
	}
	if len(projects) == 0 {
		return fmt.Errorf("%s has no project yet; create one with `%s projects create`", name, brand.Slug)
	}

	// The project: named, the only one there is, or asked for.
	target := projects[0]
	if in.project == "" && len(projects) > 1 {
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

	source := ""
	switch {
	case in.image != "":
		source = "image"
	case in.git != "":
		source = "git"
	}
	if appName == "" || source == "" {
		if !s.interactive {
			return fmt.Errorf("usage: %s apps create NAME --project P (--image IMAGE | --git REPOSITORY) --port N", brand.Slug)
		}
		var fields []huh.Field
		if appName == "" {
			fields = append(fields, huh.NewInput().Title("App name").
				Description("Lowercase letters, digits and dashes. Unique in "+target.Name+".").
				CharLimit(40).Value(&appName).
				Validate(func(v string) error {
					if !appNameRe.MatchString(strings.TrimSpace(v)) {
						return errors.New("1 to 40 lowercase letters, digits or dashes, not starting or ending with a dash")
					}
					return nil
				}))
		}
		if source == "" {
			source = "image"
			fields = append(fields, huh.NewSelect[string]().Title("Source").
				Options(huh.NewOption("A ready image", "image"), huh.NewOption("A Git repository, built on each deploy", "git")).
				Value(&source))
		}
		if err := s.ask(fields...); err != nil {
			return err
		}
	}

	// What the source needs, asked only when the flags left it out.
	connectionID := ""
	switch source {
	case "image":
		if in.image == "" {
			if err := s.ask(huh.NewInput().Title("Image").Placeholder("nginx:1.27").Value(&in.image).Validate(required("an image"))); err != nil {
				return err
			}
		}
	case "git":
		connections, err := gitConnections(ctx, client)
		if err != nil {
			return again(err, name, pc)
		}
		asked := in.git == ""
		if asked {
			fields := []huh.Field{
				huh.NewInput().Title("Repository").Placeholder("https://github.com/org/app.git").Value(&in.git).Validate(required("a repository URL")),
				huh.NewInput().Title("Branch").Placeholder("main").Value(&in.branch),
			}
			if len(connections) > 0 && in.gitSource == "" {
				options := []huh.Option[string]{huh.NewOption("None: a public repository", "")}
				for _, c := range connections {
					options = append(options, huh.NewOption(c.Name+" ("+c.Type+")", c.ID))
				}
				fields = append(fields, huh.NewSelect[string]().Title("Git connection").
					Description("What can read the repository.").Options(options...).Value(&connectionID))
			}
			if err := s.ask(fields...); err != nil {
				return err
			}
		}
		if in.gitSource != "" {
			for _, c := range connections {
				if c.ID == in.gitSource || strings.EqualFold(c.Name, in.gitSource) {
					connectionID = c.ID
				}
			}
			if connectionID == "" {
				return fmt.Errorf("no Git connection %q on %s; see Git in the panel", in.gitSource, name)
			}
		}
	}
	if !portGiven {
		if !s.interactive {
			return errors.New("which port does the app listen on? add --port N")
		}
		port := ""
		err := s.ask(huh.NewInput().Title("Port").Description("The port the app listens on inside its container.").
			Placeholder("8080").Value(&port).
			Validate(func(v string) error {
				if n, err := strconv.Atoi(strings.TrimSpace(v)); err != nil || n < 1 || n > 65535 {
					return errors.New("a number between 1 and 65535")
				}
				return nil
			}))
		if err != nil {
			return err
		}
		in.port, _ = strconv.Atoi(strings.TrimSpace(port))
	}

	body := map[string]any{"name": appName, "source_type": source, "port": in.port}
	if source == "image" {
		body["image"] = in.image
	} else {
		body["git_repository"] = in.git
		set(body, "git_branch", in.branch)
		set(body, "git_source_id", connectionID)
		set(body, "builder", in.builder)
	}
	set(body, "start_command", in.command)
	if in.cpu > 0 {
		body["cpu_limit"] = in.cpu
	}
	if in.memory > 0 {
		body["memory_limit_mb"] = in.memory
	}
	if len(environment) > 0 {
		body["environment"] = environment
	}
	var raw json.RawMessage
	if err := client.do(ctx, http.MethodPost, "/api/projects/"+url.PathEscape(target.ID)+"/apps", body, &raw); err != nil {
		return again(err, name, pc)
	}
	if in.asJSON {
		_, err := fmt.Fprintln(s.out, string(raw))
		return err
	}
	var a app
	if err := json.Unmarshal(raw, &a); err != nil || a.ID == "" {
		return fmt.Errorf("%s answered with something that is not an app", name)
	}
	s.done("Created app %s in project %s (%s).", s.accent(a.Name), target.Name, a.ID)
	s.say("%s", s.muted("It is not running yet: deploy it from its page on "+pc.URL+"."))
	return nil
}

// set adds a text field to a request body when it has a value: what is left out takes
// the panel's default.
func set(body map[string]any, key, value string) {
	if strings.TrimSpace(value) != "" {
		body[key] = value
	}
}

func required(what string) func(string) error {
	return func(v string) error {
		if strings.TrimSpace(v) == "" {
			return errors.New(what + " is required")
		}
		return nil
	}
}

type gitConnection struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Name string `json:"name"`
}

// gitConnections lists the panel's Git connections. A panel without Git support has none.
func gitConnections(ctx context.Context, client *apiClient) ([]gitConnection, error) {
	var out []gitConnection
	err := client.do(ctx, http.MethodGet, "/api/git-sources", nil, &out)
	var e *apiError
	if errors.As(err, &e) && e.Status != http.StatusUnauthorized {
		return nil, nil
	}
	return out, err
}

func newAppsDeleteCmd(s streams) *cobra.Command {
	var projectRef string
	var yes bool
	cmd := &cobra.Command{
		Use:     "delete APP",
		Aliases: []string{"rm"},
		Short:   "Delete an app, its containers and its addresses",
		Long: "Delete an app: its containers are removed, its addresses stop answering, and its\n" +
			"settings, variables and deployment history are gone. This cannot be undone.\n\n" +
			"In a terminal you confirm by typing the app's name. In a script, --yes is required.\n" +
			"The panel refuses a maintainer: deleting takes an admin or the owner.",
		Example: "  " + brand.Slug + " apps delete web --project Demo\n  " + brand.Slug + " apps delete 01M7K9PCB2SC4HYK1WY4Z7SV2H --yes",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppsDelete(cmd.Context(), s, args[0], projectRef, yes)
		},
	}
	cmd.Flags().StringVarP(&projectRef, "project", "p", "", "the project the app is in (name or id)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "delete without asking")
	return cmd
}

func runAppsDelete(ctx context.Context, s streams, ref, projectRef string, yes bool) error {
	name, pc, client, err := signedIn()
	if err != nil {
		return err
	}
	target, err := findApp(ctx, client, ref, projectRef)
	if err != nil {
		return again(err, name, pc)
	}
	if !yes {
		if !s.interactive {
			return fmt.Errorf("deleting %s cannot be undone; add --yes to delete it without a prompt", target.Name)
		}
		p := s.palette()
		s.say("%s", s.card(
			s.styled(p.danger, true, "Delete app "+target.Name+"?"),
			"",
			"Project  "+target.project.Name,
			"State    "+target.state(),
			"Source   "+target.shortSource(),
			"",
			"Its containers are removed and its addresses stop answering.",
			"Settings, variables and deployment history are gone. No undo.",
		))
		typed := ""
		err := s.ask(huh.NewInput().Title("Type the app's name to delete it").Placeholder(target.Name).Value(&typed).
			Validate(func(v string) error {
				if strings.TrimSpace(v) != target.Name {
					return errors.New("that is not " + target.Name + " (Ctrl+C to keep the app)")
				}
				return nil
			}))
		if err != nil {
			return err
		}
	}
	if err := client.do(ctx, http.MethodDelete, "/api/apps/"+url.PathEscape(target.ID), nil, nil); err != nil {
		return again(err, name, pc)
	}
	s.done("Deleted app %s from project %s.", s.accent(target.Name), target.project.Name)
	return nil
}

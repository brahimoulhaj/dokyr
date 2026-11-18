package deploy

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"panel/internal/builder"
	"panel/internal/store"
)

// projectRef is a reference to a project variable inside an app's value: ${project.NAME}.
var projectRef = regexp.MustCompile(`\$\{project\.([A-Za-z_][A-Za-z0-9_]*)\}`)

// resolveEnvironment builds what a container gets: the project's variables when the project
// lets its apps inherit them, the app's own on top, and every ${project.NAME} replaced by
// that project variable. A reference to a variable the project does not have is an error,
// so a typo fails the deploy instead of starting the app with an empty value.
func resolveEnvironment(app, project map[string]string, inherit bool) (map[string]string, error) {
	out := make(map[string]string, len(app)+len(project))
	if inherit {
		for k, v := range project {
			out[k] = v
		}
	}
	for k, v := range app {
		out[k] = v
	}
	var missing []string
	for k, v := range out {
		if !strings.Contains(v, "${project.") {
			continue
		}
		out[k] = projectRef.ReplaceAllStringFunc(v, func(ref string) string {
			name := projectRef.FindStringSubmatch(ref)[1]
			val, ok := project[name]
			if !ok {
				missing = append(missing, name)
				return ref
			}
			return val
		})
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("the project has no variable %s (referenced as ${project.%s})", strings.Join(missing, ", "), missing[0])
	}
	return out, nil
}

// AppHost is the name an app answers on inside the shared network. Unlike a container's
// name it stays the same from one deployment to the next, so other apps can keep it.
func AppHost(appID string) string { return "app-" + strings.ToLower(appID) }

// appRef is a reference to another app of the project: ${app.NAME.HOST} or ${app.NAME.PORT}.
var appRef = regexp.MustCompile(`\$\{app\.([a-z0-9][a-z0-9-]{0,39})\.([A-Z_]+)\}`)

// resolveAppRefs replaces every ${app.NAME.HOST} and ${app.NAME.PORT} by the internal
// hostname and the port of that app of the project. An unknown app or field is an error.
func resolveAppRefs(env map[string]string, apps []store.App) (map[string]string, error) {
	out := make(map[string]string, len(env))
	for key, value := range env {
		if !strings.Contains(value, "${app.") {
			out[key] = value
			continue
		}
		var refErr error
		out[key] = appRef.ReplaceAllStringFunc(value, func(ref string) string {
			m := appRef.FindStringSubmatch(ref)
			for _, a := range apps {
				if a.Name != m[1] {
					continue
				}
				switch m[2] {
				case "HOST":
					return AppHost(a.ID)
				case "PORT":
					return fmt.Sprint(a.Port)
				}
				refErr = fmt.Errorf("unknown app field in %s: use HOST or PORT", ref)
				return ref
			}
			refErr = fmt.Errorf("app reference %s does not exist in this project", ref)
			return ref
		})
		if refErr != nil {
			return nil, refErr
		}
	}
	return out, nil
}

// buildVars is the part of a resolved environment the image build gets: the variables
// scoped to the build, the secret ones apart so they travel as build secrets. A variable
// the app inherits from its project has no scope of its own and stays out of the build.
func buildVars(env map[string]string, meta map[string]store.EnvVarMeta) builder.Vars {
	var vars builder.Vars
	for name, value := range env {
		m := meta[name]
		switch {
		case !m.AtBuild():
		case m.Secret:
			if vars.Secrets == nil {
				vars.Secrets = map[string]string{}
			}
			vars.Secrets[name] = value
		default:
			if vars.Args == nil {
				vars.Args = map[string]string{}
			}
			vars.Args[name] = value
		}
	}
	return vars
}

// runtimeEnvironment is the part of a resolved environment the container gets: everything
// but the variables scoped to the build alone.
func runtimeEnvironment(env map[string]string, meta map[string]store.EnvVarMeta) map[string]string {
	out := make(map[string]string, len(env))
	for name, value := range env {
		if meta[name].AtRuntime() {
			out[name] = value
		}
	}
	return out
}

// minHidden is the shortest secret value a log hides: shorter ones ("1", "yes") would
// blank out ordinary words.
const minHidden = 4

// secretValues lists what a deployment log must not show: the resolved value of every
// secret variable and, for a value of several lines (a key), each of its lines, as build
// output is logged line by line. Longest first, so a value hides before its own prefix.
func secretValues(env map[string]string, meta map[string]store.EnvVarMeta) []string {
	seen := map[string]bool{}
	var out []string
	add := func(v string, min int) {
		if len(v) >= min && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	for name, value := range env {
		if !meta[name].Secret {
			continue
		}
		add(value, minHidden)
		if strings.Contains(value, "\n") {
			for _, l := range strings.Split(value, "\n") {
				add(strings.TrimRight(l, "\r"), 2*minHidden)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) > len(out[j])
		}
		return out[i] < out[j]
	})
	return out
}

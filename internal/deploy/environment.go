package deploy

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
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

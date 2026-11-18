// Package compose converts a Docker Compose file into apps of the panel: one app for each
// service. The panel does not run Compose; what a service says that an app cannot express
// is left out and reported.
package compose

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"panel/internal/store"
)

// Result is what a Compose file becomes.
type Result struct {
	Apps  []store.AppInput // in the file's order
	Notes []Note           // what differs from the file, for the user to read before creating
	// NoPort names the apps whose service declares no port: a database, a worker. Their
	// port is a placeholder nothing should connect to until a route needs one.
	NoPort map[string]bool
}

// Note kinds.
const (
	Linked  = "linked"  // a variable now points at another app
	Check   = "check"   // converted with a guess or a change worth a look
	Skipped = "skipped" // not converted
)

// Note is one difference between the file and the apps made from it.
type Note struct {
	App  string `json:"app"` // the app it is about; "" = the whole file
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// MaxServices bounds one import.
const MaxServices = 20

var (
	appName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?$`)
	// ${VAR}, ${VAR:-default}, ${VAR-default}, ${VAR:?error}, ${VAR?error}
	interpolation = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?:(:?[-?+])([^}]*))?\}`)
	// Keys that change nothing in the panel: every app restarts on failure, joins the
	// shared network and is named by the panel.
	silent = map[string]bool{"restart": true, "container_name": true, "depends_on": true, "tty": true, "stdin_open": true,
		"hostname": true, "networks": true, "pull_policy": true, "platform": true, "init": true, "stop_grace_period": true}
)

// Convert reads a Compose file. The error is for a file nothing can be made from: not
// YAML, no services, a service built from source. Smaller losses are Notes.
func Convert(file []byte) (Result, error) {
	var doc struct {
		Services yaml.Node            `yaml:"services"`
		Volumes  map[string]yaml.Node `yaml:"volumes"`
		Secrets  yaml.Node            `yaml:"secrets"`
		Configs  yaml.Node            `yaml:"configs"`
	}
	if err := yaml.Unmarshal(file, &doc); err != nil {
		return Result{}, fmt.Errorf("the file is not valid YAML: %s", strings.TrimPrefix(err.Error(), "yaml: "))
	}
	if doc.Services.Kind != yaml.MappingNode || len(doc.Services.Content) == 0 {
		return Result{}, fmt.Errorf("the file has no services")
	}
	if len(doc.Services.Content)/2 > MaxServices {
		return Result{}, fmt.Errorf("at most %d services can be converted at once", MaxServices)
	}
	suffix, err := random(4)
	if err != nil {
		return Result{}, err
	}
	c := &converter{suffix: suffix, external: map[string]string{}, names: map[string]string{}, noPort: map[string]bool{}}
	for name, v := range doc.Volumes {
		var def struct {
			Name     string    `yaml:"name"`
			External yaml.Node `yaml:"external"`
		}
		if v.Decode(&def) == nil && def.External.Kind != 0 && def.External.Value != "false" {
			c.external[name] = name
			if def.Name != "" {
				c.external[name] = def.Name
			}
		}
	}
	if doc.Secrets.Kind != 0 || doc.Configs.Kind != 0 {
		c.note("", Skipped, "secrets and configs. Add them as variables or mounted files.")
	}
	// Names first: a service's variables may point at any other service.
	type service struct {
		name string
		keys map[string]yaml.Node
	}
	var services []service
	for i := 0; i+1 < len(doc.Services.Content); i += 2 {
		raw := doc.Services.Content[i].Value
		name := strings.NewReplacer("_", "-", ".", "-").Replace(strings.ToLower(raw))
		if !appName.MatchString(name) {
			return Result{}, fmt.Errorf("service %s: the name must be 1 to 40 letters, digits or dashes", raw)
		}
		if _, dup := c.names[raw]; dup {
			return Result{}, fmt.Errorf("service %s is defined twice", raw)
		}
		for _, other := range c.names {
			if other == name {
				return Result{}, fmt.Errorf("service %s: another service becomes the same app name %s", raw, name)
			}
		}
		keys := map[string]yaml.Node{}
		if err := doc.Services.Content[i+1].Decode(&keys); err != nil {
			return Result{}, fmt.Errorf("service %s is not a mapping", raw)
		}
		c.names[raw] = name
		services = append(services, service{raw, keys})
	}
	var out Result
	for _, s := range services {
		app, err := c.service(s.name, s.keys)
		if err != nil {
			return Result{}, err
		}
		out.Apps = append(out.Apps, app)
	}
	out.Notes, out.NoPort = c.notes, c.noPort
	return out, nil
}

type converter struct {
	suffix   string            // shared by the volumes of one import, so services keep sharing them
	external map[string]string // external volumes: used under their own name
	names    map[string]string // service name → app name
	notes    []Note
	noPort   map[string]bool
}

func (c *converter) note(app, kind, format string, args ...any) {
	c.notes = append(c.notes, Note{App: app, Kind: kind, Text: fmt.Sprintf(format, args...)})
}

func (c *converter) service(raw string, keys map[string]yaml.Node) (store.AppInput, error) {
	name := c.names[raw]
	in := store.AppInput{Name: name, SourceType: store.SourceImage, Environment: map[string]string{}, Mounts: []store.AppMount{}}
	if _, ok := keys["build"]; ok {
		if _, hasImage := keys["image"]; !hasImage {
			return in, fmt.Errorf("service %s is built from source: create it as a Git app, or give it an image", raw)
		}
		c.note(name, Skipped, "build. The image is pulled instead.")
	}
	var image string
	if n, ok := keys["image"]; !ok || n.Decode(&image) != nil || strings.TrimSpace(image) == "" {
		return in, fmt.Errorf("service %s has no image", raw)
	}
	in.Image = c.interpolate(name, "image", strings.TrimSpace(image))

	in.Port = c.port(name, keys)
	if err := c.environment(name, keys, in.Environment); err != nil {
		return in, fmt.Errorf("service %s: %w", raw, err)
	}
	if err := c.volumes(name, keys, &in); err != nil {
		return in, fmt.Errorf("service %s: %w", raw, err)
	}
	c.command(name, keys, &in)
	c.limits(name, keys, &in)

	var skipped []string
	for key := range keys {
		switch key {
		case "image", "build", "ports", "expose", "environment", "volumes", "command", "deploy", "mem_limit", "cpus":
		default:
			if !silent[key] {
				skipped = append(skipped, key)
			}
		}
	}
	if len(skipped) > 0 {
		sort.Strings(skipped)
		c.note(name, Skipped, "%s", strings.Join(skipped, ", "))
	}
	return in, nil
}

// port is the container port of the first published or exposed port: where the proxy sends
// a route's requests. The host side is dropped, nothing is published on the server. Other
// apps reach any port of the container on its internal hostname, declared or not.
func (c *converter) port(name string, keys map[string]yaml.Node) int {
	for _, key := range []string{"ports", "expose"} {
		n, ok := keys[key]
		if !ok || n.Kind != yaml.SequenceNode {
			continue
		}
		var found []int
		for _, item := range n.Content {
			if item.Kind == yaml.MappingNode {
				var long struct {
					Target int `yaml:"target"`
				}
				if item.Decode(&long) == nil && long.Target > 0 {
					found = append(found, long.Target)
				}
				continue
			}
			// "80", "8080:80", "127.0.0.1:8080:80/tcp", "3000-3005:3000-3005"
			spec := c.interpolate(name, key, item.Value)
			spec, _, _ = strings.Cut(spec[strings.LastIndex(spec, ":")+1:], "/")
			spec, _, _ = strings.Cut(spec, "-")
			if p, err := strconv.Atoi(spec); err == nil && p > 0 && p < 65536 {
				found = append(found, p)
			}
		}
		if len(found) == 0 {
			continue
		}
		return found[0]
	}
	c.noPort[name] = true
	return 80
}

func (c *converter) environment(name string, keys map[string]yaml.Node, env map[string]string) error {
	n, ok := keys["environment"]
	if !ok {
		return nil
	}
	set := func(key string, value *string) {
		key = strings.TrimSpace(key)
		if value == nil {
			c.note(name, Skipped, "%s has no value in the file. Set it in the app.", key)
			return
		}
		env[key] = c.hosts(name, key, c.interpolate(name, key, *value))
	}
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			if v := n.Content[i+1]; v.Kind != yaml.ScalarNode {
				return fmt.Errorf("variable %s must be a single value", n.Content[i].Value)
			} else if v.Tag == "!!null" {
				set(n.Content[i].Value, nil)
			} else {
				set(n.Content[i].Value, &v.Value)
			}
		}
	case yaml.SequenceNode:
		for _, item := range n.Content {
			if key, value, found := strings.Cut(item.Value, "="); found {
				set(key, &value)
			} else {
				set(key, nil)
			}
		}
	default:
		return fmt.Errorf("environment must be a mapping or a list")
	}
	return nil
}

// interpolate applies Compose's ${VAR:-default}: there is no shell here, so a variable is
// its default, or empty with a note.
func (c *converter) interpolate(service, where, value string) string {
	if !strings.Contains(value, "$") {
		return value
	}
	value = strings.ReplaceAll(value, "$$", "\x00")
	value = interpolation.ReplaceAllStringFunc(value, func(ref string) string {
		m := interpolation.FindStringSubmatch(ref)
		if strings.HasSuffix(m[2], "-") {
			return m[3]
		}
		c.note(service, Check, "%s: ${%s} has no value here. Left empty.", where, m[1])
		return ""
	})
	return strings.ReplaceAll(value, "\x00", "$")
}

// hosts points a variable that names another service (db, db:3306, mysql://user@db/app) at
// that app's internal hostname: service names mean nothing outside Compose.
func (c *converter) hosts(service, key, value string) string {
	for raw, app := range c.names {
		re := regexp.MustCompile(`(^|[/@,;=\s])` + regexp.QuoteMeta(raw) + `($|[:/,;\s])`)
		replaced := value
		// Twice: matches that share a separator ("db,db") do not overlap in one pass.
		for range 2 {
			replaced = re.ReplaceAllString(replaced, "${1}$${app."+app+".HOST}${2}")
		}
		if replaced != value {
			c.note(service, Linked, "%s now points at the app %s.", key, app)
			value = replaced
		}
	}
	return value
}

func (c *converter) volumes(name string, keys map[string]yaml.Node, in *store.AppInput) error {
	n, ok := keys["volumes"]
	if !ok {
		return nil
	}
	if n.Kind != yaml.SequenceNode {
		return fmt.Errorf("volumes must be a list")
	}
	for _, item := range n.Content {
		var source, target string
		var readOnly bool
		if item.Kind == yaml.MappingNode {
			var long struct {
				Type     string `yaml:"type"`
				Source   string `yaml:"source"`
				Target   string `yaml:"target"`
				ReadOnly bool   `yaml:"read_only"`
			}
			if err := item.Decode(&long); err != nil {
				return fmt.Errorf("a volume could not be read")
			}
			if long.Type != "" && long.Type != "volume" && long.Type != "bind" {
				c.note(name, Skipped, "%s mount on %s.", long.Type, long.Target)
				continue
			}
			source, target, readOnly = long.Source, long.Target, long.ReadOnly
		} else {
			parts := strings.Split(c.interpolate(name, "volumes", item.Value), ":")
			switch len(parts) {
			case 1:
				target = parts[0]
			case 2:
				source, target = parts[0], parts[1]
			default:
				source, target = parts[0], parts[1]
				readOnly = strings.Contains(parts[2], "ro")
			}
		}
		target = path.Clean(target)
		m := store.AppMount{Target: target, ReadOnly: readOnly}
		switch {
		case source == "":
			m.Type, m.Source = "volume", fmt.Sprintf("%s-%s-%d", name, c.suffix, len(in.Mounts))
		case strings.HasPrefix(source, "/"):
			m.Type, m.Source = "bind", path.Clean(source)
			c.note(name, Check, "%s comes from %s on the server. That folder must exist.", target, m.Source)
		case strings.HasPrefix(source, ".") || strings.HasPrefix(source, "~") || strings.Contains(source, "/"):
			c.note(name, Skipped, "%s on %s. Add it as a mounted file in the app.", source, target)
			continue
		default:
			m.Type = "volume"
			if ext, ok := c.external[source]; ok {
				m.Source = ext
			} else {
				m.Source = source + "-" + c.suffix
			}
		}
		in.Mounts = append(in.Mounts, m)
	}
	return nil
}

// command becomes the start command when it names a program. The panel runs a start
// command in place of the image's entrypoint, so bare arguments for that entrypoint
// ("--appendonly yes") cannot be kept.
func (c *converter) command(name string, keys map[string]yaml.Node, in *store.AppInput) {
	n, ok := keys["command"]
	if !ok || n.Tag == "!!null" {
		return
	}
	var command string
	switch n.Kind {
	case yaml.ScalarNode:
		command = n.Value
	case yaml.SequenceNode:
		words := make([]string, 0, len(n.Content))
		for _, w := range n.Content {
			words = append(words, quote(w.Value))
		}
		command = strings.Join(words, " ")
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return
	}
	if strings.HasPrefix(command, "-") || strings.HasPrefix(command, "'-") {
		c.note(name, Skipped, "command (%s): arguments only. Write the full start command in the app.", command)
		return
	}
	in.StartCommand = command
	c.note(name, Check, "command is now the start command. It replaces the image's entrypoint.")
}

func (c *converter) limits(name string, keys map[string]yaml.Node, in *store.AppInput) {
	var cpus, memory string
	if n, ok := keys["cpus"]; ok {
		cpus = n.Value
	}
	if n, ok := keys["mem_limit"]; ok {
		memory = n.Value
	}
	if n, ok := keys["deploy"]; ok {
		var deploy struct {
			Replicas  *int `yaml:"replicas"`
			Resources struct {
				Limits struct {
					CPUs   string `yaml:"cpus"`
					Memory string `yaml:"memory"`
				} `yaml:"limits"`
			} `yaml:"resources"`
		}
		if n.Decode(&deploy) == nil {
			if deploy.Resources.Limits.CPUs != "" {
				cpus = deploy.Resources.Limits.CPUs
			}
			if deploy.Resources.Limits.Memory != "" {
				memory = deploy.Resources.Limits.Memory
			}
			if deploy.Replicas != nil && *deploy.Replicas != 1 {
				c.note(name, Skipped, "replicas. The app runs one container.")
			}
		}
	}
	if cpus != "" {
		if v, err := strconv.ParseFloat(cpus, 64); err == nil && v > 0 && v <= 256 {
			in.CPULimit = v
		} else {
			c.note(name, Skipped, "CPU limit %s could not be read.", cpus)
		}
	}
	if memory != "" {
		if mb, ok := megabytes(memory); ok {
			in.MemoryMB = mb
		} else {
			c.note(name, Skipped, "memory limit %s could not be read.", memory)
		}
	}
}

// megabytes reads a Compose size ("512m", "1g", "1gb", "268435456") as MiB, at least 6.
func megabytes(s string) (int64, bool) {
	s = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), "b")
	unit := float64(1)
	switch {
	case strings.HasSuffix(s, "k"):
		unit, s = 1<<10, s[:len(s)-1]
	case strings.HasSuffix(s, "m"):
		unit, s = 1<<20, s[:len(s)-1]
	case strings.HasSuffix(s, "g"):
		unit, s = 1<<30, s[:len(s)-1]
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return max(int64(v*unit)>>20, 6), true
}

// quote makes one word of a command list safe for /bin/sh -c.
func quote(word string) string {
	if word != "" && !strings.ContainsAny(word, " \t\n'\"\\$`&|;<>()*?[]{}~#!") {
		return word
	}
	return "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
}

func random(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

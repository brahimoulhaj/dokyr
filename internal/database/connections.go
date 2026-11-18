package database

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"panel/internal/store"
)

// Connection exposes only the scoped application user, never the administrator.
func Connection(d store.DBInstance, creds map[string]string) map[string]string {
	port, scheme := 5432, "postgresql"
	if d.Engine == "mariadb" {
		port, scheme = 3306, "mysql"
	}
	host := ContainerName(d.ID)
	u := url.URL{Scheme: scheme, User: url.UserPassword(d.Username, creds["password"]), Host: net.JoinHostPort(host, strconv.Itoa(port)), Path: "/" + d.DatabaseName}
	if d.Engine == "postgres" {
		u.RawQuery = "sslmode=disable"
	}
	return map[string]string{"HOST": host, "PORT": strconv.Itoa(port), "DATABASE": d.DatabaseName,
		"USERNAME": d.Username, "PASSWORD": creds["password"], "URL": u.String()}
}

// ErrStarting marks a reference to a database that is still being created: waiting a
// little makes it resolve, unlike a stopped or failed one.
var ErrStarting = errors.New("it is still starting")

var dbRef = regexp.MustCompile(`\$\{db\.([a-z][a-z0-9-]{0,47})\.([A-Z_]+)\}`)

// ResolveEnvironment resolves only references actually used, and only inside the
// app's project. Generated values are never re-expanded as templates.
func ResolveEnvironment(ctx context.Context, st *store.Store, projectID string, env map[string]string) (map[string]string, error) {
	needed := false
	for _, value := range env {
		if strings.Contains(value, "${db.") {
			needed = true
			break
		}
	}
	if !needed {
		return env, nil
	}
	list, err := st.ListDBInstances(ctx)
	if err != nil {
		return nil, err
	}
	byName := map[string]store.DBInstance{}
	for _, d := range list {
		if d.ProjectID == projectID {
			byName[d.Name] = d
		}
	}
	cache := map[string]map[string]string{}
	out := make(map[string]string, len(env))
	for key, value := range env {
		var refErr error
		out[key] = dbRef.ReplaceAllStringFunc(value, func(ref string) string {
			if refErr != nil {
				return ref
			}
			match := dbRef.FindStringSubmatch(ref)
			d, ok := byName[match[1]]
			if !ok {
				refErr = fmt.Errorf("database reference %s does not exist in this project", ref)
				return ref
			}
			if d.Status == "provisioning" && d.DesiredState == store.StateRunning {
				refErr = fmt.Errorf("database %s must be running before deploying the app: %w", d.Name, ErrStarting)
				return ref
			}
			if d.Status != "running" || d.DesiredState != store.StateRunning {
				refErr = fmt.Errorf("database %s must be running before deploying the app", d.Name)
				return ref
			}
			fields, ok := cache[d.ID]
			if !ok {
				creds, err := st.DBInstanceCredentials(ctx, d.ID)
				if err != nil {
					refErr = fmt.Errorf("database %s credentials could not be read", d.Name)
					return ref
				}
				fields = Connection(d, creds)
				cache[d.ID] = fields
			}
			val, ok := fields[match[2]]
			if !ok {
				refErr = fmt.Errorf("unknown database connection field in %s", ref)
				return ref
			}
			return val
		})
		if refErr != nil {
			return nil, refErr
		}
		// An invalid database reference must not silently become an app credential.
		if strings.Contains(dbRef.ReplaceAllString(value, ""), "${db.") {
			return nil, fmt.Errorf("invalid database reference in variable %s", key)
		}
	}
	return out, nil
}

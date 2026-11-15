// Package database manages persistent database workloads through the executor seam.
package database

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"panel/internal/executor"
	"panel/internal/store"
)

type Engine struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Versions []string `json:"versions"`
	Port     int      `json:"port"`
}

// Versions are explicit supported lines, never a floating latest tag. Changing an
// instance's major version needs an upgrade workflow and is intentionally unavailable.
func Engines() []Engine {
	return []Engine{
		{"postgres", "PostgreSQL", []string{"18", "17", "16"}, 5432},
		{"mariadb", "MariaDB", []string{"11.8", "11.4", "10.11"}, 3306},
	}
}

type Driver interface {
	Spec(store.DBInstance, map[string]string) executor.ContainerSpec
	Ready(context.Context, executor.CommandExecutor, string, store.DBInstance, map[string]string) error
	Initialize(context.Context, executor.CommandExecutor, string, store.DBInstance, map[string]string) error
}

func engineDriver(engine, version string) (Driver, error) {
	for _, e := range Engines() {
		if e.ID == engine && slices.Contains(e.Versions, version) {
			if engine == "postgres" {
				return postgres{}, nil
			}
			return mariaDB{}, nil
		}
	}
	return nil, fmt.Errorf("%w: choose a supported database engine and version", ErrInvalid)
}

type postgres struct{}

func (postgres) Spec(d store.DBInstance, c map[string]string) executor.ContainerSpec {
	target := "/var/lib/postgresql/data"
	if d.Version == "18" {
		target = "/var/lib/postgresql"
	}
	return executor.ContainerSpec{Image: "postgres:" + d.Version, Env: map[string]string{
		"POSTGRES_PASSWORD": c["root_password"], "POSTGRES_DB": d.DatabaseName,
	}, Mounts: []executor.Mount{{Volume: VolumeName(d.ID), Target: target}}}
}

func (postgres) Ready(ctx context.Context, ex executor.CommandExecutor, cid string, d store.DBInstance, c map[string]string) error {
	return ex.Exec(ctx, cid, executor.Command{
		Args: []string{"psql", "-h", "127.0.0.1", "-U", "postgres", "-d", "postgres", "-v", "ON_ERROR_STOP=1", "-c", "SELECT 1"},
		Env:  map[string]string{"PGPASSWORD": c["root_password"], "PGCONNECT_TIMEOUT": "3"},
	})
}

func (postgres) Initialize(ctx context.Context, ex executor.CommandExecutor, cid string, d store.DBInstance, c map[string]string) error {
	// Replayed after an interrupted provision. Names are validated, then also quoted.
	user := `"` + strings.ReplaceAll(d.Username, `"`, `""`) + `"`
	db := `"` + strings.ReplaceAll(d.DatabaseName, `"`, `""`) + `"`
	literal := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	sql := "DO $$ BEGIN IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = " + literal(d.Username) + ") THEN CREATE ROLE " + user + " LOGIN; END IF; END $$;\n" +
		"ALTER ROLE " + user + " WITH LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD " + literal(c["password"]) + ";\n" +
		"ALTER DATABASE " + db + " OWNER TO " + user + ";\n"
	return ex.Exec(ctx, cid, executor.Command{
		Args: []string{"psql", "-h", "127.0.0.1", "-U", "postgres", "-d", "postgres", "-v", "ON_ERROR_STOP=1"},
		Env:  map[string]string{"PGPASSWORD": c["root_password"], "PGCONNECT_TIMEOUT": "3"}, Input: sql,
	})
}

type mariaDB struct{}

func (mariaDB) Spec(d store.DBInstance, c map[string]string) executor.ContainerSpec {
	return executor.ContainerSpec{Image: "mariadb:" + d.Version, Env: map[string]string{
		"MARIADB_ROOT_HOST":     "localhost",
		"MARIADB_ROOT_PASSWORD": c["root_password"], "MARIADB_DATABASE": d.DatabaseName,
		"MARIADB_USER": d.Username, "MARIADB_PASSWORD": c["password"],
	}, Mounts: []executor.Mount{{Volume: VolumeName(d.ID), Target: "/var/lib/mysql"}}}
}

func (mariaDB) Ready(ctx context.Context, ex executor.CommandExecutor, cid string, d store.DBInstance, c map[string]string) error {
	// Use the application login over TCP, not a temporary initialization server's socket.
	return ex.Exec(ctx, cid, executor.Command{
		Args: []string{"mariadb", "--protocol=tcp", "--host=127.0.0.1", "--connect-timeout=3", "--user=" + d.Username, "--database=" + d.DatabaseName, "-e", "SELECT 1"},
		Env:  map[string]string{"MYSQL_PWD": c["password"]},
	})
}

func (mariaDB) Initialize(context.Context, executor.CommandExecutor, string, store.DBInstance, map[string]string) error {
	return nil // The image creates the database and scoped application user on its first run.
}

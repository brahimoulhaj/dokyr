package database

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"panel/internal/executor"
	"panel/internal/store"
)

// Run on a Docker-enabled machine: go test ./internal/database -run TestRealDatabaseInstances -v
// Each case has an isolated network, a fresh instance, and a volume removed at cleanup.
func TestRealDatabaseInstances(t *testing.T) {
	if testing.Short() {
		t.Skip("needs Docker")
	}
	for _, engine := range []struct{ name, version string }{{"postgres", "18"}, {"postgres", "17"}, {"mariadb", "11.8"}} {
		t.Run(engine.name+engine.version, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
			defer cancel()
			network := fmt.Sprintf("dbtest-%d", time.Now().UnixNano())
			ex, err := executor.NewDocker(executor.Options{Network: network, LabelPrefix: network})
			if err != nil {
				t.Fatal(err)
			}
			ping, stop := context.WithTimeout(ctx, 3*time.Second)
			err = ex.Ping(ping)
			stop()
			if err != nil {
				ex.Close()
				t.Skipf("Docker unavailable: %v", err)
			}
			st, err := store.Open(filepath.Join(t.TempDir(), "test.db"), "")
			if err != nil {
				ex.Close()
				t.Fatal(err)
			}
			if _, err := st.Migrate(ctx); err != nil {
				st.Close()
				ex.Close()
				t.Fatal(err)
			}
			svc := New(ctx, st, ex)
			var did string
			t.Cleanup(func() {
				cancel()
				svc.Wait()
				cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
				defer stop()
				if did != "" {
					_ = ex.RemoveContainer(cleanup, ContainerName(did), true)
					if out, err := exec.CommandContext(cleanup, "docker", "volume", "rm", VolumeName(did)).CombinedOutput(); err != nil {
						t.Errorf("test volume cleanup: %v %s", err, out)
					}
				}
				_ = exec.CommandContext(cleanup, "docker", "network", "rm", network).Run()
				ex.Close()
				st.Close()
			})
			p, err := st.CreateProject(ctx, "test", "")
			if err != nil {
				t.Fatal(err)
			}
			d, err := svc.Create(ctx, input(p, engine.name, engine.version))
			if err != nil {
				t.Fatal(err)
			}
			did = d.ID
			svc.Wait()
			d, err = st.GetDBInstance(ctx, did)
			if err != nil || d.Status != "running" {
				t.Fatalf("provision = %s: %s (%v)", d.Status, d.Error, err)
			}
			creds, _ := st.DBInstanceCredentials(ctx, did)
			runSQL := func(sql string) error {
				cmd := executor.Command{}
				if engine.name == "postgres" {
					cmd.Args = []string{"psql", "-h", "127.0.0.1", "-U", d.Username, "-d", d.DatabaseName, "-v", "ON_ERROR_STOP=1"}
					cmd.Env = map[string]string{"PGPASSWORD": creds["password"], "PGCONNECT_TIMEOUT": "3"}
					cmd.Input = sql
				} else {
					cmd.Args = []string{"mariadb", "--protocol=tcp", "--host=127.0.0.1", "--user=" + d.Username, "--database=" + d.DatabaseName}
					cmd.Env = map[string]string{"MYSQL_PWD": creds["password"]}
					cmd.Input = sql
				}
				return ex.Exec(ctx, ContainerName(did), cmd)
			}
			if err := runSQL("CREATE TABLE persistence_probe (n INTEGER PRIMARY KEY); INSERT INTO persistence_probe VALUES (1);\n"); err != nil {
				t.Fatal("application user cannot write", err)
			}
			if engine.name == "postgres" {
				if err := runSQL("CREATE ROLE forbidden_superuser SUPERUSER;\n"); err == nil {
					t.Fatal("application user has administrator permissions")
				}
			} else if err := runSQL("CREATE USER 'forbidden_user'@'%' IDENTIFIED BY 'test';\n"); err == nil {
				t.Fatal("application user has administrator permissions")
			}
			manage := func(action string, payload map[string]string, want string) {
				t.Helper()
				op, err := svc.Manage(ctx, did, action, payload)
				if err != nil {
					t.Fatal(err)
				}
				svc.Wait()
				op, err = st.GetDBOperation(ctx, op.ID)
				if err != nil || op.Status != want {
					t.Fatalf("%s result = %s: %s (%v)", action, op.Status, op.Error, err)
				}
			}
			manage("database_create", map[string]string{"name": "analytics_data"}, "succeeded")
			manage("user_create", map[string]string{"name": "reporter"}, "succeeded")
			catalog, err := svc.Catalog(ctx, did)
			if err != nil {
				t.Fatal(err)
			}
			var databaseID, userID string
			for _, db := range catalog.Databases {
				if db.Name == "analytics_data" {
					databaseID = db.ID
				}
			}
			for _, user := range catalog.Users {
				if user.Name == "reporter" {
					userID = user.ID
				}
			}
			pass, err := st.DatabaseUserPassword(ctx, did, "reporter")
			if err != nil {
				t.Fatal(err)
			}
			asReporter := func(sql string) error {
				return ex.Exec(ctx, ContainerName(did), userCommand(d, "reporter", pass, "analytics_data", sql))
			}
			if err := ex.Exec(ctx, ContainerName(did), adminCommand(d, creds, "analytics_data", "CREATE TABLE records (n INTEGER PRIMARY KEY); INSERT INTO records VALUES (1);\n")); err != nil {
				t.Fatal("create test data", err)
			}
			grant := func(level, want string) {
				t.Helper()
				manage("grant_update", map[string]string{"user_id": userID, "database_id": databaseID, "access": level}, want)
			}
			// A sync reads the server back: what the panel granted must come back as it is.
			synced := func(want string) {
				t.Helper()
				op, err := svc.Manage(ctx, did, "catalog_sync", nil)
				if err != nil {
					t.Fatal(err)
				}
				svc.Wait()
				if op, err = st.GetDBOperation(ctx, op.ID); err != nil || op.Status != "succeeded" || op.Detail != want {
					t.Fatalf("sync = %s %q (%s, %v), want %q", op.Status, op.Detail, op.Error, err, want)
				}
			}
			synced("nothing to change") // a user without any access yet
			grant("read_only", "succeeded")
			synced("nothing to change")
			if err := asReporter("SELECT * FROM records;\n"); err != nil {
				t.Fatal("read-only cannot select", err)
			}
			if err := asReporter("INSERT INTO records VALUES (2);\n"); err == nil {
				t.Fatal("read-only user can write")
			}
			if err := ex.Exec(ctx, ContainerName(did), userCommand(d, "reporter", "wrong-password", "analytics_data", "SELECT 1;\n")); err == nil {
				t.Fatal("wrong password was accepted")
			}
			if err := ex.Exec(ctx, ContainerName(did), userCommand(d, "reporter", pass, d.DatabaseName, "SELECT 1;\n")); err == nil {
				t.Fatal("user can access a database without a grant")
			}
			if err := ex.Exec(ctx, ContainerName(did), adminCommand(d, creds, "analytics_data", "CREATE TABLE future_records (n INTEGER);\n")); err != nil {
				t.Fatal(err)
			}
			if err := asReporter("SELECT * FROM future_records;\n"); err != nil {
				t.Fatal("future table is not readable", err)
			}
			grant("read_write", "succeeded")
			synced("nothing to change")
			if err := asReporter("INSERT INTO records VALUES (2);\n"); err != nil {
				t.Fatal("read/write user cannot insert", err)
			}
			if err := asReporter("CREATE TABLE forbidden_ddl (n INTEGER);\n"); err == nil {
				t.Fatal("read/write user can create tables")
			}
			grant("none", "succeeded")
			synced("nothing to change")
			if err := asReporter("SELECT * FROM records;\n"); err == nil {
				t.Fatal("revoked user can still read")
			}
			grant("full", "succeeded")
			synced("nothing to change")
			if err := asReporter("CREATE TABLE owned_table (n INTEGER);\n"); err != nil {
				t.Fatal("full user cannot create table", err)
			}
			if engine.name == "postgres" {
				grant("read_only", "failed")
			} else {
				grant("read_only", "succeeded")
			}
			// Made by hand, behind the panel's back: a database, a user that reads it, and
			// the panel's own database dropped.
			admin := func(db, sql string) {
				t.Helper()
				if err := ex.Exec(ctx, ContainerName(did), adminCommand(d, creds, db, sql)); err != nil {
					t.Fatalf("%s: %v", sql, err)
				}
			}
			if engine.name == "postgres" {
				admin("postgres", "CREATE DATABASE by_hand;\nCREATE ROLE outsider LOGIN PASSWORD 'outsider-pass';\n")
				admin("by_hand", "CREATE TABLE t (n INTEGER);\nGRANT SELECT ON ALL TABLES IN SCHEMA public TO outsider;\n")
				admin("postgres", "DROP DATABASE analytics_data WITH (FORCE);\n")
			} else {
				admin("", "CREATE DATABASE by_hand;\nCREATE USER 'outsider'@'%' IDENTIFIED BY 'outsider-pass';\nGRANT SELECT ON `by\\_hand`.* TO 'outsider'@'%';\nDROP DATABASE analytics_data;\n")
			}
			synced("1 databases added, 1 databases removed, 1 users added, 1 permissions updated")
			catalog, err = svc.Catalog(ctx, did)
			if err != nil {
				t.Fatal(err)
			}
			var outsider store.DatabaseUser
			var byHand string
			for _, u := range catalog.Users {
				if u.Name == "outsider" {
					outsider = u
				}
			}
			for _, db := range catalog.Databases {
				if db.Name == "by_hand" {
					byHand = db.ID
				}
				if db.Name == "analytics_data" {
					t.Fatal("a dropped database is still listed")
				}
			}
			found := false
			for _, g := range catalog.Grants {
				if g.UserID == outsider.ID && g.DatabaseID == byHand {
					found = g.Access == "read_only"
				}
			}
			if !outsider.External || byHand == "" || !found {
				t.Fatalf("after the sync: user %+v, database %q, read only %v; grants %+v", outsider, byHand, found, catalog.Grants)
			}
			synced("nothing to change")
			// The panel takes the user over: a rotation gives it a password that works.
			manage("password_rotate", map[string]string{"user_id": outsider.ID}, "succeeded")
			rotated, err := st.DatabaseUserPassword(ctx, did, "outsider")
			if err != nil || rotated == "" {
				t.Fatal("no password after the rotation", err)
			}
			if err := ex.Exec(ctx, ContainerName(did), userCommand(d, "outsider", rotated, "by_hand", "SELECT * FROM t;\n")); engine.name == "postgres" && err != nil {
				t.Fatal("the rotated password does not work", err)
			}
			if err := svc.Action(ctx, did, "restart"); err != nil {
				t.Fatal(err)
			}
			svc.Wait()
			// Recreating the container must reuse the initialized volume and credentials.
			if err := ex.RemoveContainer(ctx, ContainerName(did), true); err != nil {
				t.Fatal(err)
			}
			if err := svc.Recover(ctx); err != nil {
				t.Fatal(err)
			}
			svc.Wait()
			if err := runSQL("INSERT INTO persistence_probe VALUES (1);\n"); err == nil {
				t.Fatal("saved row did not survive container recreation")
			}
			if err := runSQL("INSERT INTO persistence_probe VALUES (2);\n"); err != nil {
				t.Fatal("database did not survive container recreation", err)
			}
		})
	}
}

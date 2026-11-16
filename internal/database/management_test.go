package database

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"panel/internal/executor"
	"panel/internal/store"
)

func TestManageDatabasesUsersAndGrants(t *testing.T) {
	for _, engine := range []struct{ name, version string }{{"postgres", "18"}, {"mariadb", "11.8"}} {
		t.Run(engine.name, func(t *testing.T) {
			svc, h, p := testService(t)
			ctx := context.Background()
			d, err := svc.Create(ctx, input(p, engine.name, engine.version))
			if err != nil {
				t.Fatal(err)
			}
			svc.Wait()
			catalog, err := svc.Catalog(ctx, d.ID)
			if err != nil || len(catalog.Databases) != 1 || len(catalog.Users) != 1 || len(catalog.Grants) != 1 {
				t.Fatalf("initial catalog: %+v %v", catalog, err)
			}
			submit := func(action string, payload map[string]string) store.DBOperation {
				t.Helper()
				op, err := svc.Manage(ctx, d.ID, action, payload)
				if err != nil {
					t.Fatal(err)
				}
				svc.Wait()
				op, err = svc.st.GetDBOperation(ctx, op.ID)
				if err != nil || op.Status != "succeeded" {
					t.Fatalf("operation failed: %+v %v", op, err)
				}
				if got, _ := svc.st.GetDBInstance(ctx, d.ID); got.Status != "running" {
					t.Fatalf("management changed instance lifecycle status: %s %+v", action, got)
				}
				return op
			}
			submit("database_create", map[string]string{"name": "analytics_data"})
			op := submit("user_create", map[string]string{"name": "reporter"})
			password, err := svc.st.DatabaseUserPassword(ctx, d.ID, "reporter")
			if err != nil || len(password) < 40 {
				t.Fatal("generated password missing", err)
			}
			encoded, _ := json.Marshal(op)
			if strings.Contains(string(encoded), password) || strings.Contains(string(encoded), "payload") {
				t.Fatal("job response exposed password")
			}
			catalog, _ = svc.Catalog(ctx, d.ID)
			var db store.ManagedDatabase
			var user store.DatabaseUser
			for _, v := range catalog.Databases {
				if v.Name == "analytics_data" {
					db = v
				}
			}
			for _, v := range catalog.Users {
				if v.Name == "reporter" {
					user = v
				}
			}
			if db.ID == "" || user.ID == "" {
				t.Fatal("new resources missing from catalog")
			}
			for _, level := range []string{"read_only", "read_write", "full", "none"} {
				submit("grant_update", map[string]string{"database_id": db.ID, "user_id": user.ID, "access": level})
				got, _ := svc.Catalog(ctx, d.ID)
				actual := "none"
				for _, g := range got.Grants {
					if g.UserID == user.ID && g.DatabaseID == db.ID {
						actual = g.Access
					}
				}
				if actual != level {
					t.Fatalf("grant = %s want %s", actual, level)
				}
			}
			// Reject foreign resource IDs, reserved names, SQL injection and owner downgrade.
			if _, err := svc.Manage(ctx, d.ID, "grant_update", map[string]string{"database_id": "other", "user_id": user.ID, "access": "full"}); !errors.Is(err, store.ErrNotFound) {
				t.Fatal("foreign ID accepted", err)
			}
			for _, name := range []string{"postgres", "root", "pg_database_owner", "foo;drop", "UPPER", "a.b"} {
				if _, err := svc.Manage(ctx, d.ID, "user_create", map[string]string{"name": name}); !errors.Is(err, ErrInvalid) {
					t.Fatalf("unsafe username accepted: %s %v", name, err)
				}
			}
			if _, err := svc.Manage(ctx, d.ID, "user_create", map[string]string{"name": "reporter"}); !errors.Is(err, store.ErrConflict) {
				t.Fatal("duplicate accepted", err)
			}
			initial, _ := svc.st.GetDatabaseUser(ctx, d.ID, d.Username)
			for _, v := range catalog.Databases {
				if v.Name == d.DatabaseName {
					if _, err := svc.Manage(ctx, d.ID, "grant_update", map[string]string{"database_id": v.ID, "user_id": initial.ID, "access": "none"}); !errors.Is(err, ErrInvalid) {
						t.Fatal("initial access was revocable", err)
					}
				}
			}
			// Rotation applies a new password on the server and stores it, initial user included.
			appPassword, _ := svc.st.DatabaseUserPassword(ctx, d.ID, d.Username)
			for _, u := range []store.DatabaseUser{user, initial} {
				before, _ := svc.st.DatabaseUserPassword(ctx, d.ID, u.Name)
				op := submit("password_rotate", map[string]string{"user_id": u.ID})
				after, _ := svc.st.DatabaseUserPassword(ctx, d.ID, u.Name)
				last := h.commands[len(h.commands)-1].Input
				if after == before || len(after) < 40 || !strings.Contains(last, after) || !strings.Contains(last, "PASSWORD") && !strings.Contains(last, "IDENTIFIED BY") {
					t.Fatalf("rotation for %s not applied", u.Name)
				}
				if encoded, _ := json.Marshal(op); strings.Contains(string(encoded), after) {
					t.Fatal("job response exposed rotated password")
				}
			}
			if creds, _ := svc.st.DBInstanceCredentials(ctx, d.ID); creds["password"] == appPassword {
				t.Fatal("initial user's instance credentials not updated")
			}
			// The initial database and user back ${db.<name>.URL} references.
			for _, v := range catalog.Databases {
				if v.Name == d.DatabaseName {
					if _, err := svc.Manage(ctx, d.ID, "database_delete", map[string]string{"database_id": v.ID}); !errors.Is(err, ErrInvalid) {
						t.Fatal("initial database deletable", err)
					}
				}
			}
			if _, err := svc.Manage(ctx, d.ID, "user_delete", map[string]string{"user_id": initial.ID}); !errors.Is(err, ErrInvalid) {
				t.Fatal("initial user deletable", err)
			}
			submit("grant_update", map[string]string{"database_id": db.ID, "user_id": user.ID, "access": "full"})
			submit("user_delete", map[string]string{"user_id": user.ID})
			if want := "DROP USER IF EXISTS"; engine.name == "postgres" {
				want = "DROP ROLE IF EXISTS"
				if !strings.Contains(h.commands[len(h.commands)-2].Input, "REASSIGN OWNED BY") {
					t.Fatal("postgres user objects not reassigned before drop")
				}
				if !strings.Contains(h.commands[len(h.commands)-1].Input, want) {
					t.Fatal("role not dropped")
				}
			} else if !strings.Contains(h.commands[len(h.commands)-1].Input, want) {
				t.Fatal("user not dropped")
			}
			submit("database_delete", map[string]string{"database_id": db.ID})
			if !strings.Contains(h.commands[len(h.commands)-1].Input, "DROP DATABASE IF EXISTS") {
				t.Fatal("database not dropped")
			}
			catalog, _ = svc.Catalog(ctx, d.ID)
			if len(catalog.Databases) != 1 || len(catalog.Users) != 1 || len(catalog.Grants) != 1 {
				t.Fatalf("catalog after deletion: %+v", catalog)
			}
			if _, err := svc.Manage(ctx, d.ID, "user_delete", map[string]string{"user_id": user.ID}); !errors.Is(err, store.ErrNotFound) {
				t.Fatal("deleted user still manageable", err)
			}
			svc.acquire(d.ID)
			if _, err := svc.Manage(ctx, d.ID, "database_create", map[string]string{"name": "another"}); !errors.Is(err, ErrBusy) {
				t.Fatal("overlapping SQL operation accepted", err)
			}
			svc.release(d.ID)
			// Ensure no command uses an administrator password as an argument.
			creds, _ := svc.st.DBInstanceCredentials(ctx, d.ID)
			for _, cmd := range h.commands {
				if strings.Contains(strings.Join(cmd.Args, " "), creds["root_password"]) {
					t.Fatal("administrator password is in command arguments")
				}
			}
		})
	}
}

func TestPostgresPermissionSQL(t *testing.T) {
	catalog := store.DatabaseCatalog{Users: []store.DatabaseUser{{Name: "app"}, {Name: "reader"}}}
	p := map[string]string{"database": "analytics", "username": "reader", "access": "read_only"}
	sql := grantSQL(store.DBInstance{Engine: "postgres"}, p, catalog)
	for _, want := range []string{`REVOKE CONNECT, TEMPORARY ON DATABASE "analytics" FROM PUBLIC`, `GRANT SELECT ON ALL TABLES IN SCHEMA public TO "reader"`, `ALTER DEFAULT PRIVILEGES FOR ROLE "app" IN SCHEMA public GRANT SELECT`, `REVOKE ALL ON ALL TABLES`, `transfer object ownership before reducing access`, `COMMIT;`} {
		if !strings.Contains(sql, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Contains(sql, "GRANT CREATE") || strings.Contains(sql, "GRANT SELECT, INSERT") {
		t.Fatal("read-only permission includes writes")
	}
	p["access"] = "none"
	sql = grantSQL(store.DBInstance{Engine: "postgres"}, p, catalog)
	if strings.Contains(sql, "GRANT CONNECT") || strings.Contains(sql, " GRANT ") {
		t.Fatal("revocation still grants access")
	}
	p["access"] = "full"
	sql = grantSQL(store.DBInstance{Engine: "postgres"}, p, catalog)
	if strings.Contains(sql, `GRANT CREATE ON DATABASE`) {
		t.Fatal("public-schema management grants arbitrary schema creation")
	}
}

func TestMariaDBPermissionSQL(t *testing.T) {
	p := map[string]string{"database": "analytics_data", "username": "reader", "access": "read_only"}
	sql := grantSQL(store.DBInstance{Engine: "mariadb"}, p, store.DatabaseCatalog{})
	if !strings.Contains(sql, "ON `analytics\\_data`.*") || !strings.Contains(sql, "HANDLER FOR 1141") || !strings.Contains(sql, "GRANT SELECT, SHOW VIEW") || strings.Contains(sql, "ON *.*") {
		t.Fatal("MariaDB grants were not isolated correctly", sql)
	}
}

type failingManagementHost struct{ *testHost }

func (h failingManagementHost) Exec(ctx context.Context, id string, cmd executor.Command) error {
	if strings.Contains(cmd.Input, "REVOKE ALL ON ALL TABLES") {
		return errors.New("SQL contains a private password")
	}
	return h.testHost.Exec(ctx, id, cmd)
}

func TestFailedPermissionDoesNotReportSuccess(t *testing.T) {
	svc, h, p := testService(t)
	ctx := context.Background()
	d, _ := svc.Create(ctx, input(p, "postgres", "18"))
	svc.Wait()
	catalog, _ := svc.Catalog(ctx, d.ID)
	user, err := svc.st.AddDatabaseUser(ctx, d.ID, "reader", "secret", false)
	if err != nil {
		t.Fatal(err)
	}
	svc.ex = failingManagementHost{h}
	op, err := svc.Manage(ctx, d.ID, "grant_update", map[string]string{"database_id": catalog.Databases[0].ID, "user_id": user.ID, "access": "read_only"})
	if err != nil {
		t.Fatal(err)
	}
	svc.Wait()
	op, _ = svc.st.GetDBOperation(ctx, op.ID)
	if op.Status != "failed" || strings.Contains(op.Error, "private password") {
		t.Fatalf("failed job: %+v", op)
	}
	d, _ = svc.st.GetDBInstance(ctx, d.ID)
	if d.Status != "running" {
		t.Fatal("SQL failure marked healthy instance failed")
	}
	catalog, _ = svc.Catalog(ctx, d.ID)
	for _, g := range catalog.Grants {
		if g.UserID == user.ID {
			t.Fatal("failed grant was reported as applied")
		}
	}
}

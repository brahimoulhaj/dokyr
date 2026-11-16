package database

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"path"
	"strings"
	"testing"

	"panel/internal/executor"
	"panel/internal/store"
)

// instanceTestHost dumps "-- dump of <db>" per database, reports PostgreSQL owners
// and records what each restore loaded, by database.
type instanceTestHost struct {
	*testHost
	loaded map[string]string
}

func (h *instanceTestHost) ExecStream(_ context.Context, _ string, c executor.Command, in io.Reader, out io.Writer) error {
	args := strings.Join(c.Args, " ")
	if in != nil {
		raw, e := io.ReadAll(in)
		if strings.Contains(args, " -A -t") {
			_, e = io.WriteString(out, "app\nlegacy\npostgres\npg_database_owner\n")
			return e
		}
		db := ""
		for _, a := range c.Args {
			if strings.HasPrefix(a, "--database=") {
				db = strings.TrimPrefix(a, "--database=")
			}
		}
		for i, a := range c.Args {
			if a == "-d" {
				db = c.Args[i+1]
			}
		}
		h.loaded[db] = string(raw)
		return e
	}
	if strings.Contains(args, "--no-owner") {
		return errors.New("instance dumps must keep owners")
	}
	_, e := io.WriteString(out, "-- dump of "+c.Args[len(c.Args)-1]+"\n")
	return e
}

func TestInstanceBackupAndRestore(t *testing.T) {
	for _, engine := range []struct{ name, version string }{{"postgres", "18"}, {"mariadb", "11.8"}} {
		t.Run(engine.name, func(t *testing.T) {
			s, h, p := testService(t)
			host := &instanceTestHost{testHost: h, loaded: map[string]string{}}
			s.ex = host
			objects := &memoryBackups{data: map[string][]byte{}}
			s.BackupTarget = func(store.S3Input) (BackupObjects, error) { return objects, nil }
			ctx := context.Background()
			d, e := s.Create(ctx, input(p, engine.name, engine.version))
			if e != nil {
				t.Fatal(e)
			}
			s.Wait()
			run := func(op store.DBOperation, e error) store.DBOperation {
				t.Helper()
				if e != nil {
					t.Fatal(e)
				}
				s.Wait()
				op, _ = s.st.GetDBOperation(ctx, op.ID)
				if op.Status != "succeeded" {
					t.Fatalf("operation failed: %+v", op)
				}
				return op
			}
			run(s.Manage(ctx, d.ID, "database_create", map[string]string{"name": "analytics"}))
			run(s.Manage(ctx, d.ID, "user_create", map[string]string{"name": "reporter"}))
			catalog, _ := s.Catalog(ctx, d.ID)
			var analytics, reporter string
			for _, db := range catalog.Databases {
				if db.Name == "analytics" {
					analytics = db.ID
				}
			}
			for _, u := range catalog.Users {
				if u.Name == "reporter" {
					reporter = u.ID
				}
			}
			run(s.Manage(ctx, d.ID, "grant_update", map[string]string{"database_id": analytics, "user_id": reporter, "access": "read_only"}))
			c, _ := s.st.CreateS3Connection(ctx, store.S3Input{S3Connection: store.S3Connection{Name: "s3", Endpoint: "s3.example", Bucket: "b", Prefix: "p"}, SecretKey: "k"})

			run(s.BackupAction(ctx, d.ID, "backup", map[string]string{"scope": "instance", "s3_connection_id": c.ID}))
			backups, _ := s.st.ListDBBackups(ctx, d.ID)
			b := backups[0]
			if b.Scope != "instance" || !b.Ready || len(b.Files) != 3 || path.Base(b.ObjectKey) != "manifest.json" || len(objects.data) != 3 {
				t.Fatalf("instance backup: %+v (%d objects)", b, len(objects.data))
			}
			var m instanceManifest
			if e = json.Unmarshal(objects.data[b.ObjectKey], &m); e != nil || len(m.Databases) != 2 || len(m.Users) != 2 || m.DatabaseName != "app" {
				t.Fatalf("manifest %+v %v", m, e)
			}
			pw, _ := s.st.DatabaseUserPassword(ctx, d.ID, "reporter")
			if strings.Contains(string(objects.data[b.ObjectKey]), pw) {
				t.Fatal("manifest contains a password")
			}
			if engine.name == "postgres" && strings.Join(m.Databases[0].Owners, ",") != "app,legacy" {
				t.Fatalf("owners %v", m.Databases[0].Owners)
			}
			if _, e = s.BackupAction(ctx, d.ID, "restore", map[string]string{"backup_id": b.ID, "name": "copy"}); !errors.Is(e, ErrInvalid) {
				t.Fatal("instance backup restored as a single database", e)
			}

			copyInstance, op, e := s.RestoreInstance(ctx, d.ID, b.ID, store.DBInstance{ProjectID: p.ID, Name: "copy", CPULimit: .5, MemoryMB: 256})
			run(op, e)
			copyInstance, _ = s.st.GetDBInstance(ctx, copyInstance.ID)
			if copyInstance.Status != "running" || copyInstance.Engine != engine.name || copyInstance.DatabaseName != "app" {
				t.Fatalf("restored instance %+v", copyInstance)
			}
			if !strings.Contains(host.loaded["app"], "dump of app") || !strings.Contains(host.loaded["analytics"], "dump of analytics") {
				t.Fatalf("data not loaded: %v", host.loaded)
			}
			got, _ := s.Catalog(ctx, copyInstance.ID)
			granted := false
			for _, g := range got.Grants {
				for _, u := range got.Users {
					if u.ID == g.UserID && u.Name == "reporter" && g.Access == "read_only" {
						granted = true
					}
				}
			}
			if len(got.Databases) != 2 || len(got.Users) != 2 || !granted {
				t.Fatalf("restored catalog %+v", got)
			}
			if newPw, _ := s.st.DatabaseUserPassword(ctx, copyInstance.ID, "reporter"); newPw == pw || len(newPw) < 40 {
				t.Fatal("restored user did not get a new password")
			}
			legacy := false
			for _, cmd := range h.commands {
				legacy = legacy || strings.Contains(cmd.Input, `CREATE ROLE "legacy" NOLOGIN`)
			}
			if legacy != (engine.name == "postgres") {
				t.Fatal("owner role handling", legacy)
			}
			if src, _ := s.st.DBInstanceDatabaseNames(ctx, d.ID); len(src) != 2 {
				t.Fatal("source instance changed")
			}

			if _, e = s.DeleteBackup(ctx, d.ID, b.ID); e != nil || len(objects.data) != 0 {
				t.Fatalf("instance backup objects left: %d %v", len(objects.data), e)
			}
		})
	}
}

func TestInterruptedInstanceRestoreIsNotReplayed(t *testing.T) {
	s, _, p := testService(t)
	ctx := context.Background()
	in := input(p, "postgres", "18")
	d, e := s.st.CreateDBInstance(ctx, in, map[string]string{"root_password": "r", "password": "a"})
	if e != nil {
		t.Fatal(e)
	}
	op, e := s.st.CreateDBOperationPayload(ctx, d, "create", "restore", map[string]string{"restore_backup_id": "b", "restore_source_id": "s", "phase": "restoring"})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.st.StartDBOperation(ctx, op.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.Recover(ctx); e != nil {
		t.Fatal(e)
	}
	s.Wait()
	op, _ = s.st.GetDBOperation(ctx, op.ID)
	d, _ = s.st.GetDBInstance(ctx, d.ID)
	if op.Status != "failed" || !strings.Contains(op.Error, "interrupted") || d.Status != "failed" {
		t.Fatalf("interrupted restore: %+v %+v", op, d)
	}
}

func TestInstanceBackupPolicy(t *testing.T) {
	s, _, p := testService(t)
	ctx := context.Background()
	d, _ := s.Create(ctx, input(p, "postgres", "18"))
	s.Wait()
	c, _ := s.st.CreateS3Connection(ctx, store.S3Input{S3Connection: store.S3Connection{Name: "s3", Endpoint: "s3.example", Bucket: "b"}, SecretKey: "k"})
	pol, e := s.SaveBackupPolicy(ctx, store.BackupPolicy{InstanceID: d.ID, DatabaseName: "ignored", Scope: "instance", S3ConnectionID: c.ID, Enabled: true, Frequency: "daily", Hour: 3, KeepCount: 2})
	if e != nil || pol.Scope != "instance" || pol.DatabaseName != "" {
		t.Fatalf("instance policy %+v %v", pol, e)
	}
	// It coexists with a per-database policy for the initial database.
	if _, e = s.SaveBackupPolicy(ctx, store.BackupPolicy{InstanceID: d.ID, DatabaseName: "app", S3ConnectionID: c.ID, Enabled: true, Frequency: "daily", Hour: 3, KeepCount: 2}); e != nil {
		t.Fatal(e)
	}
	if list, _ := s.st.BackupPolicies(ctx, d.ID); len(list) != 2 {
		t.Fatal("policies", list)
	}
	if _, e = s.SaveBackupPolicy(ctx, store.BackupPolicy{InstanceID: d.ID, Scope: "cluster", S3ConnectionID: c.ID, Frequency: "daily", KeepCount: 2}); !errors.Is(e, ErrInvalid) {
		t.Fatal("unknown scope accepted", e)
	}
}

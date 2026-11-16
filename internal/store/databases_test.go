package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"panel/internal/secret"
)

func TestDatabasePersistence(t *testing.T) {
	s, ctx := open(t), context.Background()
	box, err := secret.LoadOrCreate(filepath.Join(t.TempDir(), "key"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UseSecrets(ctx, box); err != nil {
		t.Fatal(err)
	}
	p, _ := s.CreateProject(ctx, "shop", "")
	in := DBInstance{ProjectID: p.ID, Name: "main", Engine: "postgres", Version: "18", DatabaseName: "app", Username: "app", CPULimit: .5, MemoryMB: 512}
	creds := map[string]string{"root_password": "root-secret", "password": "app-secret"}
	d, err := s.CreateDBInstance(ctx, in, creds)
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != "provisioning" || d.DesiredState != "running" || d.CPULimit != .5 || d.CreatedAt.IsZero() {
		t.Fatalf("instance = %+v", d)
	}
	var raw string
	if err := s.db.QueryRow(`SELECT credentials FROM db_instances WHERE id = ?`, d.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !secret.IsSealed(raw) || strings.Contains(raw, "secret") {
		t.Fatal("database credentials are not encrypted")
	}
	opened, err := s.DBInstanceCredentials(ctx, d.ID)
	if err != nil || opened["password"] != creds["password"] || opened["root_password"] != creds["root_password"] {
		t.Fatal("credential round trip failed", err)
	}
	u, err := s.AddDatabaseUser(ctx, d.ID, "reader", "reader-password", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT credentials FROM db_users WHERE id = ?`, u.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !secret.IsSealed(raw) || strings.Contains(raw, "reader-password") {
		t.Fatal("managed user password is not encrypted")
	}
	op, err := s.CreateDBOperationPayload(ctx, d, "user_create", "reader", map[string]string{"password": "operation-password"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT payload FROM db_operations WHERE id = ?`, op.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !secret.IsSealed(raw) || strings.Contains(raw, "operation-password") {
		t.Fatal("operation password is not encrypted")
	}
	if err := s.FinishDBOperation(ctx, op.ID, ""); err != nil {
		t.Fatal(err)
	}
	in.Name = "MAIN"
	if _, err := s.CreateDBInstance(ctx, in, creds); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate name: %v", err)
	}
	if err := s.DeleteProject(ctx, p.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("project with DB: %v", err)
	}
	if err := s.SetDBInstanceState(ctx, d.ID, "stopped", "stopped", ""); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListDBInstances(ctx)
	if err != nil || len(list) != 1 || list[0].Status != "stopped" {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if err := s.DeleteDBInstance(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DBInstanceCredentials(ctx, d.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted credentials = %v", err)
	}
	if err := s.DeleteProject(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
}

// Rebuilding db_operations must not leave db_backups pointing at a dropped table.
func TestOperationsRebuildKeepsBackupReference(t *testing.T) {
	s := openAt(t, 25)
	ctx := context.Background()
	// An operation with a backup that references it, before 0026 rebuilds db_operations.
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO s3_connections (id, name, endpoint, bucket, access_key, secret_key, created_at, updated_at) VALUES ('s3', 's3', 'e', 'b', 'a', 's', 'now', 'now')`)
	exec(`INSERT INTO db_operations (id, instance_id, project_id, instance_name, engine, version, action, status, created_at, updated_at) VALUES ('op', 'i', 'p', 'n', 'postgres', '18', 'backup', 'succeeded', 'now', 'now')`)
	exec(`INSERT INTO db_backups (id, instance_id, database_name, s3_connection_id, object_key, created_at) VALUES ('op', 'i', 'app', 's3', 'k', 'now')`)
	if _, err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var kept int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM db_operations o JOIN db_backups b ON b.id = o.id`).Scan(&kept); err != nil || kept != 1 {
		t.Fatalf("operation rows lost: %d %v", kept, err)
	}
	exec(`INSERT INTO db_operations (id, instance_id, project_id, instance_name, engine, version, action, status, created_at, updated_at) VALUES ('op2', 'i', 'p', 'n', 'postgres', '18', 'password_rotate', 'succeeded', 'now', 'now')`)
	var schema string
	if err := s.db.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE name='db_backups'`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(schema, "db_operations_old") || !strings.Contains(schema, "REFERENCES db_operations(id)") {
		t.Fatalf("db_backups reference broken: %s", schema)
	}
	rows, err := s.db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign key violations after migration")
	}
}

// Rebuilding backup_policies (0027) must keep scheduled backups linked to their policy.
func TestPolicyRebuildKeepsBackupLinks(t *testing.T) {
	s, ctx := openAt(t, 26), context.Background()
	p, _ := s.CreateProject(ctx, "shop", "")
	d, err := s.CreateDBInstance(ctx, DBInstance{ProjectID: p.ID, Name: "main", Engine: "postgres", Version: "18", DatabaseName: "app", Username: "app", CPULimit: .5, MemoryMB: 512}, map[string]string{"password": "x"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.CreateS3Connection(ctx, S3Input{S3Connection: S3Connection{Name: "s3", Endpoint: "s3.example", Bucket: "b"}, SecretKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	// Seed with SQL: the current store code expects the newer columns.
	policy := BackupPolicy{ID: "policy"}
	op, err := s.CreateDBOperationPayload(ctx, d, "backup", "app", map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO backup_policies (id,instance_id,database_name,s3_connection_id,enabled,frequency,hour,minute,weekday,keep_count,next_run_at,updated_at) VALUES ('policy','` + d.ID + `','app','` + c.ID + `',1,'daily',2,0,0,3,'2026-01-01T00:00:00Z','now')`,
		`INSERT INTO db_backups (id,instance_id,database_name,s3_connection_id,object_key,created_at,policy_id) VALUES ('` + op.ID + `','` + d.ID + `','app','` + c.ID + `','k','now','policy')`,
	} {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	backups, err := s.ListDBBackups(ctx, d.ID)
	if err != nil || len(backups) != 1 || backups[0].PolicyID != policy.ID {
		t.Fatalf("policy link lost: %+v %v", backups, err)
	}
	got, err := s.GetBackupPolicy(ctx, policy.ID)
	if err != nil || got.KeepCount != 3 || got.Frequency != "daily" || got.Scope != "database" || backups[0].Scope != "database" {
		t.Fatalf("policy lost: %+v %v", got, err)
	}
	cron := got
	cron.Frequency, cron.Cron = "cron", ""
	if _, err := s.SaveBackupPolicy(ctx, cron); err == nil {
		t.Fatal("cron policy without an expression stored")
	}
}

// openAt opens a store migrated only up to version, to seed data before an upgrade.
func openAt(t *testing.T, version int) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "test.db"), filepath.Join(dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if _, err := s.migrate(context.Background(), version); err != nil {
		t.Fatal(err)
	}
	return s
}

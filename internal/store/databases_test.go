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

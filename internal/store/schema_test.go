package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// schema.sql is the database as the migrations leave it: what sqlc reads to type the
// queries in queries/. It is written from a migrated database, never by hand:
//
//	UPDATE_SCHEMA=1 go test ./internal/store -run TestSchemaSnapshot
//
// The test fails when a migration changed the schema and the snapshot was not renewed.
func TestSchemaSnapshot(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "schema.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if _, err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT sql FROM sqlite_master
		WHERE sql IS NOT NULL AND name NOT LIKE 'sqlite_%' AND name <> 'schema_migrations'
		ORDER BY CASE type WHEN 'table' THEN 0 WHEN 'index' THEN 1 ELSE 2 END, name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var b strings.Builder
	b.WriteString("-- Generated from the migrations by TestSchemaSnapshot (UPDATE_SCHEMA=1). Do not edit.\n")
	for rows.Next() {
		var stmt string
		if err := rows.Scan(&stmt); err != nil {
			t.Fatal(err)
		}
		b.WriteString("\n" + strings.TrimSpace(stmt) + ";\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	const file = "schema.sql"
	if os.Getenv("UPDATE_SCHEMA") != "" {
		if err := os.WriteFile(file, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	saved, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(saved) != b.String() {
		t.Fatal("schema.sql is not what the migrations make; run: UPDATE_SCHEMA=1 go test ./internal/store -run TestSchemaSnapshot && make sqlc")
	}
}

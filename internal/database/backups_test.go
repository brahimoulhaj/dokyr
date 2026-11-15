package database

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"panel/internal/executor"
	"panel/internal/store"
)

type backupTestHost struct {
	*testHost
	failDump bool
	restored string
}

func (h *backupTestHost) ExecStream(_ context.Context, _ string, c executor.Command, in io.Reader, out io.Writer) error {
	if in != nil {
		raw, e := io.ReadAll(in)
		h.restored = string(raw)
		return e
	}
	if h.failDump {
		return errors.New("secret-password SQL contents")
	}
	_, e := io.WriteString(out, "CREATE TABLE example(id int);\nINSERT INTO example VALUES(42);\n")
	return e
}

type memoryBackups struct {
	data       map[string][]byte
	fail       bool
	failDelete bool
}

func (m *memoryBackups) Upload(_ context.Context, key string, r io.Reader) (int64, error) {
	if m.fail {
		return 0, errors.New("secret S3 error")
	}
	v, e := io.ReadAll(r)
	if e != nil {
		return 0, e
	}
	m.data[key] = v
	return int64(len(v)), nil
}
func (m *memoryBackups) Delete(_ context.Context, key string) error {
	if m.failDelete {
		return errors.New("S3 delete denied")
	}
	delete(m.data, key)
	return nil
}
func (m *memoryBackups) Download(_ context.Context, key string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(m.data[key])), nil
}

func TestBackupAndRestore(t *testing.T) {
	for _, engine := range []string{"postgres", "mariadb"} {
		t.Run(engine, func(t *testing.T) {
			s, h, p := testService(t)
			host := &backupTestHost{testHost: h}
			s.ex = host
			objects := &memoryBackups{data: map[string][]byte{}}
			s.BackupTarget = func(store.S3Input) (BackupObjects, error) { return objects, nil }
			ctx := context.Background()
			version := "18"
			if engine == "mariadb" {
				version = "11.8"
			}
			d, e := s.Create(ctx, input(p, engine, version))
			if e != nil {
				t.Fatal(e)
			}
			s.Wait()
			c, e := s.st.CreateS3Connection(ctx, store.S3Input{S3Connection: store.S3Connection{Name: "backups", Endpoint: "s3.example", Bucket: "bucket", Prefix: "project"}, SecretKey: "private"})
			if e != nil {
				t.Fatal(e)
			}
			in := map[string]string{"database": "app", "s3_connection_id": c.ID}
			op, e := s.BackupAction(ctx, d.ID, "backup", in)
			if e != nil {
				t.Fatal(e)
			}
			s.Wait()
			status, _ := s.st.GetDBOperation(ctx, op.ID)
			if status.Status != "succeeded" {
				t.Fatalf("backup failed: %+v", status)
			}
			backups, e := s.st.ListDBBackups(ctx, d.ID)
			if e != nil || len(backups) != 1 || !backups[0].Ready || backups[0].SizeBytes == 0 {
				t.Fatalf("bad backup: %+v %v", backups, e)
			}
			b := backups[0]
			z, e := gzip.NewReader(bytes.NewReader(objects.data[b.ObjectKey]))
			if e != nil {
				t.Fatal(e)
			}
			raw, _ := io.ReadAll(z)
			z.Close()
			if !strings.Contains(string(raw), "VALUES(42)") {
				t.Fatal("dump lost data")
			}
			if e := s.st.DeleteS3Connection(ctx, c.ID); !errors.Is(e, store.ErrConflict) {
				t.Fatal("backup storage deletion allowed", e)
			}
			if _, e := s.BackupAction(ctx, d.ID, "restore", map[string]string{"backup_id": b.ID, "name": "app"}); !errors.Is(e, store.ErrConflict) {
				t.Fatal("existing target accepted", e)
			}
			restored, e := s.BackupAction(ctx, d.ID, "restore", map[string]string{"backup_id": b.ID, "name": "restored"})
			if e != nil {
				t.Fatal(e)
			}
			s.Wait()
			status, _ = s.st.GetDBOperation(ctx, restored.ID)
			if status.Status != "succeeded" || !strings.Contains(host.restored, "VALUES(42)") {
				t.Fatalf("restore failed %+v", status)
			}
			names, _ := s.st.DBInstanceDatabaseNames(ctx, d.ID)
			if len(names) != 2 {
				t.Fatal("restored database missing", names)
			}
			// Recovery never replays an interrupted restore into a partially imported target.
			current, _ := s.st.GetDBInstance(ctx, d.ID)
			interrupted, e := s.st.CreateDBOperationPayload(ctx, current, "restore", "interrupted", map[string]string{"backup_id": b.ID, "name": "partial"})
			if e != nil {
				t.Fatal(e)
			}
			if e = s.st.StartDBOperation(ctx, interrupted.ID); e != nil {
				t.Fatal(e)
			}
			if e = s.Recover(ctx); e != nil {
				t.Fatal(e)
			}
			s.Wait()
			status, _ = s.st.GetDBOperation(ctx, interrupted.ID)
			if status.Status != "failed" || !strings.Contains(status.Error, "interrupted") {
				t.Fatal("interrupted restore replayed", status)
			}
			// A corrupt object must fail before CREATE DATABASE is issued.
			objects.data[b.ObjectKey] = []byte("broken gzip")
			before := len(h.commands)
			bad, e := s.BackupAction(ctx, d.ID, "restore", map[string]string{"backup_id": b.ID, "name": "corrupt"})
			if e != nil {
				t.Fatal(e)
			}
			s.Wait()
			status, _ = s.st.GetDBOperation(ctx, bad.ID)
			if status.Status != "failed" || len(h.commands) != before {
				t.Fatal("corrupt backup performed SQL", status)
			}
			// Upload and dump failures must not mark the running instance failed.
			for _, failure := range []string{"upload", "dump"} {
				objects.fail = failure == "upload"
				host.failDump = failure == "dump"
				failed, e := s.BackupAction(ctx, d.ID, "backup", in)
				if e != nil {
					t.Fatal(e)
				}
				s.Wait()
				status, _ = s.st.GetDBOperation(ctx, failed.ID)
				current, _ := s.st.GetDBInstance(ctx, d.ID)
				if status.Status != "failed" || current.Status != "running" || strings.Contains(status.Error, "secret") {
					t.Fatalf("unsafe failure %+v %+v", status, current)
				}
			}
		})
	}
}

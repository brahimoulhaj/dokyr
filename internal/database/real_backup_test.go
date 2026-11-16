package database

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"panel/internal/executor"
	"panel/internal/storage"
	"panel/internal/store"
)

// Run on a Docker-enabled machine with rustfs/rustfs:latest pulled:
//
//	go test ./internal/database -run TestRealInstanceBackupRestore -v
//
// Whole-instance backup to real S3, restore into a new instance, then password
// rotation and user/database deletion on the source, and backup deletion.
func TestRealInstanceBackupRestore(t *testing.T) {
	if testing.Short() {
		t.Skip("needs Docker")
	}
	const s3Image = "rustfs/rustfs:latest"
	if out, err := exec.Command("docker", "image", "inspect", s3Image).CombinedOutput(); err != nil {
		t.Skipf("no %s image locally: %s", s3Image, out)
	}
	for _, engine := range []struct{ name, version string }{{"postgres", "18"}, {"mariadb", "11.8"}} {
		t.Run(engine.name+engine.version, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
			defer cancel()
			network := fmt.Sprintf("dbbackup-%d", time.Now().UnixNano())
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
				t.Fatal(err)
			}
			if _, err := st.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			svc := New(ctx, st, ex)
			var instances []string
			s3Name := network + "-s3"
			t.Cleanup(func() {
				cancel()
				svc.Wait()
				c, stop := context.WithTimeout(context.Background(), time.Minute)
				defer stop()
				for _, id := range instances {
					_ = ex.RemoveContainer(c, ContainerName(id), true)
					_ = exec.CommandContext(c, "docker", "volume", "rm", VolumeName(id)).Run()
				}
				_ = exec.CommandContext(c, "docker", "rm", "-f", s3Name).Run()
				_ = exec.CommandContext(c, "docker", "network", "rm", network).Run()
				ex.Close()
				st.Close()
			})

			// S3 on a loopback port: the panel process streams dumps itself.
			l, _ := net.Listen("tcp", "127.0.0.1:0")
			port := l.Addr().(*net.TCPAddr).Port
			l.Close()
			if out, err := exec.CommandContext(ctx, "docker", "run", "-d", "--name", s3Name, "-p", fmt.Sprintf("127.0.0.1:%d:9000", port),
				"-e", "RUSTFS_ACCESS_KEY=panelkey", "-e", "RUSTFS_SECRET_KEY=panelsecret", s3Image).CombinedOutput(); err != nil {
				t.Fatalf("start S3: %v %s", err, out)
			}
			conn := store.S3Input{S3Connection: store.S3Connection{Name: "s3", Endpoint: fmt.Sprintf("127.0.0.1:%d", port), Bucket: "backups", Prefix: "panel", AccessKey: "panelkey", PathStyle: true}, SecretKey: "panelsecret"}
			cl, err := storage.Client(conn)
			if err != nil {
				t.Fatal(err)
			}
			for deadline := time.Now().Add(60 * time.Second); ; {
				if err = cl.MakeBucket(ctx, "backups", minio.MakeBucketOptions{}); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("S3 never came up: %v", err)
				}
				time.Sleep(500 * time.Millisecond)
			}
			s3c, err := st.CreateS3Connection(ctx, conn)
			if err != nil {
				t.Fatal(err)
			}
			objects := func() []string {
				var keys []string
				for o := range cl.ListObjects(ctx, "backups", minio.ListObjectsOptions{Prefix: "panel/", Recursive: true}) {
					keys = append(keys, o.Key)
				}
				return keys
			}

			p, _ := st.CreateProject(ctx, "test", "")
			src, err := svc.Create(ctx, input(p, engine.name, engine.version))
			if err != nil {
				t.Fatal(err)
			}
			instances = append(instances, src.ID)
			svc.Wait()
			if src, _ = st.GetDBInstance(ctx, src.ID); src.Status != "running" {
				t.Fatalf("provision: %s %s", src.Status, src.Error)
			}
			wait := func(op store.DBOperation, err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
				svc.Wait()
				op, _ = st.GetDBOperation(ctx, op.ID)
				if op.Status != "succeeded" {
					t.Fatalf("%s failed: %s", op.Action, op.Error)
				}
			}
			// as runs SQL as a panel user over the network.
			as := func(d store.DBInstance, user, db, sql string) error {
				if _, err := svc.Catalog(ctx, d.ID); err != nil { // seeds the initial user
					return err
				}
				pw, err := st.DatabaseUserPassword(ctx, d.ID, user)
				if err != nil {
					return err
				}
				return ex.Exec(ctx, ContainerName(d.ID), userCommand(d, user, pw, db, sql))
			}
			admin := func(d store.DBInstance, db, sql string) {
				t.Helper()
				c, _ := st.DBInstanceCredentials(ctx, d.ID)
				if err := ex.Exec(ctx, ContainerName(d.ID), adminCommand(d, c, db, sql)); err != nil {
					t.Fatalf("admin SQL on %s: %v", db, err)
				}
			}
			ids := func(d store.DBInstance, user, db string) (string, string) {
				c, _ := svc.Catalog(ctx, d.ID)
				var u, b string
				for _, v := range c.Users {
					if v.Name == user {
						u = v.ID
					}
				}
				for _, v := range c.Databases {
					if v.Name == db {
						b = v.ID
					}
				}
				return u, b
			}

			// Source data: the app owns "items"; reporter owns "events" in analytics.
			if err := as(src, "app", "app", "CREATE TABLE items (n INTEGER PRIMARY KEY); INSERT INTO items VALUES (42);\n"); err != nil {
				t.Fatal(err)
			}
			wait(svc.Manage(ctx, src.ID, "database_create", map[string]string{"name": "analytics"}))
			wait(svc.Manage(ctx, src.ID, "user_create", map[string]string{"name": "reporter"}))
			reporterID, analyticsID := ids(src, "reporter", "analytics")
			wait(svc.Manage(ctx, src.ID, "grant_update", map[string]string{"user_id": reporterID, "database_id": analyticsID, "access": "full"}))
			if err := as(src, "reporter", "analytics", "CREATE TABLE events (n INTEGER PRIMARY KEY); INSERT INTO events VALUES (7);\n"); err != nil {
				t.Fatal(err)
			}
			if engine.name == "postgres" {
				// An owner the panel does not manage must survive as a role without login.
				admin(src, "app", "CREATE ROLE legacy NOLOGIN; CREATE TABLE legacy_data (n INTEGER); ALTER TABLE legacy_data OWNER TO legacy;\n")
			}

			// Whole-instance backup to real S3.
			wait(svc.BackupAction(ctx, src.ID, "backup", map[string]string{"scope": "instance", "s3_connection_id": s3c.ID}))
			backups, _ := st.ListDBBackups(ctx, src.ID)
			b := backups[0]
			if b.Scope != "instance" || !b.Ready || len(objects()) != 3 {
				t.Fatalf("backup %+v objects %v", b, objects())
			}

			// Restore into a new instance.
			restored, op, err := svc.RestoreInstance(ctx, src.ID, b.ID, store.DBInstance{ProjectID: p.ID, Name: "restored", CPULimit: .5, MemoryMB: 512})
			if restored.ID != "" {
				instances = append(instances, restored.ID)
			}
			wait(op, err)
			restored, _ = st.GetDBInstance(ctx, restored.ID)
			if restored.Status != "running" {
				t.Fatalf("restored instance: %s %s", restored.Status, restored.Error)
			}
			// Duplicate keys prove the rows came back; the inserts after prove writes work.
			if err := as(restored, "app", "app", "INSERT INTO items VALUES (42);\n"); err == nil {
				t.Fatal("restored items lost row 42")
			}
			if err := as(restored, "app", "app", "INSERT INTO items VALUES (43); ALTER TABLE items ADD COLUMN note TEXT;\n"); err != nil {
				t.Fatal("restored app cannot write or alter its own table", err)
			}
			if err := as(restored, "reporter", "analytics", "INSERT INTO events VALUES (7);\n"); err == nil {
				t.Fatal("restored events lost row 7")
			}
			if err := as(restored, "reporter", "analytics", "INSERT INTO events VALUES (8);\n"); err != nil {
				t.Fatal("restored reporter cannot write", err)
			}
			srcPw, _ := st.DatabaseUserPassword(ctx, src.ID, "reporter")
			newPw, _ := st.DatabaseUserPassword(ctx, restored.ID, "reporter")
			if srcPw == newPw {
				t.Fatal("restored user kept the old password")
			}
			if engine.name == "postgres" {
				admin(restored, "app", "DO $$ BEGIN IF (SELECT pg_get_userbyid(relowner) FROM pg_class WHERE relname='legacy_data') <> 'legacy' THEN RAISE EXCEPTION 'owner lost'; END IF; END $$;\n")
			}

			// Rotation: the old password stops working, the stored one works.
			wait(svc.Manage(ctx, src.ID, "password_rotate", map[string]string{"user_id": reporterID}))
			if err := ex.Exec(ctx, ContainerName(src.ID), userCommand(src, "reporter", srcPw, "analytics", "SELECT 1;\n")); err == nil {
				t.Fatal("old password still works after rotation")
			}
			if err := as(src, "reporter", "analytics", "SELECT 1;\n"); err != nil {
				t.Fatal("rotated password does not work", err)
			}
			appID, _ := ids(src, "app", "app")
			wait(svc.Manage(ctx, src.ID, "password_rotate", map[string]string{"user_id": appID}))
			if err := as(src, "app", "app", "SELECT 1;\n"); err != nil {
				t.Fatal("rotated initial password does not work", err)
			}

			// Deleting a user that owns objects keeps the data; deleting a database drops it.
			wait(svc.Manage(ctx, src.ID, "user_delete", map[string]string{"user_id": reporterID}))
			if err := ex.Exec(ctx, ContainerName(src.ID), loginCommand(src, "reporter", srcPw)); err == nil {
				t.Fatal("deleted user can still log in")
			}
			admin(src, "analytics", "INSERT INTO events VALUES (9);\n")
			wait(svc.Manage(ctx, src.ID, "database_delete", map[string]string{"database_id": analyticsID}))
			c, _ := st.DBInstanceCredentials(ctx, src.ID)
			if err := ex.Exec(ctx, ContainerName(src.ID), adminCommand(src, c, "analytics", "SELECT 1;\n")); err == nil {
				t.Fatal("deleted database still exists")
			}

			// Deleting the backup removes every object it wrote, and nothing else.
			if _, err := svc.DeleteBackup(ctx, src.ID, b.ID); err != nil {
				t.Fatal(err)
			}
			if left := objects(); len(left) != 0 {
				t.Fatalf("objects left in S3: %s", strings.Join(left, ", "))
			}
		})
	}
}

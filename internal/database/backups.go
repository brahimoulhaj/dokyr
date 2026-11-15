package database

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/minio/minio-go/v7"
	"panel/internal/brand"
	"panel/internal/executor"
	"panel/internal/storage"
	"panel/internal/store"
)

type BackupObjects interface {
	Upload(context.Context, string, io.Reader) (int64, error)
	Download(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
}
type s3BackupObjects struct {
	client *minio.Client
	bucket string
}

func (o s3BackupObjects) Upload(ctx context.Context, key string, r io.Reader) (int64, error) {
	v, e := o.client.PutObject(ctx, o.bucket, key, r, -1, minio.PutObjectOptions{ContentType: "application/gzip"})
	return v.Size, e
}
func (o s3BackupObjects) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	return o.client.GetObject(ctx, o.bucket, key, minio.GetObjectOptions{})
}
func (o s3BackupObjects) Delete(ctx context.Context, key string) error {
	return o.client.RemoveObject(ctx, o.bucket, key, minio.RemoveObjectOptions{})
}
func (s *Service) backupObjects(ctx context.Context, id string) (BackupObjects, error) {
	c, e := s.st.S3Secret(ctx, id)
	if e != nil {
		return nil, e
	}
	if s.BackupTarget != nil {
		return s.BackupTarget(c)
	}
	client, e := storage.Client(c)
	if e != nil {
		return nil, e
	}
	return s3BackupObjects{client, c.Bucket}, nil
}

// BackupAction serializes backups/restores with SQL and lifecycle operations.
func (s *Service) BackupAction(ctx context.Context, id, action string, input map[string]string) (store.DBOperation, error) {
	if action != "backup" && action != "restore" {
		return store.DBOperation{}, ErrInvalid
	}
	if _, ok := s.ex.(executor.StreamExecutor); !ok {
		return store.DBOperation{}, fmt.Errorf("%w: streaming database commands are unavailable", ErrInvalid)
	}
	if !s.acquire(id) {
		return store.DBOperation{}, ErrBusy
	}
	handedOff := false
	defer func() {
		if !handedOff {
			s.release(id)
		}
	}()
	d, e := s.st.GetDBInstance(ctx, id)
	if e != nil {
		return store.DBOperation{}, e
	}
	if d.Status != "running" || d.DesiredState != store.StateRunning || s.Runtime(ctx, id) != "running" {
		return store.DBOperation{}, fmt.Errorf("%w: start the instance first", ErrInvalid)
	}
	names, e := s.st.DBInstanceDatabaseNames(ctx, id)
	if e != nil {
		return store.DBOperation{}, e
	}
	payload := map[string]string{}
	detail := input["database"]
	if action == "backup" {
		found := false
		for _, name := range names {
			if name == input["database"] {
				found = true
			}
		}
		if !found {
			return store.DBOperation{}, store.ErrNotFound
		}
		c, e := s.st.GetS3Connection(ctx, input["s3_connection_id"])
		if e != nil {
			return store.DBOperation{}, e
		}
		payload["database"], payload["s3_connection_id"] = input["database"], c.ID
	} else {
		b, e := s.st.GetDBBackup(ctx, id, input["backup_id"])
		if e != nil {
			return store.DBOperation{}, e
		}
		if !b.Ready || b.Status != "succeeded" {
			return store.DBOperation{}, fmt.Errorf("%w: select a completed backup", ErrInvalid)
		}
		if !validSQLName(input["name"]) {
			return store.DBOperation{}, fmt.Errorf("%w: choose a valid new database name", ErrInvalid)
		}
		for _, name := range names {
			if name == input["name"] {
				return store.DBOperation{}, store.ErrConflict
			}
		}
		payload["backup_id"], payload["name"] = b.ID, input["name"]
		detail = b.DatabaseName + " → " + input["name"]
	}
	op, e := s.st.CreateDBOperationPayload(ctx, d, action, detail, payload)
	if e != nil {
		return op, e
	}
	handedOff = true
	s.launch(op)
	return op, nil
}

func (s *Service) backupOperation(ctx context.Context, op store.DBOperation) error {
	d, e := s.st.GetDBInstance(ctx, op.InstanceID)
	if e != nil {
		return errors.New("The database instance could not be read")
	}
	p, e := s.st.DBOperationPayload(ctx, op.ID)
	if e != nil {
		return errors.New("Backup operation input could not be read")
	}
	credentials, e := s.st.DBInstanceCredentials(ctx, d.ID)
	if e != nil {
		return errors.New("Database credentials could not be read")
	}
	stream, ok := s.ex.(executor.StreamExecutor)
	if !ok {
		return errors.New("Streaming database commands are unavailable")
	}
	if op.Action == "backup" {
		c, e := s.st.GetS3Connection(ctx, p["s3_connection_id"])
		if e != nil {
			return errors.New("The S3 connection could not be read")
		}
		key := path.Join(strings.Trim(c.Prefix, "/"), "database-backups", d.ID, op.ID+".sql.gz")
		b := store.DBBackup{ID: op.ID, InstanceID: d.ID, DatabaseName: p["database"], S3ConnectionID: c.ID, ObjectKey: key, PolicyID: p["policy_id"]}
		if e = s.st.AddDBBackup(ctx, b); e != nil {
			return errors.New("The backup record could not be saved")
		}
		objects, e := s.backupObjects(ctx, c.ID)
		if e != nil {
			return errors.New("The S3 connection is unavailable")
		}
		reader, writer := io.Pipe()
		defer reader.Close()
		dumpCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		done := make(chan error, 1)
		go func() {
			z := gzip.NewWriter(writer)
			err := stream.ExecStream(dumpCtx, ContainerName(d.ID), dumpCommand(d, credentials, p["database"]), nil, z)
			closeErr := z.Close()
			if err == nil {
				err = closeErr
			}
			writer.CloseWithError(err)
			done <- err
		}()
		size, uploadErr := objects.Upload(dumpCtx, key, reader)
		reader.CloseWithError(uploadErr)
		if uploadErr != nil {
			cancel()
		}
		dumpErr := <-done
		if uploadErr != nil || dumpErr != nil {
			return errors.New("Backup dump or S3 upload failed; check storage access and available space, then retry")
		}
		if e = s.st.CompleteDBBackup(ctx, op.ID, size); e != nil {
			return errors.New("Backup uploaded, but its completion could not be saved")
		}
		return nil
	}
	b, e := s.st.GetDBBackup(ctx, d.ID, p["backup_id"])
	if e != nil || !b.Ready {
		return errors.New("The backup is unavailable")
	}
	objects, e := s.backupObjects(ctx, b.S3ConnectionID)
	if e != nil {
		return errors.New("The S3 connection is unavailable")
	}
	source, e := objects.Download(ctx, b.ObjectKey)
	if e != nil {
		return errors.New("The backup could not be downloaded")
	}
	defer source.Close()
	z, e := gzip.NewReader(source)
	if e != nil {
		return errors.New("The backup is not a valid gzip file")
	}
	defer z.Close()
	// Validate the entire gzip (including checksum) before creating a database. Spool
	// with 0600 permissions, a bounded size and automatic cleanup, rather than RAM.
	f, e := os.CreateTemp("", brand.Slug+"-restore-*.sql")
	if e != nil {
		return errors.New("Restore temporary storage is unavailable")
	}
	defer os.Remove(f.Name())
	defer f.Close()
	const limit = int64(20 << 30)
	n, e := io.Copy(f, io.LimitReader(z, limit+1))
	if e != nil || n > limit {
		return errors.New("Backup validation failed or the uncompressed dump exceeds 20 GiB")
	}
	if _, e = f.Seek(0, io.SeekStart); e != nil {
		return errors.New("The restore file could not be read")
	}
	name := p["name"]
	run := func(db, sql string) error {
		return s.ex.Exec(ctx, ContainerName(d.ID), adminCommand(d, credentials, db, sql))
	}
	if e = run("postgres", "CREATE DATABASE "+sqlIdentifier(d.Engine, name)+";\n"); e != nil {
		return errors.New("The new database could not be created; its name may already exist")
	}
	cmd := adminCommand(d, credentials, name, "")
	if d.Engine == "postgres" {
		cmd.Args = append(cmd.Args, "--single-transaction")
	}
	if e = stream.ExecStream(ctx, ContainerName(d.ID), cmd, f, io.Discard); e != nil {
		return errors.New("Restore failed. The new database may contain partial data; inspect it and retry using a different name")
	}
	catalog, e := s.Catalog(ctx, d.ID)
	if e != nil {
		return errors.New("Data restored, but database access could not be configured")
	}
	if e = run(name, grantSQL(d, map[string]string{"database": name, "username": d.Username, "access": "full"}, catalog)); e != nil {
		return errors.New("Data restored, but application access could not be granted")
	}
	db, e := s.st.AddManagedDatabase(ctx, d.ID, name)
	if e != nil {
		return errors.New("Data restored, but its database record could not be saved")
	}
	user, e := s.st.AddDatabaseUser(ctx, d.ID, d.Username, "", true)
	if e != nil {
		return errors.New("Data restored, but its user record could not be saved")
	}
	if e = s.st.SetDatabaseGrant(ctx, user.ID, db.ID, "full"); e != nil {
		return errors.New("Data restored, but its grant record could not be saved")
	}
	return nil
}

func dumpCommand(d store.DBInstance, c map[string]string, name string) executor.Command {
	if d.Engine == "postgres" {
		return executor.Command{Args: []string{"pg_dump", "-h", "127.0.0.1", "-U", "postgres", "--no-owner", "--no-acl", "--no-tablespaces", "--dbname", name}, Env: map[string]string{"PGPASSWORD": c["root_password"]}}
	}
	return executor.Command{Args: []string{"mariadb-dump", "--user=root", "--single-transaction", "--quick", "--routines", "--events", "--triggers", "--skip-add-locks", name}, Env: map[string]string{"MYSQL_PWD": c["root_password"]}}
}

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
		found := input["scope"] == "instance"
		for _, name := range names {
			if name == input["database"] {
				found = true
			}
		}
		if !found {
			return store.DBOperation{}, store.ErrNotFound
		}
		if input["scope"] == "instance" {
			payload["scope"], detail = "instance", "all databases"
		} else {
			payload["database"] = input["database"]
		}
		c, e := s.st.GetS3Connection(ctx, input["s3_connection_id"])
		if e != nil {
			return store.DBOperation{}, e
		}
		payload["s3_connection_id"] = c.ID
	} else {
		// A backup of a deleted instance restores into any instance of its engine.
		source := input["source_id"]
		if source == "" {
			source = id
		}
		s.claimBackup(input["backup_id"])
		defer s.unclaimBackup(input["backup_id"])
		if source != id {
			o, e := s.st.GetOrphanDBBackup(ctx, input["backup_id"])
			if e != nil || o.InstanceID != source {
				return store.DBOperation{}, store.ErrNotFound
			}
			if o.Engine != d.Engine {
				return store.DBOperation{}, fmt.Errorf("%w: this backup needs a %s instance", ErrInvalid, engineName(o.Engine))
			}
			payload["source_id"] = source
		}
		b, e := s.st.GetDBBackup(ctx, source, input["backup_id"])
		if e != nil {
			return store.DBOperation{}, e
		}
		if !b.Ready || b.Status != "succeeded" {
			return store.DBOperation{}, fmt.Errorf("%w: select a completed backup", ErrInvalid)
		}
		if b.Scope == "instance" {
			return store.DBOperation{}, fmt.Errorf("%w: whole-instance backups are restored into a new instance", ErrInvalid)
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
	if op.Action == "backup" {
		c, e := s.st.GetS3Connection(ctx, p["s3_connection_id"])
		if e != nil {
			return errors.New("The S3 connection could not be read")
		}
		if p["scope"] == "instance" {
			return s.backupInstance(ctx, op, d, credentials, p, c)
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
		if e = s.st.AddDBBackupFile(ctx, op.ID, key); e != nil {
			return errors.New("The backup record could not be saved")
		}
		size, e := s.uploadDump(ctx, objects, key, d, dumpCommand(d, credentials, p["database"]))
		if e != nil {
			return e
		}
		if e = s.st.CompleteDBBackup(ctx, op.ID, size); e != nil {
			return errors.New("Backup uploaded, but its completion could not be saved")
		}
		return nil
	}
	source := p["source_id"]
	if source == "" {
		source = d.ID
	}
	b, e := s.st.GetDBBackup(ctx, source, p["backup_id"])
	if e != nil || !b.Ready {
		return errors.New("The backup is unavailable")
	}
	objects, e := s.backupObjects(ctx, b.S3ConnectionID)
	if e != nil {
		return errors.New("The S3 connection is unavailable")
	}
	name := p["name"]
	run := func(db, sql string) error {
		return s.ex.Exec(ctx, ContainerName(d.ID), adminCommand(d, credentials, db, sql))
	}
	// Validate the whole download before creating the database it goes into.
	spool, e := spoolBackup(ctx, objects, b.ObjectKey)
	if e != nil {
		return e
	}
	defer spool.Close()
	if e = run("postgres", "CREATE DATABASE "+sqlIdentifier(d.Engine, name)+";\n"); e != nil {
		return errors.New("The new database could not be created; its name may already exist")
	}
	if e = s.loadSpool(ctx, d, credentials, name, spool); e != nil {
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

// DeleteBackup removes one finished backup (manual or scheduled): exactly its recorded
// S3 object first, then its metadata, so a failed S3 call never orphans an object.
// It holds the instance lock, so it can never race a restore reading the same backup.
func (s *Service) DeleteBackup(ctx context.Context, instanceID, backupID string) (store.DBBackup, error) {
	if !s.acquire(instanceID) {
		return store.DBBackup{}, ErrBusy
	}
	defer s.release(instanceID)
	b, e := s.st.GetDBBackup(ctx, instanceID, backupID)
	if e != nil {
		return b, e
	}
	if b.Status != "succeeded" && b.Status != "failed" {
		return b, ErrBusy
	}
	if s.backupInUse(b.ID) {
		return b, fmt.Errorf("%w: a restore is reading this backup", ErrBusy)
	}
	if e = s.deleteBackupObjects(ctx, b); e != nil {
		return b, fmt.Errorf("%w: the backup could not be removed from S3; check storage access and retry", ErrInvalid)
	}
	return b, s.st.RemovePrunedBackup(ctx, b.ID)
}

// DeleteOrphanBackup deletes a backup whose instance was deleted.
func (s *Service) DeleteOrphanBackup(ctx context.Context, backupID string) (store.OrphanDBBackup, error) {
	o, e := s.st.GetOrphanDBBackup(ctx, backupID)
	if e != nil {
		return o, e
	}
	_, e = s.DeleteBackup(ctx, o.InstanceID, backupID)
	return o, e
}

// uploadDump streams one dump through gzip to S3 without buffering it in memory.
func (s *Service) uploadDump(ctx context.Context, objects BackupObjects, key string, d store.DBInstance, cmd executor.Command) (int64, error) {
	stream, ok := s.ex.(executor.StreamExecutor)
	if !ok {
		return 0, errors.New("Streaming database commands are unavailable")
	}
	reader, writer := io.Pipe()
	defer reader.Close()
	dumpCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		z := gzip.NewWriter(writer)
		err := stream.ExecStream(dumpCtx, ContainerName(d.ID), cmd, nil, z)
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
		return 0, errors.New("Backup dump or S3 upload failed; check storage access and available space, then retry")
	}
	return size, nil
}

// restoreLimit bounds the uncompressed size of one database dump.
const restoreLimit = int64(20 << 30)

// spoolBackup downloads one gzip dump and validates it completely (including the
// checksum) into a 0600 temporary file, before any SQL is issued.
func spoolBackup(ctx context.Context, objects BackupObjects, key string) (*os.File, error) {
	source, e := objects.Download(ctx, key)
	if e != nil {
		return nil, errors.New("The backup could not be downloaded")
	}
	defer source.Close()
	z, e := gzip.NewReader(source)
	if e != nil {
		return nil, errors.New("The backup is not a valid gzip file")
	}
	defer z.Close()
	f, e := os.CreateTemp("", brand.Slug+"-restore-*.sql")
	if e != nil {
		return nil, errors.New("Restore temporary storage is unavailable")
	}
	// Unlinked at once: the open handle keeps the data until Close.
	os.Remove(f.Name())
	n, e := io.Copy(f, io.LimitReader(z, restoreLimit+1))
	if e != nil || n > restoreLimit {
		f.Close()
		return nil, errors.New("Backup validation failed or the uncompressed dump exceeds 20 GiB")
	}
	if _, e = f.Seek(0, io.SeekStart); e != nil {
		f.Close()
		return nil, errors.New("The restore file could not be read")
	}
	return f, nil
}

// loadSpool feeds a validated dump into one database as the administrator.
func (s *Service) loadSpool(ctx context.Context, d store.DBInstance, credentials map[string]string, name string, f io.Reader) error {
	stream, ok := s.ex.(executor.StreamExecutor)
	if !ok {
		return errors.New("Streaming database commands are unavailable")
	}
	cmd := adminCommand(d, credentials, name, "")
	if d.Engine == "postgres" {
		cmd.Args = append(cmd.Args, "--single-transaction")
	}
	return stream.ExecStream(ctx, ContainerName(d.ID), cmd, f, io.Discard)
}

// deleteBackupObjects removes exactly the objects a backup recorded (all dumps and
// the manifest of an instance backup), never listing or clearing a prefix.
func (s *Service) deleteBackupObjects(ctx context.Context, b store.DBBackup) error {
	objects, e := s.backupObjects(ctx, b.S3ConnectionID)
	if e != nil {
		return e
	}
	keys := append([]string{}, b.Files...)
	found := false
	for _, k := range keys {
		found = found || k == b.ObjectKey
	}
	if !found {
		keys = append(keys, b.ObjectKey) // backups made before keys were recorded
	}
	for _, k := range keys {
		if e = objects.Delete(ctx, k); e != nil {
			return e
		}
	}
	return nil
}

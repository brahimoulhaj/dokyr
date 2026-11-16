package database

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"panel/internal/executor"
	"panel/internal/store"
)

// instanceManifest describes a whole-instance backup. It holds names, owners and
// grants, never passwords: a restore generates new credentials.
type instanceManifest struct {
	Format       int                `json:"format"`
	Engine       string             `json:"engine"`
	Version      string             `json:"version"`
	DatabaseName string             `json:"database_name"`
	Username     string             `json:"username"`
	Databases    []manifestDatabase `json:"databases"`
	Users        []manifestUser     `json:"users"`
	Grants       []manifestGrant    `json:"grants"`
}
type manifestDatabase struct {
	Name string `json:"name"`
	File string `json:"file"` // object key relative to the manifest's folder
	// Owners are the PostgreSQL roles owning objects in the dump; roles that are not
	// panel users are recreated without login so ownership can be restored.
	Owners []string `json:"owners,omitempty"`
}
type manifestUser struct {
	Name    string `json:"name"`
	Initial bool   `json:"initial"`
}
type manifestGrant struct {
	User     string `json:"user"`
	Database string `json:"database"`
	Access   string `json:"access"`
}

const manifestFormat = 1

// instanceDumpCommand keeps object owners (PostgreSQL) so a restored application
// can still alter its own tables; privileges are re-applied from the manifest.
func instanceDumpCommand(d store.DBInstance, c map[string]string, name string) executor.Command {
	cmd := dumpCommand(d, c, name)
	if d.Engine == "postgres" {
		args := cmd.Args[:0:0]
		for _, a := range cmd.Args {
			if a != "--no-owner" {
				args = append(args, a)
			}
		}
		cmd.Args = args
	}
	return cmd
}

const pgOwnersSQL = `WITH ns AS (SELECT oid FROM pg_namespace WHERE nspname NOT IN ('pg_catalog','information_schema') AND nspname NOT LIKE 'pg\_toast%' AND nspname NOT LIKE 'pg\_temp%')
SELECT DISTINCT pg_get_userbyid(o) FROM (
 SELECT relowner AS o FROM pg_class WHERE relnamespace IN (SELECT oid FROM ns)
 UNION SELECT nspowner FROM pg_namespace WHERE oid IN (SELECT oid FROM ns)
 UNION SELECT proowner FROM pg_proc WHERE pronamespace IN (SELECT oid FROM ns)
 UNION SELECT typowner FROM pg_type WHERE typnamespace IN (SELECT oid FROM ns)
) x;
`

// pgOwners lists the non-system roles owning objects in one database.
func (s *Service) pgOwners(ctx context.Context, d store.DBInstance, c map[string]string, db string) ([]string, error) {
	stream, ok := s.ex.(executor.StreamExecutor)
	if !ok {
		return nil, errors.New("Streaming database commands are unavailable")
	}
	cmd := adminCommand(d, c, db, "")
	cmd.Args = append(cmd.Args, "-A", "-t")
	var out bytes.Buffer
	if err := stream.ExecStream(ctx, ContainerName(d.ID), cmd, strings.NewReader(pgOwnersSQL), &out); err != nil {
		return nil, err
	}
	owners := []string{}
	for _, line := range strings.Split(out.String(), "\n") {
		role := strings.TrimSpace(line)
		if role == "" || role == "postgres" || strings.HasPrefix(role, "pg_") {
			continue
		}
		owners = append(owners, role)
	}
	return owners, nil
}

// backupInstance writes one gzip dump per managed database, then the manifest.
// Every key is recorded before its upload, so deletion can always find it.
func (s *Service) backupInstance(ctx context.Context, op store.DBOperation, d store.DBInstance, credentials, p map[string]string, c store.S3Connection) error {
	base := path.Join(strings.Trim(c.Prefix, "/"), "database-backups", d.ID, op.ID)
	manifestKey := base + "/manifest.json"
	b := store.DBBackup{ID: op.ID, InstanceID: d.ID, S3ConnectionID: c.ID, ObjectKey: manifestKey, PolicyID: p["policy_id"], Scope: "instance"}
	if e := s.st.AddDBBackup(ctx, b); e != nil {
		return errors.New("The backup record could not be saved")
	}
	objects, e := s.backupObjects(ctx, c.ID)
	if e != nil {
		return errors.New("The S3 connection is unavailable")
	}
	catalog, e := s.Catalog(ctx, d.ID)
	if e != nil {
		return errors.New("The database catalog could not be read")
	}
	m := instanceManifest{Format: manifestFormat, Engine: d.Engine, Version: d.Version, DatabaseName: d.DatabaseName, Username: d.Username}
	users, dbs := map[string]string{}, map[string]string{}
	for _, u := range catalog.Users {
		users[u.ID] = u.Name
		m.Users = append(m.Users, manifestUser{Name: u.Name, Initial: u.Initial})
	}
	for _, db := range catalog.Databases {
		dbs[db.ID] = db.Name
	}
	for _, g := range catalog.Grants {
		m.Grants = append(m.Grants, manifestGrant{User: users[g.UserID], Database: dbs[g.DatabaseID], Access: g.Access})
	}
	var total int64
	for _, db := range catalog.Databases {
		entry := manifestDatabase{Name: db.Name, File: db.Name + ".sql.gz"}
		if d.Engine == "postgres" {
			if entry.Owners, e = s.pgOwners(ctx, d, credentials, db.Name); e != nil {
				return fmt.Errorf("The owners in database %s could not be read", db.Name)
			}
		}
		key := base + "/" + entry.File
		if e = s.st.AddDBBackupFile(ctx, op.ID, key); e != nil {
			return errors.New("The backup record could not be saved")
		}
		size, e := s.uploadDump(ctx, objects, key, d, instanceDumpCommand(d, credentials, db.Name))
		if e != nil {
			return fmt.Errorf("Backup of database %s failed: dump or S3 upload; check storage access and available space, then retry", db.Name)
		}
		total += size
		m.Databases = append(m.Databases, entry)
	}
	raw, _ := json.MarshalIndent(m, "", "  ")
	if e = s.st.AddDBBackupFile(ctx, op.ID, manifestKey); e != nil {
		return errors.New("The backup record could not be saved")
	}
	if _, e = objects.Upload(ctx, manifestKey, bytes.NewReader(raw)); e != nil {
		return errors.New("The backup manifest could not be uploaded to S3")
	}
	if e = s.st.CompleteDBBackup(ctx, op.ID, total+int64(len(raw))); e != nil {
		return errors.New("Backup uploaded, but its completion could not be saved")
	}
	return nil
}

// readManifest downloads and checks a whole-instance backup's manifest.
func readManifest(ctx context.Context, objects BackupObjects, key string) (instanceManifest, error) {
	var m instanceManifest
	r, e := objects.Download(ctx, key)
	if e != nil {
		return m, errors.New("The backup manifest could not be downloaded")
	}
	defer r.Close()
	if e = json.NewDecoder(io.LimitReader(r, 4<<20)).Decode(&m); e != nil || m.Format != manifestFormat {
		return m, errors.New("The backup manifest is missing or unreadable")
	}
	names := map[string]bool{}
	for _, db := range m.Databases {
		// Names come from S3: accept only what the panel itself could have created.
		if !sqlName.MatchString(db.Name) || db.File != db.Name+".sql.gz" || names[db.Name] {
			return m, errors.New("The backup manifest contains an invalid database entry")
		}
		names[db.Name] = true
	}
	for _, u := range m.Users {
		if !sqlName.MatchString(u.Name) {
			return m, errors.New("The backup manifest contains an invalid user name")
		}
	}
	for _, g := range m.Grants {
		if g.Access != "read_only" && g.Access != "read_write" && g.Access != "full" {
			return m, errors.New("The backup manifest contains an invalid permission")
		}
	}
	return m, nil
}

// RestoreInstance creates a new instance from a whole-instance backup. The new
// instance takes engine, version and initial names from the manifest, so it works
// even when the source instance is gone; the source is never modified.
func (s *Service) RestoreInstance(ctx context.Context, sourceID, backupID string, in store.DBInstance) (store.DBInstance, store.DBOperation, error) {
	s.claimBackup(backupID)
	defer s.unclaimBackup(backupID)
	b, e := s.st.GetDBBackup(ctx, sourceID, backupID)
	if e != nil {
		return store.DBInstance{}, store.DBOperation{}, e
	}
	if b.Scope != "instance" || !b.Ready || b.Status != "succeeded" {
		return store.DBInstance{}, store.DBOperation{}, fmt.Errorf("%w: select a completed whole-instance backup", ErrInvalid)
	}
	objects, e := s.backupObjects(ctx, b.S3ConnectionID)
	if e != nil {
		return store.DBInstance{}, store.DBOperation{}, fmt.Errorf("%w: the S3 connection is unavailable", ErrInvalid)
	}
	m, e := readManifest(ctx, objects, b.ObjectKey)
	if e != nil {
		return store.DBInstance{}, store.DBOperation{}, fmt.Errorf("%w: %s", ErrInvalid, e.Error())
	}
	in.Engine, in.Version, in.DatabaseName, in.Username = m.Engine, m.Version, m.DatabaseName, m.Username
	if e = Validate(in); e != nil {
		return store.DBInstance{}, store.DBOperation{}, e
	}
	root, e := password()
	if e != nil {
		return store.DBInstance{}, store.DBOperation{}, e
	}
	app, e := password()
	if e != nil {
		return store.DBInstance{}, store.DBOperation{}, e
	}
	d, e := s.st.CreateDBInstance(ctx, in, map[string]string{"root_password": root, "password": app})
	if e != nil {
		return d, store.DBOperation{}, e
	}
	if !s.acquire(d.ID) {
		return d, store.DBOperation{}, ErrBusy
	}
	op, e := s.st.CreateDBOperationPayload(ctx, d, "create", "restore from backup "+b.ID, map[string]string{"restore_source_id": sourceID, "restore_backup_id": b.ID})
	if e != nil {
		s.release(d.ID)
		return d, op, e
	}
	s.launch(op)
	return d, op, nil
}

// restoreInstance runs after the new instance is provisioned and running: users
// (new passwords), owner roles, databases with their data, then grants.
func (s *Service) restoreInstance(ctx context.Context, d store.DBInstance, p map[string]string) error {
	b, e := s.st.GetDBBackup(ctx, p["restore_source_id"], p["restore_backup_id"])
	if e != nil || !b.Ready {
		return errors.New("Restore failed: the backup is no longer available")
	}
	objects, e := s.backupObjects(ctx, b.S3ConnectionID)
	if e != nil {
		return errors.New("Restore failed: the S3 connection is unavailable")
	}
	m, e := readManifest(ctx, objects, b.ObjectKey)
	if e != nil {
		return errors.New("Restore failed: " + strings.ToLower(e.Error()[:1]) + e.Error()[1:])
	}
	if m.Engine != d.Engine || m.Version != d.Version || m.DatabaseName != d.DatabaseName || m.Username != d.Username {
		return errors.New("Restore failed: the backup does not match this instance")
	}
	credentials, e := s.st.DBInstanceCredentials(ctx, d.ID)
	if e != nil {
		return errors.New("Restore failed: database credentials could not be read")
	}
	run := func(db, sql string) error {
		return s.ex.Exec(ctx, ContainerName(d.ID), adminCommand(d, credentials, db, sql))
	}
	if _, e = s.Catalog(ctx, d.ID); e != nil {
		return errors.New("Restore failed: the database catalog could not be saved")
	}
	known := map[string]bool{d.Username: true}
	for _, u := range m.Users {
		if u.Initial || u.Name == d.Username {
			continue
		}
		secret, e := password()
		if e != nil {
			return e
		}
		sql := "CREATE ROLE " + pgIdentifier(u.Name) + " LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOINHERIT PASSWORD " + sqlLiteral(secret) + ";\n"
		if d.Engine == "mariadb" {
			sql = "CREATE USER " + mariaAccount(u.Name) + " IDENTIFIED BY " + sqlLiteral(secret) + ";\n"
		}
		if e = run("postgres", sql); e != nil {
			return fmt.Errorf("Restore failed: user %s could not be created", u.Name)
		}
		if _, e = s.st.AddDatabaseUser(ctx, d.ID, u.Name, secret, false); e != nil {
			return errors.New("Restore failed: a user record could not be saved")
		}
		known[u.Name] = true
	}
	if d.Engine == "postgres" {
		if len(m.Users) > 1 {
			if e = run("postgres", "REVOKE CONNECT, TEMPORARY ON DATABASE "+pgIdentifier(d.DatabaseName)+" FROM PUBLIC;\n"); e != nil {
				return errors.New("Restore failed: the initial database's access rules could not be configured")
			}
		}
		for _, db := range m.Databases {
			for _, owner := range db.Owners {
				if known[owner] {
					continue
				}
				// Owners outside the panel get a role without login, keeping ownership intact.
				if e = run("postgres", "CREATE ROLE "+pgIdentifier(owner)+" NOLOGIN;\n"); e != nil {
					return fmt.Errorf("Restore failed: owner role %s could not be created", owner)
				}
				known[owner] = true
			}
		}
	}
	for _, db := range m.Databases {
		spool, e := spoolBackup(ctx, objects, path.Join(path.Dir(b.ObjectKey), db.File))
		if e != nil {
			return fmt.Errorf("Restore failed for database %s: %s", db.Name, e.Error())
		}
		if db.Name != d.DatabaseName {
			if e = run("postgres", "CREATE DATABASE "+sqlIdentifier(d.Engine, db.Name)+";\n"); e != nil {
				spool.Close()
				return fmt.Errorf("Restore failed: database %s could not be created", db.Name)
			}
			if d.Engine == "postgres" {
				if e = run(db.Name, "REVOKE ALL ON DATABASE "+pgIdentifier(db.Name)+" FROM PUBLIC;\nREVOKE CREATE ON SCHEMA public FROM PUBLIC;\n"); e != nil {
					spool.Close()
					return fmt.Errorf("Restore failed: access to database %s could not be configured", db.Name)
				}
			}
			if _, e = s.st.AddManagedDatabase(ctx, d.ID, db.Name); e != nil {
				spool.Close()
				return errors.New("Restore failed: a database record could not be saved")
			}
		}
		e = s.loadSpool(ctx, d, credentials, db.Name, spool)
		spool.Close()
		if e != nil {
			return fmt.Errorf("Restore failed while loading database %s; delete this instance and restore again", db.Name)
		}
	}
	catalog, e := s.Catalog(ctx, d.ID)
	if e != nil {
		return errors.New("Restore failed: the database catalog could not be read")
	}
	for _, g := range m.Grants {
		if g.User == d.Username && g.Database == d.DatabaseName {
			continue // the initial user's full access is set up by provisioning
		}
		var user store.DatabaseUser
		var db store.ManagedDatabase
		for _, u := range catalog.Users {
			if u.Name == g.User {
				user = u
			}
		}
		for _, v := range catalog.Databases {
			if v.Name == g.Database {
				db = v
			}
		}
		if user.ID == "" || db.ID == "" {
			continue // a grant for a resource missing from the backup
		}
		if e = run(db.Name, grantSQL(d, map[string]string{"access": g.Access, "username": user.Name, "database": db.Name}, catalog)); e != nil {
			return fmt.Errorf("Restore failed: permissions for %s on %s could not be applied", user.Name, db.Name)
		}
		if e = s.st.SetDatabaseGrant(ctx, user.ID, db.ID, g.Access); e != nil {
			return errors.New("Restore failed: a permission record could not be saved")
		}
	}
	return nil
}

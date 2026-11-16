package database

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"panel/internal/brand"
	"panel/internal/executor"
	"panel/internal/store"
)

func managementAction(action string) bool {
	switch action {
	case "database_create", "user_create", "grant_update", "backup", "restore", "database_delete", "user_delete", "password_rotate":
		return true
	}
	return false
}

func validSQLName(name string) bool {
	if !sqlName.MatchString(name) || strings.HasPrefix(name, "pg_") {
		return false
	}
	switch name {
	case "root", "postgres", "mysql", "sys", "information_schema", "performance_schema", "template0", "template1", "public":
		return false
	}
	return true
}

// Catalog lists resources created through the panel. Existing instances are seeded
// with their original application database/user; administrator accounts stay hidden.
func (s *Service) Catalog(ctx context.Context, id string) (store.DatabaseCatalog, error) {
	d, err := s.st.GetDBInstance(ctx, id)
	if err != nil {
		return store.DatabaseCatalog{}, err
	}
	db, err := s.st.AddManagedDatabase(ctx, id, d.DatabaseName)
	if err != nil {
		return store.DatabaseCatalog{}, err
	}
	user, err := s.st.AddDatabaseUser(ctx, id, d.Username, "", true)
	if err != nil {
		return store.DatabaseCatalog{}, err
	}
	if err := s.st.SetDatabaseGrant(ctx, user.ID, db.ID, "full"); err != nil {
		return store.DatabaseCatalog{}, err
	}
	return s.st.DatabaseCatalog(ctx, id)
}

// Manage validates all references against the selected instance before recording
// encrypted job input. Every SQL mutation shares the lifecycle operation lock.
func (s *Service) Manage(ctx context.Context, id, action string, input map[string]string) (store.DBOperation, error) {
	if !managementAction(action) {
		return store.DBOperation{}, ErrInvalid
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
	d, err := s.st.GetDBInstance(ctx, id)
	if err != nil {
		return store.DBOperation{}, err
	}
	if d.Status != "running" || d.DesiredState != store.StateRunning || s.Runtime(ctx, id) != "running" {
		return store.DBOperation{}, fmt.Errorf("%w: start the instance before managing databases or users", ErrInvalid)
	}
	catalog, err := s.Catalog(ctx, id)
	if err != nil {
		return store.DBOperation{}, err
	}
	payload := map[string]string{}
	detail := input["name"]
	switch action {
	case "database_create", "user_create":
		name := input["name"]
		if !validSQLName(name) {
			return store.DBOperation{}, fmt.Errorf("%w: use a non-reserved name starting with a lowercase letter, followed by lowercase letters, digits or underscores (maximum 48 characters)", ErrInvalid)
		}
		for _, db := range catalog.Databases {
			if action == "database_create" && db.Name == name {
				return store.DBOperation{}, store.ErrConflict
			}
		}
		for _, user := range catalog.Users {
			if action == "user_create" && user.Name == name {
				return store.DBOperation{}, store.ErrConflict
			}
		}
		payload["name"] = name
		if action == "user_create" {
			secret, err := password()
			if err != nil {
				return store.DBOperation{}, err
			}
			payload["password"] = secret
		}
	case "grant_update":
		var db store.ManagedDatabase
		var user store.DatabaseUser
		for _, v := range catalog.Databases {
			if v.ID == input["database_id"] {
				db = v
			}
		}
		for _, v := range catalog.Users {
			if v.ID == input["user_id"] {
				user = v
			}
		}
		if db.ID == "" || user.ID == "" {
			return store.DBOperation{}, store.ErrNotFound
		}
		access := input["access"]
		if access != "none" && access != "read_only" && access != "read_write" && access != "full" {
			return store.DBOperation{}, fmt.Errorf("%w: choose a valid database permission", ErrInvalid)
		}
		if user.Initial && db.Name == d.DatabaseName && access != "full" {
			return store.DBOperation{}, fmt.Errorf("%w: the initial application's database access is protected", ErrInvalid)
		}
		payload["database_id"], payload["user_id"], payload["access"] = db.ID, user.ID, access
		payload["database"], payload["username"] = db.Name, user.Name
		detail = user.Name + " → " + db.Name
	case "database_delete":
		var db store.ManagedDatabase
		for _, v := range catalog.Databases {
			if v.ID == input["database_id"] {
				db = v
			}
		}
		if db.ID == "" {
			return store.DBOperation{}, store.ErrNotFound
		}
		// ${db.<name>.URL} references and the initial user point at this database.
		if db.Name == d.DatabaseName {
			return store.DBOperation{}, fmt.Errorf("%w: the initial database is used by app references and cannot be deleted; delete the instance instead", ErrInvalid)
		}
		payload["name"], detail = db.Name, db.Name
	case "user_delete", "password_rotate":
		var user store.DatabaseUser
		for _, v := range catalog.Users {
			if v.ID == input["user_id"] {
				user = v
			}
		}
		if user.ID == "" {
			return store.DBOperation{}, store.ErrNotFound
		}
		if action == "user_delete" && user.Initial {
			return store.DBOperation{}, fmt.Errorf("%w: the initial user is used by app references and cannot be deleted", ErrInvalid)
		}
		payload["name"], detail = user.Name, user.Name
		if action == "password_rotate" {
			secret, err := password()
			if err != nil {
				return store.DBOperation{}, err
			}
			payload["password"] = secret
		}
	}
	op, err := s.st.CreateDBOperationPayload(ctx, d, action, detail, payload)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			err = ErrBusy
		}
		return op, err
	}
	handedOff = true
	s.launch(op)
	return op, nil
}

func (s *Service) manageOperation(ctx context.Context, op store.DBOperation) error {
	d, err := s.st.GetDBInstance(ctx, op.InstanceID)
	if err != nil {
		return errors.New("the database instance no longer exists")
	}
	c, err := s.st.DBInstanceCredentials(ctx, d.ID)
	if err != nil {
		return errors.New("database credentials could not be read")
	}
	p, err := s.st.DBOperationPayload(ctx, op.ID)
	if err != nil {
		return errors.New("operation input could not be decrypted")
	}
	catalog, err := s.Catalog(ctx, d.ID)
	if err != nil {
		return errors.New("the database catalog could not be read")
	}
	run := func(db, sql string) error { return s.ex.Exec(ctx, ContainerName(d.ID), adminCommand(d, c, db, sql)) }
	switch op.Action {
	case "database_create":
		for _, db := range catalog.Databases {
			if db.Name == p["name"] {
				return nil
			}
		}
		sql := "CREATE DATABASE " + sqlIdentifier(d.Engine, p["name"]) + ";\n"
		if err := run("postgres", sql); err != nil {
			return errors.New("Database creation failed; the name may already exist on the server")
		}
		if d.Engine == "postgres" {
			if err := run(p["name"], "REVOKE ALL ON DATABASE "+pgIdentifier(p["name"])+" FROM PUBLIC;\nREVOKE CREATE ON SCHEMA public FROM PUBLIC;\n"); err != nil {
				return errors.New("Database created, but its initial access could not be configured")
			}
		}
		_, err = s.st.AddManagedDatabase(ctx, d.ID, p["name"])
	case "user_create":
		for _, user := range catalog.Users {
			if user.Name == p["name"] {
				return nil
			}
		}
		if d.Engine == "postgres" {
			if err := run("postgres", "REVOKE CONNECT, TEMPORARY ON DATABASE "+pgIdentifier(d.DatabaseName)+" FROM PUBLIC;\n"); err != nil {
				return errors.New("The initial database's explicit access rules could not be configured")
			}
		}
		marker := brand.Slug + ":user:" + op.ID
		sql := "BEGIN;\nCREATE ROLE " + pgIdentifier(p["name"]) + " LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOINHERIT PASSWORD " + sqlLiteral(p["password"]) + ";\nCOMMENT ON ROLE " + pgIdentifier(p["name"]) + " IS " + sqlLiteral(marker) + ";\nCOMMIT;\n"
		if d.Engine == "mariadb" {
			sql = "CREATE USER " + mariaAccount(p["name"]) + " IDENTIFIED BY " + sqlLiteral(p["password"]) + ";\n"
		}
		if createErr := run("postgres", sql); createErr != nil {
			// PostgreSQL creates the role and a shared-object marker atomically.
			// MariaDB's auto-committing CREATE USER is verified over the private
			// network using this operation's random password. Never replace a user.
			verify := loginCommand(d, p["name"], p["password"])
			if d.Engine == "postgres" {
				verify = adminCommand(d, c, "postgres", "DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname="+sqlLiteral(p["name"])+" AND shobj_description(oid,'pg_authid')="+sqlLiteral(marker)+") THEN RAISE EXCEPTION 'existing role is not owned by this operation'; END IF; END $$;\n")
			}
			if verifyErr := s.ex.Exec(ctx, ContainerName(d.ID), verify); verifyErr != nil {
				return errors.New("User creation failed; the name may already exist on the server. Existing users are never replaced")
			}
		}
		if d.Engine == "postgres" {
			// Include future objects created by the newly managed user in existing grants.
			for _, g := range catalog.Grants {
				var dbName, userName string
				for _, db := range catalog.Databases {
					if db.ID == g.DatabaseID {
						dbName = db.Name
					}
				}
				for _, u := range catalog.Users {
					if u.ID == g.UserID {
						userName = u.Name
					}
				}
				if dbName != "" && userName != "" {
					if e := run(dbName, pgDefaults(p["name"], userName, g.Access)); e != nil {
						return errors.New("User created, but default permissions could not be configured")
					}
				}
			}
		}
		_, err = s.st.AddDatabaseUser(ctx, d.ID, p["name"], p["password"], false)
	case "grant_update":
		if err := run(p["database"], grantSQL(d, p, catalog)); err != nil {
			return errors.New("Permission update failed. Check the instance; a PostgreSQL user owning objects cannot be downgraded until ownership is transferred")
		}
		err = s.st.SetDatabaseGrant(ctx, p["user_id"], p["database_id"], p["access"])
	case "database_delete":
		// IF EXISTS keeps a replay after a crash idempotent; FORCE ends open sessions.
		sql := "DROP DATABASE IF EXISTS " + sqlIdentifier(d.Engine, p["name"]) + " WITH (FORCE);\n"
		if d.Engine == "mariadb" {
			sql = "DROP DATABASE IF EXISTS " + sqlIdentifier(d.Engine, p["name"]) + ";\n"
		}
		if err := run("postgres", sql); err != nil {
			return errors.New("Database deletion failed; check the instance and retry")
		}
		err = s.st.RemoveManagedDatabase(ctx, d.ID, p["name"])
	case "user_delete":
		if d.Engine == "postgres" {
			// Objects the user owns are kept and handed to the administrator, so
			// deleting a user never deletes data; its privileges go with it.
			role := pgIdentifier(p["name"])
			cleanup := "DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname=" + sqlLiteral(p["name"]) + ") THEN EXECUTE 'REASSIGN OWNED BY ' || quote_ident(" + sqlLiteral(p["name"]) + ") || ' TO postgres'; EXECUTE 'DROP OWNED BY ' || quote_ident(" + sqlLiteral(p["name"]) + "); END IF; END $$;\n"
			names := []string{"postgres"}
			for _, db := range catalog.Databases {
				names = append(names, db.Name)
			}
			for _, name := range names {
				if err := run(name, cleanup); err != nil {
					return errors.New("The user's privileges could not be removed from database " + name)
				}
			}
			if err := run("postgres", "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE usename="+sqlLiteral(p["name"])+";\nDROP ROLE IF EXISTS "+role+";\n"); err != nil {
				return errors.New("User deletion failed; check the instance and retry")
			}
		} else if err := run("postgres", "DROP USER IF EXISTS "+mariaAccount(p["name"])+";\n"); err != nil {
			return errors.New("User deletion failed; check the instance and retry")
		}
		err = s.st.RemoveDatabaseUser(ctx, d.ID, p["name"])
	case "password_rotate":
		sql := "ALTER ROLE " + pgIdentifier(p["name"]) + " PASSWORD " + sqlLiteral(p["password"]) + ";\n"
		if d.Engine == "mariadb" {
			sql = "ALTER USER " + mariaAccount(p["name"]) + " IDENTIFIED BY " + sqlLiteral(p["password"]) + ";\n"
		}
		if err := run("postgres", sql); err != nil {
			return errors.New("Password rotation failed; the previous password still works")
		}
		if err = s.st.SetDatabaseUserPassword(ctx, d.ID, p["name"], p["password"]); err != nil {
			// The server already uses the new password: fail loudly, a retry re-applies it.
			return errors.New("The password changed on the server, but could not be saved; retry the rotation now")
		}
	}
	if err != nil {
		return errors.New("The SQL operation completed, but the panel catalog could not be saved")
	}
	return nil
}

func pgIdentifier(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }
func sqlIdentifier(engine, name string) string {
	if engine == "postgres" {
		return pgIdentifier(name)
	}
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}
func sqlLiteral(value string) string  { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
func mariaAccount(name string) string { return sqlLiteral(name) + "@'%'" }

func adminCommand(d store.DBInstance, c map[string]string, db, sql string) executor.Command {
	if d.Engine == "mariadb" {
		args := []string{"mariadb", "--protocol=socket", "--user=root", "--batch"}
		if db != "" && db != "postgres" {
			args = append(args, "--database="+db)
		}
		return executor.Command{Args: args, Env: map[string]string{"MYSQL_PWD": c["root_password"]}, Input: sql}
	}
	return executor.Command{Args: []string{"psql", "-X", "-h", "127.0.0.1", "-U", "postgres", "-d", db, "-v", "ON_ERROR_STOP=1"}, Env: map[string]string{"PGPASSWORD": c["root_password"], "PGCONNECT_TIMEOUT": "3"}, Input: sql}
}

func loginCommand(d store.DBInstance, user, password string) executor.Command {
	db := "postgres"
	if d.Engine == "mariadb" {
		db = ""
	}
	return userCommand(d, user, password, db, "SELECT 1;\n")
}

func userCommand(d store.DBInstance, user, password, db, sql string) executor.Command {
	if d.Engine == "mariadb" {
		args := []string{"mariadb", "--protocol=tcp", "--host=" + ContainerName(d.ID), "--connect-timeout=3", "--user=" + user}
		if db != "" {
			args = append(args, "--database="+db)
		}
		return executor.Command{Args: args, Env: map[string]string{"MYSQL_PWD": password}, Input: sql}
	}
	return executor.Command{Args: []string{"psql", "-X", "-h", ContainerName(d.ID), "-U", user, "-d", db, "-v", "ON_ERROR_STOP=1"}, Env: map[string]string{"PGPASSWORD": password, "PGCONNECT_TIMEOUT": "3"}, Input: sql}
}

func pgDefaults(creator, user, access string) string {
	prefix := "ALTER DEFAULT PRIVILEGES FOR ROLE " + pgIdentifier(creator) + " IN SCHEMA public "
	role := pgIdentifier(user)
	sql := prefix + "REVOKE ALL ON TABLES FROM " + role + ";\n" + prefix + "REVOKE ALL ON SEQUENCES FROM " + role + ";\n"
	if access == "none" {
		return sql
	}
	table, seq := "SELECT", "SELECT"
	if access == "read_write" {
		table = "SELECT, INSERT, UPDATE, DELETE"
		seq = "USAGE, SELECT, UPDATE"
	}
	if access == "full" {
		table = "ALL"
		seq = "ALL"
	}
	return sql + prefix + "GRANT " + table + " ON TABLES TO " + role + ";\n" + prefix + "GRANT " + seq + " ON SEQUENCES TO " + role + ";\n"
}

func grantSQL(d store.DBInstance, p map[string]string, catalog store.DatabaseCatalog) string {
	access, user, db := p["access"], p["username"], p["database"]
	if d.Engine == "mariadb" {
		// Underscores are wildcards at MariaDB's database GRANT level, even when
		// quoted. Escape them, and ignore only the absent-grant error on replay.
		target := sqlIdentifier("mariadb", strings.ReplaceAll(db, "_", `\_`)) + ".*"
		sql := "DELIMITER //\nBEGIN NOT ATOMIC\n DECLARE CONTINUE HANDLER FOR 1141 BEGIN END;\n REVOKE ALL PRIVILEGES ON " + target + " FROM " + mariaAccount(user) + ";\nEND//\nDELIMITER ;\n"
		if access == "none" {
			return sql
		}
		privileges := "SELECT, SHOW VIEW"
		if access == "read_write" {
			privileges = "SELECT, INSERT, UPDATE, DELETE, SHOW VIEW"
		}
		if access == "full" {
			privileges = "ALL PRIVILEGES"
		}
		return sql + "GRANT " + privileges + " ON " + target + " TO " + mariaAccount(user) + ";\n"
	}
	role, database := pgIdentifier(user), pgIdentifier(db)
	sql := "BEGIN;\n"
	if access != "full" {
		sql += "DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_class WHERE relowner = (SELECT oid FROM pg_roles WHERE rolname=" + sqlLiteral(user) + ") AND relnamespace='public'::regnamespace) OR EXISTS (SELECT 1 FROM pg_proc WHERE proowner=(SELECT oid FROM pg_roles WHERE rolname=" + sqlLiteral(user) + ") AND pronamespace='public'::regnamespace) OR EXISTS (SELECT 1 FROM pg_type WHERE typowner=(SELECT oid FROM pg_roles WHERE rolname=" + sqlLiteral(user) + ") AND typnamespace='public'::regnamespace) THEN RAISE EXCEPTION 'transfer object ownership before reducing access'; END IF; END $$;\n"
	}
	sql += "REVOKE CONNECT, TEMPORARY ON DATABASE " + database + " FROM PUBLIC;\nREVOKE ALL ON DATABASE " + database + " FROM " + role + ";\nREVOKE ALL ON SCHEMA public FROM " + role + ";\nREVOKE ALL ON ALL TABLES IN SCHEMA public FROM " + role + ";\nREVOKE ALL ON ALL SEQUENCES IN SCHEMA public FROM " + role + ";\n"
	creators := []string{"postgres"}
	for _, u := range catalog.Users {
		creators = append(creators, u.Name)
	}
	for _, creator := range creators {
		sql += pgDefaults(creator, user, access)
	}
	if access != "none" {
		sql += "GRANT CONNECT ON DATABASE " + database + " TO " + role + ";\nGRANT USAGE ON SCHEMA public TO " + role + ";\n"
		table, seq := "SELECT", "SELECT"
		if access == "read_write" {
			table = "SELECT, INSERT, UPDATE, DELETE"
			seq = "USAGE, SELECT, UPDATE"
		}
		if access == "full" {
			table = "ALL"
			seq = "ALL"
			sql += "GRANT CREATE ON SCHEMA public TO " + role + ";\n"
		}
		sql += "GRANT " + table + " ON ALL TABLES IN SCHEMA public TO " + role + ";\nGRANT " + seq + " ON ALL SEQUENCES IN SCHEMA public TO " + role + ";\n"
	}
	return sql + "COMMIT;\n"
}

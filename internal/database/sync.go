package database

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"panel/internal/executor"
	"panel/internal/store"
)

// Catalog sync: the panel's lists of an instance's databases, users and permissions are
// a mirror of what it did itself. Someone working in Adminer or over SQL makes the two
// drift. A sync reads the server and makes the mirror say what is there: it adds what is
// missing, forgets what is gone and sets each permission to the level the server grants.
// It never changes anything on the server.

// serverName is a name the panel takes over from the server: what it can show, quote and
// pass back safely. Others are counted and left out.
var serverName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_$.-]{0,62}$`)

func importableName(name string) bool {
	if !serverName.MatchString(name) || strings.HasPrefix(name, "pg_") {
		return false
	}
	switch strings.ToLower(name) {
	case "root", "postgres", "mysql", "sys", "information_schema", "performance_schema", "template0", "template1",
		"mariadb.sys", "healthcheck", "public":
		return false
	}
	return true
}

// serverCatalog is what an instance holds, as read from it.
type serverCatalog struct {
	databases []string
	users     []string
	access    map[[2]string]string // user, database → read_only, read_write or full
	skipped   int                  // names the panel cannot take over
}

// SyncResult counts what a sync changed in the panel's lists.
type SyncResult struct {
	DatabasesAdded, DatabasesRemoved int
	UsersAdded, UsersRemoved         int
	GrantsChanged                    int
	Skipped                          int
}

// Changed tells whether the sync found any difference.
func (r SyncResult) Changed() bool {
	return r.DatabasesAdded+r.DatabasesRemoved+r.UsersAdded+r.UsersRemoved+r.GrantsChanged > 0
}

func (r SyncResult) String() string {
	if !r.Changed() && r.Skipped == 0 {
		return "nothing to change"
	}
	var parts []string
	add := func(n int, what string) {
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, what))
		}
	}
	add(r.DatabasesAdded, "databases added")
	add(r.DatabasesRemoved, "databases removed")
	add(r.UsersAdded, "users added")
	add(r.UsersRemoved, "users removed")
	add(r.GrantsChanged, "permissions updated")
	add(r.Skipped, "names skipped")
	return strings.Join(parts, ", ")
}

// queryCommand runs a read-only statement as the administrator and prints its rows as
// tab-separated lines, nothing else.
func queryCommand(d store.DBInstance, c map[string]string, db, sql string) executor.Command {
	cmd := adminCommand(d, c, db, sql)
	if d.Engine == "mariadb" {
		cmd.Args = append(cmd.Args, "--skip-column-names")
	} else {
		cmd.Args = append(cmd.Args, "-A", "-t", "-F", "\t")
	}
	return cmd
}

func rows(out string) [][]string {
	var all [][]string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		all = append(all, strings.Split(line, "\t"))
	}
	return all
}

const pgLogins = `rolcanlogin AND NOT rolsuper AND rolname !~ '^pg_' AND rolname <> 'postgres'`

// pgAccessSQL prints, for the database it runs in, each login role with what it may do
// there: connect, create in the public schema (or own the database), write and read
// tables — now, or by default for tables made later.
const pgAccessSQL = `SELECT r.rolname,
 has_database_privilege(r.oid, current_database(), 'CONNECT'),
 has_schema_privilege(r.oid, 'public', 'CREATE') OR EXISTS (SELECT 1 FROM pg_database WHERE datname = current_database() AND datdba = r.oid),
 EXISTS (SELECT 1 FROM pg_class c WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r','p') AND (has_table_privilege(r.oid, c.oid, 'INSERT') OR has_table_privilege(r.oid, c.oid, 'UPDATE') OR has_table_privilege(r.oid, c.oid, 'DELETE')))
  OR EXISTS (SELECT 1 FROM pg_default_acl d, aclexplode(d.defaclacl) a WHERE d.defaclobjtype = 'r' AND a.grantee = r.oid AND a.privilege_type IN ('INSERT','UPDATE','DELETE')),
 EXISTS (SELECT 1 FROM pg_class c WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r','p','v','m') AND has_table_privilege(r.oid, c.oid, 'SELECT'))
  OR EXISTS (SELECT 1 FROM pg_default_acl d, aclexplode(d.defaclacl) a WHERE d.defaclobjtype = 'r' AND a.grantee = r.oid AND a.privilege_type = 'SELECT')
FROM pg_roles r WHERE ` + pgLogins + `;
`

func accessLevel(connect, full, write, read bool) string {
	switch {
	case !connect:
		return ""
	case full:
		return "full"
	case write:
		return "read_write"
	case read:
		return "read_only"
	}
	return ""
}

func (s *Service) readServerCatalog(ctx context.Context, d store.DBInstance, c map[string]string) (serverCatalog, error) {
	out, ok := s.ex.(executor.CommandOutputExecutor)
	if !ok {
		return serverCatalog{}, errors.New("this panel cannot read the server's catalog")
	}
	cat := serverCatalog{access: map[[2]string]string{}}
	query := func(db, sql string) ([][]string, error) {
		text, err := out.ExecOutput(ctx, ContainerName(d.ID), queryCommand(d, c, db, sql))
		if err != nil {
			return nil, err
		}
		return rows(text), nil
	}
	names := func(db, sql string) ([]string, error) {
		found, err := query(db, sql)
		if err != nil {
			return nil, err
		}
		var list []string
		for _, row := range found {
			if importableName(row[0]) {
				list = append(list, row[0])
			} else {
				cat.skipped++
			}
		}
		return list, nil
	}
	yes := func(v string) bool { return v == "t" || v == "Y" }
	var err error
	if d.Engine == "mariadb" {
		if cat.databases, err = names("", "SELECT schema_name FROM information_schema.schemata WHERE schema_name NOT IN ('mysql','sys','information_schema','performance_schema');\n"); err != nil {
			return cat, err
		}
		if cat.users, err = names("", "SELECT User FROM mysql.user WHERE Host = '%' AND User NOT IN ('', 'root', 'mariadb.sys', 'healthcheck');\n"); err != nil {
			return cat, err
		}
		// Privileges on every database, then on each one. Columns: read, write, full.
		const cols = "Select_priv, IF(Insert_priv='Y' OR Update_priv='Y' OR Delete_priv='Y','Y','N'), IF(Select_priv='Y' AND Insert_priv='Y' AND Update_priv='Y' AND Delete_priv='Y' AND Create_priv='Y' AND Drop_priv='Y' AND Alter_priv='Y','Y','N')"
		global, err := query("", "SELECT User, "+cols+" FROM mysql.user WHERE Host = '%';\n")
		if err != nil {
			return cat, err
		}
		best := func(user, db, level string) {
			rank := map[string]int{"": 0, "read_only": 1, "read_write": 2, "full": 3}
			if rank[level] > rank[cat.access[[2]string{user, db}]] {
				cat.access[[2]string{user, db}] = level
			}
		}
		for _, row := range global {
			if len(row) < 4 {
				continue
			}
			for _, db := range cat.databases {
				best(row[0], db, accessLevel(true, yes(row[3]), yes(row[2]), yes(row[1])))
			}
		}
		perDB, err := query("", "SELECT User, Db, "+cols+" FROM mysql.db WHERE Host = '%';\n")
		if err != nil {
			return cat, err
		}
		for _, row := range perDB {
			if len(row) < 5 {
				continue
			}
			// The server stores a pattern: \_ is an underscore, a bare _ or % a wildcard.
			// The mysql client prints the backslash doubled in batch mode.
			pattern := strings.ReplaceAll(row[1], `\\`, `\`)
			for _, db := range cat.databases {
				if mariaPattern(pattern, db) {
					best(row[0], db, accessLevel(true, yes(row[4]), yes(row[3]), yes(row[2])))
				}
			}
		}
		return cat, nil
	}
	if cat.databases, err = names("postgres", "SELECT datname FROM pg_database WHERE NOT datistemplate AND datname <> 'postgres';\n"); err != nil {
		return cat, err
	}
	if cat.users, err = names("postgres", "SELECT rolname FROM pg_roles WHERE "+pgLogins+";\n"); err != nil {
		return cat, err
	}
	for _, db := range cat.databases {
		found, err := query(db, pgAccessSQL)
		if err != nil {
			return cat, err
		}
		for _, row := range found {
			if len(row) < 5 {
				continue
			}
			if level := accessLevel(yes(row[1]), yes(row[2]), yes(row[3]), yes(row[4])); level != "" {
				cat.access[[2]string{row[0], db}] = level
			}
		}
	}
	return cat, nil
}

// mariaPattern matches a database name against a mysql.db pattern.
func mariaPattern(pattern, name string) bool {
	var re strings.Builder
	re.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch c := pattern[i]; {
		case c == '\\' && i+1 < len(pattern):
			i++
			re.WriteString(regexp.QuoteMeta(pattern[i : i+1]))
		case c == '_':
			re.WriteString(".")
		case c == '%':
			re.WriteString(".*")
		default:
			re.WriteString(regexp.QuoteMeta(pattern[i : i+1]))
		}
	}
	re.WriteString("$")
	ok, err := regexp.MatchString(re.String(), name)
	return err == nil && ok
}

// syncCatalog makes the panel's lists match the server. The initial database, the
// initial user and that user's full access to it are the instance itself and stay.
func (s *Service) syncCatalog(ctx context.Context, d store.DBInstance, c map[string]string, catalog store.DatabaseCatalog) (SyncResult, error) {
	server, err := s.readServerCatalog(ctx, d, c)
	if err != nil {
		return SyncResult{}, errors.New("The server's databases and users could not be read; check the instance and retry")
	}
	res := SyncResult{Skipped: server.skipped}
	onServer := func(list []string) map[string]bool {
		m := map[string]bool{}
		for _, n := range list {
			m[n] = true
		}
		return m
	}
	dbs, users := onServer(server.databases), onServer(server.users)
	known := map[string]bool{}
	for _, db := range catalog.Databases {
		known[db.Name] = true
		if !dbs[db.Name] && db.Name != d.DatabaseName {
			if err := s.st.RemoveManagedDatabase(ctx, d.ID, db.Name); err != nil {
				return res, err
			}
			res.DatabasesRemoved++
		}
	}
	for _, name := range server.databases {
		if !known[name] {
			if _, err := s.st.AddManagedDatabase(ctx, d.ID, name); err != nil {
				return res, err
			}
			res.DatabasesAdded++
		}
	}
	known = map[string]bool{}
	for _, u := range catalog.Users {
		known[u.Name] = true
		if !users[u.Name] && !u.Initial {
			if err := s.st.RemoveDatabaseUser(ctx, d.ID, u.Name); err != nil {
				return res, err
			}
			res.UsersRemoved++
		}
	}
	for _, name := range server.users {
		if !known[name] {
			if _, err := s.st.AddExternalDatabaseUser(ctx, d.ID, name); err != nil {
				return res, err
			}
			res.UsersAdded++
		}
	}
	// Permissions, against the lists as they now are.
	now, err := s.st.DatabaseCatalog(ctx, d.ID)
	if err != nil {
		return res, err
	}
	saved := map[[2]string]string{}
	for _, g := range now.Grants {
		saved[[2]string{g.UserID, g.DatabaseID}] = g.Access
	}
	for _, u := range now.Users {
		for _, db := range now.Databases {
			if u.Initial && db.Name == d.DatabaseName {
				continue
			}
			level := server.access[[2]string{u.Name, db.Name}]
			if level == saved[[2]string{u.ID, db.ID}] {
				continue
			}
			if level == "" {
				level = "none"
			}
			if err := s.st.SetDatabaseGrant(ctx, u.ID, db.ID, level); err != nil {
				return res, err
			}
			res.GrantsChanged++
		}
	}
	return res, nil
}

package store

import (
	"context"
	"database/sql"
	"errors"

	"panel/internal/id"
)

type ManagedDatabase struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type DatabaseUser struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Initial bool   `json:"initial"`
}
type DatabaseGrant struct {
	UserID     string `json:"user_id"`
	DatabaseID string `json:"database_id"`
	Access     string `json:"access"`
}
type DatabaseCatalog struct {
	Databases []ManagedDatabase `json:"databases"`
	Users     []DatabaseUser    `json:"users"`
	Grants    []DatabaseGrant   `json:"grants"`
}

func (s *Store) AddManagedDatabase(ctx context.Context, instanceID, name string) (ManagedDatabase, error) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO db_databases (id, instance_id, name, created_at, updated_at) VALUES (?, ?, ?, ?, ?) ON CONFLICT(instance_id, name) DO NOTHING`, id.New(), instanceID, name, now(), now())
	if err != nil {
		return ManagedDatabase{}, err
	}
	var d ManagedDatabase
	err = s.db.QueryRowContext(ctx, `SELECT id, name FROM db_databases WHERE instance_id = ? AND name = ?`, instanceID, name).Scan(&d.ID, &d.Name)
	return d, err
}

func (s *Store) AddDatabaseUser(ctx context.Context, instanceID, name, password string, initial bool) (DatabaseUser, error) {
	sealed, err := s.sealEnvironment(map[string]string{"password": password})
	if err != nil {
		return DatabaseUser{}, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO db_users (id, instance_id, name, credentials, is_initial, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(instance_id, name) DO NOTHING`, id.New(), instanceID, name, sealed, initial, now(), now())
	if err != nil {
		return DatabaseUser{}, err
	}
	return s.GetDatabaseUser(ctx, instanceID, name)
}

func (s *Store) GetDatabaseUser(ctx context.Context, instanceID, name string) (DatabaseUser, error) {
	var u DatabaseUser
	err := s.db.QueryRowContext(ctx, `SELECT id, name, is_initial FROM db_users WHERE instance_id = ? AND name = ?`, instanceID, name).Scan(&u.ID, &u.Name, &u.Initial)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

func (s *Store) DatabaseUserPassword(ctx context.Context, instanceID, name string) (string, error) {
	u, err := s.GetDatabaseUser(ctx, instanceID, name)
	if err != nil {
		return "", err
	}
	if u.Initial {
		creds, err := s.DBInstanceCredentials(ctx, instanceID)
		return creds["password"], err
	}
	var sealed string
	if err := s.db.QueryRowContext(ctx, `SELECT credentials FROM db_users WHERE id = ?`, u.ID).Scan(&sealed); err != nil {
		return "", err
	}
	creds, err := s.openEnvironment(sealed)
	return creds["password"], err
}

func (s *Store) SetDatabaseGrant(ctx context.Context, userID, databaseID, access string) error {
	if access == "none" {
		_, err := s.db.ExecContext(ctx, `DELETE FROM db_user_grants WHERE user_id = ? AND database_id = ?`, userID, databaseID)
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO db_user_grants (user_id, database_id, access, created_at, updated_at) VALUES (?, ?, ?, ?, ?) ON CONFLICT(user_id, database_id) DO UPDATE SET access=excluded.access, updated_at=excluded.updated_at`, userID, databaseID, access, now(), now())
	return err
}

func (s *Store) DatabaseCatalog(ctx context.Context, instanceID string) (DatabaseCatalog, error) {
	out := DatabaseCatalog{Databases: []ManagedDatabase{}, Users: []DatabaseUser{}, Grants: []DatabaseGrant{}}
	rows, err := s.db.QueryContext(ctx, `SELECT id, name FROM db_databases WHERE instance_id = ? ORDER BY name`, instanceID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var d ManagedDatabase
		if err := rows.Scan(&d.ID, &d.Name); err != nil {
			rows.Close()
			return out, err
		}
		out.Databases = append(out.Databases, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT id, name, is_initial FROM db_users WHERE instance_id = ? ORDER BY name`, instanceID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var u DatabaseUser
		if err := rows.Scan(&u.ID, &u.Name, &u.Initial); err != nil {
			rows.Close()
			return out, err
		}
		out.Users = append(out.Users, u)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT g.user_id, g.database_id, g.access FROM db_user_grants g JOIN db_users u ON u.id = g.user_id WHERE u.instance_id = ? ORDER BY g.user_id, g.database_id`, instanceID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var g DatabaseGrant
		if err := rows.Scan(&g.UserID, &g.DatabaseID, &g.Access); err != nil {
			return out, err
		}
		out.Grants = append(out.Grants, g)
	}
	return out, rows.Err()
}

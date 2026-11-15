package api

import (
	"errors"
	"net/http"

	"panel/internal/database"
	"panel/internal/store"
)

func (s *Server) handleDatabaseCatalog(w http.ResponseWriter, r *http.Request) {
	if !s.requireDatabases(w) {
		return
	}
	catalog, err := s.databases.Catalog(r.Context(), r.PathValue("id"))
	if s.databaseError(w, err) {
		return
	}
	writeJSON(w, 200, catalog)
}

func (s *Server) handleDatabaseManage(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.requireDatabases(w) {
			return
		}
		payload := map[string]string{}
		if action == "grant_update" {
			var in struct {
				UserID     string `json:"user_id"`
				DatabaseID string `json:"database_id"`
				Access     string `json:"access"`
			}
			if !readJSON(w, r, &in) {
				return
			}
			payload["user_id"], payload["database_id"], payload["access"] = in.UserID, in.DatabaseID, in.Access
		} else {
			var in struct {
				Name string `json:"name"`
			}
			if !readJSON(w, r, &in) {
				return
			}
			payload["name"] = in.Name
		}
		op, err := s.databases.Manage(r.Context(), r.PathValue("id"), action, payload)
		if errors.Is(err, store.ErrConflict) {
			writeError(w, 409, "conflict", "A database or user with this name already exists in this instance")
			return
		}
		if s.databaseError(w, err) {
			return
		}
		s.audit(r, "database."+action, "database", op.InstanceID, op.InstanceName, op.Detail)
		writeJSON(w, 202, map[string]any{"operation": op})
	}
}

func (s *Server) handleDatabaseUserConnection(w http.ResponseWriter, r *http.Request) {
	if !s.requireDatabases(w) {
		return
	}
	d, err := s.store.GetDBInstance(r.Context(), r.PathValue("id"))
	if s.storeError(w, "get database", err, "") {
		return
	}
	catalog, err := s.databases.Catalog(r.Context(), d.ID)
	if s.databaseError(w, err) {
		return
	}
	var username, dbName string
	for _, u := range catalog.Users {
		if u.ID == r.PathValue("user") {
			username = u.Name
		}
	}
	for _, db := range catalog.Databases {
		if db.ID == r.URL.Query().Get("database_id") {
			dbName = db.Name
		}
	}
	if username == "" || dbName == "" {
		writeError(w, 404, "not_found", "database or user not found in this instance")
		return
	}
	password, err := s.store.DatabaseUserPassword(r.Context(), d.ID, username)
	if s.storeError(w, "user credentials", err, "") {
		return
	}
	d.Username, d.DatabaseName = username, dbName
	s.audit(r, "database.credentials", "database", d.ID, d.Name, username+" → "+dbName)
	writeJSON(w, 200, database.Connection(d, map[string]string{"password": password}))
}

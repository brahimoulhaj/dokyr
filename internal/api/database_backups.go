package api

import (
	"net/http"
	"panel/internal/store"
)

func (s *Server) handleDatabaseBackups(w http.ResponseWriter, r *http.Request) {
	if !s.requireDatabases(w) {
		return
	}
	if _, e := s.store.GetDBInstance(r.Context(), r.PathValue("id")); s.storeError(w, "get database", e, "") {
		return
	}
	list, e := s.store.ListDBBackups(r.Context(), r.PathValue("id"))
	if s.storeError(w, "list backups", e, "") {
		return
	}
	writeJSON(w, 200, list)
}
func (s *Server) handleDatabaseBackupAction(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.requireDatabases(w) {
			return
		}
		var in struct {
			Database       string `json:"database"`
			S3ConnectionID string `json:"s3_connection_id"`
			BackupID       string `json:"backup_id"`
			Name           string `json:"name"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		op, e := s.databases.BackupAction(r.Context(), r.PathValue("id"), action, map[string]string{"database": in.Database, "s3_connection_id": in.S3ConnectionID, "backup_id": in.BackupID, "name": in.Name})
		if s.databaseError(w, e) {
			return
		}
		s.audit(r, "database."+action, "database", op.InstanceID, op.InstanceName, op.Detail)
		writeJSON(w, 202, map[string]any{"operation": op})
	}
}

func (s *Server) handleBackupPolicies(w http.ResponseWriter, r *http.Request) {
	if !s.requireDatabases(w) {
		return
	}
	if _, e := s.store.GetDBInstance(r.Context(), r.PathValue("id")); s.storeError(w, "get database", e, "") {
		return
	}
	if r.Method == "GET" {
		p, e := s.store.BackupPolicies(r.Context(), r.PathValue("id"))
		if s.storeError(w, "list backup policies", e, "") {
			return
		}
		writeJSON(w, 200, p)
		return
	}
	var in struct {
		Database       string `json:"database"`
		S3ConnectionID string `json:"s3_connection_id"`
		Enabled        bool   `json:"enabled"`
		Frequency      string `json:"frequency"`
		Hour           int    `json:"hour"`
		Minute         int    `json:"minute"`
		Weekday        int    `json:"weekday"`
		KeepCount      int    `json:"keep_count"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	p, e := s.databases.SaveBackupPolicy(r.Context(), store.BackupPolicy{InstanceID: r.PathValue("id"), DatabaseName: in.Database, S3ConnectionID: in.S3ConnectionID, Enabled: in.Enabled, Frequency: in.Frequency, Hour: in.Hour, Minute: in.Minute, Weekday: in.Weekday, KeepCount: in.KeepCount})
	if s.databaseError(w, e) {
		return
	}
	s.audit(r, "database.backup_policy", "database", p.InstanceID, p.DatabaseName, "Updated backup schedule and retention")
	writeJSON(w, 200, p)
}

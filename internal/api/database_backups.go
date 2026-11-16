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
			Scope          string `json:"scope"` // "instance" backs up every database
		}
		if !readJSON(w, r, &in) {
			return
		}
		op, e := s.databases.BackupAction(r.Context(), r.PathValue("id"), action, map[string]string{"database": in.Database, "s3_connection_id": in.S3ConnectionID, "backup_id": in.BackupID, "name": in.Name, "scope": in.Scope})
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
		Cron           string `json:"cron"`
		Scope          string `json:"scope"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	p, e := s.databases.SaveBackupPolicy(r.Context(), store.BackupPolicy{InstanceID: r.PathValue("id"), DatabaseName: in.Database, S3ConnectionID: in.S3ConnectionID, Enabled: in.Enabled, Frequency: in.Frequency, Hour: in.Hour, Minute: in.Minute, Weekday: in.Weekday, KeepCount: in.KeepCount, Cron: in.Cron, Scope: in.Scope})
	if s.databaseError(w, e) {
		return
	}
	target := p.DatabaseName
	if p.Scope == "instance" {
		target = "all databases"
	}
	s.audit(r, "database.backup_policy", "database", p.InstanceID, target, "Updated backup schedule and retention")
	writeJSON(w, 200, p)
}

func (s *Server) handleDeleteDatabaseBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requireDatabases(w) {
		return
	}
	d, e := s.store.GetDBInstance(r.Context(), r.PathValue("id"))
	if s.storeError(w, "get database", e, "") {
		return
	}
	b, e := s.databases.DeleteBackup(r.Context(), d.ID, r.PathValue("backup"))
	if s.databaseError(w, e) {
		return
	}
	s.audit(r, "database.backup_delete", "database", d.ID, d.Name, "Deleted backup of "+b.DatabaseName+" ("+b.ID+")")
	w.WriteHeader(http.StatusNoContent)
}

// handleRestoreInstance creates a new instance from a whole-instance backup of
// this one. Engine, version and initial names come from the backup's manifest.
func (s *Server) handleRestoreInstance(w http.ResponseWriter, r *http.Request) {
	if !s.requireDatabases(w) {
		return
	}
	var in struct {
		BackupID  string  `json:"backup_id"`
		ProjectID string  `json:"project_id"`
		Name      string  `json:"name"`
		CPULimit  float64 `json:"cpu_limit"`
		MemoryMB  int64   `json:"memory_mb"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	source := r.PathValue("id")
	if in.ProjectID == "" {
		if d, e := s.store.GetDBInstance(r.Context(), source); e == nil {
			in.ProjectID, in.CPULimit, in.MemoryMB = d.ProjectID, d.CPULimit, d.MemoryMB
		}
	}
	d, op, e := s.databases.RestoreInstance(r.Context(), source, in.BackupID, store.DBInstance{ProjectID: in.ProjectID, Name: in.Name, CPULimit: in.CPULimit, MemoryMB: in.MemoryMB})
	if s.databaseError(w, e) {
		return
	}
	s.audit(r, "database.restore_instance", "database", d.ID, d.Name, "Restored from backup "+in.BackupID+" of "+source)
	out := s.databaseJSON(r.Context(), d)
	out.Operation = &op
	writeJSON(w, 202, out)
}

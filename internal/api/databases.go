package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"panel/internal/database"
	"panel/internal/store"
)

func (s *Server) SetDatabases(d *database.Service) { s.databases = d }

type databaseJSON struct {
	store.DBInstance
	Host      string             `json:"host"`
	Volume    string             `json:"volume"`
	Runtime   string             `json:"runtime"`
	Operation *store.DBOperation `json:"operation,omitempty"`
}

func (s *Server) databaseJSON(ctx context.Context, d store.DBInstance) databaseJSON {
	runtime := "unavailable"
	if s.databases != nil {
		runtime = s.databases.Runtime(ctx, d.ID)
	}
	return databaseJSON{DBInstance: d, Host: database.ContainerName(d.ID), Volume: database.VolumeName(d.ID), Runtime: runtime}
}

func (s *Server) requireDatabases(w http.ResponseWriter) bool {
	if s.databases == nil {
		writeError(w, 503, "unavailable", "database management is unavailable")
		return false
	}
	return true
}

func (s *Server) databaseError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, database.ErrInvalid) {
		invalid(w, err.Error())
		return true
	}
	if errors.Is(err, database.ErrBusy) {
		writeError(w, 409, "busy", err.Error())
		return true
	}
	if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrConflict) {
		return s.storeError(w, "database", err, "a database instance with this name already exists in the project")
	}
	// Service errors contain no SQL, environment values or raw Docker diagnostics.
	s.log.Error("database operation", "error", err)
	writeError(w, 502, "database_failed", "the database operation failed; check Docker and try again")
	return true
}

func (s *Server) handleDatabaseEngines(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, database.Engines())
}

func (s *Server) handleListDatabases(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListDBInstances(r.Context())
	if s.storeError(w, "list databases", err, "") {
		return
	}
	sums, err := s.store.DBInstanceSummaries(r.Context())
	if s.storeError(w, "summarize databases", err, "") {
		return
	}
	// The list shows counts and the latest backup beside each instance.
	type listed struct {
		databaseJSON
		store.DBInstanceSummary
	}
	out := []listed{}
	for _, d := range list {
		out = append(out, listed{s.databaseJSON(r.Context(), d), sums[d.ID]})
	}
	writeJSON(w, 200, out)
}

type projectDatabaseJSON struct {
	databaseJSON
	DatabaseNames []string `json:"database_names"`
}

func (s *Server) handleProjectDatabases(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetProject(r.Context(), r.PathValue("id"))
	if s.storeError(w, "get project", err, "") {
		return
	}
	list, err := s.store.ListProjectDBInstances(r.Context(), p.ID)
	if s.storeError(w, "list project databases", err, "") {
		return
	}
	out := []projectDatabaseJSON{}
	for _, d := range list {
		names, err := s.store.DBInstanceDatabaseNames(r.Context(), d.ID)
		if s.storeError(w, "list instance databases", err, "") {
			return
		}
		out = append(out, projectDatabaseJSON{databaseJSON: s.databaseJSON(r.Context(), d), DatabaseNames: names})
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleCreateDatabase(w http.ResponseWriter, r *http.Request) {
	if !s.requireDatabases(w) {
		return
	}
	// Only creation fields are accepted; runtime and credential fields are server-owned.
	var in struct {
		ProjectID    string  `json:"project_id"`
		Name         string  `json:"name"`
		Engine       string  `json:"engine"`
		Version      string  `json:"version"`
		DatabaseName string  `json:"database_name"`
		Username     string  `json:"username"`
		CPULimit     float64 `json:"cpu_limit"`
		MemoryMB     int64   `json:"memory_mb"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	d, op, err := s.databases.CreateWithOperation(r.Context(), store.DBInstance{ProjectID: in.ProjectID, Name: in.Name,
		Engine: in.Engine, Version: in.Version, DatabaseName: in.DatabaseName, Username: in.Username, CPULimit: in.CPULimit, MemoryMB: in.MemoryMB})
	if s.databaseError(w, err) {
		return
	}
	s.audit(r, "database.create", "database", d.ID, d.Name, d.Engine+":"+d.Version)
	out := s.databaseJSON(r.Context(), d)
	out.Operation = &op
	writeJSON(w, 202, out)
}

func (s *Server) handleGetDatabase(w http.ResponseWriter, r *http.Request) {
	d, err := s.store.GetDBInstance(r.Context(), r.PathValue("id"))
	if s.storeError(w, "get database", err, "") {
		return
	}
	writeJSON(w, 200, s.databaseJSON(r.Context(), d))
}

func (s *Server) handleUpdateDatabase(w http.ResponseWriter, r *http.Request) {
	if !s.requireDatabases(w) {
		return
	}
	var in struct {
		ProjectID string `json:"project_id"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	d, err := s.databases.MoveProject(r.Context(), r.PathValue("id"), in.ProjectID)
	if s.databaseError(w, err) {
		return
	}
	s.audit(r, "database.project.update", "database", d.ID, d.Name, "project="+d.ProjectID)
	writeJSON(w, 200, s.databaseJSON(r.Context(), d))
}

func (s *Server) handleDatabaseConnection(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	d, err := s.store.GetDBInstance(r.Context(), r.PathValue("id"))
	if s.storeError(w, "get database", err, "") {
		return
	}
	creds, err := s.store.DBInstanceCredentials(r.Context(), d.ID)
	if s.storeError(w, "database credentials", err, "") {
		return
	}
	s.audit(r, "database.credentials", "database", d.ID, d.Name, "")
	writeJSON(w, 200, database.Connection(d, creds))
}

func (s *Server) handleDatabaseAction(w http.ResponseWriter, r *http.Request) {
	if !s.requireDatabases(w) {
		return
	}
	id, action := r.PathValue("id"), r.PathValue("action")
	if action != "start" && action != "stop" && action != "restart" {
		invalid(w, "choose start, stop or restart")
		return
	}
	op, err := s.databases.Enqueue(r.Context(), id, action)
	if s.databaseError(w, err) {
		return
	}
	d, err := s.store.GetDBInstance(r.Context(), id)
	if s.storeError(w, "get database", err, "") {
		return
	}
	s.audit(r, "database."+action, "database", d.ID, d.Name, "")
	out := s.databaseJSON(r.Context(), d)
	out.Operation = &op
	writeJSON(w, 202, out)
}

func (s *Server) handleDeleteDatabase(w http.ResponseWriter, r *http.Request) {
	if !s.requireDatabases(w) {
		return
	}
	d, err := s.store.GetDBInstance(r.Context(), r.PathValue("id"))
	if s.storeError(w, "get database", err, "") {
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Name != d.Name {
		invalid(w, "confirm deletion with the instance name")
		return
	}
	op, err := s.databases.Enqueue(r.Context(), d.ID, "delete")
	if s.databaseError(w, err) {
		return
	}
	s.audit(r, "database.delete", "database", d.ID, d.Name, "Data volume retained: "+database.VolumeName(d.ID))
	writeJSON(w, 202, map[string]any{"operation": op})
}

func (s *Server) handleActiveDatabaseOperations(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ActiveDBOperations(r.Context())
	if s.storeError(w, "list database operations", err, "") {
		return
	}
	writeJSON(w, 200, list)
}

func (s *Server) handleGetDatabaseOperation(w http.ResponseWriter, r *http.Request) {
	op, err := s.store.GetDBOperation(r.Context(), r.PathValue("id"))
	if s.storeError(w, "get database operation", err, "") {
		return
	}
	writeJSON(w, 200, op)
}

func (s *Server) handleDatabaseLogs(w http.ResponseWriter, r *http.Request) {
	if !s.requireDatabases(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	lines, err := s.databases.Logs(ctx, r.PathValue("id"))
	if s.databaseError(w, err) {
		return
	}
	out := []logEvent{}
	for _, l := range lines {
		out = append(out, logEvent{Stream: l.Stream, Text: l.Text, Time: l.Time.UTC().Format(time.RFC3339Nano)})
	}
	writeJSON(w, 200, out)
}

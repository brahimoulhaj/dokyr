package api

import (
	"net/http"
	"strconv"
)

// handleDatabasePublic shows (GET) or changes (PUT) an instance's public port.
func (s *Server) handleDatabasePublic(w http.ResponseWriter, r *http.Request) {
	if !s.requireDatabases(w) {
		return
	}
	id := r.PathValue("id")
	if r.Method == http.MethodPut {
		var in struct {
			Port  int    `json:"port"` // 0 closes the port
			Allow string `json:"allow"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		d, err := s.databases.SetPublicAccess(r.Context(), id, in.Port, in.Allow)
		if s.databaseError(w, err) {
			return
		}
		detail := "Closed the public port"
		if in.Port > 0 {
			detail = "Opened public port " + strconv.Itoa(in.Port)
			if d.PublicAllow == "" {
				detail += " to any address"
			} else {
				detail += " to " + d.PublicAllow
			}
		}
		s.audit(r, "database.public", "database", d.ID, d.Name, detail)
	}
	out, err := s.databases.PublicAccess(r.Context(), id)
	if s.databaseError(w, err) {
		return
	}
	writeJSON(w, 200, out)
}

// handleDatabaseCertificate serves the gate's certificate (public data) for clients
// that verify the server, e.g. sslmode=verify-full sslrootcert=<file>.
func (s *Server) handleDatabaseCertificate(w http.ResponseWriter, r *http.Request) {
	if !s.requireDatabases(w) {
		return
	}
	d, err := s.store.GetDBInstance(r.Context(), r.PathValue("id"))
	if s.storeError(w, "get database", err, "") {
		return
	}
	pem, err := s.databases.Certificate(r.Context(), d.ID)
	if s.storeError(w, "certificate", err, "") {
		return
	}
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Header().Set("Content-Disposition", `attachment; filename="`+d.Name+`-ca.pem"`)
	_, _ = w.Write([]byte(pem))
}

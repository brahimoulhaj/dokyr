package api

import (
	"maps"
	"net/http"
	"slices"
	"strings"

	"panel/internal/database"
	"panel/internal/store"
	"panel/internal/templates"
)

// templateJSON is a template of the catalog with the names of the variables it sets.
type templateJSON struct {
	templates.Template
	Variables []string `json:"variables"`
	// DatabaseVariables are the ones left empty until a database is connected.
	DatabaseVariables []string `json:"database_variables"`
}

func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, mapSlice(templates.All(), func(t templates.Template) templateJSON {
		// Never null in JSON: a template may set no variable or keep no volume.
		out := templateJSON{Template: t, Variables: append([]string{}, slices.Sorted(maps.Keys(t.Env))...)}
		out.Volumes, out.DatabaseVariables = append([]templates.Volume{}, t.Volumes...), t.DatabaseVariables()
		return out
	}))
}

// handleCreateFromTemplate creates a template's app with its volumes and variables. Nothing
// is deployed, and no database is created: the variables a database fills are left empty
// for the app's owner to connect one (handleConnectDatabase) or to write them.
func (s *Server) handleCreateFromTemplate(w http.ResponseWriter, r *http.Request) {
	tpl, ok := templates.Get(r.PathValue("template"))
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "template not found")
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = tpl.ID
	}
	p, err := s.store.GetProject(r.Context(), r.PathValue("id"))
	if s.storeError(w, "get project", err, "") {
		return
	}
	input, err := tpl.AppInput(name)
	if err != nil {
		s.internal(w, "template", err)
		return
	}
	if !validApp(w, input) {
		return
	}
	a, err := s.store.CreateApp(r.Context(), p.ID, input)
	if s.storeError(w, "create app", err, "an app with this name already exists in the project") {
		return
	}
	hc := store.DefaultHealthCheck()
	if tpl.StartPeriod > 0 {
		hc.StartPeriod = tpl.StartPeriod
	}
	if err := s.store.SetAppHealthCheck(r.Context(), a.ID, hc); err != nil {
		s.log.Error("default health check", "app", a.ID, "error", err)
	}
	s.audit(r, "app.create", "app", a.ID, a.Name, "template "+tpl.ID)
	writeJSON(w, http.StatusCreated, map[string]any{"app": toAppJSON(a)})
}

// handleConnectDatabase fills the database variables of an app made from a template. With
// only instance_id they become references to that instance's own database, read at each
// deploy. With database_id and user_id, for another database or user of the instance, the
// connection is written into the variables, the ones holding the password as secrets.
func (s *Server) handleConnectDatabase(w http.ResponseWriter, r *http.Request) {
	if !s.requireDatabases(w) {
		return
	}
	var in struct {
		InstanceID string `json:"instance_id"`
		DatabaseID string `json:"database_id"`
		UserID     string `json:"user_id"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	a, err := s.store.GetApp(r.Context(), r.PathValue("id"))
	if s.storeError(w, "get app", err, "") {
		return
	}
	tpl, ok := templates.Get(a.Template)
	if !ok || tpl.Database == nil {
		invalid(w, "this app's template uses no database")
		return
	}
	d, err := s.store.GetDBInstance(r.Context(), in.InstanceID)
	if err != nil || d.ProjectID != a.ProjectID {
		invalid(w, "choose a database instance of this app's project")
		return
	}
	if d.Engine != tpl.Database.Engine {
		invalid(w, tpl.Name+" needs a "+tpl.Database.Engine+" database")
		return
	}
	catalog, err := s.databases.Catalog(r.Context(), d.ID)
	if s.databaseError(w, err) {
		return
	}
	dbName, user := d.DatabaseName, d.Username
	for _, db := range catalog.Databases {
		if db.ID == in.DatabaseID {
			dbName = db.Name
		}
	}
	for _, u := range catalog.Users {
		if u.ID == in.UserID {
			user = u.Name
		}
	}
	var fields map[string]string
	detail := d.Name
	if dbName != d.DatabaseName || user != d.Username {
		password, err := s.store.DatabaseUserPassword(r.Context(), d.ID, user)
		if s.storeError(w, "user credentials", err, "") {
			return
		}
		other := d
		other.Username, other.DatabaseName = user, dbName
		fields = database.Connection(other, map[string]string{"password": password})
		detail = d.Name + ": " + user + " → " + dbName
	}
	env, secret := tpl.Connect(d.Name, fields)
	input := store.AppInput{
		Mounts: a.Mounts, Processes: a.Processes, Environment: maps.Clone(a.Environment), EnvMeta: maps.Clone(a.EnvMeta), EnvOrder: a.EnvOrder, Name: a.Name, SourceType: a.SourceType, Image: a.Image, RegistryID: a.RegistryID, Git: a.Git,
		StartCommand: a.StartCommand, Port: a.Port, CPULimit: a.CPULimit, MemoryMB: a.MemoryMB,
	}
	for key, value := range env {
		input.Environment[key] = value
		if secret[key] {
			input.EnvMeta[key] = store.EnvVarMeta{Scope: store.EnvScopeRuntime, Secret: true}
		} else {
			delete(input.EnvMeta, key)
		}
	}
	if !validApp(w, input) {
		return
	}
	a, err = s.store.UpdateApp(r.Context(), a.ID, input)
	if s.storeError(w, "update app", err, "") {
		return
	}
	s.audit(r, "app.connect_database", "app", a.ID, a.Name, detail)
	writeJSON(w, http.StatusOK, toAppJSON(a))
}

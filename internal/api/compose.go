package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"panel/internal/compose"
	"panel/internal/store"
)

// composeAppJSON is one app a Compose file becomes: what the review shows before anything
// is created, and the created app's ID after.
type composeAppJSON struct {
	ID           string   `json:"id,omitempty"`
	Name         string   `json:"name"`
	Image        string   `json:"image"`
	Port         int      `json:"port"`      // 0 = the service declares none
	Variables    []string `json:"variables"` // names only
	Volumes      []string `json:"volumes"`   // container paths
	StartCommand string   `json:"start_command"`
}

type composeResultJSON struct {
	Apps  []composeAppJSON `json:"apps"`
	Notes []compose.Note   `json:"notes"` // what differs from the file
}

// problem is why validApp refuses an input, without answering the request with it.
type problemWriter struct {
	header http.Header
	body   strings.Builder
}

func (p *problemWriter) Header() http.Header         { return p.header }
func (p *problemWriter) WriteHeader(int)             {}
func (p *problemWriter) Write(b []byte) (int, error) { return p.body.Write(b) }

func appProblem(in store.AppInput) string {
	p := &problemWriter{header: http.Header{}}
	if validApp(p, in) {
		return ""
	}
	var body errorBody
	if json.Unmarshal([]byte(p.body.String()), &body) != nil || body.Error.Message == "" {
		return "invalid app"
	}
	return body.Error.Message
}

// handleImportCompose converts a pasted Compose file into apps of the project, one for each
// service. With dry_run nothing is created: the answer is what would be. The apps are not
// deployed.
func (s *Server) handleImportCompose(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Compose string `json:"compose"`
		DryRun  bool   `json:"dry_run"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	p, err := s.store.GetProject(r.Context(), r.PathValue("id"))
	if s.storeError(w, "get project", err, "") {
		return
	}
	if len(in.Compose) > 256*1024 {
		invalid(w, "the Compose file must be at most 256 KiB")
		return
	}
	result, err := compose.Convert([]byte(in.Compose))
	if err != nil {
		invalid(w, err.Error())
		return
	}
	existing, err := s.store.ListApps(r.Context(), p.ID)
	if err != nil {
		s.internal(w, "list apps", err)
		return
	}
	taken := map[string]bool{}
	for _, a := range existing {
		taken[a.Name] = true
	}
	out := composeResultJSON{Apps: []composeAppJSON{}, Notes: append([]compose.Note{}, result.Notes...)}
	for _, app := range result.Apps {
		if msg := appProblem(app); msg != "" {
			invalid(w, "service "+app.Name+": "+msg)
			return
		}
		if taken[app.Name] {
			writeError(w, http.StatusConflict, "conflict", "an app named "+app.Name+" already exists in the project: rename the service")
			return
		}
		item := composeAppJSON{Name: app.Name, Image: app.Image, Port: app.Port, StartCommand: app.StartCommand, Variables: []string{}, Volumes: []string{}}
		for key := range app.Environment {
			item.Variables = append(item.Variables, key)
		}
		for _, m := range app.Mounts {
			item.Volumes = append(item.Volumes, m.Target)
		}
		if result.NoPort[app.Name] {
			item.Port = 0 // none declared: nothing to show
		}
		out.Apps = append(out.Apps, item)
	}
	if in.DryRun {
		writeJSON(w, http.StatusOK, out)
		return
	}
	var created []store.App
	for i, app := range result.Apps {
		a, err := s.store.CreateApp(r.Context(), p.ID, app)
		if err != nil {
			// All the services or none: a stack with a part missing would not work.
			for _, c := range created {
				if derr := s.store.DeleteApp(r.Context(), c.ID); derr != nil {
					s.log.Error("compose cleanup", "app", c.ID, "error", derr)
				}
			}
			s.storeError(w, "create app", err, "an app named "+app.Name+" already exists in the project")
			return
		}
		// A service without a port (a database, a worker) has none to probe: it is healthy
		// when its container stays up.
		hc := store.DefaultHealthCheck()
		if result.NoPort[app.Name] {
			hc = store.LegacyHealthCheck()
		}
		if err := s.store.SetAppHealthCheck(r.Context(), a.ID, hc); err != nil {
			s.log.Error("default health check", "app", a.ID, "error", err)
		}
		created = append(created, a)
		out.Apps[i].ID = a.ID
	}
	for _, a := range created {
		s.audit(r, "app.create", "app", a.ID, a.Name, "compose "+a.Image)
	}
	writeJSON(w, http.StatusCreated, out)
}

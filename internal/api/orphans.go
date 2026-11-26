package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"panel/internal/executor"
)

// OrphanContainers lists and removes the containers of apps and databases the panel no
// longer knows (see executor.Orphans).
type OrphanContainers interface {
	List(ctx context.Context) ([]executor.Container, error)
	Remove(ctx context.Context, id string) (executor.Container, error)
}

// SetOrphans gives the server the orphan containers of the host.
func (s *Server) SetOrphans(o OrphanContainers) { s.orphans = o }

type orphanJSON struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Image      string    `json:"image"`
	Kind       string    `json:"kind"` // "app" or "database"
	ResourceID string    `json:"resource_id"`
	State      string    `json:"state"`
	Running    bool      `json:"running"`
	CreatedAt  time.Time `json:"created_at"`
}

// handleListOrphans answers the containers left behind by apps and databases the panel
// has forgotten. Without a container host the list is empty rather than an error: the
// page that shows it has nothing to say then.
func (s *Server) handleListOrphans(w http.ResponseWriter, r *http.Request) {
	out := []orphanJSON{}
	if s.orphans == nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	list, err := s.orphans.List(r.Context())
	if err != nil {
		s.log.Warn("list orphan containers", "error", err)
		writeJSON(w, http.StatusOK, out)
		return
	}
	for _, c := range list {
		out = append(out, orphanJSON{ID: c.ID, Name: c.Name, Image: c.Image, Kind: string(c.Kind), ResourceID: c.ResourceID, State: c.State, Running: c.Running, CreatedAt: c.CreatedAt})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleRemoveOrphan stops and removes one orphan container. Its volumes stay.
func (s *Server) handleRemoveOrphan(w http.ResponseWriter, r *http.Request) {
	if s.orphans == nil {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	c, err := s.orphans.Remove(r.Context(), r.PathValue("id"))
	if errors.Is(err, executor.ErrNotOrphan) {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	if err != nil {
		s.log.Error("remove orphan container", "container", c.Name, "error", err)
		writeError(w, http.StatusBadGateway, "docker_error", "the container could not be removed")
		return
	}
	s.audit(r, "container.orphan_removed", "container", c.ID, c.Name, string(c.Kind)+" "+c.ResourceID)
	w.WriteHeader(http.StatusNoContent)
}

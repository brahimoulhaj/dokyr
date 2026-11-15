package api

import (
	"context"
	"net/http"
	"time"
)

func (s *Server) handleAppProblems(w http.ResponseWriter, r *http.Request) {
	if !s.requireDeployer(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	list, err := s.deployer.AppProblems(ctx)
	if err != nil {
		s.log.Warn("check app runtime", "error", err)
		writeError(w, http.StatusServiceUnavailable, "runtime_unavailable", "App status could not be checked; check the Docker connection")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

package api

import "net/http"

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if len(query) > 256 {
		writeError(w, http.StatusBadRequest, "invalid_query", "Search is limited to 256 bytes")
		return
	}
	items, truncated, err := s.store.SearchResources(r.Context(), query)
	if err != nil {
		s.internal(w, "search resources", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "truncated": truncated})
}

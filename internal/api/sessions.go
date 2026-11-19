package api

import (
	"errors"
	"net/http"

	"panel/internal/store"
)

func (s *Server) browserSessionDetails(r *http.Request) store.SessionDetails {
	return store.SessionDetails{IPAddress: s.clientIP(r), UserAgent: r.UserAgent()[:min(len(r.UserAgent()), 1024)]}
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.store.UserSessions(r.Context(), userFrom(r.Context()).ID, sessionHash(r))
	if err != nil {
		s.internal(w, "list sessions", err)
		return
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (s *Server) handleDeleteOtherSessions(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) || !s.confirmLoginPassword(w, r, in.Password) {
		return
	}
	if err := s.store.DeleteOtherSessions(r.Context(), userFrom(r.Context()).ID, sessionHash(r)); err != nil {
		s.internal(w, "delete other sessions", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	userID, sessionID := userFrom(r.Context()).ID, r.PathValue("id")
	current, err := s.store.IsCurrentSession(r.Context(), userID, sessionID, sessionHash(r))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "session not found")
		} else {
			s.internal(w, "session lookup", err)
		}
		return
	}
	if !current {
		var in struct {
			Password string `json:"password"`
		}
		if !readJSON(w, r, &in) || !s.confirmLoginPassword(w, r, in.Password) {
			return
		}
	}
	if err := s.store.DeleteSessionByID(r.Context(), userID, sessionID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "session not found")
		} else {
			s.internal(w, "delete session", err)
		}
		return
	}
	if current {
		http.SetCookie(w, sessionCookie(r, "", -1))
	}
	w.WriteHeader(http.StatusNoContent)
}

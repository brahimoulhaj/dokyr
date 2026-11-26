package api

import (
	"errors"
	"net/http"
	"time"

	"panel/internal/proxy"
	"panel/internal/store"
)

type redirectJSON struct {
	ID        string    `json:"id"`
	Hostname  string    `json:"hostname"`
	Target    string    `json:"target"`
	TLSMode   string    `json:"tls_mode"`
	CreatedAt time.Time `json:"created_at"`
}

func toRedirectJSON(r store.Redirect) redirectJSON {
	return redirectJSON{ID: r.ID, Hostname: r.Hostname, Target: r.Target, TLSMode: r.TLSMode, CreatedAt: r.CreatedAt}
}

// handleListRedirects lists the hostnames that only send their visitors elsewhere.
func (s *Server) handleListRedirects(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListRedirects(r.Context())
	if err != nil {
		s.internal(w, "list redirects", err)
		return
	}
	writeJSON(w, http.StatusOK, mapSlice(list, toRedirectJSON))
}

type redirectInput struct {
	Hostname string `json:"hostname"` // the name that redirects: "www.example.com"
	Target   string `json:"target"`   // where to: "example.com"
	TLSMode  string `json:"tls_mode"` // of Hostname: "auto" (default) or "http_only"
}

// handleCreateRedirect makes a hostname answer every request with a permanent redirect
// to another hostname. The hostname must be free: no route, no other redirect, not the
// panel's or a built-in service's. The target is a plain hostname and not itself a
// redirect, so a chain or a loop cannot be built.
func (s *Server) handleCreateRedirect(w http.ResponseWriter, r *http.Request) {
	var in redirectInput
	if !readJSON(w, r, &in) {
		return
	}
	if in.TLSMode != "" && !store.ValidTLSMode(in.TLSMode) {
		invalid(w, `tls_mode must be "auto" or "http_only"`)
		return
	}
	host, _ := splitAddress(in.Hostname)
	target, _ := splitAddress(in.Target)
	if proxy.ValidHost(host) != nil || proxy.IsWildcard(host) {
		invalid(w, "hostname must be a name like www.example.com (no wildcard, no port)")
		return
	}
	if proxy.ValidHost(target) != nil || proxy.IsWildcard(target) {
		invalid(w, "target must be a name like example.com (no wildcard, no port, no path)")
		return
	}
	if host == target {
		invalid(w, "a hostname cannot redirect to itself")
		return
	}
	if s.hostReserved(w, r, host) {
		return
	}
	// A hostname the panel used to answer on still receives its old session cookies.
	if msg, err := s.retiredHostProblem(r, host, false); err != nil {
		s.internal(w, "check hostname", err)
		return
	} else if msg != "" {
		writeError(w, http.StatusForbidden, "forbidden", msg)
		return
	}
	if taken, err := s.store.HostRouted(r.Context(), host); err != nil {
		s.internal(w, "check hostname", err)
		return
	} else if taken {
		writeError(w, http.StatusConflict, "conflict", "this hostname already has routes or a redirect")
		return
	}
	if _, err := s.store.HostRedirect(r.Context(), target); err == nil {
		writeError(w, http.StatusConflict, "conflict", "the target is itself a redirect")
		return
	} else if !errors.Is(err, store.ErrNotFound) {
		s.internal(w, "check target", err)
		return
	}
	rd, err := s.store.CreateRedirect(r.Context(), host, target, in.TLSMode)
	if s.storeError(w, "create redirect", err, "this hostname already has routes or a redirect") {
		return
	}
	s.audit(r, "redirect.add", "redirect", rd.ID, rd.Hostname, rd.Target)
	s.proxyApplied(w, r, http.StatusCreated, toRedirectJSON(rd))
}

func (s *Server) handleDeleteRedirect(w http.ResponseWriter, r *http.Request) {
	rd, err := s.store.GetRedirect(r.Context(), r.PathValue("id"))
	if s.storeError(w, "get redirect", err, "") {
		return
	}
	if s.storeError(w, "delete redirect", s.store.DeleteRedirect(r.Context(), rd.ID), "") {
		return
	}
	s.audit(r, "redirect.remove", "redirect", rd.ID, rd.Hostname, rd.Target)
	s.applyProxy(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

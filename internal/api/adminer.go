package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"panel/internal/database"
)

func (s *Server) handleAdminer(w http.ResponseWriter, r *http.Request) {
	if !s.requireDatabases(w) {
		return
	}
	d, err := s.store.GetDBInstance(r.Context(), r.PathValue("id"))
	if s.storeError(w, "get database", err, "") {
		return
	}
	if d.Status != "running" || d.DesiredState != "running" || s.databases.Runtime(r.Context(), d.ID) != "running" {
		writeError(w, 409, "database_stopped", "Start the database before opening Adminer")
		return
	}
	credentials, err := s.store.DBInstanceCredentials(r.Context(), d.ID)
	if s.storeError(w, "database credentials", err, "") {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	address, key, err := s.databases.Adminer(ctx)
	if s.databaseError(w, err) {
		return
	}
	driver := "server"
	if d.Engine == "postgres" {
		driver = "pgsql"
	}
	connection, _ := json.Marshal(map[string]string{"driver": driver, "server": database.ContainerName(d.ID), "username": d.Username, "password": credentials["password"], "database": d.DatabaseName})
	target, _ := url.Parse(address)
	prefix := "/api/databases/" + d.ID + "/adminer/"
	proxy := adminerProxy(target, key, base64.StdEncoding.EncodeToString(connection), prefix)
	if r.URL.RawQuery == "" && r.Method == "GET" {
		s.audit(r, "database.adminer", "database", d.ID, d.Name, "Opened Adminer as "+d.Username)
	}
	proxy.ServeHTTP(w, r)
}

// adminerProxy forwards only Adminer's own cookies and trusted connection metadata.
func adminerProxy(target *url.URL, key, connection, prefix string) *httputil.ReverseProxy {
	// Cookies are scoped to the instance, and the panel session never reaches PHP.
	return &httputil.ReverseProxy{
		Rewrite: func(p *httputil.ProxyRequest) {
			p.SetURL(target)
			p.Out.URL.Path = "/index.php"
			p.Out.URL.RawPath = ""
			p.Out.Header.Del("Authorization")
			p.Out.Header.Del("Cookie")
			for _, c := range p.In.Cookies() {
				if c.Name == "panel_adminer" || strings.HasPrefix(c.Name, "adminer_") {
					p.Out.AddCookie(c)
				}
			}
			p.Out.Header.Set("X-Panel-Gateway", key)
			p.Out.Header.Set("X-Panel-Connection", connection)
			p.Out.Header.Set("X-Panel-Path", prefix)
			p.Out.Header.Set("X-Panel-Https", "0")
			if p.In.TLS != nil || p.In.Header.Get("X-Forwarded-Proto") == "https" {
				p.Out.Header.Set("X-Panel-Https", "1")
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			resp.Header.Set("Cache-Control", "no-store")
			// Not "no-referrer": with it, browsers send "Origin: null" on Adminer's form
			// POSTs (edit, delete) and sameOrigin rejects them as cross-origin.
			// "same-origin" still keeps Adminer URLs from leaking to other sites.
			resp.Header.Set("Referrer-Policy", "same-origin")
			cookies := resp.Cookies()
			resp.Header.Del("Set-Cookie")
			for _, c := range cookies {
				if c.Name == "panel_adminer" || strings.HasPrefix(c.Name, "adminer_") {
					c.Path, c.Domain, c.HttpOnly, c.SameSite = prefix, "", true, http.SameSiteStrictMode
					resp.Header.Add("Set-Cookie", c.String())
				}
			}
			if location := resp.Header.Get("Location"); location != "" {
				u, err := url.Parse(location)
				if err == nil && (u.Host == "" || u.Host == target.Host) {
					u.Scheme, u.Host = "", ""
					if strings.HasPrefix(u.Path, "/") || u.Path == "index.php" {
						u.Path = prefix
					}
					resp.Header.Set("Location", u.String())
				}
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, _ error) {
			writeError(w, 502, "adminer_unavailable", "Adminer is unavailable; try opening it again")
		},
	}
}

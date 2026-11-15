package api

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"

	"panel/internal/store"
)

// S3Tester checks a connection before it is saved; storage.Test is the real one.
type S3Tester func(ctx context.Context, c store.S3Input) error

// SetS3Tester replaces the connection check (tests); nil saves connections unchecked.
func (s *Server) SetS3Tester(t S3Tester) { s.s3Test = t }

type s3JSON struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Endpoint  string    `json:"endpoint"`
	Region    string    `json:"region"`
	Bucket    string    `json:"bucket"`
	Prefix    string    `json:"prefix"`
	AccessKey string    `json:"access_key"`
	UseSSL    bool      `json:"use_ssl"`
	PathStyle bool      `json:"path_style"`
	CreatedAt time.Time `json:"created_at"`
}

func toS3JSON(c store.S3Connection) s3JSON {
	return s3JSON{ID: c.ID, Name: c.Name, Endpoint: c.Endpoint, Region: c.Region, Bucket: c.Bucket, Prefix: c.Prefix,
		AccessKey: c.AccessKey, UseSSL: c.UseSSL, PathStyle: c.PathStyle, CreatedAt: c.CreatedAt}
}

var (
	// s3EndpointRe is a host with an optional port: no scheme, no path.
	s3EndpointRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*(:[0-9]{1,5})?$`)
	s3BucketRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
	s3RegionRe   = regexp.MustCompile(`^[a-z0-9-]{0,32}$`)
	s3PrefixRe   = regexp.MustCompile(`^[A-Za-z0-9._/-]{0,200}$`)
)

func (s *Server) handleListS3(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListS3Connections(r.Context())
	if err != nil {
		s.internal(w, "list s3 connections", err)
		return
	}
	writeJSON(w, http.StatusOK, mapSlice(list, toS3JSON))
}

// handleAddS3 checks the connection (bucket exists, keys can write), then stores it; the
// secret key is encrypted and never returned.
func (s *Server) handleAddS3(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name      string `json:"name"`
		Endpoint  string `json:"endpoint"`
		Region    string `json:"region"`
		Bucket    string `json:"bucket"`
		Prefix    string `json:"prefix"`
		AccessKey string `json:"access_key"`
		SecretKey string `json:"secret_key"`
		UseSSL    *bool  `json:"use_ssl"`
		PathStyle bool   `json:"path_style"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	endpoint := strings.ToLower(strings.TrimSpace(in.Endpoint))
	useSSL := in.UseSSL == nil || *in.UseSSL
	if after, ok := strings.CutPrefix(endpoint, "https://"); ok {
		endpoint, useSSL = after, true
	} else if after, ok := strings.CutPrefix(endpoint, "http://"); ok {
		endpoint, useSSL = after, false
	}
	endpoint = strings.TrimSuffix(endpoint, "/")
	c := store.S3Input{S3Connection: store.S3Connection{
		Name: strings.TrimSpace(in.Name), Endpoint: endpoint, Region: strings.TrimSpace(in.Region),
		Bucket: strings.TrimSpace(in.Bucket), Prefix: strings.Trim(strings.TrimSpace(in.Prefix), "/"),
		AccessKey: strings.TrimSpace(in.AccessKey), UseSSL: useSSL, PathStyle: in.PathStyle,
	}, SecretKey: in.SecretKey}
	if c.Name == "" {
		c.Name = c.Bucket
	}
	switch {
	case len(c.Endpoint) > 253 || !s3EndpointRe.MatchString(c.Endpoint):
		invalid(w, "endpoint must be a host like s3.amazonaws.com or minio.example.com:9000")
		return
	case !s3BucketRe.MatchString(c.Bucket):
		invalid(w, "bucket must be a valid bucket name (3–63 lowercase letters, digits, dots and dashes)")
		return
	case !s3RegionRe.MatchString(c.Region):
		invalid(w, "region must be like us-east-1, or empty")
		return
	case !s3PrefixRe.MatchString(c.Prefix) || strings.Contains(c.Prefix, ".."):
		invalid(w, "prefix must be a folder path like backups/panel, or empty")
		return
	case c.AccessKey == "" || len(c.AccessKey) > 256 || strings.ContainsAny(c.AccessKey, " \t\r\n"):
		invalid(w, "access_key is required")
		return
	case c.SecretKey == "" || len(c.SecretKey) > 1024 || strings.ContainsAny(c.SecretKey, "\r\n"):
		invalid(w, "secret_key is required")
		return
	case len(c.Name) > 64:
		invalid(w, "name must be at most 64 characters")
		return
	}
	if s.s3Test != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		err := s.s3Test(ctx, c)
		cancel()
		if err != nil {
			writeError(w, http.StatusBadGateway, "s3", "the connection check failed: "+err.Error())
			return
		}
	}
	conn, err := s.store.CreateS3Connection(r.Context(), c)
	if err != nil {
		s.internal(w, "add s3 connection", err)
		return
	}
	s.audit(r, "s3.add", "s3", conn.ID, conn.Name, conn.Bucket+"@"+conn.Endpoint)
	writeJSON(w, http.StatusCreated, toS3JSON(conn))
}

// handleTestS3 runs the check again on a saved connection.
func (s *Server) handleTestS3(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.S3Secret(r.Context(), r.PathValue("id"))
	if s.storeError(w, "get s3 connection", err, "") {
		return
	}
	if s.s3Test != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		err := s.s3Test(ctx, c)
		cancel()
		if err != nil {
			writeError(w, http.StatusBadGateway, "s3", "the connection check failed: "+err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteS3(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.GetS3Connection(r.Context(), r.PathValue("id"))
	if s.storeError(w, "get s3 connection", err, "") {
		return
	}
	err = s.store.DeleteS3Connection(r.Context(), c.ID)
	if s.storeError(w, "delete s3 connection", err, "this connection is used by the registry, backup schedules or saved database backups") {
		return
	}
	s.audit(r, "s3.remove", "s3", c.ID, c.Name, c.Bucket+"@"+c.Endpoint)
	w.WriteHeader(http.StatusNoContent)
}

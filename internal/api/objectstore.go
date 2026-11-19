package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"panel/internal/objectstore"
	"panel/internal/proxy"
	"panel/internal/store"
)

// ObjectStore runs the built-in S3 storage.
type ObjectStore interface {
	Settings(ctx context.Context) (store.ObjectStoreSettings, error)
	Configure(ctx context.Context, enabled bool, domain, tlsMode string) (store.ObjectStoreSettings, error)
	Apply(ctx context.Context) error
	EnsureConnection(ctx context.Context) error
	State(ctx context.Context) string
	Endpoint() string
	Buckets(ctx context.Context) ([]objectstore.Bucket, error)
	CreateBucket(ctx context.Context, name string) error
	DeleteBucket(ctx context.Context, name string) error
	AccessKeys(ctx context.Context, bucket string) ([]store.S3AccessKey, error)
	CreateAccessKey(ctx context.Context, name, bucket, permission string) (store.S3AccessKey, string, error)
	DeleteAccessKey(ctx context.Context, keyID string) (store.S3AccessKey, error)
}

func (s *Server) SetObjectStore(o ObjectStore) { s.objectStore = o }

type objectStoreJSON struct {
	Enabled      bool   `json:"enabled"`
	Domain       string `json:"domain"`
	TLSMode      string `json:"tls_mode"`
	State        string `json:"state"`
	Endpoint     string `json:"endpoint"`
	ConnectionID string `json:"connection_id"`
	Image        string `json:"image,omitempty"`
}

func (s *Server) requireObjectStore(w http.ResponseWriter) bool {
	if s.objectStore == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "built-in storage is not available")
		return false
	}
	return true
}

func (s *Server) objectStoreJSON(ctx context.Context) (objectStoreJSON, error) {
	cfg, err := s.objectStore.Settings(ctx)
	if err != nil {
		return objectStoreJSON{}, err
	}
	out := objectStoreJSON{Enabled: cfg.Enabled, Domain: cfg.Domain, TLSMode: cfg.TLSMode, ConnectionID: cfg.ConnectionID,
		State: s.objectStore.State(ctx), Endpoint: s.objectStore.Endpoint()}
	if out.TLSMode == "" {
		out.TLSMode = store.TLSAuto
	}
	return out, nil
}

func (s *Server) handleGetObjectStore(w http.ResponseWriter, r *http.Request) {
	if !s.requireObjectStore(w) {
		return
	}
	out, err := s.objectStoreJSON(r.Context())
	if err != nil {
		s.internal(w, "object storage settings", err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePutObjectStore(w http.ResponseWriter, r *http.Request) {
	if !s.requireObjectStore(w) {
		return
	}
	var in struct {
		Enabled bool   `json:"enabled"`
		Domain  string `json:"domain"`
		TLSMode string `json:"tls_mode"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	ctx := r.Context()
	in.Domain = strings.ToLower(strings.TrimSpace(in.Domain))
	if in.TLSMode == "" {
		in.TLSMode = store.TLSAuto
	}
	if !store.ValidTLSMode(in.TLSMode) {
		invalid(w, `tls_mode must be "auto" or "http_only"`)
		return
	}
	if in.Domain != "" {
		if err := proxy.ValidHost(in.Domain); err != nil {
			invalid(w, "domain must be a hostname of its own like s3.example.com")
			return
		}
		panelDomain, _, err := s.store.GetSetting(ctx, store.SettingPanelDomain)
		if err != nil {
			s.internal(w, "object storage settings", err)
			return
		}
		rs, err := s.store.GetRegistrySettings(ctx)
		if err != nil {
			s.internal(w, "object storage settings", err)
			return
		}
		if in.Domain == panelDomain || (rs.Enabled && in.Domain == rs.Domain) {
			writeError(w, http.StatusConflict, "conflict", "this hostname is used by the panel or the registry; the storage needs one of its own")
			return
		}
		if taken, err := s.store.DomainExists(ctx, in.Domain); err != nil {
			s.internal(w, "object storage settings", err)
			return
		} else if taken {
			writeError(w, http.StatusConflict, "conflict", "this hostname is already used by an app")
			return
		}
	}
	if _, err := s.objectStore.Configure(ctx, in.Enabled, in.Domain, in.TLSMode); err != nil {
		s.internal(w, "object storage settings", err)
		return
	}
	detail := "off (data kept)"
	if in.Enabled {
		detail = "on"
		if in.Domain != "" {
			detail += ", " + in.Domain
		}
	}
	s.audit(r, "settings.object_store", "settings", "object-store", in.Domain, detail)

	// Starting it may pull the image first: give it time, but not forever.
	applyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
	defer cancel()
	if err := s.objectStore.Apply(applyCtx); err != nil {
		s.log.Error("apply object storage", "error", err)
		writeError(w, http.StatusBadGateway, "object_store", "saved, but the storage could not be started: "+err.Error())
		return
	}
	if in.Enabled {
		// The bucket and connection follow once the storage answers (a few seconds).
		go func() {
			bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
			defer cancel()
			for bg.Err() == nil {
				if s.objectStore.EnsureConnection(bg) == nil {
					return
				}
				time.Sleep(time.Second)
			}
		}()
	}
	if !s.applyProxy(ctx) {
		writeError(w, http.StatusServiceUnavailable, "proxy_unavailable",
			"saved, but the proxy could not be updated; it is applied when the proxy is back")
		return
	}
	s.handleGetObjectStore(w, r)
}

func (s *Server) objectStoreError(w http.ResponseWriter, what string, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, objectstore.ErrOff):
		writeError(w, http.StatusConflict, "off", "turn the built-in storage on first")
	case errors.Is(err, objectstore.ErrInvalidName):
		invalid(w, err.Error())
	case errors.Is(err, objectstore.ErrNotEmpty):
		writeError(w, http.StatusConflict, "not_empty", "the bucket still holds objects; empty it first")
	case errors.Is(err, objectstore.ErrInvalidKey):
		invalid(w, err.Error())
	case errors.Is(err, objectstore.ErrNoBucket):
		invalid(w, "no such bucket")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "not found")
	case errors.Is(err, store.ErrConflict):
		msg := "a bucket with this name exists"
		if m := err.Error(); m != store.ErrConflict.Error() {
			msg = strings.TrimPrefix(m, store.ErrConflict.Error()+": ")
		}
		writeError(w, http.StatusConflict, "conflict", msg)
	default:
		s.log.Warn(what, "error", err)
		writeError(w, http.StatusBadGateway, "storage_error", "the storage did not answer: "+err.Error())
	}
	return true
}

func (s *Server) handleListBuckets(w http.ResponseWriter, r *http.Request) {
	if !s.requireObjectStore(w) {
		return
	}
	buckets, err := s.objectStore.Buckets(r.Context())
	if s.objectStoreError(w, "list buckets", err) {
		return
	}
	writeJSON(w, http.StatusOK, buckets)
}

func (s *Server) handleCreateBucket(w http.ResponseWriter, r *http.Request) {
	if !s.requireObjectStore(w) {
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if s.objectStoreError(w, "create bucket", s.objectStore.CreateBucket(r.Context(), in.Name)) {
		return
	}
	s.audit(r, "object_store.bucket_create", "bucket", in.Name, in.Name, "")
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) handleDeleteBucket(w http.ResponseWriter, r *http.Request) {
	if !s.requireObjectStore(w) {
		return
	}
	name := r.PathValue("name")
	if s.objectStoreError(w, "delete bucket", s.objectStore.DeleteBucket(r.Context(), name)) {
		return
	}
	s.audit(r, "object_store.bucket_delete", "bucket", name, name, "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListAccessKeys(w http.ResponseWriter, r *http.Request) {
	if !s.requireObjectStore(w) {
		return
	}
	keys, err := s.objectStore.AccessKeys(r.Context(), r.URL.Query().Get("bucket"))
	if err != nil {
		s.internal(w, "list access keys", err)
		return
	}
	writeJSON(w, http.StatusOK, keys)
}

// handleCreateAccessKey makes a key for one bucket; its secret is in this answer only.
func (s *Server) handleCreateAccessKey(w http.ResponseWriter, r *http.Request) {
	if !s.requireObjectStore(w) {
		return
	}
	var in struct {
		Name       string `json:"name"`
		Bucket     string `json:"bucket"`
		Permission string `json:"permission"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	key, secret, err := s.objectStore.CreateAccessKey(r.Context(), in.Name, strings.TrimSpace(in.Bucket), in.Permission)
	if s.objectStoreError(w, "create access key", err) {
		return
	}
	s.audit(r, "object_store.key_create", "bucket", key.Bucket, key.Name, key.Permission+" on "+key.Bucket+", "+key.AccessKey)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, struct {
		store.S3AccessKey
		SecretKey string `json:"secret_key"`
	}{key, secret})
}

func (s *Server) handleDeleteAccessKey(w http.ResponseWriter, r *http.Request) {
	if !s.requireObjectStore(w) {
		return
	}
	key, err := s.objectStore.DeleteAccessKey(r.Context(), r.PathValue("id"))
	if s.objectStoreError(w, "delete access key", err) {
		return
	}
	s.audit(r, "object_store.key_delete", "bucket", key.Bucket, key.Name, key.AccessKey)
	w.WriteHeader(http.StatusNoContent)
}

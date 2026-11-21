package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"panel/internal/proxy"
	"panel/internal/registry"
	"panel/internal/store"
)

// BuiltinRegistry is the built-in registry; *registry.Service implements it.
type BuiltinRegistry interface {
	Apply(ctx context.Context) error
	State(ctx context.Context) string
	SystemUser() string
	Address(rs store.RegistrySettings) string
	Token(ctx context.Context, user, password, service string, scopes []string) (string, error)
	CheckEvents(authorization string) bool
	Repositories(ctx context.Context) ([]string, error)
	Tags(ctx context.Context, repo string) ([]string, error)
	TagDetail(ctx context.Context, repo, tag string) (registry.TagDetail, error)
	DeleteTag(ctx context.Context, repo, tag string) error
	GarbageCollect(ctx context.Context) (string, error)
}

// SetRegistry connects the built-in registry; without one its endpoints answer 503.
func (s *Server) SetRegistry(r BuiltinRegistry) { s.registry = r }

func (s *Server) requireRegistry(w http.ResponseWriter) bool {
	if s.registry == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "the built-in registry is not available in this panel")
		return false
	}
	return true
}

// registryDomain is the built-in registry's domain while it is on; "" otherwise.
func (s *Server) registryDomain(ctx context.Context) string {
	rs, err := s.store.GetRegistrySettings(ctx)
	if err != nil || !rs.Enabled {
		return ""
	}
	return rs.Domain
}

type registrySettingsJSON struct {
	Enabled      bool   `json:"enabled"`
	Domain       string `json:"domain"`
	BuildCache   bool   `json:"build_cache"`
	TLSMode      string `json:"tls_mode"`          // "auto" or "http_only"
	S3Connection string `json:"s3_connection_id"`  // "" = a Docker volume on this server
	Address      string `json:"address,omitempty"` // domain[:port], what docker login and image names use
	State        string `json:"state,omitempty"`   // the container: off, running, missing, …
	SystemUser   string `json:"system_user,omitempty"`
}

func (s *Server) handleGetRegistrySettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireRegistry(w) {
		return
	}
	rs, err := s.store.GetRegistrySettings(r.Context())
	if err != nil {
		s.internal(w, "registry settings", err)
		return
	}
	conn, _, err := s.store.GetSetting(r.Context(), store.SettingRegistryS3)
	if err != nil {
		s.internal(w, "registry settings", err)
		return
	}
	mode := rs.TLSMode
	if mode == "" {
		mode = store.TLSAuto
	}
	writeJSON(w, http.StatusOK, registrySettingsJSON{Enabled: rs.Enabled, Domain: rs.Domain, BuildCache: rs.BuildCache, TLSMode: mode,
		S3Connection: conn, Address: s.registry.Address(rs), State: s.registry.State(r.Context()), SystemUser: s.registry.SystemUser()})
}

// handlePutRegistrySettings saves the settings, then starts, recreates or removes the
// container and updates the proxy. Changing the storage does not move images.
func (s *Server) handlePutRegistrySettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireRegistry(w) {
		return
	}
	var in registrySettingsJSON
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
	if in.Domain != "" || in.Enabled {
		if err := proxy.ValidHost(in.Domain); err != nil {
			invalid(w, "domain must be a hostname of its own like registry.example.com")
			return
		}
		panelDomain, _, err := s.store.GetSetting(ctx, store.SettingPanelDomain)
		if err != nil {
			s.internal(w, "registry settings", err)
			return
		}
		if in.Domain == panelDomain {
			writeError(w, http.StatusConflict, "conflict", "this hostname is the panel's own domain; the registry needs one of its own")
			return
		}
		if obs, err := s.store.GetObjectStoreSettings(ctx); err != nil {
			s.internal(w, "registry settings", err)
			return
		} else if obs.Domain != "" && in.Domain == obs.Domain {
			writeError(w, http.StatusConflict, "conflict", "this hostname is the built-in storage's domain; the registry needs one of its own")
			return
		}
		if taken, err := s.store.HostRouted(ctx, in.Domain); err != nil {
			s.internal(w, "registry settings", err)
			return
		} else if taken {
			writeError(w, http.StatusConflict, "conflict", "this hostname is already used by an app")
			return
		}
	}
	if in.S3Connection != "" {
		if _, err := s.store.GetS3Connection(ctx, in.S3Connection); errors.Is(err, store.ErrNotFound) {
			invalid(w, "s3_connection_id is not an S3 connection of this panel")
			return
		} else if err != nil {
			s.internal(w, "registry settings", err)
			return
		}
	}
	if err := s.store.SetRegistrySettings(ctx, store.RegistrySettings{Enabled: in.Enabled, Domain: in.Domain, BuildCache: in.BuildCache, TLSMode: in.TLSMode}); err != nil {
		s.internal(w, "registry settings", err)
		return
	}
	var err error
	if in.S3Connection == "" {
		err = s.store.DeleteSetting(ctx, store.SettingRegistryS3)
	} else {
		err = s.store.SetSetting(ctx, store.SettingRegistryS3, in.S3Connection)
	}
	if err != nil {
		s.internal(w, "registry settings", err)
		return
	}
	detail := "off"
	if in.Enabled {
		detail = "on, " + in.Domain
	}
	s.audit(r, "settings.registry", "settings", "registry", in.Domain, detail)

	// Starting it may pull the image first: give it time, but not forever.
	applyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Minute)
	defer cancel()
	if err := s.registry.Apply(applyCtx); err != nil {
		s.log.Error("apply registry", "error", err)
		writeError(w, http.StatusBadGateway, "registry", "saved, but the registry could not be started: "+err.Error())
		return
	}
	if !s.applyProxy(ctx) {
		writeError(w, http.StatusServiceUnavailable, "proxy_unavailable",
			"saved, but the proxy could not be updated; it is applied when the proxy is back")
		return
	}
	in.State = s.registry.State(ctx)
	in.SystemUser = s.registry.SystemUser()
	writeJSON(w, http.StatusOK, in)
}

// ---- tokens ----

type registryTokenJSON struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Scope      string     `json:"scope"`
	Token      string     `json:"token,omitempty"` // only in the answer that creates it
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

func toRegistryTokenJSON(t store.RegistryToken) registryTokenJSON {
	out := registryTokenJSON{ID: t.ID, Name: t.Name, Scope: t.Scope, CreatedAt: t.CreatedAt}
	if !t.LastUsedAt.IsZero() {
		out.LastUsedAt = &t.LastUsedAt
	}
	return out
}

var registryTokenNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

func (s *Server) handleListRegistryTokens(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListRegistryTokens(r.Context())
	if err != nil {
		s.internal(w, "list registry tokens", err)
		return
	}
	writeJSON(w, http.StatusOK, mapSlice(list, toRegistryTokenJSON))
}

func (s *Server) handleCreateRegistryToken(w http.ResponseWriter, r *http.Request) {
	if !s.requireRegistry(w) {
		return
	}
	var in struct {
		Name  string `json:"name"`
		Scope string `json:"scope"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	name := strings.ToLower(strings.TrimSpace(in.Name))
	switch {
	case !registryTokenNameRe.MatchString(name):
		invalid(w, "name must be 1 to 64 lowercase letters, digits, dots, dashes or underscores; it is the docker login username")
		return
	case name == s.registry.SystemUser():
		invalid(w, name+" is the panel's own account; choose another name")
		return
	case in.Scope != store.RegistryPull && in.Scope != store.RegistryPush:
		invalid(w, `scope must be "pull" or "push"`)
		return
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		s.internal(w, "registry token", err)
		return
	}
	secret := "rgt_" + base64.RawURLEncoding.EncodeToString(raw)
	t, err := s.store.CreateRegistryToken(r.Context(), name, in.Scope, secret)
	if s.storeError(w, "create registry token", err, "a registry token with this name exists") {
		return
	}
	s.audit(r, "registry_token.create", "registry_token", t.ID, t.Name, t.Scope)
	out := toRegistryTokenJSON(t)
	out.Token = secret
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) handleDeleteRegistryToken(w http.ResponseWriter, r *http.Request) {
	err := s.store.DeleteRegistryToken(r.Context(), r.PathValue("id"))
	if s.storeError(w, "delete registry token", err, "") {
		return
	}
	s.audit(r, "registry_token.delete", "registry_token", r.PathValue("id"), "", "")
	w.WriteHeader(http.StatusNoContent)
}

// ---- token endpoint (called by docker, on the registry's domain) ----

// handleRegistryToken is the registry's token server: docker sends the login with Basic
// auth and the scopes it needs. Failed logins are rate limited per address.
func (s *Server) handleRegistryToken(w http.ResponseWriter, r *http.Request) {
	if !s.requireRegistry(w) {
		return
	}
	if err := r.ParseForm(); err != nil {
		invalid(w, "bad form")
		return
	}
	user, password := registry.Credentials(r)
	tok, err := s.registry.Token(r.Context(), user, password, r.Form.Get("service"), r.Form["scope"])
	switch {
	case errors.Is(err, registry.ErrDenied):
		if ok, retry := s.authRate.allow("registry:" + s.clientIP(r)); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
			writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts, try again later")
			return
		}
		writeJSON(w, http.StatusUnauthorized, map[string]any{"errors": []any{map[string]string{"code": "UNAUTHORIZED", "message": "wrong username or token"}}})
		return
	case err != nil:
		writeError(w, http.StatusBadRequest, "registry", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token": tok, "access_token": tok, "expires_in": 300, "issued_at": time.Now().UTC().Format(time.RFC3339),
	})
}

// handleRegistryEvents takes the registry's push reports and deploys the image apps that
// follow the pushed tag (with deploy on push on).
func (s *Server) handleRegistryEvents(w http.ResponseWriter, r *http.Request) {
	if !s.requireRegistry(w) {
		return
	}
	if !s.registry.CheckEvents(r.Header.Get("Authorization")) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		invalid(w, "bad body")
		return
	}
	pushes, err := registry.ParseEvents(body)
	if err != nil {
		invalid(w, "bad events")
		return
	}
	// Answer at once: the registry waits on this call. The deploys start in the background.
	w.WriteHeader(http.StatusOK)
	domain := s.registryDomain(r.Context())
	if domain == "" || s.deployer == nil {
		return
	}
	ctx := context.WithoutCancel(r.Context())
	for _, p := range pushes {
		apps, err := s.store.AppsByImage(ctx, domain+"/"+p.Repository+":"+p.Tag)
		if err != nil {
			s.log.Error("registry push: find apps", "error", err)
			continue
		}
		for _, a := range apps {
			if !a.PushDeploy {
				continue
			}
			if _, _, err := s.deployer.DeployOrQueue(ctx, a.ID, "registry"); err != nil {
				s.log.Warn("registry push: deploy", "app", a.Name, "error", err)
			}
		}
	}
}

// ---- repositories ----

type repositoryJSON struct {
	Name string    `json:"name"`
	Size int64     `json:"size"` // stored size of all its tags, shared layers counted once
	Tags []tagJSON `json:"tags"`
}

type tagJSON struct {
	Name      string   `json:"name"`
	Digest    string   `json:"digest,omitempty"`
	Size      int64    `json:"size"`
	Platforms []string `json:"platforms"`
}

func (s *Server) handleListRepositories(w http.ResponseWriter, r *http.Request) {
	if !s.requireRegistry(w) {
		return
	}
	if s.registryDomain(r.Context()) == "" {
		writeJSON(w, http.StatusOK, []repositoryJSON{})
		return
	}
	repos, err := s.registry.Repositories(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "registry", err.Error())
		return
	}
	out := make([]repositoryJSON, 0, len(repos))
	for _, name := range repos {
		tags, err := s.registry.Tags(r.Context(), name)
		if err != nil {
			writeError(w, http.StatusBadGateway, "registry", err.Error())
			return
		}
		if len(tags) == 0 {
			continue // every tag deleted: nothing to show until garbage collection
		}
		rj := repositoryJSON{Name: name, Tags: make([]tagJSON, 0, len(tags))}
		blobs := map[string]int64{}
		for _, tag := range tags {
			tj := tagJSON{Name: tag, Platforms: []string{}}
			// A tag whose manifest cannot be read is still listed, without its details.
			if d, err := s.registry.TagDetail(r.Context(), name, tag); err == nil {
				tj.Digest, tj.Size, tj.Platforms = d.Digest, d.Size, d.Platforms
				for digest, size := range d.Blobs {
					blobs[digest] = size
				}
			} else {
				s.log.Warn("registry tag detail", "repo", name, "tag", tag, "err", err)
			}
			rj.Tags = append(rj.Tags, tj)
		}
		for _, size := range blobs {
			rj.Size += size
		}
		out = append(out, rj)
	}
	writeJSON(w, http.StatusOK, out)
}

var (
	repoNameRe = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*(/[a-z0-9]+([._-][a-z0-9]+)*)*$`)
	repoTagRe  = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)
)

func (s *Server) handleDeleteRepositoryTag(w http.ResponseWriter, r *http.Request) {
	if !s.requireRegistry(w) {
		return
	}
	repo, tag := r.PathValue("repo"), r.URL.Query().Get("tag")
	if !repoNameRe.MatchString(repo) || !repoTagRe.MatchString(tag) {
		invalid(w, "repository or tag is not valid")
		return
	}
	err := s.registry.DeleteTag(r.Context(), repo, tag)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "registry", err.Error())
		return
	}
	s.audit(r, "registry.tag_delete", "registry", repo, repo+":"+tag, "")
	w.WriteHeader(http.StatusNoContent)
}

// handleRegistryGC frees the space of deleted images; the registry pauses meanwhile.
func (s *Server) handleRegistryGC(w http.ResponseWriter, r *http.Request) {
	if !s.requireRegistry(w) {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Minute)
	defer cancel()
	out, err := s.registry.GarbageCollect(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, "registry", err.Error())
		return
	}
	s.audit(r, "registry.gc", "registry", "registry", "", "")
	writeJSON(w, http.StatusOK, map[string]string{"output": out})
}

// handleSetPushDeploy turns deploy-on-push on or off for one app.
func (s *Server) handleSetPushDeploy(w http.ResponseWriter, r *http.Request) {
	var in struct {
		On bool `json:"on"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	a, err := s.store.GetApp(r.Context(), r.PathValue("id"))
	if s.storeError(w, "get app", err, "") {
		return
	}
	if err := s.store.SetAppPushDeploy(r.Context(), a.ID, in.On); s.storeError(w, "set push deploy", err, "") {
		return
	}
	s.audit(r, "app.push_deploy", "app", a.ID, a.Name, map[bool]string{true: "on", false: "off"}[in.On])
	a.PushDeploy = in.On
	writeJSON(w, http.StatusOK, toAppJSON(a))
}

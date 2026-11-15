// Package registry runs the built-in image registry (CNCF Distribution) in a container of
// its own, like the proxy: off until enabled in Settings, served on its own domain through
// Caddy, storing images in a Docker volume or an S3 connection. The panel is its token
// server: docker login checks a registry token here, and the registry trusts the JWTs the
// panel signs. Pushes are reported to the panel, which deploys the apps on that tag.
package registry

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"panel/internal/executor"
	"panel/internal/store"
)

const (
	resourceID   = "registry"      // its resource id in the container labels
	specLabel    = "registry.spec" // hash of the container spec and config; a change recreates it
	internalPort = 5000
	// TokenPath is where the panel answers token requests, on the registry's own domain
	// (Caddy sends that path to the panel).
	TokenPath = "/api/registry/token"
	// EventsPath is where the registry reports pushes, over the shared network.
	EventsPath = "/api/registry/events"
	tokenTTL   = 5 * time.Minute
)

// IsRegistry reports whether a managed container is the registry; startup reconciliation uses it.
func IsRegistry(kind executor.Kind, id string) bool {
	return kind == executor.KindService && id == resourceID
}

type Options struct {
	Executor executor.Executor
	Store    *store.Store
	Log      *slog.Logger
	Name     string // container name; also its host name on the shared network
	Image    string
	Dir      string // keys and config, as the panel sees it
	// Volume and VolumePath: Dir lives in this named volume (the panel runs in a container).
	Volume     string
	VolumePath string
	DataVolume string // the named volume images are kept in, without an S3 connection
	// PanelUpstream is host:port where the registry reaches the panel to report pushes.
	PanelUpstream string
	Issuer        string // token issuer and the system account's name
	// PublishPort publishes the registry on 127.0.0.1 of the host (development, where the
	// panel runs off the shared network); 0 = not published.
	PublishPort int
	// Realm replaces the token endpoint's URL (https://<domain>/api/registry/token); tests
	// point it at a local server.
	Realm string
}

// Service runs the registry and answers its token and event requests.
type Service struct {
	opts   Options
	signer *signer
	system string // password of the panel's own account (deploy pulls, build pushes)
	events string // shared secret of push reports

	mu sync.Mutex // serialises Apply
}

func New(opts Options) (*Service, error) {
	if opts.Executor == nil || opts.Store == nil || opts.Log == nil || opts.Name == "" || opts.Image == "" || opts.Issuer == "" {
		return nil, errors.New("registry: executor, store, log, name, image and issuer are required")
	}
	dir, err := filepath.Abs(opts.Dir)
	if err != nil {
		return nil, err
	}
	opts.Dir = dir
	sg, err := loadSigner(dir, opts.Issuer)
	if err != nil {
		return nil, err
	}
	system, err := loadSecret(filepath.Join(dir, "system.secret"))
	if err != nil {
		return nil, err
	}
	events, err := loadSecret(filepath.Join(dir, "events.secret"))
	if err != nil {
		return nil, err
	}
	return &Service{opts: opts, signer: sg, system: system, events: events}, nil
}

// loadSecret reads a random secret from path, creating it on first use.
func loadSecret(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil && len(b) >= 32 {
		return strings.TrimSpace(string(b)), nil
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	v := hex.EncodeToString(raw)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	return v, os.WriteFile(path, []byte(v), 0o600)
}

// SystemUser is the account the panel itself signs in with.
func (s *Service) SystemUser() string { return s.opts.Issuer }

// Settings returns the saved settings.
func (s *Service) Settings(ctx context.Context) (store.RegistrySettings, error) {
	return s.opts.Store.GetRegistrySettings(ctx)
}

// Active returns the settings when the registry is on, with a domain.
func (s *Service) Active(ctx context.Context) (store.RegistrySettings, bool) {
	rs, err := s.Settings(ctx)
	return rs, err == nil && rs.Enabled && rs.Domain != ""
}

// Upstream is where Caddy and the panel reach the registry.
func (s *Service) Upstream() string {
	return fmt.Sprintf("%s:%d", s.opts.Name, internalPort)
}

// baseURL is where the panel itself calls the registry's API.
func (s *Service) baseURL() string {
	if s.opts.PublishPort > 0 {
		return fmt.Sprintf("http://127.0.0.1:%d", s.opts.PublishPort)
	}
	return "http://" + s.Upstream()
}

// PullAuth returns the panel's credentials for an image on the built-in registry, and nil
// for any other image.
func (s *Service) PullAuth(ctx context.Context, image string) *executor.RegistryAuth {
	rs, ok := s.Active(ctx)
	if !ok || store.ImageServer(image) != rs.Domain {
		return nil
	}
	return &executor.RegistryAuth{Server: rs.Domain, Username: s.SystemUser(), Password: s.system}
}

// Credentials reads the login of a token request: Basic auth (docker's GET), or the
// OAuth2 password grant in a form (containerd's POST). Call r.ParseForm first.
func Credentials(r *http.Request) (user, password string) {
	if u, p, ok := r.BasicAuth(); ok {
		return u, p
	}
	if r.Method == http.MethodPost && r.PostForm.Get("grant_type") == "password" {
		return r.PostForm.Get("username"), r.PostForm.Get("password")
	}
	return "", ""
}

// buildsRepo is the repository the Git builds of an app are kept in.
func buildsRepo(appID string) string { return "builds/app-" + strings.ToLower(appID) }

// BuildCache returns where a Git build (local tag "app-<id>:<commit>") is kept in the
// registry, and the credentials to push or pull it; ok is false while the registry is off
// or does not keep builds.
func (s *Service) BuildCache(ctx context.Context, local string) (ref string, auth *executor.RegistryAuth, ok bool) {
	rs, active := s.Active(ctx)
	if !active || !rs.BuildCache || !strings.HasPrefix(local, "app-") {
		return "", nil, false
	}
	return rs.Domain + "/builds/" + local, &executor.RegistryAuth{Server: rs.Domain, Username: s.SystemUser(), Password: s.system}, true
}

// CachedBuilds lists the builds of an app kept in the registry, by their local tag.
func (s *Service) CachedBuilds(ctx context.Context, appID string) map[string]bool {
	rs, active := s.Active(ctx)
	if !active || !rs.BuildCache {
		return nil
	}
	tags, err := s.Tags(ctx, buildsRepo(appID))
	if err != nil {
		return nil
	}
	out := make(map[string]bool, len(tags))
	for _, t := range tags {
		out["app-"+strings.ToLower(appID)+":"+t] = true
	}
	return out
}

// ErrDenied means the credentials of a token request are wrong.
var ErrDenied = errors.New("registry: wrong username or token")

// Token answers a token request: user and password from docker login (empty for an
// anonymous request), the scopes asked for. It grants what the account may do of what was
// asked: a pull token pulls, a push token pushes and pulls, the panel's own account does
// everything. Anonymous requests get a token with no access, so only public endpoints work.
func (s *Service) Token(ctx context.Context, user, password, service string, scopes []string) (string, error) {
	rs, ok := s.Active(ctx)
	if !ok {
		return "", errors.New("registry: not enabled")
	}
	if service != "" && service != rs.Domain {
		return "", fmt.Errorf("registry: unknown service %q", service)
	}
	var allowed func(a Access) []string
	switch {
	case user == "" && password == "":
		allowed = func(Access) []string { return nil }
	case user == s.SystemUser():
		if subtle.ConstantTimeCompare([]byte(password), []byte(s.system)) != 1 {
			return "", ErrDenied
		}
		allowed = func(a Access) []string { return a.Actions }
	default:
		t, err := s.opts.Store.CheckRegistryToken(ctx, user, password)
		if errors.Is(err, store.ErrNotFound) {
			return "", ErrDenied
		}
		if err != nil {
			return "", err
		}
		allowed = func(a Access) []string {
			if a.Type != "repository" {
				return nil
			}
			var out []string
			for _, act := range a.Actions {
				if act == "pull" || (act == "push" && t.Scope == store.RegistryPush) {
					out = append(out, act)
				}
			}
			return out
		}
	}
	var grant []Access
	for _, field := range scopes {
		// containerd's OAuth2 POST (Docker Desktop's image store) sends every scope in one
		// space-separated field; docker's GET sends one scope parameter each.
		for _, sc := range strings.Fields(field) {
			a, ok := ParseScope(sc)
			if !ok {
				continue
			}
			if acts := allowed(a); len(acts) > 0 {
				grant = append(grant, Access{Type: a.Type, Name: a.Name, Actions: acts})
			}
		}
	}
	return s.signer.issue(rs.Domain, user, grant, tokenTTL)
}

// internalToken is a token of the panel's own account for its calls to the registry API.
func (s *Service) internalToken(ctx context.Context, access ...Access) (string, error) {
	rs, ok := s.Active(ctx)
	if !ok {
		return "", errors.New("registry: not enabled")
	}
	return s.signer.issue(rs.Domain, s.SystemUser(), access, tokenTTL)
}

// Push is a new tag pushed to the registry.
type Push struct {
	Repository string
	Tag        string
	Digest     string
}

// CheckEvents reports whether a push report carries the shared secret.
func (s *Service) CheckEvents(authorization string) bool {
	return subtle.ConstantTimeCompare([]byte(authorization), []byte("Bearer "+s.events)) == 1
}

// ParseEvents reads a push report and returns the tags pushed (manifest pushes with a
// tag; layer pushes and pulls are left out).
func ParseEvents(body []byte) ([]Push, error) {
	var env struct {
		Events []struct {
			Action string `json:"action"`
			Target struct {
				MediaType  string `json:"mediaType"`
				Repository string `json:"repository"`
				Tag        string `json:"tag"`
				Digest     string `json:"digest"`
			} `json:"target"`
		} `json:"events"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, err
	}
	var out []Push
	for _, e := range env.Events {
		if e.Action != "push" || e.Target.Tag == "" || e.Target.Repository == "" {
			continue
		}
		out = append(out, Push{Repository: e.Target.Repository, Tag: e.Target.Tag, Digest: e.Target.Digest})
	}
	return out, nil
}

// config is the registry's configuration file. JSON is YAML, so no YAML writer is needed.
func (s *Service) config(ctx context.Context, rs store.RegistrySettings) ([]byte, error) {
	storage := map[string]any{
		"delete":   map[string]any{"enabled": true},
		"cache":    map[string]any{"blobdescriptor": "inmemory"},
		"redirect": map[string]any{"disable": true}, // clients never talk to S3 directly
	}
	connID, _, err := s.opts.Store.GetSetting(ctx, store.SettingRegistryS3)
	if err != nil {
		return nil, err
	}
	if connID != "" {
		c, err := s.opts.Store.S3Secret(ctx, connID)
		if err != nil {
			return nil, fmt.Errorf("registry storage: %w", err)
		}
		scheme := "https"
		if !c.UseSSL {
			scheme = "http"
		}
		region := c.Region
		if region == "" {
			region = "us-east-1"
		}
		s3 := map[string]any{
			"accesskey": c.AccessKey, "secretkey": c.SecretKey, "region": region,
			"regionendpoint": scheme + "://" + c.Endpoint, "bucket": c.Bucket,
			"secure": c.UseSSL, "forcepathstyle": c.PathStyle,
		}
		if c.Prefix != "" {
			s3["rootdirectory"] = "/" + c.Prefix
		}
		storage["s3"] = s3
	} else {
		storage["filesystem"] = map[string]any{"rootdirectory": "/var/lib/registry"}
	}
	httpSecret := sha256.Sum256([]byte("http:" + s.system))
	cfg := map[string]any{
		"version": "0.1",
		"log":     map[string]any{"level": "info", "formatter": "text", "accesslog": map[string]any{"disabled": true}},
		"storage": storage,
		"http": map[string]any{
			"addr":         fmt.Sprintf(":%d", internalPort),
			"relativeurls": true, // upload locations follow the host clients used
			"secret":       hex.EncodeToString(httpSecret[:]),
			"headers":      map[string]any{"X-Content-Type-Options": []string{"nosniff"}},
		},
		"auth": map[string]any{"token": map[string]any{
			"realm": s.realm(rs), "service": rs.Domain, "issuer": s.opts.Issuer,
			"rootcertbundle": "/etc/distribution/token.crt",
		}},
		"notifications": map[string]any{"endpoints": []any{map[string]any{
			"name": "panel", "url": "http://" + s.opts.PanelUpstream + EventsPath,
			"headers": map[string]any{"Authorization": []string{"Bearer " + s.events}},
			"timeout": "5s", "threshold": 5, "backoff": "10s",
			"ignore":            map[string]any{"actions": []string{"pull"}},
			"ignoredmediatypes": []string{"application/octet-stream"},
		}}},
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func (s *Service) realm(rs store.RegistrySettings) string {
	if s.opts.Realm != "" {
		return s.opts.Realm
	}
	return "https://" + rs.Domain + TokenPath
}

// Apply makes the container match the settings: removed when the registry is off,
// created or recreated when its configuration changed, started when stopped. Images stay
// in the volume or bucket when it is turned off.
func (s *Service) Apply(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ex := s.opts.Executor
	rs, err := s.Settings(ctx)
	if err != nil {
		return err
	}
	c, err := ex.InspectContainer(ctx, s.opts.Name)
	found := err == nil
	if err != nil && !errors.Is(err, executor.ErrNotFound) {
		return err
	}
	if found && !IsRegistry(c.Kind, c.ResourceID) {
		return fmt.Errorf("registry: a container named %s exists and is not managed by this panel; remove or rename it", s.opts.Name)
	}
	if !rs.Enabled || rs.Domain == "" {
		if found {
			s.opts.Log.Info("registry turned off; removing its container")
			return ex.RemoveContainer(ctx, c.ID, true)
		}
		return nil
	}

	raw, err := s.config(ctx, rs)
	if err != nil {
		return err
	}
	if err := s.writeFiles(raw); err != nil {
		return err
	}
	spec := s.spec(raw)
	if found {
		if c.Labels[specLabel] == spec.Labels[specLabel] {
			if c.Running {
				return nil
			}
			return ex.StartContainer(ctx, c.ID)
		}
		s.opts.Log.Info("registry configuration changed; recreating its container")
		if err := ex.RemoveContainer(ctx, c.ID, true); err != nil {
			return err
		}
	}
	if err := ex.EnsureNetwork(ctx); err != nil {
		return err
	}
	id, err := ex.CreateContainer(ctx, spec)
	if errors.Is(err, executor.ErrNotFound) {
		s.opts.Log.Info("pulling registry image", "image", spec.Image)
		if err := ex.PullImage(ctx, spec.Image, nil, nil); err != nil {
			return err
		}
		id, err = ex.CreateContainer(ctx, spec)
	}
	if err != nil {
		return err
	}
	if err := ex.StartContainer(ctx, id); err != nil {
		_ = ex.RemoveContainer(context.WithoutCancel(ctx), id, true)
		return err
	}
	return nil
}

// writeFiles puts the configuration and the token certificate where the container reads them.
func (s *Service) writeFiles(config []byte) error {
	etc := filepath.Join(s.opts.Dir, "etc")
	if err := os.MkdirAll(etc, 0o700); err != nil {
		return err
	}
	// The configuration may hold an S3 secret key: readable by its owner only.
	for _, f := range []struct {
		name string
		body []byte
		mode os.FileMode
	}{{"config.yml", config, 0o600}, {"token.crt", s.signer.certPEM, 0o644}} {
		p := filepath.Join(etc, f.name)
		if err := os.WriteFile(p+".tmp", f.body, f.mode); err != nil {
			return err
		}
		if err := os.Rename(p+".tmp", p); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) spec(config []byte) executor.ContainerSpec {
	o := s.opts
	etc := executor.Mount{Source: filepath.Join(o.Dir, "etc"), Target: "/etc/distribution", ReadOnly: true}
	if o.Volume != "" {
		etc = executor.Mount{Volume: o.Volume, SubPath: path.Join(o.VolumePath, "etc"), Target: "/etc/distribution", ReadOnly: true}
	}
	spec := executor.ContainerSpec{
		Name:       o.Name,
		Image:      o.Image,
		Kind:       executor.KindService,
		ResourceID: resourceID,
		Command:    []string{"/etc/distribution/config.yml"},
		Mounts:     []executor.Mount{etc, {Volume: o.DataVolume, Target: "/var/lib/registry"}},
		// Lets the registry report pushes to a panel that runs on the host (development).
		ExtraHosts: []string{"host.docker.internal:host-gateway"},
	}
	if o.PublishPort > 0 {
		spec.Ports = []executor.PortBinding{{HostIP: "127.0.0.1", HostPort: o.PublishPort, ContainerPort: internalPort}}
	}
	sum := sha256.Sum256(append([]byte(fmt.Sprintf("%+v", spec)), config...))
	spec.Labels = map[string]string{specLabel: hex.EncodeToString(sum[:8])}
	return spec
}

// GarbageCollect frees the space of deleted tags and untagged images. The registry must
// not take pushes meanwhile, so it is stopped, a one-off container runs the collector on
// the same storage, and the registry starts again (whatever happened). It returns the
// collector's last lines.
func (s *Service) GarbageCollect(ctx context.Context) (string, error) {
	rs, ok := s.Active(ctx)
	if !ok {
		return "", errors.New("registry: not enabled")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ex := s.opts.Executor
	raw, err := s.config(ctx, rs)
	if err != nil {
		return "", err
	}
	spec := s.spec(raw)
	if err := ex.StopContainer(ctx, s.opts.Name, 30*time.Second); err != nil && !errors.Is(err, executor.ErrNotFound) {
		return "", err
	}
	defer func() {
		if err := ex.StartContainer(context.WithoutCancel(ctx), s.opts.Name); err != nil {
			s.opts.Log.Error("registry: start again after garbage collection", "error", err)
		}
	}()
	gc := spec
	gc.Name, gc.ResourceID, gc.Ports, gc.Labels = s.opts.Name+"-gc", "registry-gc", nil, nil
	gc.Command = []string{"garbage-collect", "--delete-untagged", "/etc/distribution/config.yml"}
	gc.Restart = executor.RestartNo
	_ = ex.RemoveContainer(ctx, gc.Name, true) // left by an interrupted run
	id, err := ex.CreateContainer(ctx, gc)
	if err != nil {
		return "", err
	}
	defer ex.RemoveContainer(context.WithoutCancel(ctx), id, true)
	if err := ex.StartContainer(ctx, id); err != nil {
		return "", err
	}
	for {
		c, err := ex.InspectContainer(ctx, id)
		if err != nil {
			return "", err
		}
		if !c.Running {
			var lines []string
			_ = ex.Logs(ctx, id, executor.LogOptions{Tail: 5}, func(l executor.LogLine) { lines = append(lines, l.Text) })
			out := strings.Join(lines, "\n")
			if c.ExitCode != 0 {
				return out, fmt.Errorf("registry: garbage collection failed (exit code %d): %s", c.ExitCode, out)
			}
			return out, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// State is what the registry's container is doing: "off" when it is not enabled,
// "missing" when it should run but does not exist, else Docker's state (running, …).
func (s *Service) State(ctx context.Context) string {
	if _, ok := s.Active(ctx); !ok {
		return "off"
	}
	c, err := s.opts.Executor.InspectContainer(ctx, s.opts.Name)
	switch {
	case errors.Is(err, executor.ErrNotFound):
		return "missing"
	case err != nil:
		return "unknown"
	}
	return c.State
}

// Supervise checks the registry every interval and brings it back when it is gone or
// stopped. It returns when ctx is cancelled.
func (s *Service) Supervise(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	var lastErr string
	for {
		applyCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		err := s.Apply(applyCtx)
		cancel()
		switch {
		case ctx.Err() != nil:
			return
		case err == nil:
			lastErr = ""
		case err.Error() != lastErr:
			s.opts.Log.Error("registry", "error", err)
			lastErr = err.Error()
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

package registry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"panel/internal/executor"
	"panel/internal/store"
)

func TestParseScope(t *testing.T) {
	for in, want := range map[string]Access{
		"repository:shop/api:pull,push":      {Type: "repository", Name: "shop/api", Actions: []string{"pull", "push"}},
		"repository:host:5000/shop/api:pull": {Type: "repository", Name: "host:5000/shop/api", Actions: []string{"pull"}},
		"registry:catalog:*":                 {Type: "registry", Name: "catalog", Actions: []string{"*"}},
	} {
		got, ok := ParseScope(in)
		if !ok || got.Type != want.Type || got.Name != want.Name || strings.Join(got.Actions, ",") != strings.Join(want.Actions, ",") {
			t.Errorf("ParseScope(%q) = %+v %v", in, got, ok)
		}
	}
	for _, bad := range []string{"", "repository", "repository:x", "repository:x:", ":x:pull"} {
		if _, ok := ParseScope(bad); ok {
			t.Errorf("ParseScope(%q) accepted", bad)
		}
	}
}

func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return st
}

// claims decodes a token's payload, unverified.
func claims(t *testing.T, tok string) map[string]any {
	t.Helper()
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("token %q", tok)
	}
	raw, err := base64URL(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTokenGrants(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	s, err := New(Options{Executor: nopExecutor{}, Store: st, Log: slog.New(slog.DiscardHandler), Name: "r", Image: "registry:3", Dir: t.TempDir(), Issuer: "berth"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Token(ctx, "", "", "", nil); err == nil {
		t.Error("a token while the registry is off")
	}
	st.SetRegistrySettings(ctx, store.RegistrySettings{Enabled: true, Domain: "registry.example.com"})
	st.CreateRegistryToken(ctx, "ci", store.RegistryPush, "push-secret")
	st.CreateRegistryToken(ctx, "reader", store.RegistryPull, "pull-secret")

	access := func(user, pass string, scopes ...string) []any {
		t.Helper()
		tok, err := s.Token(ctx, user, pass, "registry.example.com", scopes)
		if err != nil {
			t.Fatalf("%s: %v", user, err)
		}
		c := claims(t, tok)
		if c["aud"] != "registry.example.com" || c["iss"] != "berth" {
			t.Errorf("claims = %v", c)
		}
		a, _ := c["access"].([]any)
		return a
	}
	if a := access("ci", "push-secret", "repository:shop/api:pull,push"); len(a) != 1 || fmt.Sprint(a[0].(map[string]any)["actions"]) != "[pull push]" {
		t.Errorf("push token = %v", a)
	}
	if a := access("reader", "pull-secret", "repository:shop/api:pull,push", "registry:catalog:*"); len(a) != 1 || fmt.Sprint(a[0].(map[string]any)["actions"]) != "[pull]" {
		t.Errorf("pull token = %v", a)
	}
	// containerd's OAuth2 POST: every scope in one space-separated field.
	if a := access("ci", "push-secret", "repository:shop/base:pull repository:shop/api:pull,push"); len(a) != 2 ||
		a[1].(map[string]any)["name"] != "shop/api" || fmt.Sprint(a[1].(map[string]any)["actions"]) != "[pull push]" {
		t.Errorf("space-separated scopes = %v", a)
	}
	if a := access("", "", "repository:shop/api:pull"); len(a) != 0 {
		t.Errorf("anonymous = %v", a)
	}
	if a := access("berth", s.system, "registry:catalog:*", "repository:x:delete"); len(a) != 2 {
		t.Errorf("system = %v", a)
	}
	if _, err := s.Token(ctx, "ci", "wrong", "", nil); err != ErrDenied {
		t.Errorf("wrong secret: %v", err)
	}
	if _, err := s.Token(ctx, "berth", "wrong", "", nil); err != ErrDenied {
		t.Errorf("wrong system secret: %v", err)
	}
	if _, err := s.Token(ctx, "ci", "push-secret", "other.example.com", nil); err == nil {
		t.Error("another service accepted")
	}
	if got := s.PullAuth(ctx, "registry.example.com/shop/api:1"); got == nil || got.Username != "berth" {
		t.Errorf("pull auth = %+v", got)
	}
	if got := s.PullAuth(ctx, "ghcr.io/shop/api:1"); got != nil {
		t.Errorf("pull auth for another registry = %+v", got)
	}
}

func TestParseEvents(t *testing.T) {
	body := `{"events":[
		{"action":"push","target":{"mediaType":"application/vnd.oci.image.manifest.v1+json","repository":"shop/api","tag":"1.2","digest":"sha256:ab"}},
		{"action":"push","target":{"mediaType":"application/vnd.oci.image.layer.v1.tar+gzip","repository":"shop/api","digest":"sha256:cd"}},
		{"action":"pull","target":{"repository":"shop/api","tag":"1.2"}}]}`
	got, err := ParseEvents([]byte(body))
	if err != nil || len(got) != 1 || got[0] != (Push{Repository: "shop/api", Tag: "1.2", Digest: "sha256:ab"}) {
		t.Fatalf("ParseEvents = %+v, %v", got, err)
	}
}

// TestRealRegistry runs the registry in Docker, checks that it accepts the panel's tokens
// (a pull token cannot push, a push token can) and that it reports the push.
func TestRealRegistry(t *testing.T) {
	if testing.Short() {
		t.Skip("needs Docker; skipped with -short")
	}
	ctx := context.Background()
	name := fmt.Sprintf("paneltest-%d", time.Now().UnixNano())
	d, err := executor.NewDocker(executor.Options{Network: name, LabelPrefix: name})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Ping(ctx); err != nil {
		t.Skipf("no Docker daemon: %v", err)
	}
	// The registry reports pushes here, through the Docker host gateway.
	var mu sync.Mutex
	var reports [][]byte
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		reports = append(reports, body)
		mu.Unlock()
		w.WriteHeader(200)
	})}
	go srv.Serve(ln)
	defer srv.Close()

	port := freePort(t)
	st := newStore(t)
	s, err := New(Options{
		Executor: d, Store: st, Log: slog.New(slog.DiscardHandler), Name: name + "-registry", Image: "registry:3.0.0",
		Dir: t.TempDir(), DataVolume: name + "-data", Issuer: "berth", PublishPort: port,
		PanelUpstream: fmt.Sprintf("host.docker.internal:%d", ln.Addr().(*net.TCPAddr).Port),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		list, _ := d.ListContainers(ctx)
		for _, c := range list {
			_ = d.RemoveContainer(ctx, c.ID, true)
		}
		exec.Command("docker", "network", "rm", name).Run()
		exec.Command("docker", "volume", "rm", name+"-data").Run()
		d.Close()
	})
	if err := st.SetRegistrySettings(ctx, store.RegistrySettings{Enabled: true, Domain: "registry.example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	base := s.baseURL()
	waitUp(t, base+"/v2/")

	// Without a token: 401, pointing at the panel's token endpoint.
	resp, err := http.Get(base + "/v2/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 || !strings.Contains(resp.Header.Get("WWW-Authenticate"), `realm="https://registry.example.com/api/registry/token"`) {
		t.Fatalf("anonymous /v2/ = %d %q", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	}

	st.CreateRegistryToken(ctx, "reader", store.RegistryPull, "pull-secret")
	st.CreateRegistryToken(ctx, "ci", store.RegistryPush, "push-secret")
	scope := "repository:shop/api:pull,push"
	readTok, _ := s.Token(ctx, "reader", "pull-secret", "registry.example.com", []string{scope})
	pushTok, _ := s.Token(ctx, "ci", "push-secret", "registry.example.com", []string{scope})

	// A pull token cannot start an upload; a push token can, and pushes an image.
	if code := status(t, "POST", base+"/v2/shop/api/blobs/uploads/", readTok, nil, ""); code != 401 {
		t.Errorf("upload with a pull token = %d, want 401", code)
	}
	config := []byte(`{"architecture":"amd64","os":"linux","rootfs":{"type":"layers","diff_ids":[]}}`)
	cfgDigest := upload(t, base, pushTok, config)
	manifest := fmt.Sprintf(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"%s","size":%d},"layers":[]}`, cfgDigest, len(config))
	if code := status(t, "PUT", base+"/v2/shop/api/manifests/1.2", pushTok, []byte(manifest), "application/vnd.oci.image.manifest.v1+json"); code != 201 {
		t.Fatalf("manifest push = %d", code)
	}
	// The pull token reads it back.
	if code := status(t, "GET", base+"/v2/shop/api/manifests/1.2", readTok, nil, ""); code != 200 {
		t.Errorf("pull with a pull token = %d", code)
	}
	tags, err := s.Tags(ctx, "shop/api")
	if err != nil || strings.Join(tags, ",") != "1.2" {
		t.Errorf("Tags = %v, %v", tags, err)
	}
	repos, err := s.Repositories(ctx)
	if err != nil || strings.Join(repos, ",") != "shop/api" {
		t.Errorf("Repositories = %v, %v", repos, err)
	}

	// The push is reported, with the shared secret.
	deadline := time.Now().Add(20 * time.Second)
	for {
		mu.Lock()
		var pushes []Push
		for _, r := range reports {
			p, _ := ParseEvents(r)
			pushes = append(pushes, p...)
		}
		mu.Unlock()
		if len(pushes) > 0 {
			if pushes[0].Repository != "shop/api" || pushes[0].Tag != "1.2" {
				t.Errorf("pushes = %+v", pushes)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no push report from the registry")
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Deleting the tag removes it from the list.
	if err := s.DeleteTag(ctx, "shop/api", "1.2"); err != nil {
		t.Fatalf("DeleteTag: %v", err)
	}
	if tags, err := s.Tags(ctx, "shop/api"); err != nil || len(tags) != 0 {
		t.Errorf("tags after delete = %v, %v", tags, err)
	}

	// Garbage collection frees the deleted manifest's blobs; the registry is back after.
	if out, err := s.GarbageCollect(ctx); err != nil {
		t.Fatalf("GarbageCollect: %v\n%s", err, out)
	}
	waitUp(t, base+"/v2/")
	if c, err := d.InspectContainer(ctx, name+"-registry"); err != nil || !c.Running {
		t.Errorf("registry after gc: %+v %v", c, err)
	}
	if _, err := d.InspectContainer(ctx, name+"-registry-gc"); err == nil {
		t.Error("the gc container was left behind")
	}

	// Turned off: the container goes.
	st.SetRegistrySettings(ctx, store.RegistrySettings{Enabled: false, Domain: "registry.example.com"})
	if err := s.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := d.InspectContainer(ctx, name+"-registry"); err == nil {
		t.Error("the container is still there after turning the registry off")
	}
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func waitUp(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if resp, err := http.Get(url); err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("%s never answered", url)
}

func status(t *testing.T, method, url, tok string, body []byte, ctype string) int {
	t.Helper()
	req, _ := http.NewRequest(method, url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", manifestTypes)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		t.Logf("%s %s = %d: %s", method, url, resp.StatusCode, b)
	}
	return resp.StatusCode
}

// upload pushes a blob in one PUT and returns its digest. The registry answers with
// absolute https://<domain> locations; the path is kept, the base swapped.
func upload(t *testing.T, base, tok string, blob []byte) string {
	t.Helper()
	req, _ := http.NewRequest("POST", base+"/v2/shop/api/blobs/uploads/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 202 {
		t.Fatalf("start upload = %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if i := strings.Index(loc, "/v2/"); i >= 0 {
		loc = base + loc[i:]
	}
	sum := sha256.Sum256(blob)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	sep := "?"
	if strings.Contains(loc, "?") {
		sep = "&"
	}
	if code := status(t, "PUT", loc+sep+"digest="+digest, tok, blob, "application/octet-stream"); code != 201 {
		t.Fatalf("finish upload = %d", code)
	}
	return digest
}

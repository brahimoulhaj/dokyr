package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"panel/internal/store"
)

// A maintainer must not reach the host through an app: no new bind mounts, no builder
// image of their own. What an admin set stays editable around.
func TestMaintainerCannotReachHost(t *testing.T) {
	e := newEnv(t)
	e.signIn()
	admin := e.join("admin@b.co", "admin")
	maint := e.join("maint@b.co", "maintainer")
	pid := maint.must(201, "POST", "/api/projects", map[string]any{"name": "Shop"})["id"].(string)
	apps := "/api/projects/" + pid + "/apps"

	sock := map[string]any{"type": "bind", "source": "/var/run/docker.sock", "target": "/var/run/docker.sock"}
	volume := map[string]any{"type": "volume", "source": "data", "target": "/data"}

	// Bind mounts: refused on create, on update and in a Compose file; volumes are fine.
	maint.must(403, "POST", apps, map[string]any{"name": "web", "image": "nginx:1", "port": 80, "mounts": []any{sock}})
	app := maint.must(201, "POST", apps, map[string]any{"name": "web", "image": "nginx:1", "port": 80, "mounts": []any{volume}})
	appURL := "/api/apps/" + app["id"].(string)
	maint.must(403, "PATCH", appURL, map[string]any{"mounts": []any{volume, sock}})
	file := "services:\n  db:\n    image: nginx:1\n    volumes:\n      - /var/run/docker.sock:/var/run/docker.sock\n"
	maint.must(403, "POST", "/api/projects/"+pid+"/compose", map[string]any{"compose": file})
	maint.must(403, "POST", "/api/projects/"+pid+"/compose", map[string]any{"compose": file, "dry_run": true})

	// A maintainer's API token carries the same rule.
	tok := maint.must(201, "POST", "/api/tokens", map[string]any{"name": "ci"})
	if code, body := e.bearer(tok["token"].(string), "PATCH", appURL, map[string]any{"mounts": []any{sock}}); code != 403 {
		t.Fatalf("maintainer token bind mount = %d %s", code, body)
	}

	// An admin adds the bind; the maintainer then keeps it, but cannot repoint it.
	etc := map[string]any{"type": "bind", "source": "/srv/shop", "target": "/srv", "read_only": true}
	admin.must(200, "PATCH", appURL, map[string]any{"mounts": []any{volume, etc}})
	maint.must(200, "PATCH", appURL, map[string]any{"port": 8080})
	maint.must(200, "PATCH", appURL, map[string]any{"mounts": []any{etc}})
	for name, changed := range map[string]map[string]any{
		"source":    {"type": "bind", "source": "/", "target": "/srv", "read_only": true},
		"target":    {"type": "bind", "source": "/srv/shop", "target": "/host", "read_only": true},
		"writable":  {"type": "bind", "source": "/srv/shop", "target": "/srv"},
		"docker.sk": sock,
	} {
		if code, m := maint.call("PATCH", appURL, map[string]any{"mounts": []any{changed}}); code != 403 {
			t.Fatalf("maintainer changed bind %s = %d %v", name, code, m)
		}
	}

	// The Buildpacks builder image runs as root with the Docker socket.
	git := func(extra map[string]any) map[string]any {
		in := map[string]any{"name": "site", "source_type": "git", "git_repository": "https://github.com/org/app.git", "port": 80}
		for k, v := range extra {
			in[k] = v
		}
		return in
	}
	evil := map[string]any{"image": "attacker/builder:1"}
	maint.must(403, "POST", apps, git(map[string]any{"builder": "buildpacks", "build_options": evil}))
	site := maint.must(201, "POST", apps, git(map[string]any{"builder": "buildpacks"})) // the default builder
	siteURL := "/api/apps/" + site["id"].(string)
	maint.must(403, "PATCH", siteURL, map[string]any{"build_options": evil})
	// Parking the image under another builder and switching afterwards is the same thing.
	maint.must(200, "PATCH", siteURL, map[string]any{"builder": "static", "build_options": evil})
	maint.must(403, "PATCH", siteURL, map[string]any{"builder": "buildpacks"})
	// An admin picks it; the maintainer keeps working on the app, without changing it.
	trusted := map[string]any{"image": "heroku/builder:24"}
	admin.must(200, "PATCH", siteURL, map[string]any{"builder": "buildpacks", "build_options": trusted})
	maint.must(200, "PATCH", siteURL, map[string]any{"git_branch": "release"})
	maint.must(403, "PATCH", siteURL, map[string]any{"build_options": evil})
}

// The panel's address is where everyone signs in: a maintainer neither moves it nor
// takes over a hostname it used to have, which still receives session cookies.
func TestPanelDomainIsAdminOnly(t *testing.T) {
	e := newEnv(t)
	e.signIn()
	admin := e.join("admin@b.co", "admin")
	maint := e.join("maint@b.co", "maintainer")

	maint.must(200, "GET", "/api/settings/panel", nil)
	maint.must(403, "PUT", "/api/settings/panel", map[string]any{"domain": "evil.example.com"})
	maint.must(403, "PUT", "/api/settings/panel", map[string]any{"domain": "", "tls_mode": "http_only"})
	tok := admin.must(201, "POST", "/api/tokens", map[string]any{"name": "ci"})
	if code, body := e.bearer(tok["token"].(string), "PUT", "/api/settings/panel", map[string]any{"domain": "x.example.com"}); code != 403 {
		t.Fatalf("token panel settings = %d %s", code, body)
	}

	admin.must(200, "PUT", "/api/settings/panel", map[string]any{"domain": "panel.example.com"})
	admin.must(200, "PUT", "/api/settings/panel", map[string]any{"domain": "console.example.com"})

	pid := maint.must(201, "POST", "/api/projects", map[string]any{"name": "Shop"})["id"].(string)
	app := maint.must(201, "POST", "/api/projects/"+pid+"/apps", map[string]any{"name": "web", "image": "nginx:1", "port": 80})

	// The retired name: not as a domain, not as a subdomain route, not under a wildcard.
	maint.must(403, "POST", "/api/domains", map[string]any{"hostname": "panel.example.com"})
	maint.must(403, "POST", "/api/projects/"+pid+"/domains", map[string]any{"hostname": "panel.example.com"})
	d := maint.must(201, "POST", "/api/projects/"+pid+"/domains", map[string]any{"hostname": "example.com"})
	routes := "/api/domains/" + d["id"].(string) + "/routes"
	maint.must(403, "POST", routes, map[string]any{"subdomain": "panel", "path": "/", "app_id": app["id"]})
	maint.must(403, "POST", routes, map[string]any{"subdomain": "*", "path": "/", "app_id": app["id"]})
	rt := maint.must(201, "POST", routes, map[string]any{"subdomain": "shop", "path": "/", "app_id": app["id"]})
	maint.must(403, "PATCH", "/api/routes/"+rt["id"].(string), map[string]any{"subdomain": "panel", "path": "/", "app_id": app["id"]})

	// An admin decides; and a name the panel takes back is no longer retired.
	admin.must(201, "POST", routes, map[string]any{"subdomain": "panel", "path": "/api", "app_id": app["id"]})
	admin.must(200, "PUT", "/api/settings/panel", map[string]any{"domain": "old.example.com"})
	admin.must(200, "PUT", "/api/settings/panel", map[string]any{"domain": "console.example.com"})
	maint.must(403, "POST", "/api/domains", map[string]any{"hostname": "old.example.com"})
	maint.must(201, "POST", "/api/domains", map[string]any{"hostname": "other.example.org"})
}

// What moves the panel's own data, its updates and where its alerts go is for admins.
func TestAdminOnlyOperations(t *testing.T) {
	e := newEnv(t)
	e.signIn()
	admin := e.join("admin@b.co", "admin")
	maint := e.join("maint@b.co", "maintainer")

	for _, c := range []struct {
		method, path string
		body         any
	}{
		{"POST", "/api/settings/panel-backup/run", nil},
		{"GET", "/api/settings/panel-backup/backups", nil},
		{"POST", "/api/updates/apply", nil},
		{"PUT", "/api/updates/settings", map[string]any{"auto": true}},
		{"PUT", "/api/object-store", map[string]any{"enabled": true}},
		{"PUT", "/api/registry/settings", map[string]any{"enabled": true}},
		{"POST", "/api/notification-channels", map[string]any{"name": "x", "kind": "webhook", "config": map[string]string{"url": "http://169.254.169.254/"}}},
		{"PUT", "/api/notification-channels/any", map[string]any{}},
		{"POST", "/api/notification-channels/any/test", nil},
	} {
		if code, m := maint.call(c.method, c.path, c.body); code != 403 {
			t.Errorf("maintainer %s %s = %d %v, want 403", c.method, c.path, code, m)
		}
		if code, m := admin.call(c.method, c.path, c.body); code == 403 || code == 401 {
			t.Errorf("admin %s %s = %d %v", c.method, c.path, code, m)
		}
	}
	// Reading stays open: the pages still load for a maintainer.
	maint.must(200, "GET", "/api/updates", nil)
	maint.must(200, "GET", "/api/notification-channels", nil)
}

// fakeObjectStore answers the two calls creating an access key makes.
type fakeObjectStore struct{ ObjectStore }

func (fakeObjectStore) Endpoint() string { return "dokyr-s3:9000" }
func (fakeObjectStore) CreateAccessKey(_ context.Context, name, bucket, permission string) (store.S3AccessKey, string, error) {
	return store.S3AccessKey{ID: "k1", Name: name, Bucket: bucket, Permission: permission, AccessKey: "AK"}, "secret", nil
}

// A key opens its whole bucket: a maintainer gets none for a bucket that holds backups.
func TestMaintainerStorageKeys(t *testing.T) {
	e := newEnv(t)
	e.signIn()
	admin := e.join("admin@b.co", "admin")
	maint := e.join("maint@b.co", "maintainer")
	e.srv.SetObjectStore(fakeObjectStore{})
	e.srv.s3Test = func(context.Context, store.S3Input) error { return nil }
	e.must(201, "POST", "/api/s3-connections", map[string]any{
		"name": "Backups", "endpoint": "dokyr-s3:9000", "bucket": "db-backups", "access_key": "a", "secret_key": "b", "path_style": true,
	})

	key := func(bucket string) map[string]any {
		return map[string]any{"name": "k", "bucket": bucket, "permission": "write"}
	}
	maint.must(403, "POST", "/api/object-store/keys", key("panel"))      // the panel's own bucket
	maint.must(403, "POST", "/api/object-store/keys", key("db-backups")) // a connection's bucket
	maint.must(201, "POST", "/api/object-store/keys", key("uploads"))
	admin.must(201, "POST", "/api/object-store/keys", key("panel"))
}

// A maintainer cannot aim the panel's own requests at the server's networks.
func TestMaintainerInternalAddresses(t *testing.T) {
	e := newEnv(t)
	e.signIn()
	admin := e.join("admin@b.co", "admin")
	maint := e.join("maint@b.co", "maintainer")
	e.srv.s3Test = func(context.Context, store.S3Input) error { return nil }
	e.srv.lookupHost = func(_ context.Context, host string) ([]string, error) {
		switch host {
		case "git.internal.example":
			return []string{"203.0.113.5", "10.0.0.7"}, nil // one private address is enough
		case "metadata.example":
			return []string{"169.254.169.254"}, nil
		}
		return []string{"203.0.113.5"}, nil
	}
	pid := maint.must(201, "POST", "/api/projects", map[string]any{"name": "Shop"})["id"].(string)
	apps := "/api/projects/" + pid + "/apps"
	git := func(name, repo string) map[string]any {
		return map[string]any{"name": name, "source_type": "git", "git_repository": repo, "port": 80}
	}
	s3 := func(endpoint string) map[string]any {
		return map[string]any{"name": "x", "endpoint": endpoint, "bucket": "bucket", "access_key": "a", "secret_key": "b"}
	}

	maint.must(403, "POST", apps, git("a", "https://git.internal.example/org/app.git"))
	maint.must(403, "POST", apps, git("a", "https://127.0.0.1/org/app.git"))
	maint.must(403, "POST", apps, git("a", "https://[::1]:8443/org/app.git"))
	app := maint.must(201, "POST", apps, git("a", "https://github.com/org/app.git"))
	maint.must(403, "PATCH", "/api/apps/"+app["id"].(string), map[string]any{"git_repository": "https://metadata.example/x.git"})
	maint.must(200, "PATCH", "/api/apps/"+app["id"].(string), map[string]any{"port": 8080}) // unchanged repository: no lookup verdict
	maint.must(403, "POST", "/api/s3-connections", s3("metadata.example"))
	maint.must(403, "POST", "/api/s3-connections", s3("http://10.1.2.3:9000"))
	maint.must(403, "POST", "/api/s3-connections", s3("100.64.0.1:9000"))
	maint.must(201, "POST", "/api/s3-connections", s3("s3.amazonaws.com"))

	// An admin knows the network: a GitLab or a MinIO on a private address is theirs to add.
	admin.must(201, "POST", apps, git("b", "https://git.internal.example/org/app.git"))
	admin.must(201, "POST", "/api/s3-connections", s3("http://10.1.2.3:9000"))
}

func TestSecurityHeaders(t *testing.T) {
	e := newEnv(t)
	get := func(path string, headers ...string) http.Header {
		t.Helper()
		req, _ := http.NewRequest("GET", e.url+path, nil)
		for i := 0; i+1 < len(headers); i += 2 {
			req.Header.Set(headers[i], headers[i+1])
		}
		res, err := e.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.Header
	}
	if h := get("/api/meta"); h.Get("Content-Security-Policy") != "default-src 'none'; frame-ancestors 'none'" || h.Get("Strict-Transport-Security") != "" {
		t.Fatalf("API over http: %v", h)
	}
	if h := get("/api/meta", "X-Forwarded-Proto", "https"); h.Get("Strict-Transport-Security") == "" {
		t.Fatalf("no HSTS over https: %v", h)
	}
	page := get("/projects").Get("Content-Security-Policy")
	if !strings.Contains(page, "script-src 'self';") || !strings.Contains(page, "frame-ancestors 'none'") {
		t.Fatalf("page policy = %q", page)
	}
	// An inline script of index.html is allowed by its hash, and only that one.
	sum := sha256.Sum256([]byte("init()"))
	want := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
	if got := pageCSP([]byte(`<html><script>init()</script><script type="module" src="/a.js"></script></html>`)); !strings.Contains(got, "script-src 'self' "+want+";") {
		t.Fatalf("policy = %q, want the hash %s", got, want)
	}
}

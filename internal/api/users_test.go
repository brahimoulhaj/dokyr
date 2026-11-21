package api

import (
	"net/http"
	"net/http/cookiejar"
	"testing"
)

// as returns a second browser on the same server: its own cookie jar, so its own session.
func (e *env) as() *env {
	jar, _ := cookiejar.New(nil)
	c := *e
	c.client = &http.Client{Jar: jar}
	return &c
}

// join invites an email with a role as the owner and accepts the invite in a new browser.
func (e *env) join(email, role string) *env {
	e.t.Helper()
	inv := e.must(201, "POST", "/api/invites", map[string]any{"email": email, "role": role})
	member := e.as()
	if got := member.must(200, "POST", "/api/invites/lookup", map[string]any{"token": inv["token"]}); got["email"] != email || got["role"] != role {
		e.t.Fatalf("lookup = %v", got)
	}
	if u := member.must(201, "POST", "/api/invites/accept", map[string]any{"token": inv["token"], "password": "member-pass"}); u["role"] != role {
		e.t.Fatalf("accepted = %v", u)
	}
	return member
}

func TestInvites(t *testing.T) {
	e := newEnv(t)
	e.must(401, "POST", "/api/invites", map[string]any{"email": "m@b.co", "role": "maintainer"})
	e.signIn()

	for name, bad := range map[string]map[string]any{
		"owner role":   {"email": "m@b.co", "role": "owner"},
		"unknown role": {"email": "m@b.co", "role": "boss"},
		"bad email":    {"email": "nope", "role": "admin"},
	} {
		if code, m := e.call("POST", "/api/invites", bad); code != 422 {
			t.Fatalf("%s = %d %v", name, code, m)
		}
	}
	e.must(409, "POST", "/api/invites", map[string]any{"email": "a@b.co", "role": "admin"}) // the owner's email

	// Inviting the same email again replaces the link.
	first := e.must(201, "POST", "/api/invites", map[string]any{"email": "m@b.co", "role": "admin"})
	second := e.must(201, "POST", "/api/invites", map[string]any{"email": "M@b.co", "role": "maintainer"})
	if got := e.list("/api/invites"); len(got) != 1 || got[0]["role"] != "maintainer" || got[0]["token"] != nil {
		t.Fatalf("invites = %v", got)
	}
	guest := e.as()
	guest.must(403, "POST", "/api/invites/lookup", map[string]any{"token": first["token"]})
	guest.must(403, "POST", "/api/invites/accept", map[string]any{"token": "nope", "password": "member-pass"})
	guest.must(422, "POST", "/api/invites/accept", map[string]any{"token": second["token"], "password": "short"})
	guest.must(201, "POST", "/api/invites/accept", map[string]any{"token": second["token"], "password": "member-pass"})
	e.as().must(403, "POST", "/api/invites/accept", map[string]any{"token": second["token"], "password": "member-pass"}) // used up
	if got := e.list("/api/invites"); len(got) != 0 {
		t.Fatalf("invites after accept = %v", got)
	}
	if got := e.list("/api/users"); len(got) != 2 || got[1]["email"] != "m@b.co" || got[1]["role"] != "maintainer" {
		t.Fatalf("users = %v", got)
	}

	// A revoked invite no longer works.
	inv := e.must(201, "POST", "/api/invites", map[string]any{"email": "x@b.co", "role": "admin"})
	e.must(204, "DELETE", "/api/invites/"+inv["id"].(string), nil)
	e.must(404, "DELETE", "/api/invites/"+inv["id"].(string), nil)
	e.as().must(403, "POST", "/api/invites/lookup", map[string]any{"token": inv["token"]})

	// The member signs in with the password they chose.
	e.as().must(200, "POST", "/api/auth/login", map[string]any{"email": "m@b.co", "password": "member-pass"})
}

func TestRoles(t *testing.T) {
	e := newEnv(t)
	e.signIn()
	admin := e.join("admin@b.co", "admin")
	maint := e.join("maint@b.co", "maintainer")

	// Only the owner manages users.
	for _, member := range []*env{admin, maint} {
		member.must(403, "GET", "/api/users", nil)
		member.must(403, "GET", "/api/invites", nil)
		member.must(403, "POST", "/api/invites", map[string]any{"email": "z@b.co", "role": "admin"})
	}
	users := e.list("/api/users")
	ownerID, adminID, maintID := users[0]["id"].(string), users[1]["id"].(string), users[2]["id"].(string)
	admin.must(403, "PATCH", "/api/users/"+maintID, map[string]any{"role": "admin"})
	admin.must(403, "DELETE", "/api/users/"+maintID, nil)

	// A maintainer creates and changes, but does not delete.
	p := maint.must(201, "POST", "/api/projects", map[string]any{"name": "Shop"})
	pid := p["id"].(string)
	maint.must(200, "PATCH", "/api/projects/"+pid, map[string]any{"name": "Store"})
	app := maint.must(201, "POST", "/api/projects/"+pid+"/apps", map[string]any{"name": "web", "image": "nginx:1", "port": 80})
	maint.must(403, "DELETE", "/api/apps/"+app["id"].(string), nil)
	maint.must(403, "DELETE", "/api/projects/"+pid, nil)
	maint.must(403, "DELETE", "/api/hosts/any.example.com", nil)
	maint.must(403, "DELETE", "/api/routes/any", nil)
	maint.must(403, "DELETE", "/api/databases/any", nil)
	maint.must(403, "GET", "/api/host/terminal", nil)
	maint.must(403, "GET", "/api/settings/panel-backup/backups/x", nil)

	// Their own tokens stay theirs to revoke, and a token carries the same role.
	tok := maint.must(201, "POST", "/api/tokens", map[string]any{"name": "ci"})
	if code, body := e.bearer(tok["token"].(string), "DELETE", "/api/apps/"+app["id"].(string), nil); code != 403 {
		t.Fatalf("maintainer token delete = %d %s", code, body)
	}
	maint.must(204, "DELETE", "/api/tokens/"+tok["id"].(string), nil)

	// An admin deletes.
	admin.must(204, "DELETE", "/api/apps/"+app["id"].(string), nil)

	// The owner changes roles; it takes effect on the member's next request.
	e.must(422, "PATCH", "/api/users/"+maintID, map[string]any{"role": "owner"})
	e.must(403, "PATCH", "/api/users/"+ownerID, map[string]any{"role": "admin"})
	e.must(403, "DELETE", "/api/users/"+ownerID, nil)
	e.must(404, "PATCH", "/api/users/missing", map[string]any{"role": "admin"})
	e.must(200, "PATCH", "/api/users/"+maintID, map[string]any{"role": "admin"})
	maint.must(204, "DELETE", "/api/projects/"+pid, nil)

	// Removing a user ends their sessions.
	e.must(204, "DELETE", "/api/users/"+adminID, nil)
	admin.must(401, "GET", "/api/auth/me", nil)
	if got := e.list("/api/users"); len(got) != 2 {
		t.Fatalf("users after removal = %v", got)
	}
}

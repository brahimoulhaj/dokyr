package api

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeMailer stands in for the notifier: it records the mail the panel sends itself.
type fakeMailer struct {
	channel           string
	fail              error
	to, subject, text string
}

func (f *fakeMailer) Test(context.Context, string) error { return nil }
func (f *fakeMailer) Mailer(context.Context) (string, bool) {
	return f.channel, f.channel != ""
}
func (f *fakeMailer) Mail(_ context.Context, to, subject, text string) error {
	f.to, f.subject, f.text = to, subject, text
	return f.fail
}

// TestInviteByEmail: with an email channel and a panel address the invite link is mailed
// to the invited address; without them the request is refused, and a mail that fails
// leaves the invite and says why.
func TestInviteByEmail(t *testing.T) {
	e := newEnv(t)
	e.srv.authRate.max = 1000
	e.signIn()
	send := func(email string) map[string]any {
		return map[string]any{"email": email, "role": "admin", "send": true}
	}

	e.must(422, "POST", "/api/invites", send("a@x.co")) // no notifier
	mailer := &fakeMailer{}
	e.srv.SetNotifier(mailer)
	e.must(422, "POST", "/api/invites", send("a@x.co")) // no email channel
	if got := e.must(200, "GET", "/api/invites/mail", nil); got["available"] != false {
		t.Fatalf("mail = %v", got)
	}
	mailer.channel = "Alerts"
	e.must(422, "POST", "/api/invites", send("a@x.co")) // no panel address for the link
	if list := e.list("/api/invites"); len(list) != 0 {
		t.Fatalf("a refused request made an invite: %v", list)
	}

	e.srv.SetAuthPublicURL(e.url)
	if got := e.must(200, "GET", "/api/invites/mail", nil); got["available"] != true || got["channel"] != "Alerts" {
		t.Fatalf("mail = %v", got)
	}
	inv := e.must(201, "POST", "/api/invites", send("New@X.co"))
	token, _ := inv["token"].(string)
	if inv["emailed"] != true || token == "" || mailer.to != "new@x.co" ||
		!strings.Contains(mailer.text, e.url+"/invite?token="+token) || !strings.Contains(mailer.text, "a@b.co") {
		t.Fatalf("invite = %v, mail to %q: %s", inv, mailer.to, mailer.text)
	}
	// The mailed link is the invite: it opens the account.
	member := e.as()
	member.must(201, "POST", "/api/invites/accept", map[string]any{"token": token, "password": "member-pass"})

	mailer.fail = errors.New("smtp: connection refused")
	inv = e.must(201, "POST", "/api/invites", send("b@x.co"))
	if inv["emailed"] == true || inv["email_error"] != "smtp: connection refused" || inv["token"] == "" {
		t.Fatalf("invite with a failed mail = %v", inv)
	}
	// Without send nothing is mailed.
	mailer.fail, mailer.to = nil, ""
	if inv := e.must(201, "POST", "/api/invites", map[string]any{"email": "c@x.co", "role": "maintainer"}); mailer.to != "" || inv["emailed"] == true {
		t.Fatalf("mailed without being asked: %v", inv)
	}
}

// TestTransferOwnership: the owner hands the panel to a member after confirming their
// password; they become an admin, and only the new owner manages users from then on.
func TestTransferOwnership(t *testing.T) {
	e := newEnv(t)
	e.srv.authRate.max = 1000
	e.signIn()
	admin := e.join("admin@b.co", "admin")
	var ownerID, adminID string
	for _, u := range e.list("/api/users") {
		if u["role"] == "owner" {
			ownerID = u["id"].(string)
		} else {
			adminID = u["id"].(string)
		}
	}
	good := map[string]any{"password": "s3cret-pass"}

	admin.must(403, "POST", "/api/users/"+ownerID+"/make-owner", map[string]any{"password": "member-pass"})
	e.must(401, "POST", "/api/users/"+adminID+"/make-owner", map[string]any{})
	e.must(401, "POST", "/api/users/"+adminID+"/make-owner", map[string]any{"password": "wrong"})
	e.must(409, "POST", "/api/users/"+ownerID+"/make-owner", good)
	e.must(404, "POST", "/api/users/nobody/make-owner", good)
	if me := e.must(200, "GET", "/api/auth/me", nil); me["role"] != "owner" {
		t.Fatalf("a refused transfer changed the owner: %v", me)
	}

	if u := e.must(200, "POST", "/api/users/"+adminID+"/make-owner", good); u["role"] != "owner" || u["email"] != "admin@b.co" {
		t.Fatalf("new owner = %v", u)
	}
	if me := e.must(200, "GET", "/api/auth/me", nil); me["role"] != "admin" {
		t.Fatalf("the former owner is %v", me["role"])
	}
	e.must(403, "POST", "/api/invites", map[string]any{"email": "x@b.co", "role": "admin"})
	admin.must(201, "POST", "/api/invites", map[string]any{"email": "x@b.co", "role": "admin"})
	owners := 0
	for _, u := range admin.list("/api/users") {
		if u["role"] == "owner" {
			owners++
		}
	}
	if owners != 1 {
		t.Fatalf("%d owners", owners)
	}
}

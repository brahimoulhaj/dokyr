package update

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestParseRef(t *testing.T) {
	for _, tc := range []struct{ in, registry, repo, tag, repository string }{
		{"dokyr", "docker.io", "library/dokyr", "latest", "dokyr"},
		{"dokyr:0.1.0", "docker.io", "library/dokyr", "0.1.0", "dokyr"},
		{"me/dokyr:1.0.0", "docker.io", "me/dokyr", "1.0.0", "me/dokyr"},
		{"ghcr.io/me/dokyr:0.2.0", "ghcr.io", "me/dokyr", "0.2.0", "ghcr.io/me/dokyr"},
		{"localhost:5000/dokyr:0.1.0", "localhost:5000", "dokyr", "0.1.0", "localhost:5000/dokyr"},
		{"10.0.0.5:5000/team/dokyr", "10.0.0.5:5000", "team/dokyr", "latest", "10.0.0.5:5000/team/dokyr"},
	} {
		r, err := ParseRef(tc.in)
		if err != nil || r.Registry != tc.registry || r.Repo != tc.repo || r.Tag != tc.tag || r.Repository() != tc.repository {
			t.Errorf("ParseRef(%q) = %+v (%s), %v", tc.in, r, r.Repository(), err)
		}
	}
	for _, bad := range []string{"", "dokyr@sha256:abc", "ghcr.io/me/dokyr:"} {
		if _, err := ParseRef(bad); err == nil {
			t.Errorf("ParseRef(%q) accepted", bad)
		}
	}
}

func TestNewest(t *testing.T) {
	tags := []string{"latest", "0.1.0", "0.1.1", "0.2.0-rc.1", "v0.1.10", "0.1.9", "main", "1.0.0-beta"}
	cur, _ := ParseVersion("0.1.0")
	if v, ok := Newest(cur, tags); !ok || v.Tag != "v0.1.10" {
		t.Fatalf("newest stable = %+v %v", v, ok)
	}
	top, _ := ParseVersion("v0.1.10")
	if v, ok := Newest(top, tags); ok {
		t.Fatalf("nothing newer expected, got %v", v)
	}
	pre, _ := ParseVersion("0.2.0-beta.2")
	if v, ok := Newest(pre, tags); !ok || v.Tag != "1.0.0-beta" {
		t.Fatalf("pre-release track = %+v %v", v, ok)
	}
	a, _ := ParseVersion("1.0.0-rc.2")
	b, _ := ParseVersion("1.0.0-rc.10")
	c, _ := ParseVersion("1.0.0")
	if !a.Less(b) || !b.Less(c) || c.Less(a) {
		t.Fatal("pre-release ordering")
	}
}

// A registry that wants an anonymous token and pages its tag list.
func TestTagsWithTokenAndPages(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			if r.URL.Query().Get("scope") != "repository:me/dokyr:pull" || r.URL.Query().Get("service") != "test" {
				http.Error(w, "bad scope", 400)
				return
			}
			fmt.Fprint(w, `{"token":"t0k"}`)
		case r.Header.Get("Authorization") != "Bearer t0k":
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="test",scope="x"`, srv.URL))
			w.WriteHeader(401)
		case r.URL.Query().Get("last") == "":
			w.Header().Set("Link", `</v2/me/dokyr/tags/list?n=2&last=0.1.1>; rel="next"`)
			fmt.Fprint(w, `{"tags":["0.1.0","0.1.1"]}`)
		default:
			fmt.Fprint(w, `{"tags":["0.2.0"]}`)
		}
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	g := &Registry{HTTP: srv.Client(), Insecure: func(h string) bool { return h == host }}
	r, _ := ParseRef(host + "/me/dokyr:0.1.0")
	tags, err := g.Tags(context.Background(), r)
	if err != nil || strings.Join(tags, ",") != "0.1.0,0.1.1,0.2.0" {
		t.Fatalf("tags = %v, %v", tags, err)
	}
}

func TestNextPage(t *testing.T) {
	got := nextPage("https://r.example/v2/a/tags/list?n=2", `</v2/a/tags/list?last=b&n=2>; rel="next"`)
	u, _ := url.Parse(got)
	if u.Host != "r.example" || u.Query().Get("last") != "b" {
		t.Fatalf("next = %s", got)
	}
	if nextPage("https://r.example/x", "") != "" {
		t.Fatal("no link should end paging")
	}
}

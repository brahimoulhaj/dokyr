package database

import (
	"net/url"
	"testing"

	"panel/internal/store"
)

func TestConnectionFormats(t *testing.T) {
	for _, tc := range []struct{ engine, scheme, port, query string }{
		{"postgres", "postgresql", "5432", "sslmode=disable"},
		{"mariadb", "mysql", "3306", ""},
	} {
		t.Run(tc.engine, func(t *testing.T) {
			d := store.DBInstance{ID: "TEST", Engine: tc.engine, DatabaseName: "app", Username: "app"}
			password := "special:@/?#&=secret"
			c := Connection(d, map[string]string{"password": password})
			uri, err := url.Parse(c["URL"])
			if err != nil || uri.Scheme != tc.scheme || uri.User == nil || uri.User.Username() != "app" {
				t.Fatal("standard URI changed")
			}
			if got, _ := uri.User.Password(); got != password {
				t.Fatal("standard URI lost password encoding")
			}
			if uri.Hostname() != ContainerName(d.ID) || uri.Port() != tc.port || uri.Path != "/app" || uri.RawQuery != tc.query {
				t.Fatal("connection URI has incorrect endpoint or options")
			}
			if c["HOST"] != uri.Hostname() || c["PORT"] != tc.port || c["DATABASE"] != "app" || c["USERNAME"] != "app" || c["PASSWORD"] != password {
				t.Fatal("individual connection fields missing")
			}
		})
	}
}

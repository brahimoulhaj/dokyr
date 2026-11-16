package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"panel/internal/brand"
)

type adminerTransport func(*http.Request) (*http.Response, error)

func (f adminerTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAdminerProxyIsolation(t *testing.T) {
	target, _ := url.Parse("http://adminer:8080")
	prefix := "/api/databases/example/adminer/"
	proxy := adminerProxy(target, "trusted-key", "encrypted-on-backend-only", prefix)
	proxy.Transport = adminerTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "adminer:8080" || r.URL.Path != "/index.php" || r.URL.Query().Get("sql") != "" {
			t.Fatalf("bad upstream: %s", r.URL)
		}
		if r.Header.Get("X-Panel-Gateway") != "trusted-key" || r.Header.Get("X-Panel-Connection") != "encrypted-on-backend-only" || r.Header.Get("X-Panel-Path") != prefix || r.Header.Get("X-Panel-Https") != "1" {
			t.Fatal("untrusted proxy metadata")
		}
		if r.Header.Get("Authorization") != "" || strings.Contains(r.Header.Get("Cookie"), "panel-secret") || r.Header.Get("Cookie") != "panel_adminer=php-session" {
			t.Fatal("panel credentials forwarded")
		}
		h := make(http.Header)
		h.Add("Set-Cookie", "panel_adminer=new-session; Path=/; Domain=adminer")
		h.Add("Set-Cookie", brand.SessionCookie()+"=unexpected; Path=/")
		h.Set("Location", "http://adminer:8080/index.php?server=db&username=app")
		return &http.Response{StatusCode: 302, Header: h, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	req := httptest.NewRequest("GET", "https://panel.example"+prefix+"?sql=", nil)
	req.Header.Set("Authorization", "Bearer private")
	req.Header.Set("X-Panel-Gateway", "attacker")
	req.Header.Set("X-Panel-Connection", "attacker")
	req.AddCookie(&http.Cookie{Name: brand.SessionCookie(), Value: "panel-secret"})
	req.AddCookie(&http.Cookie{Name: "panel_adminer", Value: "php-session"})
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)
	// "no-referrer" would make browsers send "Origin: null" on Adminer's POSTs.
	if rec.Header().Get("Referrer-Policy") != "same-origin" {
		t.Fatalf("referrer policy %q breaks same-origin POSTs", rec.Header().Get("Referrer-Policy"))
	}
	if rec.Code != 302 || rec.Header().Get("Location") != prefix+"?server=db&username=app" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unsafe response: %d %v", rec.Code, rec.Header())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "panel_adminer" || cookies[0].Path != prefix || cookies[0].Domain != "" || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("unscoped upstream cookies: %+v", cookies)
	}

}

// Package proxy runs Caddy in front of the panel and every app. The panel owns the whole
// Caddy configuration: it is rebuilt from the database and pushed through the admin API,
// never edited in place, so the database stays the single source of truth.
package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strings"
)

// Config is everything the Caddy configuration is derived from.
type Config struct {
	AdminListen   string // Caddy network address of the admin API, e.g. "unix//run/caddy/admin.sock"
	PanelUpstream string // host:port where Caddy reaches the panel
	// PanelToken, when set, is sent to the panel with every request in PanelTokenHeader:
	// the panel believes X-Forwarded-For only from requests that carry it.
	PanelToken    string
	PanelDomain   string // optional; served over HTTPS with an automatic certificate
	PanelHTTPOnly bool   // serve PanelDomain over plain HTTP only (CDN terminates TLS)
	// PanelDomainOnly drops requests for unknown hostnames (e.g. http://<server-ip>) instead
	// of sending them to the panel. Only honoured when PanelDomain is set, so the panel can
	// never become unreachable through the proxy.
	PanelDomainOnly bool
	ACMEEmail       string  // optional; the account email for the certificate authority
	Routes          []Route // built-in services on hostnames of their own (the registry)
	Sites           []Site  // project domains, each with its path routes to apps
}

// Site is a project domain: one hostname whose paths go to apps. Paths are matched longest
// first; a request no path matches answers 404.
//
// Host may be a wildcard ("*.example.com"): every name one label under example.com that
// no other site or route claims. No authority signs a wildcard without a DNS challenge, so
// each name gets its own certificate, issued on its first TLS handshake (on-demand TLS)
// once the panel has confirmed the name (AskPath).
type Site struct {
	Host     string
	HTTPOnly bool // plain HTTP only, no certificate or redirect (a CDN terminates TLS)
	Paths    []Path
}

// Path sends a path prefix ("/" or "/api", matching /api and /api/...) to an app.
type Path struct {
	ID        string   // stable id (the route's), addresses it in the admin API
	AppID     string   // SetUpstreams switches every path of an app at once
	Prefix    string   // "/" or "/api"
	Strip     bool     // drop the prefix before proxying: /api/users arrives as /users
	Upstreams []string // empty answers 503
}

func pathKey(id string) string { return "path-" + id }

// proxyIndex is the position of the reverse_proxy handler in a path's handle list: after
// the prefix rewrite when there is one.
func (p Path) proxyIndex() int {
	if p.Strip && p.Prefix != "/" {
		return 1
	}
	return 0
}

// Route sends requests for a set of hostnames to a set of upstreams. By default a host is
// served over HTTPS with an automatic certificate and plain HTTP is redirected to it. Hosts
// listed in HTTPOnly are served over plain HTTP instead, with no certificate and no
// redirect: for a CDN that terminates TLS itself and talks HTTP to the origin (Cloudflare's
// "Flexible" SSL), where a redirect to HTTPS would loop.
type Route struct {
	ID        string   // stable id, e.g. the app id; addresses the route in the admin API
	Hosts     []string // hostnames, lowercase
	HTTPOnly  []string // subset of Hosts served over plain HTTP only
	Upstreams []string // host:port on the shared network; empty answers 503
	// PanelPaths are path patterns ("/api/registry/token") served by the panel instead of
	// the upstreams: the built-in registry's token endpoint lives on the registry's host.
	// A route with them cannot be switched by SetUpstreams; Apply rebuilds it.
	PanelPaths []string
}

// split returns the hosts served over HTTPS and those served over plain HTTP.
func (r Route) split() (tls, plain []string) {
	for _, h := range r.Hosts {
		if contains(r.HTTPOnly, h) {
			plain = append(plain, h)
		} else {
			tls = append(tls, h)
		}
	}
	return tls, plain
}

// keys are the admin API ids of the Caddy routes this route turns into.
func (r Route) keys() []string {
	tls, plain := r.split()
	var keys []string
	if len(tls) > 0 {
		keys = append(keys, routeKey(r.ID))
	}
	if len(plain) > 0 {
		keys = append(keys, httpRouteKey(r.ID))
	}
	return keys
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Ports Caddy listens on inside its container. The host ports they are published on are
// a property of the container, not of this configuration.
const (
	httpListen  = ":80"
	httpsListen = ":443"
)

type object = map[string]any

// AskPath is the panel endpoint Caddy asks, with ?domain=<name>, before it issues a
// certificate on demand for a name under a wildcard site. 200 allows it.
const AskPath = "/api/proxy/ask"

func routeKey(id string) string     { return "route-" + id }
func httpRouteKey(id string) string { return "route-" + id + "-http" }

// Build returns the complete Caddy JSON configuration for c.
//
// Layout: an "https" server holds one route per hostname group; an "http" server redirects
// those hostnames to HTTPS, proxies HTTP-only hostnames directly, and sends everything else
// to the panel, which is what makes http://<server-ip> work on a fresh install.
// Certificates come from Caddy's automatic HTTPS, driven by the host matchers on the
// "https" server only, so HTTP-only hostnames never get one.
// PanelTokenHeader is the request header that carries Config.PanelToken to the panel.
const PanelTokenHeader = "X-Panel-Proxy-Token"

// toPanel is the handler that passes a request on to the panel. It replaces whatever
// token header the client sent with the real one, so only the proxy can present it.
func (c Config) toPanel() object {
	h := object{"handler": "reverse_proxy", "upstreams": []object{{"dial": c.PanelUpstream}}}
	if c.PanelToken != "" {
		h["headers"] = object{"request": object{"set": object{PanelTokenHeader: []string{c.PanelToken}}}}
	}
	return h
}

func Build(c Config) ([]byte, error) {
	if c.AdminListen == "" {
		return nil, errors.New("proxy config: admin listen address is required")
	}
	if err := validUpstream(c.PanelUpstream); err != nil {
		return nil, fmt.Errorf("proxy config: panel upstream: %w", err)
	}

	routes := append([]Route(nil), c.Routes...)
	sort.Slice(routes, func(i, j int) bool { return routes[i].ID < routes[j].ID })
	if c.PanelDomain != "" {
		// The panel is a route like any other; listed first so its id is predictable.
		panel := Route{ID: "panel", Hosts: []string{c.PanelDomain}, Upstreams: []string{c.PanelUpstream}}
		if c.PanelHTTPOnly {
			panel.HTTPOnly = []string{c.PanelDomain}
		}
		routes = append([]Route{panel}, routes...)
	}

	seenID := map[string]bool{}
	seenHost := map[string]string{}
	var httpsRoutes, httpRoutes []object
	var allHosts []string
	for _, r := range routes {
		if r.ID == "" || strings.ContainsAny(r.ID, "/ \t\n") {
			return nil, fmt.Errorf("proxy config: invalid route id %q", r.ID)
		}
		if seenID[r.ID] {
			return nil, fmt.Errorf("proxy config: duplicate route id %q", r.ID)
		}
		seenID[r.ID] = true
		if len(r.Hosts) == 0 {
			return nil, fmt.Errorf("proxy config: route %s has no hostnames", r.ID)
		}
		for _, h := range r.Hosts {
			if err := ValidHost(h); err != nil {
				return nil, fmt.Errorf("proxy config: route %s: %w", r.ID, err)
			}
			if other, dup := seenHost[h]; dup {
				return nil, fmt.Errorf("proxy config: hostname %s is used by routes %s and %s", h, other, r.ID)
			}
			seenHost[h] = r.ID
		}
		for _, h := range r.HTTPOnly {
			if !contains(r.Hosts, h) {
				return nil, fmt.Errorf("proxy config: route %s: http-only host %s is not one of its hosts", r.ID, h)
			}
		}
		upstreams := make([]object, 0, len(r.Upstreams))
		for _, u := range r.Upstreams {
			if err := validUpstream(u); err != nil {
				return nil, fmt.Errorf("proxy config: route %s: %w", r.ID, err)
			}
			upstreams = append(upstreams, object{"dial": u})
		}
		tlsHosts, plainHosts := r.split()
		proxyTo := []object{{"handler": "reverse_proxy", "upstreams": upstreams}}
		if r.ID == "panel" && c.PanelDomain != "" {
			proxyTo = []object{c.toPanel()}
		}
		if len(r.PanelPaths) > 0 {
			proxyTo = []object{{"handler": "subroute", "routes": []object{
				{"match": []object{{"path": r.PanelPaths}}, "handle": []object{c.toPanel()}, "terminal": true},
				{"handle": proxyTo},
			}}}
		}
		if len(tlsHosts) > 0 {
			allHosts = append(allHosts, tlsHosts...)
			httpsRoutes = append(httpsRoutes, object{
				"@id":      routeKey(r.ID),
				"match":    []object{{"host": tlsHosts}},
				"handle":   proxyTo,
				"terminal": true,
			})
			httpRoutes = append(httpRoutes, object{
				"match": []object{{"host": tlsHosts}},
				"handle": []object{{
					"handler":     "static_response",
					"status_code": 308,
					"headers":     object{"Location": []string{"https://{http.request.host}{http.request.uri}"}},
				}},
				"terminal": true,
			})
		}
		if len(plainHosts) > 0 {
			httpRoutes = append(httpRoutes, object{
				"@id":      httpRouteKey(r.ID),
				"match":    []object{{"host": plainHosts}},
				"handle":   proxyTo,
				"terminal": true,
			})
		}
	}

	// Routes are matched in order: exact hostnames go ahead of the wildcards covering them.
	sites := append([]Site(nil), c.Sites...)
	sort.SliceStable(sites, func(i, j int) bool { return !IsWildcard(sites[i].Host) && IsWildcard(sites[j].Host) })
	for _, site := range sites {
		if err := ValidSiteHost(site.Host); err != nil {
			return nil, fmt.Errorf("proxy config: site: %w", err)
		}
		if other, dup := seenHost[site.Host]; dup {
			return nil, fmt.Errorf("proxy config: hostname %s is used by %s and a project domain", site.Host, other)
		}
		seenHost[site.Host] = "site"
		paths := append([]Path(nil), site.Paths...)
		sort.SliceStable(paths, func(i, j int) bool { return len(paths[i].Prefix) > len(paths[j].Prefix) })
		var sub []object
		catchAll := false
		for _, p := range paths {
			ups := make([]object, 0, len(p.Upstreams))
			for _, u := range p.Upstreams {
				if err := validUpstream(u); err != nil {
					return nil, fmt.Errorf("proxy config: %s%s: %w", site.Host, p.Prefix, err)
				}
				ups = append(ups, object{"dial": u})
			}
			var handle []object
			if p.proxyIndex() == 1 {
				handle = append(handle, object{"handler": "rewrite", "strip_path_prefix": p.Prefix})
			}
			handle = append(handle, object{"handler": "reverse_proxy", "upstreams": ups})
			route := object{"@id": pathKey(p.ID), "handle": handle, "terminal": true}
			if p.Prefix == "/" {
				catchAll = true
			} else {
				route["match"] = []object{{"path": []string{p.Prefix, p.Prefix + "/*"}}}
			}
			sub = append(sub, route)
		}
		if !catchAll {
			sub = append(sub, object{"handle": []object{{"handler": "static_response", "status_code": 404}}})
		}
		siteRoute := object{
			"match":    []object{{"host": []string{site.Host}}},
			"handle":   []object{{"handler": "subroute", "routes": sub}},
			"terminal": true,
		}
		if site.HTTPOnly {
			httpRoutes = append(httpRoutes, siteRoute)
			continue
		}
		allHosts = append(allHosts, site.Host)
		httpsRoutes = append(httpsRoutes, siteRoute)
		httpRoutes = append(httpRoutes, object{
			"match": []object{{"host": []string{site.Host}}},
			"handle": []object{{
				"handler":     "static_response",
				"status_code": 308,
				"headers":     object{"Location": []string{"https://{http.request.host}{http.request.uri}"}},
			}},
			"terminal": true,
		})
	}

	// Last on the HTTP server: anything that is not a known hostname reaches the panel, or,
	// once the panel has its own domain and IP access is off, the connection is dropped.
	if c.PanelDomainOnly && c.PanelDomain != "" {
		httpRoutes = append(httpRoutes, object{
			"@id":    "panel-ip-blocked",
			"handle": []object{{"handler": "static_response", "abort": true}},
		})
	} else {
		httpRoutes = append(httpRoutes, object{
			"handle": []object{c.toPanel()},
		})
	}

	servers := object{
		"http": object{"listen": []string{httpListen}, "routes": httpRoutes},
	}
	if len(httpsRoutes) > 0 {
		servers["https"] = object{
			"listen": []string{httpsListen},
			"routes": httpsRoutes,
			// The redirects are written out above, ahead of the panel catch-all.
			"automatic_https": object{"disable_redirects": true},
		}
	}

	apps := object{"http": object{"servers": servers}}
	if policies, onDemand := tlsPolicies(allHosts, c.ACMEEmail); len(policies) > 0 {
		automation := object{"policies": policies}
		if onDemand {
			// Without this check anyone pointing a name at the server could make it request
			// certificates without end.
			automation["on_demand"] = object{"permission": object{
				"module": "http", "endpoint": "http://" + c.PanelUpstream + AskPath,
			}}
		}
		apps["tls"] = object{"automation": automation}
	}

	return json.MarshalIndent(object{
		// persist=false: the panel writes the boot configuration itself (see Manager).
		"admin": object{"listen": c.AdminListen, "config": object{"persist": false}},
		"apps":  apps,
	}, "", "  ")
}

// tlsPolicies gives names no public authority will sign (localhost, .test, …) a certificate
// from Caddy's internal authority, and sets the account email for everything else.
// Wildcard hosts get on-demand policies: a certificate per name, at its first handshake.
// The first policy matching a name wins, so exact names come before the wildcards that
// would otherwise cover them, and the catch-all comes last. onDemand reports whether any
// policy issues on demand.
func tlsPolicies(hosts []string, email string) (policies []object, onDemand bool) {
	var internal, wildInternal, wildPublic, covered []string
	for _, h := range hosts {
		switch {
		case IsWildcard(h) && isInternalName(h):
			wildInternal = append(wildInternal, h)
		case IsWildcard(h):
			wildPublic = append(wildPublic, h)
		case isInternalName(h):
			internal = append(internal, h)
		}
	}
	// Exact public names under a wildcard keep their certificate managed up front.
	for _, h := range hosts {
		if !IsWildcard(h) && !isInternalName(h) && contains(wildPublic, WildcardOf(h)) {
			covered = append(covered, h)
		}
	}
	acme := object{"module": "acme"}
	if email != "" {
		acme["email"] = email
	}
	if len(internal) > 0 {
		policies = append(policies, object{"subjects": internal, "issuers": []object{{"module": "internal"}}})
	}
	if len(covered) > 0 {
		policies = append(policies, object{"subjects": covered, "issuers": []object{acme}})
	}
	if len(wildInternal) > 0 {
		policies = append(policies, object{"subjects": wildInternal, "issuers": []object{{"module": "internal"}}, "on_demand": true})
	}
	if len(wildPublic) > 0 {
		policies = append(policies, object{"subjects": wildPublic, "issuers": []object{acme}, "on_demand": true})
	}
	if email != "" {
		policies = append(policies, object{"issuers": []object{acme}})
	}
	return policies, len(wildInternal)+len(wildPublic) > 0
}

// IsWildcard reports whether h is a wildcard hostname ("*.example.com").
func IsWildcard(h string) bool { return strings.HasPrefix(h, "*.") }

// WildcardOf returns the wildcard hostname covering h ("*.example.com" for
// "shop.example.com"), or "" when h has a single label.
func WildcardOf(h string) string {
	_, parent, ok := strings.Cut(h, ".")
	if !ok {
		return ""
	}
	return "*." + parent
}

// ValidSiteHost is ValidHost for project domains, which may also be a wildcard one label
// deep ("*.example.com").
func ValidSiteHost(h string) error {
	if IsWildcard(h) {
		if err := ValidHost(h[2:]); err != nil {
			return fmt.Errorf("invalid hostname %q", h)
		}
		return nil
	}
	return ValidHost(h)
}

func isInternalName(h string) bool {
	if h == "localhost" {
		return true
	}
	for _, suffix := range []string{".localhost", ".test", ".internal", ".local", ".example", ".invalid"} {
		if strings.HasSuffix(h, suffix) {
			return true
		}
	}
	return false
}

// ValidHost reports whether h is a hostname the proxy can route: a lowercase DNS name
// with at least two labels (or "localhost"), no wildcard, no port, not an IP address.
func ValidHost(h string) error {
	if h == "" || len(h) > 253 {
		return fmt.Errorf("invalid hostname %q", h)
	}
	if _, err := netip.ParseAddr(h); err == nil {
		return fmt.Errorf("invalid hostname %q: an IP address cannot get a certificate", h)
	}
	labels := strings.Split(h, ".")
	if len(labels) < 2 && h != "localhost" {
		return fmt.Errorf("invalid hostname %q", h)
	}
	for _, l := range labels {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return fmt.Errorf("invalid hostname %q", h)
		}
		for _, r := range l {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return fmt.Errorf("invalid hostname %q", h)
			}
		}
	}
	return nil
}

func validUpstream(u string) error {
	host, port, err := net.SplitHostPort(u)
	if err != nil || host == "" || port == "" {
		return fmt.Errorf("invalid upstream %q, want host:port", u)
	}
	return nil
}

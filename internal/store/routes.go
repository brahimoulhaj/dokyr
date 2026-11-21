package store

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"

	"panel/internal/id"
)

// Route sends a path of a hostname (and everything under it) to an app. The hostname is
// the route's own: there is no list of domains to add first, and a hostname belongs to no
// project. Routes of one hostname share a TLS mode.
type Route struct {
	ID          string
	Hostname    string // "shop.example.com", or "*.example.com" for every name one label under example.com
	Path        string // "/" or a prefix like "/api"
	AppID       string
	AppName     string
	ProjectID   string // the app's
	ProjectName string
	StripPrefix bool   // send /api/users to the app as /users
	TLSMode     string // the hostname's
	CreatedAt   time.Time
}

// RouteInput is what a route is made or changed from.
type RouteInput struct {
	Hostname    string
	Path        string
	AppID       string
	StripPrefix bool
	// TLSMode applies when the hostname has no route yet; otherwise the route takes the
	// hostname's. "" is TLSAuto.
	TLSMode string
}

// wildcardPrefix starts the hostname of a route that answers on every name one label
// under a domain that no exact hostname claims.
const wildcardPrefix = "*."

const routeSelect = `SELECT r.id, r.hostname, r.path, r.app_id, a.name, p.id, p.name, r.strip_prefix, r.tls_mode, r.created_at
	FROM routes r JOIN apps a ON a.id = r.app_id JOIN projects p ON p.id = a.project_id`

func scanRoute(row scanner) (Route, error) {
	var r Route
	var created string
	err := row.Scan(&r.ID, &r.Hostname, &r.Path, &r.AppID, &r.AppName, &r.ProjectID, &r.ProjectName, &r.StripPrefix, &r.TLSMode, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Route{}, ErrNotFound
	}
	r.CreatedAt = parseTime(created)
	return r, err
}

func (s *Store) listRoutes(ctx context.Context, where string, args ...any) ([]Route, error) {
	rows, err := s.db.QueryContext(ctx, routeSelect+` WHERE `+where+` ORDER BY r.hostname, r.path`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Route{}
	for rows.Next() {
		r, err := scanRoute(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CreateRoute adds a path of a hostname; ErrConflict when the hostname already routes it.
func (s *Store) CreateRoute(ctx context.Context, in RouteInput) (Route, error) {
	if in.TLSMode == "" {
		in.TLSMode = TLSAuto
	}
	rid, ts := id.New(), now()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO routes (id, hostname, path, app_id, strip_prefix, tls_mode, created_at, updated_at)
		VALUES (?1, ?2, ?3, ?4, ?5, COALESCE((SELECT tls_mode FROM routes WHERE hostname = ?2 LIMIT 1), ?6), ?7, ?7)`,
		rid, in.Hostname, in.Path, in.AppID, in.StripPrefix, in.TLSMode, ts)
	if err != nil {
		return Route{}, conflict(err)
	}
	return s.GetRoute(ctx, rid)
}

func (s *Store) GetRoute(ctx context.Context, routeID string) (Route, error) {
	return scanRoute(s.db.QueryRowContext(ctx, routeSelect+` WHERE r.id = ?`, routeID))
}

// UpdateRoute changes a route's hostname, path, app or prefix handling; ErrConflict on a
// taken path. Moved to a hostname that has routes, it takes that hostname's TLS mode;
// otherwise it keeps its own.
func (s *Store) UpdateRoute(ctx context.Context, routeID string, in RouteInput) (Route, error) {
	_, err := s.db.ExecContext(ctx,
		`UPDATE routes SET
			tls_mode = COALESCE((SELECT o.tls_mode FROM routes o WHERE o.hostname = ?2 AND o.id <> ?1 LIMIT 1), tls_mode),
			hostname = ?2, path = ?3, app_id = ?4, strip_prefix = ?5, updated_at = ?6
		WHERE id = ?1`,
		routeID, in.Hostname, in.Path, in.AppID, in.StripPrefix, now())
	if err != nil {
		return Route{}, conflict(err)
	}
	return s.GetRoute(ctx, routeID)
}

func (s *Store) DeleteRoute(ctx context.Context, routeID string) error {
	return affected(s.db.ExecContext(ctx, `DELETE FROM routes WHERE id = ?`, routeID))
}

// AllRoutes lists every route, by hostname then path.
func (s *Store) AllRoutes(ctx context.Context) ([]Route, error) {
	return s.listRoutes(ctx, `1 = 1`)
}

// AppRoutes lists the routes that send traffic to an app.
func (s *Store) AppRoutes(ctx context.Context, appID string) ([]Route, error) {
	return s.listRoutes(ctx, `r.app_id = ?`, appID)
}

// HostRoutes lists the routes of one hostname; none when nothing is routed there.
func (s *Store) HostRoutes(ctx context.Context, hostname string) ([]Route, error) {
	return s.listRoutes(ctx, `r.hostname = ?`, hostname)
}

// HostRouted reports whether a route answers on exactly this hostname. A wildcard route
// takes no name: "shop.example.com" is free while only "*.example.com" is routed.
func (s *Store) HostRouted(ctx context.Context, hostname string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM routes WHERE hostname = ?`, hostname).Scan(&n)
	return n > 0, err
}

// SetHostTLS changes the TLS mode of every route of a hostname; ErrNotFound when it has none.
func (s *Store) SetHostTLS(ctx context.Context, hostname, tlsMode string) error {
	return affected(s.db.ExecContext(ctx, `UPDATE routes SET tls_mode = ?, updated_at = ? WHERE hostname = ?`, tlsMode, now(), hostname))
}

// DeleteHost removes every route of a hostname; ErrNotFound when it has none.
func (s *Store) DeleteHost(ctx context.Context, hostname string) error {
	return affected(s.db.ExecContext(ctx, `DELETE FROM routes WHERE hostname = ?`, hostname))
}

// WildcardServesTLS reports whether "*.<parent>" is routed and served with certificates
// (not HTTP-only): names under parent may then get one on demand.
func (s *Store) WildcardServesTLS(ctx context.Context, parent string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM routes WHERE hostname = ? AND tls_mode <> ?`,
		wildcardPrefix+parent, TLSHTTPOnly).Scan(&n)
	return n > 0, err
}

// ProxySite is the routing state of one hostname: its paths, longest first.
type ProxySite struct {
	Host     string
	HTTPOnly bool
	Paths    []ProxyPath
}

// ProxyPath is one route as the proxy needs it.
type ProxyPath struct {
	RouteID  string
	AppID    string
	Path     string
	Strip    bool
	Upstream string // container:port; empty when the app is stopped or was never deployed
}

// ProxySites is what the proxy's app routing is built from: every routed hostname with
// its routes.
func (s *Store) ProxySites(ctx context.Context) ([]ProxySite, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.hostname, r.tls_mode, r.id, r.path, r.strip_prefix, a.id, a.desired_state, COALESCE(dp.container_name, ''), COALESCE(dp.port, 0)
		FROM routes r
		JOIN apps a ON a.id = r.app_id
		LEFT JOIN deployments dp ON dp.id = a.current_deployment_id
		ORDER BY r.hostname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProxySite
	for rows.Next() {
		var host, mode, routeID, path, appID, state, container string
		var strip bool
		var port int
		if err := rows.Scan(&host, &mode, &routeID, &path, &strip, &appID, &state, &container, &port); err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].Host != host {
			out = append(out, ProxySite{Host: host, HTTPOnly: mode == TLSHTTPOnly})
		}
		p := ProxyPath{RouteID: routeID, AppID: appID, Path: path, Strip: strip}
		if state == StateRunning && container != "" {
			p.Upstream = Upstream(container, port)
		}
		out[len(out)-1].Paths = append(out[len(out)-1].Paths, p)
	}
	for i := range out {
		SortPaths(out[i].Paths)
	}
	return out, rows.Err()
}

// SortPaths orders routes the way they are matched: the longest path first, "/" last.
func SortPaths(paths []ProxyPath) {
	sort.SliceStable(paths, func(i, j int) bool {
		if len(paths[i].Path) != len(paths[j].Path) {
			return len(paths[i].Path) > len(paths[j].Path)
		}
		return paths[i].Path < paths[j].Path
	})
}

// NormalizePath turns what someone typed into a route path: a leading slash, no trailing
// one ("/" stays "/"). It reports false for anything that is not a plain path prefix.
func NormalizePath(p string) (string, bool) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "/", true
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if len(p) > 1 {
		p = strings.TrimRight(p, "/")
	}
	if len(p) > 200 {
		return "", false
	}
	for _, seg := range strings.Split(strings.TrimPrefix(p, "/"), "/") {
		if p == "/" {
			break
		}
		if seg == "" || seg == "." || seg == ".." {
			return "", false
		}
		for _, r := range seg {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-._~", r)) {
				return "", false
			}
		}
	}
	return p, true
}

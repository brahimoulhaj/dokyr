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

// Domain is a hostname served by this panel, with its certificate. It is added on its own
// and then used by at most one project, whose apps its routes send paths to.
type Domain struct {
	ID          string
	ProjectID   string // "" while no project uses it
	ProjectName string
	Hostname    string
	TLSMode     string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Route sends a path of a domain (and everything under it) to an app.
type Route struct {
	ID          string
	DomainID    string
	Hostname    string // the domain's, for display
	TLSMode     string // the domain's
	Path        string // "/" or a prefix like "/api"
	AppID       string
	AppName     string
	StripPrefix bool // send /api/users to the app as /users
	CreatedAt   time.Time
}

const domainSelect = `SELECT d.id, COALESCE(d.project_id, ''), COALESCE(p.name, ''), d.hostname, d.tls_mode, d.created_at, d.updated_at
	FROM domains d LEFT JOIN projects p ON p.id = d.project_id`

func scanDomain(row scanner) (Domain, error) {
	var d Domain
	var created, updated string
	err := row.Scan(&d.ID, &d.ProjectID, &d.ProjectName, &d.Hostname, &d.TLSMode, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Domain{}, ErrNotFound
	}
	d.CreatedAt, d.UpdatedAt = parseTime(created), parseTime(updated)
	return d, err
}

// CreateDomain adds a hostname, used by projectID or by no project yet when that is "".
// It returns ErrConflict when the hostname is already taken.
func (s *Store) CreateDomain(ctx context.Context, projectID, hostname, tlsMode string) (Domain, error) {
	if tlsMode == "" {
		tlsMode = TLSAuto
	}
	did, ts := id.New(), now()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO domains (id, project_id, hostname, tls_mode, created_at, updated_at) VALUES (?, NULLIF(?, ''), ?, ?, ?, ?)`,
		did, projectID, hostname, tlsMode, ts, ts)
	if err != nil {
		return Domain{}, conflict(err)
	}
	return s.GetDomain(ctx, did)
}

func (s *Store) GetDomain(ctx context.Context, domainID string) (Domain, error) {
	return scanDomain(s.db.QueryRowContext(ctx, domainSelect+` WHERE d.id = ?`, domainID))
}

func (s *Store) DomainExists(ctx context.Context, hostname string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM domains WHERE hostname = ?`, hostname).Scan(&n)
	return n > 0, err
}

func (s *Store) listDomains(ctx context.Context, where string, args ...any) ([]Domain, error) {
	rows, err := s.db.QueryContext(ctx, domainSelect+where+` ORDER BY d.hostname`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Domain{}
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ListDomains lists the domains a project uses.
func (s *Store) ListDomains(ctx context.Context, projectID string) ([]Domain, error) {
	return s.listDomains(ctx, ` WHERE d.project_id = ?`, projectID)
}

// AllDomains lists every domain, used by a project or not.
func (s *Store) AllDomains(ctx context.Context) ([]Domain, error) {
	return s.listDomains(ctx, ``)
}

// AttachDomain gives an unused domain to a project. ErrConflict when another project
// already uses it; attaching it again to its own project changes nothing.
func (s *Store) AttachDomain(ctx context.Context, domainID, projectID string) (Domain, error) {
	err := affected(s.db.ExecContext(ctx,
		`UPDATE domains SET project_id = ?, updated_at = ? WHERE id = ? AND project_id IS NULL`, projectID, now(), domainID))
	if errors.Is(err, ErrNotFound) {
		d, getErr := s.GetDomain(ctx, domainID)
		if getErr != nil {
			return Domain{}, getErr
		}
		if d.ProjectID != projectID {
			return Domain{}, ErrConflict
		}
		return d, nil
	}
	if err != nil {
		return Domain{}, err
	}
	return s.GetDomain(ctx, domainID)
}

// DetachDomain takes a domain back from its project: its routes go (they point at that
// project's apps), the domain stays in the list for another project to use.
func (s *Store) DetachDomain(ctx context.Context, domainID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := affected(tx.ExecContext(ctx, `UPDATE domains SET project_id = NULL, updated_at = ? WHERE id = ?`, now(), domainID)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM domain_routes WHERE domain_id = ?`, domainID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SetDomainTLS(ctx context.Context, domainID, tlsMode string) (Domain, error) {
	if err := affected(s.db.ExecContext(ctx, `UPDATE domains SET tls_mode = ?, updated_at = ? WHERE id = ?`, tlsMode, now(), domainID)); err != nil {
		return Domain{}, err
	}
	return s.GetDomain(ctx, domainID)
}

// DeleteDomain removes a domain with its routes.
func (s *Store) DeleteDomain(ctx context.Context, domainID string) error {
	return affected(s.db.ExecContext(ctx, `DELETE FROM domains WHERE id = ?`, domainID))
}

const routeSelect = `SELECT r.id, r.domain_id, d.hostname, d.tls_mode, r.path, r.app_id, a.name, r.strip_prefix, r.created_at
	FROM domain_routes r JOIN domains d ON d.id = r.domain_id JOIN apps a ON a.id = r.app_id`

func scanRoute(row scanner) (Route, error) {
	var r Route
	var created string
	err := row.Scan(&r.ID, &r.DomainID, &r.Hostname, &r.TLSMode, &r.Path, &r.AppID, &r.AppName, &r.StripPrefix, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Route{}, ErrNotFound
	}
	r.CreatedAt = parseTime(created)
	return r, err
}

func (s *Store) listRoutes(ctx context.Context, where string, args ...any) ([]Route, error) {
	rows, err := s.db.QueryContext(ctx, routeSelect+` WHERE `+where+` ORDER BY d.hostname, r.path`, args...)
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

// CreateRoute adds a path of a domain; ErrConflict when the domain already routes that path.
func (s *Store) CreateRoute(ctx context.Context, domainID, path, appID string, strip bool) (Route, error) {
	rid, ts := id.New(), now()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO domain_routes (id, domain_id, path, app_id, strip_prefix, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		rid, domainID, path, appID, strip, ts, ts)
	if err != nil {
		return Route{}, conflict(err)
	}
	return s.GetRoute(ctx, rid)
}

func (s *Store) GetRoute(ctx context.Context, routeID string) (Route, error) {
	return scanRoute(s.db.QueryRowContext(ctx, routeSelect+` WHERE r.id = ?`, routeID))
}

// UpdateRoute changes a route's path, app or prefix handling; ErrConflict on a taken path.
func (s *Store) UpdateRoute(ctx context.Context, routeID, path, appID string, strip bool) (Route, error) {
	_, err := s.db.ExecContext(ctx, `UPDATE domain_routes SET path = ?, app_id = ?, strip_prefix = ?, updated_at = ? WHERE id = ?`,
		path, appID, strip, now(), routeID)
	if err != nil {
		return Route{}, conflict(err)
	}
	return s.GetRoute(ctx, routeID)
}

func (s *Store) DeleteRoute(ctx context.Context, routeID string) error {
	return affected(s.db.ExecContext(ctx, `DELETE FROM domain_routes WHERE id = ?`, routeID))
}

// ProjectRoutes lists every route of a project's domains.
func (s *Store) ProjectRoutes(ctx context.Context, projectID string) ([]Route, error) {
	return s.listRoutes(ctx, `d.project_id = ?`, projectID)
}

// AllRoutes lists the routes of every domain.
func (s *Store) AllRoutes(ctx context.Context) ([]Route, error) {
	return s.listRoutes(ctx, `1 = 1`)
}

// AppRoutes lists the routes that send traffic to an app.
func (s *Store) AppRoutes(ctx context.Context, appID string) ([]Route, error) {
	return s.listRoutes(ctx, `r.app_id = ?`, appID)
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

// ProxySites is what the proxy's app routing is built from: every domain, with its routes.
func (s *Store) ProxySites(ctx context.Context) ([]ProxySite, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT dm.hostname, dm.tls_mode, COALESCE(r.id, ''), COALESCE(r.path, ''), COALESCE(r.strip_prefix, 0), COALESCE(a.id, ''),
			a.desired_state, COALESCE(dp.container_name, ''), COALESCE(dp.port, 0)
		FROM domains dm
		LEFT JOIN domain_routes r ON r.domain_id = dm.id
		LEFT JOIN apps a ON a.id = r.app_id
		LEFT JOIN deployments dp ON dp.id = a.current_deployment_id
		ORDER BY dm.hostname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProxySite
	for rows.Next() {
		var host, mode, routeID, path, appID, container string
		var state sql.NullString
		var strip bool
		var port int
		if err := rows.Scan(&host, &mode, &routeID, &path, &strip, &appID, &state, &container, &port); err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].Host != host {
			out = append(out, ProxySite{Host: host, HTTPOnly: mode == TLSHTTPOnly})
		}
		if routeID == "" {
			continue // a domain without routes yet
		}
		p := ProxyPath{RouteID: routeID, AppID: appID, Path: path, Strip: strip}
		if state.String == StateRunning && container != "" {
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

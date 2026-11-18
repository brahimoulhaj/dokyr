package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"panel/internal/id"
)

// ErrConflict is returned when a write breaks a uniqueness rule or would orphan rows.
var ErrConflict = errors.New("store: conflict")

func conflict(err error) error {
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return ErrConflict
	}
	return err
}

func affected(res sql.Result, err error) error {
	if err != nil {
		return conflict(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

type scanner interface{ Scan(...any) error }

// ---- projects ----

type Project struct {
	ID          string
	Name        string
	Description string
	AppCount    int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

const projectSelect = `SELECT p.id, p.name, p.description,
	(SELECT COUNT(*) FROM apps a WHERE a.project_id = p.id), p.created_at, p.updated_at FROM projects p`

func scanProject(row scanner) (Project, error) {
	var p Project
	var created, updated string
	err := row.Scan(&p.ID, &p.Name, &p.Description, &p.AppCount, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	p.CreatedAt, p.UpdatedAt = parseTime(created), parseTime(updated)
	return p, err
}

func (s *Store) CreateProject(ctx context.Context, name, description string) (Project, error) {
	pid, ts := id.New(), now()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO projects (id, name, description, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		pid, name, description, ts, ts)
	if err != nil {
		return Project{}, conflict(err)
	}
	return s.GetProject(ctx, pid)
}

func (s *Store) GetProject(ctx context.Context, projectID string) (Project, error) {
	return scanProject(s.db.QueryRowContext(ctx, projectSelect+` WHERE p.id = ?`, projectID))
}

func (s *Store) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx, projectSelect+` ORDER BY p.name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) UpdateProject(ctx context.Context, projectID, name, description string) (Project, error) {
	err := affected(s.db.ExecContext(ctx,
		`UPDATE projects SET name = ?, description = ?, updated_at = ? WHERE id = ?`, name, description, now(), projectID))
	if err != nil {
		return Project{}, err
	}
	return s.GetProject(ctx, projectID)
}

// DeleteProject refuses with ErrConflict while the project still has workloads.
func (s *Store) DeleteProject(ctx context.Context, projectID string) error {
	err := affected(s.db.ExecContext(ctx,
		`DELETE FROM projects WHERE id = ? AND NOT EXISTS (SELECT 1 FROM apps WHERE project_id = ?)
        AND NOT EXISTS (SELECT 1 FROM db_instances WHERE project_id = ?)`, projectID, projectID, projectID))
	if errors.Is(err, ErrNotFound) {
		if _, getErr := s.GetProject(ctx, projectID); getErr == nil {
			return ErrConflict // it exists, so it was the apps that stopped the delete
		}
	}
	return err
}

// ---- apps ----

const (
	StateRunning = "running"
	StateStopped = "stopped"
)

type AppMount struct {
	Type     string `json:"type"`
	Source   string `json:"source"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"read_only"`
	Content  string `json:"content"`
}

// AppProcess is an extra container of the app: the same image, environment, mounts and
// limits, with its own command, for a queue worker or a scheduler. It gets no traffic.
type AppProcess struct {
	Name    string `json:"name"`    // slug, unique inside the app; part of the container name
	Command string `json:"command"` // run by /bin/sh -c
}

type App struct {
	Mounts              []AppMount
	Processes           []AppProcess // started with every deployment, next to the main container
	Environment         map[string]string
	EnvMeta             map[string]EnvVarMeta // scope and secrecy of the variables that are not the default
	EnvOrder            []string              // the variables as their owner ordered them, comments between: see NormalizeEnvOrder
	ID                  string
	ProjectID           string
	Name                string
	SourceType          string
	Image               string
	DeployHook          bool   // the deploy webhook is on (its token is read with AppDeployHook)
	PushDeploy          bool   // an image on the built-in registry deploys when its tag is pushed
	RegistryID          string // registry an image app is pulled from with credentials; "" = public
	StartCommand        string // replaces the image's entrypoint and command, run by /bin/sh -c; "" = the image's own
	Port                int
	CPULimit            float64
	MemoryMB            int64
	DesiredState        string
	CurrentDeploymentID string    // empty until the first successful deploy
	Git                 GitSource // set when SourceType is SourceGit
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

const appCols = `id, project_id, name, source_type, image, port, cpu_limit, memory_limit_mb,
	desired_state, COALESCE(current_deployment_id, ''),
	git_repository, git_branch, builder, build_context, dockerfile_path, COALESCE(git_source_id, ''), environment, created_at, updated_at, mounts, start_command, COALESCE(registry_id, ''), deploy_hook_hash IS NOT NULL, push_deploy, processes, environment_meta, environment_order`

func (s *Store) scanApp(row scanner) (App, error) {
	var a App
	var environment, mounts, processes, envMeta, envOrder string
	var created, updated string
	err := row.Scan(&a.ID, &a.ProjectID, &a.Name, &a.SourceType, &a.Image, &a.Port, &a.CPULimit, &a.MemoryMB,
		&a.DesiredState, &a.CurrentDeploymentID,
		&a.Git.Repository, &a.Git.Branch, &a.Git.Builder, &a.Git.BuildContext, &a.Git.Dockerfile, &a.Git.ConnectionID, &environment, &created, &updated, &mounts, &a.StartCommand, &a.RegistryID, &a.DeployHook, &a.PushDeploy, &processes, &envMeta, &envOrder)
	if errors.Is(err, sql.ErrNoRows) {
		return App{}, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	if a.Environment, err = s.openEnvironment(environment); err != nil {
		return a, fmt.Errorf("app %s environment: %w", a.ID, err)
	}
	if err := json.Unmarshal([]byte(mounts), &a.Mounts); err != nil {
		return a, err
	}
	if a.Mounts == nil {
		a.Mounts = []AppMount{}
	}
	if err := json.Unmarshal([]byte(processes), &a.Processes); err != nil {
		return a, err
	}
	if a.Processes == nil {
		a.Processes = []AppProcess{}
	}
	if err := json.Unmarshal([]byte(envMeta), &a.EnvMeta); err != nil {
		return a, fmt.Errorf("app %s environment flags: %w", a.ID, err)
	}
	a.EnvMeta = nonNilMeta(a.EnvMeta)
	a.EnvOrder = s.openEnvOrder(envOrder, a.Environment)
	a.CreatedAt, a.UpdatedAt = parseTime(created), parseTime(updated)
	return a, err
}

// Deploy strategies. Rolling starts the new container next to the old one, so there is
// no gap. Recreate stops the old one first: two containers writing the same volume (a
// SQLite file, an upload directory) could corrupt it, so an app with a named volume or a
// bind mount always deploys this way, with a short gap.
const (
	StrategyRolling  = "rolling"
	StrategyRecreate = "recreate"
)

// Strategy is how the app deploys: recreate when it mounts a volume or a host directory,
// rolling otherwise. Inline files are written per deployment, so they do not count.
func (a App) Strategy() string {
	for _, m := range a.Mounts {
		if m.Type == "volume" || m.Type == "bind" {
			return StrategyRecreate
		}
	}
	return StrategyRolling
}

// Source types of an app.
const (
	SourceImage = "image" // a ready image pulled from a registry
	SourceGit   = "git"   // built from a Git repository on every deploy
)

// Builders of a Git app.
const (
	// BuilderAuto uses the Dockerfile when the repository has one at the configured path,
	// and Railpack otherwise. The default for new apps.
	BuilderAuto       = "auto"
	BuilderDockerfile = "dockerfile"
	BuilderRailpack   = "railpack"
)

// GitSource says where a Git app's code is and how it is built.
type GitSource struct {
	ConnectionID string // Git connection used to fetch a private repository; "" = public
	Repository   string // https URL of the repository
	Branch       string
	Builder      string // BuilderAuto, BuilderDockerfile or BuilderRailpack
	BuildContext string // folder inside the repository; "" = its root
	Dockerfile   string // path relative to the build context; with BuilderAuto, its presence picks the Dockerfile
}

// AppInput holds the fields a user can set on an app. Image is used by image apps and
// Git by Git apps; the other one is stored empty.
type AppInput struct {
	Mounts       []AppMount
	Processes    []AppProcess
	Environment  map[string]string
	EnvMeta      map[string]EnvVarMeta
	EnvOrder     []string
	Name         string
	SourceType   string
	Image        string
	RegistryID   string // image apps: private registry to pull from; "" = public
	Git          GitSource
	StartCommand string
	Port         int
	CPULimit     float64
	MemoryMB     int64
}

// normalized empties the fields the source type does not use.
func (in AppInput) normalized() AppInput {
	if in.SourceType == SourceGit {
		in.Image, in.RegistryID = "", ""
	} else {
		in.SourceType, in.Git = SourceImage, GitSource{}
	}
	return in
}

func (s *Store) CreateApp(ctx context.Context, projectID string, in AppInput) (App, error) {
	in = in.normalized()
	mounts, err := json.Marshal(in.Mounts)
	if err != nil {
		return App{}, err
	}
	processes, err := json.Marshal(append([]AppProcess{}, in.Processes...))
	if err != nil {
		return App{}, err
	}
	environment, err := s.sealEnvironment(in.Environment)
	if err != nil {
		return App{}, err
	}
	envMeta, err := json.Marshal(nonNilMeta(in.EnvMeta))
	if err != nil {
		return App{}, err
	}
	aid, ts := id.New(), now()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO apps (id, project_id, name, source_type, image, port, cpu_limit, memory_limit_mb, desired_state,
			git_repository, git_branch, builder, build_context, dockerfile_path, git_source_id, environment, created_at, updated_at, mounts, start_command, registry_id, processes, environment_meta, environment_order)
		SELECT ?, id, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ? FROM projects WHERE id = ?`,
		aid, in.Name, in.SourceType, in.Image, in.Port, in.CPULimit, in.MemoryMB, StateStopped,
		in.Git.Repository, in.Git.Branch, in.Git.Builder, in.Git.BuildContext, in.Git.Dockerfile, in.Git.ConnectionID, string(environment), ts, ts, string(mounts), in.StartCommand, in.RegistryID, string(processes), string(envMeta), sealEnvOrder(in.EnvOrder, in.Environment), projectID)
	if err != nil {
		return App{}, conflict(err)
	}
	return s.GetApp(ctx, aid) // ErrNotFound when the project does not exist
}

func (s *Store) GetApp(ctx context.Context, appID string) (App, error) {
	return s.scanApp(s.db.QueryRowContext(ctx, `SELECT `+appCols+` FROM apps WHERE id = ?`, appID))
}

func (s *Store) AppExists(ctx context.Context, appID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM apps WHERE id = ?`, appID).Scan(&n)
	return n > 0, err
}

func (s *Store) ListApps(ctx context.Context, projectID string) ([]App, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+appCols+` FROM apps WHERE project_id = ? ORDER BY name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []App{}
	for rows.Next() {
		a, err := s.scanApp(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) UpdateApp(ctx context.Context, appID string, in AppInput) (App, error) {
	in = in.normalized()
	mounts, err := json.Marshal(in.Mounts)
	if err != nil {
		return App{}, err
	}
	processes, err := json.Marshal(append([]AppProcess{}, in.Processes...))
	if err != nil {
		return App{}, err
	}
	environment, err := s.sealEnvironment(in.Environment)
	if err != nil {
		return App{}, err
	}
	envMeta, err := json.Marshal(nonNilMeta(in.EnvMeta))
	if err != nil {
		return App{}, err
	}
	err = affected(s.db.ExecContext(ctx, `
		UPDATE apps SET name = ?, source_type = ?, image = ?, port = ?, cpu_limit = ?, memory_limit_mb = ?,
			git_repository = ?, git_branch = ?, builder = ?, build_context = ?, dockerfile_path = ?,
			git_source_id = NULLIF(?, ''), environment = ?, updated_at = ?, mounts = ?, start_command = ?,
			registry_id = NULLIF(?, ''), processes = ?, environment_meta = ?, environment_order = ?
		WHERE id = ?`,
		in.Name, in.SourceType, in.Image, in.Port, in.CPULimit, in.MemoryMB,
		in.Git.Repository, in.Git.Branch, in.Git.Builder, in.Git.BuildContext, in.Git.Dockerfile, in.Git.ConnectionID, string(environment), now(), string(mounts), in.StartCommand, in.RegistryID, string(processes), string(envMeta), sealEnvOrder(in.EnvOrder, in.Environment), appID))
	if err != nil {
		return App{}, err
	}
	return s.GetApp(ctx, appID)
}

func (s *Store) SetAppDesiredState(ctx context.Context, appID, state string) error {
	return affected(s.db.ExecContext(ctx,
		`UPDATE apps SET desired_state = ?, updated_at = ? WHERE id = ?`, state, now(), appID))
}

// SetAppCurrentDeployment points the app at a deployment (empty clears it) and sets the
// desired state in the same statement, so the proxy never sees one without the other.
func (s *Store) SetAppCurrentDeployment(ctx context.Context, appID, deploymentID, state string) error {
	return affected(s.db.ExecContext(ctx,
		`UPDATE apps SET current_deployment_id = NULLIF(?, ''), desired_state = ?, updated_at = ? WHERE id = ?`,
		deploymentID, state, now(), appID))
}

// DeleteApp removes the app with its domains and deployments.
func (s *Store) DeleteApp(ctx context.Context, appID string) error {
	return affected(s.db.ExecContext(ctx, `DELETE FROM apps WHERE id = ?`, appID))
}

// TLS modes of a domain.
const (
	TLSAuto     = "auto"      // HTTPS with an automatic certificate; HTTP redirects to it
	TLSHTTPOnly = "http_only" // plain HTTP only, for a CDN that terminates TLS (Cloudflare Flexible)
)

// ValidTLSMode reports whether m is a TLS mode the proxy implements.
func ValidTLSMode(m string) bool { return m == TLSAuto || m == TLSHTTPOnly }

// Upstream is the address the proxy dials for a container on the shared network.
func Upstream(container string, port int) string {
	return net.JoinHostPort(container, strconv.Itoa(port))
}

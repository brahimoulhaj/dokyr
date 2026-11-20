package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"panel/internal/secret"
)

// UseSecrets sets the key that encrypts app environments at rest, then encrypts every
// environment still stored as plain JSON (written before encryption existed, or by a
// store without a key). It runs once at startup, before the store serves requests.
func (s *Store) UseSecrets(ctx context.Context, box *secret.Box) (int, error) {
	if box == nil {
		return 0, errors.New("store: no secret key")
	}
	s.box = box
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id, environment FROM apps WHERE environment NOT LIKE 'v1:%'`)
	if err != nil {
		return 0, err
	}
	plain := map[string]string{}
	for rows.Next() {
		var id, env string
		if err := rows.Scan(&id, &env); err != nil {
			rows.Close()
			return 0, err
		}
		plain[id] = env
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for id, env := range plain {
		vars, err := s.openEnvironment(env)
		if err != nil {
			return 0, fmt.Errorf("app %s environment: %w", id, err)
		}
		sealed, err := s.sealEnvironment(vars)
		if err != nil {
			return 0, err
		}
		// updated_at is left alone: the app did not change, only how it is stored.
		if _, err := tx.ExecContext(ctx, `UPDATE apps SET environment = ? WHERE id = ?`, sealed, id); err != nil {
			return 0, err
		}
	}
	return len(plain), tx.Commit()
}

// sealEnvironment is how an environment is stored: JSON encrypted with the store's key.
// Without a key (tests only) it stays plain JSON.
func (s *Store) sealEnvironment(env map[string]string) (string, error) {
	if env == nil {
		env = map[string]string{}
	}
	raw, err := json.Marshal(env)
	if err != nil {
		return "", err
	}
	if s.box == nil {
		return string(raw), nil
	}
	return s.box.Encrypt(raw)
}

// openEnvironment reads a stored environment, encrypted or (from before encryption) plain.
func (s *Store) openEnvironment(stored string) (map[string]string, error) {
	raw := []byte(stored)
	if secret.IsSealed(stored) {
		if s.box == nil {
			return nil, errors.New("encrypted, and the store has no secret key")
		}
		var err error
		if raw, err = s.box.Decrypt(stored); err != nil {
			return nil, err
		}
	}
	env := map[string]string{}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	if env == nil { // "null"
		env = map[string]string{}
	}
	return env, nil
}

// ProjectEnvironment returns a project's shared variables and whether its apps inherit
// them all.
func (s *Store) ProjectEnvironment(ctx context.Context, projectID string) (map[string]string, bool, error) {
	var stored string
	var inherit bool
	err := s.db.QueryRowContext(ctx, `SELECT environment, env_inherit FROM projects WHERE id = ?`, projectID).Scan(&stored, &inherit)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, ErrNotFound
	}
	if err != nil {
		return nil, false, err
	}
	env, err := s.openEnvironment(stored)
	if err != nil {
		return nil, false, fmt.Errorf("project %s environment: %w", projectID, err)
	}
	return env, inherit, nil
}

// SetProjectEnvironment replaces a project's shared variables, stored encrypted.
func (s *Store) SetProjectEnvironment(ctx context.Context, projectID string, env map[string]string, inherit bool) error {
	sealed, err := s.sealEnvironment(env)
	if err != nil {
		return err
	}
	return affected(s.db.ExecContext(ctx, `UPDATE projects SET environment = ?, env_inherit = ?, updated_at = ? WHERE id = ?`,
		sealed, inherit, now(), projectID))
}

// Where an app variable is available.
const (
	EnvScopeRuntime = "runtime" // in the container; the default
	EnvScopeBuild   = "build"   // in the image build only
	EnvScopeBoth    = "both"
)

// EnvVarMeta is what an app variable is beyond its value. The zero value means the
// default: runtime only, not secret.
type EnvVarMeta struct {
	Scope  string `json:"scope"`  // EnvScopeRuntime, EnvScopeBuild or EnvScopeBoth
	Secret bool   `json:"secret"` // never returned by the API; a build secret at build time
}

// AtBuild reports whether the variable is passed to the image build.
func (m EnvVarMeta) AtBuild() bool { return m.Scope == EnvScopeBuild || m.Scope == EnvScopeBoth }

// AtRuntime reports whether the variable is set in the container.
func (m EnvVarMeta) AtRuntime() bool { return m.Scope != EnvScopeBuild }

// MergeEnvironment applies a change to an app's variables: env and meta as a client sent
// them (nil = not sent) on top of the stored ones. A client never sees a secret's value,
// so it cannot send it back: a variable that is secret before and after, and has no value
// in env, keeps its stored value. One that stops being secret needs a new value, so
// unmarking never reveals the old one. A client that sends no meta (one from before meta
// existed) leaves the flags, and the secrets it cannot see, as they are.
//
// The result may hold meta for a name without a value (a secret marked without one);
// ValidateEnvMeta rejects that.
func MergeEnvironment(curEnv map[string]string, curMeta map[string]EnvVarMeta, env *map[string]string, meta *map[string]EnvVarMeta) (map[string]string, map[string]EnvVarMeta) {
	if env == nil && meta == nil {
		return curEnv, curMeta
	}
	newMeta := curMeta
	if meta != nil {
		newMeta = *meta
	}
	out := map[string]string{}
	if env != nil {
		for k, v := range *env {
			out[k] = v
		}
	} else {
		for k, v := range curEnv {
			if !curMeta[k].Secret {
				out[k] = v
			}
		}
	}
	for k, m := range newMeta {
		if _, sent := out[k]; sent || !m.Secret || !curMeta[k].Secret {
			continue
		}
		if v, ok := curEnv[k]; ok {
			out[k] = v
		}
	}
	outMeta := map[string]EnvVarMeta{}
	for k, m := range newMeta {
		if m.Scope == "" {
			m.Scope = EnvScopeRuntime
		}
		_, has := out[k]
		if !has && (meta == nil || m == (EnvVarMeta{Scope: EnvScopeRuntime})) {
			continue // a variable that is gone, or a default entry for nothing
		}
		if has && m == (EnvVarMeta{Scope: EnvScopeRuntime}) {
			continue // the default is not stored
		}
		outMeta[k] = m
	}
	return out, outMeta
}

// ValidateEnvMeta checks the flags of an app's variables against its values.
func ValidateEnvMeta(env map[string]string, meta map[string]EnvVarMeta) error {
	for name, m := range meta {
		if m.Scope != EnvScopeRuntime && m.Scope != EnvScopeBuild && m.Scope != EnvScopeBoth {
			return fmt.Errorf("environment variable %s: scope must be runtime, build or both", name)
		}
		if _, ok := env[name]; !ok {
			if m.Secret {
				return fmt.Errorf("environment variable %s is secret and has no stored value: send one", name)
			}
			return fmt.Errorf("environment variable %s has flags but no value", name)
		}
	}
	return nil
}

// VisibleEnvironment is the part of an app's variables the API returns: every value that
// is not secret.
func VisibleEnvironment(env map[string]string, meta map[string]EnvVarMeta) map[string]string {
	out := make(map[string]string, len(env))
	for k, v := range env {
		if !meta[k].Secret {
			out[k] = v
		}
	}
	return out
}

func nonNilMeta(m map[string]EnvVarMeta) map[string]EnvVarMeta {
	if m == nil {
		return map[string]EnvVarMeta{}
	}
	return m
}

// An environment's order is a list of lines: a variable name, a comment ("# ...") or ""
// (a blank line). It is how the owner wrote the variables, kept so the editor shows them
// the same way again; it changes nothing about what a container or a build gets.

const (
	maxEnvOrderLines = 2000
	maxEnvOrderLine  = 1024
)

func envOrderNote(line string) bool { return line == "" || strings.HasPrefix(line, "#") }

// ValidateEnvOrder checks the comments of an order a client sent. Names are not checked:
// NormalizeEnvOrder drops the ones that are not variables.
func ValidateEnvOrder(order []string) error {
	if len(order) > maxEnvOrderLines {
		return fmt.Errorf("environment order: at most %d lines", maxEnvOrderLines)
	}
	for _, line := range order {
		if len(line) > maxEnvOrderLine || strings.ContainsAny(line, "\x00\r\n") {
			return fmt.Errorf("environment order: a line is at most %d bytes, on one line", maxEnvOrderLine)
		}
	}
	return nil
}

// NormalizeEnvOrder fits an order to the variables that exist: names that are not in env
// (or come twice) are dropped, variables the order does not name are added at the end,
// sorted. Comments stay where they are. The result is never nil.
func NormalizeEnvOrder(order []string, env map[string]string) []string {
	out := make([]string, 0, len(order)+len(env))
	seen := make(map[string]bool, len(env))
	for _, line := range order {
		if !envOrderNote(line) {
			if _, ok := env[line]; !ok || seen[line] {
				continue
			}
			seen[line] = true
		}
		out = append(out, line)
	}
	var rest []string
	for name := range env {
		if !seen[name] {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

func (s *Store) openEnvOrder(stored string, env map[string]string) []string {
	var order []string
	_ = json.Unmarshal([]byte(stored), &order) // a damaged order is only a lost layout
	return NormalizeEnvOrder(order, env)
}

func sealEnvOrder(order []string, env map[string]string) string {
	raw, _ := json.Marshal(NormalizeEnvOrder(order, env))
	return string(raw)
}

// ProjectEnvOrder returns the order of a project's shared variables.
func (s *Store) ProjectEnvOrder(ctx context.Context, projectID string) ([]string, error) {
	var env, order string
	err := s.db.QueryRowContext(ctx, `SELECT environment, environment_order FROM projects WHERE id = ?`, projectID).Scan(&env, &order)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	vars, err := s.openEnvironment(env)
	if err != nil {
		return nil, fmt.Errorf("project %s environment: %w", projectID, err)
	}
	return s.openEnvOrder(order, vars), nil
}

// SetProjectEnvOrder stores the order of a project's shared variables, fitted to env.
func (s *Store) SetProjectEnvOrder(ctx context.Context, projectID string, order []string, env map[string]string) error {
	return affected(s.db.ExecContext(ctx, `UPDATE projects SET environment_order = ? WHERE id = ?`, sealEnvOrder(order, env), projectID))
}

// AddAppBuildVariable adds a build-time variable to an app unless a variable of that name
// exists, whatever its value: one the user emptied or changed stays as they left it. It
// returns the app as stored afterwards and whether the variable was added.
func (s *Store) AddAppBuildVariable(ctx context.Context, appID, name, value string) (App, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return App{}, false, err
	}
	defer tx.Rollback()
	a, err := s.scanApp(tx.QueryRowContext(ctx, `SELECT `+appCols+` FROM apps WHERE id = ?`, appID))
	if err != nil {
		return App{}, false, err
	}
	if _, has := a.Environment[name]; has {
		return a, false, nil
	}
	if a.Environment == nil {
		a.Environment = map[string]string{}
	}
	a.Environment[name] = value
	a.EnvMeta = nonNilMeta(a.EnvMeta)
	a.EnvMeta[name] = EnvVarMeta{Scope: EnvScopeBuild}
	a.EnvOrder = append(a.EnvOrder, name)
	environment, err := s.sealEnvironment(a.Environment)
	if err != nil {
		return App{}, false, err
	}
	meta, err := json.Marshal(a.EnvMeta)
	if err != nil {
		return App{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE apps SET environment = ?, environment_meta = ?, environment_order = ?, updated_at = ? WHERE id = ?`,
		environment, string(meta), sealEnvOrder(a.EnvOrder, a.Environment), now(), appID); err != nil {
		return App{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return App{}, false, err
	}
	a, err = s.GetApp(ctx, appID)
	return a, true, err
}

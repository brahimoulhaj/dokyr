package store

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"panel/internal/secret"
)

func TestEnvironmentSpecialValuesRoundTrip(t *testing.T) {
	s, ctx := open(t), context.Background()
	p, err := s.CreateProject(ctx, "environment-test", "")
	if err != nil {
		t.Fatal(err)
	}
	in := AppInput{Name: "web", Image: "nginx:latest", Port: 80, Environment: map[string]string{"EMPTY": "", "TOKEN": "a=b#c", "MULTILINE": "first\nsecond", "UNICODE": "مرحبا"}}
	a, err := s.CreateApp(ctx, p.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.Environment, in.Environment) {
		t.Fatalf("environment did not round-trip: %#v", a.Environment)
	}
	in.Environment = map[string]string{"REPLACEMENT": "new"}
	a, err = s.UpdateApp(ctx, a.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.Environment, in.Environment) {
		t.Fatal("environment replacement failed")
	}
	in.Environment = map[string]string{}
	a, err = s.UpdateApp(ctx, a.ID, in)
	if err != nil || len(a.Environment) != 0 {
		t.Fatalf("clear environment: %v, %v", a.Environment, err)
	}
}

func TestEnvironmentEncryptedAtRest(t *testing.T) {
	s, ctx := open(t), context.Background()
	p, err := s.CreateProject(ctx, "environment-secret", "")
	if err != nil {
		t.Fatal(err)
	}
	// Written before the key is set, as by a panel from before encryption.
	legacy, err := s.CreateApp(ctx, p.ID, AppInput{Name: "old", Image: "nginx:1", Port: 80, Environment: map[string]string{"DB_PASSWORD": "hunter2"}})
	if err != nil {
		t.Fatal(err)
	}
	stored := func(appID string) string {
		var env string
		if err := s.db.QueryRowContext(ctx, `SELECT environment FROM apps WHERE id = ?`, appID).Scan(&env); err != nil {
			t.Fatal(err)
		}
		return env
	}
	if !strings.Contains(stored(legacy.ID), "hunter2") {
		t.Fatalf("without a key the environment is plain JSON: %q", stored(legacy.ID))
	}

	box, err := secret.New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := s.GetApp(ctx, legacy.ID)
	n, err := s.UseSecrets(ctx, box)
	if err != nil || n != 1 {
		t.Fatalf("UseSecrets = %d, %v; want 1 app encrypted", n, err)
	}
	if env := stored(legacy.ID); !secret.IsSealed(env) || strings.Contains(env, "hunter2") {
		t.Fatalf("legacy environment not encrypted: %q", env)
	}
	after, err := s.GetApp(ctx, legacy.ID)
	if err != nil || after.Environment["DB_PASSWORD"] != "hunter2" || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("after encryption: %+v, %v", after, err)
	}
	if n, err := s.UseSecrets(ctx, box); err != nil || n != 0 {
		t.Fatalf("second UseSecrets = %d, %v; want nothing left to encrypt", n, err)
	}

	fresh, err := s.CreateApp(ctx, p.ID, AppInput{Name: "new", Image: "nginx:1", Port: 80, Environment: map[string]string{"API_KEY": "s3cr3t"}})
	if err != nil {
		t.Fatal(err)
	}
	if env := stored(fresh.ID); !secret.IsSealed(env) || strings.Contains(env, "s3cr3t") {
		t.Fatalf("new environment stored in clear: %q", env)
	}
	if fresh.Environment["API_KEY"] != "s3cr3t" {
		t.Fatalf("new environment = %v", fresh.Environment)
	}
	if _, err := s.UpdateApp(ctx, fresh.ID, AppInput{Name: "new", Image: "nginx:1", Port: 80, Environment: map[string]string{"API_KEY": "rotated"}}); err != nil {
		t.Fatal(err)
	}
	if env := stored(fresh.ID); strings.Contains(env, "rotated") {
		t.Fatalf("updated environment stored in clear: %q", env)
	}
	if apps, err := s.ListApps(ctx, p.ID); err != nil || len(apps) != 2 {
		t.Fatalf("ListApps = %+v, %v", apps, err)
	}

	// Another key cannot read it, and says which app is affected.
	other, _ := secret.New(bytes.Repeat([]byte{9}, 32))
	s.box = other
	if _, err := s.GetApp(ctx, fresh.ID); err == nil || !strings.Contains(err.Error(), fresh.ID) {
		t.Fatalf("read with the wrong key: %v", err)
	}
}

func TestEnvironmentMetaRoundTrip(t *testing.T) {
	s, ctx := open(t), context.Background()
	p, err := s.CreateProject(ctx, "environment-meta", "")
	if err != nil {
		t.Fatal(err)
	}
	// An app from before the flags: every variable is runtime and plain.
	a, err := s.CreateApp(ctx, p.ID, AppInput{Name: "web", Image: "nginx:1", Port: 80, Environment: map[string]string{"A": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.EnvMeta) != 0 || !a.EnvMeta["A"].AtRuntime() || a.EnvMeta["A"].AtBuild() || a.EnvMeta["A"].Secret {
		t.Fatalf("default flags: %#v", a.EnvMeta)
	}
	meta := map[string]EnvVarMeta{"TOKEN": {Scope: EnvScopeBuild, Secret: true}, "URL": {Scope: EnvScopeBoth}}
	a, err = s.UpdateApp(ctx, a.ID, AppInput{Name: "web", Image: "nginx:1", Port: 80, Environment: map[string]string{"A": "1", "TOKEN": "t0ken", "URL": "u"}, EnvMeta: meta})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.EnvMeta, meta) {
		t.Fatalf("flags did not round-trip: %#v", a.EnvMeta)
	}
	if got := VisibleEnvironment(a.Environment, a.EnvMeta); !reflect.DeepEqual(got, map[string]string{"A": "1", "URL": "u"}) {
		t.Fatalf("visible = %v", got)
	}
	// The flags hold names only: no value ends up outside the sealed environment.
	var stored string
	if err := s.db.QueryRowContext(ctx, `SELECT environment_meta FROM apps WHERE id = ?`, a.ID).Scan(&stored); err != nil || strings.Contains(stored, "t0ken") {
		t.Fatalf("stored flags = %q, %v", stored, err)
	}
}

func TestMergeEnvironment(t *testing.T) {
	curEnv := map[string]string{"PLAIN": "p", "TOKEN": "t0ken", "BUILD": "b"}
	curMeta := map[string]EnvVarMeta{"TOKEN": {Scope: EnvScopeBoth, Secret: true}, "BUILD": {Scope: EnvScopeBuild}}
	env := func(m map[string]string) *map[string]string { return &m }
	meta := func(m map[string]EnvVarMeta) *map[string]EnvVarMeta { return &m }
	for _, tc := range []struct {
		name     string
		env      *map[string]string
		meta     *map[string]EnvVarMeta
		wantEnv  map[string]string
		wantMeta map[string]EnvVarMeta
		invalid  bool
	}{
		{name: "nothing sent", wantEnv: curEnv, wantMeta: curMeta},
		{name: "a client without flags keeps the secrets it cannot see",
			env:     env(map[string]string{"PLAIN": "p2"}),
			wantEnv: map[string]string{"PLAIN": "p2", "TOKEN": "t0ken"}, wantMeta: map[string]EnvVarMeta{"TOKEN": {Scope: EnvScopeBoth, Secret: true}}},
		{name: "a secret sent without a value keeps its value",
			env:      env(map[string]string{"PLAIN": "p", "BUILD": "b"}),
			meta:     meta(map[string]EnvVarMeta{"TOKEN": {Scope: EnvScopeBuild, Secret: true}, "BUILD": {Scope: EnvScopeBuild}, "PLAIN": {}}),
			wantEnv:  curEnv,
			wantMeta: map[string]EnvVarMeta{"TOKEN": {Scope: EnvScopeBuild, Secret: true}, "BUILD": {Scope: EnvScopeBuild}}},
		{name: "a secret sent with a value is replaced",
			env:     env(map[string]string{"TOKEN": "new"}),
			meta:    meta(map[string]EnvVarMeta{"TOKEN": {Scope: EnvScopeRuntime, Secret: true}}),
			wantEnv: map[string]string{"TOKEN": "new"}, wantMeta: map[string]EnvVarMeta{"TOKEN": {Scope: EnvScopeRuntime, Secret: true}}},
		{name: "a secret left out of both is removed",
			env:     env(map[string]string{"PLAIN": "p"}),
			meta:    meta(map[string]EnvVarMeta{}),
			wantEnv: map[string]string{"PLAIN": "p"}, wantMeta: map[string]EnvVarMeta{}},
		{name: "unmarking a secret without a new value does not reveal it",
			env:     env(map[string]string{"PLAIN": "p"}),
			meta:    meta(map[string]EnvVarMeta{"TOKEN": {Scope: EnvScopeBoth}}),
			invalid: true},
		{name: "unmarking with flags alone does not reveal it either",
			meta:    meta(map[string]EnvVarMeta{"TOKEN": {Scope: EnvScopeBoth}}),
			invalid: true},
		{name: "a plain variable becomes secret and keeps its value",
			meta:    meta(map[string]EnvVarMeta{"PLAIN": {Secret: true}, "TOKEN": {Scope: EnvScopeBoth, Secret: true}}),
			wantEnv: curEnv, wantMeta: map[string]EnvVarMeta{"PLAIN": {Scope: EnvScopeRuntime, Secret: true}, "TOKEN": {Scope: EnvScopeBoth, Secret: true}}},
		{name: "a new secret needs a value",
			env:     env(map[string]string{}),
			meta:    meta(map[string]EnvVarMeta{"NEW": {Scope: EnvScopeBuild, Secret: true}}),
			invalid: true},
		{name: "an unknown scope is refused",
			env:     env(map[string]string{"X": "1"}),
			meta:    meta(map[string]EnvVarMeta{"X": {Scope: "deploy"}}),
			invalid: true},
	} {
		gotEnv, gotMeta := MergeEnvironment(curEnv, curMeta, tc.env, tc.meta)
		err := ValidateEnvMeta(gotEnv, gotMeta)
		if tc.invalid {
			if err == nil {
				t.Errorf("%s: accepted %v %v", tc.name, gotEnv, gotMeta)
			}
			if v, ok := gotEnv["TOKEN"]; ok && !gotMeta["TOKEN"].Secret {
				t.Errorf("%s: the secret became visible: %q", tc.name, v)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(gotEnv, tc.wantEnv) || !reflect.DeepEqual(gotMeta, tc.wantMeta) {
			t.Errorf("%s: got %v %v (%v)", tc.name, gotEnv, gotMeta, err)
		}
	}
}

func TestEnvOrder(t *testing.T) {
	s, ctx := open(t), context.Background()
	p, err := s.CreateProject(ctx, "order-test", "")
	if err != nil {
		t.Fatal(err)
	}
	in := AppInput{Name: "web", Image: "nginx:1", Port: 80,
		Environment: map[string]string{"ZED": "1", "ALPHA": "2", "MID": "3"},
		EnvOrder:    []string{"# first", "ZED", "", "GONE", "ALPHA", "ZED"},
	}
	a, err := s.CreateApp(ctx, p.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	// GONE is no variable, the second ZED is a repeat, MID was not named: it comes last.
	want := []string{"# first", "ZED", "", "ALPHA", "MID"}
	if !reflect.DeepEqual(a.EnvOrder, want) {
		t.Fatalf("order = %q, want %q", a.EnvOrder, want)
	}
	// Removing a variable removes it from the order; an app saved without one is sorted.
	in.Environment = map[string]string{"ZED": "1", "MID": "3"}
	in.EnvOrder = a.EnvOrder
	if a, err = s.UpdateApp(ctx, a.ID, in); err != nil {
		t.Fatal(err)
	}
	if want := []string{"# first", "ZED", "", "MID"}; !reflect.DeepEqual(a.EnvOrder, want) {
		t.Fatalf("order = %q, want %q", a.EnvOrder, want)
	}
	if got := NormalizeEnvOrder(nil, map[string]string{"B": "", "A": ""}); !reflect.DeepEqual(got, []string{"A", "B"}) {
		t.Fatalf("unordered = %q", got)
	}
	if err := ValidateEnvOrder([]string{"# two\nlines"}); err == nil {
		t.Fatal("a comment over two lines was accepted")
	}

	env := map[string]string{"B": "1", "A": "2"}
	if err := s.SetProjectEnvironment(ctx, p.ID, env, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectEnvOrder(ctx, p.ID, []string{"B", "# then", "A"}, env); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ProjectEnvOrder(ctx, p.ID); err != nil || !reflect.DeepEqual(got, []string{"B", "# then", "A"}) {
		t.Fatalf("project order = %q, %v", got, err)
	}
}

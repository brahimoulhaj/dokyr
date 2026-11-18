package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"panel/internal/store"
	"strings"
	"testing"
)

func TestEnvironmentValidation(t *testing.T) {
	for _, tc := range []struct {
		key, value string
		valid      bool
	}{
		{"APP_ENV", "production", true}, {"EMPTY", "", true}, {"1INVALID", "v", false}, {"BAD=KEY", "v", false}, {"BAD", "a\x00b", false},
	} {
		in := store.AppInput{Name: "web", SourceType: "image", Image: "nginx:latest", Port: 80, Environment: map[string]string{tc.key: tc.value}}
		if validApp(httptest.NewRecorder(), in) != tc.valid {
			t.Errorf("unexpected validation for %q", tc.key)
		}
	}
	original := store.AppInput{Environment: map[string]string{"KEEP": "value"}}
	if (appFields{}).apply(original).Environment["KEEP"] != "value" {
		t.Fatal("omitted environment must be preserved")
	}
	empty := map[string]string{}
	if len((appFields{Environment: &empty}).apply(original).Environment) != 0 {
		t.Fatal("empty environment must clear variables")
	}
}

// The API never returns a secret's value: not on create, update, get or the project's list.
func TestEnvironmentSecretsNotReturned(t *testing.T) {
	e := newEnv(t)
	e.signIn()
	pid := e.must(201, "POST", "/api/projects", map[string]any{"name": "Shop"})["id"].(string)
	check := func(what string, got any, wantEnv, wantMeta string) {
		t.Helper()
		raw, _ := json.Marshal(got)
		if strings.Contains(string(raw), "t0p-s3cret") || strings.Contains(string(raw), "r0tated") {
			t.Errorf("%s returns the secret: %s", what, raw)
		}
		if wantEnv == "" {
			return
		}
		m := got.(map[string]any)
		env, _ := json.Marshal(m["environment"])
		meta, _ := json.Marshal(m["environment_meta"])
		if string(env) != wantEnv || string(meta) != wantMeta {
			t.Errorf("%s: environment %s, meta %s", what, env, meta)
		}
	}
	secret := func(scope string) map[string]any { return map[string]any{"scope": scope, "secret": true} }
	a := e.must(201, "POST", "/api/projects/"+pid+"/apps", map[string]any{"name": "web", "image": "nginx:1", "port": 80,
		"environment":      map[string]string{"APP_ENV": "production", "TOKEN": "t0p-s3cret", "NODE_VERSION": "22"},
		"environment_meta": map[string]any{"TOKEN": secret("both"), "NODE_VERSION": map[string]any{"scope": "build"}}})
	id := a["id"].(string)
	env := `{"APP_ENV":"production","NODE_VERSION":"22"}`
	meta := `{"NODE_VERSION":{"scope":"build","secret":false},"TOKEN":{"scope":"both","secret":true}}`
	check("create", a, env, meta)
	check("get", e.must(200, "GET", "/api/apps/"+id, nil), env, meta)
	check("list", e.list("/api/projects/"+pid+"/apps"), "", "")

	// What the UI sends back: the secret by name only. Its value stays.
	built := `{"TOKEN":{"scope":"build","secret":true}}`
	check("update", e.must(200, "PATCH", "/api/apps/"+id, map[string]any{
		"environment": map[string]string{"APP_ENV": "staging"}, "environment_meta": map[string]any{"TOKEN": secret("build")}}),
		`{"APP_ENV":"staging"}`, built)
	// Unmarking it without a new value is refused, so the old value is never shown.
	e.must(422, "PATCH", "/api/apps/"+id, map[string]any{
		"environment": map[string]string{"APP_ENV": "staging"}, "environment_meta": map[string]any{"TOKEN": map[string]any{"scope": "build"}}})
	// A client from before the flags sends values alone: the secret it cannot see stays.
	check("old client", e.must(200, "PATCH", "/api/apps/"+id, map[string]any{"environment": map[string]string{"APP_ENV": "dev"}}),
		`{"APP_ENV":"dev"}`, built)
	check("rotate", e.must(200, "PATCH", "/api/apps/"+id, map[string]any{
		"environment": map[string]string{"APP_ENV": "dev", "TOKEN": "r0tated"}, "environment_meta": map[string]any{"TOKEN": secret("build")}}),
		`{"APP_ENV":"dev"}`, built)
	if app, err := e.srv.store.GetApp(context.Background(), id); err != nil || app.Environment["TOKEN"] != "r0tated" {
		t.Errorf("stored secret = %q, %v", app.Environment["TOKEN"], err)
	}
}

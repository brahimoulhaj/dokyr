package templates

import (
	"regexp"
	"strings"
	"testing"

	"panel/internal/database"
	"panel/internal/store"
)

// Every template of the catalog must make an app and a database the panel accepts.
var (
	categories = map[string]bool{"cms": true, "dev": true, "automation": true, "monitoring": true, "analytics": true, "data": true,
		"files": true, "collaboration": true, "security": true, "media": true, "ai": true}
	appName = regexp.MustCompile(`^[a-z]([a-z0-9-]{0,38}[a-z0-9])?$`)
	image   = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/:@-]{0,254}$`)
)

func in(t *testing.T, tpl Template) store.AppInput {
	t.Helper()
	out, err := tpl.AppInput("site")
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCatalog(t *testing.T) {
	seen := map[string]bool{}
	for _, tpl := range All() {
		if seen[tpl.ID] || tpl.Name == "" || tpl.Description == "" || tpl.Image == "" || tpl.Port < 1 {
			t.Fatalf("template %q is incomplete or repeated", tpl.ID)
		}
		seen[tpl.ID] = true
		if !categories[tpl.Category] || !strings.HasPrefix(tpl.Website, "https://") || len(tpl.Description) > 160 {
			t.Fatalf("template %q: unknown category %q, bad website or a description over 160 characters", tpl.ID, tpl.Category)
		}
		if !appName.MatchString(tpl.ID) || !image.MatchString(tpl.Image) || len(in(t, tpl).StartCommand) > 4096 {
			t.Fatalf("template %q: its id is the default app name and its image must be a plain reference", tpl.ID)
		}
		if got, ok := Get(tpl.ID); !ok || got.ID != tpl.ID {
			t.Fatalf("Get(%q) failed", tpl.ID)
		}
		in, err := tpl.AppInput("site")
		if err != nil {
			t.Fatal(err)
		}
		if err := store.ValidateAppMounts(in.Mounts); err != nil {
			t.Fatalf("%s mounts: %v", tpl.ID, err)
		}
		if err := store.ValidateEnvMeta(in.Environment, in.EnvMeta); err != nil {
			t.Fatalf("%s environment: %v", tpl.ID, err)
		}
		if len(in.Mounts) != len(tpl.Volumes) {
			t.Fatalf("%s: %d mounts for %d volumes", tpl.ID, len(in.Mounts), len(tpl.Volumes))
		}
		for key, value := range in.Environment {
			if strings.Contains(value, "${secret}") || strings.Contains(value, "${password}") || strings.Contains(value, "${key}") || strings.Contains(value, "${db.HOST") {
				t.Fatalf("%s: %s kept a placeholder: %s", tpl.ID, key, value)
			}
			if strings.Contains(value, "${db.") && tpl.Database == nil {
				t.Fatalf("%s: %s refers to a database the template does not create", tpl.ID, key)
			}
		}
		// A database variable starts empty, and Connect fills every one of them.
		vars := tpl.DatabaseVariables()
		if (tpl.Database == nil) != (len(vars) == 0) && tpl.ID != "drupal" {
			t.Fatalf("%s: database %v but database variables %v", tpl.ID, tpl.Database, vars)
		}
		refs, _ := tpl.Connect("main", nil)
		for _, key := range vars {
			if in.Environment[key] != "" || !strings.Contains(refs[key], "${db.main.") || strings.Contains(refs[key], "${db.HOST") {
				t.Fatalf("%s: %s = %q at creation, %q connected", tpl.ID, key, in.Environment[key], refs[key])
			}
		}
		if tpl.Database != nil {
			d := store.DBInstance{Name: "main", Engine: tpl.Database.Engine, Version: tpl.Database.Version,
				DatabaseName: tpl.Database.Name, Username: tpl.Database.Name}
			if err := database.Validate(d); err != nil {
				t.Fatalf("%s database: %v", tpl.ID, err)
			}
		}
	}
	if _, ok := Get("missing"); ok {
		t.Fatal("Get found a template that does not exist")
	}
}

func TestAppInput(t *testing.T) {
	wp, _ := Get("wordpress")
	a, _ := wp.AppInput("blog")
	b, _ := wp.AppInput("blog")
	if v, ok := a.Environment["WORDPRESS_DB_HOST"]; !ok || v != "" || a.Template != "wordpress" {
		t.Fatalf("a database variable must exist, empty: %q %v; template %q", v, ok, a.Template)
	}
	if a.Mounts[0].Source == b.Mounts[0].Source || !strings.HasPrefix(a.Mounts[0].Source, "blog-") {
		t.Fatalf("volume names must be unique per app: %q, %q", a.Mounts[0].Source, b.Mounts[0].Source)
	}
	// A password the owner signs in with stays readable; a value only the app reads is a secret.
	kc, _ := Get("keycloak")
	c, _ := kc.AppInput("kc")
	d, _ := kc.AppInput("kc")
	pw := c.Environment["KC_BOOTSTRAP_ADMIN_PASSWORD"]
	if len(pw) < 24 || pw == d.Environment["KC_BOOTSTRAP_ADMIN_PASSWORD"] || c.EnvMeta["KC_BOOTSTRAP_ADMIN_PASSWORD"].Secret {
		t.Fatalf("a login password must be long, different for each app, and readable: %q", pw)
	}
	um, _ := Get("directus")
	u, _ := um.AppInput("cms")
	if !u.EnvMeta["SECRET"].Secret || u.EnvMeta["ADMIN_PASSWORD"].Secret || len(u.Environment["SECRET"]) < 32 {
		t.Fatalf("directus: SECRET is a secret, ADMIN_PASSWORD is readable: %v", u.EnvMeta)
	}
}

func TestConnect(t *testing.T) {
	wp, _ := Get("wordpress")
	env, secret := wp.Connect("main", nil)
	if env["WORDPRESS_DB_HOST"] != "${db.main.HOST}:${db.main.PORT}" || env["WORDPRESS_DB_PASSWORD"] != "${db.main.PASSWORD}" || len(secret) != 0 || len(env) != 4 {
		t.Fatalf("references = %v, secret %v", env, secret)
	}
	fields := map[string]string{"HOST": "db-1", "PORT": "3306", "DATABASE": "shop", "USERNAME": "shop", "PASSWORD": "s3cret"}
	env, secret = wp.Connect("main", fields)
	if env["WORDPRESS_DB_HOST"] != "db-1:3306" || env["WORDPRESS_DB_NAME"] != "shop" || env["WORDPRESS_DB_PASSWORD"] != "s3cret" {
		t.Fatalf("written values = %v", env)
	}
	if !secret["WORDPRESS_DB_PASSWORD"] || secret["WORDPRESS_DB_HOST"] {
		t.Fatalf("only the variable with the password is secret: %v", secret)
	}
	um, _ := Get("umami")
	if env, secret = um.Connect("main", fields); env["DATABASE_URL"] != "postgres://shop:s3cret@db-1:3306/shop" || !secret["DATABASE_URL"] {
		t.Fatalf("a URL with the password is secret: %v %v", env, secret)
	}
}

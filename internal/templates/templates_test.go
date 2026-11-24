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
		if len(in.Mounts) != len(tpl.Volumes)+len(tpl.Files) {
			t.Fatalf("%s: %d mounts for %d volumes and %d files", tpl.ID, len(in.Mounts), len(tpl.Volumes), len(tpl.Files))
		}
		// Each service makes an app the panel accepts too, named after the template's own, and
		// no reference to the stack is left for the deployment to stumble on.
		stack, err := tpl.Stack(tpl.ID)
		if err != nil || len(stack) != 1+len(tpl.Services) || stack[0].Name != tpl.ID {
			t.Fatalf("%s: stack of %d apps for %d services: %v", tpl.ID, len(stack), len(tpl.Services), err)
		}
		names := map[string]bool{}
		for _, app := range stack {
			names[app.Name] = true
		}
		for i, app := range stack {
			if i > 0 {
				svc := tpl.Services[i-1]
				if app.Name != tpl.ID+"-"+svc.Name || !appName.MatchString(app.Name) || !image.MatchString(app.Image) || app.Port < 1 || app.Template != "" {
					t.Fatalf("%s: service %q makes an app the panel refuses: %+v", tpl.ID, svc.Name, app.AppInput)
				}
			}
			if err := store.ValidateAppMounts(app.Mounts); err != nil {
				t.Fatalf("%s mounts: %v", app.Name, err)
			}
			if err := store.ValidateEnvMeta(app.Environment, app.EnvMeta); err != nil {
				t.Fatalf("%s environment: %v", app.Name, err)
			}
			texts := []string{}
			for _, v := range app.Environment {
				texts = append(texts, v)
			}
			for _, m := range app.Mounts {
				texts = append(texts, m.Content)
			}
			for _, text := range texts {
				if strings.Contains(text, "${stack.") {
					t.Fatalf("%s kept a reference to its stack: %s", app.Name, text)
				}
				for _, ref := range fileHost.FindAllStringSubmatch(text, -1) {
					if !names[ref[1]] {
						t.Fatalf("%s refers to %s, which the template does not create", app.Name, ref[0])
					}
				}
			}
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

// A template of several apps: one password for all of them, and hostnames a file can hold.
func TestStack(t *testing.T) {
	sz, _ := Get("signoz")
	stack, err := sz.Stack("obs")
	if err != nil || len(stack) != 4 {
		t.Fatalf("stack = %d apps, %v", len(stack), err)
	}
	byName := map[string]StackApp{}
	for _, app := range stack {
		byName[app.Name] = app
	}
	main, collector, ch := byName["obs"], byName["obs-collector"], byName["obs-clickhouse"]
	dsn := main.Environment["SIGNOZ_TELEMETRYSTORE_CLICKHOUSE_DSN"]
	if !strings.HasSuffix(dsn, "@${app.obs-clickhouse.HOST}:9000") || dsn != collector.Environment["SIGNOZ_OTEL_COLLECTOR_CLICKHOUSE_DSN"] || !main.EnvMeta["SIGNOZ_TELEMETRYSTORE_CLICKHOUSE_DSN"].Secret {
		t.Fatalf("the apps share one address of ClickHouse, a secret: %q", dsn)
	}
	password := strings.TrimSuffix(strings.TrimPrefix(dsn, "tcp://default:"), "@${app.obs-clickhouse.HOST}:9000")
	other, _ := sz.Stack("obs")
	if len(password) < 32 || !strings.Contains(ch.Mounts[1].Content, "password: "+password+"\n") || strings.Contains(other[0].Environment["SIGNOZ_TELEMETRYSTORE_CLICKHOUSE_DSN"], password) {
		t.Fatalf("ClickHouse must ask for the password the other apps send, new for each stack: %q", password)
	}
	if !collector.NoProbe || main.NoProbe || main.StartPeriod != 120 || main.Template != "signoz" || ch.Mounts[0].Type != "volume" || !ch.Mounts[1].ReadOnly {
		t.Fatalf("stack apps = %+v", stack)
	}
	// Files get the hostnames once the apps exist; variables keep their references.
	hosts := map[string]string{"obs": "app-1", "obs-keeper": "app-2", "obs-clickhouse": "app-3"}
	mounts, changed := Hosts(ch.Mounts, hosts)
	if !changed || !strings.Contains(mounts[1].Content, "host: app-2\n") || !strings.Contains(mounts[1].Content, "host: app-3\n") || strings.Contains(mounts[1].Content, "${") {
		t.Fatalf("ClickHouse's file must name its keeper and itself: %s", mounts[1].Content)
	}
	if strings.Contains(ch.Mounts[1].Content, "app-2") {
		t.Fatal("Hosts must not change the mounts it was given")
	}
	if opamp, _ := Hosts(collector.Mounts, hosts); opamp[1].Content != "server_endpoint: ws://app-1:4320/v1/opamp\n" {
		t.Fatalf("opamp = %q", opamp[1].Content)
	}
	if _, changed := Hosts(main.Mounts, hosts); changed {
		t.Fatal("an app without files has nothing to change")
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

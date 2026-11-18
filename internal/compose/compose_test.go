package compose

import (
	"strings"
	"testing"

	"panel/internal/store"
)

const wordpress = `
services:
  db:
    image: mariadb:11.4
    restart: always
    command: --character-set-server=utf8mb4
    environment:
      MARIADB_DATABASE: wp
      MARIADB_PASSWORD: ${DB_PASSWORD:-secret}
      MARIADB_ROOT_PASSWORD: ${ROOT_PASSWORD}
    volumes:
      - db_data:/var/lib/mysql
    healthcheck:
      test: ["CMD", "healthcheck.sh"]
  web_site:
    image: wordpress:6
    depends_on: [db]
    ports:
      - "127.0.0.1:8080:80/tcp"
      - "8443:443"
    environment:
      - WORDPRESS_DB_HOST=db:3306
      - WORDPRESS_DB_URL=mysql://wp:secret@db/wp
      - WORDPRESS_DEBUG
      - PRICE=$$5
    volumes:
      - wp:/var/www/html
      - ./php.ini:/usr/local/etc/php/conf.d/custom.ini:ro
      - /srv/uploads:/uploads:ro
      - shared:/shared
    deploy:
      resources:
        limits: { cpus: "0.5", memory: 512M }
  worker:
    image: redis:7
    command: ["redis-server", "--save", "60 1"]
    expose: ["6379"]
    mem_limit: 1g
    volumes:
      - type: volume
        source: shared
        target: /data/
        read_only: true
      - /cache
volumes:
  db_data:
  wp:
  shared:
    external: true
    name: company-shared
`

func TestConvert(t *testing.T) {
	r, err := Convert([]byte(wordpress))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Apps) != 3 || r.Apps[0].Name != "db" || r.Apps[1].Name != "web-site" || r.Apps[2].Name != "worker" {
		t.Fatalf("apps = %+v", r.Apps)
	}
	db, web, worker := r.Apps[0], r.Apps[1], r.Apps[2]
	var lines []string
	for _, n := range r.Notes {
		lines = append(lines, n.App+" "+n.Kind+": "+n.Text)
	}
	notes := strings.Join(lines, "\n")

	if db.Image != "mariadb:11.4" || db.Port != 80 || db.StartCommand != "" {
		t.Fatalf("db = %+v", db)
	}
	if db.Environment["MARIADB_PASSWORD"] != "secret" || db.Environment["MARIADB_ROOT_PASSWORD"] != "" {
		t.Fatalf("db environment = %v", db.Environment)
	}
	if len(db.Mounts) != 1 || db.Mounts[0].Type != "volume" || !strings.HasPrefix(db.Mounts[0].Source, "db_data-") || db.Mounts[0].Target != "/var/lib/mysql" {
		t.Fatalf("db mounts = %+v", db.Mounts)
	}

	if web.Port != 80 || web.CPULimit != 0.5 || web.MemoryMB != 512 {
		t.Fatalf("web = %+v", web)
	}
	if web.Environment["WORDPRESS_DB_HOST"] != "${app.db.HOST}:3306" || web.Environment["WORDPRESS_DB_URL"] != "mysql://wp:secret@${app.db.HOST}/wp" || web.Environment["PRICE"] != "$5" {
		t.Fatalf("web environment = %v", web.Environment)
	}
	if _, set := web.Environment["WORDPRESS_DEBUG"]; set {
		t.Fatal("a variable without a value must be left out")
	}
	want := []store.AppMount{
		{Type: "volume", Source: web.Mounts[0].Source, Target: "/var/www/html"},
		{Type: "bind", Source: "/srv/uploads", Target: "/uploads", ReadOnly: true},
		{Type: "volume", Source: "company-shared", Target: "/shared"},
	}
	if len(web.Mounts) != 3 || web.Mounts[0] != want[0] || web.Mounts[1] != want[1] || web.Mounts[2] != want[2] {
		t.Fatalf("web mounts = %+v", web.Mounts)
	}

	if worker.Port != 6379 || worker.MemoryMB != 1024 || worker.StartCommand != "redis-server --save '60 1'" {
		t.Fatalf("worker = %+v", worker)
	}
	if len(worker.Mounts) != 2 || worker.Mounts[0] != (store.AppMount{Type: "volume", Source: "company-shared", Target: "/data", ReadOnly: true}) || worker.Mounts[1].Target != "/cache" {
		t.Fatalf("worker mounts = %+v", worker.Mounts)
	}
	for _, a := range r.Apps {
		if err := store.ValidateAppMounts(a.Mounts); err != nil {
			t.Fatalf("%s mounts: %v", a.Name, err)
		}
	}
	for _, want := range []string{
		"db skipped: command (--character-set-server=utf8mb4)", "db skipped: healthcheck", "${ROOT_PASSWORD}",
		"web-site linked: WORDPRESS_DB_HOST now points at the app db", "web-site skipped: WORDPRESS_DEBUG has no value",
		"./php.ini", "from /srv/uploads on the server", "worker check: command is now the start command",
	} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes lack %q:\n%s", want, notes)
		}
	}
	if !r.NoPort["db"] || r.NoPort["web-site"] || r.NoPort["worker"] || strings.Contains(notes, "port") {
		t.Errorf("only db has no port, and ports need no note: %v\n%s", r.NoPort, notes)
	}
	if strings.Contains(notes, "restart") || strings.Contains(notes, "depends_on") {
		t.Errorf("keys that change nothing must stay silent:\n%s", notes)
	}
}

func TestConvertErrors(t *testing.T) {
	for file, want := range map[string]string{
		"services: [":                                                     "not valid YAML",
		"version: '3'":                                                    "no services",
		"services:\n  web:\n    build: .":                                 "built from source",
		"services:\n  web:\n    restart: always":                          "has no image",
		"services:\n  Bad Name:\n    image: nginx":                        "name must be",
		"services:\n  a_b:\n    image: x\n  a-b:\n    image: x":           "same app name",
		"services:\n  web:\n    image: x\n    environment:\n      A: [1]": "single value",
	} {
		if _, err := Convert([]byte(file)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Convert(%q) = %v, want %q", file, err, want)
		}
	}
	// A build next to an image: the image wins.
	r, err := Convert([]byte("services:\n  web:\n    build: .\n    image: ghcr.io/org/web:1\n    ports: [3000]"))
	if err != nil || r.Apps[0].Image != "ghcr.io/org/web:1" || r.Apps[0].Port != 3000 {
		t.Fatalf("build with image = %+v, %v", r, err)
	}
}

func TestMegabytes(t *testing.T) {
	for in, want := range map[string]int64{"512m": 512, "512M": 512, "1g": 1024, "2GB": 2048, "268435456": 256, "1k": 6} {
		if got, ok := megabytes(in); !ok || got != want {
			t.Errorf("megabytes(%q) = %d, %v", in, got, ok)
		}
	}
	if _, ok := megabytes("lots"); ok {
		t.Error("megabytes accepted a word")
	}
}

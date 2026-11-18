// Package templates is the catalog of ready-made apps: an image with the database,
// volumes and variables it needs, created in a project in one step.
package templates

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"regexp"
	"slices"
	"strings"

	"panel/internal/store"
)

// Database is the database a template's app needs: its owner connects one after creating the app.
type Database struct {
	Engine  string `json:"engine"`
	Version string `json:"version"`
	// Name is a fitting name for a database made for the app.
	Name string `json:"name"`
}

// Volume is a named volume the app keeps its data in.
type Volume struct {
	Name   string `json:"name"` // suffix of the Docker volume's name
	Target string `json:"target"`
}

// Template describes one app of the catalog. In Env, ${db.FIELD} refers to the template's
// own database (HOST, PORT, DATABASE, USERNAME, PASSWORD, URL). ${secret} is replaced by a
// value generated for the app and ${key} by 32 random bytes in base64 (a Laravel key): both
// are stored as secret variables, which the panel never shows again, so they are for values
// only the app reads. ${password} is a generated password its owner signs in with: it stays
// readable in the app's variables.
type Template struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Category    string            `json:"category"`
	Website     string            `json:"website"`
	Image       string            `json:"image"`
	Port        int               `json:"port"`
	Database    *Database         `json:"database,omitempty"`
	Volumes     []Volume          `json:"volumes"`
	Env         map[string]string `json:"-"`
	// StartCommand replaces the image's entrypoint and command, run by /bin/sh -c; "" = the image's own.
	StartCommand string `json:"-"`
	// StartPeriod is how long the app may take to accept connections, in seconds; 0 = the default.
	StartPeriod int `json:"-"`
}

// Shorthands for the catalog.
func pg(name string) *Database    { return &Database{Engine: "postgres", Version: "17", Name: name} }
func maria(name string) *Database { return &Database{Engine: "mariadb", Version: "11.4", Name: name} }
func vol(pairs ...string) []Volume {
	out := make([]Volume, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, Volume{Name: pairs[i], Target: pairs[i+1]})
	}
	return out
}

type env = map[string]string

// dbURL is a connection URL without the driver options ${db.URL} carries.
const dbURL = "postgres://${db.USERNAME}:${db.PASSWORD}@${db.HOST}:${db.PORT}/${db.DATABASE}"

var catalog = []Template{
	// ---- websites and content ----
	{
		ID: "wordpress", Name: "WordPress", Category: "cms", Website: "https://wordpress.org",
		Description: "Website and blog platform, with its MariaDB database.",
		Image:       "wordpress:6-apache", Port: 80, Database: maria("wordpress"),
		Volumes: vol("data", "/var/www/html"),
		Env: env{
			"WORDPRESS_DB_HOST": "${db.HOST}:${db.PORT}", "WORDPRESS_DB_NAME": "${db.DATABASE}",
			"WORDPRESS_DB_USER": "${db.USERNAME}", "WORDPRESS_DB_PASSWORD": "${db.PASSWORD}",
		},
	},
	{
		ID: "joomla", Name: "Joomla", Category: "cms", Website: "https://www.joomla.org",
		Description: "Content management system, with its MariaDB database.",
		Image:       "joomla:5-apache", Port: 80, Database: maria("joomla"),
		Volumes: vol("data", "/var/www/html"),
		Env: env{
			"JOOMLA_DB_HOST": "${db.HOST}:${db.PORT}", "JOOMLA_DB_NAME": "${db.DATABASE}",
			"JOOMLA_DB_USER": "${db.USERNAME}", "JOOMLA_DB_PASSWORD": "${db.PASSWORD}",
		},
	},
	{
		ID: "drupal", Name: "Drupal", Category: "cms", Website: "https://www.drupal.org",
		Description: "Content management framework. Its installer asks for the database: copy the connection from the database's page.",
		Image:       "drupal:11-apache", Port: 80, Database: pg("drupal"),
		Volumes: vol("sites", "/var/www/html/sites", "modules", "/var/www/html/modules", "themes", "/var/www/html/themes", "profiles", "/var/www/html/profiles"),
	},
	{
		ID: "directus", Name: "Directus", Category: "cms", Website: "https://directus.io",
		Description: "Headless CMS and data API over PostgreSQL. Admin admin@example.com; the password is in the app's variables.",
		Image:       "directus/directus:11", Port: 8055, Database: pg("directus"),
		Volumes: vol("uploads", "/directus/uploads", "extensions", "/directus/extensions"),
		Env: env{
			"SECRET": "${secret}", "DB_CLIENT": "pg", "DB_HOST": "${db.HOST}", "DB_PORT": "${db.PORT}",
			"DB_DATABASE": "${db.DATABASE}", "DB_USER": "${db.USERNAME}", "DB_PASSWORD": "${db.PASSWORD}",
			"ADMIN_EMAIL": "admin@example.com", "ADMIN_PASSWORD": "${password}",
		},
		StartPeriod: 120,
	},
	{
		ID: "bookstack", Name: "BookStack", Category: "cms", Website: "https://www.bookstackapp.com",
		Description: "Wiki organised as books and pages. Set APP_URL to the app's address before deploying.",
		Image:       "lscr.io/linuxserver/bookstack:latest", Port: 80, Database: maria("bookstack"),
		Volumes: vol("config", "/config"),
		Env: env{
			"APP_URL": "http://localhost", "APP_KEY": "base64:${key}",
			"DB_HOST": "${db.HOST}", "DB_PORT": "${db.PORT}", "DB_DATABASE": "${db.DATABASE}",
			"DB_USERNAME": "${db.USERNAME}", "DB_PASSWORD": "${db.PASSWORD}",
		},
		StartPeriod: 120,
	},
	{
		ID: "wikijs", Name: "Wiki.js", Category: "cms", Website: "https://js.wiki",
		Description: "Wiki with a visual and Markdown editor, on PostgreSQL.",
		Image:       "ghcr.io/requarks/wiki:2", Port: 3000, Database: pg("wiki"),
		Env: env{
			"DB_TYPE": "postgres", "DB_HOST": "${db.HOST}", "DB_PORT": "${db.PORT}",
			"DB_NAME": "${db.DATABASE}", "DB_USER": "${db.USERNAME}", "DB_PASS": "${db.PASSWORD}",
		},
	},

	// ---- developer tools ----
	{
		ID: "jenkins", Name: "Jenkins", Category: "dev", Website: "https://www.jenkins.io",
		Description: "Automation server for builds, tests and deployments. The first admin password is in the app's logs.",
		Image:       "jenkins/jenkins:lts-jdk21", Port: 8080,
		Volumes:     vol("home", "/var/jenkins_home"),
		StartPeriod: 180,
	},
	{
		ID: "gitea", Name: "Gitea", Category: "dev", Website: "https://about.gitea.com",
		Description: "Git hosting with pull requests, issues and CI, on PostgreSQL.",
		Image:       "gitea/gitea:1", Port: 3000, Database: pg("gitea"),
		Volumes: vol("data", "/data"),
		Env: env{
			"GITEA__database__DB_TYPE": "postgres", "GITEA__database__HOST": "${db.HOST}:${db.PORT}",
			"GITEA__database__NAME": "${db.DATABASE}", "GITEA__database__USER": "${db.USERNAME}", "GITEA__database__PASSWD": "${db.PASSWORD}",
		},
	},
	{
		ID: "forgejo", Name: "Forgejo", Category: "dev", Website: "https://forgejo.org",
		Description: "Community Git forge with pull requests, issues and CI, on PostgreSQL.",
		Image:       "codeberg.org/forgejo/forgejo:12", Port: 3000, Database: pg("forgejo"),
		Volumes: vol("data", "/data"),
		Env: env{
			"FORGEJO__database__DB_TYPE": "postgres", "FORGEJO__database__HOST": "${db.HOST}:${db.PORT}",
			"FORGEJO__database__NAME": "${db.DATABASE}", "FORGEJO__database__USER": "${db.USERNAME}", "FORGEJO__database__PASSWD": "${db.PASSWORD}",
		},
	},
	{
		ID: "code-server", Name: "code-server", Category: "dev", Website: "https://coder.com/docs/code-server",
		Description: "VS Code in the browser. The password is in the app's variables.",
		Image:       "codercom/code-server:latest", Port: 8080,
		Volumes: vol("home", "/home/coder"),
		Env:     env{"PASSWORD": "${password}"},
	},
	{
		ID: "sonarqube", Name: "SonarQube", Category: "dev", Website: "https://www.sonarsource.com/products/sonarqube",
		Description: "Code quality and security analysis, on PostgreSQL. First login admin / admin. Needs about 2 GB of memory.",
		Image:       "sonarqube:community", Port: 9000, Database: pg("sonarqube"),
		Volumes: vol("data", "/opt/sonarqube/data", "extensions", "/opt/sonarqube/extensions", "logs", "/opt/sonarqube/logs"),
		Env: env{
			"SONAR_JDBC_URL":      "jdbc:postgresql://${db.HOST}:${db.PORT}/${db.DATABASE}",
			"SONAR_JDBC_USERNAME": "${db.USERNAME}", "SONAR_JDBC_PASSWORD": "${db.PASSWORD}",
			"SONAR_ES_BOOTSTRAP_CHECKS_DISABLE": "true",
		},
		StartPeriod: 300,
	},
	{
		ID: "mailpit", Name: "Mailpit", Category: "dev", Website: "https://mailpit.axllent.org",
		Description: "Catches the mail your apps send, for testing. Web inbox on this port; other apps send to ${app.mailpit.HOST}:1025.",
		Image:       "axllent/mailpit:latest", Port: 8025,
		Volumes: vol("data", "/data"),
		Env:     env{"MP_DATABASE": "/data/mailpit.db"},
	},

	// ---- automation ----
	{
		ID: "n8n", Name: "n8n", Category: "automation", Website: "https://n8n.io",
		Description: "Workflow automation with hundreds of integrations.",
		Image:       "n8nio/n8n:stable", Port: 5678,
		Volumes: vol("data", "/home/node/.n8n"),
		Env:     env{"N8N_PROXY_HOPS": "1"},
	},
	{
		ID: "node-red", Name: "Node-RED", Category: "automation", Website: "https://nodered.org",
		Description: "Flow-based programming for events, APIs and devices.",
		Image:       "nodered/node-red:4.0", Port: 1880,
		Volumes: vol("data", "/data"),
	},

	// ---- monitoring ----
	{
		ID: "uptime-kuma", Name: "Uptime Kuma", Category: "monitoring", Website: "https://uptime.kuma.pet",
		Description: "Uptime monitoring with status pages and alerts.",
		Image:       "louislam/uptime-kuma:2", Port: 3001,
		Volumes: vol("data", "/app/data"),
	},
	{
		ID: "grafana", Name: "Grafana", Category: "monitoring", Website: "https://grafana.com",
		Description: "Dashboards for metrics and logs. User admin; the password is in the app's variables.",
		Image:       "grafana/grafana:latest", Port: 3000,
		Volumes: vol("data", "/var/lib/grafana"),
		Env:     env{"GF_SECURITY_ADMIN_PASSWORD": "${password}"},
	},
	{
		ID: "prometheus", Name: "Prometheus", Category: "monitoring", Website: "https://prometheus.io",
		Description: "Metrics collection and alerting. Mount your prometheus.yml as a file on /etc/prometheus/prometheus.yml.",
		Image:       "prom/prometheus:v3", Port: 9090,
		Volumes: vol("data", "/prometheus"),
	},
	{
		ID: "gotify", Name: "Gotify", Category: "monitoring", Website: "https://gotify.net",
		Description: "Push notification server. User admin; the password is in the app's variables.",
		Image:       "gotify/server:2", Port: 80,
		Volumes: vol("data", "/app/data"),
		Env:     env{"GOTIFY_DEFAULTUSER_PASS": "${password}"},
	},
	{
		ID: "ntfy", Name: "ntfy", Category: "monitoring", Website: "https://ntfy.sh",
		Description: "Push notifications to phone and desktop over plain HTTP.",
		Image:       "binwiederhier/ntfy:v2", Port: 80, StartCommand: "ntfy serve",
		Volumes: vol("cache", "/var/cache/ntfy", "config", "/etc/ntfy"),
		Env:     env{"NTFY_CACHE_FILE": "/var/cache/ntfy/cache.db"},
	},

	// ---- analytics ----
	{
		ID: "clickhouse", Name: "ClickHouse", Category: "analytics", Website: "https://clickhouse.com",
		Description: "Column database for real-time analytics, on its HTTP interface. User default; the password is in the app's variables.",
		Image:       "clickhouse/clickhouse-server:25.8", Port: 8123,
		Volumes:     vol("data", "/var/lib/clickhouse", "logs", "/var/log/clickhouse-server"),
		Env:         env{"CLICKHOUSE_USER": "default", "CLICKHOUSE_PASSWORD": "${password}", "CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT": "1"},
		StartPeriod: 90,
	},
	{
		ID: "umami", Name: "Umami", Category: "analytics", Website: "https://umami.is",
		Description: "Privacy-friendly web analytics, on PostgreSQL. First login admin / umami.",
		Image:       "ghcr.io/umami-software/umami:postgresql-latest", Port: 3000, Database: pg("umami"),
		Env:         env{"DATABASE_URL": dbURL, "APP_SECRET": "${secret}"},
		StartPeriod: 120,
	},
	{
		ID: "metabase", Name: "Metabase", Category: "analytics", Website: "https://www.metabase.com",
		Description: "Charts and dashboards over your databases, on PostgreSQL.",
		Image:       "metabase/metabase:latest", Port: 3000, Database: pg("metabase"),
		Env: env{
			"MB_DB_TYPE": "postgres", "MB_DB_HOST": "${db.HOST}", "MB_DB_PORT": "${db.PORT}",
			"MB_DB_DBNAME": "${db.DATABASE}", "MB_DB_USER": "${db.USERNAME}", "MB_DB_PASS": "${db.PASSWORD}",
		},
		StartPeriod: 300,
	},
	{
		ID: "matomo", Name: "Matomo", Category: "analytics", Website: "https://matomo.org",
		Description: "Web analytics you own, with its MariaDB database.",
		Image:       "matomo:5-apache", Port: 80, Database: maria("matomo"),
		Volumes: vol("data", "/var/www/html"),
		Env: env{
			"MATOMO_DATABASE_ADAPTER": "mysql", "MATOMO_DATABASE_HOST": "${db.HOST}", "MATOMO_DATABASE_DBNAME": "${db.DATABASE}",
			"MATOMO_DATABASE_USERNAME": "${db.USERNAME}", "MATOMO_DATABASE_PASSWORD": "${db.PASSWORD}",
		},
	},

	// ---- data stores ----
	{
		ID: "redis", Name: "Redis", Category: "data", Website: "https://redis.io",
		Description: "In-memory cache and message broker. Other apps connect to ${app.redis.HOST}:6379; the password is in the app's variables.",
		Image:       "redis:8", Port: 6379, StartCommand: `redis-server --appendonly yes --requirepass "$REDIS_PASSWORD"`,
		Volumes: vol("data", "/data"),
		Env:     env{"REDIS_PASSWORD": "${password}"},
	},
	{
		ID: "mongodb", Name: "MongoDB", Category: "data", Website: "https://www.mongodb.com",
		Description: "Document database. User root; the password is in the app's variables.",
		Image:       "mongo:8", Port: 27017,
		Volumes: vol("data", "/data/db"),
		Env:     env{"MONGO_INITDB_ROOT_USERNAME": "root", "MONGO_INITDB_ROOT_PASSWORD": "${password}"},
	},
	{
		ID: "rabbitmq", Name: "RabbitMQ", Category: "data", Website: "https://www.rabbitmq.com",
		Description: "Message broker, with its management UI on this port and AMQP on 5672. User admin; the password is in the app's variables.",
		Image:       "rabbitmq:4-management", Port: 15672,
		Volumes: vol("data", "/var/lib/rabbitmq"),
		// The data folder is named after the node: a fixed name keeps it across deployments.
		Env:         env{"RABBITMQ_DEFAULT_USER": "admin", "RABBITMQ_DEFAULT_PASS": "${password}", "RABBITMQ_NODENAME": "rabbit@localhost"},
		StartPeriod: 90,
	},
	{
		ID: "meilisearch", Name: "Meilisearch", Category: "data", Website: "https://www.meilisearch.com",
		Description: "Fast full-text search engine. The master key is in the app's variables.",
		Image:       "getmeili/meilisearch:v1", Port: 7700,
		Volumes: vol("data", "/meili_data"),
		Env:     env{"MEILI_MASTER_KEY": "${password}", "MEILI_ENV": "production"},
	},
	{
		ID: "qdrant", Name: "Qdrant", Category: "data", Website: "https://qdrant.tech",
		Description: "Vector database for search and AI. The API key is in the app's variables.",
		Image:       "qdrant/qdrant:v1", Port: 6333,
		Volumes: vol("storage", "/qdrant/storage"),
		Env:     env{"QDRANT__SERVICE__API_KEY": "${password}"},
	},

	// ---- files ----
	{
		ID: "nextcloud", Name: "Nextcloud", Category: "files", Website: "https://nextcloud.com",
		Description: "Files, calendar and contacts, on PostgreSQL. Add your domain to NEXTCLOUD_TRUSTED_DOMAINS.",
		Image:       "nextcloud:32-apache", Port: 80, Database: pg("nextcloud"),
		Volumes: vol("data", "/var/www/html"),
		Env: env{
			"POSTGRES_HOST": "${db.HOST}:${db.PORT}", "POSTGRES_DB": "${db.DATABASE}",
			"POSTGRES_USER": "${db.USERNAME}", "POSTGRES_PASSWORD": "${db.PASSWORD}",
			"NEXTCLOUD_TRUSTED_DOMAINS": "localhost",
		},
		StartPeriod: 300,
	},

	// ---- collaboration ----
	{
		ID: "mattermost", Name: "Mattermost", Category: "collaboration", Website: "https://mattermost.com",
		Description: "Team chat with channels and threads, on PostgreSQL.",
		Image:       "mattermost/mattermost-team-edition:release-11", Port: 8065, Database: pg("mattermost"),
		Volumes: vol("data", "/mattermost/data", "config", "/mattermost/config", "logs", "/mattermost/logs", "plugins", "/mattermost/plugins"),
		Env: env{
			"MM_SQLSETTINGS_DRIVERNAME": "postgres",
			"MM_SQLSETTINGS_DATASOURCE": dbURL + "?sslmode=disable&connect_timeout=10",
		},
		StartPeriod: 120,
	},
	{
		ID: "planka", Name: "Planka", Category: "collaboration", Website: "https://planka.app",
		Description: "Kanban boards, on PostgreSQL. Set BASE_URL to the app's address. Admin admin@example.com; the password is in the app's variables.",
		Image:       "ghcr.io/plankanban/planka:latest", Port: 1337, Database: pg("planka"),
		Volumes: vol("favicons", "/app/public/favicons", "avatars", "/app/public/user-avatars", "backgrounds", "/app/public/background-images", "attachments", "/app/private/attachments"),
		Env: env{
			"BASE_URL": "http://localhost:1337", "DATABASE_URL": dbURL, "SECRET_KEY": "${secret}",
			"DEFAULT_ADMIN_EMAIL": "admin@example.com", "DEFAULT_ADMIN_PASSWORD": "${password}",
			"DEFAULT_ADMIN_NAME": "Admin", "DEFAULT_ADMIN_USERNAME": "admin",
		},
	},
	{
		ID: "hedgedoc", Name: "HedgeDoc", Category: "collaboration", Website: "https://hedgedoc.org",
		Description: "Collaborative Markdown notes, on PostgreSQL. Set CMD_DOMAIN to the app's domain.",
		Image:       "quay.io/hedgedoc/hedgedoc:1.10.3", Port: 3000, Database: pg("hedgedoc"),
		Volumes: vol("uploads", "/hedgedoc/public/uploads"),
		Env:     env{"CMD_DB_URL": dbURL, "CMD_SESSION_SECRET": "${secret}", "CMD_DOMAIN": "localhost", "CMD_PROTOCOL_USESSL": "true"},
	},
	{
		ID: "nocodb", Name: "NocoDB", Category: "collaboration", Website: "https://nocodb.com",
		Description: "Spreadsheet interface over a database, on PostgreSQL.",
		Image:       "nocodb/nocodb:latest", Port: 8080, Database: pg("nocodb"),
		Volumes: vol("data", "/usr/app/data"),
		Env: env{
			"NC_DB":              "pg://${db.HOST}:${db.PORT}?u=${db.USERNAME}&p=${db.PASSWORD}&d=${db.DATABASE}",
			"NC_AUTH_JWT_SECRET": "${secret}",
		},
		StartPeriod: 120,
	},
	{
		ID: "listmonk", Name: "listmonk", Category: "collaboration", Website: "https://listmonk.app",
		Description: "Newsletters and mailing lists, on PostgreSQL. User admin; the password is in the app's variables.",
		Image:       "listmonk/listmonk:latest", Port: 9000, Database: pg("listmonk"),
		// Creates or upgrades its tables, then serves: what the project's own Compose file runs.
		StartCommand: `./listmonk --install --idempotent --yes --config '' && ./listmonk --upgrade --yes --config '' && ./listmonk --config ''`,
		Volumes:      vol("uploads", "/listmonk/uploads"),
		Env: env{
			"LISTMONK_app__address": "0.0.0.0:9000", "LISTMONK_db__host": "${db.HOST}", "LISTMONK_db__port": "${db.PORT}",
			"LISTMONK_db__database": "${db.DATABASE}", "LISTMONK_db__user": "${db.USERNAME}", "LISTMONK_db__password": "${db.PASSWORD}",
			"LISTMONK_db__ssl_mode": "disable", "LISTMONK_ADMIN_USER": "admin", "LISTMONK_ADMIN_PASSWORD": "${password}",
		},
	},

	// ---- security ----
	{
		ID: "vaultwarden", Name: "Vaultwarden", Category: "security", Website: "https://github.com/dani-garcia/vaultwarden",
		Description: "Password manager compatible with Bitwarden apps. Needs HTTPS: add a domain. The admin page token is in the app's variables.",
		Image:       "vaultwarden/server:latest", Port: 80,
		Volumes: vol("data", "/data"),
		Env:     env{"ADMIN_TOKEN": "${password}"},
	},
	{
		ID: "keycloak", Name: "Keycloak", Category: "security", Website: "https://www.keycloak.org",
		Description: "Single sign-on and identity management, on PostgreSQL. User admin; the password is in the app's variables.",
		Image:       "quay.io/keycloak/keycloak:26.4", Port: 8080, Database: pg("keycloak"),
		StartCommand: "/opt/keycloak/bin/kc.sh start --http-enabled=true --proxy-headers=xforwarded --hostname-strict=false",
		Env: env{
			"KC_DB": "postgres", "KC_DB_URL": "jdbc:postgresql://${db.HOST}:${db.PORT}/${db.DATABASE}",
			"KC_DB_USERNAME": "${db.USERNAME}", "KC_DB_PASSWORD": "${db.PASSWORD}",
			"KC_BOOTSTRAP_ADMIN_USERNAME": "admin", "KC_BOOTSTRAP_ADMIN_PASSWORD": "${password}",
		},
		StartPeriod: 300,
	},

	// ---- reading and media ----
	{
		ID: "freshrss", Name: "FreshRSS", Category: "media", Website: "https://freshrss.org",
		Description: "Feed reader for RSS and Atom.",
		Image:       "freshrss/freshrss:1", Port: 80,
		Volumes: vol("data", "/var/www/FreshRSS/data", "extensions", "/var/www/FreshRSS/extensions"),
	},
	{
		ID: "miniflux", Name: "Miniflux", Category: "media", Website: "https://miniflux.app",
		Description: "Minimal feed reader, on PostgreSQL. User admin; the password is in the app's variables.",
		Image:       "miniflux/miniflux:latest", Port: 8080, Database: pg("miniflux"),
		Env: env{
			"DATABASE_URL": dbURL + "?sslmode=disable", "RUN_MIGRATIONS": "1", "CREATE_ADMIN": "1",
			"ADMIN_USERNAME": "admin", "ADMIN_PASSWORD": "${password}",
		},
	},
	{
		ID: "jellyfin", Name: "Jellyfin", Category: "media", Website: "https://jellyfin.org",
		Description: "Media server for films, series and music.",
		Image:       "jellyfin/jellyfin:10", Port: 8096,
		Volumes: vol("config", "/config", "cache", "/cache", "media", "/media"),
	},

	// ---- AI ----
	{
		ID: "ollama", Name: "Ollama", Category: "ai", Website: "https://ollama.com",
		Description: "Runs language models on the server, behind an HTTP API. Models need several GB of memory and disk.",
		Image:       "ollama/ollama:latest", Port: 11434,
		Volumes: vol("models", "/root/.ollama"),
	},
	{
		ID: "open-webui", Name: "Open WebUI", Category: "ai", Website: "https://openwebui.com",
		Description: "Chat interface for Ollama and OpenAI-compatible APIs. Set OLLAMA_BASE_URL to reach an Ollama app.",
		Image:       "ghcr.io/open-webui/open-webui:main", Port: 8080,
		Volumes:     vol("data", "/app/backend/data"),
		Env:         env{"WEBUI_SECRET_KEY": "${secret}"},
		StartPeriod: 300,
	},
}

// All returns the catalog, in display order.
func All() []Template { return catalog }

// Get returns the template with this ID.
func Get(id string) (Template, bool) {
	for _, t := range catalog {
		if t.ID == id {
			return t, true
		}
	}
	return Template{}, false
}

// AppInput is the app this template creates under name. Volume names are global to the
// Docker host, so they carry a random part. The variables that come from a database are
// created empty: the app's owner picks the database, and Connect fills them.
func (t Template) AppInput(name string) (store.AppInput, error) {
	suffix, err := random(4)
	if err != nil {
		return store.AppInput{}, err
	}
	in := store.AppInput{Name: name, SourceType: store.SourceImage, Image: t.Image, Port: t.Port, StartCommand: t.StartCommand, Template: t.ID,
		Environment: map[string]string{}, EnvMeta: map[string]store.EnvVarMeta{}, Mounts: []store.AppMount{}}
	for _, v := range t.Volumes {
		in.Mounts = append(in.Mounts, store.AppMount{Type: "volume", Source: name + "-" + suffix + "-" + v.Name, Target: v.Target})
	}
	for key, value := range t.Env {
		if strings.Contains(value, "${db.") {
			in.Environment[key] = ""
			continue
		}
		if strings.Contains(value, "${secret}") {
			secret, err := random(24)
			if err != nil {
				return store.AppInput{}, err
			}
			value = strings.ReplaceAll(value, "${secret}", secret)
			in.EnvMeta[key] = store.EnvVarMeta{Scope: store.EnvScopeRuntime, Secret: true}
		}
		if strings.Contains(value, "${password}") {
			// Not a secret: the app's owner reads it in the variables to sign in.
			password, err := random(12)
			if err != nil {
				return store.AppInput{}, err
			}
			value = strings.ReplaceAll(value, "${password}", password)
		}
		if strings.Contains(value, "${key}") {
			raw := make([]byte, 32)
			if _, err := rand.Read(raw); err != nil {
				return store.AppInput{}, err
			}
			value = strings.ReplaceAll(value, "${key}", base64.StdEncoding.EncodeToString(raw))
			in.EnvMeta[key] = store.EnvVarMeta{Scope: store.EnvScopeRuntime, Secret: true}
		}
		in.Environment[key] = value
	}
	return in, nil
}

// DatabaseVariables names the variables the template fills from its database, sorted.
func (t Template) DatabaseVariables() []string {
	out := []string{}
	for key, value := range t.Env {
		if strings.Contains(value, "${db.") {
			out = append(out, key)
		}
	}
	slices.Sort(out)
	return out
}

var dbField = regexp.MustCompile(`\$\{db\.([A-Z_]+)\}`)

// Connect is the template's database variables pointed at a database. With instance, they
// are references to that instance's own database (${db.NAME.HOST}), resolved at each
// deploy. With fields (HOST, PORT, DATABASE, USERNAME, PASSWORD, URL), for another database
// or user of an instance, they are those values written out; secret tells which variables
// then carry the password.
func (t Template) Connect(instance string, fields map[string]string) (env map[string]string, secret map[string]bool) {
	env, secret = map[string]string{}, map[string]bool{}
	for key, value := range t.Env {
		if !strings.Contains(value, "${db.") {
			continue
		}
		if fields == nil {
			env[key] = strings.ReplaceAll(value, "${db.", "${db."+instance+".")
			continue
		}
		secret[key] = strings.Contains(value, "${db.PASSWORD}") || strings.Contains(value, "${db.URL}")
		env[key] = dbField.ReplaceAllStringFunc(value, func(ref string) string {
			return fields[dbField.FindStringSubmatch(ref)[1]]
		})
	}
	return env, secret
}

func random(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

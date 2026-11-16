// Package api is the panel's HTTP surface: the JSON API under /api and the embedded web app.
package api

import (
	"context"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"panel/internal/account"
	"panel/internal/brand"
	"panel/internal/database"
	"panel/internal/gitsource"
	"panel/internal/storage"
	"panel/internal/store"
)

type Server struct {
	store         *store.Store
	accounts      *account.Service
	log           *slog.Logger
	version       string
	web           fs.FS
	authRate      *rateLimiter
	hookRate      *rateLimiter // webhook calls per IP
	proxy         ProxyApplier
	deployer      Deployer
	updater       Updater
	git           *gitsource.Service
	metrics       HostMetrics
	dockerMetrics DockerMetrics
	buildDefaults store.BuildLimits
	registryCheck RegistryChecker
	s3Test        S3Tester
	registry      BuiltinRegistry
	databases     *database.Service
	notifier      Notifier

	logPoll  time.Duration // how often a deployment log stream looks for new lines
	stop     chan struct{} // closed when the server shuts down; ends open streams
	stopOnce sync.Once

	// Seams for tests.
	lookupHost  func(ctx context.Context, host string) ([]string, error)
	serverAddrs func() []string
}

func New(st *store.Store, accounts *account.Service, log *slog.Logger, version string, web fs.FS) *Server {
	return &Server{
		store:    st,
		accounts: accounts,
		log:      log,
		version:  version,
		web:      web,
		authRate: newRateLimiter(10, 5*time.Minute),
		hookRate: newRateLimiter(60, time.Minute),

		logPoll: 300 * time.Millisecond,
		stop:    make(chan struct{}),

		lookupHost:  net.DefaultResolver.LookupHost,
		serverAddrs: publicAddrs,
		s3Test:      storage.Test,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/meta", s.handleMeta)

	mux.HandleFunc("POST /api/setup", s.limit(s.authRate, s.handleSetup))
	mux.HandleFunc("POST /api/auth/login", s.limit(s.authRate, s.handleLogin))
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/auth/me", s.requireAuth(s.handleMe))

	mux.HandleFunc("GET /api/settings/panel", s.requireAuth(s.handleGetPanelSettings))
	mux.HandleFunc("PUT /api/settings/panel", s.requireAuth(s.handlePutPanelSettings))
	mux.HandleFunc("GET /api/settings/panel/dns", s.requireAuth(s.handlePanelDNS))

	mux.HandleFunc("GET /api/settings/builds", s.requireAuth(s.handleGetBuildSettings))
	mux.HandleFunc("PUT /api/settings/builds", s.requireAuth(s.handlePutBuildSettings))
	mux.HandleFunc("DELETE /api/settings/builds", s.requireAuth(s.handleResetBuildSettings))

	mux.HandleFunc("GET /api/onboarding", s.requireAuth(s.handleGetOnboarding))
	mux.HandleFunc("POST /api/onboarding/complete", s.requireAuth(s.handleCompleteOnboarding))

	mux.HandleFunc("GET /api/updates", s.requireAuth(s.handleGetUpdates))
	mux.HandleFunc("POST /api/updates/check", s.requireAuth(s.handleCheckUpdates))
	mux.HandleFunc("POST /api/updates/apply", s.requireAuth(s.handleApplyUpdate))
	mux.HandleFunc("PUT /api/updates/settings", s.requireAuth(s.handleUpdateSettings))

	mux.HandleFunc("GET /api/tokens", s.requireAuth(sessionOnly(s.handleListTokens)))
	mux.HandleFunc("POST /api/tokens", s.requireAuth(sessionOnly(s.handleCreateToken)))
	mux.HandleFunc("DELETE /api/tokens/{id}", s.requireAuth(sessionOnly(s.handleDeleteToken)))

	mux.HandleFunc("GET /api/git-sources", s.requireAuth(s.withGit(s.handleListGitSources)))
	mux.HandleFunc("POST /api/git-sources/gitlab", s.requireAuth(s.withGit(s.handleAddGitLab)))
	mux.HandleFunc("POST /api/git-sources/deploy-key", s.requireAuth(s.withGit(s.handleAddDeployKey)))
	mux.HandleFunc("POST /api/git-sources/github", s.requireAuth(s.withGit(s.handleStartGitHub)))
	mux.HandleFunc("GET "+gitsource.CallbackPath, s.requireAuth(s.withGit(s.handleGitHubCallback)))
	mux.HandleFunc("DELETE /api/git-sources/{id}", s.requireAuth(s.withGit(s.handleDeleteGitSource)))
	mux.HandleFunc("GET /api/git-sources/{id}/repos", s.requireAuth(s.withGit(s.handleGitSourceRepos)))
	mux.HandleFunc("GET /api/git-sources/{id}/branches", s.requireAuth(s.withGit(s.handleGitSourceBranches)))

	mux.HandleFunc("GET /api/registries", s.requireAuth(s.handleListRegistries))
	mux.HandleFunc("POST /api/registries", s.requireAuth(s.handleAddRegistry))
	mux.HandleFunc("DELETE /api/registries/{id}", s.requireAuth(s.handleDeleteRegistry))
	mux.HandleFunc("GET /api/registry/settings", s.requireAuth(s.handleGetRegistrySettings))
	mux.HandleFunc("PUT /api/registry/settings", s.requireAuth(sessionOnly(s.handlePutRegistrySettings)))
	mux.HandleFunc("GET /api/registry/tokens", s.requireAuth(s.handleListRegistryTokens))
	mux.HandleFunc("POST /api/registry/tokens", s.requireAuth(sessionOnly(s.handleCreateRegistryToken)))
	mux.HandleFunc("DELETE /api/registry/tokens/{id}", s.requireAuth(sessionOnly(s.handleDeleteRegistryToken)))
	mux.HandleFunc("GET /api/registry/repositories", s.requireAuth(s.handleListRepositories))
	mux.HandleFunc("DELETE /api/registry/repositories/{repo...}", s.requireAuth(s.handleDeleteRepositoryTag))
	mux.HandleFunc("GET /api/registry/token", s.handleRegistryToken)    // docker's token requests, on the registry's domain
	mux.HandleFunc("POST /api/registry/token", s.handleRegistryToken)   // (the OAuth2 form flow)
	mux.HandleFunc("POST /api/registry/events", s.handleRegistryEvents) // the registry's push reports, with a shared secret
	mux.HandleFunc("POST /api/registry/gc", s.requireAuth(sessionOnly(s.handleRegistryGC)))
	mux.HandleFunc("PUT /api/apps/{id}/push-deploy", s.requireAuth(s.handleSetPushDeploy))
	mux.HandleFunc("GET /api/s3-connections", s.requireAuth(s.handleListS3))
	mux.HandleFunc("POST /api/s3-connections", s.requireAuth(s.handleAddS3))
	mux.HandleFunc("POST /api/s3-connections/{id}/test", s.requireAuth(s.handleTestS3))
	mux.HandleFunc("DELETE /api/s3-connections/{id}", s.requireAuth(s.handleDeleteS3))

	mux.HandleFunc("GET /api/projects", s.requireAuth(s.handleListProjects))
	mux.HandleFunc("GET /api/database-engines", s.requireAuth(s.handleDatabaseEngines))
	mux.HandleFunc("GET /api/database-operations/active", s.requireAuth(s.handleActiveDatabaseOperations))
	mux.HandleFunc("GET /api/database-operations/{id}", s.requireAuth(s.handleGetDatabaseOperation))
	mux.HandleFunc("GET /api/databases", s.requireAuth(s.handleListDatabases))
	mux.HandleFunc("POST /api/databases", s.requireAuth(s.handleCreateDatabase))
	mux.HandleFunc("GET /api/databases/{id}", s.requireAuth(s.handleGetDatabase))
	mux.HandleFunc("PATCH /api/databases/{id}", s.requireAuth(s.handleUpdateDatabase))
	mux.HandleFunc("GET /api/databases/{id}/connection", s.requireAuth(sessionOnly(s.handleDatabaseConnection)))
	mux.HandleFunc("/api/databases/{id}/adminer/", s.requireAuth(sessionOnly(s.handleAdminer)))
	mux.HandleFunc("GET /api/databases/{id}/public", s.requireAuth(s.handleDatabasePublic))
	mux.HandleFunc("PUT /api/databases/{id}/public", s.requireAuth(sessionOnly(s.handleDatabasePublic)))
	mux.HandleFunc("GET /api/databases/{id}/public/certificate", s.requireAuth(s.handleDatabaseCertificate))
	mux.HandleFunc("GET /api/databases/{id}/logs", s.requireAuth(s.handleDatabaseLogs))
	mux.HandleFunc("GET /api/databases/{id}/backup-policies", s.requireAuth(s.handleBackupPolicies))
	mux.HandleFunc("PUT /api/databases/{id}/backup-policies", s.requireAuth(sessionOnly(s.handleBackupPolicies)))
	mux.HandleFunc("GET /api/databases/{id}/backups", s.requireAuth(s.handleDatabaseBackups))
	mux.HandleFunc("POST /api/databases/{id}/backups", s.requireAuth(sessionOnly(s.handleDatabaseBackupAction("backup"))))
	mux.HandleFunc("DELETE /api/databases/{id}/backups/{backup}", s.requireAuth(sessionOnly(s.handleDeleteDatabaseBackup)))
	mux.HandleFunc("GET /api/database-backups/orphaned", s.requireAuth(s.handleOrphanBackups))
	mux.HandleFunc("DELETE /api/database-backups/orphaned/{backup}", s.requireAuth(sessionOnly(s.handleDeleteOrphanBackup)))
	mux.HandleFunc("POST /api/database-backups/orphaned/{backup}/restore", s.requireAuth(sessionOnly(s.handleRestoreOrphanBackup)))
	mux.HandleFunc("POST /api/databases/{id}/restore-instance", s.requireAuth(sessionOnly(s.handleRestoreInstance)))
	mux.HandleFunc("POST /api/databases/{id}/restore", s.requireAuth(sessionOnly(s.handleDatabaseBackupAction("restore"))))
	mux.HandleFunc("GET /api/databases/{id}/catalog", s.requireAuth(s.handleDatabaseCatalog))
	mux.HandleFunc("POST /api/databases/{id}/databases", s.requireAuth(sessionOnly(s.handleDatabaseManage("database_create"))))
	mux.HandleFunc("POST /api/databases/{id}/users", s.requireAuth(sessionOnly(s.handleDatabaseManage("user_create"))))
	mux.HandleFunc("DELETE /api/databases/{id}/databases/{database}", s.requireAuth(sessionOnly(s.handleDatabaseManage("database_delete"))))
	mux.HandleFunc("DELETE /api/databases/{id}/users/{user}", s.requireAuth(sessionOnly(s.handleDatabaseManage("user_delete"))))
	mux.HandleFunc("POST /api/databases/{id}/users/{user}/password", s.requireAuth(sessionOnly(s.handleDatabaseManage("password_rotate"))))
	mux.HandleFunc("PUT /api/databases/{id}/grants", s.requireAuth(sessionOnly(s.handleDatabaseManage("grant_update"))))
	mux.HandleFunc("GET /api/databases/{id}/users/{user}/connection", s.requireAuth(sessionOnly(s.handleDatabaseUserConnection)))
	mux.HandleFunc("POST /api/databases/{id}/{action}", s.requireAuth(s.handleDatabaseAction))
	mux.HandleFunc("DELETE /api/databases/{id}", s.requireAuth(s.handleDeleteDatabase))
	mux.HandleFunc("POST /api/projects", s.requireAuth(s.handleCreateProject))
	mux.HandleFunc("GET /api/projects/{id}", s.requireAuth(s.handleGetProject))
	mux.HandleFunc("PATCH /api/projects/{id}", s.requireAuth(s.handleUpdateProject))
	mux.HandleFunc("DELETE /api/projects/{id}", s.requireAuth(s.handleDeleteProject))
	mux.HandleFunc("GET /api/projects/{id}/environment", s.requireAuth(s.handleGetProjectEnv))
	mux.HandleFunc("PUT /api/projects/{id}/environment", s.requireAuth(s.handlePutProjectEnv))
	mux.HandleFunc("GET /api/projects/{id}/apps", s.requireAuth(s.handleListApps))
	mux.HandleFunc("GET /api/projects/{id}/databases", s.requireAuth(s.handleProjectDatabases))
	mux.HandleFunc("POST /api/projects/{id}/apps", s.requireAuth(s.handleCreateApp))

	mux.HandleFunc("GET /api/apps/{id}", s.requireAuth(s.handleGetApp))
	mux.HandleFunc("GET /api/apps/problems", s.requireAuth(s.handleAppProblems))
	mux.HandleFunc("PATCH /api/apps/{id}", s.requireAuth(s.handleUpdateApp))
	mux.HandleFunc("DELETE /api/apps/{id}", s.requireAuth(s.handleDeleteApp))
	mux.HandleFunc("POST /api/apps/{id}/deploy", s.requireAuth(s.handleDeploy))
	mux.HandleFunc("POST /api/apps/{id}/start", s.requireAuth(s.handleAppAction("start")))
	mux.HandleFunc("POST /api/apps/{id}/stop", s.requireAuth(s.handleAppAction("stop")))
	mux.HandleFunc("POST /api/apps/{id}/restart", s.requireAuth(s.handleAppAction("restart")))
	mux.HandleFunc("GET /api/apps/{id}/deployments", s.requireAuth(s.handleListDeployments))
	mux.HandleFunc("GET /api/apps/{id}/logs/stream", s.requireAuth(s.handleAppLogStream))
	mux.HandleFunc("GET /api/apps/{id}/terminal", s.requireAuth(sessionOnly(s.handleTerminal)))
	mux.HandleFunc("GET /api/apps/{id}/routes", s.requireAuth(s.handleAppRoutes))
	mux.HandleFunc("GET /api/notifications/kinds", s.requireAuth(s.handleNotificationKinds))
	mux.HandleFunc("GET /api/notification-channels", s.requireAuth(s.handleListChannels))
	mux.HandleFunc("POST /api/notification-channels", s.requireAuth(sessionOnly(s.handleCreateChannel)))
	mux.HandleFunc("PUT /api/notification-channels/{id}", s.requireAuth(sessionOnly(s.handleUpdateChannel)))
	mux.HandleFunc("DELETE /api/notification-channels/{id}", s.requireAuth(sessionOnly(s.handleDeleteChannel)))
	mux.HandleFunc("POST /api/notification-channels/{id}/test", s.requireAuth(sessionOnly(s.handleTestChannel)))
	mux.HandleFunc("GET /api/alert-rules", s.requireAuth(s.handleListRules))
	mux.HandleFunc("POST /api/alert-rules", s.requireAuth(sessionOnly(s.handleCreateRule)))
	mux.HandleFunc("PUT /api/alert-rules/{id}", s.requireAuth(sessionOnly(s.handleUpdateRule)))
	mux.HandleFunc("DELETE /api/alert-rules/{id}", s.requireAuth(sessionOnly(s.handleDeleteRule)))
	mux.HandleFunc("GET /api/apps/{id}/metrics", s.requireAuth(s.handleResourceMetrics("app")))
	mux.HandleFunc("GET /api/databases/{id}/metrics", s.requireAuth(s.handleResourceMetrics("database")))
	mux.HandleFunc("GET /api/projects/{id}/domains", s.requireAuth(s.handleListDomains))
	mux.HandleFunc("POST /api/projects/{id}/domains", s.requireAuth(s.handleCreateDomain))
	mux.HandleFunc("POST /api/domains/{id}/routes", s.requireAuth(s.handleCreateRoute))
	mux.HandleFunc("PATCH /api/routes/{id}", s.requireAuth(s.handleUpdateRoute))
	mux.HandleFunc("DELETE /api/routes/{id}", s.requireAuth(s.handleDeleteRoute))

	mux.HandleFunc("GET /api/apps/{id}/health-check", s.requireAuth(s.handleGetHealthCheck))
	mux.HandleFunc("PUT /api/apps/{id}/health-check", s.requireAuth(s.handleSetHealthCheck))
	mux.HandleFunc("GET /api/apps/{id}/deploy-hook", s.requireAuth(s.handleGetDeployHook))
	mux.HandleFunc("POST /api/apps/{id}/deploy-hook", s.requireAuth(s.handleNewDeployHook))
	mux.HandleFunc("DELETE /api/apps/{id}/deploy-hook", s.requireAuth(s.handleDeleteDeployHook))
	mux.HandleFunc("POST /api/hooks/deploy/{token}", s.limit(s.hookRate, s.handleDeployHook))

	mux.HandleFunc("GET /api/deployments/active", s.requireAuth(s.handleActiveDeployments))
	mux.HandleFunc("GET /api/deployments/{id}", s.requireAuth(s.handleGetDeployment))
	mux.HandleFunc("POST /api/deployments/{id}/cancel", s.requireAuth(s.handleCancelDeployment))
	mux.HandleFunc("POST /api/deployments/{id}/rollback", s.requireAuth(s.handleRollback))
	mux.HandleFunc("GET /api/deployments/{id}/log", s.requireAuth(s.handleDeploymentLog))
	mux.HandleFunc("GET /api/deployments/{id}/log/stream", s.requireAuth(s.handleDeploymentLogStream))
	mux.HandleFunc("PATCH /api/domains/{id}", s.requireAuth(s.handleUpdateDomain))
	mux.HandleFunc("DELETE /api/domains/{id}", s.requireAuth(s.handleDeleteDomain))
	mux.HandleFunc("GET /api/domains/{id}/dns", s.requireAuth(s.handleDomainDNS))

	mux.HandleFunc("GET /api/events", s.requireAuth(s.handleListEvents))
	mux.HandleFunc("GET /api/host/metrics", s.requireAuth(s.handleHostMetrics))
	mux.HandleFunc("GET /api/docker/metrics", s.requireAuth(s.handleDockerMetrics))

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "no such endpoint")
	})
	mux.Handle("/", s.spa())

	return s.recoverPanics(s.logRequests(securityHeaders(sameOrigin(mux))))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "unhealthy", "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleMeta is public: the web app reads the product name and setup state from here.
func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	required, err := s.accounts.SetupRequired(r.Context())
	if err != nil {
		s.internal(w, "meta", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":           brand.Name,
		"slug":           brand.Slug,
		"version":        s.version,
		"setup_required": required,
	})
}

func (s *Server) internal(w http.ResponseWriter, what string, err error) {
	s.log.Error(what, "error", err)
	writeError(w, http.StatusInternalServerError, "internal", "internal server error")
}

// StopStreams ends every open event stream. Call it when the HTTP server starts shutting
// down: a stream never finishes on its own, so it would hold the shutdown until it times out.
func (s *Server) StopStreams() { s.stopOnce.Do(func() { close(s.stop) }) }

// streamContext is the request context, also cancelled by StopStreams.
func (s *Server) streamContext(r *http.Request) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(r.Context())
	go func() {
		select {
		case <-s.stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

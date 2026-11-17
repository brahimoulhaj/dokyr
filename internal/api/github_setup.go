package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"panel/internal/auth"
	"panel/internal/brand"
	"panel/internal/store"
)

const githubSetupCallback = "/api/auth/github/setup/callback"

var githubSetupCode = regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`)

func (s *Server) startGitHubSetup(w http.ResponseWriter, r *http.Request, f loginFlow) {
	f.Kind = "github-setup"
	manifest, err := json.Marshal(map[string]any{
		"name":                brand.Slug + "-signin-" + auth.NewToken()[:8],
		"url":                 f.Origin,
		"description":         "Sign in to your " + brand.Name + " panel with GitHub.",
		"redirect_url":        f.Origin + githubSetupCallback,
		"callback_urls":       []string{f.Origin + githubLoginCallback},
		"hook_attributes":     map[string]any{"url": f.Origin, "active": false},
		"public":              false,
		"default_permissions": map[string]string{},
		"default_events":      []string{},
	})
	if err != nil {
		s.internal(w, "GitHub sign-in manifest", err)
		return
	}
	state, err := s.beginLoginFlow(w, f)
	if err != nil {
		writeError(w, http.StatusTooManyRequests, "busy", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"action_url": "https://github.com/settings/apps/new?state=" + url.QueryEscape(state),
		"manifest":   string(manifest),
	})
}

func (s *Server) handleGitHubSetupCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	fail := func() { http.Redirect(w, r, "/settings?tab=security&auth_error=github", http.StatusSeeOther) }
	cookie, err := r.Cookie(flowCookieName("github-setup"))
	if err != nil || r.URL.Query().Get("state") == "" || r.URL.Query().Get("state") != cookie.Value {
		fail()
		return
	}
	f, err := s.takeLoginFlow(w, r, "github-setup")
	code := r.URL.Query().Get("code")
	if err != nil || !githubSetupCode.MatchString(code) || r.URL.Query().Get("error") != "" {
		fail()
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	cfg, err := s.exchangeGitHubManifest(ctx, code)
	if err != nil {
		s.log.Warn("GitHub sign-in setup failed")
		fail()
		return
	}
	if err := s.store.CreateGitHubLoginConfig(ctx, cfg); err != nil {
		fail()
		return
	}
	// Continue straight into account authorization; creating an app never links
	// an identity by itself. Only the verified /user response can establish a link.
	link, err := s.githubAuthorizeURL(w, f, cfg)
	if err != nil {
		fail()
		return
	}
	http.Redirect(w, r, link, http.StatusSeeOther)
}

func (s *Server) exchangeGitHubManifest(ctx context.Context, code string) (store.GitHubLoginConfig, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.github.com/app-manifests/"+code+"/conversions", nil)
	if err != nil {
		return store.GitHubLoginConfig{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	client := s.authHTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	res, err := client.Do(req)
	if err != nil {
		return store.GitHubLoginConfig{}, err
	}
	defer res.Body.Close()
	var app struct {
		ID           int64  `json:"id"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&app)
	if err != nil || res.StatusCode != http.StatusCreated || app.ID <= 0 || app.ClientID == "" || app.ClientSecret == "" || len(app.ClientID) > 200 || len(app.ClientSecret) > 500 {
		return store.GitHubLoginConfig{}, errors.New("GitHub returned an invalid sign-in app")
	}
	return store.GitHubLoginConfig{ClientID: app.ClientID, ClientSecret: app.ClientSecret}, nil
}

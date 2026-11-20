package api

import (
	"cmp"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"panel/internal/store"
)

// tagRe is an image tag, as Docker accepts it.
var tagRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)

type deployHookJSON struct {
	Enabled bool   `json:"enabled"`
	Token   string `json:"token,omitempty"` // the URL is /api/hooks/deploy/<token>
}

// handleGetDeployHook tells whether the app's deploy webhook is on, with its token.
func (s *Server) handleGetDeployHook(w http.ResponseWriter, r *http.Request) {
	a, err := s.store.GetApp(r.Context(), r.PathValue("id"))
	if s.storeError(w, "get app", err, "") {
		return
	}
	token, err := s.store.AppDeployHook(r.Context(), a.ID)
	if err != nil {
		s.internal(w, "deploy hook", err)
		return
	}
	writeJSON(w, http.StatusOK, deployHookJSON{Enabled: token != "", Token: token})
}

// handleNewDeployHook turns the webhook on with a new token; an old URL stops working.
func (s *Server) handleNewDeployHook(w http.ResponseWriter, r *http.Request) {
	a, err := s.store.GetApp(r.Context(), r.PathValue("id"))
	if s.storeError(w, "get app", err, "") {
		return
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		s.internal(w, "deploy hook", err)
		return
	}
	token := hex.EncodeToString(b)
	if err := s.store.SetAppDeployHook(r.Context(), a.ID, token); err != nil {
		s.internal(w, "deploy hook", err)
		return
	}
	detail := "on"
	if a.DeployHook {
		detail = "new URL"
	}
	s.audit(r, "app.deploy_hook", "app", a.ID, a.Name, detail)
	writeJSON(w, http.StatusOK, deployHookJSON{Enabled: true, Token: token})
}

func (s *Server) handleDeleteDeployHook(w http.ResponseWriter, r *http.Request) {
	a, err := s.store.GetApp(r.Context(), r.PathValue("id"))
	if s.storeError(w, "get app", err, "") {
		return
	}
	if err := s.store.SetAppDeployHook(r.Context(), a.ID, ""); err != nil {
		s.internal(w, "deploy hook", err)
		return
	}
	s.audit(r, "app.deploy_hook", "app", a.ID, a.Name, "off")
	writeJSON(w, http.StatusOK, deployHookJSON{})
}

// hookEvent is what a webhook request asks for. Git hosts send a push (with the branch
// in ref); a CI script calls with no event, optionally with an image tag.
type hookEvent struct {
	kind   string // "push", "ping", "call", or the host's own event name (ignored)
	branch string // push: the branch pushed; "" for a tag push
	delete bool   // push: the branch was deleted
	tag    string // call: deploy this image tag
	after  string // push: the commit the branch now points at, when the host says
	repo   string // push: owner/name of the repository, when the host says
	// push: the paths the pushed commits added, changed or removed. filesKnown is false
	// when the host did not list them all (a forced push, a new branch, more commits than
	// the payload carries): then nothing can be said about what changed.
	files      []string
	filesKnown bool
}

// maxHookBody is what GitHub caps a push payload at.
const maxHookBody = 25 << 20

func readHookEvent(r *http.Request) (hookEvent, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxHookBody))
	if err != nil {
		return hookEvent{}, err
	}
	return parseHookEvent(r, raw)
}

func parseHookEvent(r *http.Request, raw []byte) (hookEvent, error) {
	event := strings.ToLower(firstHeader(r, "X-GitHub-Event", "X-Gitea-Event", "X-Forgejo-Event", "X-Gogs-Event", "X-Gitlab-Event"))
	var body struct {
		Ref     string `json:"ref"`
		Deleted bool   `json:"deleted"`
		Created bool   `json:"created"`
		Forced  bool   `json:"forced"`
		After   string `json:"after"`
		Tag     string `json:"tag"`
		Commits []struct {
			Added    []string `json:"added"`
			Modified []string `json:"modified"`
			Removed  []string `json:"removed"`
		} `json:"commits"`
		TotalCommits      int `json:"total_commits"`       // Gitea, Forgejo
		TotalCommitsCount int `json:"total_commits_count"` // GitLab, which lists 20 at most
		Repository        struct {
			FullName          string `json:"full_name"`           // GitHub, Gitea
			PathWithNamespace string `json:"path_with_namespace"` // GitLab
		} `json:"repository"`
		Project struct {
			PathWithNamespace string `json:"path_with_namespace"` // GitLab
		} `json:"project"`
	}
	// GitHub's default content type sends the JSON form-encoded, in a payload field.
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		form, err := url.ParseQuery(string(raw))
		if err != nil {
			return hookEvent{}, errors.New("the form body could not be read")
		}
		raw = []byte(form.Get("payload"))
	}
	if len(strings.TrimSpace(string(raw))) > 0 {
		// Git hosts send JSON (GitHub only when the webhook's content type is application/json).
		if err := json.Unmarshal(raw, &body); err != nil {
			return hookEvent{}, errors.New("the body must be JSON")
		}
	}
	if tag := r.URL.Query().Get("tag"); tag != "" {
		body.Tag = tag
	}
	push := func() hookEvent {
		e := hookEvent{kind: "push", delete: body.Deleted || strings.Trim(body.After, "0") == "" && body.After != ""}
		if b, ok := strings.CutPrefix(body.Ref, "refs/heads/"); ok {
			e.branch = b
		}
		if !e.delete {
			e.after = body.After
		}
		e.repo = cmp.Or(body.Repository.FullName, body.Repository.PathWithNamespace, body.Project.PathWithNamespace)
		for _, c := range body.Commits {
			e.files = append(append(append(e.files, c.Added...), c.Modified...), c.Removed...)
		}
		// GitHub lists 2048 commits at most and says nothing of the rest.
		total := max(body.TotalCommits, body.TotalCommitsCount)
		e.filesKnown = len(body.Commits) > 0 && len(body.Commits) < 2048 && total <= len(body.Commits) && !body.Forced && !body.Created
		return e
	}
	switch event {
	case "push", "push hook":
		return push(), nil
	case "ping":
		return hookEvent{kind: "ping"}, nil
	case "":
		if body.Ref != "" { // a host that names no event but sends a push payload
			return push(), nil
		}
		return hookEvent{kind: "call", tag: body.Tag}, nil
	}
	return hookEvent{kind: event}, nil
}

func firstHeader(r *http.Request, names ...string) string {
	for _, n := range names {
		if v := r.Header.Get(n); v != "" {
			return v
		}
	}
	return ""
}

// handleDeployHook is POST /api/hooks/deploy/<token>: the token in the URL is the only
// credential. A push to the app's branch deploys it; pushes to other branches, deleted
// branches and tags are ignored, with the reason in the answer. A call without a Git
// event deploys; an image app can name a new tag. A deploy arriving while the app is busy
// runs once the current one ends.
func (s *Server) handleDeployHook(w http.ResponseWriter, r *http.Request) {
	if !s.requireDeployer(w) {
		return
	}
	a, err := s.store.AppByDeployHook(r.Context(), r.PathValue("token"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no app has this deploy webhook; it may have been turned off or replaced")
		return
	}
	if err != nil {
		s.internal(w, "deploy hook", err)
		return
	}
	ev, err := readHookEvent(r)
	if err != nil {
		invalid(w, err.Error())
		return
	}
	ignore := func(reason string) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ignored", "reason": reason})
	}
	trigger := "webhook"
	switch ev.kind {
	case "ping":
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "app": a.Name})
		return
	case "push":
		switch {
		case a.SourceType != store.SourceGit:
			ignore("the app deploys an image, not a Git branch; call the webhook from CI instead")
			return
		case ev.branch == "":
			ignore("not a branch push (a tag?)")
			return
		case ev.delete:
			ignore("the branch was deleted")
			return
		case ev.branch != a.Git.Branch:
			ignore("push to " + ev.branch + "; the app deploys " + a.Git.Branch)
			return
		case ev.filesKnown && !watchMatch(a.WatchPaths, ev.files):
			ignore("the push changed nothing under the app's watch paths")
			return
		case s.pushes.seen(a.ID, ev.after):
			ignore("this commit is already being deployed")
			return
		}
		trigger = "push"
	case "call":
		if ev.tag != "" {
			if a.SourceType != store.SourceImage {
				invalid(w, "tag is for image apps; a Git app deploys its branch")
				return
			}
			if !tagRe.MatchString(ev.tag) {
				invalid(w, "tag must be an image tag like 1.4.2 or sha-3f2a9c1")
				return
			}
			image := withTag(a.Image, ev.tag)
			if image != a.Image {
				if err := s.store.SetAppImage(r.Context(), a.ID, image); err != nil {
					s.internal(w, "deploy hook", err)
					return
				}
				a.Image = image
			}
		}
	default:
		ignore(ev.kind + " events do not deploy; send push events")
		return
	}

	dep, queued, err := s.deployer.DeployOrQueue(r.Context(), a.ID, trigger)
	if s.deployError(w, "deploy hook", err) {
		return
	}
	detail := trigger
	if ev.tag != "" {
		detail += " " + a.Image
	}
	if err := s.store.AddEvent(r.Context(), store.Event{Actor: "webhook", Action: "app.deploy", TargetType: "app", TargetID: a.ID, TargetName: a.Name, Detail: detail}); err != nil {
		s.log.Error("record event", "error", err)
	}
	if queued {
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "queued", "reason": "a deployment is running; this one starts when it ends"})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "deploying", "deployment": toDeploymentJSON(dep)})
}

// recentPushes remembers which commit each app was last told to deploy by a push, for a
// short while: an app with both a repository webhook and the GitHub App's hears of one
// push twice, and deploys it once.
type recentPushes struct {
	mu sync.Mutex
	at map[string]time.Time // app id and commit → when
}

const pushMemory = 2 * time.Minute

// seen reports whether this commit was already announced for the app, and remembers it.
// A push that names no commit is never a repeat.
func (p *recentPushes) seen(appID, commit string) bool {
	if commit == "" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	for k, t := range p.at {
		if now.Sub(t) > pushMemory {
			delete(p.at, k)
		}
	}
	if p.at == nil {
		p.at = map[string]time.Time{}
	}
	key := appID + "\x00" + commit
	if _, ok := p.at[key]; ok {
		return true
	}
	p.at[key] = now
	return false
}

// repoName is owner/name of a repository URL, lower case: "https://github.com/Org/App.git"
// and "git@github.com:Org/App.git" are both "org/app".
func repoName(repository string) string {
	name := repository
	if u, err := url.Parse(repository); err == nil && u.Host != "" {
		name = u.Path
	} else if i := strings.Index(repository, ":"); i >= 0 {
		name = repository[i+1:]
	}
	return strings.ToLower(strings.TrimSuffix(strings.Trim(name, "/"), ".git"))
}

// handleGitHubAppHook is POST /api/hooks/github: the webhook of a GitHub App connection.
// GitHub signs every delivery with the app's webhook secret; nothing is done before the
// signature is checked. A push deploys the apps of that connection that build the pushed
// repository and branch and have deploy on push turned on.
func (s *Server) handleGitHubAppHook(w http.ResponseWriter, r *http.Request) {
	if !s.requireDeployer(w) {
		return
	}
	if s.git == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "Git connections are not available on this panel")
		return
	}
	appID, err := strconv.ParseInt(r.Header.Get("X-GitHub-Hook-Installation-Target-ID"), 10, 64)
	signature, signed := strings.CutPrefix(r.Header.Get("X-Hub-Signature-256"), "sha256=")
	if err != nil || appID <= 0 || !signed {
		writeError(w, http.StatusUnauthorized, "unauthorized", "not a signed delivery of a GitHub App")
		return
	}
	connID, secret, err := s.git.GitHubWebhook(r.Context(), appID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no Git connection is this GitHub App")
		return
	}
	if err != nil {
		s.internal(w, "github hook", err)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxHookBody))
	if err != nil {
		invalid(w, "the body could not be read")
		return
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(raw)
	if want, err := hex.DecodeString(signature); err != nil || !hmac.Equal(want, mac.Sum(nil)) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "the signature does not match this app's webhook secret")
		return
	}
	ev, err := parseHookEvent(r, raw)
	if err != nil {
		invalid(w, err.Error())
		return
	}
	switch {
	case ev.kind == "ping":
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
		return
	case ev.kind != "push":
		writeJSON(w, http.StatusOK, map[string]any{"status": "ignored", "reason": ev.kind + " events do not deploy"})
		return
	case ev.branch == "" || ev.delete || ev.repo == "":
		writeJSON(w, http.StatusOK, map[string]any{"status": "ignored", "reason": "not a push of commits to a branch"})
		return
	}
	apps, err := s.store.AppsByGitConnection(r.Context(), connID)
	if err != nil {
		s.internal(w, "github hook", err)
		return
	}
	// GitHub waits ten seconds for an answer; the deploys start behind it.
	ctx := context.WithoutCancel(r.Context())
	deployed, ignored := []string{}, map[string]string{}
	for _, a := range apps {
		if repoName(a.Git.Repository) != strings.ToLower(ev.repo) || a.Git.Branch != ev.branch {
			continue
		}
		switch {
		case !a.PushDeploy:
			ignored[a.Name] = "deploy on push is off"
			continue
		case ev.filesKnown && !watchMatch(a.WatchPaths, ev.files):
			ignored[a.Name] = "the push changed nothing under the app's watch paths"
			continue
		case s.pushes.seen(a.ID, ev.after):
			ignored[a.Name] = "this commit is already being deployed"
			continue
		}
		if _, _, err := s.deployer.DeployOrQueue(ctx, a.ID, "push"); err != nil {
			s.log.Warn("github hook: deploy", "app", a.Name, "error", err)
			ignored[a.Name] = "the deployment could not start"
			continue
		}
		deployed = append(deployed, a.Name)
		if err := s.store.AddEvent(ctx, store.Event{Actor: "webhook", Action: "app.deploy", TargetType: "app", TargetID: a.ID, TargetName: a.Name, Detail: "push (GitHub App)"}); err != nil {
			s.log.Error("record event", "error", err)
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "ok", "deployed": deployed, "ignored": ignored})
}

// withTag replaces the tag (or digest) of an image reference: "ghcr.io/org/app:1" with
// tag 2 is "ghcr.io/org/app:2". A colon before the last slash is a registry port.
func withTag(image, tag string) string {
	repo := image
	if i := strings.Index(repo, "@"); i >= 0 {
		repo = repo[:i]
	}
	if i := strings.LastIndex(repo, ":"); i > strings.LastIndex(repo, "/") {
		repo = repo[:i]
	}
	return repo + ":" + tag
}

package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"panel/internal/auth"
	"panel/internal/brand"
	"panel/internal/store"
)

// Sign-in of the remote CLI through the browser (an authorization code with PKCE):
//
//  1. The CLI listens on a loopback port and opens /cli/authorize in the browser with
//     that port, a state and the SHA-256 of a secret it keeps (the challenge).
//  2. The signed-in user approves there: POST /api/cli/authorize answers with a one-time
//     code inside a redirect to http://127.0.0.1:<port>/callback.
//  3. The CLI exchanges the code and its secret (the verifier) at POST /api/cli/token
//     for an API token of that user.
//
// The panel builds the redirect itself from the port alone, so a code only ever travels
// to the machine the browser runs on, and it is useless there without the verifier.

const (
	cliGrantTTL  = 5 * time.Minute
	cliGrantsMax = 1000 // pending approvals kept at once
	cliDeviceMax = 48   // runes; leaves room for the prefix in a token name of 64
)

// cliChallengeRe is a SHA-256 in unpadded base64url; cliStateRe what the CLI may send as state.
var (
	cliChallengeRe = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	cliStateRe     = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)
)

// cliGrant is an approval waiting for its exchange.
type cliGrant struct {
	userID    string
	challenge string
	device    string
	expires   time.Time
}

// cliGrants holds pending approvals by the hash of their code, in memory: they live a
// few minutes, and a restart only means running the login command again.
type cliGrants struct {
	mu sync.Mutex
	m  map[string]cliGrant
}

func (g *cliGrants) add(codeHash string, grant cliGrant) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	for k, v := range g.m {
		if now.After(v.expires) {
			delete(g.m, k)
		}
	}
	if len(g.m) >= cliGrantsMax {
		return false
	}
	if g.m == nil {
		g.m = map[string]cliGrant{}
	}
	g.m[codeHash] = grant
	return true
}

// take removes and returns the grant of a code: a code works once, whatever comes of it.
func (g *cliGrants) take(codeHash string) (cliGrant, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	grant, ok := g.m[codeHash]
	delete(g.m, codeHash)
	return grant, ok && time.Now().Before(grant.expires)
}

// cliDeviceName is the name the CLI gave its machine, made safe to show and to store.
func cliDeviceName(raw string) string {
	name := strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || !unicode.IsPrint(r) {
			return -1
		}
		return r
	}, raw))
	if utf8.RuneCountInString(name) > cliDeviceMax {
		name = string([]rune(name)[:cliDeviceMax])
	}
	if name == "" {
		name = "unknown device"
	}
	return name
}

// handleCLIAuthorize is the click on "Approve" in the web app. Session only: a token
// cannot approve a sign-in, so it cannot mint another token.
func (s *Server) handleCLIAuthorize(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Port      int    `json:"port"`
		State     string `json:"state"`
		Challenge string `json:"challenge"`
		Device    string `json:"device"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Port < 1024 || in.Port > 65535 || !cliStateRe.MatchString(in.State) || !cliChallengeRe.MatchString(in.Challenge) {
		writeError(w, http.StatusUnprocessableEntity, "invalid", "this sign-in request is not valid; run the login command again")
		return
	}
	code := auth.NewToken()
	grant := cliGrant{userID: userFrom(r.Context()).ID, challenge: in.Challenge, device: cliDeviceName(in.Device), expires: time.Now().Add(cliGrantTTL)}
	if !s.cliGrants.add(auth.HashToken(code), grant) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts, try again later")
		return
	}
	redirect := url.URL{
		Scheme:   "http",
		Host:     "127.0.0.1:" + strconv.Itoa(in.Port),
		Path:     "/callback",
		RawQuery: url.Values{"code": {code}, "state": {in.State}}.Encode(),
	}
	writeJSON(w, http.StatusOK, map[string]string{"redirect": redirect.String()})
}

// handleCLIToken exchanges an approved code for an API token. No session and no token
// here: the code and the verifier are the proof.
func (s *Server) handleCLIToken(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code     string `json:"code"`
		Verifier string `json:"verifier"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	refuse := func() {
		writeError(w, http.StatusBadRequest, "invalid_grant", "this sign-in code is not valid or has expired; run the login command again")
	}
	grant, ok := s.cliGrants.take(auth.HashToken(in.Code))
	if !ok {
		refuse()
		return
	}
	sum := sha256.Sum256([]byte(in.Verifier))
	if subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(grant.challenge)) != 1 {
		refuse()
		return
	}
	u, err := s.store.GetUserByID(r.Context(), grant.userID)
	if err == store.ErrNotFound { // removed between the approval and the exchange
		refuse()
		return
	}
	if err != nil {
		s.internal(w, "cli sign-in user", err)
		return
	}
	secret := brand.Slug + "_" + auth.NewToken()
	t, err := s.store.CreateAPIToken(r.Context(), u.ID, "CLI on "+grant.device, auth.HashToken(secret))
	if err != nil {
		s.internal(w, "create cli token", err)
		return
	}
	if err := s.store.AddEvent(r.Context(), store.Event{
		UserID: u.ID, Actor: u.Email, Action: "token.create", TargetType: "token", TargetID: t.ID, TargetName: t.Name, Detail: "cli sign-in",
	}); err != nil {
		s.log.Error("record event", "action", "token.create", "error", err)
	}
	writeJSON(w, http.StatusCreated, map[string]any{"token": secret, "token_name": t.Name, "user": toUserJSON(u)})
}

// handleCLILogout revokes the token the request came with: `dokyr logout`. A token
// cannot touch any other token (see sessionOnly), but ending itself harms nobody, and
// a signed-out laptop should not leave a working credential behind.
func (s *Server) handleCLILogout(w http.ResponseWriter, r *http.Request) {
	t := tokenFrom(r.Context())
	if t == nil {
		writeError(w, http.StatusBadRequest, "token_required", "this endpoint ends an API token; a browser session signs out at /api/auth/logout")
		return
	}
	gone, err := s.store.DeleteAPIToken(r.Context(), userFrom(r.Context()).ID, t.ID)
	if s.storeError(w, "revoke own token", err, "") {
		return
	}
	s.audit(r, "token.revoke", "token", gone.ID, gone.Name, "cli sign-out")
	w.WriteHeader(http.StatusNoContent)
}

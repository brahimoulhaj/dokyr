package api

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/gorilla/websocket"
	"panel/internal/brand"
	"panel/internal/executor"
	"panel/internal/store"
)

type terminalDeployer interface {
	OpenTerminal(context.Context, string, string, uint, uint) (executor.Terminal, error)
	// HostPaths lists the host paths the app's running container mounts.
	HostPaths(ctx context.Context, appID string) ([]string, error)
}

func terminalOrigin(r *http.Request) bool {
	u, err := url.Parse(r.Header.Get("Origin"))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host == r.Host && u.User == nil
}

func terminalSize(cols, rows int) bool { return cols >= 2 && cols <= 500 && rows >= 1 && rows <= 300 }

func (s *Server) handleTerminal(w http.ResponseWriter, r *http.Request) {
	if !terminalOrigin(r) {
		writeError(w, http.StatusForbidden, "cross_origin", "cross-origin terminal blocked")
		return
	}
	d, ok := s.deployer.(terminalDeployer)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "container terminal is unavailable")
		return
	}
	cols, _ := strconv.Atoi(r.URL.Query().Get("cols"))
	rows, _ := strconv.Atoi(r.URL.Query().Get("rows"))
	shell := r.URL.Query().Get("shell")
	if !terminalSize(cols, rows) || !executor.ValidShell(shell) {
		invalid(w, "choose a supported shell and valid terminal dimensions")
		return
	}
	a, err := s.store.GetApp(r.Context(), r.PathValue("id"))
	if s.storeError(w, "get terminal app", err, "") {
		return
	}
	// A shell in a container that mounts a host path is a shell on that path. The running
	// container decides, not only the saved settings: removing the mount there changes
	// nothing until the next deploy.
	if !isAdmin(userFrom(r.Context())) {
		bound := hasBind(store.AppInput{Mounts: a.Mounts})
		if !bound {
			paths, err := d.HostPaths(r.Context(), a.ID)
			bound = err == nil && len(paths) > 0
		}
		if bound {
			writeError(w, http.StatusForbidden, "forbidden", "this app mounts a host path, so only an admin can open its terminal")
			return
		}
	}
	s.serveTerminal(w, r, terminalSession{
		open: func(ctx context.Context) (executor.Terminal, error) {
			return d.OpenTerminal(ctx, a.ID, shell, uint(cols), uint(rows))
		},
		failed: "Cannot open shell. Check the container is running and the selected shell is installed.",
		audit:  func(action string) { s.audit(r, "app.terminal."+action, "app", a.ID, a.Name, shell) },
	})
}

// HostTerminal opens a shell on the host the panel runs on, as user (empty: root).
type HostTerminal func(ctx context.Context, user string, cols, rows uint) (executor.Terminal, error)

func (s *Server) SetHostTerminal(open HostTerminal) { s.hostTerminal = open }

// hostUnlockFor is how long a password confirmation lets a browser session open host shells.
const hostUnlockFor = 15 * time.Minute

// hostUnlocks remembers which browser sessions confirmed their password, and until when.
type hostUnlocks struct {
	mu    sync.Mutex
	until map[string]time.Time // by session hash
}

func (u *hostUnlocks) grant(session string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	now := time.Now()
	for k, t := range u.until {
		if now.After(t) {
			delete(u.until, k)
		}
	}
	if u.until == nil {
		u.until = map[string]time.Time{}
	}
	u.until[session] = now.Add(hostUnlockFor)
}

func (u *hostUnlocks) ok(session string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return session != "" && time.Now().Before(u.until[session])
}

// confirmedFor is how long a passkey confirmation waits for the action it was given for.
const confirmedFor = 2 * time.Minute

// confirmations holds the passkey confirmations not yet spent, by browser session. One
// confirmation allows one action: take spends it.
type confirmations struct {
	mu    sync.Mutex
	until map[string]time.Time
}

func (c *confirmations) grant(session string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, t := range c.until {
		if now.After(t) {
			delete(c.until, k)
		}
	}
	if c.until == nil {
		c.until = map[string]time.Time{}
	}
	c.until[session] = now.Add(confirmedFor)
}

func (c *confirmations) take(session string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.until[session]
	delete(c.until, session)
	return ok && session != "" && time.Now().Before(t)
}

// handleBeginConfirmPasskey starts a passkey confirmation of one destructive action, for
// whoever does not have the password at hand.
func (s *Server) handleBeginConfirmPasskey(w http.ResponseWriter, r *http.Request) {
	s.beginPasskeyConfirmation(w, r, "passkey-confirm")
}

// handleFinishConfirmPasskey lets this browser session do one such action, soon.
func (s *Server) handleFinishConfirmPasskey(w http.ResponseWriter, r *http.Request) {
	if s.finishPasskeyConfirmation(w, r, "passkey-confirm") {
		s.confirmed.grant(sessionHash(r))
		w.WriteHeader(http.StatusNoContent)
	}
}

// Idle minutes of a host shell: the default, and the longest that can be set.
const (
	hostIdleDefault = 30
	hostIdleMax     = 480
)

type hostTerminalSettings struct {
	Enabled     bool   `json:"enabled"`
	User        string `json:"user"`         // empty: root
	IdleMinutes *int   `json:"idle_minutes"` // without input before a shell is closed; 0: never
}

// hostIdleUnit is a minute; a test shortens it.
var hostIdleUnit = time.Minute

func (c hostTerminalSettings) idle() time.Duration {
	if c.IdleMinutes == nil {
		return hostIdleDefault * hostIdleUnit
	}
	return time.Duration(*c.IdleMinutes) * hostIdleUnit
}

func (s *Server) hostTerminalSettings(ctx context.Context) (hostTerminalSettings, error) {
	off, _, err := s.store.GetSetting(ctx, store.SettingHostTermOff)
	if err != nil {
		return hostTerminalSettings{}, err
	}
	user, _, err := s.store.GetSetting(ctx, store.SettingHostTermUsr)
	if err != nil {
		return hostTerminalSettings{}, err
	}
	idle := hostIdleDefault
	if v, ok, err := s.store.GetSetting(ctx, store.SettingHostTermIdle); err != nil {
		return hostTerminalSettings{}, err
	} else if ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= hostIdleMax {
			idle = n
		}
	}
	return hostTerminalSettings{Enabled: off != "true", User: user, IdleMinutes: &idle}, nil
}

func (s *Server) handleGetHostTerminalSettings(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.hostTerminalSettings(r.Context())
	if err != nil {
		s.internal(w, "host terminal settings", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": cfg.Enabled, "user": cfg.User, "idle_minutes": *cfg.IdleMinutes,
		"available": s.hostTerminal != nil, "unlocked": s.hostUnlocks.ok(sessionHash(r)),
		"passkey": s.hasPasskey(r),
	})
}

func (s *Server) handlePutHostTerminalSettings(w http.ResponseWriter, r *http.Request) {
	var in hostTerminalSettings
	if !readJSON(w, r, &in) {
		return
	}
	in.User = strings.TrimSpace(in.User)
	if in.User == "root" {
		in.User = ""
	}
	if !executor.ValidHostUser(in.User) {
		invalid(w, "user must be a user name of this server: lowercase letters, digits, - and _")
		return
	}
	if in.IdleMinutes == nil { // a client from before the setting keeps what is saved
		current, err := s.hostTerminalSettings(r.Context())
		if err != nil {
			s.internal(w, "host terminal settings", err)
			return
		}
		in.IdleMinutes = current.IdleMinutes
	}
	if *in.IdleMinutes < 0 || *in.IdleMinutes > hostIdleMax {
		invalid(w, "idle time must be between 0 (never) and "+strconv.Itoa(hostIdleMax)+" minutes")
		return
	}
	off := "false"
	if !in.Enabled {
		off = "true"
	}
	if err := s.store.SetSetting(r.Context(), store.SettingHostTermOff, off); err != nil {
		s.internal(w, "host terminal settings", err)
		return
	}
	if err := s.store.SetSetting(r.Context(), store.SettingHostTermUsr, in.User); err != nil {
		s.internal(w, "host terminal settings", err)
		return
	}
	if err := s.store.SetSetting(r.Context(), store.SettingHostTermIdle, strconv.Itoa(*in.IdleMinutes)); err != nil {
		s.internal(w, "host terminal settings", err)
		return
	}
	detail := "off"
	if in.Enabled {
		detail = "on, as " + cmp.Or(in.User, "root")
		if *in.IdleMinutes > 0 {
			detail += ", closed after " + strconv.Itoa(*in.IdleMinutes) + " min idle"
		}
	}
	s.audit(r, "settings.host_terminal", "settings", "host_terminal", "", detail)
	s.handleGetHostTerminalSettings(w, r)
}

// handleUnlockHostTerminal takes the password again: a browser session alone, which can
// be stolen, does not open a shell on the host.
func (s *Server) handleUnlockHostTerminal(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) || !s.confirmLoginPassword(w, r, in.Password) {
		return
	}
	s.hostUnlocks.grant(sessionHash(r))
	w.WriteHeader(http.StatusNoContent)
}

// handleHostTerminal is a shell on the host: browser session of the owner or an admin
// only, after a password confirmation, unless it is switched off in Settings.
func (s *Server) handleHostTerminal(w http.ResponseWriter, r *http.Request) {
	if !terminalOrigin(r) {
		writeError(w, http.StatusForbidden, "cross_origin", "cross-origin terminal blocked")
		return
	}
	if s.hostTerminal == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "host terminal is unavailable")
		return
	}
	cols, _ := strconv.Atoi(r.URL.Query().Get("cols"))
	rows, _ := strconv.Atoi(r.URL.Query().Get("rows"))
	if !terminalSize(cols, rows) {
		invalid(w, "choose valid terminal dimensions")
		return
	}
	cfg, err := s.hostTerminalSettings(r.Context())
	if err != nil {
		s.internal(w, "host terminal settings", err)
		return
	}
	if !cfg.Enabled {
		writeError(w, http.StatusForbidden, "disabled", "the host terminal is switched off in Settings")
		return
	}
	if !s.hostUnlocks.ok(sessionHash(r)) {
		writeError(w, http.StatusForbidden, "confirm_required", "confirm your password to open the host terminal")
		return
	}
	s.serveTerminal(w, r, terminalSession{
		open: func(ctx context.Context) (executor.Terminal, error) {
			return s.hostTerminal(ctx, cfg.User, uint(cols), uint(rows))
		},
		failed: "Cannot open a shell on the host.",
		idle:   cfg.idle(),
		admin:  true,
		audit: func(action string) {
			s.audit(r, "host.terminal."+action, "host", "", "host", "as "+cmp.Or(cfg.User, "root"))
		},
	})
}

type terminalSession struct {
	open   func(context.Context) (executor.Terminal, error)
	failed string              // shown in the browser when open fails
	audit  func(action string) // "open" and "close"
	idle   time.Duration       // closed after this long without input; 0: never
	admin  bool                // closed when the user stops being an admin (host shells)
}

// terminalIdleClose is the WebSocket close code of a shell closed for being idle.
const terminalIdleClose = 4001

// serveTerminal upgrades to a WebSocket and bridges it to the session's PTY.
func (s *Server) serveTerminal(w http.ResponseWriter, r *http.Request, session terminalSession) {
	if !websocket.IsWebSocketUpgrade(r) {
		writeError(w, http.StatusBadRequest, "invalid", "WebSocket upgrade required")
		return
	}
	ctx, cancel := s.streamContext(r)
	defer cancel()
	// Bound session lifetime; no detached WebSocket may outlive server shutdown.
	ctx, timeout := context.WithTimeout(ctx, 8*time.Hour)
	defer timeout()
	upgrader := websocket.Upgrader{CheckOrigin: terminalOrigin, HandshakeTimeout: 10 * time.Second, ReadBufferSize: 4096, WriteBufferSize: 16384}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	ws.SetReadLimit(64 * 1024)
	// A logout or expired browser session also closes an existing terminal.
	cookie, cookieErr := r.Cookie(brand.SessionCookie())
	if cookieErr == nil {
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					// A demoted admin loses an open host shell too, not only new ones.
					if u, err := s.accounts.SessionUser(ctx, cookie.Value); err != nil || (session.admin && !isAdmin(u)) {
						cancel()
						return
					}
				}
			}
		}()
	}
	go func() { <-ctx.Done(); ws.Close() }()
	pty, err := session.open(ctx)
	if err != nil {
		s.log.Warn("open terminal", "error", err)
		ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
		_ = ws.WriteJSON(map[string]string{"type": "error", "message": session.failed})
		return
	}
	defer pty.Close()
	session.audit("open")
	defer session.audit("close")
	go func() { <-ctx.Done(); pty.Close() }()
	if err := ws.WriteJSON(map[string]string{"type": "ready"}); err != nil {
		return
	}
	// What the user types counts as activity; output (a `tail -f` left open) does not.
	var lastInput atomic.Int64
	lastInput.Store(time.Now().UnixNano())
	if session.idle > 0 {
		go func() {
			ticker := time.NewTicker(min(session.idle/4, 15*time.Second))
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if time.Since(time.Unix(0, lastInput.Load())) >= session.idle {
						ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(terminalIdleClose, "idle"), time.Now().Add(time.Second))
						cancel()
						return
					}
				}
			}
		}()
	}
	// Limit unrendered browser output to eight frames (128 KiB).
	credits := make(chan struct{}, 8)
	for i := 0; i < cap(credits); i++ {
		credits <- struct{}{}
	}
	// Only this goroutine writes WebSocket data. Synchronous writes provide backpressure.
	go func() {
		defer cancel()
		buf := make([]byte, 16384)
		for {
			select {
			case <-credits:
			case <-ctx.Done():
				return
			}
			n, readErr := pty.Read(buf)
			if n > 0 {
				ws.SetWriteDeadline(time.Now().Add(15 * time.Second))
				if ws.WriteMessage(websocket.BinaryMessage, buf[:n]) != nil {
					return
				}
			}
			if readErr != nil {
				ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "Shell exited"), time.Now().Add(time.Second))
				return
			}
		}
	}()
	for {
		ws.SetReadDeadline(time.Now().Add(60 * time.Second))
		kind, payload, err := ws.ReadMessage()
		if err != nil {
			return
		}
		if kind == websocket.BinaryMessage {
			lastInput.Store(time.Now().UnixNano())
			if _, err := pty.Write(payload); err != nil {
				return
			}
			continue
		}
		var msg struct {
			Type string `json:"type"`
			Cols int    `json:"cols"`
			Rows int    `json:"rows"`
		}
		if json.Unmarshal(payload, &msg) != nil {
			return
		}
		switch msg.Type {
		case "ack":
			select {
			case credits <- struct{}{}:
			default:
			}
		case "resize":
			if !terminalSize(msg.Cols, msg.Rows) {
				return
			}
			resizeCtx, stop := context.WithTimeout(ctx, 5*time.Second)
			err := pty.Resize(resizeCtx, uint(msg.Cols), uint(msg.Rows))
			stop()
			if err != nil {
				return
			}
		case "ping": // Keeps otherwise-idle connections active through proxies.
		default:
			return
		}
	}
}

// hasPasskey says whether the signed-in user can confirm with a passkey from this origin.
func (s *Server) hasPasskey(r *http.Request) bool {
	origin, err := s.loginOrigin(r)
	if err != nil {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	keys, err := s.store.Passkeys(r.Context(), userFrom(r.Context()).ID, u.Hostname())
	return err == nil && len(keys) > 0
}

// handleBeginHostUnlockPasskey starts a passkey confirmation in place of the password.
func (s *Server) handleBeginHostUnlockPasskey(w http.ResponseWriter, r *http.Request) {
	s.beginPasskeyConfirmation(w, r, "passkey-host-unlock")
}

// handleFinishHostUnlockPasskey unlocks the host terminal as the password does.
func (s *Server) handleFinishHostUnlockPasskey(w http.ResponseWriter, r *http.Request) {
	if s.finishPasskeyConfirmation(w, r, "passkey-host-unlock") {
		s.hostUnlocks.grant(sessionHash(r))
		w.WriteHeader(http.StatusNoContent)
	}
}

// beginPasskeyConfirmation answers a challenge for the signed-in user's own passkeys,
// user verification required. The kind keeps one confirmation from standing for another.
func (s *Server) beginPasskeyConfirmation(w http.ResponseWriter, r *http.Request, kind string) {
	origin, err := s.loginOrigin(r)
	if err != nil {
		writeError(w, 422, "origin", err.Error())
		return
	}
	rp, err := passkeyRP(origin)
	if err != nil {
		s.internal(w, "passkey RP", err)
		return
	}
	u, err := s.passkeyUser(r.Context(), userFrom(r.Context()), rp.Config.RPID)
	if err != nil {
		s.internal(w, "passkey user", err)
		return
	}
	if len(u.Credentials) == 0 {
		writeError(w, 422, "no_passkey", "no passkey is registered for this account; confirm with your password")
		return
	}
	options, session, err := rp.BeginLogin(u, webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		s.internal(w, "passkey confirmation", err)
		return
	}
	if _, err = s.beginLoginFlow(w, loginFlow{Kind: kind, Origin: origin, UserID: u.ID, SessionHash: sessionHash(r), Session: *session}); err != nil {
		writeError(w, 429, "busy", err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, options)
}

// finishPasskeyConfirmation checks the assertion against the challenge of this user and
// this browser session. It reports whether the passkey confirmed; if not, it has answered.
func (s *Server) finishPasskeyConfirmation(w http.ResponseWriter, r *http.Request, kind string) bool {
	f, err := s.takeLoginFlow(w, r, kind)
	if err != nil {
		writeError(w, 400, "challenge", err.Error())
		return false
	}
	s.loginFlows.passkeyMu.Lock()
	defer s.loginFlows.passkeyMu.Unlock()
	rp, err := passkeyRP(f.Origin)
	if err != nil {
		s.internal(w, "passkey RP", err)
		return false
	}
	u, err := s.passkeyUser(r.Context(), userFrom(r.Context()), rp.Config.RPID)
	if err != nil {
		s.internal(w, "passkey user", err)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	cred, err := rp.FinishLogin(u, f.Session, r)
	if err != nil || cred.Authenticator.CloneWarning {
		writeError(w, 401, "invalid_credentials", "could not verify this passkey; use your password or try another passkey")
		return false
	}
	data, err := json.Marshal(cred)
	if err != nil {
		s.internal(w, "passkey credential", err)
		return false
	}
	if err = s.store.UsePasskey(r.Context(), u.ID, rp.Config.RPID, base64.RawURLEncoding.EncodeToString(cred.ID), data); err != nil {
		writeError(w, 401, "invalid_credentials", "this passkey is no longer registered")
		return false
	}
	return true
}

package api

import (
	"cmp"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"panel/internal/brand"
	"panel/internal/executor"
	"panel/internal/store"
)

type terminalDeployer interface {
	OpenTerminal(context.Context, string, string, uint, uint) (executor.Terminal, error)
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

type hostTerminalSettings struct {
	Enabled bool   `json:"enabled"`
	User    string `json:"user"` // empty: root
}

func (s *Server) hostTerminalSettings(ctx context.Context) (hostTerminalSettings, error) {
	off, _, err := s.store.GetSetting(ctx, store.SettingHostTermOff)
	if err != nil {
		return hostTerminalSettings{}, err
	}
	user, _, err := s.store.GetSetting(ctx, store.SettingHostTermUsr)
	return hostTerminalSettings{Enabled: off != "true", User: user}, err
}

func (s *Server) handleGetHostTerminalSettings(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.hostTerminalSettings(r.Context())
	if err != nil {
		s.internal(w, "host terminal settings", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": cfg.Enabled, "user": cfg.User,
		"available": s.hostTerminal != nil, "unlocked": s.hostUnlocks.ok(sessionHash(r)),
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
	detail := "off"
	if in.Enabled {
		detail = "on, as " + cmp.Or(in.User, "root")
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
		audit: func(action string) {
			s.audit(r, "host.terminal."+action, "host", "", "host", "as "+cmp.Or(cfg.User, "root"))
		},
	})
}

type terminalSession struct {
	open   func(context.Context) (executor.Terminal, error)
	failed string              // shown in the browser when open fails
	audit  func(action string) // "open" and "close"
}

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
					if _, err := s.accounts.SessionUser(ctx, cookie.Value); err != nil {
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
		ws.WriteJSON(map[string]string{"type": "error", "message": session.failed})
		return
	}
	defer pty.Close()
	session.audit("open")
	defer session.audit("close")
	go func() { <-ctx.Done(); pty.Close() }()
	if err := ws.WriteJSON(map[string]string{"type": "ready"}); err != nil {
		return
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

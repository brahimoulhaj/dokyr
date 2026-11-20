package api

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"panel/internal/executor"
)

func TestTerminalOriginAndDimensions(t *testing.T) {
	for _, tc := range []struct {
		origin string
		want   bool
	}{
		{"http://panel.test", true}, {"https://panel.test", true}, {"https://evil.test", false},
		{"", false}, {"null", false}, {"https://panel.test.evil.test", false}, {"https://user@panel.test", false},
	} {
		r := httptest.NewRequest("GET", "http://panel.test/api/apps/a/terminal", nil)
		r.Header.Set("Origin", tc.origin)
		if terminalOrigin(r) != tc.want {
			t.Errorf("origin %q", tc.origin)
		}
	}
	for _, size := range [][2]int{{0, 24}, {80, 0}, {501, 24}, {80, 301}, {-1, 24}} {
		if terminalSize(size[0], size[1]) {
			t.Errorf("accepted size %v", size)
		}
	}
	if !terminalSize(80, 24) {
		t.Fatal("normal size refused")
	}
}

func TestTerminalRequiresSession(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest("GET", "/api/apps/a/terminal?cols=80&rows=24&shell=/bin/sh", nil)
	w := httptest.NewRecorder()
	s.requireAuth(sessionOnly(s.handleTerminal))(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated terminal = %d", w.Code)
	}
	w = httptest.NewRecorder()
	r.Header.Set("Origin", "https://evil.test")
	s.handleTerminal(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin terminal = %d", w.Code)
	}
}

type terminalStub struct {
	Deployer
	conn  net.Conn
	sizes chan [2]uint
	binds []string // host paths of the running container
}

func (d *terminalStub) HostPaths(context.Context, string) ([]string, error) { return d.binds, nil }

func (d *terminalStub) OpenTerminal(context.Context, string, string, uint, uint) (executor.Terminal, error) {
	return &testPTY{Conn: d.conn, sizes: d.sizes}, nil
}

type testPTY struct {
	net.Conn
	sizes chan [2]uint
}

func (p *testPTY) Resize(_ context.Context, cols, rows uint) error {
	p.sizes <- [2]uint{cols, rows}
	return nil
}

func TestTerminalWebSocketBridge(t *testing.T) {
	e := newEnv(t)
	e.signIn()
	pid := e.must(201, "POST", "/api/projects", map[string]any{"name": "terminal"})["id"].(string)
	a := e.must(201, "POST", "/api/projects/"+pid+"/apps", map[string]any{"name": "shell", "image": "busybox", "port": 80})
	server, process := net.Pipe()
	defer process.Close()
	defer server.Close()
	sizes := make(chan [2]uint, 1)
	e.srv.deployer = &terminalStub{Deployer: e.deployer, conn: server, sizes: sizes}
	dialer := websocket.Dialer{Jar: e.client.Jar}
	ws, _, err := dialer.Dial("ws"+strings.TrimPrefix(e.url, "http")+"/api/apps/"+a["id"].(string)+"/terminal?cols=80&rows=24&shell=/bin/sh", http.Header{"Origin": []string{e.url}})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, ready, err := ws.ReadMessage()
	if err != nil || !strings.Contains(string(ready), "ready") {
		t.Fatalf("ready: %s %v", ready, err)
	}
	go process.Write([]byte("\x1b[32mhello\x1b[0m\r\n"))
	kind, output, err := ws.ReadMessage()
	if err != nil || kind != websocket.BinaryMessage || string(output) != "\x1b[32mhello\x1b[0m\r\n" {
		t.Fatalf("output: %q %v", output, err)
	}
	if err := ws.WriteMessage(websocket.BinaryMessage, []byte("pwd\r")); err != nil {
		t.Fatal(err)
	}
	process.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 32)
	n, err := process.Read(buf)
	if err != nil || string(buf[:n]) != "pwd\r" {
		t.Fatalf("input: %q %v", buf[:n], err)
	}
	ws.WriteJSON(map[string]any{"type": "resize", "cols": 120, "rows": 40})
	select {
	case size := <-sizes:
		if size != [2]uint{120, 40} {
			t.Fatal(size)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("resize not delivered")
	}
	ws.Close()
	process.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := process.Read(buf); err == nil {
		t.Fatal("PTY not closed on disconnect")
	}
}

func TestHostTerminal(t *testing.T) {
	e := newEnv(t)
	e.signIn()
	url := "ws" + strings.TrimPrefix(e.url, "http") + "/api/host/terminal?cols=80&rows=24"
	dialer := websocket.Dialer{Jar: e.client.Jar}
	origin := http.Header{"Origin": []string{e.url}}

	// Not configured: refused before any upgrade.
	if _, res, err := dialer.Dial(url, origin); err == nil || res == nil || res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured host terminal: %v %v", res, err)
	}

	server, process := net.Pipe()
	defer process.Close()
	defer server.Close()
	e.srv.SetHostTerminal(func(_ context.Context, user string, cols, rows uint) (executor.Terminal, error) {
		if cols != 80 || rows != 24 || user != "deploy" {
			t.Errorf("size %dx%d as %q", cols, rows, user)
		}
		return &testPTY{Conn: server, sizes: make(chan [2]uint, 1)}, nil
	})
	if _, res, err := (&websocket.Dialer{}).Dial(url, origin); err == nil || res == nil || res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("signed-out host terminal: %v %v", res, err)
	}
	refused := func(why string) {
		t.Helper()
		if _, res, err := dialer.Dial(url, origin); err == nil || res == nil || res.StatusCode != http.StatusForbidden {
			t.Fatalf("%s host terminal: %v %v", why, res, err)
		}
	}

	// The session alone is not enough: the password is asked again.
	refused("locked")
	e.must(401, "POST", "/api/host/terminal/unlock", map[string]any{"password": "wrong"})
	refused("wrong password")
	e.must(204, "POST", "/api/host/terminal/unlock", map[string]any{"password": "s3cret-pass"})
	if got := e.must(200, "GET", "/api/host/terminal/settings", nil); got["unlocked"] != true || got["enabled"] != true || got["user"] != "" {
		t.Fatalf("settings = %v", got)
	}

	// Switched off, nobody opens it, unlocked or not.
	e.must(422, "PUT", "/api/host/terminal/settings", map[string]any{"enabled": true, "user": "bad name; id"})
	e.must(200, "PUT", "/api/host/terminal/settings", map[string]any{"enabled": false, "user": "deploy"})
	refused("switched off")
	if got := e.must(200, "PUT", "/api/host/terminal/settings", map[string]any{"enabled": true, "user": "deploy"}); got["user"] != "deploy" {
		t.Fatalf("settings = %v", got)
	}
	ws, _, err := dialer.Dial(url, origin)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, ready, err := ws.ReadMessage(); err != nil || !strings.Contains(string(ready), "ready") {
		t.Fatalf("ready: %s %v", ready, err)
	}
	go process.Write([]byte("root@host:~# "))
	if _, output, err := ws.ReadMessage(); err != nil || string(output) != "root@host:~# " {
		t.Fatalf("output: %q %v", output, err)
	}
}

func TestHostTerminalIdle(t *testing.T) {
	hostIdleUnit = 200 * time.Millisecond
	defer func() { hostIdleUnit = time.Minute }()
	e := newEnv(t)
	e.signIn()
	if got := e.must(200, "GET", "/api/host/terminal/settings", nil); got["idle_minutes"] != float64(30) {
		t.Fatalf("default idle time = %v", got["idle_minutes"])
	}
	for _, bad := range []int{-1, 481} {
		e.must(422, "PUT", "/api/host/terminal/settings", map[string]any{"enabled": true, "user": "", "idle_minutes": bad})
	}
	// A client that does not send the field keeps what is saved.
	e.must(200, "PUT", "/api/host/terminal/settings", map[string]any{"enabled": true, "user": "", "idle_minutes": 2})
	if got := e.must(200, "PUT", "/api/host/terminal/settings", map[string]any{"enabled": true, "user": ""}); got["idle_minutes"] != float64(2) {
		t.Fatalf("idle time after a save without it = %v", got["idle_minutes"])
	}
	e.must(204, "POST", "/api/host/terminal/unlock", map[string]any{"password": "s3cret-pass"})

	open := func() (*websocket.Conn, net.Conn) {
		t.Helper()
		server, process := net.Pipe()
		t.Cleanup(func() { process.Close(); server.Close() })
		e.srv.SetHostTerminal(func(context.Context, string, uint, uint) (executor.Terminal, error) {
			return &testPTY{Conn: server, sizes: make(chan [2]uint, 1)}, nil
		})
		ws, _, err := (&websocket.Dialer{Jar: e.client.Jar}).Dial("ws"+strings.TrimPrefix(e.url, "http")+"/api/host/terminal?cols=80&rows=24", http.Header{"Origin": []string{e.url}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { ws.Close() })
		ws.SetReadDeadline(time.Now().Add(5 * time.Second))
		if _, ready, err := ws.ReadMessage(); err != nil || !strings.Contains(string(ready), "ready") {
			t.Fatalf("ready: %s %v", ready, err)
		}
		return ws, process
	}

	// Two "minutes" (400 ms here) without input close the shell, whatever it prints.
	ws, process := open()
	go func() {
		for range 20 {
			if _, err := process.Write([]byte(".")); err != nil {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	start := time.Now()
	for {
		_, _, err := ws.ReadMessage()
		if err == nil {
			ws.WriteJSON(map[string]string{"type": "ack"})
			continue
		}
		if !websocket.IsCloseError(err, terminalIdleClose) {
			t.Fatalf("idle shell ended with %v, want close code %d", err, terminalIdleClose)
		}
		break
	}
	if d := time.Since(start); d < 350*time.Millisecond {
		t.Fatalf("closed after %v, before the idle time", d)
	}
	process.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := process.Read(make([]byte, 1)); err == nil {
		t.Fatal("PTY not closed with the idle shell")
	}

	// Typing keeps it open past the idle time.
	ws, process = open()
	go io.Copy(io.Discard, process)
	for range 8 {
		if err := ws.WriteMessage(websocket.BinaryMessage, []byte("x")); err != nil {
			t.Fatalf("shell closed while typing: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// 0 never closes it.
	e.must(200, "PUT", "/api/host/terminal/settings", map[string]any{"enabled": true, "user": "", "idle_minutes": 0})
	ws, process = open()
	go io.Copy(io.Discard, process)
	time.Sleep(600 * time.Millisecond)
	if err := ws.WriteJSON(map[string]string{"type": "ping"}); err != nil {
		t.Fatalf("shell with no idle time closed: %v", err)
	}
	ws.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, _, err := ws.ReadMessage(); websocket.IsCloseError(err, terminalIdleClose) {
		t.Fatal("shell with no idle time closed as idle")
	}
}

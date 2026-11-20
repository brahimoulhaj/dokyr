package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"panel/internal/brand"
)

// loginWait is how long the CLI waits for the approval; the panel's code lives 5 minutes.
const loginWait = 5 * time.Minute

// runLogin signs in through the browser: the user signs in on the panel as usual and
// approves this device there, and the CLI never sees a password. See internal/api/cli_login.go.
func runLogin(ctx context.Context, args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(errOut)
	insecure := fs.Bool("insecure", false, "sign in to a panel served over plain HTTP")
	noOpen := fs.Bool("no-open", false, "print the link instead of opening the browser")
	// Flags may come before or after the URL.
	var target string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return err
		}
		args = fs.Args()
		if len(args) > 0 {
			if target != "" {
				return errors.New("login takes one panel URL")
			}
			target, args = args[0], args[1:]
		}
	}
	if target == "" {
		return fmt.Errorf("usage: %s login https://your-panel [--insecure] [--no-open]", brand.Slug)
	}
	base, err := panelURL(target, *insecure)
	if err != nil {
		return err
	}
	if base.Scheme == "http" && !isLoopback(base.Hostname()) {
		fmt.Fprintln(errOut, "warning: this panel is served over plain HTTP; the token travels unencrypted")
	}

	client := newClient(base.String(), "")
	var meta struct {
		Slug          string `json:"slug"`
		Name          string `json:"name"`
		SetupRequired bool   `json:"setup_required"`
	}
	if err := client.do(ctx, http.MethodGet, "/api/meta", nil, &meta); err != nil {
		return err
	}
	if meta.Slug != brand.Slug {
		return fmt.Errorf("%s is not a %s panel", base, brand.Name)
	}
	if meta.SetupRequired {
		return fmt.Errorf("%s has no owner yet; finish its setup in the browser first", base)
	}

	// What binds the answer to this run: the verifier never leaves this process until
	// the exchange, and the state tells our redirect from anyone else's request.
	verifier, state := randomToken(), randomToken()
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	shortCode := randomCode()
	device, _ := os.Hostname()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("cannot listen on a local port for the sign-in: %w", err)
	}
	defer ln.Close()
	result := make(chan callback, 1)
	srv := &http.Server{Handler: callbackHandler(state, meta.Name, result), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	link := *base
	link.Path = "/cli/authorize"
	link.RawQuery = url.Values{
		"port":      {strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)},
		"state":     {state},
		"challenge": {challenge},
		"device":    {device},
		"code":      {shortCode},
	}.Encode()

	fmt.Fprintf(out, "Code: %s\n\n", shortCode)
	if *noOpen || openBrowser(link.String()) != nil {
		fmt.Fprintf(out, "Open this link in a browser on this machine, check the code and approve:\n\n  %s\n\n", link.String())
	} else {
		fmt.Fprintf(out, "Your browser opens %s. Check the code there and approve.\nIf nothing opened, use this link:\n\n  %s\n\n", base.Host, link.String())
	}
	fmt.Fprintln(out, "Waiting for the approval (Ctrl+C to give up)…")

	var cb callback
	select {
	case cb = <-result:
	case <-ctx.Done():
		return errors.New("sign-in cancelled")
	case <-time.After(loginWait):
		return errors.New("no approval within 5 minutes; run the command again")
	}
	if cb.err != "" {
		if cb.err == "access_denied" {
			return errors.New("the sign-in was denied in the browser")
		}
		return fmt.Errorf("the panel refused the sign-in (%s)", cb.err)
	}

	var granted struct {
		Token string `json:"token"`
		User  struct {
			Email string `json:"email"`
			Role  string `json:"role"`
		} `json:"user"`
	}
	if err := client.do(ctx, http.MethodPost, "/api/cli/token", map[string]string{"code": cb.code, "verifier": verifier}, &granted); err != nil {
		return err
	}
	if granted.Token == "" {
		return errors.New("the panel answered without a token")
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	cfg.Contexts[base.Host] = panelContext{URL: base.String(), User: granted.User.Email, Role: granted.User.Role, Token: granted.Token}
	cfg.Current = base.Host
	if err := saveConfig(cfg); err != nil {
		return fmt.Errorf("signed in, but the sign-in could not be saved: %w", err)
	}
	fmt.Fprintf(out, "Signed in to %s as %s (%s).\n", base.Host, granted.User.Email, granted.User.Role)
	return nil
}

// panelURL is the panel's origin from what the user typed. https is assumed; plain HTTP
// is for a panel on this machine, or asked for with --insecure.
func panelURL(raw string, insecure bool) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil {
		return nil, fmt.Errorf("%q is not a panel URL; give it as https://panel.example.com", raw)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !insecure && !isLoopback(u.Hostname()) {
			return nil, fmt.Errorf("%s is plain HTTP: the token would travel unencrypted. Use https, or add --insecure to go on", u.Host)
		}
	default:
		return nil, fmt.Errorf("%q is not a panel URL; give it as https://panel.example.com", raw)
	}
	return &url.URL{Scheme: u.Scheme, Host: u.Host}, nil
}

func isLoopback(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// callback is what the browser brought back: a code, or why there is none.
type callback struct{ code, err string }

// callbackHandler answers the panel's redirect. Only a request with this run's state
// counts; anything else on the port is refused and the wait goes on.
func callbackHandler(state, product string, result chan<- callback) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
		q := r.URL.Query()
		if r.Method != http.MethodGet || r.URL.Path != "/callback" ||
			subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1 {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, callbackPage(product, "Not a sign-in of this terminal", "Run the login command again."))
			return
		}
		cb := callback{code: q.Get("code"), err: q.Get("error")}
		if cb.code == "" && cb.err == "" {
			cb.err = "no code"
		}
		select {
		case result <- cb:
		default: // a second visit of the same link: the first one counts
		}
		if cb.err != "" {
			fmt.Fprint(w, callbackPage(product, "Sign-in denied", "Nothing was granted. You can close this tab."))
			return
		}
		fmt.Fprint(w, callbackPage(product, "Signed in", "You can close this tab and go back to your terminal."))
	})
}

func callbackPage(product, title, body string) string {
	return `<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">` +
		`<title>` + html.EscapeString(product) + ` CLI</title>` +
		`<style>body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;font:15px/1.5 ui-monospace,Menlo,monospace;background:#0b0b0c;color:#e8e8e8}` +
		`main{max-width:22rem;padding:1.5rem;border:1px solid #333}h1{margin:0 0 .5rem;font-size:1rem;text-transform:uppercase}p{margin:0;color:#9a9a9a}</style>` +
		`<main><h1>` + html.EscapeString(title) + `</h1><p>` + html.EscapeString(body) + `</p></main></html>`
}

func openBrowser(link string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", link)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", link)
	default:
		cmd = exec.Command("xdg-open", link)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// randomToken is 256 random bits, URL-safe.
func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// randomCode is the short code shown in the terminal and on the approval page, so the
// user can tell their own sign-in from a link someone sent them. Letters and digits
// that read alike are left out.
func randomCode() string {
	const alphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b[:4]) + "-" + string(b[4:])
}

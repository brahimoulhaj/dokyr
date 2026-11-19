// Package notify sends alerts to notification channels (email, Slack, Discord,
// Telegram, a generic webhook) when events matching an alert rule are recorded.
package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var ErrInvalid = errors.New("invalid channel settings")

// Message is one alert, rendered by each channel in its own format.
type Message struct {
	Title string
	Text  string
	Level string // "info", "success", "error"
	URL   string // link into the panel; empty when no panel domain is set
	At    time.Time
}

// Kind describes a channel type: its fields, which of them are secret, which are required.
type Kind struct {
	ID       string   `json:"id"`
	Fields   []string `json:"fields"`
	Secret   []string `json:"secret"`
	Required []string `json:"required"`
}

var Kinds = []Kind{
	{ID: "email", Fields: []string{"host", "port", "security", "username", "password", "from", "to"}, Secret: []string{"password"}, Required: []string{"host", "port", "from", "to"}},
	{ID: "slack", Fields: []string{"webhook_url"}, Secret: []string{"webhook_url"}, Required: []string{"webhook_url"}},
	{ID: "discord", Fields: []string{"webhook_url"}, Secret: []string{"webhook_url"}, Required: []string{"webhook_url"}},
	{ID: "telegram", Fields: []string{"bot_token", "chat_id"}, Secret: []string{"bot_token"}, Required: []string{"bot_token", "chat_id"}},
	{ID: "webhook", Fields: []string{"url", "secret"}, Secret: []string{"secret"}, Required: []string{"url"}},
}

func kind(id string) (Kind, bool) {
	for _, k := range Kinds {
		if k.ID == id {
			return k, true
		}
	}
	return Kind{}, false
}

// Merge returns a channel's new settings: only the kind's fields, trimmed, with an
// empty secret meaning "keep the saved one". It checks the result.
func Merge(kindID string, saved, in map[string]string) (map[string]string, error) {
	k, ok := kind(kindID)
	if !ok {
		return nil, fmt.Errorf("%w: unknown channel type", ErrInvalid)
	}
	out := map[string]string{}
	for _, f := range k.Fields {
		v := strings.TrimSpace(in[f])
		if v == "" && contains(k.Secret, f) {
			v = saved[f]
		}
		if v != "" {
			out[f] = v
		}
	}
	// A saved password belongs to the server it was entered for: pointing the channel at
	// another host must not send it there.
	if kindID == "email" && saved["password"] != "" && strings.TrimSpace(in["password"]) == "" && out["host"] != saved["host"] {
		return nil, fmt.Errorf("%w: enter the password again to change the mail server", ErrInvalid)
	}
	for _, f := range k.Required {
		if out[f] == "" {
			return nil, fmt.Errorf("%w: %s is required", ErrInvalid, strings.ReplaceAll(f, "_", " "))
		}
	}
	return out, check(kindID, out)
}

// Public is what the browser may see: the non-secret fields, and which secrets are set.
func Public(kindID string, cfg map[string]string) map[string]any {
	k, _ := kind(kindID)
	out := map[string]any{}
	for _, f := range k.Fields {
		if contains(k.Secret, f) {
			out[f+"_set"] = cfg[f] != ""
		} else if cfg[f] != "" {
			out[f] = cfg[f]
		}
	}
	return out
}

func check(kindID string, c map[string]string) error {
	httpURL := func(v string) error {
		u, err := url.Parse(v)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return fmt.Errorf("%w: enter a full http(s) URL", ErrInvalid)
		}
		return nil
	}
	switch kindID {
	case "slack", "discord":
		return httpURL(c["webhook_url"])
	case "webhook":
		return httpURL(c["url"])
	case "telegram":
		if strings.ContainsAny(c["bot_token"], "/ ?#") {
			return fmt.Errorf("%w: the bot token looks wrong", ErrInvalid)
		}
	case "email":
		if p, err := strconv.Atoi(c["port"]); err != nil || p < 1 || p > 65535 {
			return fmt.Errorf("%w: port must be 1–65535", ErrInvalid)
		}
		switch c["security"] {
		case "", "starttls", "tls", "none":
		default:
			return fmt.Errorf("%w: security must be starttls, tls or none", ErrInvalid)
		}
		if _, err := mail.ParseAddress(c["from"]); err != nil {
			return fmt.Errorf("%w: the sender address is not valid", ErrInvalid)
		}
		if _, err := mail.ParseAddressList(c["to"]); err != nil {
			return fmt.Errorf("%w: the recipient addresses are not valid", ErrInvalid)
		}
		if strings.ContainsAny(c["host"], "/ :") {
			return fmt.Errorf("%w: enter the SMTP host name without a port", ErrInvalid)
		}
	}
	return nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Sender delivers messages; its fields are replaceable in tests.
type Sender struct {
	HTTP *http.Client
	// TelegramAPI is the Bot API base URL.
	TelegramAPI string
	// SendMail sends one email; nil uses SMTP.
	SendMail func(ctx context.Context, cfg map[string]string, from string, to []string, raw []byte) error
}

func NewSender() *Sender {
	// A webhook answers where it is called: a redirect is reported, not followed, so a
	// channel cannot be bounced to an address nobody entered.
	noRedirect := func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Sender{HTTP: &http.Client{Timeout: 15 * time.Second, CheckRedirect: noRedirect}, TelegramAPI: "https://api.telegram.org"}
}

func (s *Sender) Send(ctx context.Context, kindID string, cfg map[string]string, m Message) error {
	text := m.Text
	if m.URL != "" {
		text += "\n" + m.URL
	}
	switch kindID {
	case "slack":
		return s.postJSON(ctx, cfg["webhook_url"], map[string]any{"text": "*" + m.Title + "*\n" + text}, nil)
	case "discord":
		color := map[string]int{"error": 0xe5534b, "success": 0x9ece6a}[m.Level]
		if color == 0 {
			color = 0x7f8798
		}
		embed := map[string]any{"title": m.Title, "description": m.Text, "color": color, "timestamp": m.At.UTC().Format(time.RFC3339)}
		if m.URL != "" {
			embed["url"] = m.URL
		}
		return s.postJSON(ctx, cfg["webhook_url"], map[string]any{"embeds": []any{embed}}, nil)
	case "telegram":
		endpoint := strings.TrimRight(s.TelegramAPI, "/") + "/bot" + cfg["bot_token"] + "/sendMessage"
		return s.postJSON(ctx, endpoint, map[string]any{"chat_id": cfg["chat_id"], "text": m.Title + "\n" + text, "disable_web_page_preview": true}, nil)
	case "webhook":
		body := map[string]any{"title": m.Title, "text": m.Text, "level": m.Level, "url": m.URL, "at": m.At.UTC().Format(time.RFC3339)}
		return s.postJSON(ctx, cfg["url"], body, func(raw []byte, h http.Header) {
			if cfg["secret"] != "" {
				mac := hmac.New(sha256.New, []byte(cfg["secret"]))
				mac.Write(raw)
				h.Set("X-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
			}
		})
	case "email":
		return s.email(ctx, cfg, m, text)
	}
	return fmt.Errorf("%w: unknown channel type", ErrInvalid)
}

func (s *Sender) postJSON(ctx context.Context, endpoint string, body any, sign func([]byte, http.Header)) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return errors.New("the URL is not valid")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "panel-notifications")
	if sign != nil {
		sign(raw, req.Header)
	}
	res, err := s.HTTP.Do(req)
	if err != nil {
		// Never echo the URL: webhook URLs and bot tokens are secrets.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("the request failed: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(res.Body, 200))
		detail := strings.TrimSpace(string(snippet))
		if detail != "" {
			return fmt.Errorf("the service answered %d: %s", res.StatusCode, detail)
		}
		return fmt.Errorf("the service answered %d", res.StatusCode)
	}
	return nil
}

func (s *Sender) email(ctx context.Context, cfg map[string]string, m Message, text string) error {
	from, err := mail.ParseAddress(cfg["from"])
	if err != nil {
		return err
	}
	list, err := mail.ParseAddressList(cfg["to"])
	if err != nil {
		return err
	}
	to := make([]string, 0, len(list))
	names := make([]string, 0, len(list))
	for _, a := range list {
		to = append(to, a.Address)
		names = append(names, a.String())
	}
	subject := strings.NewReplacer("\r", " ", "\n", " ").Replace(m.Title)
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n",
		from.String(), strings.Join(names, ", "), mime.QEncoding.Encode("utf-8", subject), m.At.Format(time.RFC1123Z))
	b.WriteString(strings.ReplaceAll(text, "\n", "\r\n"))
	b.WriteString("\r\n")
	send := s.SendMail
	if send == nil {
		send = sendSMTP
	}
	return send(ctx, cfg, from.Address, to, []byte(b.String()))
}

// sendSMTP sends over implicit TLS ("tls", usually port 465), STARTTLS (the
// default; required, never skipped) or plain ("none", local relays only).
func sendSMTP(ctx context.Context, cfg map[string]string, from string, to []string, raw []byte) error {
	host := cfg["host"]
	addr := net.JoinHostPort(host, cfg["port"])
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	var conn net.Conn
	var err error
	if cfg["security"] == "tls" {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("could not connect to %s: %v", addr, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("SMTP greeting failed: %v", err)
	}
	defer c.Close()
	if cfg["security"] == "" || cfg["security"] == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("the server does not offer STARTTLS; choose TLS or none")
		}
		if err = c.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("STARTTLS failed: %v", err)
		}
	}
	if cfg["username"] != "" {
		if err = c.Auth(smtp.PlainAuth("", cfg["username"], cfg["password"], host)); err != nil {
			return fmt.Errorf("SMTP login failed: %v", err)
		}
	}
	if err = c.Mail(from); err != nil {
		return fmt.Errorf("sender refused: %v", err)
	}
	for _, r := range to {
		if err = c.Rcpt(r); err != nil {
			return fmt.Errorf("recipient %s refused: %v", r, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err = w.Write(raw); err != nil {
		return err
	}
	if err = w.Close(); err != nil {
		return fmt.Errorf("the message was refused: %v", err)
	}
	return c.Quit()
}

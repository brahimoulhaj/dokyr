package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"panel/internal/hostmetrics"
	"panel/internal/store"
)

type received struct {
	mu   sync.Mutex
	reqs []*http.Request
	body [][]byte
}

func (r *received) server(t *testing.T, status int) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		raw, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.reqs, r.body = append(r.reqs, req), append(r.body, raw)
		r.mu.Unlock()
		w.WriteHeader(status)
		_, _ = io.WriteString(w, "nope")
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (r *received) count() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.body) }

func TestMergeAndPublic(t *testing.T) {
	if _, err := Merge("slack", nil, map[string]string{"webhook_url": "ftp://x"}); err == nil {
		t.Fatal("accepted a non-http URL")
	}
	if _, err := Merge("telegram", nil, map[string]string{"bot_token": "abc"}); err == nil {
		t.Fatal("accepted a missing chat id")
	}
	if _, err := Merge("email", nil, map[string]string{"host": "smtp.example.com", "port": "587", "from": "a@example.com", "to": "not an address"}); err == nil {
		t.Fatal("accepted a bad recipient")
	}
	saved := map[string]string{"host": "smtp.example.com", "port": "587", "from": "a@example.com", "to": "b@example.com", "username": "u", "password": "secret"}
	cfg, err := Merge("email", saved, map[string]string{"host": " smtp.example.com ", "port": "465", "security": "tls", "from": "a@example.com", "to": "b@example.com, c@example.com", "username": "u", "extra": "x"})
	if err != nil || cfg["password"] != "secret" || cfg["port"] != "465" || cfg["extra"] != "" || cfg["host"] != "smtp.example.com" {
		t.Fatalf("merge kept %v %v", cfg, err)
	}
	// The saved password does not follow the channel to another mail server.
	moved := map[string]string{"host": "smtp.evil.example", "port": "587", "from": "a@example.com", "to": "b@example.com", "username": "u"}
	if _, err := Merge("email", saved, moved); err == nil {
		t.Fatal("kept the saved password for another mail server")
	}
	moved["password"] = "new"
	if got, err := Merge("email", saved, moved); err != nil || got["password"] != "new" {
		t.Fatalf("new server with its password: %v %v", got, err)
	}
	pub := Public("email", cfg)
	if pub["password"] != nil || pub["password_set"] != true || pub["host"] != "smtp.example.com" {
		t.Fatalf("public %v", pub)
	}
	if p := Public("slack", map[string]string{"webhook_url": "https://hooks.slack.com/x"}); p["webhook_url"] != nil || p["webhook_url_set"] != true {
		t.Fatalf("slack URL is a secret: %v", p)
	}
}

func TestSenders(t *testing.T) {
	ctx := context.Background()
	m := Message{Title: "Deployed web", Text: "ok", Level: "success", URL: "https://panel.example.com/x", At: time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)}
	var r received
	srv := r.server(t, 200)
	s := NewSender()
	s.TelegramAPI = srv.URL
	for _, c := range []struct {
		kind  string
		cfg   map[string]string
		check func(*http.Request, map[string]any)
	}{
		{"slack", map[string]string{"webhook_url": srv.URL + "/slack"}, func(_ *http.Request, b map[string]any) {
			if !strings.Contains(b["text"].(string), "*Deployed web*") || !strings.Contains(b["text"].(string), m.URL) {
				t.Fatalf("slack %v", b)
			}
		}},
		{"discord", map[string]string{"webhook_url": srv.URL + "/discord"}, func(_ *http.Request, b map[string]any) {
			e := b["embeds"].([]any)[0].(map[string]any)
			if e["title"] != "Deployed web" || e["url"] != m.URL || e["color"].(float64) != 0x9ece6a {
				t.Fatalf("discord %v", b)
			}
		}},
		{"telegram", map[string]string{"bot_token": "123:abc", "chat_id": "-42"}, func(req *http.Request, b map[string]any) {
			if req.URL.Path != "/bot123:abc/sendMessage" || b["chat_id"] != "-42" {
				t.Fatalf("telegram %s %v", req.URL.Path, b)
			}
		}},
		{"webhook", map[string]string{"url": srv.URL + "/hook", "secret": "s3cret"}, func(req *http.Request, b map[string]any) {
			if b["level"] != "success" || b["at"] != "2026-11-16T12:00:00Z" {
				t.Fatalf("webhook %v", b)
			}
		}},
	} {
		if err := s.Send(ctx, c.kind, c.cfg, m); err != nil {
			t.Fatal(c.kind, err)
		}
		n := r.count() - 1
		var body map[string]any
		if err := json.Unmarshal(r.body[n], &body); err != nil {
			t.Fatal(err)
		}
		c.check(r.reqs[n], body)
	}
	mac := hmac.New(sha256.New, []byte("s3cret"))
	mac.Write(r.body[3])
	if got := r.reqs[3].Header.Get("X-Signature-256"); got != "sha256="+hex.EncodeToString(mac.Sum(nil)) {
		t.Fatal("webhook signature", got)
	}

	var mail []byte
	var rcpt []string
	s.SendMail = func(_ context.Context, cfg map[string]string, from string, to []string, raw []byte) error {
		mail, rcpt = raw, to
		return nil
	}
	m.Title = "Déploiement réussi"
	if err := s.Send(ctx, "email", map[string]string{"from": "Panel <panel@example.com>", "to": "a@example.com, B <b@example.com>"}, m); err != nil {
		t.Fatal(err)
	}
	if strings.Join(rcpt, ",") != "a@example.com,b@example.com" || !strings.Contains(string(mail), "Subject: =?utf-8?q?") || !strings.Contains(string(mail), "\r\n\r\nok\r\nhttps://panel.example.com/x") {
		t.Fatalf("mail to %v:\n%s", rcpt, mail)
	}

	// Errors never include the URL (webhook URLs and bot tokens are secrets).
	bad := (&received{}).server(t, 500)
	s.TelegramAPI = bad.URL
	err := s.Send(ctx, "telegram", map[string]string{"bot_token": "123:secret", "chat_id": "1"}, m)
	if err == nil || strings.Contains(err.Error(), "secret") || !strings.Contains(err.Error(), "500") {
		t.Fatal("error", err)
	}
	s.TelegramAPI = "http://127.0.0.1:1"
	if err = s.Send(ctx, "telegram", map[string]string{"bot_token": "123:secret", "chat_id": "1"}, m); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("connection error leaks the token:", err)
	}
}

func TestDispatch(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if _, err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var good, failing received
	okSrv, badSrv := good.server(t, 200), failing.server(t, 500)
	n := New(ctx, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	st.SetEventHook(n.Handle)
	ch, _ := st.CreateNotificationChannel(ctx, "ops", "webhook", map[string]string{"url": okSrv.URL})
	broken, _ := st.CreateNotificationChannel(ctx, "broken", "slack", map[string]string{"webhook_url": badSrv.URL})
	for _, r := range []store.AlertRule{
		{Event: "deployment.failed", ChannelID: ch.ID, Enabled: true},
		{Event: "deployment.failed", TargetID: "app1", ChannelID: ch.ID, Enabled: true}, // overlaps: still one message
		{Event: "deployment.succeeded", TargetID: "app2", ChannelID: ch.ID, Enabled: true},
		{Event: "backup.failed", ChannelID: broken.ID, Enabled: true},
	} {
		if _, err := st.CreateAlertRule(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	_ = st.SetSetting(ctx, store.SettingPanelDomain, "panel.example.com")
	record := func(action, targetType, target string) {
		t.Helper()
		if err := st.AddEvent(ctx, store.Event{Actor: "system", Action: action, TargetType: targetType, TargetID: target, TargetName: target, Detail: "boom"}); err != nil {
			t.Fatal(err)
		}
		n.Wait()
	}
	record("deployment.rolled_back", "app", "app1")
	if good.count() != 1 {
		t.Fatalf("rolled back deploy sent %d messages", good.count())
	}
	var body map[string]any
	_ = json.Unmarshal(good.body[0], &body)
	if body["level"] != "error" || !strings.Contains(body["text"].(string), "previous version is serving again") || !strings.Contains(body["text"].(string), "Boom") {
		t.Fatalf("message %v", body)
	}
	record("deployment.succeeded", "app", "app1") // rule is for app2 only
	record("app.create", "app", "app1")           // not an alert event
	if good.count() != 1 {
		t.Fatalf("unmatched events sent: %d", good.count())
	}
	record("deployment.succeeded", "app", "app2")
	if good.count() != 2 {
		t.Fatal("targeted rule did not send")
	}
	record("backup.failed", "database", "db1")
	if c, _ := st.GetNotificationChannel(ctx, broken.ID); !strings.Contains(c.LastError, "500") || c.LastSentAt != nil {
		t.Fatalf("failure not recorded: %+v", c)
	}
	if c, _ := st.GetNotificationChannel(ctx, ch.ID); c.LastSentAt == nil || c.LastError != "" {
		t.Fatalf("success not recorded: %+v", c)
	}
	// Disabled rules are skipped; deleting a channel removes its rules.
	rules, _ := st.ListAlertRules(ctx)
	for _, r := range rules {
		_ = st.SetAlertRuleEnabled(ctx, r.ID, false)
	}
	record("deployment.succeeded", "app", "app2")
	if good.count() != 2 {
		t.Fatal("disabled rule sent")
	}
	if err := n.Test(ctx, ch.ID); err != nil || good.count() != 3 {
		t.Fatal("test send", err)
	}
	_ = st.DeleteNotificationChannel(ctx, ch.ID)
	if rules, _ = st.ListAlertRules(ctx); len(rules) != 1 {
		t.Fatalf("rules left after channel removal: %+v", rules)
	}

	// Disk: fires once past 90%, re-arms under 85%.
	_, _ = st.CreateAlertRule(ctx, store.AlertRule{Event: "disk.high", ChannelID: broken.ID, Enabled: true})
	disk := func(p float64) hostmetrics.Snapshot {
		return hostmetrics.Snapshot{Hostname: "vps", Disk: &hostmetrics.Disk{Memory: hostmetrics.Memory{Total: 100 << 30, Available: uint64((100 - p)) << 30, Percent: p}, Path: "/data"}}
	}
	fired := false
	for _, p := range []float64{80, 91, 95, 88, 84, 92} {
		fired = n.checkDisk(ctx, disk(p), fired)
	}
	n.Wait()
	events, _ := st.ListEvents(ctx, 50)
	high := 0
	for _, e := range events {
		if e.Action == "disk.high" {
			high++
		}
	}
	if high != 2 || failing.count() != 3 { // backup + two disk alerts
		t.Fatalf("disk alerts %d, sent %d", high, failing.count())
	}
}

func TestUsageRules(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if _, err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var got received
	srv := got.server(t, 200)
	n := New(ctx, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	st.SetEventHook(n.Handle)
	ch, _ := st.CreateNotificationChannel(ctx, "ops", "webhook", map[string]string{"url": srv.URL})
	p, _ := st.CreateProject(ctx, "shop", "")
	app, _ := st.CreateApp(ctx, p.ID, store.AppInput{Name: "web", Image: "nginx", Port: 80})
	other, _ := st.CreateApp(ctx, p.ID, store.AppInput{Name: "api", Image: "nginx", Port: 80})
	cpu, _ := st.CreateAlertRule(ctx, store.AlertRule{Event: UsageCPU, TargetID: app.ID, ChannelID: ch.ID, Enabled: true, Threshold: 1, Duration: 300})
	if _, err := st.CreateAlertRule(ctx, store.AlertRule{Event: UsageMemory, ChannelID: ch.ID, Enabled: true, Threshold: 90, Duration: 120}); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	// sample writes 10-second samples from..to (seconds after base) for an app.
	sample := func(appID string, from, to int, cpu float64, memory int64) {
		for s := from; s < to; s += 10 {
			_ = st.AddMetricSamples(ctx, base.Add(time.Duration(s)*time.Second).Unix(), []store.MetricSample{{Kind: "app", ResourceID: appID, CPU: cpu, Memory: memory, MemoryLimit: 1000}})
		}
	}
	bodies := func() []string {
		got.mu.Lock()
		defer got.mu.Unlock()
		var out []string
		for _, b := range got.body {
			var m map[string]any
			_ = json.Unmarshal(b, &m)
			out = append(out, m["title"].(string))
		}
		return out
	}
	fired := map[string]bool{}
	check := func(at int) { n.checkUsage(ctx, base.Add(time.Duration(at)*time.Second), fired); n.Wait() }

	// Only 2 minutes of high CPU: a 5-minute rule waits; one low sample in the window holds it back.
	sample(app.ID, 0, 120, 1.5, 100)
	check(120)
	sample(app.ID, 120, 130, 0.2, 100)
	sample(app.ID, 130, 310, 1.5, 100)
	check(310)
	if len(bodies()) != 0 {
		t.Fatalf("fired too early: %v", bodies())
	}
	sample(app.ID, 310, 440, 1.5, 100)
	check(440) // 300 s window from 140 s: all high
	check(450) // still high: no repeat
	if b := bodies(); len(b) != 1 || b[0] != "High CPU: web" {
		t.Fatalf("cpu alert %v", b)
	}
	sample(app.ID, 440, 450, 0.3, 100)
	check(450)
	if b := bodies(); len(b) != 2 || b[1] != "CPU back to normal: web" {
		t.Fatalf("cpu recovery %v", b)
	}
	// The memory rule covers every app: api at 95% of its limit for 2 minutes fires.
	sample(other.ID, 330, 460, 0.1, 950)
	check(460)
	if b := bodies(); len(b) != 3 || b[2] != "High memory: api" {
		t.Fatalf("memory alert %v", b)
	}
	// Recorded in Activity, and never re-sent through the event hook.
	events, _ := st.ListEvents(ctx, 20)
	if len(events) != 3 || events[0].Action != UsageMemory || events[1].Action != "usage.cpu_normal" {
		t.Fatalf("events %+v", events)
	}
	// A deleted rule stops its alerts.
	_ = st.DeleteAlertRule(ctx, cpu.ID)
	check(470)
	for k := range fired {
		if strings.HasPrefix(k, cpu.ID) {
			t.Fatal("state of a deleted rule kept")
		}
	}
}

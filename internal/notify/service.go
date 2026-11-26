package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"panel/internal/hostmetrics"
	"panel/internal/store"
)

// Events that alert rules can name. Rules for deployment.failed also match a
// deployment that failed and was rolled back.
var Events = []string{"deployment.started", "deployment.succeeded", "deployment.failed", "app.health_failing", "app.health_recovered", "app.crashed", "backup.failed", "disk.high", UsageCPU, UsageMemory}

// DiskHigh and DiskClear: the disk alert fires at DiskHigh percent and re-arms
// only once usage falls under DiskClear, so it does not repeat every minute.
const (
	DiskHigh  = 90.0
	DiskClear = 85.0
)

type Service struct {
	st     *store.Store
	log    *slog.Logger
	sender *Sender
	ctx    context.Context
	wg     sync.WaitGroup
}

func New(ctx context.Context, st *store.Store, log *slog.Logger, sender *Sender) *Service {
	if sender == nil {
		sender = NewSender()
	}
	return &Service{st: st, log: log, sender: sender, ctx: ctx}
}

// Wait blocks until sends in progress are done (tests, shutdown).
func (s *Service) Wait() { s.wg.Wait() }

func ruleEvent(action string) string {
	if action == "deployment.rolled_back" {
		return "deployment.failed"
	}
	return action
}

// Handle is the store's event hook: it matches rules and sends in the background.
func (s *Service) Handle(e store.Event) {
	event := ruleEvent(e.Action)
	if !contains(Events, event) || strings.HasPrefix(event, "usage.") {
		return // usage rules are sent by checkUsage, each to its own channel
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
		defer cancel()
		rules, err := s.st.MatchingAlertRules(ctx, event, e.TargetID)
		if err != nil {
			s.log.Error("alert rules", "error", err)
			return
		}
		seen := map[string]bool{}
		m := s.message(ctx, event, e)
		for _, r := range rules {
			if seen[r.ChannelID] {
				continue // one message per channel even when rules overlap
			}
			seen[r.ChannelID] = true
			_ = s.deliver(ctx, r.ChannelID, m) // a failed delivery is recorded on its channel
		}
	}()
}

func (s *Service) deliver(ctx context.Context, channelID string, m Message) error {
	c, err := s.st.GetNotificationChannel(ctx, channelID)
	if err != nil {
		return err
	}
	err = s.sender.Send(ctx, c.Kind, c.Config, m)
	msg := ""
	if err != nil {
		msg = err.Error()
		s.log.Warn("notification failed", "channel", c.Name, "error", msg)
	}
	if rerr := s.st.RecordNotificationResult(ctx, c.ID, msg); rerr != nil {
		s.log.Error("record notification", "error", rerr)
	}
	return err
}

// ErrNoMailer: no email channel is set up, so the panel has no mail server to send through.
var ErrNoMailer = errors.New("no email notification channel is set up")

// mailer is the email channel the panel sends its own mail through: the oldest one.
func (s *Service) mailer(ctx context.Context) (store.NotificationChannel, error) {
	list, err := s.st.ListNotificationChannels(ctx)
	if err != nil {
		return store.NotificationChannel{}, err
	}
	for _, c := range list {
		if c.Kind == "email" {
			return c, nil
		}
	}
	return store.NotificationChannel{}, ErrNoMailer
}

// Mailer names the email channel whose mail server the panel sends its own mail through
// (an invitation); false when there is none.
func (s *Service) Mailer(ctx context.Context) (string, bool) {
	c, err := s.mailer(ctx)
	return c.Name, err == nil
}

// Mail sends one message to one address through that channel's mail server, from its
// sender address. The channel's own recipients do not receive it.
func (s *Service) Mail(ctx context.Context, to, subject, text string) error {
	c, err := s.mailer(ctx)
	if err != nil {
		return err
	}
	cfg := make(map[string]string, len(c.Config))
	for k, v := range c.Config {
		cfg[k] = v
	}
	cfg["to"] = to
	return s.sender.Send(ctx, "email", cfg, Message{Title: subject, Text: text, Level: "info", At: time.Now()})
}

// Test sends a test message to a channel and returns the delivery error.
func (s *Service) Test(ctx context.Context, channelID string) error {
	url, _ := s.panelURL(ctx, "/settings?tab=notifications")
	return s.deliver(ctx, channelID, Message{Title: "Test notification", Text: "This channel is set up: alerts from the panel will arrive here.", Level: "info", URL: url, At: time.Now()})
}

func (s *Service) panelURL(ctx context.Context, path string) (string, bool) {
	domain, ok, err := s.st.GetSetting(ctx, store.SettingPanelDomain)
	if err != nil || !ok || domain == "" {
		return "", false
	}
	scheme := "https"
	if tlsMode, _, _ := s.st.GetSetting(ctx, store.SettingPanelTLS); tlsMode == store.TLSHTTPOnly {
		scheme = "http"
	}
	return scheme + "://" + domain + path, true
}

func (s *Service) message(ctx context.Context, event string, e store.Event) Message {
	m := Message{At: e.CreatedAt, Level: "info"}
	if m.At.IsZero() {
		m.At = time.Now()
	}
	detail := strings.TrimSpace(e.Detail)
	switch event {
	case "deployment.started":
		m.Title = "Deploying " + e.TargetName
		m.Text = "A deployment of " + e.TargetName + " started."
	case "deployment.succeeded":
		m.Title, m.Level = "Deployed "+e.TargetName, "success"
		m.Text = e.TargetName + " was deployed successfully."
	case "deployment.failed":
		m.Title, m.Level = "Deployment failed: "+e.TargetName, "error"
		m.Text = "The deployment of " + e.TargetName + " failed."
		if e.Action == "deployment.rolled_back" {
			m.Text += " The previous version is serving again."
		}
	case "app.health_failing":
		m.Title, m.Level = "Unhealthy: "+e.TargetName, "error"
		m.Text = e.TargetName + " is failing."
	case "app.health_recovered":
		m.Title, m.Level = "Recovered: "+e.TargetName, "success"
		m.Text = e.TargetName + " is healthy again."
	case "app.crashed":
		m.Title, m.Level = "Crashed: "+e.TargetName, "error"
		m.Text = e.TargetName + " crashed and was restarted."
	case "backup.failed":
		m.Title, m.Level = "Backup failed: "+e.TargetName, "error"
		m.Text = "A backup of the database instance " + e.TargetName + " failed."
		if e.TargetType == "panel" {
			m.Text = "The panel's own backup (its database and key) failed."
		}
	case "disk.high":
		m.Title, m.Level = "Disk almost full on "+e.TargetName, "error"
		m.Text = "The data disk is nearly full."
	}
	if detail != "" {
		m.Text += "\n" + strings.ToUpper(detail[:1]) + detail[1:] // reasons are written as clauses
	}
	switch e.TargetType {
	case "app":
		if a, err := s.st.GetApp(ctx, e.TargetID); err == nil {
			tab := "deployments"
			if strings.HasPrefix(event, "app.") {
				tab = "logs" // a crash or failing check: the runtime logs say why
			}
			m.URL, _ = s.panelURL(ctx, "/projects/"+a.ProjectID+"/apps/"+a.ID+"?tab="+tab)
		}
	case "database":
		m.URL, _ = s.panelURL(ctx, "/databases/"+e.TargetID+"?tab=backups")
	case "panel":
		m.URL, _ = s.panelURL(ctx, "/backups")
	default:
		m.URL, _ = s.panelURL(ctx, "/")
	}
	return m
}

// WatchDisk records a disk.high event when the data disk passes DiskHigh percent,
// checking every interval, and re-arms below DiskClear.
func (s *Service) WatchDisk(ctx context.Context, snapshot func() hostmetrics.Snapshot, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	fired := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		fired = s.checkDisk(ctx, snapshot(), fired)
	}
}

func (s *Service) checkDisk(ctx context.Context, snap hostmetrics.Snapshot, fired bool) bool {
	if snap.Disk == nil {
		return fired
	}
	p := snap.Disk.Percent
	if fired {
		return p >= DiskClear
	}
	if p < DiskHigh {
		return false
	}
	host := snap.Hostname
	if host == "" {
		host = "the server"
	}
	detail := fmt.Sprintf("%.0f%% used: %s free of %s (%s).", p, gib(snap.Disk.Available), gib(snap.Disk.Total), snap.Disk.Path)
	if err := s.st.AddEvent(ctx, store.Event{Actor: "system", Action: "disk.high", TargetType: "host", TargetName: host, Detail: detail}); err != nil {
		s.log.Error("record event", "error", err)
	}
	return true
}

func gib(n uint64) string { return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30)) }

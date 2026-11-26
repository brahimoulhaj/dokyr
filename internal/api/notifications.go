package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"panel/internal/notify"
	"panel/internal/store"
)

// Notifier sends test messages; the store's event hook does the rest.
type Notifier interface {
	Test(ctx context.Context, channelID string) error
	// Mailer names the email channel the panel sends its own mail through, if any.
	Mailer(ctx context.Context) (name string, ok bool)
	// Mail sends one message to one address through that channel's mail server.
	Mail(ctx context.Context, to, subject, text string) error
}

func (s *Server) SetNotifier(n Notifier) { s.notifier = n }

type channelJSON struct {
	store.NotificationChannel
	Config map[string]any `json:"config"`
}

func toChannelJSON(c store.NotificationChannel) channelJSON {
	return channelJSON{NotificationChannel: c, Config: notify.Public(c.Kind, c.Config)}
}

func (s *Server) notifyError(w http.ResponseWriter, err error) bool {
	if errors.Is(err, notify.ErrInvalid) {
		invalid(w, strings.TrimPrefix(err.Error(), notify.ErrInvalid.Error()+": "))
		return true
	}
	return s.storeError(w, "notification channel", err, "a channel with this name already exists")
}

func (s *Server) handleNotificationKinds(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"kinds": notify.Kinds, "events": notify.Events})
}

func (s *Server) handleListChannels(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListNotificationChannels(r.Context())
	if s.storeError(w, "list notification channels", err, "") {
		return
	}
	out := make([]channelJSON, 0, len(list))
	for _, c := range list {
		out = append(out, toChannelJSON(c))
	}
	writeJSON(w, 200, out)
}

type channelInput struct {
	Name   string            `json:"name"`
	Kind   string            `json:"kind"`
	Config map[string]string `json:"config"`
}

func (in *channelInput) name() (string, bool) {
	n := strings.TrimSpace(in.Name)
	return n, n != "" && len(n) <= 64
}

func (s *Server) handleCreateChannel(w http.ResponseWriter, r *http.Request) {
	var in channelInput
	if !readJSON(w, r, &in) {
		return
	}
	name, ok := in.name()
	if !ok {
		invalid(w, "enter a name of at most 64 characters")
		return
	}
	cfg, err := notify.Merge(in.Kind, nil, in.Config)
	if s.notifyError(w, err) {
		return
	}
	c, err := s.store.CreateNotificationChannel(r.Context(), name, in.Kind, cfg)
	if s.notifyError(w, err) {
		return
	}
	s.audit(r, "notification.channel_create", "notification_channel", c.ID, c.Name, c.Kind)
	writeJSON(w, 201, toChannelJSON(c))
}

// handleUpdateChannel keeps the type; an empty secret field keeps the saved secret.
func (s *Server) handleUpdateChannel(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.GetNotificationChannel(r.Context(), r.PathValue("id"))
	if s.storeError(w, "get notification channel", err, "") {
		return
	}
	var in channelInput
	if !readJSON(w, r, &in) {
		return
	}
	name, ok := in.name()
	if !ok {
		invalid(w, "enter a name of at most 64 characters")
		return
	}
	cfg, err := notify.Merge(c.Kind, c.Config, in.Config)
	if s.notifyError(w, err) {
		return
	}
	if s.notifyError(w, s.store.UpdateNotificationChannel(r.Context(), c.ID, name, cfg)) {
		return
	}
	s.audit(r, "notification.channel_update", "notification_channel", c.ID, name, c.Kind)
	c, err = s.store.GetNotificationChannel(r.Context(), c.ID)
	if s.storeError(w, "get notification channel", err, "") {
		return
	}
	writeJSON(w, 200, toChannelJSON(c))
}

func (s *Server) handleDeleteChannel(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.GetNotificationChannel(r.Context(), r.PathValue("id"))
	if s.storeError(w, "get notification channel", err, "") {
		return
	}
	if s.storeError(w, "delete notification channel", s.store.DeleteNotificationChannel(r.Context(), c.ID), "") {
		return
	}
	s.audit(r, "notification.channel_delete", "notification_channel", c.ID, c.Name, c.Kind)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTestChannel(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.GetNotificationChannel(r.Context(), r.PathValue("id"))
	if s.storeError(w, "get notification channel", err, "") {
		return
	}
	if s.notifier == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "notifications are starting up")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := s.notifier.Test(ctx, c.ID); err != nil {
		writeError(w, http.StatusBadGateway, "send_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListRules(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListAlertRules(r.Context())
	if s.storeError(w, "list alert rules", err, "") {
		return
	}
	writeJSON(w, 200, list)
}

func (s *Server) handleCreateRule(w http.ResponseWriter, r *http.Request) {
	var in store.AlertRule
	if !readJSON(w, r, &in) {
		return
	}
	if !contains(notify.Events, in.Event) {
		invalid(w, "unknown event")
		return
	}
	if _, err := s.store.GetNotificationChannel(r.Context(), in.ChannelID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			invalid(w, "choose a notification channel")
		} else {
			s.internal(w, "get notification channel", err)
		}
		return
	}
	usage := strings.HasPrefix(in.Event, "usage.")
	if usage {
		max := 1024.0 // cores
		if in.Event == notify.UsageMemory {
			max = 100 // percent of the limit
		}
		if in.Threshold <= 0 || in.Threshold > max {
			invalid(w, fmt.Sprintf("the threshold must be above 0 and at most %g", max))
			return
		}
		if in.Duration < 60 || in.Duration > 3600 {
			invalid(w, "the duration must be 1 to 60 minutes")
			return
		}
	} else {
		in.Threshold, in.Duration = 0, 0
	}
	target := ""
	if in.TargetID != "" {
		var err error
		switch {
		case usage: // one app or one database instance
			if a, aerr := s.store.GetApp(r.Context(), in.TargetID); aerr == nil {
				target = a.Name
			} else if d, derr := s.store.GetDBInstance(r.Context(), in.TargetID); derr == nil {
				target = d.Name
			} else {
				err = store.ErrNotFound
			}
		case strings.HasPrefix(in.Event, "deployment.") || strings.HasPrefix(in.Event, "app."):
			var a store.App
			a, err = s.store.GetApp(r.Context(), in.TargetID)
			target = a.Name
		case in.Event == "backup.failed":
			var d store.DBInstance
			d, err = s.store.GetDBInstance(r.Context(), in.TargetID)
			target = d.Name
		default:
			err = store.ErrNotFound
		}
		if err != nil {
			invalid(w, "this event cannot be limited to that target")
			return
		}
	}
	in.Enabled = true
	rule, err := s.store.CreateAlertRule(r.Context(), in)
	if s.storeError(w, "create alert rule", err, "this rule already exists") {
		return
	}
	if target == "" {
		target = "all"
	}
	s.audit(r, "notification.rule_create", "alert_rule", rule.ID, in.Event, target)
	writeJSON(w, 201, rule)
}

func (s *Server) handleUpdateRule(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if s.storeError(w, "update alert rule", s.store.SetAlertRuleEnabled(r.Context(), r.PathValue("id"), in.Enabled), "") {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	if s.storeError(w, "delete alert rule", s.store.DeleteAlertRule(r.Context(), r.PathValue("id")), "") {
		return
	}
	s.audit(r, "notification.rule_delete", "alert_rule", r.PathValue("id"), "", "")
	w.WriteHeader(http.StatusNoContent)
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

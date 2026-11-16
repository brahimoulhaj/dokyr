package store

import (
	"context"
	"database/sql"
	"errors"

	"panel/internal/id"
)

// NotificationChannel is where alerts go. Config holds the kind's settings,
// secrets included; it is sealed at rest and never sent to the browser.
type NotificationChannel struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Kind       string            `json:"kind"`
	Config     map[string]string `json:"-"`
	LastSentAt *string           `json:"last_sent_at"`
	LastError  string            `json:"last_error"`
	CreatedAt  string            `json:"created_at"`
}

// AlertRule sends one event (for one target, or all when TargetID is empty) to a channel.
type AlertRule struct {
	ID        string `json:"id"`
	Event     string `json:"event"`
	TargetID  string `json:"target_id"`
	ChannelID string `json:"channel_id"`
	Enabled   bool   `json:"enabled"`
}

const channelCols = `id,name,kind,config,last_sent_at,last_error,created_at`

func (s *Store) scanChannel(row interface{ Scan(...any) error }) (NotificationChannel, error) {
	var c NotificationChannel
	var sealed string
	var sent sql.NullString
	if err := row.Scan(&c.ID, &c.Name, &c.Kind, &sealed, &sent, &c.LastError, &c.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c, ErrNotFound
		}
		return c, err
	}
	if sent.Valid {
		c.LastSentAt = &sent.String
	}
	cfg, err := s.openEnvironment(sealed)
	if err != nil {
		return c, err
	}
	c.Config = cfg
	return c, nil
}

func (s *Store) ListNotificationChannels(ctx context.Context) ([]NotificationChannel, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+channelCols+` FROM notification_channels ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NotificationChannel{}
	for rows.Next() {
		c, err := s.scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) GetNotificationChannel(ctx context.Context, cid string) (NotificationChannel, error) {
	return s.scanChannel(s.db.QueryRowContext(ctx, `SELECT `+channelCols+` FROM notification_channels WHERE id=?`, cid))
}

func (s *Store) CreateNotificationChannel(ctx context.Context, name, kind string, config map[string]string) (NotificationChannel, error) {
	sealed, err := s.sealEnvironment(config)
	if err != nil {
		return NotificationChannel{}, err
	}
	cid, ts := id.New(), now()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO notification_channels(id,name,kind,config,created_at,updated_at) VALUES(?,?,?,?,?,?)`, cid, name, kind, sealed, ts, ts); err != nil {
		return NotificationChannel{}, conflict(err)
	}
	return s.GetNotificationChannel(ctx, cid)
}

func (s *Store) UpdateNotificationChannel(ctx context.Context, cid, name string, config map[string]string) error {
	sealed, err := s.sealEnvironment(config)
	if err != nil {
		return err
	}
	return affected(s.db.ExecContext(ctx, `UPDATE notification_channels SET name=?,config=?,updated_at=? WHERE id=?`, name, sealed, now(), cid))
}

// DeleteNotificationChannel also removes its rules.
func (s *Store) DeleteNotificationChannel(ctx context.Context, cid string) error {
	return affected(s.db.ExecContext(ctx, `DELETE FROM notification_channels WHERE id=?`, cid))
}

// RecordNotificationResult keeps the outcome of the latest send (empty error = delivered).
func (s *Store) RecordNotificationResult(ctx context.Context, cid, errMsg string) error {
	ts := now()
	if errMsg != "" {
		_, err := s.db.ExecContext(ctx, `UPDATE notification_channels SET last_error=? WHERE id=?`, errMsg, cid)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE notification_channels SET last_sent_at=?,last_error='' WHERE id=?`, ts, cid)
	return err
}

func (s *Store) ListAlertRules(ctx context.Context) ([]AlertRule, error) {
	return s.alertRules(ctx, `SELECT id,event,target_id,channel_id,enabled FROM alert_rules ORDER BY event,target_id,channel_id`)
}

// MatchingAlertRules returns the enabled rules for an event on a target.
func (s *Store) MatchingAlertRules(ctx context.Context, event, targetID string) ([]AlertRule, error) {
	return s.alertRules(ctx, `SELECT id,event,target_id,channel_id,enabled FROM alert_rules WHERE enabled=1 AND event=? AND (target_id='' OR target_id=?)`, event, targetID)
}

func (s *Store) alertRules(ctx context.Context, query string, args ...any) ([]AlertRule, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AlertRule{}
	for rows.Next() {
		var r AlertRule
		if err := rows.Scan(&r.ID, &r.Event, &r.TargetID, &r.ChannelID, &r.Enabled); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) CreateAlertRule(ctx context.Context, r AlertRule) (AlertRule, error) {
	r.ID = id.New()
	ts := now()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO alert_rules(id,event,target_id,channel_id,enabled,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, r.ID, r.Event, r.TargetID, r.ChannelID, r.Enabled, ts, ts); err != nil {
		return r, conflict(err)
	}
	return r, nil
}

func (s *Store) SetAlertRuleEnabled(ctx context.Context, rid string, enabled bool) error {
	return affected(s.db.ExecContext(ctx, `UPDATE alert_rules SET enabled=?,updated_at=? WHERE id=?`, enabled, now(), rid))
}

func (s *Store) DeleteAlertRule(ctx context.Context, rid string) error {
	return affected(s.db.ExecContext(ctx, `DELETE FROM alert_rules WHERE id=?`, rid))
}

// SetEventHook calls fn after every recorded event (audit log entries included);
// the notification dispatcher uses it. fn must not block.
func (s *Store) SetEventHook(fn func(Event)) { s.eventHook = fn }

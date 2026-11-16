package store

import (
	"context"
	"strings"
	"time"

	"panel/internal/id"
)

// Event is one entry of the audit log.
type Event struct {
	ID         string
	UserID     string // empty for system events
	Actor      string // email at the time, or "system"
	Action     string // e.g. "app.create"
	TargetType string
	TargetID   string
	TargetName string
	Detail     string
	CreatedAt  time.Time
}

func (s *Store) AddEvent(ctx context.Context, e Event) error {
	ts := now()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO events (id, user_id, actor, action, target_type, target_id, target_name, detail, created_at, updated_at)
		VALUES (?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, ?)`,
		id.New(), e.UserID, e.Actor, e.Action, e.TargetType, e.TargetID, e.TargetName, e.Detail, ts, ts)
	if err == nil && s.eventHook != nil {
		e.CreatedAt = parseTime(ts)
		s.eventHook(e)
	}
	return err
}

// ListEvents returns the newest first.
func (s *Store) ListEvents(ctx context.Context, limit int) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, COALESCE(user_id, ''), actor, action, target_type, target_id, target_name, detail, created_at
		FROM events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		var created string
		if err := rows.Scan(&e.ID, &e.UserID, &e.Actor, &e.Action, &e.TargetType, &e.TargetID, &e.TargetName, &e.Detail, &created); err != nil {
			return nil, err
		}
		e.CreatedAt = parseTime(created)
		out = append(out, e)
	}
	return out, rows.Err()
}

// EventGroups name sets of actions the Activity page filters by (action prefixes).
var EventGroups = map[string][]string{
	"deploys":   {"deployment.", "app.deploy", "app.rollback", "app.push_deploy"},
	"apps":      {"app."},
	"databases": {"database.", "backup."},
	"projects":  {"project.", "domain.", "route."},
	"alerts":    {"app.health_", "app.crashed", "disk.", "usage.", "notification.", "backup.failed", "deployment.failed", "deployment.rolled_back"},
	"settings":  {"settings.", "panel.", "token.", "git_source.", "registry"},
}

// failureActions are events that report something going wrong.
var failureActions = []string{"deployment.rolled_back", "app.crashed", "app.health_failing", "disk.high", "usage.cpu_high", "usage.memory_high"}

// EventQuery filters the audit log. Empty fields match everything.
type EventQuery struct {
	Text     string    // in the actor, action, target name or detail
	Group    string    // a key of EventGroups
	Actor    string    // "people" or "system" (the panel itself and webhooks)
	Failures bool      // only events reporting a failure
	Since    time.Time // zero: no limit
	Offset   int
	Limit    int
}

// SearchEvents returns one page of matching events, newest first, and how many match.
func (s *Store) SearchEvents(ctx context.Context, q EventQuery) ([]Event, int, error) {
	where, args := []string{"1=1"}, []any{}
	if t := strings.TrimSpace(q.Text); t != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(strings.ToLower(t)) + "%"
		where = append(where, `(lower(actor) LIKE ? ESCAPE '\' OR lower(action) LIKE ? ESCAPE '\' OR lower(target_name) LIKE ? ESCAPE '\' OR lower(detail) LIKE ? ESCAPE '\')`)
		args = append(args, like, like, like, like)
	}
	if prefixes, ok := EventGroups[q.Group]; ok {
		var or []string
		for _, p := range prefixes {
			or = append(or, "action LIKE ?")
			args = append(args, p+"%")
		}
		where = append(where, "("+strings.Join(or, " OR ")+")")
	}
	switch q.Actor {
	case "system":
		where = append(where, "actor IN ('system','webhook','')")
	case "people":
		where = append(where, "actor NOT IN ('system','webhook','')")
	}
	if q.Failures {
		marks := strings.TrimSuffix(strings.Repeat("?,", len(failureActions)), ",")
		where = append(where, "(action LIKE '%failed%' OR action IN ("+marks+"))")
		for _, a := range failureActions {
			args = append(args, a)
		}
	}
	if !q.Since.IsZero() {
		where = append(where, "created_at >= ?")
		args = append(args, fmtTime(q.Since))
	}
	cond := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, COALESCE(user_id, ''), actor, action, target_type, target_id, target_name, detail, created_at
		FROM events WHERE `+cond+` ORDER BY id DESC LIMIT ? OFFSET ?`, append(args, q.Limit, q.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		var created string
		if err := rows.Scan(&e.ID, &e.UserID, &e.Actor, &e.Action, &e.TargetType, &e.TargetID, &e.TargetName, &e.Detail, &created); err != nil {
			return nil, 0, err
		}
		e.CreatedAt = parseTime(created)
		out = append(out, e)
	}
	return out, total, rows.Err()
}

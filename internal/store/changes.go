package store

import (
	"sync"

	"modernc.org/sqlite"
)

// Changes tells listeners which tables a committed transaction wrote, so the web UI can
// refetch what it shows instead of polling. Every write goes through SQLite, so catching
// it there means no code path can forget to announce a change.
//
// Only table names are sent, never row contents. Tables that change constantly or carry
// nothing the UI shows live (metric samples) are left out.
type Changes struct {
	mu   sync.Mutex
	subs map[*Subscription]struct{}
}

// watchedTables are the tables whose changes are announced.
var watchedTables = map[string]bool{
	"projects": true, "apps": true, "app_health_checks": true, "deployments": true,
	"domains": true, "domain_routes": true, "events": true,
	"db_instances": true, "db_operations": true, "db_backups": true, "backup_policies": true,
	"db_databases": true, "db_users": true, "db_user_grants": true,
	"registries": true, "registry_tokens": true, "git_sources": true, "s3_connections": true,
	"notification_channels": true, "alert_rules": true, "settings": true, "api_tokens": true,
	"sessions": true, "cron_jobs": true, "cron_job_runs": true,
}

// Subscription collects the tables changed since it was last drained.
type Subscription struct {
	// Ready receives a value when changes are waiting; it never blocks the writer.
	Ready   chan struct{}
	mu      sync.Mutex
	pending map[string]struct{}
}

// Drain returns the tables changed since the last call and resets the set.
func (s *Subscription) Drain() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.pending))
	for table := range s.pending {
		out = append(out, table)
	}
	clear(s.pending)
	return out
}

// Subscribe starts collecting changes. Call the returned function to stop.
func (c *Changes) Subscribe() (*Subscription, func()) {
	sub := &Subscription{Ready: make(chan struct{}, 1), pending: map[string]struct{}{}}
	c.mu.Lock()
	c.subs[sub] = struct{}{}
	c.mu.Unlock()
	return sub, func() {
		c.mu.Lock()
		delete(c.subs, sub)
		c.mu.Unlock()
	}
}

func (c *Changes) publish(tables map[string]struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for sub := range c.subs {
		sub.mu.Lock()
		for table := range tables {
			sub.pending[table] = struct{}{}
		}
		sub.mu.Unlock()
		select {
		case sub.Ready <- struct{}{}:
		default: // already signalled; the reader picks these up with the rest
		}
	}
}

// changeHubs maps a DSN to its store's hub. SQLite connection hooks are registered on
// the driver, once for every store, so each new connection finds its hub by DSN.
var changeHubs sync.Map // string -> *Changes

func init() {
	sqlite.RegisterConnectionHook(func(conn sqlite.ExecQuerierContext, dsn string) error {
		value, ok := changeHubs.Load(dsn)
		if !ok {
			return nil
		}
		hooks, ok := conn.(sqlite.HookRegisterer)
		if !ok {
			return nil
		}
		hub := value.(*Changes)
		// A connection runs one statement at a time, so this set needs no lock.
		pending := map[string]struct{}{}
		hooks.RegisterPreUpdateHook(func(d sqlite.SQLitePreUpdateData) {
			if d.DatabaseName == "main" && watchedTables[d.TableName] {
				pending[d.TableName] = struct{}{}
			}
		})
		hooks.RegisterCommitHook(func() int32 {
			if len(pending) > 0 {
				hub.publish(pending)
				clear(pending)
			}
			return 0 // let the commit go ahead
		})
		hooks.RegisterRollbackHook(func() { clear(pending) })
		return nil
	})
}

-- Notification channels (config is JSON sealed with the panel key: it holds
-- webhook URLs, tokens and SMTP passwords) and the rules that route events to them.
CREATE TABLE notification_channels (
 id TEXT PRIMARY KEY,
 name TEXT NOT NULL UNIQUE,
 kind TEXT NOT NULL CHECK(kind IN ('email','slack','discord','telegram','webhook')),
 config TEXT NOT NULL,
 last_sent_at TEXT,
 last_error TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
-- target_id '' matches every app or database instance.
CREATE TABLE alert_rules (
 id TEXT PRIMARY KEY,
 event TEXT NOT NULL,
 target_id TEXT NOT NULL DEFAULT '',
 channel_id TEXT NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
 enabled INTEGER NOT NULL DEFAULT 1,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 UNIQUE(event,target_id,channel_id)
);
CREATE INDEX alert_rules_event ON alert_rules(event);

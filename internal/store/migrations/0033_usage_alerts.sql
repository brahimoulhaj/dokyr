-- Usage alert rules (usage.cpu_high, usage.memory_high) carry a threshold (cores, or
-- percent of the memory limit) and how long usage must stay above it, in seconds.
ALTER TABLE alert_rules ADD COLUMN threshold REAL NOT NULL DEFAULT 0;
ALTER TABLE alert_rules ADD COLUMN duration_s INTEGER NOT NULL DEFAULT 0;

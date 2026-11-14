-- How long, in seconds, the old container keeps running after traffic switched to the
-- new one, so the requests it is still answering can finish. 0 = removed at once.
ALTER TABLE app_health_checks ADD COLUMN drain_s INTEGER NOT NULL DEFAULT 10;

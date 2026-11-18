-- A fingerprint of the settings the app's running deployment was made with (image or
-- source, port, limits, variables, mounts, processes, start command, health check). When
-- the settings no longer match it, the app has changes a deploy would apply. '' = unknown:
-- apps deployed before this, which show no such notice until their next deploy.
ALTER TABLE apps ADD COLUMN deployed_config TEXT NOT NULL DEFAULT '';

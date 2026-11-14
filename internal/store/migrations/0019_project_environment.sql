-- Shared environment variables of a project, encrypted like an app's. Apps reference them
-- as ${project.NAME}; with env_inherit = 1 every app also gets them all, its own values
-- winning. Off by default: shared values should not reach every app unasked.
ALTER TABLE projects ADD COLUMN environment TEXT NOT NULL DEFAULT '{}';
ALTER TABLE projects ADD COLUMN env_inherit INTEGER NOT NULL DEFAULT 0 CHECK (env_inherit IN (0, 1));

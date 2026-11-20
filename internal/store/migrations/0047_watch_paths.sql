-- Paths a push must touch to deploy a Git app, as a JSON list of patterns
-- (services/api/**, *.md); '[]' = every push to the branch deploys.
ALTER TABLE apps ADD COLUMN watch_paths TEXT NOT NULL DEFAULT '[]';

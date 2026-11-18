-- Extra processes of an app (queue workers, schedulers): a JSON list of {name, command}.
-- Each runs in its own container from the app's image, started with every deployment.
ALTER TABLE apps ADD COLUMN processes TEXT NOT NULL DEFAULT '[]';

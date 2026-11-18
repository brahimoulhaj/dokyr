-- The template an app was created from ("wordpress"), or '' for an app made by hand. It
-- tells the panel which variables the app expects from a database, so it can offer to fill them.
ALTER TABLE apps ADD COLUMN template TEXT NOT NULL DEFAULT '';

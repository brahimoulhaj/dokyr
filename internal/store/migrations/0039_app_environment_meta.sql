-- What each app variable is beyond its value: a JSON object {NAME: {scope, secret}}. scope
-- is runtime (the container), build (the image build) or both; secret hides the value from
-- the API and passes it to a build as a build secret, not a build argument. Only variables
-- that differ from the default (runtime, not secret) are listed, so existing apps keep
-- behaving as before. Names and flags only: the values stay in the encrypted environment.
ALTER TABLE apps ADD COLUMN environment_meta TEXT NOT NULL DEFAULT '{}';

-- Settings of the builders that need more than a Dockerfile path: the build command
-- (Nixpacks, Static), the output folder and single-page fallback (Static), and an image
-- (the Buildpacks builder, or the image a Static site is built in). JSON, '{}' = defaults.
ALTER TABLE apps ADD COLUMN build_options TEXT NOT NULL DEFAULT '{}';

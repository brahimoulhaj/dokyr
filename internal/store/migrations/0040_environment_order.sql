-- The order of an environment as its owner wrote it, with the comments between the
-- variables: a JSON list of lines, each a variable name, a "# comment" or "" (a blank
-- line). Names and notes only: the values stay in the encrypted environment. A variable
-- missing from the list (every one, for environments saved before this) comes last, by name.
ALTER TABLE apps ADD COLUMN environment_order TEXT NOT NULL DEFAULT '[]';
ALTER TABLE projects ADD COLUMN environment_order TEXT NOT NULL DEFAULT '[]';

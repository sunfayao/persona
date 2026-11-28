-- The conversation that supervised a to-do before a supervisor Agent took it over: where a decision of that Agent goes.
ALTER TABLE todo_supervision ADD COLUMN IF NOT EXISTS above text NOT NULL DEFAULT '';

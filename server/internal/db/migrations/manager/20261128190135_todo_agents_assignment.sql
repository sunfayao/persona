-- What one Agent answers for when several work on a to-do (JSON the assistant owns); NULL for an Agent that works on the whole.
ALTER TABLE todo_agents ADD COLUMN IF NOT EXISTS assignment jsonb;

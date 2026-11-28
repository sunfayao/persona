-- The bounded class of a background task's work (test, build, ...); never its command or description.
-- Rows written before this column, and by older nodes, keep it NULL.
ALTER TABLE agent_lifecycle_events ADD COLUMN IF NOT EXISTS activity text;

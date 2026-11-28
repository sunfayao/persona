-- The typed checks a to-do declares on its criteria (kind and arg per criterion text) that the hub runs itself.
ALTER TABLE todo_supervision ADD COLUMN IF NOT EXISTS checks jsonb NOT NULL DEFAULT '[]'::jsonb;

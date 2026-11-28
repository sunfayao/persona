CREATE TABLE IF NOT EXISTS todo_outcome_completions (
    todo_id TEXT NOT NULL REFERENCES todos(id) ON DELETE CASCADE,
    completion_key TEXT NOT NULL,
    PRIMARY KEY (todo_id, completion_key)
);

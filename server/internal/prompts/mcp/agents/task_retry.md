Start a failed task's agent again: in its kept tab (once task_start_keys answered what stopped it), else in a new tab, reopening it; with launcher, on that launcher of the same computer and workspace. At most 3 retries; the hub never retries itself. Returns at once; the outcome comes as a supervision message. Only this conversation's tasks; reason goes in their history.

```json
{
  "type": "object",
  "required": ["todo_id", "reason"],
  "properties": {
    "todo_id": {
      "type": "string",
      "description": "the task's id, from the supervision message"
    },
    "reason": {
      "type": "string",
      "description": "why it should start now, in the user's language, one sentence"
    },
    "launcher": {
      "type": "string",
      "description": "optional: another launcher of that computer, as the supervision message lists"
    },
    "model": {
      "type": "string",
      "description": "with launcher: a model id (default: its own)"
    }
  }
}
```

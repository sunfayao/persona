Change a worker's done_when or expect_minutes after it started, or the expect_minutes of your own to-do: when an estimate runs out (estimate_due, overdue) or what a worker must deliver changes. It is logged on the to-do's brief with what changes and why; the worker is not told, so send it the change with agent_send. Give done_when as the whole list after the change. Your own to-do's done_when is the main conversation's to change: ask it with report_to_supervisor (kind question). A step only the owner takes (deploying, merging, releasing, public sends, deleting, spending) is refused.

```json
{
  "type": "object",
  "required": ["todo_id", "change", "why"],
  "additionalProperties": false,
  "properties": {
    "todo_id": {
      "type": "string",
      "description": "a worker's to-do id, or your own to-do's (expect_minutes only)"
    },
    "change": {
      "type": "string",
      "description": "what changes, in a sentence the owner can read"
    },
    "why": {
      "type": "string"
    },
    "done_when": {
      "type": "array",
      "maxItems": 6,
      "items": {
        "type": "string"
      },
      "description": "a worker's criteria as they are after the change, the whole list"
    },
    "expect_minutes": {
      "type": "integer",
      "minimum": 0,
      "description": "minutes it should take from now; 0: as it is"
    }
  }
}
```

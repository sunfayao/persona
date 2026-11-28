Change a worker's criteria or expect_minutes after it started, or your own to-do's expect_minutes. Logged on the brief; the worker is not told (agent_send). Use add, drop and edit by number (an edit keeps its place) or done_when whole; a criterion is one result of at most 200 characters. Your own to-do's done_when is the main conversation's (report_to_supervisor, kind question). A step only the owner takes (deploying, merging, releasing, public sends, deleting, spending) is refused.

```json
{
  "type": "object",
  "required": ["todo_id", "change", "why"],
  "additionalProperties": false,
  "properties": {
    "todo_id": {"type": "string", "description": "a worker's to-do id, or your own (expect_minutes only)"},
    "change": {"type": "string", "description": "what changes, one sentence"},
    "why": {"type": "string"},
    "done_when": {"type": "array", "maxItems": 6, "items": {"type": "string"}, "description": "the whole list after the change"},
    "checks": {"type": "array", "items": {"type": "object", "properties": {"criterion": {"type": "integer"}, "kind": {"type": "string"}, "arg": {"type": "string"}}}, "description": "hub-run checks by number: pr_merged, ci_green or deployed"},
    "add": {"type": "array", "items": {"type": "string"}},
    "drop": {"type": "array", "items": {"type": "integer"}},
    "edit": {"type": "array", "items": {"type": "object", "properties": {"number": {"type": "integer"}, "text": {"type": "string"}}}},
    "expect_minutes": {"type": "integer", "minimum": 0, "description": "minutes it should take from now; 0: as it is"}
  }
}
```

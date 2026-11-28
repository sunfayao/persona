Finish a supervision wake-up without cluttering the user's main conversation. Use visibility="silent" when no user decision or useful result needs showing, with a short summary of what you checked or changed; then end the turn. visibility="user" shows the turn without a notification. notify_user, ask_user and task_review user cards also make it visible; a silent finish after that stays visible. batch: the id on the message's persona-supervision tag (default: the oldest open one). Only for supervision-triggered turns, not normal user messages.

```json
{
  "type": "object",
  "required": ["visibility", "summary"],
  "properties": {
    "visibility": {"type": "string", "enum": ["silent", "user"]},
    "summary": {"type": "string", "minLength": 1, "maxLength": 1000},
    "batch": {"type": "string"}
  },
  "additionalProperties": false
}
```

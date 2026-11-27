Create or change recurring work at a time of day, week or month: "every morning at 8", "weekdays at 18:00". One-off and interval work is schedule_followup. Example: {"title": "Morning briefing", "time": "08:00", "weekdays": [1, 2, 3, 4, 5], "prompt": "Tell the user what needs them today"}. Each run opens its own conversation with prompt, so make it self-contained. Use time (with weekdays) or a 5-field cron, in the owner's time zone, and check the next runs in the reply. To change a routine, pass its id from list_followups and only the fields to change; enabled: false pauses it. Get the owner's consent before creating or changing recurring work.

Details: read .claude/skills/persona-work/references/schedules.md in your working directory.

```json
{
  "type": "object",
  "properties": {
    "todo_id": {
      "type": "string",
      "description": "a supervised to-do this conversation advances; empty clears it"
    },
    "fresh_each_run": { "type": "boolean" },
    "id": {
      "type": "string"
    },
    "title": {
      "type": "string"
    },
    "prompt": {
      "type": "string"
    },
    "time": {
      "type": "string",
      "description": "HH:MM, 24-hour"
    },
    "weekdays": {
      "type": "array",
      "items": {
        "type": "integer",
        "minimum": 1,
        "maximum": 7
      },
      "description": "1 = Monday … 7 = Sunday; omit for every day"
    },
    "cron": {
      "type": "string",
      "description": "instead of time: minute hour day-of-month month day-of-week, or @daily etc."
    },
    "enabled": {
      "type": "boolean"
    },
    "report": {
      "type": "boolean",
      "description": "a daily report that opens with Persona's north-star lines"
    },
    "push": {
      "type": "string",
      "enum": ["every", "asked"],
      "description": "every (new default): each run's answer is pushed; asked: only what a run sends with notify_user"
    },
    "inbox": {
      "type": "string",
      "enum": ["off", "worthy", "every"],
      "description": "only when the owner asked for Inbox rows"
    },
    "run_if": {
      "type": ["object", "null"],
      "description": "owner-approved shell check before each run: exit 0 runs, 1 skips; null removes it",
      "properties": {
        "command": {"type": "string", "maxLength": 4096},
        "timeout_s": {"type": "integer", "minimum": 1, "maximum": 60, "default": 30},
        "on_error": {"type": "string", "enum": ["run", "skip"], "default": "run"}
      },
      "required": ["command"],
      "additionalProperties": false
    },
    "until": {
      "type": ["string", "null"],
      "description": "end: RFC 3339 or 30m / 2h / 1d; null clears it"
    },
    "launcher": {
      "type": "string",
      "description": "its own launcher instead of Main chat's; empty: Main chat's again"
    },
    "model": {
      "type": "string"
    },
    "effort": {
      "type": "string",
      "enum": ["low", "medium", "high", "xhigh", "max"]
    }
  }
}
```

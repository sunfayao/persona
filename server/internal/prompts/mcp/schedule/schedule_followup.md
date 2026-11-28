Schedule a one-off reminder or follow-up ("remind me at 15:00", "check again in an hour", "tell me when X finishes") or work repeated on an interval ("every 30 minutes"); a time of day or week is a routine, not a follow-up. Example: {"at": "1h", "title": "Login fix CI", "prompt": "Check whether CI on the login fix passed and tell the user"}. At that time prompt comes back to this conversation (with new_conversation: true, to a conversation of its own), so make it self-contained. at takes RFC 3339 or 30m / 2h / 1d. To repeat, set every (5m to 7d) or a 5-field cron once and never schedule the next run yourself; check the next runs in the reply. Get the owner's consent before creating or changing recurring work.

Details: .claude/skills/persona-work/references/schedules.md in the working dir.

```json
{
  "type": "object",
  "required": ["prompt"],
  "properties": {
    "todo_id": {
      "type": "string",
      "description": "repeats only: a supervised to-do this conversation advances; empty clears it"
    },
    "fresh_each_run": { "type": "boolean" },
    "at": {
      "type": "string",
      "description": "when; with every, the first run (default: one interval from now)"
    },
    "every": {
      "type": "string",
      "description": "repeat this often, e.g. 30m"
    },
    "cron": {
      "type": "string",
      "description": "instead of at / every: minute hour day-of-month month day-of-week"
    },
    "prompt": {
      "type": "string"
    },
    "title": {
      "type": "string"
    },
    "new_conversation": {
      "type": "boolean"
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
    "until": {
      "type": ["string", "null"],
      "description": "end of a repeat: RFC 3339 or 30m / 2h / 1d; null clears it"
    },
    "launcher": {
      "type": "string",
      "description": "its own launcher instead of Main chat's; needs new_conversation: true"
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

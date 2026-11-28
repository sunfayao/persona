Review a direct worker of your to-do. accepted (outcome done) closes verified work: one met check with evidence per done_when, by number or text. rework needs a concrete gap. Repeated or omitted rework kinds cap at two stalled rounds; substantive ones at ten. needs_user sends a decision up (reason, recommend). cancelled closes an unwanted one.

```json
{
  "type": "object",
  "required": [
    "todo_id",
    "verdict"
  ],
  "additionalProperties": false,
  "properties": {
    "todo_id": {
      "type": "string",
      "description": "the worker's task id"
    },
    "verdict": {
      "type": "string",
      "enum": ["accepted", "rework", "needs_user", "cancelled"]
    },
    "verify": {"type": "boolean", "description": "run the hub's declared checks again; no verdict"},
    "outcome": {"type": "string", "enum": ["done"]},
    "rework_kind": {
      "type": "string",
      "enum": ["external_input", "substantive_new", "repeated_error", "low_roi_edge"],
      "description": "rework, optional: external_input (outside input; skips the stalled cap), substantive_new (new defect after the gap was fixed; resets it), repeated_error/low_roi_edge (default)"
    },
    "note": {
      "type": "string",
      "description": "rework: what is missing, written to the worker; accepted and cancelled: one line of why"
    },
    "checks": {
      "type": "array",
      "maxItems": 6,
      "items": {
        "type": "object",
        "required": ["met"],
        "additionalProperties": false,
        "properties": {
          "number": {"type": "integer"},
          "criterion": {"type": "string", "description": "one of the task's done_when"},
          "met": {"type": "boolean"},
          "evidence": {"type": "string", "description": "what shows it, or what is missing"}
        }
      },
      "description": "each done_when with whether it is met, and the evidence"
    },
    "reason": {
      "type": "string",
      "description": "needs_user: one sentence on what is to be decided"
    },
    "recommend": {
      "type": "string",
      "description": "needs_user: the option you recommend"
    }
  }
}
```

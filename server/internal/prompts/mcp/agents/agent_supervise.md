Main can adopt an Agent on no open task for supervision and task_review. Omit role for a worker. A leader needs done_when and a non-work-trust computer. It gets instructions to plan and review direct workers, who cannot delegate. On your open task, use task_scope for done_when/expect_minutes.

```json
{
  "type": "object",
  "required": [
    "machine",
    "session_id"
  ],
  "properties": {
    "machine": {
      "type": "string",
      "description": "agents_list machine id or name"
    },
    "session_id": {
      "type": "string",
      "description": "Agent session id or first 8 characters"
    },
    "title": {
      "type": "string",
      "description": "the task in a short line ({{lang .Lang}}, under 20 characters); empty: the agent's title"
    },
    "role": {
      "type": "string",
      "enum": ["leader"]
    },
    "done_when": {
      "type": "array",
      "maxItems": 6,
      "items": {
        "type": "string"
      },
      "description": "checkable completion criteria"
    },
    "expect_minutes": {
      "type": "integer",
      "description": "estimated minutes from now"
    }
  }
}
```

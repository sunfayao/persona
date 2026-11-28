Main can adopt an Agent on no open task for supervision and task_review. Omit role for a worker. A leader needs done_when and a non-work-trust computer. It gets instructions to plan and review direct workers, who cannot delegate. On your open task, use task_scope for done_when/expect_minutes. With todo_id and action, an Agent becomes a member of an open to-do you supervise (attach; done_when, by number or text, are the criteria it answers for), leaves it and keeps running (detach), or moves from its to-do to todo_id (move); several members work on one to-do, each on its own part. The last Agent working on a to-do in progress cannot leave it.

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
    "todo_id": {
      "type": "string",
      "description": "an open to-do you supervise"
    },
    "action": {
      "type": "string",
      "enum": ["attach", "detach", "move"],
      "description": "of the Agent on todo_id; default attach"
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

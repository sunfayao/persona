Send a message to one of your open workers' Agent: the answer to its question, a nudge to finish or to report, what is still missing, "continue" with what was left after its turn was cut off or its computer restarted, or when to rebase and merge so the workers' changes go in one after another. A closed Agent is resumed by the message. No new work: a new part is a new worker (task_plan). Your own workers only.

```json
{
  "type": "object",
  "required": ["todo_id", "text"],
  "additionalProperties": false,
  "properties": {
    "todo_id": {
      "type": "string",
      "description": "the worker's to-do id"
    },
    "session_id": {
      "type": "string",
      "description": "one of several members"
    },
    "text": {
      "type": "string",
      "description": "the message"
    }
  }
}
```

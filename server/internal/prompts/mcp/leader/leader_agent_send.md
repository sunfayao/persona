Send a message to one of your open workers' Agent: the answer to its question, a nudge to finish or to report, what is still missing, "continue" with what was left after its turn was cut off or its computer restarted, or when to rebase and merge so the workers' changes go in one after another. A closed Agent is resumed by the message. It carries no new work: a new part of your to-do is a new worker (task_plan). It reaches your own workers only.

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
    "text": {
      "type": "string",
      "description": "the message, for the worker to act on"
    }
  }
}
```

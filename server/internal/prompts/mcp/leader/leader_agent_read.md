Read the recent exchanges of one of your workers' Agent: what it was told, what it said, and its tool calls in short form. Use it before you review a worker (check its evidence yourself), to see why it stopped or what it waits for, and before you message it. todo_id is the worker's to-do, as its supervision message names it; turns: recent exchanges (default 2, at most 10). Your own workers only.

```json
{
  "type": "object",
  "required": ["todo_id"],
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
    "turns": {
      "type": "integer",
      "minimum": 1,
      "maximum": 10,
      "description": "recent exchanges; default 2"
    }
  }
}
```

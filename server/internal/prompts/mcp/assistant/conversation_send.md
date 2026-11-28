Send a native follow-up to another conversation, even mid-turn. Replies use conversation_send. Few hops per user message (main: no cap; a wake-up resets; a first reply adds none). Past the rate it queues: the result says when. Ack or FYI only: wake false. Follow policy refusals; don't retry. Owner-verbatim words to main: start with them. Handing on a user request: set quote (free once).

```json
{
  "type": "object",
  "required": ["id", "text"],
  "properties": {
    "id": {
      "type": "string",
      "description": "full id or its first 8 characters"
    },
    "text": {
      "type": "string"
    },
    "quote": {
      "type": "string",
      "description": "\"latest\" (this turn), a message id or the user's exact words"
    },
    "wake": {
      "type": "boolean",
      "description": "false: no turn; read with the next one"
    }
  }
}
```

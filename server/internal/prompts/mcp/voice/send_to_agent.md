Send a message to an Agent, as if the user typed it into its input and pressed Enter. Use only when the user explicitly asks. If the recipient and content are clear, send directly without repeating them or waiting for confirmation; ask first if you are unsure which Agent. machine_id and session_id come from list_agents.

```json
{
  "type": "object",
  "required": [
    "machine_id",
    "session_id",
    "text"
  ],
  "properties": {
    "machine_id": {
      "type": "string"
    },
    "session_id": {
      "type": "string"
    },
    "text": {
      "type": "string",
      "description": "The message to send"
    }
  }
}
```

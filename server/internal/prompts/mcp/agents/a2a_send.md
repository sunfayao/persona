Send a text request to another Persona session, start a new Agent, or supply input on an existing A2A task. Use a new message_id for each message and reuse it on retries. Set machine_id and session_id for an existing session. Set machine_id and cwd to start an Agent, with an optional launcher. Give tier to start it on one of the user's Agent presets, which sets its launcher, model, effort and speed but not its permission mode; a2a_sessions with presets true lists each computer's presets, what they run and when to use them. For a reply set task_id. With wake enabled (the default), questions and results return to your session after your current turn ends. You can stop your turn while waiting.

```json
{
  "type": "object",
  "required": [
    "message_id",
    "text"
  ],
  "properties": {
    "machine_id": {
      "type": "string"
    },
    "session_id": {
      "type": "string"
    },
    "task_id": {
      "type": "string"
    },
    "context_id": {
      "type": "string"
    },
    "message_id": {
      "type": "string"
    },
    "text": {
      "type": "string",
      "maxLength": 12000
    },
    "wake": {
      "type": "boolean"
    },
    "cwd": {
      "type": "string"
    },
    "launcher": {
      "type": "string"
    },
    "tier": {
      "type": "string"
    }
  }
}
```

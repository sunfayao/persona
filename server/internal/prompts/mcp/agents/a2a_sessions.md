List Persona sessions enrolled for A2A communication, optionally on one machine. Use the exact machine_id and session_id when sending. With presets true the answer is {sessions, computers}: the computers a2a_send can start an Agent on, yours included, each with the Agent presets (tier) it can run, what each runs and when to use it.

```json
{
  "type": "object",
  "properties": {
    "machine_id": {
      "type": "string"
    },
    "presets": {
      "type": "boolean"
    }
  }
}
```

Read the recent conversation of one agent (from agents_list): the user's messages, what the agent said, tool calls in short form. `turns` = how many recent exchanges (default 2, max 10). Use it to answer the user's follow-ups about work an agent or task already did (its conclusion, a PR link, why it stopped) instead of asking the agent with agent_send, which makes it take another turn while the user waits (and a stopped agent takes no messages: it is read-only). With `conversation` instead of machine and session_id it reads another of your Persona conversations (from agents_list kind conversations), cut the same way.

```json
{
  "type": "object",
  "properties": {
    "machine": {
      "type": "string"
    },
    "session_id": {
      "type": "string",
      "description": "full id or its first 8 characters"
    },
    "turns": {
      "type": "integer"
    },
    "conversation": {
      "type": "string",
      "description": "a Persona conversation's id, or its first 8 characters, instead of machine and session_id"
    }
  }
}
```

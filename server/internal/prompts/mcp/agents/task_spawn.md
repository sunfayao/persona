Use this only to hand a separate part of this assignment to a sidebar Agent. You must never hand over all of it, and do the rest yourself. The child joins the same to-do with no more access than you have and reports to your supervisor, not to you. Leave the files it changes alone, and do not poll it, read its transcript or wait for it. Use this instead of standalone thread tools like Codex create_thread. Omit launch options to inherit your run. Do not retry an uncertain start.

```json
{
  "type": "object",
  "properties": {
    "title": {
      "type": "string",
      "description": "Short title for this part of the existing task"
    },
    "prompt": {
      "type": "string",
      "description": "Assignment and evidence to return"
    },
    "launcher": {
      "type": "string",
      "description": "Launcher on this computer, e.g. traex-ferry"
    },
    "model": {
      "type": "string",
      "description": "Model ID for the launcher"
    },
    "effort": {
      "type": "string",
      "description": "Thinking level, e.g. xhigh"
    },
    "tier": {
      "type": "string",
      "description": "Agent preset from the active model profile"
    }
  },
  "required": ["title", "prompt"],
  "additionalProperties": false
}
```

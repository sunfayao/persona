Hand work to the Persona main conversation: the full assistant can use all the user's data and tools (calendar, mail, Lark, documents, computers, scheduled tasks, writing and more). It can do much more than you, but takes tens of seconds to several minutes. Use it when your own tools cannot answer or the user needs something done. The call does not return immediately: tell the user you have handed it to the main conversation, then keep talking normally. When that turn finishes, this call returns its answer, possibly minutes later; tell the user then. Make task clear and self-contained, with the work to do in its first sentence: the main conversation cannot see this voice conversation's context.

```json
{
  "type": "object",
  "required": [
    "task"
  ],
  "properties": {
    "task": {
      "type": "string",
      "description": "The work for the main conversation, in one or more complete sentences"
    }
  }
}
```

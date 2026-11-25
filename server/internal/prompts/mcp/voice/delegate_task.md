Hand work to the conversation bound to this call (identified in the instructions). Calling sends it immediately, without repeating it or waiting for confirmation, so ask first if you are unsure the user wants it done. The call does not return immediately: tell the user you handed it over, then keep talking normally. When that turn finishes, this call returns its answer, possibly minutes later; tell the user then. Put the work in task's first sentence as a complete instruction: the conversation cannot see this voice context. To ask what it is doing or how far it has got, start task with the exact prefix “Status:” (not sent to it; returns its current status). When the user explicitly asks it to stop, use the exact prefix “Stop:”. When the user is answering its question, use the exact prefix “Answer:”.

```json
{
  "type": "object",
  "required": [
    "task"
  ],
  "properties": {
    "task": {
      "type": "string",
      "description": "The work in complete sentences, or a sentence starting with the exact prefix “Status:”, “Stop:” or “Answer:”"
    }
  }
}
```

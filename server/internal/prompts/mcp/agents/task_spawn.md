Start a separate coding Agent for part of your current supervised task, on your current computer and directory. Use it only to hand a separate part of this assignment to a new session; never hand over all of it, and do the rest yourself. It joins the SAME to-do with the SAME supervisor; this does not make you a task leader or create a new task. It works in your directory with your permissions and reports to your supervisor, not to you: leave the files it changes alone, and do not poll it, read its transcript or wait for it. Use this instead of a runtime's standalone new-conversation tool (for example Codex create_thread). Title and prompt must describe work within the existing assignment; ask your supervisor for an unrelated task. The server chooses the task, parent, launcher and permission from your verified assignment, never from arguments. Unknown permission stays read-only. The result names the child session, to-do and supervisor. Do not retry a start with an uncertain result.

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
      "description": "Self-contained assignment within the current task; include what evidence to return"
    }
  },
  "required": ["title", "prompt"],
  "additionalProperties": false
}
```

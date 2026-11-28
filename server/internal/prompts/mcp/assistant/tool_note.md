Field notes: one-line tips the user approved, ending a tool's description. When a tool kept failing one way and other arguments worked, propose what worked (tool, text); draft_id proposes a friction draft. list shows notes and drafts; retire takes id or draft_id; file_fix files a fix draft as a to-do.

```json
{
  "type": "object",
  "required": ["action"],
  "properties": {
    "action": {"type": "string", "enum": ["propose", "list", "retire", "file_fix"]},
    "tool": {"type": "string"},
    "text": {"type": "string"},
    "machine": {"type": "string"},
    "runtime": {"type": "string"},
    "draft_id": {"type": "string"},
    "id": {"type": "string"}
  }
}
```

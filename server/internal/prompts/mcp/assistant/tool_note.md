Field notes are one-line tips at the end of a tool's description in every assistant conversation (at most 3 a tool), each added only when the user approves its card. When a tool keeps failing the same way and a retry with other arguments worked, `propose` a note that says what works: one line of at most 200 characters, with no secret, path, address or computer name (`machine` scopes it to one computer, `runtime` to one runtime). `list` shows the notes, the friction drafts Persona made of repeated failures, and the notes due for review. A note draft is proposed by its `draft_id`; `file_fix` makes a fix draft (Persona's own defect) a to-do; `dismiss` sets a draft aside. A note leaves the descriptions at its review date unless renewed (`renew`, after a card), and retires itself when its tool's description changes; `retire` takes one out at once.

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["action"],
  "properties": {
    "action": {"type": "string", "enum": ["propose", "list", "retire", "dismiss", "renew", "file_fix"]},
    "tool": {"type": "string", "description": "propose: the tool the note is for. list: only this tool's notes and drafts."},
    "text": {"type": "string", "maxLength": 200, "description": "propose: the note, saying what to do. With draft_id: replaces the draft's text."},
    "machine": {"type": "string", "description": "propose: the computer the note holds on, when it holds only there."},
    "runtime": {"type": "string", "enum": ["claude", "codex", "agy"], "description": "propose: only conversations on this runtime get the note."},
    "evidence": {"type": "string", "maxLength": 300, "description": "propose: what showed it (the failures, the retry that worked)."},
    "review_days": {"type": "integer", "minimum": 1, "maximum": 180, "description": "propose, renew: days until the note is due for review (default 90)."},
    "draft_id": {"type": "string", "description": "propose, dismiss, file_fix: a friction draft from list."},
    "id": {"type": "string", "description": "retire, renew: the note's id."},
    "reason": {"type": "string", "description": "retire: why it no longer holds."},
    "status": {"type": "string", "enum": ["proposed", "approved", "due", "retired"], "description": "list: only notes in this state (retired ones are listed only so)."}
  }
}
```

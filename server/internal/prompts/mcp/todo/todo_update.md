Update supplied title, notes, reminder, project, part_of or after immediately, including task to-dos; no confirmation. Omitted fields stay; "" clears notes/remind/project/part_of, [] clears after. New reminder times notify again. Completion/reopening uses todo_complete/todo_reopen.

```json
{
  "type": "object",
  "required": [
    "id"
  ],
  "properties": {
    "id": {
      "type": "string",
      "description": "To-do id from todo_list."
    },
    "title": {
      "type": "string",
      "description": "the new title"
    },
    "notes": {
      "type": "string",
      "description": "Replacement notes; retain anything still needed. \"\" clears."
    },
    "remind": {
      "type": "string",
      "description": "New reminder datetime (user's time zone/RFC3339) or relative 30m/2h/1d; never past. \"\" clears."
    },
    "project": {
      "type": "string",
      "description": "Project label, preferably a long-term work area. Set only when clear; reuse an existing one, do not invent near-duplicates. \"\" clears."
    },
    "part_of": {
      "type": "string",
      "description": "Request to-do this is a piece of; changing clears after."
    },
    "after": {
      "type": "array",
      "items": {
        "type": "string"
      },
      "description": "Sibling piece ids it waits for; replaces old."
    }
  }
}
```

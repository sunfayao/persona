Save a short procedure worked out in this conversation as a skill on the assistant's computer, once the user approves it on a card. Use it for steps the user wants followed again in later conversations; a fact or a preference belongs in memory, and a skill with scripts or other files is a ZIP for skill_import. Persona writes the front matter from name and description; the body is the Markdown procedure alone. Text with a secret, an e-mail or network address, a URL, a home-directory path or a computer's name is refused with those parts marked: rewrite them. A name in use is refused. New conversations load the skill. Returns a receipt: skill_import undo with its reference and environment takes the skill back within ten minutes.

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["name", "description", "body"],
  "properties": {
    "name": {"type": "string", "maxLength": 64, "pattern": "^[a-z0-9]+(-[a-z0-9]+)*$", "description": "The skill's directory name, such as deploy-checklist."},
    "description": {"type": "string", "maxLength": 200, "description": "One line: what the skill is for and when to use it."},
    "body": {"type": "string", "description": "The procedure in Markdown, without front matter; at most 8 KiB."}
  }
}
```

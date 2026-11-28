Ask the user multiple-choice questions on a card in Persona and wait for the answers. Use it to confirm anything risky (sending messages or email for them, deleting, changing live systems) or to let them choose. Your runtime's own question tool does not reach the user. Use wait false only where your instructions say to; a confirmation this turn needs (approving a message before it is sent) keeps the default.

Details: read .claude/skills/persona-work/references/ask-user.md in your working directory.

```json
{
  "type": "object",
  "required": ["questions"],
  "properties": {
    "wait": {
      "type": "boolean",
      "description": "false: put the card up and return at once; the answer comes later as a new message"
    },
    "deadline": {
      "type": "string",
      "description": "until when they wait: \"4h\" or an RFC 3339 time; needs reversible and a recommend on every question"
    },
    "reversible": {
      "type": "boolean",
      "description": "with deadline: whether taking the recommended options can be undone; false for deleting, sending to others, spending money or changing production"
    },
    "questions": {
      "type": "array",
      "minItems": 1,
      "maxItems": 4,
      "items": {
        "type": "object",
        "required": ["question", "options"],
        "properties": {
          "question": {
            "type": "string"
          },
          "header": {
            "type": "string",
            "description": "short label, ≤12 chars"
          },
          "multiSelect": {
            "type": "boolean"
          },
          "recommend": {
            "type": "string",
            "description": "the label of the option you recommend"
          },
          "options": {
            "type": "array",
            "minItems": 2,
            "maxItems": 4,
            "items": {
              "type": "object",
              "required": ["label"],
              "properties": {
                "label": {
                  "type": "string"
                },
                "description": {
                  "type": "string"
                }
              }
            }
          }
        }
      }
    }
  }
}
```

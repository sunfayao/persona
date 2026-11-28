Skills on the main conversation's computer; each install is the user's to approve on their own card, never yours. `save` keeps a short procedure worked out in this conversation as a skill (name, description, and a Markdown body without front matter); facts and preferences go to memory instead. `import` takes a skill ZIP the user asked for through Persona's Settings importer, by `op`: preview the base64 archive (at most 4 MiB compressed) for validated metadata, a reference and an environment; install with the same archive, reference and environment; status after a lost or uncertain answer, before any retry; cancel an unused preview; undo an installed receipt. A same-name skill is never overwritten, no URL is fetched and no script runs; skills load in new conversations.

```json
{
  "type": "object",
  "required": [
    "action"
  ],
  "properties": {
    "action": {
      "type": "string",
      "enum": [
        "import",
        "save"
      ],
      "description": "save: keep a procedure from this conversation; import: a skill ZIP, step by step with op"
    },
    "op": {
      "type": "string",
      "enum": [
        "preview",
        "install",
        "status",
        "cancel",
        "undo"
      ]
    },
    "archive": {
      "type": "string",
      "maxLength": 5592408,
      "description": "Base64 ZIP bytes, required only for preview and install. Supply identical bytes for both; do not put secrets in the archive."
    },
    "reference": {
      "type": "string",
      "maxLength": 256,
      "description": "Opaque receipt from preview/install, required except for preview. Never invent one."
    },
    "environment": {
      "type": "string",
      "maxLength": 128,
      "description": "Environment returned by preview, required except for preview. A changed assistant computer requires a fresh preview."
    },
    "name": {
      "type": "string"
    },
    "description": {
      "type": "string"
    },
    "body": {
      "type": "string"
    }
  },
  "additionalProperties": false
}
```

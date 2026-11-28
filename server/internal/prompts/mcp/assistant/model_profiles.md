Model profiles: model settings only, never credentials. `list`: the profiles, the current one, its changed fields, the rollback preview and recent changes. `get` with `id`: one profile's purpose bindings, launcher model fields and Agent presets (change a preset with persona_settings key model_profiles.tiers). `switch` with `id`, only when the user asks: it applies at the next model call or Agent start, interrupts nothing and keeps a before-image. `rollback`: undoes the last switch; a second rollback redoes it.

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
        "list",
        "get",
        "switch",
        "rollback"
      ],
      "description": "list, get one, switch to one, or roll back the last switch"
    },
    "id": {
      "type": "string",
      "description": "get, switch: the profile's id from list"
    }
  },
  "additionalProperties": false
}
```

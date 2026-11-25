Request a shared CI lease for a project, full commit and group set. Returns granted, queued, joined, cached or refused. Only the granted holder submits; joined callers read the existing result.

```json
{
  "type": "object",
  "properties": {
    "resource": {
      "type": "string"
    },
    "project": {
      "type": "string"
    },
    "commit": {
      "type": "string"
    },
    "groups": {
      "type": "array",
      "items": {
        "type": "string"
      }
    },
    "lease_id": {
      "type": "string"
    },
    "holder": {
      "type": "object",
      "properties": {
        "label": {
          "type": "string"
        },
        "pid": {
          "type": "integer"
        }
      }
    }
  },
  "required": [
    "resource",
    "project",
    "commit",
    "groups"
  ]
}
```

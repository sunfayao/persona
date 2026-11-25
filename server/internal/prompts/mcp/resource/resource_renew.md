Renew this session’s queued or granted lease. Use token 0 while queued and the fencing token once granted. Stop the runner client when fenced.

```json
{
  "type": "object",
  "properties": {
    "resource": {
      "type": "string"
    },
    "lease_id": {
      "type": "string"
    },
    "token": {
      "type": "integer"
    }
  },
  "required": [
    "resource",
    "lease_id",
    "token"
  ]
}
```

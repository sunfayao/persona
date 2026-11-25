Cancel a queued lease owned by this session. Joined callers cannot cancel the holder’s work.

```json
{
  "type": "object",
  "properties": {
    "resource": {
      "type": "string"
    },
    "lease_id": {
      "type": "string"
    }
  },
  "required": [
    "resource",
    "lease_id"
  ]
}
```

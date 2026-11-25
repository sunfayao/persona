Read a lease owned by or joined by this session; never submit again while waiting.

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

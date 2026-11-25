List your Persona conversations, newest activity first: id, title, last activity, main/running state and last-message gist. Archived ones need `include_archived`; this conversation is marked. Use it to identify the user's conversation before reading or messaging it.{{template "shared/mcp_language" .}}

```json
{
  "type": "object",
  "properties": {
    "query": {
      "type": "string",
      "description": "case-insensitive match on title or last message"
    },
    "include_archived": {
      "type": "boolean",
      "description": "include archived conversations (marked as such)"
    },
    "limit": {
      "type": "integer",
      "description": "default 30, max 100"
    }
  }
}
```

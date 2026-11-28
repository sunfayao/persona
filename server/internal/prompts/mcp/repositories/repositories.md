This assistant's saved repository references. `list` searches their names, aliases, full paths and purposes to resolve repository identity (query, cursor, limit); `get` with `id` reads one when its details are useful: provider, instance, full path, canonical URL, aliases, purpose, verified_at. `add` saves one when the user asks, after their permission card (url, and optional provider, name, aliases, purpose); duplicates and URL credentials are refused, and after an uncertain add look with `list` before retrying. The user's explicit repository URL or path takes precedence. A reference is not authorization or an allowlist; never infer a machine or local checkout path. Saved metadata only: nothing here checks the remote.

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
        "add"
      ],
      "description": "list (search), get one by id, or add one on the user's request"
    },
    "query": {
      "type": "string",
      "maxLength": 200,
      "description": "Optional case-insensitive substring of a saved name, alias, full path or purpose; omit to browse. Preserve explicit user repository choices."
    },
    "cursor": {
      "type": "string",
      "maxLength": 1024,
      "description": "Opaque next_cursor from this assistant's previous page with the same query. Restart without it if the saved references changed."
    },
    "limit": {
      "type": "integer",
      "minimum": 1,
      "maximum": 50,
      "description": "Results per page, default 20; at most 50."
    },
    "id": {
      "type": "string",
      "minLength": 1,
      "maxLength": 80,
      "description": "get: the reference's id from list"
    },
    "url": {
      "type": "string",
      "minLength": 1,
      "maxLength": 2048,
      "description": "Repository URL, repository page, or git SSH remote without credentials."
    },
    "provider": {
      "type": "string",
      "enum": [
        "github",
        "gitlab"
      ],
      "description": "Required for a self-hosted GitHub/GitLab; other providers are detected from the host."
    },
    "name": {
      "type": "string",
      "maxLength": 80
    },
    "aliases": {
      "type": "array",
      "maxItems": 10,
      "items": {
        "type": "string",
        "minLength": 1,
        "maxLength": 80
      }
    },
    "purpose": {
      "type": "string",
      "maxLength": 500
    }
  },
  "additionalProperties": false
}
```

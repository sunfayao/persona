Read your open todo's decisions in force with `{}`. The `by` field identifies user and agent decisions. Reread after compaction. For engineering knowledge scoped to your todo's project, use `knowledge_search` with a query or `knowledge_read` with a returned path.

```json
{
  "type": "object",
  "properties": {
    "action": {"type": "string", "enum": ["knowledge_search", "knowledge_read"]},
    "query": {"type": "string"},
    "path": {"type": "string"}
  }
}
```

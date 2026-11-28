Live cards in this assistant conversation. `show` creates a persistent card from a View (the persona-view schema: sections of components, a required plain-text fallback, optional named pages; at most 64 KB a card and 200 a conversation) and returns {id, version}: keep the id. `update` replaces a card this conversation made with a complete View: give the version `get` read to refuse a stale update (a validation error or a conflict leaves the card as it was) and keep component keys stable. `get` reads a card of this conversation: its View, version and the user's actions (replies and submitted values, with their delivery state). A card sends no push: keep the conclusion in your reply too. Never collect passwords or secrets in a card; an html component's persona.reply and persona.open need a key and the user's confirmation.

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
        "show",
        "update",
        "get"
      ],
      "description": "show: a new card; update: replace one of this conversation's cards; get: read one"
    },
    "id": {
      "type": "string",
      "description": "update, get: the card's id from show, with its view_ prefix"
    },
    "view": {
      "type": "object",
      "description": "A View from schema/extension/view.schema.json with a required plain-text fallback and sections. Only assistant reply/open/push.page/pop actions, not extension ui.action ids."
    },
    "version": {
      "type": "integer",
      "minimum": 1,
      "maximum": 9007199254740991,
      "description": "Expected version from get; a stale version returns a conflict instead of overwriting."
    }
  },
  "additionalProperties": false
}
```

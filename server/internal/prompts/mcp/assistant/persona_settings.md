This is how the user changes Persona's settings: ask them in conversation, there is no settings page for these. `list` (the default) shows every key with a one-line description, how to set it and its current value (secrets only as set or unset); `get` reads one. `set`, `add` and `remove` show before → after as a confirmation card in Persona and change nothing until they approve; secrets are entered there, never in chat. Change only what was asked. Areas: assistant behavior (tasks, supervision and close-out, idle compaction and release, auto-naming, cache keep-alive, suggestions, folds, the computer and launcher the main chat runs on), session triage (`agents.triage.*`), About Me, saved prompts, start page and sidebar, inbox sorting and task links, mail accounts, extensions (`extension.<id>`, which include channels and connectors) and their download mirrors, hub update channel and auto-update, device notifications, each agent CLI's own settings per computer (`agent_settings.<cli>`) and muted Claude config directories. Model profiles are not here: use `model_profiles_list`, `model_profile_get`, `model_profile_switch` and `model_profile_rollback`.

Model groups `model_config.kb`, `model_config.inbox.triage`, `model_config.agent.delegate` and `model_config.translate`: `get` returns their effective binding, revision and compatible `purpose.provider_ids`. `set` and `test` take one listed provider ID as value. A changed provider clears the old model, effort and permission mode. `test` only checks the connection, without approval or saving; an unverified or offline provider may still be saved.

`assistant.standing_authorizations`: only the main conversation may replace the list at its current revision with the user’s approval in Persona. Each grant names `machine_id`, absolute `directory`, `actions` (merge, push, deploy-test, deploy-production, send, delete, spend), `conditions`, exact user `quote`, `source` and `reversible`. Keep unchanged grants by ID; replace one by removing it and adding it without an ID. Never invent a quote or expand scope. Only independently verifiable conditions may permit automatic answers. Revoke in Settings. Tool permissions are never automatically approved.

```json
{
  "type": "object",
  "properties": {
    "action": {
      "type": "string",
      "enum": ["list", "get", "set", "test", "add", "remove"],
      "description": "default list"
    },
    "key": {
      "type": "string",
      "description": "the setting, from list (assistant.*, agents.triage.*, navigation.*, hub.*, inbox.rules, mail.accounts, extension.<id>…)"
    },
    "value": {
      "type": ["string", "number", "integer", "boolean", "object", "array", "null"],
      "description": "set: the new value; test: the provider ID to check; add: the item (each setting's list entry says what)"
    },
    "id": {
      "type": "string",
      "description": "remove: which item (from get)"
    }
  },
  "additionalProperties": false
}
```

# Persona's settings with `persona_settings`

The tool's description holds what every call needs. This page holds the rest: what the settings cover, extensions, model groups and standing authorizations. A refusal says what to change; never send the same call again unchanged.

## Reading and changing

- `list` (the default) shows every key with a one-line description, the actions it takes, how to set it and its current value (secrets only as set or unset); `get` reads one whole. Reading is free.
- `set`, `add` and `remove` apply immediately for non-secret settings and return a before → after summary. Only changes that require the owner to enter an unknown secret (such as adding a mail account) show a confirmation card in Persona; secrets are entered on that card, never in chat. A card declined, closed or left unanswered changes nothing; one nobody answered is not a decline, so say the change waits for their approval.
- Change only what the owner asked for.

## What the settings cover

- Assistant behavior (`assistant.*`): tasks, supervision and close-out, idle compaction and release, auto-naming, cache keep-alive, suggestions, folds, and the computer and launcher the main conversation runs on.
- Session triage (`agents.triage.*`), About Me, saved prompts, the start page and sidebar.
- The Inbox's sorting rules and task links (`inbox.rules`, `inbox.agent_rules`), mail accounts (`mail.accounts`).
- Extensions (`extension.<id>`, channels and connectors included) and their download mirrors.
- The hub's update channel and auto-update (`hub.*`), device notifications (`notifications.devices`), each agent CLI's own settings per computer (`agent_settings.<cli>`), and muted Claude config directories.
- Model profiles are not here: use `model_profiles` (list, get, switch, rollback). Routines and follow-ups have their own tools.

## Extensions

- An extension has its own switch, `enabled`, apart from its settings: `list` shows `enabled: true` or `enabled: false` as its current value, and `get` starts with it. While an extension is switched off it runs nothing and offers no tools, whatever its settings say: a computer listed in its settings does not get its tools (`machines_list` then shows that feature off).
- `set` takes an object of only what changes: `{"enabled": true}`, a field or several, or both, such as `{"enabled": true, "machines": ["<machine id>"]}`. `null` puts a field back to its default.
- The extension is switched immediately as on the Settings page, and the answer names the tools it brought or took away. Running conversations are sent their new tool list. An extension that others need is switched off only after them; a pack is switched on the Settings page, which switches its members with it.

## Model groups

`model_config.kb`, `model_config.inbox.triage`, `model_config.agent.delegate` and `model_config.translate`: `get` returns the effective binding, its revision and the compatible `purpose.provider_ids`. `set` and `test` take one listed provider ID as `value`. A changed provider clears the old model, effort and permission mode. `test` only checks the connection, without approval or saving; an unverified or offline provider may still be saved.

## Standing authorizations

`assistant.standing_authorizations`: only the main conversation may replace the list, at its current revision; changes apply immediately and return before → after. Each grant names `machine_id`, an absolute `directory`, `actions` (merge, push, deploy-test, deploy-production, send, delete, spend), `conditions`, the owner's exact `quote`, `source` and `reversible`.

- `get` first. Keep unchanged grants by their ID. To change one, remove it and add it again without an ID; to revoke one, replace the list without it.
- Never invent a quote or widen a grant's scope.
- Only conditions that can be checked independently may let Persona answer for the owner. Tool permissions are never approved automatically.

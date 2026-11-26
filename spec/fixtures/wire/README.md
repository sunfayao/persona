# Wire JSON samples

"Wire" here means the JSON structures the hub actually sends. All data is made up; nothing connects to a live hub or reads a real conversation. The samples are exported by [wire_samples_test.go](../../../server/internal/hub/wire_samples_test.go) and are golden samples of the hub's wire format. They follow `docs/protocol/protocol.md`.

## Files and interfaces

| File | Interface or event | How it is exported |
| --- | --- | --- |
| [connection.json](connection.json) | WS `hello` and `revs`; GET `/api/v1/health` | Calls `Hub.hello` and `Hub.health`, then puts the fixed `wireFeatures` in place of the hub's feature list; revs uses the real envelope and `client.sendJSON` |
| [sessions.json](sessions.json) | GET `/api/v1/sessions`; WS `sessions` | `proto.Session` after `agentState` and `fillStateMarks`, sent in the map structure of the list and of the events |
| [machines.json](machines.json) | GET `/api/v1/machines`; WS `machines` | Calls `Machine.snapshot`, then `writeJSON` or `sendJSON` |
| [machines-concern.json](machines-concern.json) | Anonymous machines for concern-grid tests | `writeJSON` encodes the machine input |
| [items.json](items.json) | GET `/api/v1/sessions/{mid}/{sid}/items` | `proto.Item`, `WithoutImageData`, `FoldWork`; incremental and expanded pages call `Hub.sessionItems` |
| [item_events.json](item_events.json) | WS `items`, including the backlog after a subscription | Calls `Hub.itemsToSubscribers` and `Hub.sendBacklog` and reads the client's send queue |
| [interactions.json](interactions.json) | GET `/api/v1/interactions`; WS `interaction` and `interaction_resolved` | `proto.Interaction` in the real list or envelope structure; screens go through `ParseDialog` and `SecretPrompt` |
| [inbox.json](inbox.json) | GET `/api/v1/inbox` | `InboxItem.listed` removes internal fields from the list, then `writeJSON` |
| [feed.json](feed.json) | GET `/api/v1/assistant/conversations/{id}/feed` | `answeredBody`, built by `answeredItems` and others, then `writeJSON` |
| [queue.json](queue.json) | The answer of POST `/api/v1/sessions/{mid}/{sid}/queue` (taking back, deleting and moving queued messages of a Persona conversation) | `proto.QueueEdit` as the node encodes it, written by `writeJSON`; the hub forwards the answer unchanged |
| [errors.json](errors.json) | 401 for an unauthenticated request; 409 for a refused send or screen action; 413 `images_over_limit` for a message with too many pictures | Calls `Hub.auth`, `callErr` and `apierr.ImagesOverLimit`, and asserts the HTTP status |
| [compatibility.json](compatibility.json) | Older formats, unknown fields and abnormal types of the shapes above | Built with the Go encoder as compatibility input; abnormal input is not a normal hub output |

`wireNode` serves fixed `session.items` RPC answers through an in-memory pipe; the hub still runs its real paging wrapper, image handling and backlog logic. No sample starts a full hub, node or herdr, listens on a port or touches a database.

Time is fixed at `2026-01-02T03:04:05Z`; the zero time is Go's own. Identifiers, domains, paths and message bodies are placeholders; example core counts and memory sizes are made up and are not measurements.

The export replaces two values after the production builders return: `hello.client_id`, which is random, and the feature list of `hello` and `/health`, which becomes the fixed `wireFeatures` (rules below).

## Rules and cases

Paths are relative to the repository root; function and type names locate the code. Every JSON case also has its own `source`.

| Rule | Source | Cases |
| --- | --- | --- |
| hello and a trusted health carry the hub's feature list under `features`. The samples carry a fixed list instead (`wireFeatures` in `wire_samples_test.go`): a few long-lived flags the clients read and one no client knows, `example_unknown_feature`, which decoders keep reading past. Adding or removing a feature flag changes no sample; `TestHelloAndHealthCarryTheHubsFeatures` checks that both carry exactly the hub's `features()`, `proto.Features` first and in order. | `server/internal/hub/hello.go`: `hello`, `health`; `server/internal/hub/hello_features_test.go` | `hello_features`, `health_features` |
| revs maps session identifiers to integer revisions. | the case's `source` | `revs_changed_sessions` |
| The session list keeps mode, queue, background tasks, subtasks and loop information; an event assigns the session to a machine from the machine identifier in the envelope. | `server/internal/proto/session.go`: `Session` | `sessions_list`, `sessions_event` |
| A full session row carries the provider switch waiting for the next message (`pending_switch`) and the model a Persona conversation chose for itself (`model_pick`); a summary row keeps `model_pick` and omits the launcher and `pending_switch`, which clients take from the earlier full row. | `server/internal/proto/session.go`: `PendingSwitch`, `ModelPick`; `server/internal/hub/sessions_list.go`: `runtimeSummary` | `sessions_model_switch`, `sessions_summary_model_pick` |
| A Persona conversation's `queued` lists the user's own messages first, and the last `queued_relayed` entries come from other conversations or the system; clients only show and copy those. | `server/internal/proto/session.go`: `Session`; `server/internal/assistant/fleet_sessions.go`: `Service.decorateInventory` | `sessions_assistant_queue` |
| The answer to editing a Persona conversation's queue is `{queue, images}`: `queue` is the messages that were queued before the edit, `images[i]` the pictures of message i (null where it has none; the key is omitted when no message has any); the client puts them back in the input when it takes a message back. | `server/internal/proto/session.go`: `QueueEdit`; `server/internal/node/assistant_queue.go`: `assistantEditQueue`; `docs/protocol/protocol.md`: `POST /api/v1/sessions/{mid}/{sid}/queue` | `queue_edit_images`, `queue_edit_words` |
| Online and offline machines use one type; system information and version fields are optional. | `server/internal/hub/machine.go`: `Machine.snapshot` | `machines_list`, `machines_event` |
| Items tell content blocks apart by kind; custom keys in tool input are kept as they are. | `server/internal/proto/item.go`: `Item` | `items_all_kinds` |
| An assistant item carries the model that wrote it (`model`), a user item does not; clients use it to mark where the reply model changes. | `server/internal/proto/item.go`: `Item.Model` | `items_reply_models` |
| A tool call and its result share `tool_use_id`; a failed result carries `is_error`. | `proto.Item` | `items_all_kinds` |
| A lazy image's `data` stays an empty string and carries its size and a lazy flag; an ordinary image keeps its base64. | `proto.Image.Placeholder`, `WithoutImageData` | `items_all_kinds`, `items_inline_image`, `items_live_lazy`, `items_http_after` |
| A background task notice is still a notice; `task` tells shell, monitor and agent apart, and a zero exit code can appear explicitly. | `proto.TaskNote` | `items_all_kinds`, `items_task_notifications` |
| A fold stores the count and end point of the folded items; paging an expanded range uses `more`. | `server/internal/transcript/fold.go`: `FoldWork` | `items_folded_work`, `items_fold_range` |
| A fold's `tasks` store snapshots of the folded tasks and the position of their original call (`source_item_id`, `source_tool_use_id`, `source_seq`); older clients ignore these fields. | `server/internal/transcript/fold.go`: `FoldWork`; `proto.FoldTaskSnapshot`; `docs/protocol/protocol.md`, "Folded work" | `items_folded_tasks` |
| A turn's duration is a `turn_duration` item that keeps where the duration came from and its native correlation; live delivery uses the subscribed items event, with no new event or top-level field; older clients treat it as an unknown kind. | `server/internal/transcript/transcript.go`: `ParseLine`; `proto.TurnDuration`; `Hub.itemsToSubscribers` | `items_recorded_turn_duration`, `items_live_recorded_turn_duration` |
| A live event's `from_seq` can be zero, and an empty `items` can still advance `to_seq`. | `Hub.itemsToSubscribers`; `docs/protocol/protocol.md`, "Continuity of items" | `items_live_lazy`, `items_empty_advance` |
| A latest-page backlog carries `has_more`, a catch-up backlog does not, and when the backlog limit is reached the last batch carries `more_after`. | `Hub.sendBacklog`; `docs/protocol/protocol.md`, "WebSocket" | `items_backlog_latest`, `items_backlog_catchup`, `items_backlog_capped` |
| An HTTP incremental page carries `history`, `from_seq`, `to_seq` and `more_after`. | `Hub.sessionItems` | `items_http_after` |
| question, plan, permission and screen interactions each carry their own payload; a question supports single and multiple choice. | `proto.Interaction` | `interaction_question`, `interaction_plan`, `interaction_permission`, `interaction_screen`, `interactions_list` |
| A screen dialog carries its options and `rev`; a password prompt uses `secret` and records no password input. | `server/internal/screen/dialog.go`: `ParseDialog`; `secret.go`: `SecretPrompt` | `interaction_screen`, `interaction_secret` |
| An elicitation interaction carries the MCP server name and a form (`requested_schema`) or url payload, and goes only to connections that declare the elicitation capability. | `server/internal/hub/http.go`: `Hub.broadcast`, `broadcastTo`; `proto.Elicitation` | `interaction_elicitation_form`, `interaction_elicitation_url` |
| `interaction_resolved` withdraws an interaction by id. | `server/internal/hub/hub.go`: `resolve` | `interaction_resolved` |
| The assistant's `ask_user` question card also carries `deadline`, `reversible` (absent when not said) and a `recommend` per question; clients do not model these keys and read the rest of the card as usual. | `server/internal/proto/interaction.go`: `Interaction.Deadline`, `Interaction.Reversible`, `Question.Recommend`; `server/internal/assistant/decision_deadline.go`: `keepAskDecision` | `interaction_question_decision` |
| When a question the assistant answered is withdrawn it also carries `answered_by`, `reason` and `answers` keyed by question text; the re-answer event `interaction_reanswered` carries only the changed questions, in `resolution.answers`. | `server/internal/hub/fleet_link.go`: `EventFleetResolved`; `server/internal/assistant/fleet_delegate.go`: `reanswer`; `Hub.relayBroadcastFrom`; `docs/protocol/protocol.md`, "WebSocket `/api/v1/ws`" | `interaction_resolved_assistant`, `interaction_reanswered` |
| The inbox list carries Lark and mail alike, drops internal fields such as `context` and keeps the drafted reply. | `server/internal/assistant/inbox.go`: `InboxItem.listed` | `inbox_list` |
| One made-up item carries every block an inbox item has, as the list shows it: the item's bookkeeping and each block's internal fields are left out, and `TestListedKeys` pins which keys those are. | `server/internal/assistant/inbox.go`: `InboxItem.listed`; `server/internal/assistant/inbox_listed_keys_test.go`: `TestListedKeys` | `inbox_every_block` |
| An answered record keeps a one-line summary, and `items` stores the full questions and answers in pairs with line breaks; an old record has no `items`. | `server/internal/assistant/answered.go`: `answeredBody`, `answeredItems` | `feed_answered_items` |
| A 401 keeps a string `error`; a 409's `code`, `delivered`, `draft` and `clearable` sit flat beside `error`. | `Hub.auth`, `callErr`; `docs/protocol/protocol.md`, "Sending messages" | `unauthorized`, `conflict_input_draft`, `conflict_dialog_open`, `conflict_prompt_changed` |

## JSON structure

Each file is an array of cases, UTF-8, two-space indent, trailing newline.

- `name`: an English snake_case name, unique across all files of this domain.
- `description`: one sentence.
- `source`: an array of where the rule comes from: a file and function, a type, a unit test or a document section.
- `from_test`: the test name, when the case takes an existing unit test's assertion directly; every other case is marked `derived: true`. The two are mutually exclusive.
- `decoder`: the kind of sample (`event`, `compat_event`, `compat_session`, `sessions`, `machines`, `interactions`, `inbox`, `items`, `feed`, `items_after`, `items_range`, `fleet`, `error`, `health`).
- `http_status`: the HTTP status of an error sample, checked when Go captured the response.
- `input`: the actual JSON message or response body; the whole fixture wrapper need not go to the decoder.
- `expected.accepted`: whether the decoding side accepts the input; false means the whole event is ignored or decoding fails.
- `expected.fields`: partial assertions on the decoded result, keyed by JSON Pointer with array indexes from zero; fields not listed are still golden input but have no further semantic assertion.

The Go test compares the whole golden byte for byte and reads the repository files again, decoding the normal samples with the real Go types. `compat_*` samples are compatibility input built with the Go encoder and are not normal hub output.

## Generating and checking

```sh
cd server
go test -count=1 -p 1 ./internal/hub -run '^TestWireSamples$'
go test -count=1 -p 1 ./internal/hub -run '^TestWireSamples$' -update
```

The first only compares: a missing file, drift or an unregistered extra JSON fails. The second rewrites every JSON of this domain. **When the hub changes the protocol, run `-update`, read the diff, and describe the field and behavior changes in the commit.** Do not edit the generated JSON by hand.

To add a case, build made-up input in the matching topic of `wire_samples_test.go`, calling the real handler when possible; use the same map structure and the production encoder only when no handler can be called on its own. Fill in the source and the expectation, update the rule table here, and run the Go golden check. When a compatibility rule changes, check the real behavior first; never edit only `expected` to make the test pass. A new field that shares a name with a known key of `Envelope` must keep the original type.

## Where the samples differ from the protocol document

- The protocol document's "all JSON fields are snake_case" has existing exceptions: Fleet uses camelCase fields, and Lark parts keep the upstream `refId` and `metadata` key names.

The cases record current behavior.

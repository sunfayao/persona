# Approval policy tables

A resolution center should query these tables. The input is the entry point, the interaction kind and the lock state; the output is the available actions, the plan mode and the unlock requirement. The text of an answer and the form gates are judged separately by the [answer fixtures](../answer/README.md). Both layers must allow an action before it is sent.

## The two profiles

| Entry | `ios_current`: plan approval | `unified`: plan approval | Unlock rule for reject / send back |
| --- | --- | --- | --- |
| Conversation page | `bypassPermissions` or `acceptEdits` | Both kept; approving everything requires having read the whole plan, and both are disabled while the feedback is non-empty | Opening the app needs an unlock; no extra biometrics |
| Notification | `acceptEdits` | Only `acceptEdits`, unlocked before approving | `ios_current`: reject needs no unlock, send back does; `unified`: neither does |
| Lock-screen live activity | `acceptEdits` | Only `acceptEdits`, unlocked before approving | Reject needs no unlock; a plan has no direct send-back button, tapping View opens the app |
| Watch | `acceptEdits` | Only `acceptEdits`, approval requires authentication | The code declares no action-level authentication; the unified table sets reject to need none |
| Siri / shortcut | `acceptEdits` | Only `acceptEdits`, approval requires authentication | The shortcut approves only the oldest pending item and has no reject action |

Sources: `docs/protocol/protocol.md`.

The protocol also allows `default`, but none of these entries has a button that selects it, so the fixtures invent no third current mode. The authentication requirement for sending a plan back from a notification conflicts with the UX target, so both versions are kept.

In the unified profile every external entry approves a plan with `acceptEdits`. How the watch authenticates through the system is not implemented; the unified policy for it is a contract kept for later. A notification handles "one question, single choice" as a complete answer that can be submitted at once; several questions or multiple choice need the app, so that a notification button never submits half an answer.

## JSON structure and lookup

Both files have `schema_version: 1` and a `cases` array: `ios_current.json` pins the existing client's behavior (it is still read by `server/internal/proto/resolution_fixtures_test.go`), `unified.json` pins the target. Each covers 5 entries x 4 kinds x locked/unlocked. A case has a unique `name`, a `description`, a `source` array, `input` and `expected`; the source markers are the same as in the answer domain.

`input` fields:

| Field | Value |
| --- | --- |
| `profile` | `ios_current` or `unified` |
| `entry` | `conversation`, `notification`, `lock_screen`, `watch` or `siri_shortcut` |
| `kind` | `plan`, `permission`, `question` or `screen` of the existing protocol |
| `locked` | Whether the device of that entry is locked; for the watch, the watch itself, never derived from the phone's lock |

`expected.actions` is an array of action descriptions. The order only keeps the fixture stable; a client looks an action up by `id`; an action not listed is unavailable.

| Action field | Meaning |
| --- | --- |
| `id` | `approve_all`, `approve_edits`, `approve_stepwise` (a plan on the conversation page: approve, confirm each step, mode default), `reject`, `allow`, `deny`, `always`, `answer`, `screen_answer`, `choose_option`, `open_app`, `dismiss` |
| `mode` | Set only for plan approval, passed straight into the Resolution; other actions must not carry a mode |
| `authentication` | `required`: authentication, or opening an already unlocked app; `none`: no extra authentication; `platform_default`: not declared in the current source; `unspecified`: the design does not say |
| `requires_unlock` | True when locked and authentication is explicitly required; false when unlocked or explicitly not needed; null when unknown |
| `available_now` | Whether the action can continue without unlocking first; the complement of `requires_unlock`, null likewise when unknown |
| `requirements` | Extra content gates, to be judged together with the answer fixtures; not a request body field |
| `input` | `feedback` or `message`: text input is supported; `none`: no text box; when omitted the matching form decides |
| `max_options` | A notification shows at most 3 option buttons; the other options are handled by opening the app |

`requirements` takes `not_rejecting`, `feedback_empty`, `reason_empty`, `full_plan_read`, `session_present`, `single_question_single_choice`, `question_not_irreversible` (a question with `reversible: false` can only be answered in the app), `plain_dialog_option` and `always_offered` (the permission card is Claude's, or another Agent's card carries an `always_label`: for a Codex, TRAE CLI or Agent runtime card the button exists only when the terminal's "always allow" says what it would permit). `choose_option` is a button template, expanded in the original option order to the first 3 only when the requirements hold; a text-input or turn-back-to-conversation option does not pose as an ordinary button. `open_app` is navigation and produces no Resolution; `answer` and `screen_answer` open or operate the matching form and do not approve a permission.

Null is an explicit unknown and must not be treated as false: the current behavior does not declare the watch's authentication requirement, which is not enough to prove that an action can run while locked. For question and screen option buttons on a notification the original behavior did not say whether authentication is needed; the decision is that **they need an unlock like Allow** (an option lets the Agent proceed, and a screen dialog's option is often a command approval), and only reject and send-back do not. The unified table says `required` accordingly.

On the conversation page `required` means that while locked the unlocked app must be entered first, not that every button inside the app asks again. `available_now` answers only the lock dimension; gates such as the filled-in reason, whether the plan was read and whether the request expired combine with the answer rules. For notifications and the lock screen, `open_app` includes tapping the notification or card itself; other actions are buttons.

## Checking and limits

```sh
cd server
go test -count=1 -p 1 -run 'TestResolutionFixture' -v ./internal/proto
```

The shared test is `server/internal/proto/resolution_fixtures_test.go`. Go checks unique names, sources, no duplicate actions within a matrix cell, the relation between unlocking and immediate availability, and that the unified external entries use only `acceptEdits`.

Authentication ability was not measured apart from the system; the `ios_current` values are the existing client's record. The watch has no authentication assertion, so the table uses null and guesses no system default.

`unified` is a design specification and changes no production implementation. "Notification send-back needs no unlock", reason input and the watch's authentication must be implemented when each entry connects. MCP `elicitation` is not part of the existing Interaction protocol and is not added to this table; the answer fixtures record that it cannot be submitted directly.

## Rules and covering cases

Case names follow `<profile>_<entry>_<kind>_<locked|unlocked>`, for example `ios_current_notification_plan_locked` and `unified_watch_permission_unlocked`; every combination of profile (`ios_current`, `unified`), entry (`conversation`, `notification`, `lock_screen`, `watch`, `siri_shortcut`), kind (`plan`, `permission`, `question`, `screen`) and state exists. The full expectation of actions and modes is in the JSON. For every entry the rules are:

| Rule | Kind |
| --- | --- |
| The entry selects the plan approval mode of its profile and obeys the authentication requirement while locked. | `plan` |
| The entry offers only the permission actions it has; in `unified` an allowed action must satisfy the authentication requirement. | `permission` |
| A question is answered in the form or by opening the app; the notification quick options apply only to a complete single-choice answer. | `question` |
| A screen card is answered in the terminal or by opening the app; authentication that is not specified is never treated as needing no unlock. | `screen` |

## Adding cases

When adding an entry or an interaction kind, first list the evidence for the current behavior, then state the unified target, and fill in both the locked and unlocked rows; never write "authentication not declared" as "needs no unlock". When changing an approval mode, update the Resolution in the answer domain and confirm that external entries never send `bypassPermissions`. When adding a content gate, say how the form supplies the context; never assume the condition holds from the interaction kind alone.

Run the check command and update the rule list. A boundary derived from the implementation is marked `derived`; an assertion covered directly by an existing unit test is marked `from_test`; a rule that the design adds or overrides is marked `design_change`. Use synthetic data only.

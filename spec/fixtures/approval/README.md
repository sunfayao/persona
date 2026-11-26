# Conversation approval policy

`unified.json` captures the proposed web conversation actions for plans, permissions, questions and terminal prompts. [The answer fixtures](../answer/README.md) describe the corresponding requests.

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



## Checking and limits

```sh
cd server
go test -count=1 -p 1 -run 'TestResolutionFixture' -v ./internal/proto
```

The shared test is `server/internal/proto/resolution_fixtures_test.go`. Go checks unique names, sources, no duplicate actions within a matrix cell, the relation between unlocking and immediate availability, and that the unified external entries use only `acceptEdits`.



## Rules and covering cases


| Rule | Kind |
| --- | --- |
| The entry selects the plan approval mode of its profile and obeys the authentication requirement while locked. | `plan` |
| The entry offers only the permission actions it has; in `unified` an allowed action must satisfy the authentication requirement. | `permission` |
| A question is answered in the form or by opening the app; the notification quick options apply only to a complete single-choice answer. | `question` |
| A screen card is answered in the terminal or by opening the app; authentication that is not specified is never treated as needing no unlock. | `screen` |

## Adding cases

When adding an entry or an interaction kind, first list the evidence for the current behavior, then state the unified target, and fill in both the locked and unlocked rows; never write "authentication not declared" as "needs no unlock". When changing an approval mode, update the Resolution in the answer domain and confirm that external entries never send `bypassPermissions`. When adding a content gate, say how the form supplies the context; never assume the condition holds from the interaction kind alone.

Run the check command and update the rule list. A boundary derived from the implementation is marked `derived`; an assertion covered directly by an existing unit test is marked `from_test`; a rule that the design adds or overrides is marked `design_change`. Use synthetic data only.

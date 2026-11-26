# Interaction answer fixtures

These fixtures turn UI operations into the `Resolution` sent to the hub. A Go test reads them and checks them against the server's protocol types; behavior the design adds is kept clearly apart from current behavior.

## Files and scope

| File | Contents |
| --- | --- |
| `questions.json` | Single choice, multiple choice, free text, paging through several questions, and the submit gate |
| `recommended.json` | Showing the recommended mark and sending the original label |
| [`../question-review/review.json`](../question-review/review.json) | The review of a card of several questions: the click that opens it, its Submit and Previous, and the cards that skip it |
| `decisions.json` | Plans, permissions, answering from a notification and handing back to the terminal |
| `screen.json` | Terminal options, free input and the step bar, for a hub with and without `dialog` |
| `transport.json` | Offline, success, 410, a changed prompt and what happens to the card after a failure |
| `unified.json` | Rules the design adds: feedback exclusivity, the read-the-whole-plan gate, state gates, reason length and the MCP protocol gap; "always allow" is offered only for a Claude card or a card with `always_label` |

`ios_current` is the assertion of the existing client's behavior (the cases keep that name); `unified` is the design assertion still to be implemented. When implementing the unified profile, keep the rules of the current cases that it does not explicitly replace, and apply `unified.json` and the override rules of the [approval policy tables](../approval/README.md). Do not require both profiles to hold in a conflicting scenario.

## JSON structure

The top level is `schema_version: 1` and a `cases` array; each case has a unique English snake_case `name`, a `description`, a `source` array, `input` and `expected`.

- `from_test`: takes an existing unit test's assertion directly, naming the test; a synthetic protocol wrapper is not part of the original assertion.
- `derived: true`: a boundary added from the implementation.
- `design_change: true`: a design requirement that no implementation can be replayed against yet; do not treat it as existing behavior.
- `input.profile`: `ios_current` or `unified`.
- `input.interaction`: an Interaction of the existing protocol; identifiers are synthetic placeholders, and the real structure of a terminal `dialog` is the Go type.
- `input.operation`: the UI operation the test drives, not a network field; unless a context is listed, the request is pending, the connection is available and nothing has been sent.
- `expected.allowed`: whether the operation can be triggered now; being allowed to page or update a choice does not mean it has been sent.
- `expected.resolution`: the full JSON body of POST `/api/v1/interactions/{id}/resolve`; `null` means no request is sent, and a JSON null must not be sent instead.
- `expected.next_page`, `picks`, `display`, `dialog`, `card_present`: extra assertions that appear only in the cases that need them.

`operation.picks` is an array of strings indexed by the original question text, and means a set; the answer must not be assembled from the order of the input array. `other` is indexed by the question text as well. `page` and `option_index` start at zero. `pick_option` is a click, `advance` is next question or submit, `submit` is a click on an available submit entry; the state is a snapshot before the click. When the last question is answered and an earlier one is not, the operation is allowed but only jumps back to the unanswered question, and `resolution` stays null.

**The review step.** A card of several questions shows a review before its answers go: with every question answered, Submit (or the button on an earlier question) only opens the review, and the review's own Submit sends the answers. `operation.reviewing` is whether the review shows before the click (default false), and `expected.reviewing` whether it shows after. `confirm_review` is the review's Submit and `review_back` its Previous; both are disabled while the answer is on its way, and the options and the form's footer are not reachable while the review shows. `../question-review/review.json` asserts these transitions, starting with the first click that sends nothing. It lives outside this folder because the answer replay reads every file here and does not model the review. `questions.json` stays a synchronous answer-construction contract: two of its cases (`multiple_questions_send_original_keys`, `all_answered_can_submit_from_earlier_page`) expect what the final confirmation sends. Every other question case must leave the review closed.

The `option` of `screen_option` is a string key given by the terminal, not an array index. With a hub `dialog`, `option` is sent and `rev` is not put into the Resolution; the hub takes the version from the pending Interaction and checks it before the node presses keys. A hub without `dialog` uses `keys`; free text runs as `pre_keys`, then `text`, then `keys`. Raw terminal input is disabled only when it is the empty string, and whitespace is sent as it is; a free-text option is trimmed at both ends.

The `resolve` operation carries `api_available`, `network_up` and `outcome`; `resolution` is the request actually attempted, and an HTTP failure does not turn an attempted request into null. A 500 is not a gap and is not retried. As on the hub (`server/internal/assistant/fleet_ix_screen.go`, `server/internal/assistant/fleet_ix_codex.go`), `prompt_changed` ends the card before its 409 comes back, which is why the old card is not put back.

Optional protocol fields are kept or omitted exactly as encoded: a rejection must be `allow: false` and cannot be omitted; an empty plan feedback on the conversation page is encoded as `feedback: ""`. Go's `omitempty` drops empty strings, and the comparison test tolerates only those known empty-string differences; it must not be used to drop a false.

## Confirmed differences

Rules `unified.json` adds by design:

| Scenario | Unified profile |
| --- | --- |
| Reason for rejecting | Sent as `message`; while non-empty, Allow and Always allow are disabled; at most 4000 characters |
| Plan feedback | While there is feedback, both approve entries are disabled; clearing it restores them |
| Approving everything | On the conversation page only after the whole plan has been read, with `bypassPermissions` |
| State gate | Only `pending` can start a new answer; `unknown` checks the request first and does not resend the answer |
| MCP form | Needs the protocol and capability negotiation; for now only records that submitting is unavailable, and handing back to the terminal uses the existing `dismiss` |

The reason-length cases use synthetic ASCII so that they do not decide whether a "character" counts UTF-16 units, code points or graphemes; Unicode counting and whitespace normalization of reasons and feedback still need a product specification. A non-empty reason in the fixtures is sent as written. Sending a plan back with empty feedback keeps the current behavior. Although the protocol lists `default` as a plan mode, the plan form has no button for it, and a UI entry must not be added from the protocol alone.

`transport.json` keeps the current behavior: with no network the card is not removed; a normal submit removes it first; an ordinary failure restores it; 410 and `prompt_changed` do not restore the old card. The `answering → answered` states and "your choice was sent back" of `docs/protocol/protocol.md` belong to a later state machine, and the current optimistic removal must not be taken for them.

## Checking

```sh
cd server
go test -count=1 -p 1 -run 'TestResolutionFixture' -v ./internal/proto
```

`TestResolutionFixtures` (`server/internal/proto/resolution_fixtures_test.go`) reads all JSON of both domains, checks source markers, unique names and format, decodes with the real `proto.Interaction` and `proto.Resolution` and checks the re-encoded fields; it also forbids unknown Resolution fields and checks the question text keys, explicit plan modes and terminal options. A synthetic screen with a hub dialog must also be parsed by the real `screen.ParseDialog` into the same structure and version.

The check is limited to protocol decoding and format; cases the design adds are checked only for consistency between the protocol and the specification.

## Rules and cases

Each row is a behavior rule with the cases that cover it; function names in the source files are how to find the code, and line numbers are not a stable contract.

### decisions.json

| Rule | Cases |
| --- | --- |
| The conversation page approves everything with `bypassPermissions`. | `plan_approve_all` |
| The conversation page accepts only edits with `acceptEdits`. | `plan_approve_edits` |
| Sending a plan back keeps the leading and trailing whitespace of the feedback. | `plan_reject_preserves_whitespace` |
| Sending a plan back with no feedback is allowed. | `plan_reject_empty` |
| Allow once sends only `allow`. | `permission_allow` |
| Denying in the current permission panel carries no reason. | `permission_deny` |
| Always allow for this session carries `always`. | `permission_always` |
| Without a session identifier Always allow is not offered. | `permission_always_without_session` |
| When the manager says it will not remember (`always_honored: false`), Always allow is not offered. | `permission_always_not_honored` |
| When the manager says it will remember (`always_honored: true`), another Agent's card without `always_label` also offers Always allow. | `permission_always_honored_agent` |
| While sending a plan back is being edited, the approve button is hidden. | `plan_rejecting_hides_approve` |
| After cancelling the send-back the old feedback is kept and the existing approve buttons work. | `plan_cancel_retains_feedback_allows_approve` |
| Notification actions map to the existing Resolution. | `notification_ix_allow`, `notification_ix_deny`, `notification_ix_approve_plan`, `notification_ix_reject_plan` |
| Sending a plan back from a notification without text sends empty feedback. | `notification_reject_without_text` |
| Handing over to the computer sends `dismiss`, which is not a rejection. | `dismiss_question`, `dismiss_plan`, `dismiss_permission`, `dismiss_screen` |

### questions.json

| Rule | Cases |
| --- | --- |
| Tapping a single-choice option of a single question sends the original label with its recommended suffix at once. | `single_tap_sends_original_label` |
| Picking a single-choice option clears the free-text answer. | `single_pick_clears_other` |
| The other answer is sent with leading and trailing whitespace removed. | `other_answer_trimmed` |
| With no pick and no free-text answer, submitting is not possible. | `empty_answer_cannot_submit` |
| A whitespace-only other answer cannot be submitted. | `blank_other_cannot_submit` |
| Multiple choice joins the picks in option order, not click order. | `multiple_picks_follow_option_order` |
| A free-text answer of a multiple-choice question overrides the picks and is not merged with them. | `multiple_other_overrides_picks` |
| Blank free text does not override the picks. | `blank_other_uses_picks` |
| Clicking a multiple-choice option only updates the selection and sends nothing. | `multiple_pick_waits_for_submit` |
| Clicking a multiple-choice option again unselects it. | `multiple_pick_toggles_off` |
| A multi-question submit is keyed by the original question text and holds every answer. | `multiple_questions_send_original_keys` |
| When the last question is answered and an earlier one is not, it jumps back to the gap and sends nothing. | `last_page_missing_answer_returns_to_gap` |
| With several questions, Next on an answered question only pages. | `next_page_does_not_send` |
| Once everything is answered, an earlier question can submit directly. | `all_answered_can_submit_from_earlier_page` |
| While sending, the option buttons cannot be tapped again. | `sending_disables_option` |

### question-review/review.json

| Rule | Case |
| --- | --- |
| On a card of several questions, Submit on the last one opens the review and sends nothing yet. | `stepped_submit_opens_review` |
| With every question answered, the button on an earlier question also opens the review. | `stepped_advance_from_earlier_page_opens_review` |
| A question left unanswered goes back to it; the review does not open. | `stepped_missing_answer_skips_review` |
| The review's Submit sends every answer once, keyed by the original question. | `review_confirm_sends_all_answers` |
| While the answer is on its way the review's buttons are disabled. | `review_confirm_while_sending_sends_nothing` |
| Previous on the review closes it without sending. | `review_back_returns_to_questions` |
| The review replaces the options. | `review_hides_options` |
| A card of one question has no review. | `single_question_sends_without_review` |

### recommended.json

The recommended mark is shown only for the supported endings, and the original label is sent unchanged.

| Cases |
| --- |
| `recommended_spaced`, `recommended_unspaced`, `recommended_chinese_fullwidth`, `recommended_chinese_ascii`, `recommended_trims_prefix_space`, `recommended_case_sensitive`, `recommended_suffix_only`, `recommended_trailing_space` |

### screen.json

| Rule | Cases |
| --- | --- |
| An ordinary, free-text and back-to-conversation terminal option each build their answer by the hub's capability, with `dialog` and without it. | `hub_screen_choice`, `hub_screen_custom`, `hub_screen_chat`, `legacy_screen_choice`, `legacy_screen_custom`, `legacy_screen_chat` |
| A free-text option that is only whitespace cannot be sent. | `screen_custom_blank_blocked` |
| Raw terminal input is sent as written and is disabled only when empty. | `raw_terminal_text`, `raw_terminal_empty`, `raw_terminal_whitespace` |
| Raw terminal keys are sent in the order given. | `raw_terminal_keys` |
| A terminal multi-question tab bar parses into steps and keeps the option text. | `screen_question_steps` |
| Without numbered options it is not parsed into a dialog. | `screen_without_numbered_options` |

### transport.json

Answering keeps or removes the interaction card according to the connection conditions and the hub's result (`docs/protocol/protocol.md`).

| Cases |
| --- |
| `resolve_no_client`, `resolve_network_down`, `resolve_success`, `resolve_gone`, `resolve_prompt_changed`, `resolve_error_restores_card` |

### unified.json

| Rule | Cases |
| --- | --- |
| After a rejection reason is entered, Allow once is not possible. | `reason_disables_allow` |
| After a rejection reason is entered, Always allow for this session is not possible. | `reason_disables_always` |
| The rejection reason is passed to the hub unchanged in `message`. | `deny_with_reason` |
| Clearing the reason restores Allow. | `clear_reason_enables_allow` |
| After plan feedback is entered the approve buttons are unavailable. | `feedback_disables_approve_all`, `feedback_disables_approve_edits` |
| With plan feedback, it is sent back as a rejection. | `feedback_rejects_plan` |
| Without having read the whole plan, approving everything is not possible. | `unread_plan_blocks_bypass` |
| On the conversation page, with the plan read and no feedback, approving everything is possible. | `read_plan_allows_bypass` |
| Without feedback, accepting only edits is possible. | `unified_accept_edits` |
| A state other than pending cannot submit an answer again. | `state_answering_blocks_submit`, `state_answered_blocks_submit`, `state_expired_blocks_submit`, `state_resolvedelsewhere_blocks_submit`, `state_handedtoterminal_blocks_submit`, `state_unknown_blocks_submit` |
| The pending state allows answering. | `state_pending_allows_submit` |
| The current protocol cannot encode an MCP form answer, so it falls back to the terminal. | `mcp_form_requires_protocol_extension` |
| The rejection reason is limited by design to 4000 characters. | `reason_length_at_limit`, `reason_length_above_limit` |

## Adding cases

Locate the protocol type or design section first and decide whether the case is current behavior or something the design adds; add a uniquely named case to the matching topic file and update the rule tables here. Name protocol fields as the Go definitions do, and do not put page state into the Resolution. Use `from_test` for an assertion taken over directly, `derived` for an implementation boundary and `design_change` for new design, naming the part that has no implementation yet.

Run the check command above and then the checks the server requires. Do not rewrite another answer algorithm in a temporary driver as the source of expected values. Run the privacy scan before committing; data is made-up content and placeholder identifiers only.

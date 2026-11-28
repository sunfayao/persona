# Recording a verdict with `task_review`

The tool's description holds what every call needs. This page holds the rest: what each verdict does, its fields and defaults, and what Persona refuses. A refusal names what is missing or what to do instead; nothing is written until a call is accepted.

Judge the task by its scoped brief and its current criteria, not by handovers or the Agent's claims. Within your supervision role, record the outcome; see [Reviewing results](review.md) for how to check the evidence.

## Who may review

The main conversation, the conversation that supervises the task, and a task leader for its own workers record verdicts; anyone else is refused. A supervisor Agent's `accepted` passes the same gates as the main conversation's: the owner's wait, every criterion checked, the delivery gate. A conversation that is not main and reviews a task another conversation supervises only accepts or cancels it, and sends what is missing to the Agent with `agent_send`.

## accepted

- Give `outcome` (`done`, the default, when the work is complete or the answer is yes; `cannot` when it proved impossible), `reason` (one sentence; it defaults to the note's first line), `link` (one evidence link; it defaults to the task's PR, a link in the note or checks, or the Agent's chat) and `note` (the result in a line).
- With `outcome: done`, `checks` lists every current `done_when` criterion exactly once, with `met: true` and non-empty `evidence`. Name a criterion by `number` (its one-based position in the current `done_when`, as digests and `todo_get` print them), by `criterion` text, or both, which must name the same one. A review whose numbers were read from a list that has changed since is refused: read the task and review again. `outcome: cannot`, a cancelled task and a task without criteria need no checks. Other verdicts may leave checks out, or record progress with them: each check updates the stored state, its evidence, its time and its supervisor, and criteria left out keep their progress.
- The delivery gate (`delivery_gate=merged` by default) keeps PR and issue work open until the PR is merged and its linked issues are closed. Accepting an open PR is refused: park it until the merge (`waiting_on` `external_event` with `evidence.kind` `external_event`, its `url` and `pr_event: terminal`, or `merge_review`), send what is missing with rework, or accept it with `outcome: cannot` when nothing could be delivered. A brief that asks only for a PR or a draft, a read-only task without a merge or close criterion, and `outcome: cannot` skip the gate.
- Without `outcome`, a wording guard reads your note, reason, the checks' evidence and the Agent's last replies (never the criteria's own text) and refuses a result that looks blocked on the owner or inconclusive. When only the worker's closeout `Status` is not done while every criterion has a met check, the acceptance goes through if `note` says what the worker left outside the criteria; the receipt says the status disagreed.
- Acceptance closes verified work. Use `ask_user` with options for an interactive choice in the conversation, or record an actionable task decision with `needs_user`.
- `also_done`: up to 6 lines on what was done beyond the brief; the task's page lists them after the criteria.
- When the owner opted in at Start, an accepted `done` task closes its idle Agent's tab. Report whether it was closed, skipped or still pending; never close the tab yourself.
- In your reply give each criterion as met, not met or dropped, then what was also done.

## rework

- `note` tells the Agent concretely what to do next, in the owner's language.
- `rework_kind`: `repeated_error` or `low_roi_edge` (the default) count as stalled rounds, and a third stalled round is refused; `external_input` (external review, a conflict, the owner's input) does not count as stalled; `substantive_new` (the earlier gap is fixed and a new defect was found) starts the stalled count again. Every kind is refused after 10 rounds in all. At a cap, accept the result as it is (a brief note is better than bouncing low-value edge cases or style nitpicks), cancel it, or ask the owner with `needs_user`.
- When every Agent of the task has stopped, the round is recorded but not sent: start the next round with `task_plan` and `continues`, or resume the stopped Agent on purpose with `agent_send` (`resume: true`, `resume_why`).

## parked

A task that waits for something outside: a person's answer, a merge, an event or a date.

- `reason` (what it waits for; include the PR link when it waits on a PR), `waiting_on` (`someone_else`, `external_event` or `date`) and `evidence` are required. A decision only the owner can make is `needs_user`, not a park.
- `until`: `3d`, `36h`, `90m` or an RFC 3339 time, 1 minute to 30 days ahead; 24 hours by default. A `date` wait needs an explicit RFC 3339 `until`.
- `evidence`: inspect the source before you park.
  - `kind`: `person_reply`, `merge_review`, `external_event` or `date`; `target` names the person or object and `wake` the observable change that ends the wait.
  - `next_actor`: `external` (a third party, a system or time, with no owner step first). When the owner must act first, use `needs_user`.
  - `url`: the ready PR or merge request for `merge_review`, the observable object for `external_event`. `ready: true` (merge_review only) says the PR is ready for the reviewer.
  - `contact`: the request already sent, required for `person_reply` and `merge_review`: `channel`, `url` (the sent message or its receipt, not a channel's home page), `sender`, `sent_at` (the actual time, not in the future), `request` and `status: "sent"`.
  - `reply` (`person_reply` through lark-cli only): the exact ids from a configured lark-cli reader, never names; `person_open_id` is the person you expect to answer, not you, `thread_id` the topic id, not the root message's, and `contact.url` the sent request's topic AppLink. A verified reply asks the current supervisor to review; it is not delivered to the Agent.
  - `interaction_id` (`external_event` only): the exact pending owner request of this task's worker, from `agents_list`; the existing request is shown instead of a new one.
  - `pr_event` (GitHub PR waits only): `terminal` wakes on merge or close, `terminal_or_review` also on a newly submitted review.
  - `recorded_by` is output only. An older park without evidence counts as unverified.

## needs_user

Ask the owner only what they alone can decide, and decide anything else yourself.

- Required: `why_owner` (`irreversible`: delete, a production write, spending; `visible_to_others`: an external message, comment or review; `priority`: whether and in which order; `acceptance`: a visual check; `only_owner_knows`: a private fact), `reason` (what is to be decided), `options` (2 to 4, each a button `label` and what it `leads_to`; the recommended one is shown first), `recommend` (the label of one option; the owner's OK sends it to the Agent) and `cost_of_waiting` (what it costs if the owner does not answer soon).
- `question_quote`: the Agent's original question, verbatim, when `reason` paraphrases it. If the owner already answered it in the Agent's conversation, no card is sent and you review their reply instead.
- `deadline`: how long the decision waits (`4h`, `90m` or an RFC 3339 time, 1 minute to 30 days ahead), with `reversible`, which says whether taking `recommend` can be undone. Without a deadline the owner's silence takes the recommendation after 24 hours. Deleting, sending to other people, spending and production changes are never reversible, and `irreversible` or `visible_to_others` decisions are never taken by default: the owner is only reminded. The decision is rechecked at 24 hours.
- `can_wait: true` lists a low-stakes question in the next daily briefing's Needs you instead of a push or card: when waiting costs little, nobody is blocked today and no deadline falls within 12 hours. It is ignored, and the reply says why, for `irreversible` or `visible_to_others`, for a deadline within 12 hours or with `reversible: false`, and when no briefing comes in time.
- The owner's "You decide", "This isn't what I asked for" and "Tell me the background first" come back to you for supervision. Clarify without repeating the question and without acting irreversibly or externally.

## cancelled

Only for duplicate or unwanted tasks. The task closes, its pending decision ends, and its Agents are stopped and archived (`agent_archive` with `restore` brings one back), and their waiting cards end.

## Typed checks the hub runs

`task_plan` and `task_scope` take `checks`: entries `{criterion, kind, arg}` that name a criterion by its number in the list as it is after the call. `pr_merged`, `ci_green` (while the pull request is open) and `deployed` read the pull requests on record for the task; `http` is a GET of an http or https URL (`arg`: the URL, optionally the expected status, 200 by default, and text the body must contain; 10 seconds, 3 redirects, 64 KB, no credentials or headers). The main conversation declares any kind, a supervisor Agent only the first three, a worker none. A check belongs to its criterion's exact text: a dropped or edited criterion loses its check.

The hub runs the checks when the task enters pending review and when you call `task_review` with `verify: true` (one line per criterion; no verdict is written), and records each result on the criterion as its own. A result of `cannot` (no GitHub access, no pull request on record, the computer offline) is never met and never skipped: the criterion stays not met and its evidence says why. When every criterion has a check, all are met and the task needs no confirmation from the owner (high priority), the hub accepts it itself, with no model turn; otherwise the review comes to its supervisor with the hub's evidence on the ledger.

## Changing criteria with `task_scope`

A criterion is one checkable result of at most 200 characters, written as it is to be checked; procedure and context belong in the prompt. A longer one is refused wherever criteria are written (`task_plan`, `task_scope`, `agent_send` with `task`, `agent_supervise`), and nothing is written.

- `add`: new criteria, appended. `drop`: numbers of criteria to remove, recorded as dropped. `edit`: a list of `{number, text}` that rewrites a criterion in place; it keeps its position and what was reported on it, and is recorded as an edit, never as dropped. These cannot be combined with `done_when`, the whole list after the change, which records as dropped every criterion it leaves out. Numbers are the one-based positions in the current list; a task keeps 1 to 6 criteria.
- A call that changes only `expect_minutes` answers in one line. Any other call answers with the original and the effective criteria, numbered, and what was dropped.

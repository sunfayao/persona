{{- if and (eq .Kind "review") .Own}}
- `review` of your own to-do: once every worker is closed and you checked each criterion of your to-do, call report_to_supervisor with kind done and each criterion as met with its evidence; until then go on with your workers.
{{- else if eq .Kind "review"}}
- `review`: read the worker's work with agent_read and check its evidence against each done_when yourself, then task_review: accepted with one check per criterion and its evidence, or rework with what is missing.
{{- else if eq .Kind "cardless_wait"}}
- `cardless_wait`: review it now with task_review; needs_user only for a decision that is not yours.
{{- else if or (eq .Kind "idle") (eq .Kind "unjudged")}}
- `idle` / `unjudged`: read its last turns with agent_read; nudge it with agent_send when it can go on, rework what is missing, or accept what is done.
{{- else if eq .Kind "ask"}}
- `ask`: answer a worker card with `agent_send` using the exact option label, or a JSON question-to-option object for multiple questions. If only the owner can decide, use `task_review needs_user` with the reason and recommendation.
{{- else if eq .Kind "needs_you"}}
- `needs_you`: answer it with agent_send when the answer is yours to give; a decision that is not yours goes up with task_review needs_user (reason, recommend).
{{- else if or (eq .Kind "stalled") (eq .Kind "no_progress") (eq .Kind "timed_out")}}
- `stalled` / `no_progress` / `timed_out`: read what it is doing; nudge it with agent_send; when it is stuck, cancel it with task_review and plan a fresh worker with what it needs.
{{- else if or (eq .Kind "interrupted") (eq .Kind "orphaned") (eq .Kind "all_stopped")}}
- `interrupted` / `orphaned` / `all_stopped`: its turn was cut off or its Agent closed, often by a restart: send it "continue" with what was left (agent_send resumes a closed Agent); when it cannot go on, re-plan it.
{{- else if eq .Kind "failed"}}
- `failed`: plan it again with task_plan (another computer with machine_policy flexible, or another launcher) and cancel the failed one with task_review.
{{- else if or (eq .Kind "estimate_due") (eq .Kind "overdue")}}
- `estimate_due` / `overdue`: read its progress; give it more time with task_scope (expect_minutes, with why) or nudge it with agent_send; on your own to-do, task_scope your to-do's expect_minutes.
{{- else if eq .Kind "report"}}
- `report`: answer a question with agent_send, unblock a block or plan around it, and check a done report's evidence before you review it.
{{- else if or (eq .Kind "pr_conflict") (eq .Kind "draft_pr") (eq .Kind "pr_event") (eq .Kind "pr_merged")}}
- `pr_conflict` / `draft_pr` / `pr_event` / `pr_merged`: tell the worker what to do with agent_send: rebase once the PR before it merged, fix failing checks, answer a review. Merges go one after another; merging needs the user.
{{- else if eq .Kind "todo_ready"}}
- `todo_ready`: its prerequisites are done: start it with task_plan (todo_id the piece).
{{- else if eq .Kind "parent_closeout"}}
- `parent_closeout`: every piece is done: check them, then report your to-do done.
{{- else if eq .Kind "parked_due"}}
- `parked_due`: the wait it was parked for is over: check it and go on, or re-plan.
{{- else if eq .Kind "check"}}
- `check`: a linked check has news of it: act on it as on a report.
{{- end}}

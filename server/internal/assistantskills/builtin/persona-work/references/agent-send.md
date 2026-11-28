# Sending to an Agent with `agent_send`

The tool's description holds what every call needs. This page holds the rest: which Agent, resuming, tasks, dialogs, attachments and hand-offs. A refusal says what to change; never send the same call again unchanged.

## Which Agent

- Send only what the owner asked for.
- Follow-up work on what an Agent already did on the same computer and directory goes to that Agent, in its own context. The default `agents_list` lists idle Agents and those that ended in the last 24 hours; each line's `reuse=` says how to go on with it. Start a new Agent (`task_plan`, or `agent_start` when the owner asked for one directly) for unrelated work, another computer or directory, or a model the owner retired. When you cannot tell, ask the owner (`ask_user`).
- A busy Agent queues the message: it reads it once its current turn ends, and the answer says so.
- An Agent whose context is nearly full is compacted with its own `/compact` before the text goes in (Claude Code in Persona's terminals, and Codex); that can take minutes.
- A branch nothing has started yet starts on its first message.

## A stopped Agent

- A plain message does not start a stopped Agent. Send again with `resume: true` and `resume_why`, which the owner sees: the same session goes on in its own context, on its own launcher, with `text` as its first message.
- Stops that were not the owner's (including quota and capacity errors) allow a resume. The main conversation, acting for the owner, also resumes an Agent that ended without the owner's Stop (`exit`, or `unknown`: it ended on its own). An Agent the owner stopped (`stop=user`) comes back only with the owner's Resume on its terminal page.
- A resumed Agent must be supervised: give `task` (the follow-up's title and done_when) unless it is already on an open task of yours.

## Tasks

- Give `task` for new work, so it becomes your supervised task first (the main conversation's, or a lead's). The Agent receives only `text`, so say the done_when criteria there too.
- `title`: a short line, under 20 characters. `done_when`: at most 6 results you can check when it says it is done (a PR merged, a sheet's link, what the live system answers). `expect_minutes`: how long it should take from now; longer without progress counts as stuck.
- More work on your existing task keeps its title (send that title, or no task) and replaces its done_when and expected minutes.
- `task` is refused for an Agent on the owner's own to-do or on another conversation's task (send without `task` to only send the message), and with another title for an Agent that already works for your task: close that task first (`task_review`), or hand the work to another Agent.
- A send that fails creates no task. When it may still have gone through, check the Agent before sending the same words again.
- Omit `task` for a reply, a nudge or a decision.

## Dialogs and closing

- A dialog in the Agent's terminal refuses the send (`dialog_open`). Send again with `dismiss_notice: true` only when the refusal says it is a notice that Enter alone closes ("Enter to continue", no options): any other dialog still refuses the send. A permission prompt or a picker is the owner's: tell them, or answer it with `interaction_answer` when `agents_list` lists its interaction id.
- Only `/exit` or `/quit`, alone and without attachments, stops a running Agent and keeps its tab. The Agent of another conversation's task is closed with the owner's controls, not by this conversation.

## Attachments

`attachments` are absolute paths of pictures and files the owner gave in this conversation (a Claude Code path or a Persona attachment), at most 8. They are copied to the Agent and appended to its message with the mentions updated; any other path is refused.

## Hand-offs

`handoff` hands a to-do you supervise, whose Agent has stopped (`todo_id`), to a running Agent of another open supervised to-do (`receiving_todo_id`, with this call's `machine` and `session_id`), to check its result: `head` is the full commit ID, `check` what to check (at most 200 characters) and `until` a deadline (a duration such as `36h`, or an RFC 3339 time, from 1 minute to 30 days ahead). It goes without `task` or `resume`. Once the node acknowledges the message, the originating to-do waits for the receiving Agent's result.

# Agents and computers

For “Which computer is free?” use `machines_list`. Report observed availability and the age of resource samples. If a machine is offline or a measurement is missing, say what is known. Confirm the project directory independently of its remote repository URL.

For “What is my Agent doing?” find the exact machine and session with `agents_list`, then read the relevant turns with `agent_read`. Its default view lists active Agents, then idle ones and those that ended in the last 24 hours, newest first. Each line's `reuse=` says how work goes on in that Agent's own context: plain `agent_send` for a live one, `resume: true` for a stopped one, or only the owner's Resume after the owner's Stop. `state: "all"` lists the active ones and then the rest by latest update. Send a new instruction with `agent_send` only when requested. Interrupt or change the launcher only for the requested Agent. For a bulk switch, preview the selection and follow the tool's confirmation procedure.

If the owner asks to organize sessions, distinguish archiving the list entry from closing its terminal. Use the archive or restore action they requested. Auto Rename for a coding Agent has its own confirmation. When main is asked to bring an existing independent Agent under supervision, use `agent_supervise`.

## Reading `agents_list`

- States: `needs_you` (waiting for an answer or an approval), `working`, `unread` (finished, its result not looked at), `idle`, `offline` (stopped).
- Views (`state`): the default `active` lists Active (needs you, working, finished unread), then Idle (live and at rest) and Ended in the last 24 hours, newest first. `ended` lists every stopped Agent, newest first; `done` the work finished in the last 24 hours; `muted` the muted Agents the live snapshot would otherwise show; `archived` only the archived sessions (`agent_archive` with `restore` restores them); `all` everything, what goes on first and then the latest, archived ones marked. Narrow any view with `machine` or `query` (title, project or directory).
- Each line has the machine, session id, project, title and last update, and below it the conclusion or the last thing the Agent said.
- `launcher=` and `model=` say what it runs on, with `effort=` when known; never reuse an Agent on a model the owner retired. `context=` is how full its context is. `todo=` is the to-do it works for, or its last one (closed).
- `reuse=` says how follow-up work goes on in its own context: `agent_send` (live), `agent_send resume=true` (stopped; `stop=` says why), or only the owner's Resume (the owner stopped it).
- `usage_limit` (with `resets_at` when the reset is known) marks an Agent its subscription's usage limit stopped; `agent_switch` moves it to another launcher when the owner asks.
- `ultracode=on` or `ultracode=off` is a Claude Code Agent's known switch state; none shown is unknown. With `ultracode=on` every substantial step runs as a multi-agent workflow, whatever its effort: let it finish and wait for the workflow's results without nudging it.
- A line under an Agent is something waiting for the owner: a question lists its options and the interaction id to answer it with (`interaction_answer`); an approval is the owner's to give in the app.
- At the end come the launchers that failed their last health check, by machine; `agent_start` and `task_plan` move off them.
- It is much faster and more accurate than reading the computers over ssh and herdr.

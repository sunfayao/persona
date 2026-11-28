# Planning tasks with `task_plan`

The tool's description holds what every call needs. This page holds the rest: what each field does, the defaults, and what Persona refuses. A refusal says what to change; never send the same plan again unchanged.

## Before planning

- Check `machines_list` for a computer that can run the work and `agents_list` for an Agent that already has its context. Follow-up on the same computer and directory goes to that Agent with `agent_send` (see [Delegating work](delegate.md)).
- Resolve a repository only when the work is about one. The owner's explicit repository URL or path takes precedence. Otherwise search with `repositories` list, and use `repositories` get only when its details help. A saved binding is a reference, not authorization or an allowlist: never infer a machine or checkout path from it. Non-repository tasks need no lookup, and retrieval is not a mandatory dispatch preflight.
- A reminder or calendar event another tool makes for you is not a task.

## Who starts it and who follows it

- Read-only tasks, tasks the owner asked for in their latest message (`user_requested`) and kinds the owner preapproved start at once. The rest wait as a card for the owner's Start.
- The receipt confirms a queued dispatch, not a started Agent. Check the to-do's outcome before you report, and reach its Agent with `agent_send`.
- The conversation that plans a task owns it: its signals, its cards, the remedies for a start that failed (`task_start_repair` screen, keys and retry), its review and its cancellation. The receipt names who supervises the task and any running work that looks like the same task: check that work before letting both go on.
- Closing the Agent's terminal on acceptance is the owner's choice when they press Start; `task_plan` cannot set it.

## The brief and its fields

A started task keeps a brief that the hub records and never rewrites: the owner's request that led to it, or your `started_why` when you start it on your own (after a supervision message, a notice or a routine). A brief you write yourself is dropped. The card shows the brief, so the owner's Start confirms it.

- `prompt`: the goal, the owner's bounds and the facts the Agent cannot find itself. The manager adds the criteria and the framework rules once. `context` holds facts the manager cannot resolve.
- `title` (under 20 characters), `why`, `goal`, `done_when` and `out_of_bounds` are written in the owner's language.
- `done_when`: up to 6 checkable results, shared with the Agent. A writable task needs at least one. For PR or issue work the delivery gate defaults to merged and closed, unless the brief asks only for a PR or a draft. It is ignored with `continues`.
- `goal`: the owner's outcome in one line. A read-only task needs it; a writable task defaults to its title.
- `out_of_bounds`: up to 6 actions or areas the Agent must not touch.
- `why`: one line on the machine, directory and model you chose. With `launcher` empty the default launcher is used (Claude Code first, then the fallback), and `why` says which.
- `expect_minutes`: omit it for an estimate from comparable history, or the 60-minute system default. A positive value overrides it. The task is due for review at 1×, counts as stuck at 1.5× (at least 20 minutes) and as overdue at 3× (at least 24 hours). An Agent started by hand has no estimate.
- `user_requested`: true only for a task the owner asked for in their latest message; false for inferred work or your own idea, which also gives `started_why`.
- `attachments`: absolute paths of pictures and files the owner gave in this conversation (a Claude Code path or a Persona attachment). They are copied to the Agent and its message's mentions are updated; any other path is refused.
- `project`: a label for a clear long-term area. Reuse an existing one; never make a near-duplicate.
- `part_of`: the request to-do this task is a piece of, and `after`: the pieces that must finish first.
- `todo_id`: an open or failed-start to-do that this conversation opened or supervises and that has no live Agent, such as a Lark topic's to-do, a split piece or a failed start. No new to-do is made, and stopped Agents are released at Start. With `continues` both must be the same.
- `inbox_item`: the Inbox item handed to you, or `none` for unrelated work. Persona checks the conversation and the manager, and links the task to the item without filing or answering it.

## Continuing, retrying and changing scope

- To continue, rework or replace a task, give `continues` with the original to-do id and `continues_why`. The task keeps its to-do, its brief and its criteria, even after a Start that failed; never split it into a new to-do. Start stops and releases the earlier Agents that are still active, and reopens the to-do if it was closed. Persona refuses a takeover you are not authorized for, earlier Agents it cannot reach to stop, and an open legacy successor.
- After an owner-authorized scope change, record it with `task_scope` first and pass its `scope_revision`; a plan with a stale revision is refused.
- A start that did not come up is retried with `task_start_repair` retry, also on another launcher.

## Read-only tasks and permissions

- `readonly: true` is for work that must not change project files, commit or write to external systems. The Agent's first message then says: "This task is read-only: do not change project files, commit, or write to external systems."
- Claude-family launchers run read-only tasks with bypassPermissions. Codex, TRAE and agy (agy through its Antigravity companion) run them in plan mode on current nodes, unless `assistant.readonly_mode` is `prompt`: then read-only is an instruction to the Agent, not a security boundary. pi and cursor refuse read-only tasks.
- Changes need `readonly: false` and the owner's authorization. A read-only Codex sandbox cannot be made writable in place: to fix what it found, plan a writable task, which may continue the same to-do. When the permissions already match, send the follow-up to the live Agent with `agent_send`.
- `permission` sets a writable task's mode; empty means the launcher's default. TRAE and Ferry take `bypassPermissions` and `effort` on current nodes. omp takes neither.

## Machines, launchers and models

- `machine` is a `machines_list` id. With `machine_policy` `fixed` (the default) the task stays there, retries included, and a launcher that is unavailable there is refused. `flexible` lets portable work move to another computer when its launcher fails a health check, and `why` then says so.
- `launcher`: a launcher listed for that machine; an unknown name is refused with the choices. `host`: `agy` for the Antigravity companion, or an enabled engine.
- `tier`: an Agent preset from the snapshot's Model profile line, chosen by purpose and resolved at Start with its profile. An explicit `launcher`, `model`, `effort` or `speed` overrides it. Omit `effort` for the tier's default.
- For `traex-ferry`, name a `model` with `effort`, for example `ferry-gpt-6-2-sol` with `xhigh`. You can also pass `ferry-gpt-6-2-sol@xhigh` as the model ID. `traex` takes native reasoning effort.
- `speed`: `standard` (the launcher's default), `fast` or `ultrafast` for Codex, `fast` for Claude Code.
- `ultracode: true` runs every substantial step of a large task (many files, checks from several angles) as a Claude Code multi-agent workflow: slower and costlier, so leave it off for small work and never write the word into the prompt or the title. Only Claude Code 2.1.284 or later takes it.
- `role`: `worker` for ordinary work; `leader` when the task's Agent will hand work to direct workers through its own restricted `task_plan` and `task_review`. Workers can use `task_spawn` to hand off part of their assignment on the same to-do. They can name `launcher`, `model`, `effort` or `tier`. Persona fixes the child's computer and directory to the parent's and caps its permission.

## Workspaces

`workspace` says where the Agent works:

- Omitted: a read-only task uses `none`. A writable task in a Git checkout follows `assistant.task_workspace_preference` (`prefer-existing` by default, `prefer-new` to isolate), and gets a new workspace when its `cwd` is busy. An explicit `existing` is never switched silently.
- `"new"` isolates a checkout; `{"new": {"branch": "task/my-change"}}` names its branch (default `task/<todo id>`). Reusing an explicitly named branch needs a clean checkout, no live Agent and no open owner; otherwise the plan is refused with the path and the owner.
- `{"existing": "<cwd or workspace_id>", "queue": true}` works in that checkout. With `queue: true`, or with the occupying task in `after`, the task queues first-in first-out and starts by itself when the checkout is free; without either, a busy checkout is refused with its owner.
- `"none"` creates nothing.
- `{"backend": {"operation_id": "<id>"}}` with `todo_id` selects a saved workspace naming plan for an existing to-do. `{"container": {"selected_repo_ids": ["<repo_id>", ...]}}` selects a multi-repository container workspace, or `{"container": {"operation_id": "<id>"}}` with `todo_id` a prepared one. Every selected repository must be ready before Start.
- The machine's engine creates and registers the workspace, and the card shows its editable path before Start. Never run `git worktree add` yourself or ask the Agent to: Persona and the engine would not see that checkout. When creating the workspace fails, no Agent starts. Workspaces stay after acceptance.
- An organization policy may keep some conversations read-only; follow its refusal instead of retrying.

## Examples

A read-only investigation:

```json
{"tasks": [{"title": "Login timeout cause", "prompt": "Find why logins time out after the last deploy. Read the server logs and the recent changes; change nothing.", "goal": "Know why logins time out", "role": "worker", "machine": "<machine id>", "cwd": "~/src/shop", "readonly": true, "user_requested": true, "why": "The shop repository and its logs are on this computer"}]}
```

A fix on a branch of its own, waiting up to 30 seconds for the Agent's first answer:

```json
{"wait_seconds": 30, "tasks": [{"title": "Fix login timeout", "prompt": "Raise the session store timeout to the documented value and add a regression test.", "done_when": ["The regression test passes", "The PR is merged"], "role": "worker", "machine": "<machine id>", "cwd": "~/src/shop", "workspace": {"new": {"branch": "task/login-timeout"}}, "readonly": false, "user_requested": true, "why": "The shop repository is on this computer"}]}
```

A second task for a checkout another task is using, started when that one finishes:

```json
{"tasks": [{"title": "Update the docs", "prompt": "Document the new session timeout in the operations guide.", "done_when": ["The guide names the new timeout"], "role": "worker", "machine": "<machine id>", "cwd": "~/src/shop", "workspace": {"existing": "~/src/shop", "queue": true}, "readonly": false, "user_requested": true, "why": "Same checkout as the fix"}]}
```

Rework of an accepted task after the owner widened its scope with `task_scope`:

```json
{"tasks": [{"title": "Fix login timeout", "prompt": "Also cover the admin login, as the owner asked.", "continues": "<todo id>", "continues_why": "The owner added the admin login", "scope_revision": "<from task_scope>", "role": "worker", "machine": "<machine id>", "cwd": "~/src/shop", "readonly": false, "user_requested": true, "why": "Same checkout and Agent context"}]}
```

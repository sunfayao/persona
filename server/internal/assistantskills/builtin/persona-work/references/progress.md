# Progress and follow-up

Read the latest snapshot, `todo_get` and, if needed, `agent_read` for the linked Agent. Read an Assistant conversation with `agent_read` (`conversation`). Report what is done, what is blocked and any decision needed, using the returned links. A status question can usually be answered without sending a new message.

For a requested change, send the new information to the existing Agent with `agent_send`. Use `task_scope` to record an owner-authorized change to the task's boundaries. A failed start can use `task_start_repair` retry; first check its recorded outcome. Respect the conversation's actual supervision role. When an Agent reports, bring its to-do up to date in the same turn: criteria with `task_scope`, dates and notes with `todo_update`, a verified result with `task_review`.

For a progress report, use the available work-log progress card if one is provided. Otherwise give one line per piece of work with results, current work or blockers, and what the owner needs to do, starting with what needs the owner.

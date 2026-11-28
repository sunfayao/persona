# Antigravity CLI transcript fixtures

| File | Source | Coverage |
| --- | --- | --- |
| `agy-tools.jsonl` | Redacted excerpts from local Antigravity CLI full brain exports | User input and a settings change; planner thinking and text; foreground and background commands, ranged reads, edits and writes, checklist states, task list/status/kill, timer and cron scheduling, web search, question and `A1:` answer, direct MCP and wrapped Persona calls. Includes `DONE`, `RUNNING`, `ERROR` and `INVALID` results. |
| `agy-system.jsonl` | Redacted excerpts from local Antigravity CLI full brain exports | Background launches and their completion, cancellation and still-running notices; timer and cron wakes with their launches and matching senders; server restart, efficiency reminder, near-512k context usage and checkpoint, and two API errors. |

The samples use `transcript_full.jsonl`, not the compact export. Tool arguments retain native JSON types. Planners
and their result slots are kept as groups: a call at zero-based position `i` has its `GENERIC` result at
`planner.step_index + 1 + i`. Indices may be rebased between source excerpts; the task handle's step-index suffix
moves with its result. Conversation IDs, paths and prose are replaced, and times are shifted. The compaction summary
is shortened. Follow the manual privacy review in [the transcript fixture guide](../../../../transcript/testdata/README.md)
before adding lines.

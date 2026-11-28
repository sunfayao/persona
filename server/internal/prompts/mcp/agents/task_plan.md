Plan code, repository research and other multi-step work for coding Agents, one supervised task per independent piece. Check machines_list and agents_list first; follow-up for an Agent with the context goes to agent_send. Read-only, user_requested and preapproved tasks start at once, others on the owner's Start; a queued receipt is not a started Agent.

Write title (under 20 characters), why, goal, done_when and out_of_bounds in {{lang .Lang}}. A writable task needs checkable done_when, a read-only one goal. Continue a task with continues (scope_revision after task_scope); retry a failed start with task_start_repair retry. readonly=true: Claude-family launchers run with bypassPermissions; Codex and agy (Antigravity companion) in plan mode, or with assistant.readonly_mode=prompt by instruction only, not a security boundary; pi and cursor refuse it.

`wait_seconds` observes up to 50 seconds after dispatch, appending `Inline observation` JSON: `completed`(this input's `reply`), `question`(pending `interaction_id`), `timeout`/`cancelled`/`unavailable`/`interrupted`/`failed`, with known input/session ids. Observation never answers questions, accepts tasks, resends or cancels work; normal supervision/`task_review` applies. Older nodes may time out rather than return an unrelated reply. Only an immediately dispatchable single-task plan waits.

Details: read .claude/skills/persona-work/references/task-plan.md in your working directory.

```json
{
  "type": "object",
  "required": [
    "tasks"
  ],
  "properties": {
    "wait_seconds": {
      "type": "integer",
      "minimum": 0,
      "description": "0/omitted: normal receipt"
    },
    "tasks": {
      "type": "array",
      "minItems": 1,
      "maxItems": 8,
      "items": {
        "type": "object",
        "required": [
          "title",
          "prompt",
          "role",
          "machine",
          "readonly",
          "user_requested",
          "why"
        ],
        "anyOf": [
          {"required": ["cwd"]},
          {"type": "object", "required": ["workspace", "todo_id"], "properties": {"workspace": {"type": "object", "required": ["backend"]}}},
          {"type": "object", "required": ["workspace"], "properties": {"workspace": {"type": "object", "required": ["container"]}}}
        ],
        "properties": {
          "title": {
            "type": "string"
          },
          "context": {"type": "string"},
          "prompt": {
            "type": "string"
          },
          "machine": {
            "type": "string"
          },
          "machine_policy": {
            "type": "string",
            "enum": ["fixed", "flexible"]
          },
          "cwd": {
            "type": "string"
          },
          "workspace": {
            "type": ["string", "object"],
            "description": "omit for the default; new: an engine-made checkout (never have the Agent run git worktree add); existing + queue: true waits until it is free",
            "oneOf": [
              { "type": "string", "enum": ["new", "none"] },
              { "type": "object", "required": ["backend"], "properties": { "backend": { "type": "object", "required": ["operation_id"], "properties": { "operation_id": { "type": "string" }, "host": { "type": "string" } } } } },
              { "type": "object", "required": ["container"], "properties": { "container": { "type": "object", "properties": { "selected_repo_ids": { "type": "array", "minItems": 1, "items": { "type": "string" } }, "operation_id": { "type": "string" }, "host": { "type": "string" } } } } },
              { "type": "object", "required": ["existing"], "properties": { "existing": { "type": "string" }, "queue": { "type": "boolean" } } },
              { "type": "object", "required": ["new"], "properties": { "new": { "type": "object", "properties": { "branch": { "type": "string" } } } } }
            ]
          },
          "launcher": {
            "type": "string"
          },
          "host": {
            "type": "string",
            "description": "agy (Antigravity companion) or an enabled engine"
          },
          "model": {
            "type": "string"
          },
          "permission": {
            "type": "string",
            "enum": [
              "default",
              "acceptEdits",
              "plan",
              "auto",
              "bypassPermissions"
            ]
          },
          "role": {
            "type": "string",
            "enum": ["worker", "leader"],
            "description": "leader: its Agent plans and reviews workers"
          },
          "ultracode": {
            "type": "boolean",
            "description": "Claude Code multi-agent workflow for large tasks; slower, costlier"
          },
          "readonly": {
            "type": "boolean",
            "description": "true: no file changes, commits or external writes"
          },
          "user_requested": {
            "type": "boolean",
            "description": "true only if the owner's latest message asked for it"
          },
          "why": {
            "type": "string",
            "description": "why this machine, directory and model"
          },
          "done_when": {
            "type": "array",
            "maxItems": 6,
            "items": {
              "type": "string"
            }
          },
          "goal": {
            "type": "string"
          },
          "out_of_bounds": {
            "type": "array",
            "maxItems": 6,
            "items": {
              "type": "string"
            }
          },
          "continues": {
            "type": "string"
          },
          "scope_revision": {
            "type": "string"
          },
          "continues_why": {
            "type": "string"
          },
          "started_why": {
            "type": "string",
            "description": "why you start it unasked"
          },
          "attachments": {
            "type": "array",
            "maxItems": 8,
            "items": {
              "type": "string"
            },
            "description": "absolute paths of files the owner gave here"
          },
          "expect_minutes": {
            "type": "integer",
            "minimum": 0
          },
          "project": {
            "type": "string"
          },
          "part_of": {
            "type": "string"
          },
          "after": {
            "type": "array",
            "items": {
              "type": "string"
            }
          },
          "todo_id": {
            "type": "string",
            "description": "this chat's open or failed-start to-do with no live Agent"
          },
          "inbox_item": {
            "type": "string"
          },
          "tier": {
            "type": "string",
            "description": "preset from the snapshot's Model profile line; explicit launcher, model, effort or speed override it"
          },
          "speed": {
            "type": "string",
            "enum": ["standard", "fast", "ultrafast"],
            "description": "standard (default), fast or ultrafast speed; Claude Code: fast only"
          },
          "effort": {
            "type": "string"
          }
        }
      }
    }
  }
}
```

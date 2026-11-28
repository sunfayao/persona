Start a coding Agent directly on one of the owner's computers, in a project directory, with a first prompt; returns its session id. Use it for development work the owner asked to start directly (planned, supervised work goes through task_plan), and check machines_list first. Claude-family launchers start in bypassPermissions. ultracode is for Claude Code only; pi and cursor ignore effort, and TRAE CLI and omp refuse it. secret_env maps environment variable names to secret-provider references such as op://vault/item/field, never to values: the owner approves the hand-off on a card first; tell the Agent the variables exist, and never ask it to print them.

`wait_seconds` observes up to 50 seconds after dispatch, appending `Inline observation` JSON: `completed`(this input's `reply`), `question`(pending `interaction_id`), `timeout`/`cancelled`/`unavailable`/`interrupted`/`failed`, with known input/session ids. Observation never answers questions, accepts tasks, resends or cancels work; normal supervision/`task_review` applies. Older nodes may time out rather than return an unrelated reply.

Details: read .claude/skills/persona-work/references/agent-start.md in your working directory.

```json
{
  "type": "object",
  "required": [
    "machine",
    "cwd",
    "prompt"
  ],
  "properties": {
    "wait_seconds": {
      "type": "integer",
      "minimum": 0,
      "description": "0/omitted: normal receipt"
    },
    "machine": {
      "type": "string"
    },
    "cwd": {
      "type": "string",
      "description": "absolute or ~/ path; with worktree, a directory of the repository to branch from"
    },
    "prompt": {
      "type": "string"
    },
    "attachments": {
      "type": "array",
      "maxItems": 8,
      "items": { "type": "string" },
      "description": "files the owner gave in this chat, by absolute path"
    },
    "launcher": {
      "type": "string"
    },
    "secret_env": {
      "type": "object",
      "maxProperties": 8,
      "additionalProperties": { "type": "string" }
    },
    "ultracode": {
      "type": "boolean",
      "description": "Claude Code multi-agent workflow for a large task; slower and costlier"
    },
    "worktree": {
      "type": "object",
      "description": "a checkout of its own that the machine's engine makes (never have the Agent run git worktree add)",
      "required": [
        "branch"
      ],
      "properties": {
        "branch": {
          "type": "string",
          "description": "made from the workspace's HEAD when new; one checked out elsewhere is refused"
        }
      }
    },
    "tier": {
      "type": "string",
      "description": "preset from the snapshot's Model profile line; explicit launcher, model, effort or speed override it"
    },
    "host": {
      "type": "string",
      "description": "agy (Antigravity companion) or herdr (the computer's terminal); leave out to let Persona choose"
    },
    "speed": {
      "type": "string",
      "enum": ["standard", "fast", "ultrafast"],
      "description": "standard (default), fast or ultrafast speed; Claude Code: fast only"
    },
    "effort": {
      "type": "string"
    },
    "model": {
      "type": "string"
    }
  }
}
```

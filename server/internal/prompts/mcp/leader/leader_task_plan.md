Hand a part of your to-do to a worker: each task starts a new coding Agent at once, beneath your to-do, with no card for the owner. Workers belong to your to-do only: role=worker on every task, todo_id only for a piece of your to-do (part_of it) with no Agent yet, no handing work on, at most 8 of your workers open at a time. Give each worker machine and cwd (computer and project directory): you run in a home directory, and a worker without them starts there. Give it done_when (results you can check); a task that only reads sets readonly true, gets the read-only constraint in its first message and is an independent check of another worker's result. Claude-family launchers always start in bypassPermissions; Codex read-only tasks keep the plan-mode sandbox. What a worker reports, and when it stops, stalls, runs past its estimate or waits for review, reaches you in a message starting "Persona · supervision". Returns at once with each worker's to-do; it does not confirm the Agent started.

```json
{
  "type": "object",
  "required": [
    "tasks"
  ],
  "additionalProperties": false,
  "properties": {
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
          "readonly"
        ],
        "additionalProperties": false,
        "properties": {
          "todo_id": {
            "type": "string",
            "description": "a piece of your to-do with no Agent yet; omitted: a new worker to-do"
          },
          "title": {
            "type": "string",
            "description": "the worker's task in a short line"
          },
          "prompt": {
            "type": "string",
            "description": "the worker's first message: all it needs, without asking you"
          },
          "role": {
            "type": "string",
            "enum": ["worker"],
            "description": "required: worker; a worker cannot hand work on"
          },
          "machine": {
            "type": "string",
            "description": "machine id: where the worker works; empty: the computer you run on"
          },
          "machine_policy": {
            "type": "string",
            "enum": ["fixed", "flexible"],
            "description": "fixed (default) keeps the machine; flexible: a failed launcher health check may move it"
          },
          "cwd": {
            "type": "string",
            "description": "project directory, absolute or ~/…; empty: your own directory (no project)"
          },
          "launcher": {
            "type": "string",
            "description": "launcher name, one that machine has; empty: the default"
          },
          "model": {
            "type": "string",
            "description": "model id; empty: the launcher's default"
          },
          "tier": {
            "type": "string",
            "description": "an Agent preset (a tier of the model profile); explicit launcher, model, effort and speed override it"
          },
          "speed": {
            "type": "string",
            "enum": ["standard", "fast", "ultrafast"],
            "description": "speed; overrides the tier; standard/omitted: launcher default. Codex: fast, ultrafast; Claude Code: fast only"
          },
          "effort": {
            "type": "string",
            "description": "explicit effort override; omit to use the tier's"
          },
          "permission": {
            "type": "string",
            "enum": [
              "bypassPermissions",
              "plan"
            ],
            "description": "Writable mode. TRAE and Ferry take bypassPermissions and effort; omp takes neither."
          },
          "readonly": {
            "type": "boolean",
            "description": "true when the worker only reads and changes no project files; false when it changes files"
          },
          "done_when": {
            "type": "array",
            "maxItems": 6,
            "items": {
              "type": "string"
            },
            "description": "what done means: results the worker must show, one each; the worker is told them"
          },
          "checks": {
            "type": "array",
            "items": {"type": "object", "properties": {"criterion": {"type": "integer"}, "kind": {"type": "string"}, "arg": {"type": "string"}}},
            "description": "hub-run checks on done_when, by number: kind pr_merged, ci_green or deployed"
          },
          "goal": {
            "type": "string",
            "description": "a read-only worker's goal in one sentence (a writable one needs done_when); estimates are automatic"
          },
          "expect_minutes": {
            "type": "integer",
            "minimum": 0,
            "description": "optional minutes override; omit/0 for comparable history or the 60-minute system_default. Review at 1×; stuck at 1.5× (min 20m); overdue at 3× (min 24h)"
          },
          "why": {
            "type": "string",
            "description": "optional: one line on why this machine and directory"
          }
        }
      }
    }
  }
}
```

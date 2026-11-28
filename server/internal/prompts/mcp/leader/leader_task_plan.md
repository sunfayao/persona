Hand a part of your to-do to a worker: each task starts a new coding Agent at once, beneath your to-do, with no card for the owner. Workers belong to your to-do only: every task sets role=worker, todo_id names only a piece of your to-do (part_of it) that has no Agent yet, a worker cannot hand work on, and at most 8 of your workers are open at a time. Give each worker machine and cwd, the computer and the project directory it works in: you run in a home directory, and a worker left without them starts there. Give it done_when (results you can check); a task that only reads sets readonly true and gets the read-only constraint in its first message, which makes it an independent check of another worker's result. Claude-family launchers always start in bypassPermissions mode; Codex read-only tasks keep the plan-mode sandbox. What a worker reports, and when it stops, stalls, runs past its estimate or waits for review, reaches you in a message that starts with "Persona · supervision". With join true, todo_id is a to-do you supervise and the task starts one more member beside those working, whose part is its title, done_when (criteria, by number or text) and expect_minutes. Returns at once, with each worker's to-do; it does not confirm the Agent started.

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
            "description": "a piece of your to-do with no Agent yet, to run this worker on; omitted: a new worker to-do"
          },
          "join": {
            "type": "boolean",
            "description": "another Agent on your to-do (todo_id); done_when: its criteria"
          },
          "title": {
            "type": "string",
            "description": "the worker's task in a short line"
          },
          "prompt": {
            "type": "string",
            "description": "the worker's first message: all it needs to work without asking you"
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
            "description": "fixed (default): keep this machine; flexible: a failed launcher health check may move it"
          },
          "cwd": {
            "type": "string",
            "description": "the project directory, absolute or ~/…; empty: your own, which is no project"
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
            "description": "a tier of the model profile in use; explicit launcher, model, effort and speed override it"
          },
          "speed": {
            "type": "string",
            "enum": ["standard", "fast", "ultrafast"],
            "description": "standard, fast or ultrafast speed; Claude Code: fast only"
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
            "description": "true when the worker only reads (changes no project files); its first message then carries the read-only constraint"
          },
          "done_when": {
            "type": "array",
            "maxItems": 6,
            "items": {
              "type": "string"
            },
            "description": "what done means: results the worker must show, one each; the worker is told them"
          },
          "goal": {
            "type": "string",
            "description": "a read-only worker's goal, one sentence (a writable one needs done_when)"
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

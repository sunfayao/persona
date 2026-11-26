# Plan: split the server's giant packages

Status: in progress. This is the target structure of `server/` and the order of the moves. Each stage is a branch of
its own that keeps behaviour unchanged.

## Why

Measured on 2026-11-26:

| Package | Product lines | Test lines | Tests | Shape |
| --- | --- | --- | --- | --- |
| `internal/assistant` | 206,000 in 619 files | 295,000 in 1,067 files | 6,476 | 3,897 methods on one `Service` struct of about 173 fields; 462 package variables |
| `internal/node` | 168,000 in 640 files | 209,000 in 840 files | 4,534 | 1,802 methods on `Node`; 633 package variables |
| `internal/hub` | 63,000 in 248 files | 97,000 in 434 files | 1,677 | 1,064 methods on `Hub` |

A Go package is one compile unit and one test binary. A one-line change in `assistant` recompiles and relinks 200,000
lines, and its 6,476 tests share package variables and the process environment, so none of them can run in parallel:
a serial run takes about 12 minutes of mostly waiting (the test process uses a quarter of one core). The work-around
today is to run eight processes of the same binary side by side (`scripts/test/go_partitions.py`).

The second cost is reading. With every method on one receiver, nothing says which of the 173 fields a feature uses,
so every change has to assume it may touch anything.

## Target structure

```
server/
  cmd/<program>/                 main packages only: flags, wiring, exit codes
  internal/
    <platform packages>          wire types, storage, text: proto, rpc, db, i18n, apierr, prompts, transcript, ...
                                 (flat, as today; they import no manager, hub or node package)
    hub/                         the communication center (routes, auth, relay); unchanged, already guarded
    assistant/                   the assistant manager's composition root: Service wiring, RPC and tool registry
      conversation/              main and other conversations, the live snapshot, relays, compaction
      agents/                    the Agents tools (list, read, send, start), A2A, dispatch between computers
      tasks/                     task plans, placement, start, workspaces, delivery
      supervision/               signals, digests, review, decisions, questions, parking
      todo/                      to-dos, repository and issue/PR links (projection exists already)
      inbox/                     triage, grouping, handling, drafts
        feishu/, channels/       one package per source that needs more than a connector
      schedule/                  follow-ups, routines, briefings
      models/                    profiles, presets, launchers, launcher health
      settings/                  the settings registry and the settings tool
    node/                        the node's composition root: Node wiring and RPC dispatch
      sessions/                  the session list, session state, history
      harness/claude/            one package per coding CLI: start, send, read, questions, modes
      harness/codex/
      harness/agy/, harness/trae/, harness/cursor/, ...
      engine/herdr/, engine/orca/, engine/paseo/, engine/tmux/   one per terminal engine
      assistantrt/               running the assistant's own conversations (Claude Code headless, Codex, runtimes)
      gates/                     tool hooks, the Lark gates, the gate proxy and relay
      machine/                   machine facts, files, ports, previews, worktrees, updates
    devices/, extmgr/            the other managers; split the same way once they pass the size limit
```

## Rules

1. **Dependencies point one way**: `cmd` → a root (`assistant`, `node`, `hub`, `devices`, `extmgr`) → its domain
   packages → platform packages. A domain package never imports its root or a sibling domain.
2. **A domain states what it needs.** Each domain package has one constructor that takes its dependencies as small
   interfaces it declares itself (the store methods it calls, the node calls it makes, a clock, a logger). The root
   implements them and wires the domains together; a domain reaches another domain only through such an interface.
3. **No package-level state in a domain package**: no mutable package variables, no reads of the process environment
   outside the constructor. State lives on the domain's struct, so two tests can run two instances side by side.
4. **Time is injected.** A domain takes a clock; its tests use a fake one and never `time.Sleep`.
5. **Size limits** for every package under `internal/`: at most 15,000 non-test lines and 80 non-test files. The two
   roots are exempt until their last stage, and their numbers may only go down.
6. **The root keeps the names the outside sees.** RPC method names, tool names, HTTP routes, table names and wire types
   do not change in a move. A move is a move: no behaviour change, no prompt change, goldens stay byte-identical.

`server/internal/archguard` enforces 1 and 5: an import of a root or a sibling domain from a domain package fails,
and so does a package over the limits. The limits of the packages that are still too big are recorded in a baseline
that may only shrink.

## How a domain moves

1. List the domain's files (they share a filename prefix today, for example `a2a_*.go`) and what they use from the
   root: fields of `Service`, other methods, package variables.
2. Declare that as the domain's dependency interfaces and its own struct; move the files into the package, turning
   `func (s *Service) x(...)` into `func (d *Domain) x(...)`.
3. The root holds the domain (`s.a2a`) and keeps only thin forwards for the entry points other code calls; callers
   inside the root are changed to call the domain.
4. Tests move with their files and use the domain's constructor with fakes, a fake clock and `t.Parallel()`.
5. Build, vet, the whole server test run and the goldens show no change in behaviour.

Extract leaves before trunks: a domain that nothing else in the root calls into moves first.

## Stages

| Stage | Moves | Notes |
| --- | --- | --- |
| 0 | The guard: the import and size rules in `archguard`, with the baseline of today's sizes | No code moves; makes the rules checkable |
| A1 | `assistant/agents`: A2A (`a2a_*`), dispatch between computers (`dispatch*`) | About 3,000 lines; proves the pattern on a domain with a store, node calls and a delivery loop |
| A2 | `assistant/schedule`: follow-ups, routines, briefing | About 9,000 lines; timers, so the fake clock pays most here |
| A3 | `assistant/models` and `assistant/settings` | About 20,000 lines; few callers into the rest |
| A4 | `assistant/todo`, then `assistant/inbox` with `feishu` and `channels` | About 45,000 lines |
| A5 | `assistant/tasks`, `assistant/supervision`, `assistant/conversation` | The core; last, when the smaller domains have left |
| N1 | `node/gates`, `node/machine` | Leaves of the node: about 15,000 lines |
| N2 | `node/engine/*`, then `node/harness/*` | One package per engine and CLI |
| N3 | `node/assistantrt`, `node/sessions` | The node's core; last |

Stages of the two roots are independent and can run side by side. After each stage the baseline in `archguard` is
lowered and the partition counts in `scripts/test/go_partitions.py` are revisited: a root whose serial run fits in a
minute no longer needs partitions.

## Acceptance of a stage

- `go build ./...`, `go vet ./...` and `gofmt` are clean.
- The whole server test run passes, through `scripts/test/go_test_partitioned.py`.
- Golden files are unchanged (`git diff --stat -- '*/testdata/*'` is empty), and so are the prompt templates.
- No RPC, tool, route, table or wire name changed; `git diff` of the moved code shows moves and receiver changes only.
- The new package passes the rules above, and its tests run with `-count=3 -shuffle=on` and in parallel.
- The stage's own report gives the root's line and file counts before and after, and the package's test time.

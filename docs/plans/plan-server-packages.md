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
      assistantrt/               running the assistant's own conversations (Claude Code headless, Codex, runtimes)
      gates/                     tool hooks, the Lark gates, the gate proxy and relay
      doctor/                    node diagnostics and LaunchAgent repair
      preview/                   local browser and preview streams
      ports/                     discovery and probing for live agent panes
      files/                     file reads, metadata, listings, search and downloads
      computeruse/               computer and browser tools after separating their shared lifecycle from the root
      machine/                   machine facts, the remaining file operations, worktrees and updates
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
5. **Size limits** for every package under `internal/`: at most 15,000 non-test lines and 80 non-test files.
   Packages already above either limit have a baseline. A recorded number may grow by at most one percent, so a
   fix in place passes while a feature goes to a package of its own; an update of the baseline only lowers it.
6. **The root keeps the names the outside sees.** RPC method names, tool names, HTTP routes, table names and wire types
   do not change in a move. A move is a move: no behaviour change, no prompt change, goldens stay byte-identical.

`server/internal/archguard` enforces 1 and 5: an import of a root or a sibling domain from a domain package fails,
and so does a package over the limits. The limits of the packages that are still too big are recorded in a baseline
that may only shrink.
From `server/`, run `UPDATE_GOLDEN=1 go test ./internal/archguard -run '^TestPackageRules$'` after a reduction.

## How a domain moves

1. List the domain's files (they share a filename prefix today, for example `a2a_*.go`) and what they use from the
   root: fields of `Service`, other methods, package variables. Take the list from the compiler (move the files and
   read what no longer resolves), not from filenames: a helper without the prefix can belong to the domain, and one
   that several domains use and that holds no state of the root becomes a platform package.
2. Declare that as the domain's dependency interfaces and its own struct; move the files into the package, turning
   `func (s *Service) x(...)` into `func (d *Domain) x(...)`.
3. The root holds the domain (`s.a2a`) and keeps only thin forwards for the entry points other code calls; callers
   inside the root are changed to call the domain.
4. Tests of the domain's behaviour move with their files and use the domain's constructor with fakes, a fake clock
   and `t.Parallel()`. Tests of registration, authorization and the path through the whole root stay in the root.
5. Build, vet, the whole server test run and the goldens show no change in behaviour.

Extract leaves before trunks: a domain that nothing else in the root calls into moves first.

## Stages

| Stage | Moves | Notes |
| --- | --- | --- |
| 0 | Import and size rules in `archguard` | Done. The baseline records existing import exceptions with reasons and oversized packages with both counts. |
| A1 | `assistant/agents`: A2A (`a2a_*`) and dispatch's A2A-backed tracking | Done. Dispatch's orchestration stays in the root until A3 and A5: it calls model and launcher resolution, settings, task placement and supervision. |
| A2 | `assistant/schedule`: follow-ups, routines, briefing | About 9,000 lines; timers, so the fake clock pays most here |
| A3 | `assistant/models` and `assistant/settings` | About 20,000 lines; few callers into the rest |
| A4 | `assistant/todo`, then `assistant/inbox` with `feishu` and `channels` | About 45,000 lines |
| A5 | `assistant/tasks`, `assistant/supervision`, `assistant/conversation` | The core; last, when the smaller domains have left |
| N1 | `node/doctor`, `node/preview`, `node/ports` and `node/files` | Done for these four domains. The node wires RPC, workspace policy, and stream transport at the existing entry points. |
| N2 | Remaining `node/harness/*`, then `node/gates` and the rest of `node/machine` | Codex parsing (`node/harness/codex`) and its app-server transport (`node/harness/codex/appserver`, with `tomltext` and `gitworktree`) are done. Codex session start, send, inventory, approvals, modes and stop stay in the root: split the pane, session, writer and notification seams (`newTab`, `bindPane`, `runAgentCommand`, `putAgentState`, `notifyCodex`) before moving them or starting Antigravity. Extract the shared Screen Sharing and approval or installer seams before moving `node/computeruse`. Keep file writes and watches with the node path policy until their shared mutation workflow has a boundary. |
| N3 | `node/assistantrt`, `node/sessions` | The node's core; last |

Stages of the two roots are independent and can run side by side. After each stage the baseline in `archguard` is
lowered and the partition counts in `scripts/test/go_partitions.py` are revisited: a root whose serial run fits in a
minute no longer needs partitions.

### Stage A1 measurements

| Package | Product files | Product lines | Methods on `Service` | Test time |
| --- | ---: | ---: | ---: | ---: |
| `internal/assistant` before | 619 | 206,187 | 3,897 | 8 partitions, about 90 s |
| `internal/assistant` after | 613 | 203,894 | 3,847 | 8 partitions, about 90 s |
| `internal/assistant/agents` | 10 | 2,574 | | 1 s, in parallel |

`agents.New` takes eight dependencies, each an interface the package declares and the root implements in
`assistant/agents_domain.go`: `Store` (task rows, claims, revisions), `Nodes` (session lookup and node calls),
`Machines` (computers, trust, connectivity), `Visibility` (which conversations and directories A2A may see),
`Outbound` (send and launch guards, confirmation), `Launches` (presets, model and host choice), `Admission` (the
launch lock and capacity shared with dispatch) and `Clock`. `Launches` and `Admission` shrink when A3 and A5 move
what they forward to.

### Stage N1 measurements

Product counts include build-tagged Go files and exclude Go tests. Times are for one uncached test run with the
disposable database wrapper on Linux.

| Package | Product files | Product lines | Test time |
| --- | ---: | ---: | ---: |
| `internal/node` before | 640 | 167,841 | Partitioned |
| `internal/node` after | 611 | 160,494 | Partitioned |
| `internal/node/doctor` | 3 | 1,357 | 2.781 s |
| `internal/node/preview` | 9 | 1,853 | 0.036 s |
| `internal/node/ports` | 5 | 669 | 0.192 s |
| `internal/node/files` | 12 | 1,206 | 0.175 s |

- Doctor needs generated configuration, installed extension checks, sandbox status and the startup preflight.
- Preview owns the browser and tabs. The node starts the project's dev server in a terminal and supplies streams.
- Ports needs live pane process IDs, pane ownership and a change callback. The node also owns Screen Sharing streams.
- Files needs the node's path policy, permissions explanations and Git commands. File writes and watches still share
  the node's path policy and change events.
- Computer and browser tools remain in the root. The computer, plugin and gate entry points share installer and
  cancellation state. Computer calls also use node-owned approval sessions and screen, cursor and idle state. Define
  these lifecycle boundaries before moving the tool handlers.

## Acceptance of a stage

- `go build ./...`, `go vet ./...` and `gofmt` are clean.
- The whole server test run passes, through `scripts/test/go_test_partitioned.py`.
- Golden files are unchanged (`git diff --stat -- '*/testdata/*'` is empty), and so are the prompt templates.
- No RPC, tool, route, table or wire name changed; `git diff` of the moved code shows moves and receiver changes only.
- The new package passes the rules above, and its tests run with `-count=3 -shuffle=on` and in parallel.
- The stage's own report gives the root's line and file counts before and after, and the package's test time.

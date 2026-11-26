# Naming and modules

> Names of files, packages, identifiers and stored things, and how modules are divided. Read this when you create a
> file, a package, a type that crosses a boundary, a key or a test. You usually arrive from the naming table of
> `AGENTS.md`.

---

## Go

| Thing | Convention | Example |
| --- | --- | --- |
| Package | One lower-case word; a domain under its root; a contract package ends in `wire` | `assistant/agents`, `node/ports`, `assistantwire` |
| File | `snake_case.go`, named for its subject; a role suffix when it helps | `captures.go`, `inbox_api.go`, `fleet_routes.go`, `fleet_tools.go`, `acceptance_loop.go` |
| Platform file | `x_unix.go`, `x_windows.go`, `x_darwin.go`, `x_linux.go`, `x_other.go`, with the `//go:build` line first | `ports_windows.go` |
| Test file | `x_test.go`; scenarios `x_<scenario>_test.go`; a source guard `x_guard_test.go` | `ownership_guard_test.go` |
| Test name | A sentence about behaviour | `TestOnlyUserActionsCloseAgentTerminals` |
| Receiver | The type's initial, a pointer | `s *Service`, `n *Node`, `h *Hub`, `d *DB`, `d *Domain` |
| Constructor | `New(Options)` or `New(Dependencies)` | `agents.New(agents.Dependencies{...})` |
| Interface a package needs | Declared in that package, named for the role | `Store`, `Nodes`, `Machines`, `Clock` |
| Error sentinel | `ErrX` | `ErrStaleSnapshot` |
| Generated file | `*_gen.go` | `todo_view_gen.go` |

Inside the old roots (`assistant`, `node`, `hub`) a domain is still a filename prefix (`inbox_*`, `todo_*`,
`models_*`). That is the state being left, not a pattern to extend: a new domain is a package (`02`).

A root exports almost nothing: its constructor, its interface methods and what the wire needs. `internal/db` is the
opposite, its exported methods are the storage API. A type that two processes share lives in a contract package and
is aliased where it is used (`type Device = assistantwire.Device`).

## TypeScript

| Thing | Convention | Example |
| --- | --- | --- |
| Component | `PascalCase.tsx`, one main export of the same name | `ConnectionPill.tsx` |
| Logic module | `kebab-case.ts` in `src/lib/` | `session-groups.ts` |
| Hook | `use-<name>.ts` | `use-shortcuts.ts` |
| Small parts of one screen | `<Screen>Parts.tsx` | `SidebarParts.tsx` |
| Test | `X.test.ts(x)` beside X; a topic as `X.<topic>.test.tsx` | `Home.paging.test.tsx`, `AppAgentArchives.lazy.test.ts` |
| Generated types | `*-wire.ts` | `todo-view-wire.ts` |
| Element a test or script selects | a `data-*` attribute or `Button testId` | `data-connection-pill` |
| Shared CSS | Tailwind utilities inline; the few shared class names are semantic | `touch-hit`, `busy-motion` |

## Names that are data

These are read by other versions, other processes, stored rows or the owner's files. You add new ones; you never
rename or reuse one.

| Kind | Form | Example |
| --- | --- | --- |
| Route | `"METHOD /api/v1/<area>/<thing>"`, Go mux patterns | `POST /api/v1/assistant/captures` |
| RPC method, event type | `<area>.<verb_or_thing>`, snake case | `assistant.track_inbox_write`, `ws.broadcast` |
| Node capability, client feature | a snake-case word with a version | `track_inbox_v1`, `persona_contracts_v1` |
| Tool | snake case, verb last or first as its family does | `a2a_send`, `task_plan`, `persona_settings` |
| API error code | snake case, stable | `agent_not_listed` |
| Migration | `YYYYMMDDHHMMSS_<snake_case>.sql` | `20261123053622_assistant_captures.sql` |
| Table and column | snake case, plural tables | `assistant_captures` |
| Text key | `<namespace>.<snake_name>`; the namespace is the file name, `area-feature` | `assistant-captures.push_title` |
| Prompt template | `<scene>/<snake_name>.md`; its name is the path | `agents/a2a_message` |
| Setting key | dotted lower case | `navigation.start_page` |
| Environment variable | `PERSONA_<UPPER_SNAKE>` | `PERSONA_HOME`, `PERSONA_TEST_DATABASE_URL` |
| Language code | canonical, without a region | `en`, `zh-Hans`, `ja` |

Identifiers containing `jarvis` are the product's earlier name. They stay where another version or stored data
reads them (`06`); nothing new is named that way.

## Modules and what may import what

| Module | Path | Imports |
| --- | --- | --- |
| The server | `server/` | `sdk/go` through a `replace`; third-party modules |
| The extension SDK | `sdk/go/` (its own `go.mod`, no dependencies) | nothing of this repository |
| The web app | `web/` (one `package.json`) | generated types from `schema/`; nothing of `server/` |
| A runtime package | `integrations/<name>/` | `integrations/runtime-sdk` |

Within the server the direction is `cmd → root → domain → platform`, with contract packages importable by all
(`02`). Within the web app it is `views → ui → lib` (`07`).

## Comments

A doc comment starts with the name it documents and says what the thing guarantees and why it must be so. A file
that opens a feature may begin with a short block that says what the feature is. No narration of history, no "new",
"old", "now" or "previously", no issue numbers. When you touch code under a stale comment, correct the comment.

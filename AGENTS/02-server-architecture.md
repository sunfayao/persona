# Server architecture

> The layering of the Go server and the rules tests enforce on it. Read this before you add a package, a file to
> `assistant`, `node` or `hub`, an import between parts, or anything that starts a process or opens a listener. You
> usually arrive from core constraints 1 and 2 of `AGENTS.md`.

---

## The parts and the direction of dependencies

```
cmd/<program>  →  a root (hub | assistant | devices | extmgr | node)  →  its domain packages  →  platform packages
                                                                            (never back up, never sideways)
```

| Part | Packages | May import | Must not import |
| --- | --- | --- | --- |
| Hub | `hub`, `hubedge`, `hubcli` | contract and platform packages | any manager, `node`, connectors, `ai` |
| Managers | `assistant`, `devices`, `extmgr`, `manager` | contract and platform packages, their own domains | `hub` |
| Node | `node`, `nodecmd`, `link` | contract and platform packages, its own domains | hub, managers, `db`, any database driver |
| Domain package | `assistant/agents`, `node/ports`, ... | platform and contract packages | its root, a sibling domain |
| Contract package | `proto`, `rpc`, `nodeproto`, `*wire` | the standard library and other contract packages | everything else |

The composition roots are `hub.Run` (`hub/hub.go`), `assistant.New` and `Service.Run` (`assistant/service.go`,
`assistant/schedule.go`), `devices.New`, `extmgr.New`, `node.New`. A root wires its domains and implements the
interfaces they ask for; it holds no logic that a domain could hold.

## The hub is only a communication center

The hub authenticates, routes, relays and serves files. It calls no model, reads no manager file, owns no product
table and answers only the routes listed in `hub/testdata/hub_routes_allowed.txt`. A manager route is relayed
automatically from the manager's route list (see `03`), so adding one never touches the hub. If your change needs
the hub to "know" something about a task, a to-do, an inbox item or an agent, it belongs in a manager.

## New code goes into a domain package

`assistant` (about 200,000 lines), `node` (160,000) and `hub` (63,000) are single Go packages: one compile unit, one
test binary, thousands of methods on one struct. They are being split (`docs/plans/plan-server-packages.md`), and
`archguard` records their size: a recorded package may grow by at most one percent. So:

- You add a feature as a package of its own under its root (`assistant/<domain>`, `node/<domain>`), not as files in
  the root. `assistant/agents` and `node/ports` are the models to copy.
- The package has **one constructor** that takes its dependencies as **small interfaces it declares itself**, grouped
  by purpose (a store, the node calls, a clock, a logger). The root implements them in one adapter file
  (`assistant/agents_domain.go`).
- The package keeps **no mutable package-level state** and reads the environment only in its constructor. State lives
  on its struct, so its tests run in parallel.
- It takes a **clock** wherever it waits, stamps or expires; its tests drive a fake clock and never sleep.
- The root keeps the names the outside sees (routes, RPC methods, tool names, table names). A move changes no
  behaviour.

Skeleton of a domain package:

```go
package widgets

type Store interface { // only what this package calls
	InsertWidget(ctx context.Context, w Widget) error
}
type Clock interface{ Now() time.Time }

type Dependencies struct {
	Store func() Store
	Clock Clock
	Logf  func(string, ...any)
}

type Domain struct{ /* fields set from Dependencies */ }

func New(deps Dependencies) *Domain { /* default the clock, copy the fields */ }
```

Inside a root you are still allowed to fix and change existing files. What you must not do is add a new feature
there, add a package variable that a test has to flip, or add an import the table above forbids.

## Rules that tests enforce

Each of these fails `go test` in the named place. None can be satisfied by adding a line to an allowlist: the lists
are shrink-only, and a stale entry fails too.

| Rule | Where it is checked |
| --- | --- |
| Parts stay apart (table above), transitively | `archguard` `TestPartsStayApart`; `cmd/persona-{hub,manager,node}` link guards with `internal/depguard` |
| Domain packages import neither their root nor a sibling; package size limits (80 files, 15,000 lines) | `archguard` `TestPackageRules`, baseline `package_rules_baseline.json` |
| Contract packages are pure; every `*wire` package is listed | `archguard` `TestContractsArePure`, `testdata/contracts.txt` |
| The hub answers only its own routes, imports no manager, calls no model, keeps a small store | `hub/architecture_guard_test.go` |
| Each side's store touches only its own migration set's tables | `hub/store_tables_guard_test.go` |
| Every listener comes from `osproc.Listen` or `osproc.FileListener`, never `net.Listen` | `archguard/listen_test.go` |
| `osproc.ReapInherited` runs before anything that can start a child process | `archguard/reap_test.go` |
| No package-level `regexp.MustCompile`: use `lazyre.New` | `internal/lazyre/guard_test.go` |
| Never the machine's time zone (`time.Local`, `time.Now().Format`): use the user's zone | `internal/tzguard` |
| No build constraint on `public` or a `ts_omit_*` tag: there is one build | `internal/update` `TestOneBuild` |
| Only listed user actions close an agent's terminal pane; every path that starts an agent is supervised or listed | `assistant` and `node` `terminal_guard_test.go`, `assistant/start_paths_test.go` |

Lower a baseline after you shrink something with the switch its failure message names
(`UPDATE_GOLDEN=1 go test ./internal/archguard -run '^TestPackageRules$'`).

## Conventions inside the code

| Topic | Convention |
| --- | --- |
| Construction | `New(Options)` or `New(Dependencies)` with injected functions and interfaces; fakes implement them in tests |
| Logging | An injected `logf(format, args...)`; messages start with the component (`"assistant: push: %v"`). No `slog`, no global logger |
| Background work | A loop is started from the root's `Run(ctx)` and selects on `ctx.Done()`; work that must outlive a request uses `context.WithoutCancel` |
| Errors | Wrap with `fmt.Errorf("...: %w", err)`; sentinels with `errors.Is`; anything a person reads is an `i18n.Err` (`08`) |
| Platforms | `x.go` plus `x_unix.go`, `x_windows.go`, `x_darwin.go`, `x_linux.go` or `x_other.go`. Linux, macOS and Windows builds of the node and link must keep compiling |
| Embedded outputs | `internal/runtimepkg/dist` and `internal/hub/webdist` are build outputs: run `go generate ./internal/runtimepkg` before building or testing |

## Traps

- A package that the hub and a manager share must not import a manager-side package: the hub would link it and the
  link guard fails.
- A test that sets a package variable or the environment cannot run in parallel; in a root restore the old value in
  `t.Cleanup`, in a domain package do not need it.
- A unix-only call in a file without a build constraint breaks the Windows build; `go vet` with `GOOS=windows` finds
  it.
- A status belongs to the component that sets it and can be skipped: do not make a status transition the only
  trigger of something that must happen.
- A reservation taken before a check is given back on every path that leaves without doing the work, or one failed
  path blocks the resource for good.
- A mutex held across a database call must not be reachable from under a lock that hot paths take.

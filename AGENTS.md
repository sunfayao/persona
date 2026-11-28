# Persona — rules for coding agents

This file is the top-level rule set you must follow whenever you develop, change or debug anything in this
repository. It is complete on its own: read it and you can start working. The files it indexes under `AGENTS/` go
deeper; open one when the table below says your task needs it.

---

## The project

**Persona** is a self-hosted agent product for one owner: a hub on a server, a node on each of the owner's computers
that runs coding agents, an assistant that plans and supervises their work, and a web app as the only client. This
repository is the owner's own project. It began as a fork of an open-source project and no longer follows it or
takes anything from it. One person works on it with coding agents. There are no issues, pull requests, hosted CI,
releases or native clients.

| Layer | Technology |
| --- | --- |
| Server | Go 1.27, one module in `server/`: programs in `server/cmd/`, 140 packages in `server/internal/` |
| Storage | Postgres through pgx v5, hand-written SQL, embedded migrations in two sets (hub, manager) |
| Web app | React 19, TypeScript 5.9 strict, Vite 7, Tailwind 4, vitest 4; own router and store |
| Agent runtimes | TypeScript packages in `integrations/`, embedded into the node |
| Extensions | `sdk/go` (separate Go module), `sdk/ts`, JSON Schemas in `schema/` |
| Tooling | Python 3 scripts in `scripts/`: test runner, deploy, generators, mock hub, privacy scan |

Processes: `web app ⇄ hub ⇄ manager (assistant, devices, extmgr) ⇄ node on each computer ⇄ agents`. Hub, manager
and nodes are replaced at different moments, so parts of different versions always talk to each other.

---

## Core constraints (you must follow these)

### 1. The hub is only a communication center

**You must not put product logic, a product table, a model call or an import of a manager into
`server/internal/hub`.** The hub authenticates, routes, relays and serves files. Whatever knows about tasks,
to-dos, the inbox, agents or extensions belongs to a manager (`assistant`, `devices`, `extmgr`). A manager's routes
are relayed from its route list automatically, so a new product route never touches the hub.

Details → [AGENTS/02-server-architecture.md](AGENTS/02-server-architecture.md)

### 2. New code goes into a domain package, not into a giant root

**You must add a feature as a package of its own under its root (`assistant/<domain>`, `node/<domain>`), never as
new files in `assistant`, `node` or `hub`.** Those roots are single packages of 60,000 to 200,000 lines and are
being split; a guard lets them grow by at most one percent. A domain package has one constructor, takes its
dependencies as small interfaces it declares itself, keeps no package-level state, takes a clock, and imports
neither its root nor a sibling domain. `assistant/agents` and `node/ports` are the models.

Details → [AGENTS/02-server-architecture.md](AGENTS/02-server-architecture.md)

### 3. A domain registers its own entries

**You must declare a route, a node method, a capability, a scope or a feature flag in the domain's own file and
append it to the shared list; you must not edit a central switch or a hub allowlist to make it work.** Handlers
bound the request body, check the caller's role, return errors through `apierr` with a stable `code`, and leave
logic to the domain. A node validates everything it is sent.

Details → [AGENTS/03-server-entry-points.md](AGENTS/03-server-entry-points.md)

### 4. Every contract change is additive and gated

**You must not rename, repurpose or remove a route, RPC method, event type, tool name, JSON field, error code or
stored identifier that another version can still use.** Add a new one, gate it behind a capability the peer
announces or a feature flag of the hello, tolerate its absence, and tolerate unknown fields and values. Change the
Go type, the wire sample in `spec/fixtures/wire/` and `web/src/lib/types.ts` together.

Details → [AGENTS/06-wire-contracts-and-extensions.md](AGENTS/06-wire-contracts-and-extensions.md)

### 5. Migrations are append-only and belong to one side

**You must not edit, rename or delete an applied migration.** A new one is named by the UTC second it was written
(`date -u +%Y%m%d%H%M%S`), lives in exactly one set (`migrations/hub` or `migrations/manager`), is idempotent, and
keeps an older build working on the new schema. All SQL lives in `server/internal/db`; users of a store declare the
narrow interface they need.

Details → [AGENTS/04-database.md](AGENTS/04-database.md)

### 6. Text for models lives in `internal/prompts`, in English

**You must not build prompt text from Go string literals, and you must not write a template in any language but
English.** One template per file, rendered by a typed function; the owner's language enters only as a name printed
with `{{lang .Language}}`. Goldens are regenerated with `UPDATE_GOLDEN=1` and their diff is the review of your
change. A message Persona types into a conversation changes together with its recogniser, and legacy recognisers
stay. A tool name is a wire name.

Details → [AGENTS/05-prompts-and-agent-tools.md](AGENTS/05-prompts-and-agent-tools.md)

### 7. Text for people comes from the catalogs, in three languages

**You must not hard-code a string a person will read.** Add the key to the namespace file in English, Simplified
Chinese and Japanese (`server/internal/i18n/locales`, `web/src/i18n/locales`) and use `i18n.T` / `i18n.Err` or
`t()`. Delete a key when its last use goes: tests fail on unused keys, on missing translations and on new Chinese
literals. Comments, documents, logs and commit messages are English.

Details → [AGENTS/08-text-and-languages.md](AGENTS/08-text-and-languages.md)

### 8. Records are regenerated, never edited, and never padded

**You must not edit a generated file, a golden, a wire sample or a baseline by hand, and you must not add a line to
an allowlist to make a test pass.** Generated code comes from `schema/` through `scripts/gen/`. Allowlists and
baselines are shrink-only: either the code is wrong, or the record is regenerated with the switch its failure names
and the diff is reviewed.

Details → [AGENTS/09-testing.md](AGENTS/09-testing.md)

### 9. The web app stays lazy, tokenised and honest about state

**You must make every new page and dialog lazy, use the design tokens and the components of `src/ui/`, and send
messages only through the outbox.** The first screen has a byte budget that only `npm run build` checks. Colours,
radii and text sizes are tokens of `src/styles.css`. A send shows Sending, then Processing when the hub has it, and
is never repeated as a new message when its fate is unknown. Paths go through `BASE`; persisted keys through
`compatibility.ts`.

Details → [AGENTS/07-frontend-development.md](AGENTS/07-frontend-development.md)

### 10. Verify on the build machine, through the runner

**You must not report a result from a run without the test database, and you must not run a whole big package with
plain `go test`.** Server tests run on the Linux build machine through `scripts/test/server_test_env.py`; a whole
run goes through `scripts/test/go_test_partitioned.py` with `-p 16` and takes about a minute. Every test is a fixed
unit test (no real waiting, no child process, no listener but `httptest`, milliseconds). Compare every failure with
the trunk before you call it yours or not yours, and report what ran and what it printed.

Details → [AGENTS/09-testing.md](AGENTS/09-testing.md)

### 11. Work on a task branch; the trunk, the machines and the secrets are the owner's

**You must not merge into `main`, deploy, force-push, delete branches, restart a service, or read the owner's live
configuration unless the owner asked for exactly that.** Work on `task/<slug>` cut from `origin/main`, commit in
Conventional Commits in English without attribution trailers, push the task branch, and clean up your worktree when
it is merged. Nothing private enters a file, a fixture, a log or a commit message; the privacy hook is never skipped.

Details → [AGENTS/12-version-and-policies.md](AGENTS/12-version-and-policies.md) and
[AGENTS/10-deployment.md](AGENTS/10-deployment.md)

### 12. Remove completely, add nothing speculative

**You must not leave code, tests, fixtures, text keys, mock data or documents of a feature you removed, and you
must not add a fallback, an option or a compatibility path that no caller needs today.** The exception is a reader
of data that can still exist on the owner's machines or in old transcripts: that stays until the data cannot.

Details → [AGENTS/12-version-and-policies.md](AGENTS/12-version-and-policies.md)

---

## Index of the detailed rules

| No. | File | Contents | Read it when |
| --- | --- | --- | --- |
| 01 | [01-project-overview.md](AGENTS/01-project-overview.md) | What Persona is, its processes, technology, directory map, the path of one message | You are new here or cannot tell where a change belongs |
| 02 | [02-server-architecture.md](AGENTS/02-server-architecture.md) | Parts and dependency direction, the hub's limits, domain packages, guards on layering | You add a package, a file in a root, an import, a listener or a child process |
| 03 | [03-server-entry-points.md](AGENTS/03-server-entry-points.md) | Routes, node calls, events, handler shape, errors, loops | You add or change a route, an RPC method, an event or a client-visible error |
| 04 | [04-database.md](AGENTS/04-database.md) | Migration sets and rules, stores, settings, secrets, files on disk | You add a table, a column, a query, a setting or a data file |
| 05 | [05-prompts-and-agent-tools.md](AGENTS/05-prompts-and-agent-tools.md) | Templates, English-only, goldens, injected messages, agent-to-agent messages, defining a tool | You change anything a model reads or a tool it can call |
| 06 | [06-wire-contracts-and-extensions.md](AGENTS/06-wire-contracts-and-extensions.md) | Contract packages, compatibility rules, samples and fixtures, generated code, extensions, runtimes | A change crosses a process boundary or touches `schema/`, `spec/`, `sdk/`, `integrations/` |
| 07 | [07-frontend-development.md](AGENTS/07-frontend-development.md) | Web app structure, lazy loading, data access, design rules, checking a change | You touch anything under `web/` |
| 08 | [08-text-and-languages.md](AGENTS/08-text-and-languages.md) | The three kinds of text, adding a string, what the catalog tests require | You write any string a person or a model will read |
| 09 | [09-testing.md](AGENTS/09-testing.md) | Where tests run, the commands, reading a red run, writing a test, regenerating records | You run, add or report on tests |
| 10 | [10-deployment.md](AGENTS/10-deployment.md) | What runs where, the deploy command, changes that affect a running system | You are asked to deploy, or change start-up, update or restart behaviour |
| 11 | [11-naming-and-modules.md](AGENTS/11-naming-and-modules.md) | Names of files, packages, identifiers and stored things; module boundaries; comments | You create a file, a package, a key or a name another part reads |
| 12 | [12-version-and-policies.md](AGENTS/12-version-and-policies.md) | Git workflow, commits, what never enters the repository, the full prohibited and required lists | Before your first commit, and whenever you are unsure whether you may do something |

Reference material that these files link to lives in `docs/` (the protocol, the design system, configuration,
per-integration behaviour). It describes how things work; it does not override a rule here. Where a document and
the code disagree, the code is right and the document is to be corrected or deleted.

---

## Keeping these documents in step

Your context window is limited: every stale or irrelevant line you read is a line of code or rule you could not
load. The same holds for these files. They record **patterns, global conventions and core infrastructure**, never a
single route, screen or test. After each change, decide: did it alter a pattern these files describe? If so, update
the owning file, briefly, in the same commit. If not, write nothing.

### Changes that require an update

| Code change | File to update | What to update |
| --- | --- | --- |
| A top-level directory, a program in `server/cmd/`, a process or a link between processes is added or removed | `01`, and the tree below | The table or diagram that names it |
| A layering rule, an `archguard` rule, the shape of a domain package or of a composition root changes | `02` | The dependency table, the guard table or the skeleton |
| The way routes, node methods, capabilities, scopes or events are registered or encoded changes | `03` | The registration table and the handler shape |
| The migration naming or freeze rule, the store pattern, the settings registry or the data directory layout changes | `04` | The affected rule table |
| The template rules, template functions, golden workflow, injected-message recognition or tool definition steps change | `05` | The affected section |
| A contract package is added, a compatibility rule changes, a schema gains a generated output, the extension guarantees change | `06` | The package table, rule table or generator table |
| The web entry points, the lazy-loading mechanism, the store or API conventions, a design rule or the build checks change | `07` | The affected table or rule |
| A language is added, the catalog layout or a catalog test rule changes | `08` | The affected table |
| The test runner, its flags, the partition list, a regeneration switch or the required checks change | `09`, and the commands below | The command block or the record table |
| `scripts/deploy` behaviour, the service layout or the update mechanism changes | `10` | The affected table |
| A naming convention or a module boundary changes | `11`, and the naming table below | The affected row |
| The git workflow, the privacy scan, or a prohibited or required item changes | `12`, and the two quick lists below | The affected row, in both places |

### Changes that do not

| Code change | Why |
| --- | --- |
| A new route, tool, page, component, table or setting that follows the existing pattern | The code is the record; these files hold patterns |
| A bug fix that changes no interface | Implementation detail |
| Refactoring inside a package, performance work | The pattern is unchanged |
| Copy, layout or style changes | Interface detail |
| A new private function or type inside a package | No effect across packages |
| A dependency version bump | Not an architectural change |
| New tests for existing behaviour | Tests explain themselves |
| A package moved out of a root following `docs/plans/plan-server-packages.md` | The plan document records the move; update `02` only if the pattern changed |

### What each file may contain

| File | Only | Never |
| --- | --- | --- |
| `AGENTS.md` | Constraints, the index, these tables, the quick reference | Code, per-feature detail |
| `01` | What the system is and where things are | Rules of how to write code |
| `02` | Layering, package shape, layering guards | Route or prompt details |
| `03` | How entries are registered, handler and call shapes | A list of routes or methods |
| `04` | Migration, store, settings and file rules | Table schemas |
| `05` | Rules for model-facing text and tools | The wording of any prompt, a list of tools |
| `06` | Contracts, compatibility, generated code, extension guarantees | Field lists of any message |
| `07` | Web structure, data access, design rules | The layout of any screen, a component catalogue |
| `08` | Which text goes where and how to add it | Any string or glossary |
| `09` | How to run, read and write tests | Any test's assertions, lists of known failures |
| `10` | How deployment works and what is off limits | Machine names, addresses, credentials |
| `11` | Naming and module conventions | Lists of existing names |
| `12` | Workflow, policy, the two complete lists | Technical how-to that belongs to 02 to 11 |

---

## Quick reference

### Directory tree

```
persona/
├── AGENTS.md                    # this file
├── AGENTS/                      # detailed rules, 01 to 12
├── README.md
├── config.example.json          # shape of the deployment configuration
├── server/                      # the Go module
│   ├── cmd/                     # personad, persona-hub, persona-manager, persona-node, persona-link,
│   │                            #   persona-projects, password-wasm
│   ├── build-tags.txt           # the build tags of the one build (generated)
│   └── internal/
│       ├── hub/                 # the communication center
│       ├── assistant/           # the assistant manager (root) and its domains: agents/, repository/, todo/, ...
│       ├── devices/, extmgr/    # the device and extension managers
│       ├── node/                # the node (root) and its domains: doctor/, files/, ports/, preview/, ...
│       ├── db/                  # stores; migrations/hub and migrations/manager
│       ├── prompts/             # every template and tool description
│       ├── i18n/                # server text catalogs, three languages
│       ├── proto/, rpc/, nodeproto/, *wire/   # contract packages
│       ├── archguard/           # layering and size rules
│       └── ...                  # platform packages (apierr, osproc, lazyre, textfmt, transcript, ...)
├── web/                         # the web app
│   ├── src/                     # views/, ui/, lib/, features/, i18n/, admin/, screen/, sharing/, showcase/
│   ├── public-share/            # the public share viewer
│   └── e2e/                     # scripted browser checks
├── integrations/                # agent runtime packages and the runtime SDK
├── sdk/                         # go/ (separate module), ts/, templates/
├── schema/                      # configuration/, extension/, wire/: sources of generated code
├── spec/                        # fixtures shared by server and web tests
├── scripts/                     # test/, deploy/, gen/, mock/, ci/, diag/, hooks/, privacy_check.py
└── docs/                        # reference material
```

### Common commands

```bash
# --- server: build and check (on the build machine, in server/) ---
export GOPROXY='https://goproxy.byted.org|direct' GOFLAGS=-trimpath
go generate ./internal/runtimepkg
go build -tags "$(cat build-tags.txt)" ./... && go vet -tags "$(cat build-tags.txt)" ./...
gofmt -l internal cmd

# --- server: tests ---
python3 -I ../scripts/test/server_test_env.py --database-file "$PERSONA_TEST_PROFILE" -- \
  go test -count=1 ./internal/<package> -run '^TestName$'
GOMAXPROCS=24 python3 -I ../scripts/test/server_test_env.py --database-file "$PERSONA_TEST_PROFILE" -- \
  python3 -I ../scripts/test/go_test_partitioned.py go test -count=1 -p 16 -timeout 30m ./...

# --- server: regenerate records (read the diff afterwards) ---
UPDATE_GOLDEN=1 go test ./internal/<package> -run Golden
UPDATE_GOLDEN=1 go test ./internal/archguard -run '^TestPackageRules$'
PERSONA_UPDATE_ROUTE_SCOPES=1 go test ./internal/hub -run TestRouteScopes
go test -count=1 ./internal/hub -run '^TestWireSamples$' -update

# --- repository checks (from the root) ---
scripts/test/check-fast.sh                      # gofmt, migration freeze, English-only prompts, privacy
for d in ci deploy diag mock test; do (cd scripts/$d && python3 -m unittest discover -p 'test_*.py') || break; done
python3 scripts/gen/extension_schema.py --check # generated wire code matches the schemas

# --- web (in web/) ---
npm ci --no-audit --no-fund
npx tsc -b && npx vitest run <files> && npm run build
npx vitest run --maxWorkers=24                  # every web test

# --- look at the web app against made-up data ---
PORT=18760 python3 -I scripts/mock/mock_hub.py
cd web && PERSONA_HUB_URL=http://127.0.0.1:18760 npx vite --port 5173 --strictPort

# --- git ---
git config core.hooksPath scripts/hooks          # once per clone
git fetch origin && git switch -c task/<slug> origin/main
```

### Naming

| Thing | Style | Example |
| --- | --- | --- |
| Go package | one lower-case word; contracts end in `wire` | `agents`, `assistantwire` |
| Go file | `snake_case.go`, platform suffix | `capture_sort.go`, `ports_windows.go` |
| Go test | a sentence | `TestOnlyUserActionsCloseAgentTerminals` |
| React component | `PascalCase.tsx` | `ConnectionPill.tsx` |
| Web logic module, hook | `kebab-case.ts`, `use-<name>.ts` | `session-groups.ts`, `use-shortcuts.ts` |
| Web test | `X.test.tsx`, `X.<topic>.test.tsx` | `Home.paging.test.tsx` |
| Route | `"METHOD /api/v1/<area>/<thing>"` | `POST /api/v1/assistant/captures` |
| RPC method, tool, error code | snake case | `assistant.track_inbox_write`, `a2a_send`, `agent_not_listed` |
| Migration | `YYYYMMDDHHMMSS_<snake>.sql` | `20261123053622_assistant_captures.sql` |
| Text key | `<area-feature>.<snake_name>` | `assistant-captures.push_title` |
| Prompt template | `<scene>/<snake_name>.md` | `agents/a2a_message.md` |
| Branch | `task/<slug>` | `task/inbox-grouping` |
| Commit | Conventional, English | `fix(node): keep the pane when a restart is refused` |

### Prohibited

| No. | You must not |
| --- | --- |
| F1 | put product logic, a product table, a model call or a manager import into `internal/hub` |
| F2 | add a feature as new files in `assistant`, `node` or `hub`; or import a root or a sibling domain from a domain package |
| F3 | add a line to an allowlist, baseline or golden to make a test pass |
| F4 | rename or reuse a route, RPC method, event type, tool name, JSON field, error code, text key in use, table, column or setting key |
| F5 | edit, rename or delete an applied migration, or put a migration in the wrong set |
| F6 | build prompt text in Go string literals outside `internal/prompts`, or write a template in a language other than English |
| F7 | hard-code text a person reads instead of using the catalogs; or use a catalog key in a prompt |
| F8 | edit a generated file, a golden, a wire sample or a baseline by hand |
| F9 | call `net.Listen`, package-level `regexp.MustCompile`, or use the machine's time zone; start a child before `osproc.ReapInherited` |
| F10 | run a whole big package with plain `go test`, or report a run made without the test database as passing |
| F11 | send a chat message with a bare `api()` post, write `/api/...` URLs by hand, or use raw `localStorage` for a persisted key |
| F12 | statically import a lazy page from first-screen code, or add first-screen strings without building |
| F13 | read, edit or print the owner's live configuration, data directory or secrets; restart or signal a running service |
| F14 | merge into `main`, deploy, force-push or delete branches without being asked; open issues or pull requests |
| F15 | commit a secret, a real name or address, or skip the privacy hook |
| F16 | add a compatibility path, a fallback or an option without a caller that needs it today |
| F17 | keep code, tests, fixtures, text keys or documents of a feature you removed |
| F18 | add a document, report or plan file that the task did not ask for |

### Required

| No. | You must |
| --- | --- |
| M1 | put a new feature in a domain package with one constructor, narrow interfaces it declares, no package state and an injected clock |
| M2 | register routes, node methods, capabilities, scopes and feature flags from the domain's own file |
| M3 | gate every new thing a peer must understand behind a capability or feature flag, and keep working when it is absent |
| M4 | bound request bodies, validate on the node, check the caller's role in the handler |
| M5 | return errors a person reads as `i18n.Err`, with a stable `code` when a client must branch |
| M6 | add text for people to all three languages, and delete a key when its last use goes |
| M7 | keep templates under `internal/prompts`, render through a typed function, regenerate goldens with the documented switch |
| M8 | update a recogniser in the same commit as the injected message it recognises, keeping the legacy shapes |
| M9 | name a migration by its UTC second and make its DDL idempotent and backward compatible |
| M10 | change the Go type, the wire sample and `web/src/lib/types.ts` together when a client-visible shape changes |
| M11 | make new pages and dialogs lazy, use tokens and `src/ui/` components, and look at the change in light and dark at phone and desktop widths |
| M12 | run build, vet, gofmt, the whole partitioned test run and `check-fast.sh` before handing over; for web also `tsc -b` and `npm run build` |
| M13 | compare every failure with the trunk before calling it yours or not yours, and report results exactly |
| M14 | write code comments, documents, prompts and commit messages in English |
| M15 | update the rule file that owns a pattern when your change alters that pattern |
| M16 | leave no branch, worktree, scratch file or build directory behind when the task is done |

# Testing

> Where and how tests run, how to write one, and how to read a red run. Read this before you run anything, add a
> test, or report that something passes. You usually arrive from core constraint 10 of `AGENTS.md`.

---

## Where tests run

Server tests need Go 1.27, Postgres and the embedded runtime packages. They run on the **Linux build machine**, in a
checkout of your branch, never against the owner's live data directory. A machine without the test database skips
every database test silently and still prints `ok`: such a run proves nothing.

## The commands

```bash
export GOPROXY='https://goproxy.byted.org|direct' GOFLAGS=-trimpath
cd server
go generate ./internal/runtimepkg                    # once per checkout: packs integrations/ into the node
go build -tags "$(cat build-tags.txt)" ./... && go vet -tags "$(cat build-tags.txt)" ./...
gofmt -l internal cmd                                # must print nothing

# One package or a few tests (seconds):
python3 -I ../scripts/test/server_test_env.py --database-file "$PERSONA_TEST_PROFILE" -- \
  go test -count=1 ./internal/assistant -run '^(TestA|TestB)$'

# Everything, about a minute once the test binaries are built (two and a half minutes when every one is rebuilt):
GOMAXPROCS=24 python3 -I ../scripts/test/server_test_env.py --database-file "$PERSONA_TEST_PROFILE" -- \
  python3 -I ../scripts/test/go_test_partitioned.py go test -count=1 -p 16 -timeout 30m ./...

cd .. && scripts/test/check-fast.sh                  # gofmt, migration freeze, English-only prompts, privacy

# The web app's tests, about a hundred seconds:
(cd web && npx vitest run --maxWorkers=24)

# Every script test, under twenty seconds:
for d in ci deploy diag mock test; do (cd scripts/$d && python3 -m unittest discover -p 'test_*.py') || break; done
```

| Rule | Why |
| --- | --- |
| Always through `server_test_env.py` | It isolates the run from the live profile and selects the test database |
| A whole big package (`assistant`, `node`, `hub`, `devices`, `extmgr`, `voice`) only through `go_test_partitioned.py` | Their thousands of tests run one after another inside a process; the runner splits each into several processes. A plain `go test ./...` waits almost two minutes for `assistant` alone |
| `-p 16` for a whole-module run | With `-p 1` the 180 small packages run one at a time |
| `-count=1` | The test cache does not see inputs outside `server/` (fixtures, schemas, templates) |
| Narrow with `-run` while you work; run everything once before you hand over | Fast loop, honest end |
| Several runs at once are fine | Every test process creates and drops its own database |
| Under `timeout`, exit 124 is a timeout; any other non-zero exit is a failure even when every partition printed `ok` lines | Read the exit status, not only the tail |
| `GOFLAGS=-trimpath` | All checkouts share one build cache. A test therefore finds repository files from its working directory (the package directory), never through `runtime.Caller` |

Web tests are in `07`. Script tests are Python `unittest` files beside the scripts
(`cd scripts/test && python3 -m unittest test_<name>`). A test that needs a git checkout (the privacy check, the SDK
bundle, the generated-copy guard) skips in a copy without one.

## What a test may do

Every test in the three suites is a fixed unit test: it calls the code in its own process with fakes for what that
code talks to, and it finishes in milliseconds. Scenarios that need real processes, a real browser or real waiting
are the scripted browser checks in `web/e2e/` and `scripts/test/web_*_e2e.py`, run only when asked for.

| A test may | A test may not |
| --- | --- |
| Call the code in process, with the package's fakes or small fakes of its own | Start a child process: git, tmux, herdr, node, bun, a shell, the test binary again |
| Use its own database through `dbtest`, a template copied rather than migrated again | Listen on a socket or reach the network, but for `httptest` |
| Use the injected fake clock, or a short package timeout restored in `t.Cleanup` | Wait on real time: a sleep, a poll with a real interval, a product timeout left at its length |
| Read fixtures, goldens and the source tree | Drive a browser, soak, hunt races, or loop hundreds of random seeds |

| Kind | Budget on the build machine, in a whole run | Over it |
| --- | --- | --- |
| Go test | well under 0.3 s | Make it instant with a knob the test already has (a shorter timeout the code takes, the fake clock, a smaller fixture, fewer seeds), or delete it with what only it used |
| Web test | under 0.25 s, and a file under 3 s | The same |
| Script test | under 0.5 s | The same |

Guards that read the whole source tree, migration tests, the contract fixtures shared with the web app and goldens may
take a second or two; nothing else may. A test that fails one run in ten, or only beside certain neighbours, is
deleted or fixed, never kept as a known failure.

## Reading a red run

1. Search the output for `--- FAIL` and `panic:`; a panic in one partition hides the results of its other tests.
2. Before you blame your change, run the failing test alone and on the untouched trunk. The suites are kept green on
   the build machine, so a failure there is one to explain.
3. A test that fails one run in ten under load and passes alone is a timing fault in the test. Say so in your
   report; do not "fix" it with a longer sleep: make it a fixed unit test or delete it.
4. A golden, a baseline or a fixture that changed means behaviour changed. Regenerate it only when that change is
   what you intended, and say which files moved.

You report results as they are: which commands ran, their last lines, what failed and why you believe it is or is
not yours. "Should pass" is not a result.

## Writing a test

| Topic | Convention |
| --- | --- |
| Name | A sentence about behaviour: `TestASnapshotCannotClaimAnotherManagersSession`, not `TestClaim2` |
| Place | `x_test.go` beside `x.go`, more scenario files as `x_<scenario>_test.go` |
| Database | `dbtest.Main(m)` in the package's `TestMain`; tests get a private database |
| Home directory | `testhome.Main` gives a throw-away `HOME` and fails the run if a test creates `~/.persona` |
| Time | In a domain package: the injected fake clock, no `time.Sleep`. In an old root: a short package timeout restored in `t.Cleanup`, or a channel; never a sleep to "let it settle" |
| Parallelism | A domain package's tests call `t.Parallel()` and pass `-count=3 -shuffle=on`. Tests that use `t.Setenv` or swap a package variable cannot be parallel |
| Fakes | The assistant's `fakeHub` and `newTestService` in `assistant/fake_test.go`; a domain package builds its own small fakes for the interfaces it declares |
| Goldens | `golden(t, name, got)`; regenerate with `UPDATE_GOLDEN=1`, never by hand, with placeholder data only |
| Live tests | Tests that need a real model, terminal or device read an environment variable (`PERSONA_LIVE_...`) and skip without it. Do not add one for something a fake can show |
| Environment | A test that calls an entry point which sets a process variable sets that variable with `t.Setenv` first, so it is restored |
| Test data | Invented names and `example.com` only; never a real conversation, machine or account. `scripts/mock/mock_hub.py` serves made-up hub data and `scripts/mock/mock_model.py` a made-up model endpoint |

What a test is for: behaviour that still exists. When you remove a feature you remove its tests, fixtures, goldens
and mock data with it. You do not keep a test of something that is gone, and you do not delete a failing test of
something that is still there.

## Guards and what "shrink-only" means

Some tests read the source tree instead of running code: the layering rules (`02`), the text catalogs (`08`), the
English-only prompts (`05`), route scopes and wire samples (`06`), package sizes. Their lists and baselines are
**shrink-only**: you never add a line to make a test pass. Either the code is wrong, or the record is regenerated
with the switch the failure names and its diff reviewed.

| Record | Regenerate with |
| --- | --- |
| Prompt and tool goldens | `UPDATE_GOLDEN=1 go test ./internal/<package> -run Golden` |
| Package size baseline | `UPDATE_GOLDEN=1 go test ./internal/archguard -run '^TestPackageRules$'` |
| Route scopes | `PERSONA_UPDATE_ROUTE_SCOPES=1 go test ./internal/hub -run TestRouteScopes` |
| Wire samples | `go test -count=1 ./internal/hub -run '^TestWireSamples$' -update` |
| Hard-coded text counts | `UPDATE_I18N_BASELINE=1 go test ./internal/i18n -run Hardcoded` (only ever lower) |
| Build tags file | `go run ./internal/buildtags/printtags > build-tags.txt` after a dependency bump |

## What must stay green before you hand over

`go build`, `go vet`, `gofmt`, the whole partitioned run with no failure, `check-fast.sh`, the script tests, and for web
changes `tsc -b`, the web tests and `npm run build`. For a change in `server/internal/node` or
`internal/link` also `GOOS=windows GOARCH=amd64 go vet ./internal/node/... ./internal/link/...` and
`GOOS=darwin GOARCH=arm64 go build ./...`.

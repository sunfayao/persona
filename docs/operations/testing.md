# Testing

## Commands

```sh
cd server && go test ./... && go vet ./...                    # server
n=0 failed=""; for f in $(find scripts -name 'test_*.py' -not -path '*/node_modules/*' | sort); do grep -q unittest "$f" || continue; n=$((n + 1)); (cd "$(dirname "$f")" && python3 -B -m unittest -q "$(basename "$f" .py)") || failed="$failed $f"; done; echo "$n suites, failed:${failed:- none}"; [ -z "$failed" ]   # all Python suites in scripts/, like CI (it skips files that never mention unittest); any failure exits nonzero
```

## Hosted CI

**No workflow runs automatically for a pull request, branch push, merge queue or schedule.** Before merging a PR, run the relevant
commands in "Build and verify" locally, inspect required screenshots and run `scripts/privacy_check.py`. The `CI required` check is
not a PR gate. A scheduler explicitly dispatches full CI on the latest `main`; a failure is fixed separately. Local PR verification
and scheduled post-merge CI are both required operational checks, not substitutes for one another. The independent
`main-verify-fix-loop` (`docs/operations/main-verification.md`) also checks new main commits for Go build/vet and repository-rule
failures and opens a bounded repair PR; the PR semantic-review/merge watcher does not repeat those full checks or own main repairs.

`ci.yml` accepts only `workflow_dispatch`. Its `full` boolean defaults to **true**, so a plain dispatch runs all four required
platform lanes even on unchanged `main`; path filtering cannot silently turn a default run
into a collection of skipped jobs. Set `full=false` to use `lanes`: `all`, `auto` (diff against `origin/main`), or comma-separated
`server,web,runtimes,scripts`. `base` overrides the comparison for `auto`. Unknown lane names fail.
Inspect selection locally with `python3 scripts/ci/paths.py --all` or `python3 scripts/ci/paths.py --base origin/main`.

```sh
gh workflow run ci.yml --ref main -f full=true                       # full suite (the default)
gh workflow run ci.yml --ref main -f lanes=all -f full=false         # equivalent explicit lane choice
gh workflow run ci.yml --ref <branch> -f full=false -f lanes=server,web
gh workflow run ci.yml --ref <branch> -f full=false -f lanes=auto -f base=<commit>
gh workflow run pr-needs-issue.yml --ref main -f pr_number=<number>  # metadata enforcement
```

The four required lanes have independent reusable workflows. **CI required** rejects failed, cancelled, missing or unexpectedly skipped
selected jobs, and any run whose path selection or guards did not succeed; it reads only the jobs' results, so it checks out one
commit, not the whole history. An unselected lane is never claimed as tested.
All workflows except the image publisher accept only `workflow_dispatch` and, for reusable platform workflows, `workflow_call`.
The image and Release publisher additionally retains `push` for **`v*` release tags**: Docker installations use its version and `latest` images, and the same tag gets a GitHub Release.
No branch push or PR starts it. The issue-link Action also requires explicit dispatch from the default branch with a PR number.

The `changes` and `guards` jobs and the scripts shards need history and the base, so they fetch every branch and tag, but with file
contents only at the tip (`filter: blob:none`, #12753); a full fetch would also download every screenshot branch's files, which no
check reads. Git fetches a blob a check reads at the base when it asks for it, without a token (checkout has dropped it by then; the
repository is public). One such fetch is quick; a new check there that reads many old files one at a time (`git blame`, `git log -p`
over a long range) would wait on each, so it belongs in a job with a full checkout. A fetch can also fail, and then `git show` and
`git cat-file -e` fail just as they do for a file the base does not have. So a check that reads a file at the base decides whether
the file is there from the tree (`git ls-tree <base> -- <path>`, which needs no fetch) and fails when a listed file cannot be read;
counting a failed read as "not at the base" would skip a never-raise comparison and pass. The migration freeze, the sleep and
prompt-language ratchets and the counts table's base read work this way.

The `guards` job (**Manual repository guards**) runs `scripts/ci/guards.sh`: repository/commit privacy and path safety, migration freeze, the sleep, web font and prompt-language ratchets ("Count ratchets" below).
It checks against the base the `changes` job (**Manual path selection**) resolved, so one run has one base. No platform lane needs it:
a failing guard says nothing about the platforms, so their results still come out, and **CI required**, which needs both
jobs, fails and names the guards. Before #12725 the guards ran inside `changes` and a guard failure skipped every lane.
An edit, deletion or rename of a `.sql` file already on the base under `server/internal/db/migrations/hub` or
`server/internal/db/migrations/manager` fails that check. Comment-only and whitespace-only edits pass; add a new migration instead.
A new file is named by the UTC second it is written, `YYYYMMDDHHMMSS_<name>.sql` in lower case (`$(date -u +%Y%m%d%H%M%S)_<name>.sql`),
a real time from 2026-11-18 00:00:00 UTC to a day after the check runs; a numbered `NNNN_<name>.sql` outside the frozen numbered
files (up to `0561`) fails, whatever the base. Of two files of one set that share a version on the base, one may be renamed
(`docs/server/server.md`, "Migrations").
Each `_test.go` file under `server/` and `sdk/go/` has a committed count of `time.Sleep` calls (`scripts/ci/sleep_baseline.json`). The
count may stay the same or go down, never up, and a new file starts at zero. Comments, string literals and a line carrying
`// sleep-ok: <reason>` (the reason must be non-empty) do not count. `python3 scripts/ci/sleep_ratchet.py --update` only lowers
the baseline; it refuses to raise a count or to add a file that still sleeps. See `docs/lessons/testing.md`.
Each file under `web/src` has a committed count of hard-coded pixel font sizes (`text-[13px]`, `text-[length:13px]`, inline `fontSize: 13`, CSS `font-size: 13px`; tests excluded) in `scripts/ci/web_font_baseline.json`. `python3 scripts/ci/web_font_ratchet.py` (in `scripts/ci/guards.sh`) fails when a count rises or a new file has one; `--update` only lowers the baseline. Use the named sizes (`text-xs`, `text-sm`, the `--chat-*` variables) instead; the role table is `docs/design/task-cards.md` section 4.1.
Both are count ratchets: only a rise fails, and lowering a baseline is tidying ("Count ratchets" below).
Both ratchets also run when a PR merges (#7977): `scripts/hooks/merge-pr.sh` runs `scripts/ci/changed_ratchets.py --base <base sha> --head <head sha>`.
It reads only the Go test files and `web/src` sources the PR changes and the baseline entries it edits, from Git objects and as the
squash merge leaves them (`git merge-file` where `main` changed a file too), without a checkout. Each ratchet's own counting and
messages apply, the sleep ratchet's never-raise check included, and the merge is refused only when a file ends further above its
baseline than `main` has it; a file the PR leaves under its baseline merges, as the checkers pass it. A file `main` already fails is
left to its own fix unless the PR moves it further; a file both sides change in conflicting ways is left to the merge, which refuses
it. It does not hold a changed file to its count at the base the way the checkers' `--base` mode does. A new per-file ratchet goes
into that script's `RATCHETS` too.
With `lanes=auto`, the base defaults to `origin/main`; an explicit `base` is resolved to a commit once, in `changes`, and the guards use that commit.

Dispatched code runs with `contents: read`, without persisted checkout credentials, private config or new secrets. The issue-link
workflow uses metadata-only write permissions and runs only from the default branch. Image and GitHub Release publishing have `packages: write` and `contents: write` only in the publish job. CI concurrency keys use the repository/branch and full/lanes selection; dispatches in the same group cancel stale
pending/running work, while different selections stay independent. Reusable workflows retain their file-specific lane groups: deriving
a caller and callee's group only from `github.workflow` would let a called workflow cancel its caller.

The manual server matrix allows at most **two** simultaneous shards; the full scripts matrix allows **one**. All shards
still execute with `fail-fast: false`; these caps trade wall-clock latency for available runner slots. Hosted queue time is outside
repository control and must be reported separately from test execution. Do not change repository settings/rulesets or procure paid or
self-hosted capacity as part of these controls.

Server keeps four independent shards (at most two concurrent runners): browser/node, hub/voice/assistant, all other packages, and
formatting/vet/build/Windows/SDK checks. Every package from `go list ./...` belongs to exactly one test shard, including future packages.
Browser tests remain isolated from other packages. `internal/node` runs its roughly 4,300 serial tests in six concurrent `go test -run '^(…)$'` partitions, split round-robin from `go test -list`; every test runs in exactly one, and each partition keeps the shard's `-p` and `-timeout=10m`. Its two partitions reached about 569 and 572 seconds of test time before the unchanged 600-second lane stopped them with subtests still advancing (#12256); its tests mostly wait (about 1,060 s of test time on under a core and a half), and on the CI machine three partitions took 360 s where six take 186 s, without removing a test or lengthening a timeout. `internal/assistant` (about 4,100 serial tests, 570 s in one process on the hub machine, #7644; about 7,300 by 2026-11-22) runs the same way in eight partitions in the managers shard beside `internal/hub` and `internal/voice`: its tests wait on timers and Postgres, not the CPU, and with the test database's `fsync` off eight finished within 165 s where three took up to 500 s. `internal/hub` (about 2,000 tests, 283 s in one process beside them) runs in three (112 s). In the other shard `internal/devices` (about 580 tests, 260 s in one process) and `internal/extmgr` (about 1,040 tests, 145 s) run in three partitions each (106 s and 59 s), beside the shard's other packages at `-p 4`. `scripts/test/go_partitions.py` holds the packages and their split for the shards, the release build (`scripts/release/build_hub_release.py`) and `scripts/test/test-server.sh` on the hub machine and `scripts/test/check-fast.sh`'s Go checks (through `scripts/test/go_test_partitioned.py`, which runs the partitions beside the other packages with the caller's flags and prints each partition's output whole when it ends; it also expands a `./<dir>/...` that covers `internal/assistant` with `go list`, leaving `internal/node` found there to its own split, #12765), so they cannot drift; each serial run had outgrown the ten-minute limits (#5328, #5594, #7644). A plain `go test ./internal/node`, as a PR verifier runs it (no `-run`, `-list`, `-bench`, `-fuzz`, `-v`, `-json`, coverage or profiles), splits itself the same way: its `TestMain` lists the tests and runs them round-robin in concurrent processes of the test binary, half the CPUs, at least three and at most six by default, prints each one's output whole when it ends, keeps go test's cache log, and fails when any partition does (`server/internal/node/partition_test.go`, #5734). A run with `-run` (the partitions above, a helper process) or with `NODE_TEST_PARTITIONS=1` runs in one process; `NODE_TEST_PARTITIONS=n` sets how many share the tests. Go module/build caches are keyed by exact Go version, shard, dependency sums and
source inputs, with same-shard fallback keys; `-count=1` keeps test execution uncached. Test-event summaries report the slowest real
tests, rather than inferring runtime from `Sleep` inside fake-clock tests, and end by repeating each failed test with its first lines
(from its panic, if any; a timeout's names the test still running) so an excerpt that keeps only a log's head and tail, like the
PR watcher's report, still says what failed (`scripts/ci/server-shards.py`). The full scripts suite splits complete sorted discovery
across two independently isolated jobs, scheduled one at a time; each has its own synthetic HOME and mock ports.

The Web suite's default file-worker count is `min(8, max(availableParallelism() - 1, 1))` (`web/test-scheduling.ts`, #12458). Vitest otherwise starts one fork per host CPU; under a CPU quota that can leave dozens of processes initializing isolated module graphs at once. On main `c866b5a377`, Node 24.22.3 under a four-CPU quota and 8 GiB memory limit, the complete suite took 79.06 seconds with two concurrency-sensitive assertion failures at the default 63 workers, 448.08 seconds with three workers, and 175.31 seconds with eight workers. Eight preserved all 1,542 files / 17,295 assertions (one skip in each count) and reduced CPU time from 2,301.7 to 1,660.4 seconds. The cap does not remove or reuse isolated test environments, and explicit project or command-line `maxWorkers` settings still override it for diagnosis. Measure wall time, test counts and failures together when changing it; the 600-second Web test deadline stays unchanged.

**Why tests stay uncached.** check-fast, the CI shards, `scripts/test/test-server.sh` and the bounded runner's
commands below all pass `-count=1`, so no Go test result comes from Go's test cache; the build cache still serves
compilation. The test cache reuses a result while the test binary, its flags and the environment variables and files
the test process itself consulted are unchanged, and it checks those files only inside the module root, `server/`
(`go help test`). Server tests read inputs outside it (`spec/fixtures/` alone in 19 packages; `schema/`, `sdk/go/`,
and `docs/` sources), which are exactly the paths check-fast routes to the packages that read
them, and guards such as `internal/archguard` read the module through a child `go list`, whose reads are never
recorded. A cached run then repeats an old `ok` for a change that breaks the test, while a hit saves only seconds per
package, and only on packages the change did not reach
(`docs/lessons/testing.md`, "Go's test cache can repeat a pass the change broke").
A plain `go test` while editing is fine; a result reported as verification comes from `-count=1`.

| Platform / reusable workflow | Local entry | Checks | Hosted runner | Planning duration (warm / cold) |
| --- | --- | --- | --- | --- |
| Server / `.github/workflows/server-test.yml` | `scripts/ci/server.sh` | gofmt, vet, complete Go tests against disposable Postgres 18, build, Windows cross-build/vet, the programs and tunnel tests with `server/build-tags.txt`, extension schema/examples/SDK bundle, Go SDK tests; hub route/architecture, prompts and hub i18n guards are Go tests | ubuntu-latest | 8–15 / 15–30 min |
| Web / `.github/workflows/web-test.yml` | `scripts/ci/web.sh` | npm ci, scoped ESLint, TypeScript, complete Vitest, production build and privacy | ubuntu-latest | 2–4 / 4–7 min |
| Scripts/docs / `.github/workflows/scripts-tests.yml` | `scripts/ci/scripts.sh` | every Python script test, doc/code paths and index, layout/shell/Python compatibility, privacy-rule regression, catalog/translation/token checks, actionlint, issue-link logic, website worker | ubuntu-latest | 2–4 / 4–7 min |
| Runtimes / `.github/workflows/runtimes-test.yml` | `scripts/ci/runtimes.sh` | TypeScript SDK typecheck/test/portability, every runtime integration's npm tests | ubuntu-latest | 2–4 / 4–7 min |
| Container and Release / `.github/workflows/release.yml` | `scripts/ci/hub-image.sh` | amd64 build; isolated Compose Postgres, HTTP/TLS health, pairing, web, admin isolation, file ownership/0600; publication remains amd64+arm64, then a version-tag run creates the GitHub Release | ubuntu-latest | 8–15 / 15–30 min |

These planning estimates predate sharding and are not measured success claims; use job timestamps and each script's PASS/FAIL/TIMEOUT
durations for an actual run. Parallelism reduces wall-clock latency but can increase total runner minutes through repeated setup and
compilation. Cancellation reduces wasted old runs, not already-used minutes. Public-repository standard hosted runners do not consume
private Actions minutes; billing multipliers and plans may change. Measure actual queue and runner time before budgeting.
Go/npm download/build caches are used; caches on dispatched branches are read-only except for the cache-writing paths explicitly restricted to `main`, and no credentials
are stored. The server Go cache restores with `actions/cache/restore` and saves only on a `workflow_dispatch` of `main`
(`scripts/ci/test_shards.py`), so branch runs cannot push main's entries out of the
repository's 10 GB cache. Cache and parallelization work remains
tracked in #1214; cache-based test substitution is not allowed.

Local prerequisites: Go from `server/go.mod`, Node from `web/.nvmrc`, Python 3.9+ (CI uses 3.11), Docker/Compose for server/image,
`pip install -r scripts/ci/requirements.txt` and `go install github.com/rhysd/actionlint/cmd/actionlint@v1.7.12` for scripts, Bun 1.4.2 for
TypeScript SDK.
Local scripts fail on missing required tools rather than claim success. They do not deploy or publish.
A maintainer-approved `gh run rerun --failed` runs the original commit; it cannot acquire diagnostic code added by a later commit.

Deliberate boundaries: web `compatibility.live.test.ts` needs an isolated live hub/token and explicitly skips its one probe without them.
The offline web tests still run completely. Real Agent integrations, release signing and publication need explicit lab inputs or secrets; retain the manual checks below. Platform-gated and explicitly opt-in tests still report their own skips; they are not evidence of device/live integration coverage.
On Linux, `test_persona_cutover.py` has six optional database/container cases
without dedicated cutover inputs. The complete manual Python lane still discovers and runs every suite; no failing suite is excluded.
Manual server CI's Postgres does run ordinary database tests (unlike the old database-free workflow). Specialized offline cutover fixtures requiring extra
container/migration inputs are not supplied. Private-value scanning of commits and PR metadata remains in `scripts/hooks/merge-pr.sh` on
the merge machine; CI runs the generic privacy rules without those values. No baseline is raised to make a gate green.

`release` uses only public dependencies, the GitHub CLI preinstalled on hosted runners, and the existing `GITHUB_TOKEN`; no private registry or machine service is required. Its
repository enablement is a separate setting: enable only after confirming the hosted build/smoke result. Do not dispatch publication just
to test workflow dispatch. Runner migration removes workflow label variables; after green CI, delete unused repository variables and unregister
repository runners if present. Do not uninstall machine services or delete their files; organization-provided runners require their owner.


A test binary never touches the developer's real `~/.persona`: the packages that start Persona processes (`internal/hub`, `internal/assistant`) run through `testhome.Main` in their `TestMain`, with `HOME` set to an empty directory and `PERSONA_HOME` unset, and the run fails if anything created `<HOME>/.persona`. A test that needs state sets `PERSONA_HOME` (`t.Setenv`) to a directory of its own, as the two assistant-process tests in `internal/hub` do. A test started by hand with `go test` used to rewrite the real hub's assistant `manager.json` and `secret.key` (the next run's assistant then never came up); `scripts/test/test-server.sh` runs with a throw-away `HOME` already. Go tools a test starts keep the developer's Go settings and caches: `testhome` pins `GOENV`, `GOPATH`, `GOMODCACHE` and `GOCACHE` to their values under the real home (#5191).

Server packages with database tests call `dbtest.Main` in their `TestMain`: the binary gets a database of its own next to `PERSONA_TEST_DATABASE_URL` and drops it at the end (`docs/lessons/workflow.md`). The databases `dbtest` makes commit asynchronously (`synchronous_commit = off`): a commit is visible to other sessions at once, and only a crash of that Postgres could lose the last ones, so a migration does not wait for a WAL flush per transaction. In `internal/db`, a test that needs a new database with the current schema calls `migratedDatabase(t, sets)`, which copies a template the binary migrated once per set (`dbtest.Template`, `dbtest.Clone`); a test that starts from an older state uses `freshDatabase` and migrates. `open(t)` and `db.TruncateForTest` empty everything their `TRUNCATE ... CASCADE` reaches, but truncate only the tables that have a row (`docs/lessons/testing.md`, "Test database setup"). The CI Postgres and hub databases keep their own settings.

## Affected checks

A PR does not need every lane: `scripts/ci/affected.sh` runs only what its diff against the merge base with
`origin/main` reaches, and `scripts/ci/full.sh` runs every portable Linux lane for `main` after a merge and the
nightly run. Each lane keeps about eight CPUs busy, so full.sh runs one lane per eight CPUs it may use at a time
(`PERSONA_CI_FULL_JOBS` overrides): on eight CPUs, six lanes at once took each two to three times as long and timed
out the tests that measure their own deadlines. `PERSONA_CI_FULL_EXCLUDE` leaves lanes out for a runner that runs them
elsewhere (the gate runs web on a second slot when one is free).

- **Go**: `scripts/ci/affected.py` takes the server and `sdk/go` packages with a changed file (a non-Go file counts for
  the package whose directory holds it), every server package that imports one of them directly or through others, and
  every package whose tests import any of those (`go list` in `server/`, under a second). check-fast's plan adds the
  packages whose tests read a changed fixture, schema, doc or client source, and the repo-wide guard tests. They run
  against a disposable Postgres the way a server shard does (`scripts/ci/server-shards.py affected`, the partitioned
  packages in their partitions), with `go vet` for Linux and Windows.
- **Everything else** is check-fast's own plan for the same diff (`scripts/test/check-fast.sh --base <ref> --no-server-go`):
  the privacy scan, the scripts suites a path selects, the web tests `vitest related` picks, site, sdk/ts, integrations,
  the ratchets.
- **Full lanes instead**: a change to what selects or runs the checks (`affected.py`'s `FULL`: `scripts/ci/`, the
  workflows, `go.mod`/`go.sum`, `build-tags.txt`, the test environment helpers) or one check-fast cannot plan.

```sh
python3 scripts/ci/affected.py plan                     # what this branch would run
python3 scripts/ci/affected.py gates                    # the whole-lane gate groups that cover it
git diff --name-only origin/main... | python3 scripts/ci/affected.py plan --changed-from -
bash scripts/ci/affected.sh                             # run it (Docker for Postgres)
bash scripts/ci/full.sh [server-node web ...]           # every lane, or some, one per eight CPUs at a time
```

`gates` names the existing lanes (`server-build`, `server-managers`, `server-node`, `server-other`, `web`, `scripts`,
`runtimes`) for a runner that takes whole lanes: the server shards holding an affected package, `web` and `runtimes`
when the lane selection picks them, and `scripts` when check-fast plans more than the server's Go checks; `all` for a
full run. The test Postgres runs with `fsync`, `synchronous_commit` and `full_page_writes` off: a disposable database
needs no durability, and on a busy disk every commit waiting for the disk made the database the slowest part of the
server shards.

## Count ratchets

A count ratchet keeps a committed number per file or entry and fails only when a count **rises** (#12785). Before, most of them also
failed when a count fell below its baseline, so a change that took out a sleep had to lower the baseline in the same PR, and one that
forgot broke `main` for everyone (#2761, #5635, #6676, #10045, #11808). From 2026-11-07 to 2026-11-22, 110 of the 185 commits on
`main` that edited the portable baselines below only lowered them, each a chance to conflict with another PR. Main verification's
repair worker may not edit baselines (`docs/operations/main-verification.md`), so such a failure was never repaired automatically.

| Ratchet | Counts | Baseline | Check | Lowered by |
| --- | --- | --- | --- | --- |
| Fixed sleeps | `time.Sleep` calls per Go test file under `server/` and `sdk/go/` | `scripts/ci/sleep_baseline.json` | `scripts/ci/sleep_ratchet.py` (guards, check-fast, merge) | `--update` |
| Web font sizes | hard-coded px font sizes per `web/src` file | `scripts/ci/web_font_baseline.json` | `scripts/ci/web_font_ratchet.py` (guards, merge) | `--update` |
| Prompt language | CJK prompt templates without language handling | `scripts/ci/prompt_language_baseline.json` | `scripts/ci/prompt_language_ratchet.py` (guards) | `--update` |
| Hub hard-coded text | CJK string literals per hub and command-line file | `server/internal/i18n/hardcoded-baseline.json`, `server/internal/i18n/hardcoded-terminal-baseline.json` | `TestHardcodedChineseBaseline`, `TestHardcodedTerminalBaseline` (server lane) | `UPDATE_I18N_BASELINE=1` |
| Web hard-coded text | CJK strings per `web/src` file | `web/src/i18n/hardcoded-baseline.json` | `web/src/i18n/catalog.test.ts` (web lane) | `UPDATE_I18N_BASELINE=1` |
| Prompt sizes | bytes per instruction section, tool description and supervision event | `server/internal/assistant/testdata/prompt_sizes.json` | `TestPromptSizeRatchet` (server lane) | `PROMPT_SIZE_UPDATE=1` |
| Inbox book readers | places per file that reach the Inbox's in-memory book | `inboxBookReaders` in `server/internal/archguard/inbox_slice_test.go` | `TestInboxBookStaysInTheInbox` (server lane) | by hand |

- **Only a rise fails.** A count at or under its baseline passes; under it, the check says so and how to lower the baseline.
  A change that takes sleeps, font sizes or hard-coded text out need not touch the baseline at all, which keeps baseline files out
  of most PRs and out of their conflicts. A file or entry the baseline does not have starts at zero, so a new file cannot bring any.
- **With a base, a changed file is held to the lower of its baseline and its count at the merge base.** The sleep, web font and
  prompt-language ratchets take `--base` (default `CI_BASE_SHA`; a base that does not resolve is an error) and read the merge base
  through `scripts/ci/ratchet_base.py`. A file the change touches may not rise above its count there, even where the baseline still
  has room, so the slack a fall leaves under a baseline cannot be spent by the next change. An entry the merge base's baseline does
  not have yet is the change's own (a file the web font's `--update` let take sizes that moved to it, or a raise the sleep ratchet
  refuses anyway), and only the baseline holds it. check-fast's diff mode runs the sleep ratchet with `--base` (it does not run
  the other two), and hosted CI sets `CI_BASE_SHA`.
- **With no base, only the baseline holds**, so a full-tree run on `main` checks only `count > baseline`. There is no fallback to
  the clone's `origin/main`: it is only as fresh as the last fetch, and main verification pins `HEAD` by SHA without moving it
  (`docs/operations/main-verification.md`), so right after a merge it usually names the parent and would hold the merged files to
  the parent's counts, a limit `merge-pr.sh` does not apply. A hosted run on `main` gets `origin/main` of its own fresh clone, so
  its merge base is `HEAD`. The never-raise checks on the baseline files themselves keep their defaults (the clone's default
  branch for the sleep ratchet, `origin/main` for the prompt-language ratchet). The ratchets inside `go test` and vitest
  know no base and check only the baseline, as does `changed_ratchets.py` at merge time.
- **Merge time does not hold a file to its merge-base count.** `scripts/hooks/merge-pr.sh` (through `changed_ratchets.py`, which
  covers the sleep and web font ratchets) refuses a merge that raises a count above its baseline, but one that raises a count back
  into the room a fall left merges. check-fast's diff mode refuses that for the sleep ratchet; for the web font and prompt-language
  ratchets only a hosted run on the branch or a run with `--base` does. A merge-time refusal is a separate change.
- **Lowering is tidying anyone can run.** `python3 scripts/ci/lower_baselines.py` runs each ratchet's own update (all of the table
  but the Inbox book readers, a Go literal lowered by hand) and keeps the result only if it lowered: no raised number, no new entry, no new file (a removed entry or file is a count gone to zero). A ratchet whose
  update fails, or would raise or add (a count above its baseline, the web font's moved file, a prompt-size move between pieces), is
  put back and reported, and the run exits 1; one whose tools are missing here (`go`, or the web's installed packages) is skipped. `--dry-run`
  writes nothing, `--only sleep,web-font` picks ratchets and `--list` names them. Run it on an up-to-date `main`, weekly or whenever
  you like, and send the result as a PR of its own; nothing depends on it running.
- **Not count ratchets.** The permission allowlists stay strict, because a stale line there is a standing permission:
  `server/internal/hub/testdata/hub_routes_allowed.txt`, `server/internal/hub/testdata/hub_store_methods.txt`, the
  `server/internal/hub/testdata/route_scopes.txt` golden, `TestMovedClustersStayOut`'s names and `inboxBookBridge`. Name lists such as
  `scripts/test/doc_sections_known.txt` and `scripts/test/committed_screenshots_baseline.txt` keep their own rules.

## Bounded server package verification

Since #6286, a plain `go -C server test` uses the node package's own partitions (see "Hosted CI" above).
Diagnostic runs with `-json` or `-v` keep their normal event stream and do not use that automatic split.
The existing runner also supports those diagnostic flags and can verify either reported package set under one
600-second outer deadline:

```sh
cd server
timeout --kill-after=5s 600s python3 ../scripts/test/go_test_partitioned.py go test -count=1 \
  ./internal/assistant ./internal/assistantskills ./internal/assistantwire ./internal/hub ./internal/node
timeout --kill-after=5s 600s python3 ../scripts/test/go_test_partitioned.py go test -count=1 \
  ./internal/apierr ./internal/assistant ./internal/assistantskills ./internal/assistantwire \
  ./internal/hub ./internal/i18n ./internal/node ./internal/prompts
```

Use an isolated test environment, with a disposable database when database coverage is required (the server CI lane
prepares Postgres 18). The deadline includes discovery, compilation and execution. Exit 124 is a timeout; another
nonzero exit is a failure, even when every partition finishes. Report existing opt-in/platform skips separately.
The runner preserves caller flags, including `-skip` and `-race`; a caller's `-run` keeps the command unpartitioned.
Compare like commands: an explicit runner result does not establish that direct `go test` passed. A verifier
needs a maintainer change only to adopt this explicit entrypoint or diagnostic flags; the plain node split is
part of the test binary. Earlier measurements from before #6286 describe the serial plain command.

Shared node fixtures declare their scripted shell ready immediately; shell-readiness tests use `watchShell` to
exercise observation instead. Scripted Claude and Codex hosts poll every 20 ms; timing-sensitive start tests use
`withStartWaits` to keep the production 500 ms cadence. Virtual-time tests pin both cadences and the requirement
for two consecutive ready frames, including reset after a working frame, and exact Codex rollout matching, launcher failure and cancellation. At fixture teardown,
pending first-message checks are cancelled and joined before
the fake socket closes or shared settings are restored. Tests of delivery/recovery still await `firstWait` before
their assertions. The normal node keeps its detached delivery lifetime and all production wait limits.

The Codex app-server fixture registers a reply waiter before sending each request and wakes it on the matching
JSON-RPC response. Its five-second request budget, approval assertions and recorded responses remain intact;
cancellation, transport errors and EOF remove pending waiters without leaving its reader blocked.

## Go build cache across worktrees

Unless `-trimpath` is set, Go's build cache key for a package includes the package's absolute directory. The same
package at the same commit is then compiled and cached once per checkout: a machine with dozens of worktrees keeps a
copy of every package for each, and every new worktree starts cold. With `-trimpath` all checkouts share one entry
(#10614).

The scripts that build and test Go for verification add `-trimpath` to `GOFLAGS`: `scripts/check-fast.sh`,
`scripts/ci/server.sh`, `scripts/test/server_test_env.py` (so `scripts/test/test-server.sh` on the hub machine and the
CI server shards) and the PR watcher's verification environment. Each keeps its base
value and adds `-trimpath` unless that value already sets the flag; `-trimpath=false` opts out. All but the watcher
start from the effective `GOFLAGS` (the environment, else `go env -w`). The watcher starts from the `GOFLAGS` in its
own environment, else its default (`docs/operations/pr-review-automation.md`); either one overrides a
`go env -w GOFLAGS`, so opt out there by setting `GOFLAGS` in the watcher's environment. `scripts/test/go_trimpath.sh`
holds the rule for the shell scripts. Their steps that change one flag do it on the command line (`go vet -ldflags=`,
`go build -mod=readonly`), never by replacing `GOFLAGS`. The server test launcher passes the value on explicitly: the
temporary `HOME` it gives the tests would hide a `go env -w` setting.

Tests find repository files from the working directory (`go test` runs in the package directory), never through
`runtime.Caller`: under `-trimpath` its file names are module paths (#1133).

On a machine with many worktrees, set it for every Go command:

```sh
go env -w GOFLAGS=-trimpath
```

The trade-off: binaries and stack traces then name source files by module path
(`github.com/nwverse/persona/server/...`) instead of the local directory, so a debugger (Delve, an IDE) needs a
source path mapping to open them. Release builds already use `-trimpath` (`docs/operations/release.md`).

## Heavy build slots

Many worktrees share one machine, and several large `go test` or web builds started together slow each other down
until tests time out. `scripts/heavy_slot.py` gives each kind of heavy build a few machine-wide slots, so they queue
instead (#10714):

```sh
python3 scripts/heavy_slot.py run --kind go|web -- <command> [<argument>...]
python3 scripts/heavy_slot.py status    # every kind's slots and who holds them
```

- **Who takes one**: `scripts/check-fast.sh` for each `go test` / `go build` / `go vet` (`go`) and vitest (`web`) step; its
  gofmt step compiles nothing and takes none. check-fast takes the slot outside the step's timeout, so time in the queue
  is not a hung check; a wait past the slot timeout is a TIMEOUT that says the check never ran. While a step waits,
  check-fast prints the helper's waiting line on stderr at once, and its line when the slot comes free (`check-fast: go
  test ./...: waiting for a go slot (3 busy, up to 45m)`); the summary counts the wait apart from the check's own time
  (`PASS     server go test ./... (95s, plus 140s waiting for a go slot)`). With `PERSONA_HEAVY_SLOTS=0`, or without
  `python3`, check-fast runs the command directly, without the helper.
- **A slot** is an exclusive `fcntl.flock` on a lock file in a per-user cache directory
  (`~/Library/Caches/persona/slots` on macOS, else `$XDG_CACHE_HOME/persona/slots` or `~/.cache/persona/slots`;
  `PERSONA_HEAVY_SLOT_DIR` moves it). The helper holds the lock and runs the command as its child. The lock is not
  inherited, so a daemon the build leaves running never keeps a slot, and a helper
  that dies, even by SIGKILL, frees its own.
- **How many**: the core count divided by 4, at least 1 (14 cores: 3). `PERSONA_HEAVY_SLOTS_<KIND>=<n>` sets one kind's count (`PERSONA_HEAVY_SLOTS_GO=1`; `0` is no limit).
- **Waiting**: one line on stderr when it starts to wait and one when it gets a slot. After `PERSONA_HEAVY_SLOT_TIMEOUT`
  seconds (default 2700, 45 minutes) it gives up with one line and exit 124, without running the command.
- **Nested calls never wait**: the command's environment names the slot it holds (`PERSONA_HEAVY_SLOT_HELD`). A call
  of the same kind inside it runs at once; one of another kind takes a free slot or runs without one. Nothing waits
  for a slot while it holds one, so slots cannot deadlock.
- **Signals and exit status**: TERM, HUP, INT, QUIT, USR1 and USR2 sent to the helper reach the command, a signal the
  caller ignores stays ignored, and the helper exits with the command's status (dying by the same signal when it did).
- **Off**: `PERSONA_HEAVY_SLOTS=0` makes the helper exec the command, which then runs exactly as without it: the same
  process, stdio, environment and exit status. On Windows, which has no `fcntl`, it says so in one line and runs the
  command without a slot.

CI and the PR and main watchers never wait for a slot. A runner has one job, where a slot would only make that job's
own steps wait for each other, so a hosted lane (`.github/workflows/`, `scripts/ci/`) or a watcher step that runs a
script taking a slot sets `PERSONA_HEAVY_SLOTS=0` for it. One does today: `scripts/ci/affected_drift.py`, which the
Scripts tests job runs through its test, starts `check-fast.sh --dry-run`. `scripts/test/test_heavy_slot.py` fails for
one that does not; it reads the files git tracks among the CI lanes, `scripts/operations/` (the watchers' scripts,
example configs and services) and both watcher skills under `.claude/skills/`.

## Fast local checks (`scripts/check-fast.sh`, #2849)

Runs only relevant second-scale checks based on git changes, with independent checks in parallel; useful before committing:

```sh
./scripts/check-fast.sh                  # compare changes from git merge-base HEAD <default branch> to worktree, select by path
./scripts/check-fast.sh --base main      # choose a comparison baseline
./scripts/check-fast.sh --all            # all fast local checks except the server Postgres tests
./scripts/check-fast.sh --dry-run        # print the plan (queued checks, SKIP and INFO notes) and run nothing
./scripts/check-fast.sh --dry-run --changed-from <file|->         # plan the listed paths instead of the diff
./scripts/check-fast.sh --dry-run --changed-from <file|-> --each  # plan each listed path on its own
./scripts/check-fast.sh --compare-main   # after a failure, rerun only what failed at the merge base and say whose it is
./scripts/check-fast.sh --compare-tip    # --compare-main, and what the base fails too also at the base ref's tip
```

After the plan, an `INFO` line names the CI lanes the change selects that check-fast runs no part of (for example `runtimes`), read from `python3 scripts/ci/paths.py --lanes-for`, the selection CI and the PR watcher use; run `bash scripts/ci/<lane>.sh` for one of them before pushing. See "What a change affects" below for how the three selections are kept in step.

Without `--base`, the base is the clone's default branch, read from local refs only (it works offline): `origin/HEAD` if it points at a commit, else `origin/main`, else `origin/master`. With none of them it stops with exit 2 and says how to fix it (`git fetch origin` then `git remote set-head origin --auto`, or `--base <ref>`).

`--all` also runs all `scripts/**/test_*.py` like CI’s Scripts tests job (the same `find`; it skips only tests already covered by dedicated checks: `test_external_service_refs.py`, `test_sdk_bundle.py`, `test_extension_schema.py`, see `script_test_add`; update this skip list when renaming or removing dedicated checks). They run sequentially in one check, each suite from its own directory with its own `CHECK_FAST_TIMEOUT_OTHER`. One failure does not stop the rest; the final summary lists failed and timed-out files. A `test_*.py` that never mentions `unittest` is a verification script, not a suite: like CI (`scripts/ci/python-tests.py`), check-fast prints `SKIP <path> (not a unittest suite)` for it instead of running it, in diff mode too (`python -m unittest` runs no tests there and exits 5, "NO TESTS RAN", from Python 3.12); `scripts/test/test_check_fast.py` compares the `--all` selection with CI's.

Path mapping (a new mapping is a rule in check-fast's per-path table, `scripts/ci/check_fast_map.py`; `scripts/test/check-fast.sh` adds only what it finds at run time, such as the Go tests that read a changed fixture, and the frozen-migration and fixed-sleep checks, which also follow a rename):

| Changed path | Checks |
| --- | --- |
| `web/**` | `npm ci` (only if `node_modules` missing or `package.json` / `package-lock.json` changed) + `npm run check:deps` (fails, naming the packages and saying to run `npm ci`, when an installed direct dependency isn't the version `package-lock.json` pins: a lock bump merged from `main` leaves the old packages in place, #4776) + `npm run typecheck` (incremental, state kept in web's `node_modules/.cache/tsc/`: a second run only checks what changed, and a type error fails every run) + the tests `python3 scripts/ci/paths.py --web-plan` picks (#10619, see "Web tests a change reaches" below): `npx vitest related --run` from the changed files and every test that reads files from disk; `npm test`, the whole suite, instead when a change reaches every test: a file in `web/` outside `web/src`, `web/src/lib/store.ts`, `web/src/lib/api.ts`, `web/src/ui/atoms.tsx`, `web/src/i18n/**`, a `web/src` module the Vite configs import, or anything the vitest setup files reach |
| Files outside `web/` that `web/src` and its tests read (`spec/fixtures/<case>/…`, `schema/extension/…`, `scripts/mock/…`; `python3 scripts/ci/paths.py --web-inputs` lists them) | In addition to other checks in this table, the `web/**` checks, with the related tests (the whole suite for a file the vitest setup files read). The list is derived, never written down: the same scan gives CI's web lane its inputs outside `web/` |
| `integrations/<pkg>/**` | Package `node --test test/*.test.{js,mjs}` (skip packages without tests) |
| `site/**` | `node --test site/worker.test.mjs` |
| `sdk/ts/**` | `bun test` (skip with reason if bun absent) |
| `sdk/ts/src/**` | In addition to the `sdk/ts/**` check, `go test ./internal/extension/denohost/...`: the trusted Deno adapter `server/internal/extension/denohost/adapter.mjs` bundles these sources, and `TestTrustedAdapterMatchesOfficialSDK` fails while the committed one is stale (#4221; regenerate it with `scripts/gen/deno_adapter.sh` and the Bun version `.github/workflows/server-test.yml` pins). Skipped with a reason when bun is absent; `pre-push` is not (see "Trusted Deno adapter" below). CI's server lane runs on these paths too (`scripts/ci/paths.py`) |
| `sdk/go/**` | `go test ./...` in sdk/go module |
| `server/internal/foo/**`, `server/cmd/bar/**`, `server/plugins/baz/**` | Affected packages, e.g. `go test ./internal/foo/...`, through `scripts/test/go_test_partitioned.py` (#12765): `./internal/assistant/...` runs `internal/assistant` in its partitions beside its subpackages, inside the same 300 s, since its serial run alone takes longer; `internal/node/...` and every other group run exactly as given (`internal/node`'s `TestMain` splits itself). Unmappable server changes fall back to `go build ./...` |
| `server/**`, `sdk/go/**` | In addition to other checks in this table, the server's repo-wide guards (`job_go_guards` in `scripts/test/check-fast.sh`): `go test` on `internal/lazyre`, `internal/archguard`, `internal/tzguard`, `plugins`, `internal/i18n` and `internal/prompts`, and named tests of `internal/hub`, `internal/assistant`, `internal/update`, `cmd/persona-hub`, `cmd/persona-manager` and `cmd/persona-node`: the hub's and the assistant's architecture guards, `TestOneBuild`, each program's link guard, and the cross-package contract tests (the prompt and MCP tool goldens `TestGolden.*`, `TestNoWholeListMCPToolGoldens`, `TestWireSamples`, `TestRouteScopes`, `TestEveryExtensionRouteIsBounded`, the prompt and instruction size budgets, and the model purpose, repository guidance and start path contracts). They read other packages' source, `go list` output, the migrations or `sdk/go`, or pin what other packages render, so they fail on code outside their own package (#1936); a change otherwise tests only its own group's subtree, and the Go breakages that reached `main` past a correct check-fast run were, besides gofmt and Windows vet (the next two rows), failures of tests like these outside the touched group (#12842, #10615). All run with `-count=1`. The list is curated: not every `*guard*_test.go` is repo-wide. `scripts/test/test_check_fast_guards.py` fails when an entry of `GO_GUARD_TESTS` names no `func Test…` of the listed packages, since `go test -run` silently matches nothing after a rename. `--all` runs them within `go test ./...` |
| `server/**`, `sdk/go/**` (vet) | Also `go vet` in a job of its own (`job_go_vet`, #12894): `go vet -ldflags= ./...` over the whole server module, then `GOOS=windows GOARCH=amd64 go vet -ldflags=` on each changed package group (`./internal/foo/...`), or on `./...` for a change outside every group (`server/go.mod`, a file directly in `server/plugins/`, `sdk/go/**`, which the server module imports). CI's server lane vets the module for both (`scripts/ci/server.sh`: "Server vet", "Windows node build and vet"), and Windows vet failures were among the breakages check-fast missed (#10615). The Windows step runs also when the first one failed, and its rerun line keeps its environment (`env GOOS=windows GOARCH=amd64 go vet …`). A group with no package that builds for Windows (every file of `server/internal/fleetsoak` is `//go:build unix`, `server/cmd/password-wasm` is `js && wasm`), or whose directory is gone, is left out of the Windows step: given only such groups `go vet` says `no packages to vet` and exits 1, where CI's `./...` passes over them. It is left out only when `GOOS=windows go list` says its pattern matched no packages; any other answer, an error included, keeps it in. The PASS line names it (`PASS     server go vet (52s; windows vet left out ./internal/fleetsoak/...: no package there builds for windows)`), and with no group left the Windows step does not run. Being a separate job, a vet that runs past its timeout on a cold build cache is a `TIMEOUT` of this job alone: in the guard job it would have made that job a `TIMEOUT` and hidden a contract test's `FAIL`. `--all` vets the whole module on both platforms |
| `server/**/*.go`, `sdk/go/**/*.go` | `python3 scripts/ci/gofmt.py` on the changed `.go` files that still exist (`job_go_fmt`, #12894): the trees CI's Go formatting step covers, `testdata/` included; other `.go` files (`web/e2e/`, `sdk/templates/go/`) are not checked, as in CI. It takes no go slot, since gofmt compiles nothing, so it reports in seconds while the go slots are busy; without `gofmt` on `PATH` it is a `SKIP` line. `--all` checks both trees whole, as CI does |
| `server/internal/db/migrations/**`, `scripts/ci/migration_freeze.py` | `python3 scripts/ci/migration_freeze.py`: a migration already on the base cannot be edited, deleted or renamed (comment-only and whitespace-only edits pass; a new file may be added, named `YYYYMMDDHHMMSS_<name>.sql` by the UTC second it is written, from 2026-11-18 to a day ahead; a numbered file outside the frozen ones up to `0561` fails). A rename out of the directory counts. `--all` runs it when the script is present |
| `server/**/*_test.go`, `sdk/go/**/*_test.go`, `scripts/ci/sleep_ratchet.py`, `scripts/ci/sleep_baseline.json` | `python3 scripts/ci/sleep_ratchet.py`: the number of `time.Sleep` calls in each Go test file may stay or fall, never rise (comments, string literals and a `// sleep-ok: <reason>` line do not count; a new file starts at zero). `--update` only lowers the baseline. A rename out of those trees counts. `--all` runs it when the script is present |
| `spec/fixtures/**`, `schema/<dir>/**` (each schema family: `extension`, `wire`, `configuration`), `integrations/**`, `docs/<file>`, `scripts/test/server_test_env.py` | In addition to other checks in this table, `go test ./internal/<package>/...` for server packages reading these files (repository inputs covered by `server-test.yml` filters). At runtime, `git grep` finds packages referencing the path in `server/**/*_test.go` (strings or `filepath.Join` components; `//` comment lines excluded), so new readers need no script changes; other `spec/` files remain unmapped |
| `spec/**`, `schema/**` read by a runtime package | In addition to other checks in this table, the tests of each `integrations/<pkg>` and of `sdk/ts` whose files name the changed path or a directory it is in, outside a comment line (`integrations/runtime-sdk` reads `spec/fixtures/input-clip/cases.json`, `sdk/ts` reads `schema/extension/examples/`). Found at run time with `git grep`; CI's runtimes lane runs for all of `spec/` and `schema/` |
| `.github/**` | `scripts/test/pr_needs_issue_test.mjs` |
| `.github/workflows/*.yml`, `scripts/ci/*.sh`, `scripts/ci/*.py`, `scripts/operations/*.py`, `scripts/operations/*watch*.json`, `scripts/operations/*watch*.service`, `.claude/skills/pr-review-merge-loop/**`, `.claude/skills/main-verify-fix-loop/**` (not `test_*` files) | Also `scripts/test/test_heavy_slot.py`: a CI lane or a watcher step that names a script taking a heavy slot must set `PERSONA_HEAVY_SLOTS=0` ("Heavy build slots" above). A new lane script once broke it with nothing local running it (#11026) |
| `schema/**` | `scripts/test/test_extension_schema.py`, `scripts/test/test_sdk_bundle.py`, plus `go test ./...` in sdk/go (the same three checks as CI's Extension schema job) |
| `schema/wire/**`, `spec/fixtures/**`, `scripts/gen/extension_schema.py`, `scripts/test/test_extension_schema.py` | Also `scripts/test/test_wire_schema.py`: every sample the shared fixtures keep for a wire schema holds to it at any depth, and the generated wire types are current (it imports the extension test's validator). CI's guards step always runs it |
| `spec/fixtures/todo-detail/**`, `spec/fixtures/todo-layout/**`, `spec/fixtures/todo-view/README.md`, `spec/fixtures/agent-names/**`, `schema/wire/todo-view.schema.json`, `server/internal/assistant/todos.go`, `server/internal/assistant/todo_ask.go`, `scripts/test/test_wire_schema.py`, `scripts/test/test_extension_schema.py` | Also `scripts/test/test_todo_fixtures.py`: the to-do page and window fixtures hold to their READMEs' rules, the view schema, the todo-view line keys, the agent names and the fields the hub writes on a listed to-do (it imports the wire and extension schema tests) |
| `scripts/test/test_external_service_refs.py`, `scripts/{gen,test}/sdk_bundle*` | The scripts' own tests |
| `scripts/**` | Select second-scale privacy / PR / schema / refs / bundle / i18n checks by path; when uncertain run PR and schema. Also run changed `scripts/<subdirectory>/test_*.py`, and `test_<name>.py` for changed `<name>.py` / `<name>.sh` scripts (`-`→`_`; `.sh` also checks `test_<name>_sh.py`), from the test directory like CI (#3968). `scripts/hooks/**` also runs `scripts/test/test_privacy_check.py` and `scripts/test/test_check_deno_adapter_sh.py` (they run the hooks in their temp repositories; the latter also for `scripts/gen/deno_adapter.sh`); `scripts/privacy_check.py` also runs `scripts/release/test_check_bundle_privacy.py` (it imports `privacy_check`) and `scripts/test/test_merge_pr_sh.py`; `scripts/operations/pr_watch_merge.py`, `scripts/ci/changed_ratchets.py`, `scripts/ci/sleep_ratchet.py`, `scripts/ci/web_font_ratchet.py` and `scripts/ci/ratchet_base.py` also run `scripts/test/test_merge_pr_sh.py` (it copies the scripts into its temp repositories, where `merge-pr.sh` runs them, and checks the watcher's merge lock), and the two ratchets and `ratchet_base.py` also `scripts/ci/test_changed_ratchets.py` (it imports the ratchets, which import `ratchet_base.py`); `ratchet_base.py` also runs the sleep, web font and prompt-language ratchets' own suites, which load it through their ratchets or copy it, and `scripts/ci/prompt_language_ratchet.py` runs `scripts/test/test_prompt_language_ratchet.py`, which loads it by path; `scripts/test/go_trimpath.sh` also runs `scripts/ci/test_ci.py` and `scripts/test/test_check_fast_inputs.py` (they run `scripts/ci/server.sh` and `scripts/test/check-fast.sh`, whose `GOFLAGS` it composes); `scripts/heavy_slot.py` also runs `scripts/test/test_check_fast_inputs.py` (it runs `check-fast.sh` with it); `check-fast.sh` and `scripts/test/test_check_fast.py` also run `scripts/test/test_check_fast_guards.py` and `scripts/test/test_check_fast_inputs.py` (check-fast's tests are three files, split so each stays well under the per-suite limit; the other two import `test_check_fast.py`'s fixtures), and `scripts/ci/check_fast_compare.py`, `scripts/test/go_test_partitioned.py` and `scripts/test/go_partitions.py` all three (they copy them into their temp repositories, where every Go group runs through the partitioned runner); `scripts/ci/paths.py` also runs `scripts/ci/test_ci.py`, `scripts/test/test_check_fast_inputs.py` and `scripts/ci/test_affected_drift.py`, `scripts/test/check-fast.sh` and `scripts/operations/pr_watch_verify.py` also `scripts/ci/test_affected_drift.py`, and each `scripts/operations/pr_watch_*.py` also `scripts/operations/test_pr_watch.py`. A changed `scripts/**/<name>.py` also runs every `scripts/**/test_*.py` that imports it (`import <name>` or `from <name> import` at the start of a line) from its own directory or from a directory it names, found at run time with `git grep`; a suite a dedicated check runs (refs, bundle, extension schema) schedules that check instead |
| `docs/**`, any `*.md` | `scripts/test/test_doc_paths.py`, `scripts/test/test_doc_go_paths.py` (quoted paths and CLAUDE.md documentation index; test_doc_paths.py also fails a table that a blank line splits), `scripts/test/test_doc_sections.py` (section names quoted after a doc path match a heading, a **bold** label, an `<a id>` or a list item's opening phrase of that doc; `scripts/test/doc_sections_known.txt` lists the ones left for now and only shrinks), `scripts/test/test_doc_wire.py` (protocol.md and extensions.md list every WebSocket type, feature flag, extension point and manifest field the code has) |
| `server/internal/{proto,hub,extension}/**`, `schema/extension/manifest.schema.json` | Also `scripts/test/test_doc_wire.py`: these hold the code side of the lists it compares with the docs |
| `AGENTS.md`, `CLAUDE.md` | Also `scripts/test/test_agents_md.py`: git records `AGENTS.md` as a symlink (mode 120000) whose target is exactly `CLAUDE.md`, and `CLAUDE.md` as a regular file; it reads the index, so a checkout without symlinks passes too |
| `server/**`, `web/src/**`, `web/e2e/**`, `scripts/**`, `sdk/**`, `integrations/**`, `spec/**`, `schema/**` | Also `scripts/test/test_doc_sections.py`: most of the section names it checks are in code comments there |
| `deploy/docker/**`, `docs/operations/docker.md`, root `Dockerfile` | `scripts/test/test_docker_entrypoint.py` (also checks root `Dockerfile`’s `EXPOSE`) |
| Other paths | No code checks; list unmapped paths individually |

A search the plan depends on (the `git grep` runs above) that fails, rather than finding nothing, stops check-fast with exit 2 before anything runs: the checks it would have found would otherwise be left out without a word (#11250). So does a diff without `python3` or the table, or a table that fails or names a check the script does not know (#11535).

Any changes always run `scripts/privacy_check.py` first: local-only, no network, a few seconds. Every check has a timeout (npm ci 600 s, vitest 300 s, one go test group or go vet step 300 s, others 120 s; override with `CHECK_FAST_TIMEOUT_NPM_CI` / `CHECK_FAST_TIMEOUT_WEB_TEST` / `CHECK_FAST_TIMEOUT_GO_TEST` / `CHECK_FAST_TIMEOUT_OTHER`). The go and vitest steps first wait for a heavy build slot, outside their timeout ("Heavy build slots" above); gofmt takes none. Success/failure/timeout each prints a result and duration; the final summary lists passed/skipped (with reasons)/failed/timed-out checks and log directory. Exit: 0 if all pass or none needed; 1 for failure; 124 for timeout without failures (timeouts aren't passes; run full checks to confirm). Ctrl-C, SIGTERM and SIGHUP, sent to the script or to its whole process group (a closed terminal or tmux pane), stop every running check (and its child processes) before exiting with 130, 143 or 129, and keep the log directory. Missing toolchains skip relevant checks with reasons by default; under `--all`, missing core tools (node ≥ 22, go, python3) fail; bun is always optional. Requires GNU `timeout` (coreutils on macOS).

**Rerun lines and `--compare-main`** (#12733). Each failed or timed-out check in the summary ends with a `rerun:` line per failure, the command that reruns just that: the failing Go tests of each package (`(cd server && go test -count=1 -run '^(TestA|TestB)$' ./internal/assistant)`), the failing unittest ids (`(cd scripts/test && python3 -B -m unittest -q test_x.T.test_y)`), or the step's own command for anything else, such as the privacy scan or a ratchet. A timed-out check reruns whole.

With `--compare-main`, a run that failed then reruns only those failures at the merge base, in a detached worktree under its log directory (`<log dir>/compare/base`, removed afterwards by that path, also when the run is interrupted), and prints one line per failing test, suite or command:

- `ALREADY`: also fails at the merge base. Main has it, so it is not this change's (on any change under `internal/assistant` while #12596 was red, for example). Like the watcher's, the comparison is by name: the same test fails there, not necessarily the same way.
- `NEW`: passes at the merge base and fails again when rerun here on its own, so likely caused by this change.
- `FLAKY`: passes at the merge base and when rerun here on its own: it failed only in the full run (under load, a timing test, or after another test), so the change is not shown to cause it. A failure that passes at the base is always rerun here once before it is called `NEW`.
- `EDITED`: not attributable, because the change edits that test's files: a `_test.go` or `testdata/` file of its package, the suite or a test module it imports, or a file the command names. The base runs the old test, which says nothing about the new one.
- `UNKNOWN`: not compared, and why: no named test failed (a build failure), a web step needs its own `npm ci` at the base, the rerun timed out, the base has no such test, or the base has none of the packages a go command checks (packages the change adds).

`--compare-tip` also reruns the `ALREADY` ones at the base ref's tip: `TIP-PASS` means main fixed it after the merge base (merge it), `TIP-FAIL` that it still fails, `TIP-?` that the tip gave no answer (a timeout, no such test there). The rules are the PR watcher's (only named tests are compared; no attribution when the change edits the test), kept in `scripts/ci/check_fast_compare.py`, which does not import the watcher. A guard command is compared by its exit code, and its line names the output lines the base's run does not have, since a change can add to a failure main already has. A go command's package patterns whose directory the base does not have (`./internal/<new>/...`) are left out of its rerun there, since go fails on such a pattern before it checks anything: the Windows vet of a new package and an existing group compares the existing group alone. Go tests rerun with `-count=1` in a go slot, each rerun under its check's timeout. Nothing is decided from hosted CI, whose main runs are hours old. The exit code stays the run's: a failure main already has is still a failure, and the verdict is what to say about it in the PR. `--all --compare-main` finds the merge base and the changed files as diff mode does.

**Web tests a change reaches** (#10619). In diff mode the web check runs `npx vitest related --run` from the changed files, so a change to one view runs the tests whose imports reach it (about 56 for `web/src/views/chat/ChatView.tsx`) instead of all ~1,360 test files. `python3 scripts/ci/paths.py --web-plan` makes the plan from one scan of `web/src` and every web test, done at run time, nothing listed by hand:

- Every related run also includes each test that reads files from disk (`fs`, `node:fs`, `fs/promises`, `node:fs/promises`, `readFileSync`, `import.meta.glob`, a `?raw` or other query import; about 85): vitest related follows only imports whose id is a file on disk, so it never sees what they read. So does each test that builds or serves through a Vite config (it imports `vite` or `vitest/node`): `web/scripts/public-share-build.test.mjs` builds the public viewer, whose config reads `web/src/styles.css` with `readFileSync`. A `web/src` module that reads through `fs` brings the tests that import it, and a module that reads through Vite joins when a file it names changes (`web/src/lib/html-runtime/host.ts` for its `frame.js`).
- The whole suite (`npm test`) runs instead for a change to `web/src/lib/store.ts`, `web/src/lib/api.ts`, `web/src/ui/atoms.tsx` or `web/src/i18n/` (most tests import them: about 825, 1,040, 930 and 1,190), to a `web/src` module the Vite configs import, to anything the vitest setup files reach, or to anything in `web/` outside `web/src` (the Vite and TypeScript configs, `package.json`, `package-lock.json`, `scripts/`, `e2e/`), and always with `--all`. The setup files are the `setupFiles` and `globalSetup` the Vite configs name; what they reach is each of them, what it reads, and each module it imports, followed to the end (`web/src/i18n/test-setup.ts` loads `web/src/admin/catalog.ts`). Every test runs them first, but vitest related follows only a test's own imports. When the scan fails, a `web/` change runs the whole suite.
- Files outside `web/`: every path a scanned file names relative to itself that leads out of `web/` (`"../../../spec/fixtures/forward/cases.json"`, `` new URL(`../../../schema/extension/examples/View.v8-${name}.json`, import.meta.url) ``, `join(__dirname, "..", "..", "..", "spec", …)`) is an input. A file counts as itself; a directory, a glob or a path with a `${…}` part as everything under its literal part; a JSON Schema also brings the files its `$ref`s name. A path built from the working directory or from a variable root is not seen. `python3 scripts/ci/paths.py --web-inputs` prints the list; CI's web lane takes the same one, so the two agree by construction. `scripts/test/test_check_fast_inputs.py` holds the scan to a sample tree in each form and to the real reads.

Not covered:

- Full server tests against real Postgres still use `scripts/test/test-server.sh` (`internal/db`, etc. skip without `PERSONA_TEST_DATABASE_URL`; full `internal/node`, `internal/hub`, etc. assume a hub-machine environment. Fast-check timeouts aren't failures; test-server.sh is authoritative).

Transcript parsing uses real-sample golden tests (`server/internal/transcript/testdata/`, `fixtures_test.go`): each sample's parse result,
"incremental read while writing = one full read", and "backward paging has no gaps/duplicates". After parsing changes run `go test ./internal/transcript -run Fixture -update` and review the golden diff.
Before adding samples, redact with `scripts/diag/redact_transcript.py`, then manually review (`testdata/README.md`). Use
`scripts/diag/transcript_stats.py` to see observed record kinds and a local sample path for each. To compare two scans,
save the baseline before changing the runtime, then scan again after the change with the same roots:

```sh
python3 scripts/diag/transcript_stats.py --json > before-upgrade.json
python3 scripts/diag/transcript_stats.py --compare before-upgrade.json
```

The versioned JSON snapshot contains file and line totals and counts by record kind and runtime version. It omits
sample paths and message bodies. The comparison marks kinds and versions as added (`+`), missing (`-`) or changed
(`~`). An added kind is newly observed in the selected transcripts. Check its local sample before deciding whether
the parser needs a new fixture or handling. Pass transcript directories to either command to limit the scan.

To compare scans saved on different machines or review them later, create both snapshots with `--json`. Compare the
saved counts without copying the transcripts or passing transcript directories:

```sh
python3 scripts/diag/transcript_stats.py --compare before-upgrade.json --against after-upgrade.json
```

The offline command validates both snapshots, leaves them unchanged and reads no transcript directories.

API smoke tests: `curl -H "Authorization: Bearer $TOKEN" …/api/v1/machines`, `…/api/v1/sessions?machine_id=<machine>`.

### What a change affects: CI lanes, check-fast and the watcher (#11026)

Three selections decide what a change affects: `scripts/ci/paths.py` picks CI's lanes, `scripts/test/check-fast.sh` picks the local fast checks, and the PR watcher runs the lanes `paths.py` picks (`watcher_lanes` in `scripts/operations/pr_watch_verify.py`: CI's wildcard scripts lane only for a change under `scripts/` or `.github/`, a test-only test/fix PR's changed tests instead of its web, scripts and server lanes, no image lane). check-fast keeps its own table (`scripts/ci/check_fast_map.py`), because it picks single packages and suites where CI picks whole lanes, and asks `paths.py` for its web plan and for the lanes it names but does not run. The table is a module of its own because importing `paths.py` scans `web/`.

`python3 scripts/ci/affected_drift.py` compares the three for each tracked file (`--table <file>` writes the per-path table; paths as arguments compare only those). It checks two things:

- **Lanes.** A check-fast check counts for a lane when it runs a fast part of that lane's work: the web check for web, Go tests and builds for server, the integration and `sdk/ts` tests for runtimes. Every difference must be listed in its `ALLOWED` table with the reason: check-fast never runs cargo or the image build; CI runs the server and runtimes lanes for whole directories of client contract sources, fixtures, schemas and protocol docs where check-fast runs only the tests that read the changed file; check-fast's doc search also runs Go tests that only mention a doc; a lane's own runner and workflow. A CI lane check-fast runs no part of must also be named in its plan (the `INFO` line, or the apple `SKIP` line).
- **Readers.** Where CI runs a whole lane and check-fast runs only what reads the changed file, the readers are worked out in Python from the tests themselves (`Readers`), not from check-fast's rules, and each must be in check-fast's plan: the Go packages whose `_test.go` files name a file of the server lane's inputs outside `server/`  as a string, as `filepath.Join` elements or as a directory it is in; the `integrations/<pkg>` and `sdk/ts` packages that name a fixture or schema; the `scripts/**/test_*.py` suites that import a scripts module from their own directory or from one they name. CI must select the lane of each Go reader too. These have no `ALLOWED` rows, apart from the two client files `internal/hub`'s wire samples name as consumers.

`scripts/ci/test_affected_drift.py` runs the comparison on a derived probe set (two files per CI lane pattern, the first file of each directory and of each one below it, every path check-fast names, every path that has a reader) and fails on an unlisted difference. It also takes the line each fix of #11026 added out of a real plan and checks that the comparison then fails: the Go tests reading client sources, a schema family's Go reader, a runtime package reading a fixture, a suite importing a watcher module, and the `INFO` line.

What it does not see: a suite that runs or reads a script by its path instead of importing it (the suite rules of check-fast's table cover those); a Go test that builds a path from variables; and a file outside the server lane's inputs and `apple/`, `android/` that a Go test reads, such as `web/src/i18n/locales/` (internal/kb), which neither CI's server lane nor check-fast runs today.

## End-to-end (verified 2026-10-24)

| Scenario | Method | Result |
| --- | --- | --- |
| Create session from phone | `POST /api/v1/sessions`, launch via a launcher in `~/persona-test` on the hub machine | Directory trust accepted automatically; first message delivered |
| Questions | First message asks claude to call AskUserQuestion (one question / two, including multi-select) | question interaction appears on hub; after `resolve`, claude receives answers and continues |
| Plan approval | ExitPlanMode → deny + feedback → revise plan → allow + setMode | Both branches correct (`docs/integrations/claude-integration.md`) |

## UI checklist (inspect screenshots after every UI change)

- Chat list: light / dark; pending interactions, no sessions, hub unreachable.
- Chat: long Markdown (tables, code blocks), collapsed tool groups, diffs, to-dos, images; Working bar; question / plan / permission / terminal panels.
- New chat: machine cards (offline grayed out), recent directories, directory browser, launcher search.
- Machines, mail (empty/list/detail/compose/add account), Settings.

## Mock hub data (screenshots, README / promotional images)

Screenshots must contain no real business data. `scripts/mock/mock_hub.py` returns only made-up data (three machines, Agents in several projects, assistant chat,
one question, one plan approval, Lark + mail Inbox), enough for main pages.

Real-hub Inbox end-to-end tests (triage, event synthesis, AI twin decisions) use `scripts/mock/mock_model.py`: a fake Anthropic Messages endpoint (`POST /v1/messages`),
returning fixed JSON according to prompts (event synthesis merges the batch into the first open item in the input, or creates one; question marks count as Needs you). `POST /_mode` switches to failure, exhausted quota,
or invalid output; `GET /_calls` inspects received calls. Point the manager's `manager.json` `inbox.triage_ai` (also used by event synthesis) and `judge_ai` at it; usage is at the file top, tests in `scripts/mock/test_mock_model.py`.

```sh
python3 scripts/mock/mock_hub.py &                                  # 127.0.0.1:18760
```

- Held-push screenshots (#7782): `MOCK_LN`, `/mock/ln`, and `/api/v1/push/ln-mode` are compatibility identifiers for push grading. Start the mock with `MOCK_LN=1` so `GET /api/v1/push/ln-mode` returns enabled and `GET /api/v1/push/intercepts` returns synthetic rows: two P1 and one P2 of the local day (the first P1 links the /orders investigation to-do through `todo_id`, a source the clients follow; the others have no source) and one P1 of yesterday that clients leave out. Today's rows are hours before the request, or just after midnight when that was another day (wall-clock time, so a DST transition keeps them today), so a mock left running still has them. `GET /mock/ln?on=0|1` switches push grading while the mock runs and `/mock/reset` puts back how it started; the rows are there with push grading off too, as on the hub, and clients don't read them then. The switch opens the held-push tray once and is removed from `UserDefaults`; no real push content is used.
- Main-chat model choice screenshots (#6542, #7857): start the mock with `MOCK_MODEL_PICK=1` so `features` carries `assistant_model_pick` and the main conversation's launchers (`?purpose=assistant`) list Claude Code (Opus on the Main Chat default, with Fable, Sonnet 5.5 and Haiku 5.5 among its models), a launcher that fixes its own model, a direct API and Codex, plus unavailable entries; `PUT`/`DELETE /api/v1/assistant/conversations/<id>/model` keep or drop the conversation's own pick; `GET /mock/model_pick` lists every PUT and DELETE since the mock started, and `?reset=1` also drops every conversation's pick (the list is kept). Without `MOCK_MODEL_PICK` (or `PERSONA_MOCK_HUB`) it skips rather than fails.
- Main-chat Codex Speed (#10244): with the same `MOCK_MODEL_PICK=1`, `GET /mock/model_pick?reset=1&speed=idle|working|no-tier|ack|pending|refusal|timeout` makes the main conversation a Codex chat with its own model pick. Every case except `no-tier` advertises Standard and Fast and starts acknowledged at Standard. `ack` answers a speed POST without changing the session until `GET /mock/model_pick?speed_ack=1`; `pending` holds the POST itself for up to 15 seconds so the menu can show why its actions are disabled, and the same acknowledgment releases it. `refusal` returns 409 and `timeout` an immediate 504 with `code: "timeout"`, neither changing the acknowledged tier. `speed_requests` in the same response records each fixture `/modes` call as `{method, path, body}`, including an effort change; a reset without `speed` restores the ordinary Claude model-picker fixture and clears that log. Summary rows omit both speed fields as the hub's do, exercising detail preservation.
- A background task that reports back after a newer answer (#9140): `MOCK_WAKE_AFTER_ANSWER=1` makes Persona's main conversation one where three background checks were started, another question was asked and answered in full, and each check's notification then woke the assistant (three `wake` notices with no message of the user's). The answer to the second question stays on screen and each woken turn follows under its notice, with its own work line.
- `MOCK_TOPBAR=1|3|long`: sub-Agents of the weather-widget session for the chat header's status line: `1` one running and one ended; `3` three running (`tb3` newest); `long` a sub-Agent title of about 40 characters and a very long tab name (truncation).
- An agent's edits as diffs: `MOCK_EDIT_DIFF=1` adds MacBook's "Rain hours and the dry threshold" (`macbook/a1f3c2d4-0000-4000-8000-0000000c2915`), one turn with an Edit that changes a word in two lines of a block, a MultiEdit of two pieces, a Write and an Edit of a 56-line switch whose unchanged middle folds (Show All 58 Lines), so the BeautifulUI style's chips read "Edit 4 files +20 −6" over four changed files.
- Tool names in words: `MOCK_TOOL_NAMES=1` adds MacBook's "Why the CSV export times out" (`macbook/a1f3c2d4-0000-4000-8000-0000000c2986`), one turn that calls ToolSearch, TaskCreate, TaskUpdate (started, then completed), NotebookEdit, Monitor, SendMessage, ScheduleWakeup, another server's MCP tool (`mcp__github__create_issue`, "GitHub · Create issue") and a tool no table knows with nested arguments (`RenderChart`). The MCP call and the unknown one also have a chat of their own (`…0c2987`, `…0c2988`): open the work and tap the row to see its arguments as a key/value list with Raw Input under it. `…0c2989` is a review whose ReportFindings lists two findings, each with its file and line. `…0c298a` is the Monitor alone, its Background badge on the first screen at the largest text size.
- A reply's Markdown as written: `MOCK_REPLY_MARKDOWN=1` adds MacBook's "导出耗时整理" (`macbook/a1f3c2d4-0000-4000-8000-0000000c3863`) and puts the same turn at the end of Persona's conversation: a table whose delimiter row aligns its columns (the seconds right-aligned so the decimals line up, the result centered), headings at levels 4 to 6, and bold labels next to full-width punctuation (`**注意：**后文`, `**「必须」**`), which render bold with no asterisks left over.
- Paths with non-ASCII names in a reply: `MOCK_CJK_PATHS=1` adds MacBook's "报表整理" (`macbook/a1f3c2d4-0000-4000-8000-000000008386`) and puts the same turn at the end of Persona's conversation: bare paths to files with Chinese and Japanese names, which are there (so they link), one glued to the sentence after its extension and one before a full-width full stop.
- Tool outcomes: `MOCK_TOOL_OUTCOMES=1` adds MacBook's "Why the last export batch goes missing" (`macbook/a1f3c2d4-0000-4000-8000-0000000c4303`), one turn whose tool results carry the node's `outcome`: a test run that failed with exit code 1 and an output the page cut (its last lines in `tail`), an edit the user denied, a write refused before it ran, a command the user interrupted and one that timed out and went on in the background. Only the failed run is red; the others say in gray what stopped them.
- Full access by a restart (#6476, #6513): `MOCK_BYPASS_PENDING=1` gives the widget agent (`macbook/a1f3c2d4-0000-4000-8000-000000000001`, working in Auto-Edit) `bypass_pending`, a restart into Full access that waits for its turn to end, so the clients' pending state can be screenshotted without picking it. Any other mock agent in a pane takes a Full access pick by a restart (`MOCK_BYPASS_RESTART_SECONDS`).
- A file tool's change on its permission card: `MOCK_PERMISSION_DIFF=1` adds two MacBook chats that wait on a Claude Code permission card, as a hub sends it: "Keep the last export batch" (`macbook/a1f3c2d4-0000-4000-8000-0000000c4621`), an Edit whose diff is longer than the card shows (Show All 15 Lines), and "Test the last export batch" (`…c4622`), a Write of a short test file.
- Notification settings that can't load (#1860): `MOCK_PREFS_FAIL=1` makes `GET /api/v1/devices/<id>/prefs` answer 503; `GET /mock/prefs_fail?on=0` lets the next read through (Retry, or switching back to the app).
- herdr stopped (#1916): `MOCK_HERDR_STOPPED=macbook` stops classic herdr for mock MacBook; Agents shows "MacBook's herdr isn't running" and "Start" at the top.
  `MOCK_HERDR_START=slow|fail|stale`: startup takes 6 seconds / fails (node's reason) / starts but `GET` still reports stopped (old hub). `GET /mock/herdr?stopped=…&start=…` changes it live,
  `/mock/reset` restores environment settings; `starts` counts requests per machine.
- Personal agents' Remove from list (#138): `MOCK_PERSONAL_AGENTS=1` lists six made-up personal agent conversations and keeps hide / unhide / `GET /agents/hidden` in memory (each write sends `agents`); `GET /mock/personal-agents?reset=1` brings every one back, and `?state=partial` (#3216) keeps them listed while a second source fails (`?state=partial_setup`: isn't set up), for the notice above the list. Preferences are kept in memory too; `?prefs_fail=500` refuses their PUT (#1880), and `?reset=1` clears both the saved fixture and the failure. The page is reached from the shell's **Personal Agents ›** pill.
- Computer control (docs/extensions/computer-use.md): `MOCK_COMPUTER=ready|permissions|both|locked|missing` enables extension `cua` and mock MacBook (ready / missing Screen Recording / missing both permissions / locked / not installed: "Install Cua Driver" advances through download, verification, disabling analytics, authorization and Allow, every `MOCK_COMPUTER_STEP` seconds, default 2). Add `MOCK_WEB=web/dist` (first `npm run build`) to host the web VNC page and forward MacBook's 5900 to this Mac's own Screen Sharing; Mac Studio remains disabled. `=off` enables the extension but no machines. With `MOCK_EXTENSIONS=1`, the extension list includes "Computer control (Cua)"; "Computers it can control" syncs bidirectionally with each Mac's `computer.enabled`. `MOCK_COMPUTER_OPERATING=1` adds a running operation to the assistant's "Calculate 6×7" and the Agent widget chat, showing "Operating MacBook · …" above input. `MOCK_DEVICE_TOKEN=jd_…` makes mock `/api/` accept only that token (`Authorization` or `?token=`), returning 401 invalid token otherwise, like hub (#1498: VNC page got a double-encoded token). `MOCK_COMPUTER_APPS=1` gives MacBook two assistant-approved apps (Calculator, entire screen), removable on its machine page. "Don't auto-lock while the extension is on": machine toggle asks first, and per-machine extension toggles honor `x-confirm`; mock syncs extension-page `no_auto_lock` to each `computer.no_auto_lock`. `MOCK_SCREEN=fake` replaces forwarding to real Screen Sharing with a mock server (macOS-like ARD login first when openssl exists, then VeNCrypt Plain, account `me`, password `mock-pass`; `GET /mock/screen-password?p=…` changes password, `&clear=1` clears recorded keys; correct password+Return unlocks mock MacBook). It draws one desktop frame, supplies ZRLE when requested, records pointers/clicks/clipboard (node `GET/POST …/computer/clipboard` shares it); `?clip=…` sends clipboard to clients, `&slow=S` delays S seconds (miniwindow "Connecting…"), `&down=1` drops current and future connections (`0` restores). `MOCK_SCREEN_ANIMATE=1` moves a square at about 30 fps. `MOCK_SCREEN_SCROLL=1` fills the mock Mac's window with lines of made-up text that its wheel scrolls (6 pixels a notch, no square), so a screen recording shows a scroll reaching the Mac. `MOCK_SCREEN_DISPLAYS=2` gives the mock Mac two 480×300 displays side by side (a 960×300 picture; display 2 green, each numbered) and has its Screen Sharing list them (ExtendedDesktopSize); `=2:node` lists none there and puts them in MacBook's computer status as `displays` instead, as a node reads them. `MOCK_SCREEN_BG=black|white|gray|terminal` makes the mock Mac 1920×1080 in one plain color (a terminal: near black with lines of light text); like macOS's, its Screen Sharing sends no pointer (the app draws its own arrow), and `MOCK_SCREEN_CURSOR=1` has it send macOS's 1× arrow (Cursor pseudo-encoding) as other servers do; `?bg=…` switches the background on connected screens. The mock also keeps `events`: clipboard writes (`via` node or rfb) and keys in arrival order, cleared with `clear=1`. VNC falls back to a full page, miniwindow needs a device. The live screen's video channel (#3954): `MOCK_SCREEN_VIDEO=1` (with `MOCK_SCREEN=fake`) has MacBook offer `screen_video` and plays `scripts/mock/screen_video/fixture.bin` (a made-up desktop, 3 s of HEVC; `make_fixture.sh` next to it makes it again), or `fixture-h264.bin` when the `start` offers H.264 before HEVC or only H.264 (as the helper picks), for a pair a 5900 connection carried; `=missing` offers it without Screen Recording, before macOS asked; `=denied` without it, after asking. `GET /mock/screen-video` shows what the phone sent; `?drop=1`, `?refuse=1|0`, `?pause=<reason>` and `?resume=1` act as the helper would, and `timing` goes out each second (capture and encode made up, the network half measured from the phone's reports). `MOCK_SCREEN_X264=1` (#8859) adds `screen_video.x264` ("ffmpeg 9.0.2") to it, so MacBook's page offers x264 for the live screen; the machine page's switch and the extension page's `screen_x264` stay in step, as `no_auto_lock` does. `MOCK_PHONE_SCREEN=1` (#9314, with `MOCK_SCREEN=fake`) has MacBook offer `phone_screen`: its picture is 750×510, the 480×300 main display and a 270×510 portrait one beside it showing a terminal-like window (both at 2×, in `displays`), and `POST …/computer/phone-screen` answers `start` (that display), `windows` (five made-up windows, one minimized), `place` and `stop`.
- File browser upload / Save to Files (#4858, `docs/protocol/protocol.md` "Changing files"): `MOCK_FILE_OPS=1` makes the mock take `dest=dir` (files go into the mock tree, a taken name is 409 `exists`, `conflict=keep|replace` as the node does, outside the home `read_only`) and the rename / move / trash / restore routes.
- Remote copy / compress progress (#6113): `MOCK_FILE_OP_DELAY=<seconds>` holds copy and zip POSTs open while `GET …/files/operations/{id}` reports synthetic file / byte progress; disconnecting the POST marks the operation cancelled and leaves no output. `python3 scripts/test/test_mock_file_operations.py` checks this over real HTTP. `testRemoteCopyProgressResumesAfterEarlyReturn` leaves before the first delayed row appears and requires progress and Cancel after returning; For the real web panel, start Vite with `PERSONA_HUB_URL` pointing at that mock and run `node web/e2e/file-operations.mjs http://127.0.0.1:<web port> --hub http://127.0.0.1:<mock port> --shots <directory>`; add `--large-text` to repeat it with the app's largest text setting. The browser check also leaves and returns to the operation's folder to verify restored progress and Cancel, and completes an operation while away to verify that its workspace notice survives returning. Restart the mock between runs so earlier synthetic outputs cannot contaminate the cancellation assertion.
- Temporary-code pairing (#2766): `MOCK_PAIR_TOKEN=jp_…` behaves like `personad hub pair`: usable everywhere before `POST /api/v1/auth/tokens` exchange, then for `MOCK_PAIR_GRACE` seconds (default 10 instead of hub's 2 minutes); a second exchange returns 401. Exchanged device tokens (`jd_mock_<n>`) remain valid; every 401 logs a line. Paired sockets close after grace like hub; `ws open host=… token=…` identifies handshake tokens. `MOCK_PAIR_TRADE_DELAY=<seconds>` delays exchange, `MOCK_PAIR_DROP=<seconds>` drops the first temporary-token socket once. Add `MOCK_HUB_NAME` and `MOCK_MESH_PEERS` with `center: true`, address `http://localhost:<port>`, to reconnect through the group's center (another address). Correct only if no 401 after grace and title isn't "Connecting…".
- Browser (docs/plans/plan-ego-browser.md): `MOCK_BROWSER=missing|setup|ready` enables `ego` and MacBook (not installed / installed without first setup / ready, analytics on; `MOCK_BROWSER_ANALYTICS=off` disables analytics); Mac Studio disabled/uninstalled. "Install ego lite" / "Open ego lite" advance every `MOCK_COMPUTER_STEP` seconds. With `MOCK_EXTENSIONS=1`, list includes "Browser (ego lite)".
- Phone control (docs/extensions/iphone.md): `MOCK_IPHONE=one|several|none|pair|off` turns on the extension `iphone` with MacBook switched on. `several`: two look alike and the user picks; `off`: the extension is off, so no section appears anywhere. `MOCK_IPHONE_ASK=1` adds an `iphone_do` approval card to Persona's main conversation. The guided setup (#8199): `MOCK_IPHONE=setup MOCK_EXTENSIONS=1` starts from nothing:
  - the extension off, listed as a built-in;
  - MacBook without the bridge (Install takes 2 steps);
  - Developer Mode off until 2 steps after pairing, then the developer disk image missing until Prepare;
  - Forget unpairs; the install job's step is in `MOCK_LANG` (`en`, `ja`, default `zh-Hans`);
  - Try It answers a screenshot and Weather opened.

  `GET /mock/iphone?reset=1` starts it over.
- Cards on no session (#4059): `MOCK_SCRIPT_ASK=1` adds a question card with an empty `session_id`, like `personad ask` run outside herdr;
  Agents' needs-you list and the inbox both list it, and tapping it opens the card in a sheet.
- Password card (`secret_run`, #1137, `docs/assistant/assistant.md` "Password card"): `MOCK_SECRET_RUN=pending|ran|withheld|long` puts it in Persona's main conversation: `pending` waits for the password (two commands on the Mac Studio); `ran` is answered, every command exited 0; `withheld` the same with the first command's output withheld (it held the password); `long` the second command failed with a long output (the app reads the whole result).
- Questions the assistant sees first (web #544): `MOCK_DELEGATE=1` marks `ix_tests` and `ix_release_delegate` ("整理发布说明") `delegating`, gives `ix_review_delegate` ("检查 PR #42 的改动") `delegate` context recommending one of its choices, and adds one delegated answer to `GET /interactions/history` in the hub's row shape (`{interaction, status, resolution, resolved_at}`).
  `GET /mock/delegate?answer=<id>` answers a looking card as the assistant (`interaction_resolved` with `answered_by` / `reason` / `answers`, then into history), `?handover=<id>` hands it to you (sent again without `delegating`, with context).
  `POST /interactions/{id}/reanswer` answers like the hub (409 not answered by the assistant, 400 no answers) and broadcasts `interaction_reanswered`; `GET /mock/reanswered` shows what arrived. There is no reset: restart the mock between runs.
- Chat search (#6281, `docs/design/proposals/assistant-search.md`): `MOCK_CHAT_SEARCH=1` turns the long Persona conversation into one whose marker words each sit in one place only (title, the user's words, a reply, a thought, a tool output folded and not, early history, a quoted context, a card's answer, an injected message) and pages its history by turns, `/mock/reset` included. Web: `node web/e2e/chat-search.mjs`. `GET /api/v1/assistant/search` answers over the mock's items by the same rule as the manager (#6282, `scripts/mock/test_assistant_search_mock.py`); `MOCK_ASSISTANT_SEARCH=0` is a hub from before it (no `assistant_search` feature, 404).
- Unified main-chat entry (docs/assistant/unified-entry.md): `MOCK_TASKS=1` supplies a started plan, three tasks working/waiting/done, one Agent-start failure (red card and receipt, #1077),
  answered questions, receipts, direct task messages and a final unstarted plan. `MOCK_TASKS_AT=<minutes>` cuts newer items so earlier portions sit at the bottom without scrolling.
  `MOCK_TASKS_NO_ASKS=1` suppresses task-Agent questions above input. `MOCK_TASKS_DOCK=2` keeps two working tasks (two dock capsules, then two manual Agents); `=pair` one task + one manual Agent waiting for you; `=many` adds four tasks (dock wider than screen, "All" fixed left); `=multi` keeps only "Fix widget landscape crash" with two Agents; `=long` keeps two overlong titles. Default: four unfinished tasks + two manual Agents (#1113). `todo_task_widget`) opens details;
  Start remedies (#10365): add `MOCK_START_REMEDIES=1` to advertise `task_retry_launcher`, `start_continue` and `todo_start_answer`. Three final failed-task fixtures cover terminal `launcher_missing` with `retry_with: [codex]`, `worktree_path_taken` on a to-do without `plan_id`, and a task `shell_waiting` question; Agent `macbook/a1f3c2d4-0000-4000-8000-000000000104` has the session question. Ordinary Retry stays bodyless, the alternative sends only `launcher`, and the two answer routes accept only an offered choice. `GET /mock/start-remedies` records their raw bodies. `MOCK_TOOL_SEARCH=1` independently gives the populated main chat the raw-400 `tool_search_unsupported` turn error for its dedicated Retry card.
  with the default mock's several assistants the MacBook, where the tasks' Agents run, is another assistant's, the app lists only the current one's sessions (#5587),
  and the sheet, with no Agent page to show, draws its own header with the title as text (#10253). The test stops at launch on a hub with several, naming `MOCK_MANAGERS=1`.
  Its newest line is a morning routine's `brief` item (#2320): a waiting card that names its chat and one that doesn't, failed and done rows past what is listed, a deployed to-do and a decision to verify, two questions that can wait and two to-dos in progress.
  Hub-injected messages (#2358, `docs/protocol/protocol.md` ("Messages injected by Persona")): `MOCK_INJECTED=1` adds an unmarked task notification (recognized by text) and a marked scheduled task below receipts;
  `MOCK_TASKS=1 MOCK_INJECTED=1 MOCK_TASKS_NO_ASKS=1 MOCK_TASKS_AT=14` puts them at the bottom.
  New conversation layout: `MOCK_TASKS=1 MOCK_TASKS_REPORTS=1 MOCK_INJECTED=1 MOCK_EXCHANGES=1 MOCK_TASKS_NO_ASKS=1 MOCK_TASKS_AT=5.5` puts a long Persona → task message, the task's long report (a link in its words, two evidence links) and a supervision digest at the bottom.
  Markdown in a task's words (#2363): add `MOCK_MARKDOWN=1` to put **bold**, `code` and a heading in that message, that report and the release notes receipt (`MOCK_TASKS_AT=19.9`); they render, with no raw marks, in either layout.
  Task answer records (#1231): "Complete network-layer unit tests" has three full question/answer pairs; "Fix widget landscape crash" is an old summary-only record. `MOCK_TASKS=1 MOCK_TASKS_NO_ASKS=1 MOCK_TASKS_AT=23` puts them at bottom;
  Place maps (#5543): `MOCK_TASKS=1 MOCK_PLACE_MAP=1 MOCK_TASKS_NO_ASKS=1` ends the main chat with "where am I" (`phone_location_current`) and a nearby café search (`phone_maps_search_nearby`, four made-up places), each with its map card under the work line, then the walk to the second café worked out as a route (`phone_maps_route`, its line on the card, #5544) and handed to Maps (`phone_maps_navigate`) with the hand-off's card last (#5545).
  What Persona sent to its tasks' agents (#1542): `MOCK_TASKS=1 MOCK_TASKS_SENT=1 MOCK_TASKS_NO_ASKS=1` adds, at the bottom, a task card whose long hand-off it takes in ("1 instruction sent"), a rework to the widget task as a folded line, and the user's own word to the unit-test task as a bubble; With `MOCK_TASKS_AT=1559` the bottom is yesterday's blog-migration card, now a line, with its hand-off as a folded line under it (#1668).
  The hub's notice line (#4305): `MOCK_TASKS=1 MOCK_TASKS_NOTICE=1 MOCK_TASKS_NO_ASKS=1` adds "电脑和模型接好了，定时任务恢复运行，这次是「早报」" as the conversation's last line, small and centered.
  What didn't go through overnight on the Today card (#4124): `MOCK_TODAY_FAILED=1` (with `MOCK_TASKS=1`) adds a task whose Agent didn't start, a failed scheduled run and a skipped one, with `failed_total` 5, so the row says how many in all and "2 more"; without it the card is as before.
  Message card states (#4797): `MOCK_TASKS_MESSAGE=filed` marks the card about the widget task as handled elsewhere (`filed_at`): no Send, the draft greyed out with a line saying it wasn't sent. `MOCK_TASKS_MESSAGE=sent` marks it as replied from the card (`sent_at`).
- `MOCK_TASK_ASK_LONG=1` (with `MOCK_TASKS=1`) gives the task's question four options with long descriptions; with `MOCK_ASK_LONG=source MOCK_CARD_SOURCE=1` beside it the tests check, on the task's question as the pager's second card (#12831), that the grabber makes it taller, shorter, the tallest and the default, the height kept after a relaunch.
- `/mock/reset` puts them back.
- Typing a model ID (#1644, `docs/assistant/model-config.md` "Typing a model ID"): `MOCK_CUSTOM_MODELS=1 PORT=<dedicated port> python3 scripts/mock/mock_hub.py` lists IDs typed before after the Claude Code / Codex lists (`custom: true`), takes Forget (`DELETE …/models/custom?model=`) and remembers a start's or a `…/modes` typed model; `=forgetfail` makes Forget answer 500 (the row comes back), `=empty` gives Claude Code an empty list ("Enter Model ID…"), `=fail` a 502 for every list; `GET /mock/custom_models` shows what is remembered (`?reset=1` puts the first IDs back). The mock lists `typed_models` in its features in every mode (the chat's "Other Model…" needs it). The web's: `node web/e2e/custom-model.mjs <web URL> --hub <mock URL> [--shots <dir>]` follows the mock's mode: New Agent, a session's model panel (the composer's model pill: the IDs typed before with Forget, "Use “…”" from the search, #5032) and a Persona chat's (#5033) (`=1`), a refused Forget in New Agent and the model panel (`=forgetfail`), New Agent without a list (`=empty`, `=fail`: the group's error and Retry, still the launcher's Default row and "Use “…”" from the search, #5104); light and dark.
- A new Persona conversation's model menu: `MOCK_MODEL_DEFAULTS=1 PORT=<dedicated port> python3 scripts/mock/mock_hub.py` answers `?purpose=assistant` with Main chat's `default`.
- Main chat already running on the other agent kind (#4662, `docs/assistant/model-config.md` "A conversation's own model"): `MOCK_MAIN_ACTIONS=1 MOCK_MODEL_PICK=otherkind PORT=<dedicated port> python3 scripts/mock/mock_hub.py` keeps Main chat's default on Claude while the current main conversation runs Codex and advertises the existing New main action. `node web/e2e/main-chat-agent-notice.mjs <web URL> --hub <mock URL> [--shots <dir>] [--chromium <executable>]` checks the actual Codex model pill, the notice, the existing New main dialog, and the Main chat editor's draft-dependent explanation. It also selects Codex to suppress the editor notice, selects Claude to restore it, and uses `GET /mock/model_pick?default=codex` / `?default=gone` / `?default=claude` with pushed session revisions to check live same-kind suppression, unknown-default silence, and mismatch restoration without reloading the chat. Runs light and dark at desktop standard text and phone large text; screenshots stay outside git and no live hub is used.
- Claude Code ultracode: `MOCK_ULTRACODE=1 PORT=<dedicated port> python3 scripts/mock/mock_hub.py` (the states are listed in the mock's help: on, queued, turned off by a resume, the node's reason, off, offline, Codex at `ultra`, Mac Studio's old Claude Code).
- Launcher health in New Agent: `MOCK_LAUNCHER_HEALTH=1 PORT=<dedicated port> python3 scripts/mock/mock_hub.py` gives every computer's launcher list `work_gw` blocked as not in the terminal and `team_gw` blocked on its key (claude checked fine),
  a start that runs `work_gw` fails with the hub's coded sentence, and `macbook/f1643000-0000-4000-8000-000000000001` is a chat that already failed so. `MOCK_LAUNCHER_HEALTH=default` also makes `work_gw` Model Settings' New Agent default.
  `MOCK_LANG=en|ja` writes the hub's start-failure sentence in that language (from the hub's own catalogs; default zh-Hans), for screenshots in that language. With `MOCK_TERMINAL_PANES=1` the computers have free shells for Run Agent Here.
- Agent folders (#8578, #10053, `docs/protocol/protocol.md` "Agent directory visibility"): every mock serves `GET`/`PUT /api/v1/machines/{mid}/agent-dirs/hidden`; rows whose `cwd` an enabled hide rule matches (a rule without an `action` hides) carry `hidden_by_directory` (bounded lists leave them out unless `include_hidden=1`), rows and cards an enabled block rule matches are left out entirely, as a node that blocks them sends none, and a change pushes that computer's rows and replays its pending cards, which never leave the inventory. `GET /mock/agent_dirs_fail?n=<count>` makes the next reads answer 503. It answers that approval, so restart the mock before each run.
- Terminal-only Agent (#6170): `MOCK_TERMINAL_ONLY=1 PORT=<dedicated port> python3 scripts/mock/mock_hub.py` lists a Kiro CLI in a herdr pane (`macbook/c0de0000-0000-4000-8000-000000000002`, with a `read_only_reason` as the node sets it).
  Web: `node web/e2e/launcher-health.mjs <web URL> --shots <dir> [--locale en-US]` against the same mock (wide / 390 × light / dark: the model panel's groups for blocked launchers with their rows off, their reasons and How to fix, the line under the first message box when what runs this time is blocked (#5104), the chat's coded failure, and, with `MOCK_TERMINAL_PANES=1`, the Run agent here menu).
- `GET /mock/hello` returns the last `client_hello`'s `features` / `platform` / `version` and the `X-Persona-Client` of the last `GET /api/v1/interactions`. Answered cards leave the mock: restart it before another run.
- The composer's @ list: `MOCK_MENTIONS=1 PORT=<dedicated port> python3 scripts/mock/mock_hub.py` has the review agent (`macbook/a1f3c2d4-0000-4000-8000-000000000006`) read `README.md` and `WeatherWidget/Provider.swift` in calls the hub leaves unfolded, so they are that chat's files; its folder is the mock's file tree. The same mock serves four workspaces, including `weather-app` on `macbook` at `/Users/me/code/weather-app` on `main`. It also picks `weather-app` in Persona's main and non-main conversations and a new conversation, then reads `/mock/sent` for a leading `kind="workspace"` block with the raw `machine`, `cwd` and `branch`; an Agent chat has no workspace rows. The mock records the client-authored tag but does not perform the real manager's workspace expansion.
- New Agent's attachments: `MOCK_UPLOAD_KBPS=96 PORT=<dedicated port> python3 scripts/mock/mock_hub.py` makes an upload take half a minute. `/mock/uploads` also names each upload's computer), another computer taking the files out with its note, a Codex launcher refusing a photo, and a to-do handoff keeping its context block first. With `MOCK_OMP=1` too, the mock lists an `omp` launcher and `testOmpStartsWithItsFirstMessageAndNoOptions` checks that omp gets no Permissions & Thinking and no attachments, and that its start carries the first message and no mode, effort, model or `async` (`docs/integrations/omp.md` "Starting from the apps"); without it that test skips.
- An offline machine beside an online one (#11283): `MOCK_OFFLINE_MACHINES=<id>[,<id>…] PORT=<dedicated port> python3 scripts/mock/mock_hub.py` takes those machines offline and keeps what they run (`studio` offline beside the online MacBook, both macOS; `server`, the assistant's own computer, turns the persona's face offline).
- `/mock/reset` takes it offline again), reopens New Agent and checks the three cards (the offline one not pickable), a machine card restoring its model, two cards filling every field with nothing started (`GET /mock/started`), Remove from Recent and Clear Recent Setups.
- Away recap in message flow (#1360): `MOCK_RECAP_LONG=1` adds eight turns before "Review PR #42 changes", taller than one screen, ending in a recap. `MOCK_RECAPS=1` keeps the earlier two as small bars when three are folded to one line.
- Stable content when scrolling up (`ChatView` freeze): `MOCK_STREAM=1` adds a step/reply every 2 seconds after opening first session, 12 times;
  in App container `Documents/freeze.txt`; final `RESULT:` should be 0pt movement.
- The running work line naming its step: `MOCK_LIVE_STEP=bash|command|reads|parallel|thinking|thinking-folded` puts the weather-widget chat a minute into a new turn whose newest step is still going; `bash` a command Claude described (its description alone, cut at its end), `command` a long command with no description (cut in its middle, the clock whole), `reads` three files read at once ("Reading 3 files"), `parallel` a search with two reads beside it ("+2"), `thinking` a thought after the calls came back, pushed `MOCK_LIVE_STEP_PUSH` seconds (default 3) after the chat's latest page (a page folds finished work, a push never is); `thinking-folded` has that thought in the page from the start, inside the fold (`fold.latest_thinking`).
- New Persona chat (#511): mock pushes session list immediately, transcript starts empty, first sentence appears/pushes after `MOCK_ASSISTANT_TRANSCRIPT_SECONDS` (default 1).
  After sending, chat retains that sentence (never "No messages yet"), selects new sidebar chat with Working spinner. `MOCK_ASSISTANT_START_SECONDS=<seconds>` delays POST;
  inspect bubble and "Persona is getting ready…" while waiting. Covered by `scripts/mock/test_assistant_mock.py`.
- A new chat's first message whose start failed (#1368): `GET /mock/start-fail?n=<count>&code=<code>&delay=<seconds>` cuts the next n starts off (`502`, default `assistant_in_doubt`, delivered unknown) and delays every start by `delay`; `n=0&delay=0` puts it back.
- Lark multi-account (#1321): `MOCK_LARK_ACCOUNTS=1` adds "Me", "Work account", one signed-out account to `/api/v1/lark/accounts`; `ol_account=2` reads Work chats, top Inbox item is from Work.
- Messaging sources (`messaging` extension point, #2138): `MOCK_MESSAGING=1`, `GET /api/v1/messaging` lists `lark` (extension `larkext`); `/api/v1/messaging/lark/…` follows `docs/extensions/extension-messaging.md`. `scripts/mock/lark_messaging.py` reads mock `/api/v1/lark/…` and converts, applying extension display rules for encrypted external/unlocked display, recall/edit history. Settings is full extension UI `POST /api/v1/extensions/larkext/-/ui/display`; hello/health add `messaging` feature, and every `{type: lark}` push also emits `{type: messaging}`. Can combine other fixtures (`MOCK_LARK=1`, `MOCK_LARK_TRAINER=1`, `MOCK_LARK_NATIVE_OLK=1`…). `python3 scripts/mock/test_lark_messaging.py` verifies schemas and processing.
- Hub without Lark gateway extension (#1530): default features include extension-provided `lark`, `lark_moments`; `MOCK_NO_LARK=1` removes them and `lark_attachments`, all `/api/v1/lark/…` return 404, no Lark connector. Messages, Settings › Connectors › Lark, translation's Lark chat option vanish; Lark docs/Moments links open as regular web pages.
- Lark /agent context (#876): `MOCK_LARK_AGENT=1` turns the "Schedule Friday…" request into Lark /agent (instructions + 7 context items in `<persona-context>`, only request outside).
- Conversations an extension opened (#3758): `MOCK_AUTO_CONVERSATIONS=1 MOCK_EXTERNAL=1` gives the Lark /agent auto chat a first message of context blocks and a request, and adds one opened by another extension (`…000000000310`, origin `extension`). Their rows carry the source mark and their first message "From Lark /agent" / "From an extension". With `MOCK_EXTENSIONS=1 MOCK_OPENER_ICONS=1` too (#10358), the two extensions that opened them are installed with icons on the hub's icon route (`larkext` as a made-up third-party "Chat Bridge" that reuses `lark.agent`, `example-ext` as "Example Docs"), so the apps draw and name the opener.
- Background supervisor (#4927, #5082): `MOCK_BACKGROUND_SUPERVISOR=1` adds the built-in conversation (origin `task`, no `origin_ref`) and names it in `GET /api/v1/assistant` (`background_supervisor`); with `MOCK_TASKS=1` the reviews in the main chat's feed carry `by: "background"` (with `MOCK_TASKS_REPORTS=1` an accepted one and a rework, else one accepted). Without the flag the mock still has the switch (`main_supervises_own_tasks`, saved by `PUT`) and answers `GET/PATCH /api/v1/model-config/purposes/assistant.supervise` (never in the catalog); `MOCK_LEGACY_HUB` has neither. `python3 -I scripts/mock/test_background_supervisor_mock.py` checks it; the web's check and screenshots are `web/e2e/background-supervisor.mjs` (`docs/web/web.md`).
- Availability follow-ups for the clients (#5802 to #5804, #5806): `MOCK_RULE_SUGGESTIONS=1` attaches a suggested standing rule to the PR check's decision (`ask.suggestion`, with `MOCK_TODO_ASKS=1`), to the network tests' question card (`interaction.suggestion`) and, with `MOCK_TASKS=1`, to the open plan (`plans[…].suggestion`, filled in where the plan is read), and answers `POST /api/v1/assistant/rule-suggestions/{id}/always` and `…/not-now` once (then 404, as for an id it never offered); `MOCK_ASK_HELD=1` holds the PR check's decision for the briefing (`ask.for_briefing`, with `MOCK_TODO_ASKS=1`); `MOCK_TODO_ASK_LONG=1` gives that decision a question and a recommendation of several lines and 12 hours of waiting, for checking the Needs you tray's decision card with long text (with `MOCK_TODO_ASKS=1`); `MOCK_BRIEF_SETUP=1` adds the recommended-setup lines to the morning briefing's `brief` item (with `MOCK_TASKS=1`); `MOCK_SUPERVISE_LISTED=1` lists `assistant.supervise` in `GET /api/v1/model-config` as the last built-in purpose and gives the built-in purposes a `description`. Without the flags none of the fields is sent. `python3 -I scripts/mock/test_availability_followups_mock.py` checks them. Always on the decision's suggestion also makes a grant that `GET /api/v1/assistant/standing-authorizations` lists and `DELETE …/{id}` revokes (only with this flag; the web's Settings › Assistant profile reads it). `GET /mock/reset` offers the answered suggestions again and takes back what Always made. The web's check and screenshots are `web/e2e/rule-suggestions.mjs` (`docs/web/web.md`).
- Persona sidebar: `MOCK_ASSISTANT_SIDEBAR=1` adds 36 Recent chats (above displayed 30, showing View All); first three Needs you (blue dot), unread (green dot), Working (spinner).
- Follow latest: long chat opens at bottom; new replies follow when already there (Persona replies first stream via `delta`); scrolling up shows "Back to latest",
  replies don't pull you down; tapping returns to bottom and resumes following. Agent opens tested normally, Unread, saved position (`-chat.read.<session> <row id>`), and cold-start with disk cache far from end (mock `/mock/paging?on=1` mimics hub pagination, `/mock/delay?s=3` shows cache first, #597; `/mock/reset` disables both).
  `GET /mock/say?text=…&user=…&stream=1` pushes replies to all open chats.
  `chat.scrollState`: bottom / follows / frozen / arrow / newestShown / withheld… (`visible` draws it).
  `testRealHubChats` captures real-hub before/after.
- Half-written Markdown in a streaming reply: `GET /mock/say?preset=repair&stream=1&steps=1&ms=0&upto=<n>`
  streams that reply into every open chat only to its first n characters and leaves it open there (no transcript line), so a moment can be
  photographed for as long as needed; the same without `upto` writes it. With the preset as written, `upto` 175 is inside a numbered list
  item's code fence, 277 a table's header row just ended, 333 a half-written row, 443 an open `**` with half a link in it, 524 inside a
  ```` ```mermaid ```` fence.
- Terminal stream: `MOCK_TERMINAL_STREAM=1` (hello `terminal_stream`) sends a fake Claude full frame after subscription, then one frame/second advancing spinner.
- Full herdr UI (web, `docs/integrations/terminal.md` section 24): `MOCK_TERMINAL_FULL=1` (hello `herdr_client`, `herdr_open` supplies fake sidebar/tab strip/two panes, single column when narrow; Ctrl+Y emits OSC 52; `GET /mock/herdr_client` inspects received `herdr_*`).
- Full-UI focus sync (web, `docs/integrations/terminal.md` section 28): the same mock advertises `herdr_focus` (`MOCK_TERMINAL_FULL_NOFOCUS=1` removes for old hub); page `herdr_focus` echoes with `persona`. `GET /mock/herdr_focus?pane=w1:p2[&session=<id>&source=client|persona|other&mine=1|0&view=<id>]` moves focus like node notifications. `scripts/test/web_herdr_focus_e2e.py` (usage at top, two mocks/two pages). Playwright sync API dispatches page events only during its calls: use `page.wait_for_timeout`, not `time.sleep`.
  `MOCK_TERMINAL_FULL_EXEC="herdr session attach <temporary session>"` runs real client in PTY (only your own temporary session), `MOCK_TERMINAL_FULL_END_AFTER=<seconds>` ends view later, `MOCK_TERMINAL_FULL_REFUSE=forbidden|unsupported|too_many` refuses.
  E2E `scripts/test/web_herdr_full_e2e.py` (four mocks/four Vites, usage at top), real herdr `scripts/test/web_herdr_full_live.py`; full-UI herdr/Persona conflicts (right-click, Ctrl+B, mouse, paste/drop, #2700): `scripts/test/web_herdr_full_conflicts_e2e.py` (one mock/one Vite, usage at top), live temporary session `scripts/test/web_herdr_full_conflicts_live.py`; Go live: `PERSONA_LIVE_FULL_SESSION=<temporary session> go test -tags herdrlive -run 'TestLiveFullClient|TestLiveFocusPane' ./internal/herdr/`.
- Standalone terminal (web, `docs/integrations/terminal.md` section 19): `MOCK_TERMINAL_PANES=1` (hello `terminal_panes`, `GET/POST /api/v1/machines/{mid}/panes` returns fake spaces/tabs/panes; `pane:` opens fake zsh; `panes/agent` in a listed shell adds an idle session bound to it and answers its id, which `/mock/reset` removes).
- Phone terminal takeover: `MOCK_TERMINAL_CONTROL=1` (hello `terminal_control`, fake Claude arranged to App columns × rows, typed text in input,
  `GET /mock/terminal_input` inspects bytes); `MOCK_TERMINAL_BUSY=1` shows "Another program on the computer is using this terminal".
- Attending a meeting (#2981): `PORT=<dedicated port> python3 scripts/mock/mock_hub.py` lists `meeting_attendance`, has `meeting_attendance_default` in `GET /api/v1/assistant` (`MOCK_MEETING_ATTENDANCE_DEFAULT=listener` starts there; a bad value is 400, an answer-only device 403), takes `attendance` / `attending_session` on uploads (the `200` says the role in effect and `attendance_problem`; `detail.meta` has both) and answers `POST`/`GET …/live` and `POST …/ask` with the contract's errors in its order. The attending session takes every live line at once, and a question gets a made-up answer at once (`speak` for a participant, a heard question from the live lines of the 45 s before `at`, the same answer for the same `id`). `POST /mock/meeting_attendance` with `{"feed": "busy"|"unavailable"|"not_allowed", "answer": "slow"|"fail", "answer_seconds": 3, "kb_read_only": true}` changes that (only the keys sent; `"ok"` puts one back), and `GET /mock/meeting_attendance` shows every recording's role, live log and asks; `POST /mock/recording_minutes` forgets them with the recordings. `MOCK_NO_MEETING_ATTENDANCE=1` is a hub without it (no feature or setting; `…/live` and `…/ask` are 404 without `recording_not_found`). `python3 scripts/mock/test_meeting_attendance_mock.py` checks it.
- Meeting minutes as to-dos: `MOCK_RECORDING_MINUTES=ready PORT=<dedicated port> python3 scripts/mock/mock_hub.py` gives a meeting's minutes a `version` and action ids, makes to-dos from `POST …/minutes/todos` (`409 minutes_stale` for an old version), keeps the minutes shown while `POST …/minutes` writes the next version (3 s), and `POST /mock/recording_minutes_rewrite {"id"}` writes it at once. `python3 scripts/mock/test_recording_minutes_todos_mock.py` checks it. The summary card (#6563): `MOCK_RECORDING_MINUTES_SHAPE=legacy|overview|malformed` (default `legacy`; `POST /mock/recording_minutes {"mode": "ready", "shape": "overview"}` switches it with the fresh start, a body without `shape` goes back to the variable's, any other value is `400` and changes nothing) picks the minutes' shape. `legacy` has no `overview`. `overview` cleans every text as the hub does (no `**`, one line, at most 400 characters with a cut's "…"; a sentence left without words is no item, in every shape), makes every sentence that neither decides, assigns nor leaves something open a point and adds `overview` from the same points and decisions, one bullet each: a topic per transcript minute, titled by the subject its first line names before a colon (`Export: …`, `导出：…`) or else its first words (more than 12 minutes share topics), icons in the contract's order, two groups (the meeting's halves) from 4 topics, the first number or 完成 / done in `strong`, the first 4 decisions as the conclusions and the first sentence as the headline. `malformed` keeps the legacy minutes and adds an `overview` that the contract's "Clients" rules clean down to nothing (members that are no objects, topics without a title or bullets, bullets without words). Every shape has `language` and the hub's `markdown` (the legacy layout byte for byte, or the summary card's with no checkboxes), and action ids, confirmation and `409 minutes_stale` are the same in each. `python3 scripts/mock/test_recording_minutes_overview_mock.py` checks it, and the mock's Markdown and a client's reading of every case of the shared fixture `spec/fixtures/minutes-overview/cases.json`; `MOCK_RECORDING_MINUTES_SHAPE=overview` runs the other minutes suites on the summary card's minutes. The same hub runs `testMinutesTimeInTheAppsDateStyle` (#4875): the minutes' time from their `start` / `end` in the app's date style, in Chinese and Japanese, light and dark (`meeting-<look>-minutes-when-<language>.png`).
- Meeting conversation (#3862): the same mock also lists `meeting_conversation`. An upload with `room` makes a room meeting (its `200` carries `room`, at least `{"state": "none"}`; the top-level `attendance` / `attending_session` are ignored beside it, and `private` is taken only while there is no mark); while it attends, its room conversation (an assistant conversation, origin `meeting`, the header first) comes up a moment after the upload, each `POST …/live` with new lines is a batch to it, and `…/ask` goes to it (`via`, `heard`, `spoken`; a made-up reply in the room). Earlier meetings are an answer's material only once marked `private: false`, and the next batch tells the room what the material answered. The first upload with `ended_at` and `complete` sends the end batch and turns the room `ended` (`409 meeting_ended` for asks and for messages into it). `PUT …/private`, `POST …/just-me` (to the main chat) and `POST …/followup` (a conversation of origin `meeting_followup` seeded with the meeting) answer in the contract's order, Just me and the follow-up behind the hub's operation receipts keyed by the body's `id`; `GET …/live` has `revs` and takes `parts`, `detail.meta` has `private`, `room` and `followup`, and a content-free `meeting_live` event goes out at most once a second per recording. `/mock/recording_trim` deletes the room. `POST /mock/meeting_conversation` with `{"room": "slow"|"unavailable", "problem": "node_too_old", "room_seconds": 2, "end_seconds": 180}` changes how rooms come up and how long an upload with `ended_at` alone waits for the end; `GET` shows every meeting's state. `MOCK_NO_MEETING_CONVERSATION=1` is a manager without it (no feature, `room` and `private` ignored, the three routes 404 without a code). `python3 scripts/mock/test_meeting_conversation_mock.py` checks it.
- Meeting live view (#3862): beside `meeting_conversation` the same mock lists `meeting_live_view`. `GET …/live?from=<n>` has `lines` (`{n, start, end, text}` from position `n`, at most 500, `more: true` while there are more; no `lines` without `from`, `400` for a `from` that is not a non-negative integer), and every `GET …/live` has `topics` (`[]` before the first) with `topics_at` once they were set. Each batch writes its lines at their clock time (`[HH:MM]`, the upload's `started_at` plus the line's start in the assistant's time zone, as the hub's batches do) and shows the room the topics so far after them in the same context block (`Topics so far:`, then `- state: title` rows), and after a batch with lines the room sets made-up topics from them with a `meeting_topics_set` call in its conversation: the current topic turns `done`, the batch's first line becomes the `current` one and the open ones stay (`Next steps` at first; Chinese for a `zh` recording). `lines` (the live log's length) and `topics` move in `meeting_live` and in `revs` (`?parts=lines` with `from` for the lines, `?parts=topics`), and `/mock/recording_trim` empties the topics. `POST /mock/meeting_live_view` with `{"on": false}` turns the feature off (from the app's next `hello`); `GET` shows every recording's topics. `POST /mock/meeting_topics` with `{"id", "topics"}` is the room calling the tool with those topics (the tool's limits: `400` naming the one broken). `MOCK_NO_MEETING_LIVE_VIEW=1` is a manager without it (no feature, `from` ignored, no `lines` or `topics`). `python3 scripts/mock/test_meeting_live_view_mock.py` checks it.
- Meeting highlights (#3862): beside `meeting_live_view` the same mock lists `meeting_highlights`. Every `GET …/live` has `highlights` (`[{text, at, end}]`, at most 4, the earliest `at` first; `[]` before the first) with `highlights_at` once they were set; the `200` of `POST …/live` carries neither. Each batch shows the room the highlights so far after the topics in the lines' block (`Highlights so far: none yet.` before the first, then `- [HH:MM] text` rows, never a line starting with `[`, which the apps read as a line of the meeting), and after a batch with lines the room makes a `meeting_highlights_set` call after its `meeting_topics_set` one: the batch's last line becomes a highlight (its words without the speaker's `Name:`, a question as `Open: …`, in Chinese for a `zh` recording, cut to 40 characters) at its minute and the others stay word for word, at most 4 (the earliest go first); a point that is one already sends the list again as it is, and there is no call when the resolved list would not change (a kept minute that gained lines gets the later `end`, which is a change). As on the hub, a `said_at` is the `H:MM` or `HH:MM` of a batch line (hours 0 to 23) in the assistant's time zone at the time of the call: its minute's lines are those with text whose start falls in it (`at` the earliest start, `end` the latest end; placeholders never count; a minute the clock shows twice is its first). The call is checked whole first, then refused on a read-only knowledge base (`kb_read_only`), then each minute resolved, and a refusal is the hub's words. A room started while the feature was off keeps the tools it started with: its batches show no highlights, it makes none, and its call is `unknown tool meeting_highlights_set`. `highlights` moves in `meeting_live` and in `revs` (`?parts=highlights`; `?parts=topics` stays the topics only), and `/mock/recording_trim` empties them (their revision 0 again, no event); the follow-up's seed lists them after the topics, as the hub's (`- [m:ss] text`, each at its place in the transcript). `POST /mock/meeting_highlights` with `{"on": false}` turns the feature off and `{"on": true}` on again (apps see it from their next `hello`, and only rooms that start while it is on get the tool; `/mock/recording_minutes` keeps the switch, so a UI test sets it in its setUp and tearDown), and `{"id", "highlights": [{"text", "said_at"}]}` is the room calling the tool with that list (`400` with the tool's refusal, `404` for an unknown id or with the feature off, else `200` with `highlights` and `highlights_at`); for a test an item may give `"at"` (seconds) instead of `"said_at"`, which names the minute of the line with text that starts last at or before it (the call in the room carries that `said_at`). `GET` shows whether it is on and every recording's highlights. `MOCK_NO_MEETING_HIGHLIGHTS=1` is a manager without it (no feature, and `GET …/live` as phase 2 has it: no `highlights`, no revision, no part). `python3 scripts/mock/test_meeting_highlights_mock.py` checks it.
- Meeting photos (#3862): beside `meeting_conversation` the same mock lists `meeting_photos` and answers `POST …/photos`, `GET …/photos`, `GET …/photos/{n}` (the image) and `DELETE …/photos/{n}` with the contract's limits and errors in its order (1 MiB decoded, 4096 px on the long side, `text` 8000 and `caption` 200 characters, the same `id` returns the stored photo and another body under it is `409 op_id_reused`, a deleted photo's `id` is `410 photo_deleted`, `413 photos_full` at 50, `409 transcript_expired` once trimmed, `404 photo_not_found`; deleting a photo already deleted answers `{"deleted": false}`). A photo of a meeting whose room attends goes into the room (once it is `ready`, when it is still coming up; the image and a line from the meeting, both with `client_msg_id` `photo-<id>`; `forwarded: true`), and the room's `note` arrives a moment later as a `meeting_photo_note` call; deleting a forwarded photo sends the room the withdrawn line (`client_msg_id` `photo-withdrawn-<id>`). Minutes cite each photo that has words (`"photo": n`) and are made again when one is added or deleted, `/mock/recording_trim` keeps only the cited photos, the follow-up's seed lists the photos still there (a trimmed recording's has its minutes only), search finds a recording by its photos' words (`photos.md`), and `photos` moves in `meeting_live` and in `GET …/live`'s `revs` (`?parts=photos`). `POST /mock/meeting_photos` with `{"note": "none", "note_seconds": 1, "on": false}` leaves photos without a note, changes how long the note takes, or turns the feature off; `GET` shows every recording's photos. `MOCK_NO_MEETING_PHOTOS=1` is a manager without it (no feature, the routes 404 without a code). `python3 scripts/mock/test_meeting_photos_mock.py` checks it.
- **Meeting transcription (#3862, phase 5)**: the mock advertises `meeting_transcription` only beside `meeting_conversation` and `meeting_live_view`. This is a scripted fixture for the meeting page and start sheet, not a speech recognizer; silence works as test audio and no service is contacted.
  - `GET /api/v1/assistant` returns `meeting_transcription_default` (`device`; `MOCK_MEETING_TRANSCRIPTION_DEFAULT=hub` overrides it), `meeting_glossary` (`[]`), `meeting_transcription_extension` (`""`, or the installed `voice/gpt-live` contribution), and read-only `meeting_transcription_ready` (`MOCK_MEETING_TRANSCRIPTION_READY=0` starts unavailable). `PUT` validates mode, glossary and contribution, requires full credentials, and ignores readiness writes.
  - Uploads accept `transcription` and `timeline`; every meeting upload's `200` names the effective mode, with `transcription_problem: unavailable` when hub mode could not be chosen. The first named mode stays fixed. Non-meetings requesting hub mode get `device` / `not_meeting`. Timeline roles merge by position (latest wins); speaking spans are `{"start", "end", "text"?}` objects (the text cut at 4000 bytes) and merge when they overlap or touch, their texts joined in time order (a span received again counts once); an invalid timeline (a span as a `[from, to]` pair included) is ignored whole.
  - `GET /api/v1/recordings/{id}/audio` upgrades to a WebSocket. Caller and manager refusals are HTTP `403` / `503`; recording refusals are the first fatal `error` frame after upgrade. Send `start` with `at` and `timeline`, wait for `ready {from, gaps, next}`, then send PCM16 LE mono at 16 kHz, at most 64 KiB per frame (a text frame at most 8.5 MiB). `seek`, `role`, `speaking` (on with the answer's `text`) and `end` work on the recording clock; a second socket replaces the first with `replaced`. `ack` comes every five seconds of received audio and on `end`. Per four seconds of audio, growing `interim` captions become one made-up Chinese/English or English `line`, unchanged by the recording's language. Speaking spans hold back the sentences after they began, then mark Persona's echo by the hub's rule (a line overlapping the span widened by 0.5 s whose words are 60% of the answer's character bigrams; a span without text keeps the time rule): an echo sentence stays in the hub's transcript (`detail.segments`, `echo: true`) and never reaches a `line`, the live log or the room, while anything else said during the answer goes out as usual. A `start.timeline` that has the open span closed ends the hold at once, and so does an upload `timeline` once the socket that sent the span's `speaking` on is gone. A span open for 180 seconds of audio is assumed open from then on: what it held and every sentence after it are judged with the answer it carried, no caption goes out, and a late `speaking` off still writes it to the timeline; a `speaking` on at a later time ends the open span there for what it held, and a span open at `end` is judged up to `end.at`. An end the mock assumed is never written to the timeline. Role `off` keeps hub text but emits no live line. `POST …/live` refuses hub meetings with `409 hub_transcribes` after the attendance check. A heard `…/ask` (with `at`) first flushes the scripted sentence the audio is in, cut at `at`, once the audio reaches it, and waits for sentences held for a speaking span; in the fallback it waits for an upload whose segments reach `at` (at most 15 seconds either way).
  - `PUT …/audio?at=<seconds>` fills gaps (whole PCM samples, at most 10 MiB). `GET …/live` and `detail.meta` carry `transcription`; its revision and `?parts=transcription` work, as do `interim` and `?parts=interim`. Content-free `meeting_live` events name the changed parts. After the complete upload, `finishing` waits for the socket to close and gaps to be filled (or the 30-minute deadline), then the scripted final pass takes two seconds; it makes hub text only while the chosen contribution is ready and the upstream works, else it keeps the live sentences, and either way marks what the complete upload's timeline says is Persona's echo (`final` with only echo sentences, `failed` only with none). `detail.segments` use hub text with device text in uncovered spans (`source: hub|device`), echo marks kept; `chars`, the summary and the mock minutes leave Persona's echo out (a meeting of only echo gets none); the held audio spans are then cleared. In the fallback a device segment that is Persona's echo (marked by the phone, or by the rule against the timeline) never joins the live log. The mock does not list `meeting_echo` (nor did it before): a device-mode upload's `echo` marks are kept as sent (left out of `chars` and the minutes), and `spoken` is ignored. The trim keeps transcription mode and state. A fatal upstream error enables device fallback immediately; a disconnected or degraded uplink does so after 30 seconds.
  - `POST /mock/meeting_transcription` changes only the fields supplied: `{"on": true, "ready": true, "upstream": "ok", "fatal": "", "line_seconds": 4, "final_seconds": 2}`. `upstream` also accepts `forbidden`, `busy`, `unavailable`, `failed`, or `no_interim` (the `problem` codes `upstream_forbidden`, `upstream_busy`, `upstream_unavailable`, `upstream_failed`, `no_interim`, #13446); `fatal` injects a fatal error code (empty clears it). A degraded switch shows its `problem` at once; back to `ok`, the standing problem ends (`problem` `null`) only once a hub sentence comes again (a caption, for `no_interim`). `GET` shows the switches and each recording's received frames, byte count, timeline, gaps, held spans, whether a socket is up (`socket`) and transcription state. `POST /mock/recording_minutes` resets recordings and closes their sockets. `MOCK_NO_MEETING_TRANSCRIPTION=1` simulates an older manager: no feature or settings, upload fields ignored, audio routes `404`.
    as 30 seconds pass without more audio reaching the mock (`GET /mock/meeting_transcription`'s `bytes`), then comes back and
    stops it. It prints a `long-recording:` line per check, to line up with the app's CPU sampled beside it
.
  - Run `python3 scripts/mock/test_meeting_transcription_mock.py` (it also checks the mock's echo rule against `spec/fixtures/meeting-echo`); regress all meeting phases with `python3 -m unittest discover -s scripts/mock -p 'test_meeting_*_mock.py'`.
  - The same class checks the UI: the start's Transcription row only while the mock is ready (`{"ready": false}` and `{"on": false}`, the switch behind `MOCK_NO_MEETING_TRANSCRIPTION=1`, take it away), Settings › Meetings saving the default and the glossary (read back from the mock's `GET /api/v1/assistant`), and the meeting page's hub lines, then its fallback notice, with the Lock Screen after each, then after Stop the recording's page saying how it was transcribed; with the highlights the hub's lines on the Full Transcript page and the notice on the Highlights card and the transcript.
- Voice mode (docs/assistant/voice.md 6.8, #3508): `MOCK_VOICE=1 PORT=<dedicated port> python3 scripts/mock/mock_hub.py`.
  `testTheBarBackClosesAPictureOverItsConversation` needs the mock started with `MOCK_TURN_IMAGES=many` as well (skipped without): the blog conversation's picture opened full screen over its own voice chat: where the viewer hides the page, the bar over it and its tap closing the viewer, the bar going once the conversation shows; where the page counts as shown under it, the viewer closed by its ✕ onto the voice chat's own bar.
  `GET /mock/voice2?unprompted=1` has the calls going on hand over something nobody asked for (run refused, `unprompted`); the web's `web/e2e/voice.mjs` uses it to shoot `<chat>-<lang>-<theme>-unprompted.png` and click Send anyway.
  The same mock serves the system calls: its voice extension lists `hold`, answers `{"type":"hold"}` as the extension does (`link paused`, then `waking` and `live`), and `GET /mock/voice2` keeps each start's `surface` (`"call"` for a system call) and the app's hold frames (`holds`, oldest first). `testACallFromAnAgentsConversation` does the same from an Agent's conversation (the start's `target` is that session; its title on the call screen and the bar, the bar over the main conversation too, where the phone icon asks "End and Call" naming the Agent's conversation and Cancel keeps the call; `agent-call.json`). `testThePhoneIconIsTheOneVoiceEntry` also shoots each composer's phone icon (`main-composer-phone.png`, `agent-composer-phone.png`) and checks it says Call This Conversation. `testNothingInTheAppEndsTheCall` walks the app with the call on (the bar over the Agents page, an Agent's conversation, the + panel and Settings, the call screen opened over the panel, dictation refused, the phone icon in an Agent's conversation asked "End and Call" and cancelled, a typed line in the main conversation, Home, another app and the lock) and writes the mock's audio count and socket closes at every step to `walk.json`; `testTheAssistantOnTheCallScreen` shoots the assistant in each state; `testTheCallFollowsTheAssistant` needs a mock with `MOCK_PERSONAS=builtin MOCK_LOOK=nwverse` (the frog look) and checks its name and face on the call screen and the bar (skipped without).
- Meeting voice (#3862): with `MOCK_VOICE=1` the same mock lists `meeting` in the voice extension's `features` (`/info` and `ready`) and takes meeting calls on its socket (`scripts/mock/voice_meeting_mock.py`). A `start` with `"mode": "meeting"` must be protocol 2 and name a recording whose room is `ready` in the mock's own meeting state, with that room as `target` (the recording id in any case); anything else is the fatal error `not_meeting_room`, and an ordinary protocol 2 `start` bound to a room (origin `meeting`) is the fatal `meeting_room` (both close the socket with 1011). `ready`'s target is the room (`kind: assistant`, `agent_label: Persona`); `echo_cancellation`, `history` and `resume` are logged. Audio frames are counted inside and outside `{"type": "talk", "on": true|false}` spans; after `talk off` with audio in the span the call answers by script (the user's caption partial then final, a run `sending` → `sent` → `working` → `done` whose `voice-<16 hex>` message is the question in the room's thread, the room's answer there and as the call's caption, 24 kHz silence as its audio; Chinese for a `zh` recording), an empty span gets nothing, and a `text` frame gets the same answer from its caption on. A question once the room has ended fails its run with `meeting_ended`. A meeting call never sends `link paused`; talk frames in an ordinary call are ignored. `GET /mock/voice_meeting` shows the meeting calls' starts (the newest as `start`), the refusals, the talk frames and spans, the audio frames inside and outside spans and the questions answered (`?reset=1` forgets them); `?meeting=off` (or `MOCK_VOICE_MEETING=off`) is an extension from before phase 4: no `meeting` feature, and a meeting or room-bound start is an ordinary call. The pace is `MOCK_VOICE_PACE`'s. `python3 scripts/mock/test_meeting_voice_mock.py` checks it.
- Settings › Extensions: `MOCK_EXTENSIONS=1 PORT=<dedicated port> python3 scripts/mock/mock_hub.py`.
- Secret references and the hand-off card (#3962, `docs/extensions/secret-providers.md`): `MOCK_EXTENSIONS=1 MOCK_SECRET_HANDOFF=1 PORT=<dedicated port> python3 scripts/mock/mock_hub.py` adds a made-up provider, Example Vault (its users and one Always rule), an Example Notes secret that holds its reference, and a hand-off card in Persona's main conversation;
  Machine and launcher pickers, integer steppers (#2385): add `MOCK_EXTENSION_PICKERS=1` (extension "Example 聊天", its auto-reply launcher one the hub no longer has);
  Extension packs (#4072, #4122): add `MOCK_EXTENSION_PACK=1` (Example 合集 installed with four members, one not installed; uninstalling it with its members answers 409 `partial_uninstall` and changes nothing; Example 套件 in the catalog to preview);
  Restart mock before each round (in-memory state).
  Extension CLI/login (#4507): fresh `MOCK_EXTENSIONS=1 MOCK_EXTENSION_OPS=1 MOCK_EXTENSION_AUTH=1 PORT=<port>` mock and old hub `MOCK_EXTENSIONS=1 MOCK_EXTENSION_OPS=1 MOCK_EXTENSION_AUTH=installed MOCK_EXTENSION_AUTH_OLD=1 PORT=<port 2>` each time;
  `MOCK_EXTENSION_OPS=1` all succeeds; `MOCK_EXTENSION_OPS_STEP=<seconds>` slows for screenshots. Without OPS, `MOCK_EXTENSION_INSTALL_DELAY=<seconds>` shows old-hub Installing.
  Extension settings Review (#3692): `MOCK_EXTENSIONS=1 MOCK_EXTENSION_OPS=stall MOCK_EXTENSION_OPS_STEP=3 PORT=<port>` (the first preview of each extension stalls half-way through its download; `/mock/reset` arms it again) and an old hub `MOCK_EXTENSIONS=1 MOCK_EXTENSION_PREVIEW_DELAY=8 PORT=<port 2>` (a synchronous preview answers that late).
- Model fallback after an upstream refusal (#4489): `MOCK_MODEL_FALLBACK=1 MOCK_MAIN_ACTIONS=1 PORT=<dedicated port> python3 scripts/mock/mock_hub.py`; `GET /mock/model-fallback/reset` clears a pending restore.
- Git pull: `MOCK_FILES=1 MOCK_GIT_PULL=1 PORT=<dedicated port> python3 scripts/mock/mock_hub.py`;
  Server `TestGitPull*` uses temporary local remotes with isolated HOME/Git config to test unfetched latest commits, fast-forward/divergence, modified/staged/untracked files, existing stash, failure; never pulls real worktrees.
- File viewer: `MOCK_FILES=1` provides fake `/Users/me/code/weather`: small file per language,
  3 MB JSON, 20K-line Go, 4000+-line README, 5 MB log; `GET …/file` (2 MB/page) and `…/stat` serve it. "Add a Home Screen widget to the weather App"
  final reply links several files and two nonexistent paths; `data/chart.png` is a picture (`testPictureOpensItsViewerOverTheSheet`). Timing/fps: build `-O`
  `Library/Caches/file-perf.log`.
  Uninstall before changing mock message text: item-id caches don't replace old text under the same id.
- Spreadsheet viewer (web #1310): `MOCK_FILES=1 MOCK_SHEETS=1` adds `data/stations.csv` (3000 rows, quotes/newlines/Chinese), `reports/季度报告.xlsx` (quarterly report; three sheets, merged title, widths, numeric/percent formatting); open in file panel. `file?stream=1` returns only binary descriptions; content via `file/download`.
- Office preview (web #1345): add `MOCK_OFFICE=1`; `reports/` gains handmade `季度报告.docx` (quarterly report: headings/tables/images/footer), `季度回顾.pptx` (quarterly review: theme/master/layout placeholders/bullets/images/shapes/tables/charts/notes), `旧版说明.doc` (legacy notes; mock `file/convert` returns PDF), `路线图.pages` (roadmap; returns `no_converter`).
- Mermaid (web #1376): `MOCK_MERMAID=1` adds flowchart, sequence, pie and one invalid diagram to final widget reply.
- Jupyter (web #1407): `MOCK_NOTEBOOK=1`, `notebooks/收入分析.ipynb` (income analysis) has Markdown/math, HTML table, stdout/stderr, PNG and ANSI-colored error.
- HTML preview (web #1413): `MOCK_HTML=1`, `reports/周报.html` (weekly report) has styles, a text-changing script (default "Script did not run"), remote image (blocked), local `chart.png` and `周报.css` (weekly-report stylesheet; loaded, toolbar "Loaded 2").
- Media (web #1422): `MOCK_MEDIA=1` adds WAV, scripted SVG, browser-unrenderable HEIC (`file/thumb` draws stand-in) under `media/`; `MOCK_VIDEO_FILE=<webm/mp4>`, `MOCK_SENT_VIDEO=<same>` show file-page video/chat card (WebM safest for headless Chrome).
- Compare (web #1437): `MOCK_COMPARE=1` adds changed `reports/季度报告.docx` (quarterly report), `reports/季度回顾.pptx` (quarterly review), `assets/banner.png`; `git/blob` serves staged/HEAD versions.
- Images seen this turn (web #1540): `MOCK_TURN_IMAGES_WEB=1`: final collapsed-work turn of Review PR #42 has 9 (Read/screenshots/web/MCP/Codex view_image); release-notes turn 1 card; working widget turn 2.
- A connector tool's pictures (#8484): `MOCK_TOOL_IMAGES=1` adds the assistant conversation "这个早餐视频讲了什么": a connector tool answered with three made-up video frames, each with its `[image N of 3]` line in the result, as the hub gives them to the model.
- A picture that did not load (#182): `MOCK_IMAGES_FAIL=1` (or `GET /mock/images`) makes lazy chat pictures answer 503 until `GET /mock/images?ok=1`; with the variable set, pages also leave the pictures' bytes out (as the hub does for `lazy_images`), so a tool step's picture says "Image didn't load.
- A tile that did not load (#559): `GET /mock/images?asked=1` reads how many lazy pictures were asked for, changing nothing.
- Each test starts with `GET /mock/reset`, which restores the two own plus one relayed messages and clears the queue-edit log. The overflow case uses `GET /mock/reset?photos=4` with one photo already in the composer, then checks a refused five-photo send leaves all five in place.
- Video preview: synthesize 120-second >2 MiB H.264/AAC MP4 (e.g. `ffmpeg -f lavfi -i testsrc2=size=640x360:rate=30 -f lavfi -i sine=frequency=440:sample_rate=44100 -t 120 -c:v libx264 -b:v 3M -g 30 -pix_fmt yuv420p -c:a aac -movflags +faststart /tmp/preview.mp4`); `ffmpeg -i /tmp/preview.mp4 -c copy /tmp/preview-tail.mp4` creates tail-index version. `MOCK_FILES=1 MOCK_VIDEO_FILE=/tmp/preview.mp4`, optionally `MOCK_VIDEO_DELAY=0.01` per 32 KiB network chunk. `GET /mock/video-reads` reports Range starts/ends, bytes sent, active/cancelled; `?reset=1` clears. Mock correctly returns 206, Content-Range, ETag, If-Match 412, never counts a full response as Range success.
  Without fixture integration test skips, not passes.
- Sending video (#981, docs/protocol/protocol.md ("Sending video")): AVAssetWriter synthesizes a 6-second H.264 MOV (no ffmpeg required, gradient with sweeping bar, no real video);
  Give another to `MOCK_SENT_VIDEO=<file>` (chat card sent by "another device", file only on computer); uploaded videos are stored by mock and downloadable.
  another test opens computer-only card, downloading first.
  `MOCK_UPLOAD_KBPS=640` makes progress visible.
  Synthetic 4,116,551-byte video fully downloaded, playback advanced, seek to 8 seconds and cleanup passed. Initial unsigned keychain failure, hidden-shell assistant executing `openFile` breaking old file UI (recorded in #412), merged fixtures displacing file block, and appearance labels disagreeing with screenshots remain invalid/failed records; assertions weren't weakened.
- Avatars: `MOCK_PERSONAS=demo` uses `scripts/assets/make_persona.py demo` to draw original demo avatar "Mint" on startup;
  `MOCK_PERSONAS=<directory>` lists its packs (other avatars, packs not committed).
  It changes saved `ui.style` / `ui.color`, then restores Liquid Glass + Follow interface style.
- Hub version/update: `MOCK_HUB_UPDATE=available|waiting|notice|docker|docker-pinned`: new version with auto-update off,
  waiting on two busy Agents, first "Automatic updates enabled" notice, Docker new image (`:latest` or pinned tag). Unset: `GET /api/v1/hub/update` 404, old hub, absent everywhere.
  `GET /mock/hub_update?mode=…` changes/restarts live.
- Terminal disconnect: `MOCK_SCREEN_FAIL_AFTER=3:14` returns 503 after 3 seconds of terminal reads, recovers 14 seconds later;
  Timing starts at first request, not mock startup; if old App polls during build, start App before mock.
- Hand-off write-back card (#5932): `MOCK_TASKS=1 MOCK_HANDOFF_CARD=1` puts an item handed to the main conversation, with that conversation's reply suggestion, into the Inbox and its write-back card into the main conversation's feed, so the chat draws the card and "Wrap up: 示例客户" over the composer; confirming the card files the item as replied. `MOCK_HANDOFF_CARD=sent` starts with the reply sent.
- Hand to AI per source (#2121): `MOCK_INBOX_EVENTS=1 MOCK_EVENT_MUTES=1` gives three events their mute options. The two mail notifications ("构建失败：主分支" and "会议室变更通知") share one sender, so handing one off preselects that target, marked Last time, on the other.
- `MOCK_LARKCLI_INBOX=legacy` gives the page as an older hub did (empty kind and name), which the apps still draw as a chat. Data: `larkcli_thread_page` in `mock_hub.py`.

### Mock hub domains (`scripts/mock/mock_domains.py`, #11016)

`mock_hub.py` is one long file that most mock changes edit, so a new made-up area of the hub goes into a module of its own
(a *domain*) instead. The module has a `register(d)` function and gets one line in `DOMAINS` in `scripts/mock/mock_domains.py`;
`mock_hub.py` does not change. `register(d)` declares what the domain needs:

- `d.route("GET" | "POST" | "PUT" | "PATCH" | "DELETE" | [...], r"<path regex>", handler)`: `handler(h, req)` answers with
  `h.send_json(obj, status)`; `req` has `method`, `path`, `query`, `match` (the regex's groups), `body` and `json()`. Routes run
  after the token check (and a GET's hit count) and before the route chains of `H.do_GET` and `H.do_POST`; a handler that returns
  `False` hands the request on. A route outside `/mock/` gets the simulated network faults first, as the built-in `/api` routes
  do: `/mock/down` closes the connection unanswered and `/mock/delay` (or `MOCK_DELAY`) waits, once a request even when the
  route hands it on; `/mock/reject` and the token checks come before the routes, and a lost receipt (`POST /mock/fault`)
  reaches them through `h.send_json`.
- `d.ws("<message type>", handler)` and `d.on_ws_close(handler)`: `handler(ws, msg)` and `handler(ws)`, where `ws` has `connection`,
  `send(obj)`, `client` (its role and declared features) and `asked`. Messages reach the domains before the built-in handlers. A
  close handler that raises is logged on stderr; the other domains' close handlers and the hub's own cleanup of the socket still run.
- `d.feature(*names, when=None)` and `d.drop_feature(*names, when=None)`: the hello's and `/api/v1/health`'s `features`, applied after
  the built-in ones (`drop_feature` plays an older hub); `when` is asked each time. A block moved out of `mock_hub.py` into a domain
  can so move its features later in the list (with `MOCK_DESKTOPS=1` and `MOCK_AGENT_ARCHIVES=1`, `desktops` and
  `desktop_unregister` now follow `agent_session_archives` and `agent_session_archive_close`); the clients read the list as a set.
- `d.help("MOCK_X=1  what it does")` for `mock_hub.py --help`, and `d.on_reset(hook)` for `GET /mock/reset`, after the built-in
  reset, so a hook can put back a fixture the reset rebuilt.
- `d.hub.<NAME>`: `mock_hub.py`'s globals, read when used (`SESSIONS`, `ITEMS`, `CLIENTS`, `broadcast(msg)`, `push_machine(id)`, …).

Domains load in list order after every built-in switch has built its fixtures, and before the To-dos view
(`todo_view_mock.py`), which replaces the to-dos. A process that loads `mock_hub.py` again (`runpy` in a test) runs each domain
module again too, so its module-level state starts over as `mock_hub.py`'s globals do. A domain reads its `MOCK_*` switch from
`os.environ`; what exists only with the switch is registered only when it is on (the help line always), so with it off the hub
answers as before. The domains on the registry are desktop registration (`scripts/mock/desktops_mock.py`, #11053), GitHub's
sign-ins and thread page (`scripts/mock/github_mock.py`), Settings › Knowledge selection (`scripts/mock/kb_selection_mock.py`)
and mail's accounts and write paths (`scripts/mock/mail_mock.py`), moved out of `mock_hub.py` in #11543, and the hub's own
update (`scripts/mock/hub_update_mock.py`), Messages' reader (`scripts/mock/imessage_mock.py`; its connector stays in
`mock_hub.py`) and Settings › Saved Prompts (`scripts/mock/saved_prompts_mock.py`), moved in #11649, and a project's lessons from
finished tasks (`scripts/mock/project_lessons_mock.py`, #12295; `MOCK_PROJECT_LESSONS=none|missing`, driven by
`scripts/mock/test_project_lessons_mock.py`). The opt-in SSH → Persona
Link scenarios live in `scripts/mock/link_switch_mock.py` (#10671): `MOCK_LINK_SWITCH=1` makes `server` an eligible Linux SSH
computer, and `GET /mock/link_switch?state=available|checking|installing|connecting|rolling_back|succeeded|failed|unconfirmed|unavailable|refused|forbidden|uncertain|hold|old`
changes the fixture and pushes the machine list. `hold` keeps the POST answer out until `?release=1`; the no-query response reports
the state and start-attempt count. With the switch off, existing mock screens do not change.
`scripts/mock/test_mock_domains.py` checks every hook against a running mock. A block that moves out of `mock_hub.py` lists
the names it took in that test's `MOVED`: a branch from before the move that kept the old block when it resolved its conflict
then fails there, with the module to edit instead
(`docs/lessons/testing.md`, "A moved file or block needs a test that fails when the old copy comes back").

`MOCK_OUTSIDE_DIRECTIONS=1` (`scripts/mock/outside_directions_mock.py`, #8236) is a read-only, three-row direction
fixture: one existing conversation, one outside direction bound to an existing coding Agent, and one targetless outside
direction with a long name. The two outside ids are not Persona conversations; `/assistant`, `/leaders` and
`/directions/tree` keep the same order and metadata. The bound Agent's own to-do stays under Secretary and its idle state
does not drive the direction's running mark. It is off by default and composes with `MOCK_LEGACY_HUB=1` by leaving the
legacy 404 intact. `python3 -I scripts/mock/test_direction_tree_mock.py` checks the fixture and its reset behavior.

**No new switch in `mock_hub.py` (#12737).** Most of what `mock_hub.py` gains comes with a new `MOCK_*` switch (89 of the 195
commits that changed it from 2026-11-14 to 2026-11-22 added one, with 72% of its net added lines), so its switch names are
frozen. It may read only the names in `scripts/mock/mock_hub_switches.txt`: the ones it read when the freeze landed, then the
ones the PRs open that day were adding, each with its PR. Both parts are written on the day the freeze lands, after
`origin/main` is merged in, by `python3 scripts/mock/mock_hub_switches.py pending --write`: the names `origin/main` reads,
including a pending one whose PR has merged by then, and the names open PRs add, keeping a listed one while its PR is open.
Once the list is on `main` the command writes nothing, so it is not how a later PR gets a name on the list.
`scripts/mock/test_mock_hub.py` parses the file and fails on any other
`MOCK_*` name it reads through `os.environ.get`, `os.environ[...]`, `os.getenv` or `in os.environ`, or has as a whole string
anywhere (a copy of the environment read later, a helper given the name), and on an environment read whose name is not a
literal; a listed name the file no longer reads is a notice. Check-fast runs the test for every change to `mock_hub.py`.

A new switch is therefore a domain module, and so is **a new value on an existing switch**: `MOCK_INBOX=rules` next to
`MOCK_INBOX=empty` is as new as `MOCK_INBOX_RULES` would be. The domain module reads the switch itself, registers its routes
only for that value, and answers there first; a handler that returns `False` for a request it leaves alone hands it on to
`mock_hub.py`, which keeps serving the switch's other values:

```python
# scripts/mock/inbox_rules_mock.py, with one line in DOMAINS
import os

RULES = os.environ.get("MOCK_INBOX") == "rules"


def register(d):
    d.help("MOCK_INBOX=rules  the Inbox with filing rules (#<issue>)")
    if not RULES:
        return  # any other value: mock_hub.py answers as before
    d.route("GET", "/api/v1/inbox/policies", policies)  # policies(h, req): h.send_json({...})
    d.route("GET", "/api/v1/inbox", page)               # page(h, req): False for a request it does not change
```

(`policies` and `page` are the module's own functions.)

Editing an existing switch's code in `mock_hub.py` is fine; the freeze is on names, not lines. A block that moves into a
domain module takes its names off the list in the same change (the notice says which). A line ceiling is worth another look
only if the file keeps growing by more than about 150 lines a week without new switches.

### Readiness scenarios (`MOCK_READINESS`, #287)

New-user onboarding states ([onboarding.md](../assistant/onboarding.md) 4.4–4.6, section 5) share these fixtures across web Playwright screenshots. Start with `MOCK_READINESS=<scenario>`:
adds `assistant_readiness`, `machines_add`, `machine_trust`, `builtin_presets`, `launcher_auto_name` to `features`; `GET /api/v1/assistant/readiness[?probe=1]`, `POST …/readiness/dismiss`,
`POST /api/v1/assistant/warm` (with `readiness`) return hub-shaped data/text (read `server/internal/i18n/locales`, using `Accept-Language`, Chinese if absent).
`POST /api/v1/assistant/conversations` rejects with scenario codes; `GET /api/v1/launchers/presets` lists built-in providers (directly from `server/internal/assistant/presets_builtin.json`), then mock plugin presets.
Add/confirm computer (`MOCK_ADD_COMPUTER`) is enabled too; readiness follows machines: in `zero`, add, wait for connection, confirm, then `computer` becomes `ok`.

| Scenario | Computer | Readiness | Start chat |
| --- | --- | --- | --- |
| `zero` | None | `computer` todo `no_machine`, others `unknown` | 409 `no_machine` |
| `offline` | Demo MacBook (`mbp`) offline (`error_code: link_offline`) | `computer` problem `node_unreachable` | 503 `node_unreachable` |
| `pending` | `mbp` online, pending (`trust: pending`) | Same as `zero`: hub doesn't give pending computers to assistant | 409 `no_machine`; Agent start there (`POST /api/v1/sessions`) 409 `machine_pending` |
| `no_claude` | `mbp`, no `claude_version` | `claude` problem `claude_missing` | 409 `claude_missing` |
| `not_logged_in` | `mbp` | `model`, `first_message` problem `not_logged_in`; sole main-chat turn "Not logged in · Please run /login" (`turn_error_code`) | Normal |
| `not_logged_in_desktop` | `mbp` | As `not_logged_in`, with the model step's words for a Mac that offers the Claude desktop app's launcher: an Agent started with `claude-desktop` runs in the app on the app's own sign-in (#9366) | Normal |
| `bad_key` | `mbp` | As above, `auth_failed` ("Invalid API key"), button `edit_provider` | Normal |
| `full` | `mbp` | All `ok`, one main-chat reply | Normal |
| `demo` | Usual three mock machines | All `ok` (assistant on `server`), other data unchanged, for "after getting started" screenshots | Normal |
| `unverified` | `mbp` | `model` unknown `model_unverified` (`mode` still `full`); `?probe=1` makes it `ok` | Normal |
| `launcher_missing` | `mbp`, launcher `cc_glm` gone | `claude` problem `launcher_missing` | 409 `launcher_missing` |
| `model_route` / `rate_limited` | `mbp` | `model` problem (`rate_limited` doesn't block, `mode` is `full`) | Normal |
| `node_outdated` | `mbp` node too old (`error_code: node_outdated`) | `computer` problem, no button | Normal (without configured model; with model real hub returns 409 `node_outdated`, screenshot via `MOCK_START_ERROR=node_outdated`) |
| `assistant_offline` | `mbp` | Readiness/warm/start all 503 `assistant_offline` | 503 |

Except `demo`, only `mbp`, no Agents or other assistant chats (new-user state). `MOCK_START_ERROR=<code>` overrides scenario and rejects every start
(`no_machine`, `machine_pending`, `claude_missing`, `launcher_missing`, `node_unreachable`, `node_outdated`, `assistant_offline`) for each error card;
`MOCK_BUILTIN_PRESETS=1` only adds built-in providers, `builtin_presets` and `launcher_auto_name`. `MOCK_PROVIDERS=1` also advertises `launcher_auto_name`: a custom provider added with no startup ID gets one by the hub's rule (#5029), and New Agent lists the managed providers with their display names. It also advertises `launcher_import_parse` (#5193): `POST /api/v1/launchers/import/parse` (answered in every mode, like the alias import) reads shell aliases and exports, `settings.json`, a `curl` line and a cindy `custom` provider with a small parser, matching drafts against the managed providers' addresses (`gw_full`'s fake key is `sk-example-gw`, so pasting it gives a same-key match and any other key a replace-key one), and the alias import takes `alias`; `scripts/test/web_provider_paste_e2e.py` drives both new ways in against a fresh mock. It also advertises `launcher_import_link` (#5420): the parse route reads provider import links (cindy's `cindy://provider/import` and the web's `#import=`) with the hub's link rules, refusing a bad one with one `link_*` note; `scripts/test/web_provider_link_e2e.py` opens a link, checks the address bar is clean and the wizard filled in, and copies a link from a provider. It also advertises `launcher_auth_auto` (#5329): a typed gateway's test and fetch take `auth` like the hub, the fake gateway accepting only `x-api-key` when its address contains `xapikey`, only Bearer when it contains `bearer-only`, and both otherwise (the test's `auto` answers the first accepted, Bearer then `x-api-key`; a method it does not accept gets the hub's 401 words). In these cases, `MOCK_PROVIDERS=1` Test Connection returns Anthropic's region-block message and `code: "region_blocked"` for keys containing `region` (#4482, text from hub). Like hub, at most 3 pending computers (fourth 429 `too_many_pending`).
`MOCK_INBOX_SOURCES=none` leaves no Inbox sources (no items/mail accounts, Lark unconfigured, GitHub signed out); combine `zero` for zero-computer/zero-source empty states (#4485, #4480). `mock_scenarios.py` names them `readiness-<scenario>` for live switching.
For the probe's client boundaries (#1892), `MOCK_READINESS_PROBE_DELAY=<seconds>` delays only `?probe=1`, and `MOCK_READINESS_PROBE_ERROR=<HTTP status>` refuses it with a localized reason.
Built-in preset wizard (#4197): with a scenario or `MOCK_BUILTIN_PRESETS=1`, Test requests carrying built-in `preset` return hub text according to words in the key (`bad` 401, `forbid` 403, `nopath` 404, `busy` 429, `slow` timeout, `region` region block).
Adding with `preset` follows hub naming (`deepseek`, duplicate `-2`) and display names; with `assign`, returns `assigned` / `kept`. `MOCK_ASSIGN_KEPT=assistant:customized,…` retains listed purposes for the supplied reasons.

Drift guard: `server/internal/assistant/readiness_scenarios_test.go` builds the same fake-hub scenarios and saves real readiness responses (three languages) and start refusals in
`testdata/readiness_scenarios/<scenario>.json`; `scripts/mock/test_readiness_mock.py` compares each mock response. After readiness/text changes: `UPDATE_GOLDEN=1 go test ./internal/assistant -run TestReadinessScenariosGolden`,
review diff, update `scripts/mock/readiness_mock.py`.

## herdr integration

`./scripts/test/test-herdr.sh` starts independent headless `herdr --session persona-test-<pid> server`, unrelated to active herdr,
stopped by name afterwards. `herdrlive` tests compare socket/CLI, events; launch claude in new temporary directory,
verify trust dialog detected (`dialog_open` refusal), option answers (rev check, keys from current screen), empty input allowed, draft refused
(`input_draft`), unrelated text not cleared, clearing seen draft allows send. Without local Go, cross-build test binary on hub/copy back.
Requires herdr/claude locally (claude starts but receives no message).

First-prompt image delivery has a separate opt-in real-Agent test (`server/internal/node/firstprompt_live_test.go`,
#4794). Start your own named `herdr --session persona-firstprompt-<unique> server`; set `HERDR_SESSION` to that
name and `HERDR_SOCKET_PATH` to its socket, never the active server's. Then, from `server/`, run:

```sh
PERSONA_LIVE_FIRST_PROMPT=1 PERSONA_LIVE_LAUNCHER=<launcher> \
  go test -tags herdrlive -run '^TestLiveFirstPrompt$' ./internal/node -v -timeout 6m
```

It starts disposable Claude Agents in temporary directories/profiles, with no Persona hooks or hub/node service
changes, and sends a synthetic 768×768 PNG plus multiline text through `firstPrompt`. A launcher's first Bypass
Permissions confirmation is accepted only in its disposable test profile. The normal case uses real
`agent.prompt`; the fault-injected case pastes into the real terminal but omits initial Enter and swallows one
recovery Enter. Both require exactly one user prompt and image in the transcript, a model reply, and an empty
input box. `PERSONA_LIVE_ARTIFACTS=<existing directory outside the repo>` keeps redacted ANSI captures and JSON
receipts. Stop the test server by its exact session name afterwards. A fault-injected pass proves recovery, not
that the normal case reproduced the original timing race.

### Throwaway hubs and herdr (#10114)

A throwaway hub (`personad hub run` under an isolated `HOME` / `PERSONA_HOME`) whose machine is `kind: local` starts a
node, and that node starts herdr's headless server under the test `HOME` when none answers there (`herdr_autostart`).
Give such a machine `"herdr_autostart": false` (and `"botmux": false`) unless the check needs herdr, as
`scripts/test/node_reload_lab.py` does. Since #10114 a stdio node stops, as it exits, a herdr server it started itself
that has no panes (`StopUnusedHerdr`, `server/internal/node/herdrstart.go`); a server with panes stays. A cleanup
still ends by checking `ps eww` for processes whose `HOME` is the test's, and stops any it finds through their own
socket (`HERDR_SOCKET_PATH=<test socket> herdr server stop`), never the user's herdr.

### Switching an ssh computer to Persona Link (#10662)

`scripts/test/link_switch_e2e.py --bin <dir> --scenario success|rollback|linger_off` runs a switch end to end, as in
[link.md](link.md) "Switching from SSH". `<dir>` holds linux/amd64 `personad` and `persona-link` of one version. Each run is
disposable:

- a throwaway hub and its manager under an isolated `HOME` / `PERSONA_HOME`, with TLS on the docker bridge address;
- a Postgres container;
- a systemd container as the computer, which runs `--privileged --cgroupns=private` because systemd needs a writable cgroup.
  Without that flag it exits at once.

The hub reaches the computer over ssh only through a wrapper with its own config, key and known_hosts, so the user's `~/.ssh` is
never read. The containers (`--prefix`), the network and the key are removed at the end.

- `rollback` cuts the computer's route to the hub (`ip route add prohibit`). It sits on a network of its own, so ssh replies still
  get through. The run checks the switch fails, the kind is ssh again, nothing it installed is left, and the computer answers over
  ssh.
- `linger_off` leaves linger off and expects a refusal at the check, with nothing changed on the computer.
- `--node-service` gives the computer its own resident node service first. A switch must leave it as it was, and so
  must a rollback.
- `hub_pause` is `rollback` with the hub's slot process stopped (SIGSTOP) right after its begin, for `--pause` seconds
  (150). Until the hub is running again, Persona Link must stay on the computer; the rollback must then finish and be
  confirmed.
- Every scenario checks the pairing secret is not in the hub's log or the manager's record.
- `--hold <seconds>` keeps it all up afterwards, for screenshots of the web app (`--web web/dist`).

## Codex integration

`./scripts/test/test-codex.sh` tests real Codex question/approval cards (`docs/integrations/codex.md` ("Questions and approvals")), independent `persona-codex-<pid>` herdr
(stopped by name), temporary git repo, same node `codexAnswer` code, checks rollout/screens per case:
async questions (empty input/draft/0.163 expanded panel/already answered on computer), approvals (direct/scrolled/other tab foreground/transcript pager
with denial reason/already answered), question then approval, two Plan questions (option/free text).

- `PERSONA_CODEX_BINS="codex /path/to/other/codex"` versions, e.g. temporary npm `@openai/codex@0.163.0`;
  `PERSONA_CODEX_ARGS="-m <model>"` usable authenticated model (ChatGPT login not all models); `PERSONA_CODEX_ROUNDS` default 2;
  `PERSONA_CODEX_COLS=64` also narrow-window runs (`stty cols`, headless splits don't narrow); `PERSONA_CODEX_CASES=a,b` selected cases.
- Uses own `~/.codex` login, real tiny model calls. Trust writes `~/.codex/config.toml`, rollouts in `~/.codex/sessions`;
  both cleaned on exit. No local Go: cross-build on hub/copy back.
- `PERSONA_CODEX_DAEMON_BINS="<0.163 codex>"` daemon route (`docs/integrations/codex.md` ("Sessions on the daemon")): tests own
  `codex app-server --listen unix://<temporary directory>/app.sock`, restartable child, never active daemon. TUI `--remote … -C <directory> -s read-only`,
  node discovery disabled, only this app-server, fake hub answers/withdraws cards. Async/blocking questions, command/edit/permission approvals, denial reasons,
  computer-first/phone-first/simultaneous answers, restart, draft send; first `PERSONA_CODEX_BINS` (e.g. 0.153) alongside confirms terminal route.
  Permission tests enable `features.request_permissions_tool` in test app-server.
- Phone start (`TestLiveCodexStart`, `docs/integrations/codex.md` ("Starting from the phone")): real `session.start`, first message quotes/newlines/`$`/backticks/Chinese/thousands of characters.
  Each daemon binary via `--remote` on own server (app-server route), same with `-c` (terminal route, Connection explanation), first BINS (0.153).
  Each round: reply ≤60 s, correct thread, exact rollout first message, one session/pane,
  correct route, second phone message received after first reply; missing-command launcher errors immediately. Needs local personad (`codex-start`), built by script.
  Don't run no-argument 0.163; it joins/starts active daemon. `PERSONA_CODEX_TESTS="cards daemon start"` selects, default all.
- Each round/case prints `LIVE <version> round <n> <case>: ok in <seconds>`, failure screen; final `RESULT all: passed|FAILED`.
  Codex model-service errors (e.g. Selected model is at capacity, not Persona) log `SERVICE-RETRY` and rerun once;
  each RESULT reports retry count—include in stability reports, not pretend absent.

## New-user copy guard (`scripts/test/test_onboarding_copy.py`, #287)

Onboarding copy (guide cards, main-chat errors, login, add computer, model wizard, empty states) must not mention hub.json, token (pairing code allowed), jq, personad, TLS, node, herdr,
launcher, localhost, `~/.` ([onboarding.md](../assistant/onboarding.md) 8.1). `SCOPES` at file top: web/hub i18n in three languages. Placeholders like `{name}` excluded.

- Legitimate advanced/details wording goes in `ALLOW`, with location/reason.
- Known issues being fixed go in `PENDING`, issue/design work item. Fixed entries trigger Remove from PENDING; no new violations added.
- Add new onboarding catalogs/key prefixes to `SCOPES`.

CI Scripts tests includes it; local `cd scripts/test && python3 -m unittest -v test_onboarding_copy`.

## Privacy scan

`python3 scripts/privacy_check.py`: repository contents/paths must not contain this machine's private values from `~/.config/persona/config.json`:
hosts, hub addresses, machine names, Apple IDs, emails; `~/.config/persona/private-words.txt` (one per line: internal projects/people/tools,
maintained locally); backtick values in `CLAUDE.local.md`. Also any private IPv4 (except documentation ranges like 192.0.2.0/24) and real-domain emails
(except `example.*`, public mailboxes, public `site/` contact). Reports type and origin only, never value:
`private value (5 chars) from config machines.id, config machines.ssh`, `from private-words.txt`, `from CLAUDE.local.md`, `from the home directory`;
a key that is not a snake_case word or is itself a private value shows as `*` (`config app_store.provisioning_profiles.*`).
Staged and range scans read the files' Git objects in one `git cat-file --batch`, NUL-separated with `-z`; Git before 2.36 has no `-z` there,
and the same requests then go one per line (#3224).
**Ordinary-word config values** (#1065): a machine id like a room's name matches every variable, comment and environment variable of that name
(`_` is a word boundary), blocking whole PRs on that machine. If such a value names nobody, add a `!<word>` line to `private-words.txt`
(case-insensitive); the hint after the hits appears only when one of them can be taken back this way. The waiver goes by value, so it also covers
an ssh alias or URL host spelled the same; it only takes back a config value made of ASCII letters. Anything with a dot, digit, hyphen, `@`, `/` or space
(hosts with a domain, addresses, emails, paths, ids), a word listed plainly in either `private-words.txt`, and the home directory stay private.
A `!` line that is not ASCII letters only is searched as written, with a warning naming its line, not its value. The waiver lives where the values are
read, so every scan that takes them from `privacy_check.py` honours it (the hooks, `merge-pr.sh`, `scripts/release/check_bundle_privacy.py`).
**App identifiers** (#9461): a reverse-DNS value held only under a `bundle_id`, `application_id` or `app_group` key (`app.bundle_id`, `app.watch_widget_bundle_id`,
`android.application_id`, `app.app_group`, …) matches as a whole identifier, not continued by a letter, digit, `_` or `-`. Everything derived from it
with dots (`group.<id>`, `<id>.watchkitapp.widgets`, `<TEAM ID>.<id>`) still matches; a longer identifier that only starts the same way (`<id>d`,
`<id>-node`, a public default committed on purpose) does not, where a substring match failed every whole-repo scan on that machine. The same value
also held under another key, or listed in `private-words.txt`, keeps the substring match; `scripts/release/check_bundle_privacy.py` keeps its own matching.
**Names blocked on every machine** (#4967): specific vendor and internal-repository names never enter the repo, in any spelling (case, hyphens, underscores, spaces, camel case). Rules live in `BANNED_NAMES` in the script, their only location in the repo.
CI enforces them without private config. Needed capabilities belong in extensions that supply their own names; the main repo uses generic wording ("relay", "extension-provided IM connector"). Test examples concatenate pieces to avoid matching themselves.
A word listed only lowercase/uppercase/titlecase isn't matched across an internal lowercase→uppercase camel boundary (`fooBar`): ordinary-word identifiers (tokens/SF Symbols) can accidentally contain sensitive words, falsely blocking repo scans/web `prebuild` (#3819). To block camelcase too, list a boundary-bearing form (`FooBar`).
**Only tracked files** (`git ls-files`): `git add` new files first or they're missed (2026-11-05, a new design doc's machine id escaped to PR). Every mode reads what Git records: a symlink is its link text, never the file it points to; `--staged` and `--range` include type changes (a file turned into a symlink); a tracked file the worktree lacks (sparse checkout, skip-worktree, deleted but not staged) is read from the index.
Not private: loopback (`127.0.0.0/8`, `::1`, `localhost`, including protocol/ports, even configured, e.g. `http://127.0.0.1:<port>` or database `127.0.0.1:<port>`; private addresses with ports still reported), documentation/IETF protocol reserved ranges (`192.0.0.0/24`, SDK uses `192.0.0.192` as cloud metadata), defaults already in `config.example.json` (e.g. external group), repo Go module path (GitHub username included),
backtick paths/commands/config keys in `CLAUDE.local.md` (only ID/host/email/IP-like values count), and `site/` contact/site domain
(`app_store.feedback_email`, `privacy_url`, already public for App Store/OAuth). Docs say hub machine/another Mac;
example machine ids `server` / `laptop`. `release.sh` scans before packaging;
precommit hooks: `git config core.hooksPath scripts/hooks` (`pre-commit` staged content, plus `commit-msg` commit message, `pre-push` changed files/messages against the first of `origin/HEAD`, `origin/main`, `origin/master` that shares history with the pushed commit, every file and message of an orphan branch; `pre-push` also checks the trusted Deno adapter, see "Trusted Deno adapter", and refuses a branch under `artifacts/` or `pr-assets/`, see `docs/operations/collaboration.md`, "Screenshots in PRs").
**Message lines** (#1107): `--message <file>` checks every line, Markdown headings included (a PR's title and body become the squash commit's message as they are).
`commit-msg` passes `--commit-msg`, which checks only what `git commit` records: when git opened an editor it skips the comment lines (hints, branch, paths) and everything from the scissors line down (the `commit -v` diff);
a `-m` / `-F` message keeps its `#` lines, so they are checked. It follows git's comment string and `commit.cleanup` (`commit_msg_lines`), but cannot see `--cleanup` or `-v` on the command line; `pre-push` still reads every recorded line.
**Block again before merge** (#3696): `scripts/hooks/merge-pr.sh <PR number>` reads the PR once (`gh pr view --json`), fetches its base and head, runs `privacy_check.py --range <fetched base sha>..<head> --message <file>` on the squash message (`<title> (#N)`, blank line, body)
and `scripts/ci/changed_ratchets.py` on the files the PR changes ("Hosted CI" above), then only passes to `gh pr merge --squash --match-head-commit <head> --subject <title> (#N) --body-file <body>` (exact scanned head and message merge; an edit after the scan does not reach `main`) and deletes a same-repository branch through the API. `--range <base>..<head>` checks changed head-file contents and commit messages, not worktree, in seconds. Tests: `scripts/test/test_merge_pr_sh.py` (fake `gh`).
Why not CI: private values come from local configs unavailable to CI, which only checks generic private-IP/email/npm rules; merge machine must possess values to block them.
2026-11-05–06 six scheduled releases failed initial repo scan because machine-name docs had already been merged directly with `gh pr merge`.

### npm registry guard

Parse full JSON in every `package-lock.json` / `npm-shrinkwrap.json`, checking v1 nested `dependencies` and v2/v3
`packages` `resolved` entries. Remote downloads only official public npm (`https://registry.npmjs.org`, default port/443);
reject other hosts, HTTP, credentials, query/fragment, control characters, backslashes. Valid relative `file:` dependencies/workspace links allowed;
absolute paths, remote authorities, encoded disguised remote paths rejected. Broken JSON, duplicate keys, nonstring `resolved` error; NUL can't make lockfiles skipped as binary.
Independent of local config/private words, reports file/line/type only, never URL. `.invalid` placeholders in rejection tests, no real internal domains.

- Web manual: `cd web && npm run check:privacy`.
- Build `prebuild` automatically scans repo, used for web deployment/public publishing.
- External repository refs: `python3 -B scripts/test/test_external_service_refs.py` (CI privacy job too). Tracked text cannot contain `nwverse/<other repo>` (including `#n`/links) or placeholder paths into another checkout's integration or test-fixture directories. External services connected through APIs only reference public contracts (OpenAPI/docs/error codes). Public dependencies in `ALLOWED_REPOS` with reasons; organization alone (`"publisher": "nwverse"`) doesn't count.
- Python regressions: `python3 -B -m unittest discover -s scripts/release -p 'test*privacy*.py' && python3 -B -m unittest discover -s scripts/test -p 'test_privacy*.py'`; not in `npm test`.
  Includes public Go dependency constants/classification, verification/replace/tool-failure refusal, personal values not hidden by public constants.
  Isolated HOME/Git-config temporary repos test real hooks/staged-worktree differences, never main worktree index/history.
- hooksPath is clone-local; new clones must run `git config`; files existing doesn't mean enabled.

## Trusted Deno adapter

`server/internal/extension/denohost/adapter.mjs` is generated from `adapter.ts`, which bundles `sdk/ts/src`, and is
embedded in `personad`; `TestTrustedAdapterMatchesOfficialSDK` fails while the committed one is not what the sources
build. It went stale on `main` five times (#2171, #2416, #4217, #5862, #5971), each after an `sdk/ts/src` change pushed
without Bun. Two things keep it current (#6001):

- **No identifier minification.** `scripts/gen/deno_adapter.sh` and the test build with `--minify-syntax
  --minify-whitespace`, not `--minify`. Bun's identifier minifier renamed symbols whenever the SDK's set of symbols
  changed, so a type-only edit (#5834, #5934) changed the adapter's bytes but not its code. Now only a change to code
  the adapter bundles changes it (24.9 KB instead of 19.0 KB).
- **`pre-push` checks it.** When the pushed commits change `sdk/ts/src`, `denohost/adapter.ts`, `adapter.mjs` or the
  generator (compared with the same base as the privacy scan; an orphan branch is skipped),
  `scripts/hooks/check-deno-adapter.sh <commit>` builds the adapter from that commit's files, never the worktree, with
  that commit's generator, and compares it with the committed one. A mismatch refuses the push and names the
  regenerate command.
- **Only the pinned Bun counts**: the version `.github/workflows/server-test.yml` pins (1.4.2). Without identifier
  minification another version builds other bytes (1.4.0 renames a shadowed `out` to `out2`). The check uses
  `~/.bun-<version>/node_modules/.bin/bun` or `~/.bun-<version>/bin/bun` when there, else `bun` on PATH.
  **Without Bun it refuses**, and so does another version whose build differs; both name the pinned version and an
  install that leaves the global Bun alone: `npm install --prefix ~/.bun-1.4.2 bun@1.4.2`.
- Fix a refusal: `PATH=~/.bun-1.4.2/node_modules/.bin:$PATH scripts/gen/deno_adapter.sh`, commit `adapter.mjs`, push
  again.
- By hand: `scripts/hooks/check-deno-adapter.sh HEAD`. Tests: `scripts/test/test_check_deno_adapter_sh.py` (a fake
  `bun`, no real Bun needed). `git push --no-verify` skips every hook, the privacy scan included; don't.
- CI does not run this check. `merge-pr.sh` does not either; the test on the hub machine still catches what a push
  without the hooks lets through.

## Push replay (`TestPushReplay`)

Compare same real pushes before/after hub session-list/Live Activity timing changes:

1. Record on hub: `cd server && go run ./cmd/pushrec 20m ~/push.jsonl`. App-like hub connection (token from `~/.persona/hub.json`, never printed; `listen` address, `PERSONA_LISTEN` / `PERSONA_LISTEN` override),
   no phone/desktop registration or chat opening, `{"t": milliseconds, "msg": original}` per message. Contains real session titles/last lines, 0600, **stays on that machine**, never copied/committed.
2. Replay `PERSONA_PUSH_RECORDING=~/push.jsonl go test ./internal/hub -run TestPushReplay -v`: before-recorded/after-generated
   counts/bytes per minute; timestamped Live Activity changes pass through coalescing/throttling, report notifications/hour with App closed and longest island/hub mismatch, fail beyond limits.
3. Real `broadcastSessions` decides sends, matches byte-comparison results (decode retains fields); parallel incremental App (`sessions_delta`) counted.
4. Background `AgentSync` fetches: `python3 scripts/diag/agentsync_replay.py ~/push.jsonl`, recorded rev/state (45/min→9/min).

19-minute recording 2026-11-02 (battery investigation, `docs/server/server.md` ("Traffic pushed to the app")):

| | Before | After |
| --- | --- | --- |
| `sessions` (old App, whole list, unchanged skipped) | 36.9/min, 2605 KB/min | 29.7, 2452 KB |
| `sessions` (new App, incremental) | Same | 29.7, 79 KB |
| All messages (new App) | 104.9/min, 2616 KB/min | 97.7, 90 KB |
| Connection session snapshot | 602 KB | 227 KB (no Codex history) |
| Live Activity (App closed) | 188/hour, mismatch max 2.5 s | 115, max 29.2 s (60-second throttle: 60, 58.8 s) |
| Live Activity (App open) | Same | 0 |

## Session list reads (#3759)

How often a client reads `GET /api/v1/sessions`, and what kind of read, while it sits open on a paging hub.

- The mock in paging mode with a list about the size of a busy hub: `MOCK_SESSIONS_LIST=1 MOCK_AGENTS=2 PORT=<port> python3 scripts/mock/mock_hub.py`
  (266 sessions, a 207 KB complete list). Its `since` answers from a journal of the last 128 revisions (changed, added and
  removed rows, rows entering or leaving a filter; an older one is `409 revision_expired`), and each `sessions_list` hint
  carries the list's current revision, once per change. `MOCK_SUMMARY_STRICT=1` writes `summary=1` rows as the hub's
  `runtimeSummary` does (no `background`, `subagents` or `workflows`, only their counts and `workflow_progress`; #5888);
  without it the mock's summary rows keep the complete arrays.
- `GET /mock/sessions_reads` counts the reads by client (the `X-Persona-Client` platform: `web`, `ios-app`) and kind: `full`
  (no paging query), `baseline` (a page without `since`), `since`, `counts`, `lookup`, with bytes, plus the hints sent;
  `?reset=1` starts again.
- `GET /mock/churn?n=120&ms=500&rows=1`: a minute in which one working agent changes every half second (a real hub changes a
  row or two a second); without `rows` every working agent changes every round.
- Web: `node web/e2e/session-list-reads.mjs <web URL> --hub <mock URL> [--max-full 0]` (the mock's default pins keep the
  sidebar on its complete fallback): a quiet minute, then a churn minute; fails when the churn minute has more than
  `--max-full` complete lists.
- A real hub's `traffic` journal lines count by route template, not query: there a complete list and a delta are told
  apart only by `body_bytes / count`.

## Fleet soak (`server/internal/fleetsoak`, #3197)

What a fleet's churn costs the hub and its manager, per minute: bytes and objects allocated, CPU time, live heap.
For a change that claims to make the manager or the hub cheaper under load, run it on the base and on the change and
put both in the PR.

```sh
cd server
go run ./internal/fleetsoak -label head -minutes 20 -out <directory>
go run ./internal/fleetsoak -label base -minutes 20 -out <directory> -tree <a worktree of the base commit>/server
go run ./internal/fleetsoak -compare <base directory>/report.json <head directory>/report.json
```

- It builds `personad` from `-tree` and runs a throwaway hub with its manager: a home directory of its own under
  `/tmp`, a free loopback port, and a `persona-ci-db-fleetsoak-<pid>` Postgres 18 container, all removed when it ends
  (it needs Docker and the `postgres:18-alpine` image). Every process gets an environment made by the soak, so the
  machine's own installation is neither read nor written.
- The computers are played by the soak itself: it puts itself on the manager's `PATH` as `ssh`, and started that way
  it is the node of the host asked for (`node.go`): 10 computers with 50 sessions each by default, one of them starting
  or ending a turn every 2 s on each computer (`-machines`, `-sessions`, `-interval`). 20 WebSocket connections play
  apps left open (`-clients`). Nodes answer every other call with `{}`: transcripts, screens and cards are not played.
- The measured programs are the ones that ship, plus a sampler laid over `cmd/personad` at build time
  (`go build -overlay`, `sampler.go`): every five seconds it appends the runtime's allocation and GC counters and the
  process's CPU time to `<out>/stats/<role>-<pid>.jsonl`. The window starts after `-warmup` (a minute) and is cut into
  whole minutes; steady numbers have their min and max close to the median.
- The manager refreshes the official market first, as a hub in use has it (the report says how many catalog entries
  it got; without the network the catalog is empty and the channel list cheaper than in use).
- `<out>` also has `report.md` and `report.json`, the hub's log, CPU and allocation profiles of the window
  (`stats/*.pprof`) and what `go tool pprof -top` says of them (`manager-alloc-cum.txt`, `manager-cpu-cum.txt`, the
  hub's beside them).
- Allocation per minute repeats within about a percent from run to run, so base and head can run one after the
  other. CPU time cannot be compared that way: it follows what else the machine does and which cores the processes
  get, by a factor of two on a laptop. For CPU, run base and head at the same time (each in its own terminal, they
  share nothing): every minute is then the same machine for both, and `-compare` adds the head's CPU time over the
  base's in the same minute, for the manager and for the hub. When the change leaves the hub's work as it was, the
  hub's row shows how much is noise.
- It ends with one line: `PASS`, `FAIL` or `TIMEOUT`.

## A relayed upload's memory (`scripts/diag/relay_body_rss.py`, #3199)

What a large request body costs the hub's slot and the manager it is relayed to, and how long they keep it.

- `python3 scripts/diag/relay_body_rss.py --binary base=<personad built from main> --binary head=<personad built from the branch> --rounds 3`
  starts, for every run, a throwaway hub (`<binary> hub run`: edge, slot and built-in manager) on a free loopback port,
  with its own directory under `/tmp` as `HOME` and `PERSONA_HOME` and its own database in a `postgres:18-alpine`
  container named `persona-ci-db-relay-rss-<random>`; it removes all of them at the end. It never reads the real
  `~/.persona`, whatever the caller's environment says. Docker is the only thing it needs besides the binaries.
  With `--database-url <server>` it uses that Postgres server instead of a container (each run gets a database of
  its own there when `psql` is installed): that is how it runs inside a Linux container, next to a Postgres one
  (`docker run --network container:<postgres container> … python3 relay_body_rss.py --database-url …`, with
  linux binaries mounted in).
- Each run posts a 22.4 MB JSON body (16 MiB as one image's base64, `--body-mib`) to
  `POST /api/v1/assistant/conversations` for a machine that does not exist: the hub relays the whole body, the manager
  decodes the whole body and answers 400, and no conversation starts. It reads each process's RSS just before the
  post, at its peak, 10 s and `--settle` (60) seconds after the answer. Binaries alternate from round to round; the
  tables give the median and the range. `--posts 2 --gap 5` sends a second body inside the trim's 30 s window,
  `--chunked` one without a Content-Length, `--env GODEBUG=gctrace=1 --logs <dir>` keeps each hub's log with its
  collections.
- On Linux the peak is the kernel's (`VmHWM`). On macOS it is the highest of the samples (every 50 ms), and the
  script also prints the physical footprint: when the Mac is short of memory it compresses the pages of an idle
  process, RSS falls though the process gave nothing back, and only the footprint still counts them. Compare builds
  by the footprint there.
- Since #6574 the hub sends a body of 1 MiB or more to a manager that names `http_body_stream` on a relay stream
  of its own, so this post does not go through the bus's line any more. To measure the line (what a manager that
  takes no body stream gets, and every other line of megabytes: a large answer, a file's 1 MiB chunks to and from a
  computer), build both trees once more with `assistantwire.CapabilityHTTPBodyStream` left out of the hello in
  `server/internal/assistant/bus.go`, and compare those builds: the hub then sends the body inline, as
  `http.request`.
- The micro benchmarks behind it: `go test -run '^$' -bench 'Relay|ReadRelayed' -benchmem ./internal/hub`,
  `-bench 'Decode(22MB|1K)' ./internal/assistant`, `-bench 'CallLarge|Serve(Large|Lines)' ./internal/rpc`
  (`ServeLarge`: the reading side of a 30 MB line; `ServeLines`: streams of lines from 200 bytes to 4 MiB); compare
  two builds' test binaries in alternating rounds with `benchstat`.

## Hub transcription live check

Hub transcription against a real service, for what
the tests' scripted contribution cannot tell: how well the words come back, how soon, and what it costs.
server/internal/transcribelive plays the phone against a running test hub whose voice extension is set up with a
GPT-Live-compatible upstream (the contribution chosen for transcription, `meeting_transcription_ready` true; it also
reads the meeting aloud through that extension's speech). Run it on the hub machine, from `server/`:

```sh
cd server
go run ./internal/transcribelive -hub <hub address> -token-file <file> -kb <PERSONA_HOME>/data -lang zh-CN -out <directory>
```

- Without `-hub` and `-token-file` it takes `public_url` and `token` from `$PERSONA_HOME/hub.json` (`-kb` defaults to
  `$PERSONA_HOME/data`, where it reads the meeting's files). Never point it at a hub in use: it also sets the hub's
  `meeting_glossary` to the script's terms.
- The meeting is made up (server/internal/transcribelive/testdata/mixed_meeting.json): a third Chinese, a third
  English, a third both in one sentence, and one line said by "Persona". The hub reads each line aloud
  (`POST /api/v1/extensions/voice/speak`, `-voices F=<id>,M=<id>,E=<id>,P=<id>` picks the voices); the audio and the
  script with each line's times go to `<out>/meeting.pcm` and `<out>/script.json`, which a later run reuses
  (`-synth-only` stops there).
- It uploads the meeting in hub mode, marked private, with the room in `-role` (`listener` by default), streams the
  audio over the audio socket in real time (100 ms frames, `speaking` on and off around Persona's line, an upload with
  the timeline every 15 seconds), sends `end` and the `complete: true` upload, then waits for the final pass and the
  minutes. At the end it deletes the recording (`-keep` keeps it).
- It prints `<out>/report.json`: the live lines and the final transcript against the script (character error rate per
  line and in all, the Latin words kept as said); the latency of each `line` frame after its sentence ended, of the
  first caption after each sentence began, of the final pass after the complete upload and of the minutes after it;
  Persona's echo (marked in the hub's transcript, never a live line); the minutes' share of Han letters against
  `-lang`; the upstream's billed seconds (`-price-per-minute` adds the cost) and whether the hub deleted the audio.
  `<out>/frames.json` has every socket frame with when it came, `<out>/detail.json` the recording as the API returns it.

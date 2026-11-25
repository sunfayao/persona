# Lessons: testing and validation

- A manager route's response contract is also asserted by hub relay tests (#13234). When making a closed plan's
  dismiss response idempotent, update both the manager's route test and the hub's end-to-end plan lifecycle test;
  verify the returned state and persisted state, rather than only replacing the expected HTTP status.

- Execute release collection steps against the actual build-directory layout (#11684). The installer is rendered at
  `build-publish/install.sh`, while archives and indexes live in `build-publish/bin/`. A receipt that only hashes files
  that happened to be copied cannot detect an omitted installer. Stop on failed copies, require bootstrap files before
  binding the CLI/Web receipt, and test missing outputs as well as a complete build.

- New relayed routes need both extension-scope inventories checked (#7100): `TestRouteScopes` records the exact
  scope, and `TestEveryExtensionRouteIsBounded` records how ownership is enforced. Updating `extwire.RouteScopes`
  alone leaves those checks failing. Exercise allowed and refused tokens, another manager's Todo and explicit manager
  selectors, and body-named machines before recording the classification; keep every existing scope unchanged.
  Regenerate the route inventory after merging additions so shared-prefix routes remain sorted.

- A viewer's Close button existing, or being non-hittable, does not prove that its picture and tap recognizers are ready, or that a tap hid the chrome (#8562). The full-screen presentation may still be settling, the decoded page may still be a spinner, or a SpringBoard alert may cover the app. Dismiss the known icon-change alert, wait for the decoded `viewer-page-N` and a hittable Close before the first tap, and only then assert the hide/show sequence. A delayed-decode launch turns the old race into a deterministic failure; a longer poll after a mistimed tap only hides the synchronization gap.

- Fixture teardown is part of a test's elapsed time (#5730). A scripted terminal whose shell is ready immediately
  does not need a fixed startup pause. When a test has finished its assertions, cancel background prompt checks
  owned by that fixture and join them before restoring package settings or closing sockets. Delivery/recovery tests
  must still await the check before their assertions; cancellation at teardown must not substitute for those checks.
  Keep normal detached delivery and production wait limits unchanged, and measure serial and partitioned commands
  separately when evaluating the gain.

- A fake peer recording a user line is not the send's completion (#9673). The desktop launcher's first message
  continues in the background and still reads the shared confirmation timeout. After observing inbox receipt, wait
  for that session's existing input lock before restoring the test's package globals; keep the drain on assertion
  failure paths too. A fixed pause gives the race another schedule, not a happens-before relationship.

- A transport fixture that already reads every reply should wake its request waiter from that reader (#5730).
  Register by request ID before sending, retain JSON-RPC errors, remove waiters on cancellation/EOF/write failure,
  and keep duplicate or late replies from blocking the reader. Polling an already available approval response
  added about 23 seconds to the complete shell-name security matrix. Keep every classification assertion and
  verify the transport with real pipes and held approvals; virtual time can expose an unnecessary polling tick.

- A counter of a shared resource (lists, calls, reads) must not include what a fixture's own background passes do
  (#6121). `TestIdlePassesReadWhatRuns` counted `closeDoneTaskAgents`'s machine lists, but `liveLoop`'s signal-driven
  pass (a 100 ms coalescing window) listed the same machine from the fixture's earlier calls, so the count was 0 or 1
  depending on the race (32 failures in 200 runs). Settle the counter before the measurement: wait until no list has
  happened for a quiet window longer than the background pass's coalescing delay, then count only what the call under
  test adds. Never fix this class with a plain `time.Sleep` before the call — that only moves the race.

- A test that calls an entry point which sets the process environment must `t.Setenv` that variable first, so the
  test puts it back (#4581). `nodeclient.RunHook` with `--persona-home <dir>` sets `PERSONA_HOME`; one test left it
  pointing at its removed `t.TempDir()`, and every later `internal/node` test put `node.sock` there. On macOS that path
  passed the 104-byte Unix socket limit, so `TestStandbyOnTheSocket` failed with `bind: invalid argument` only in
  full-package runs, and a test that refuses to dial anything outside its own home skipped silently. Linux's shorter
  `/tmp` paths still bind: reproduce such leaks on macOS by running the two tests together, in order.

- Async node-start fixtures must cancel and join `n.starts` before closing fake terminal sockets, restoring globals
  or removing their temporary files (#3345). Waiting only for `pane.send_input` leaves a background start polling a
  dead host for three minutes, with its diagnostics appearing under a later test. Expected start failures use the
  node's logger, so tests can capture diagnostics without changing production logging.

- An async status fixture must carry the operation job that the real status endpoint reports (#5363). `iphoneMac`
  returned a job from `iphone.pair` but never from `iphone.status`, so a later refresh could erase the just-started
  job; `go test -count=1000 -run '^TestIPhoneJobs$' ./internal/extmgr` reproduced it in about a second. Keep the
  fixture's status/job state consistent with `node/iphoneStatus`, and assert the post-pair status directly rather than
  adding a wait or changing production synchronization.

- Bulk Postgres fixtures need physical isolation, not just row deletion (#3743). Deleting 500 feed rows left a larger
  unsent B-tree than the sent index; a controlled statistics refresh then changed a later index-choice assertion even
  though no fixture rows remained. Use the existing migrated-database clone for bulk fixtures so table, index and
  catalog state stay private. Preserve the original planner assertion; an isolated passing test alone does not prove
  that the complete package is unaffected. The controlled reproducer shows the mechanism, not which background
  statistics event happened in a particular failed run.

- An index-choice assertion needs a table of its own, shaped like production (#4191). On the few rows earlier tests
  leave behind, with `enable_seqscan = off`, two indexes cost about the same, and the leftovers decide which one wins:
  `TestMarkFeedSentUsesUnsentIndex` passed alone and failed from the third `-count=10` iteration on, before and after
  #3787. Check the plan in a migrated clone, on a few thousand synthetic rows, after `ANALYZE` and at default costs.
  Check the generic plan too (`PREPARE` + `EXPLAIN EXECUTE` under `plan_cache_mode`), since cached statements can
  switch to it. Then prove the test by mutation: drop the index, or the query's matching predicate, and it must fail.

- Sync replay must wait for its initial session, built transcript and pending-card snapshot before issuing user actions
  (#1112). Fixed startup sleeps can turn a tap on an unloaded card into a silent no-op; accepting a reply with no held
  request then falsely labels the later card snapshot as a resurrection. Require an actual request before delivering
  its reply and test a delayed initial snapshot. Streaming projection must pass the active turn and whether live text
  is drawn, as the real page does. A wake opens an exchange of its own (#9140): the answer before it stays in place
  while the woken work runs, streams and ends, and the woken work has its own summary.
  An idle or other-turn presentation still shows the last answer. Cover the ordered shape, manual inspection,
  serialized reload and other turn IDs separately. When a merge retains the maintained renderer, reconcile tests
  imported from a competing wake model instead of letting their old expectations reintroduce it (#3169).

- A second failed start inside the automatic launcher-health throttle does not schedule another probe (#2600). A retry fixture that models recovery must invoke the supported explicit health check and observe its passing record; polling the record alone cannot create recovery. Keep the production throttle, terminal-only failure rules, and retry assertions unchanged.

- On Linux, replacing a running executable requires a new file and rename; direct writes fail with `ETXTBSY`. Use a ready handshake and reap the fixture process to test the real overlap.

- Run standalone release-worker tests on the oldest supported Python, including receipt verification and verified-artifact retries (#1077). Python 3.9 has no `hashlib.file_digest`; bounded reads into `hashlib.sha256` keep hashing portable without loading an APK into memory. Cover empty files and binary inputs spanning multiple chunks.

Pitfalls, odd behavior, and the tradeoffs made at the time (originally in `CLAUDE.md`; index in `docs/lessons/README.md`). Add new lessons to the relevant section, or start a section if none fits.

- Fixed `time.Sleep` calls in `_test.go` files under `server/` and `sdk/go/` are ratcheted by `scripts/ci/sleep_ratchet.py` (baseline `scripts/ci/sleep_baseline.json`): a file's count may stay or fall, never rise, a new file starts at zero, and `// sleep-ok: <reason>` on the call's line is the only escape. Prefer `testing/synctest`, a polling helper or a channel. Taking a sleep out does not need the baseline lowered in the same change: only a rise fails, and with a base a changed file may not rise above its count at the merge base either (`docs/operations/testing.md`, "Count ratchets"). When the strict direction failed a fall too, a forgotten lowering broke `main` again and again (#2761, #5635), and baseline edits were the commonest conflict between parallel PRs.

- To keep a fake hub answer out (a press past `cardAnswerWait`), block it on a channel the test closes, not a fixed sleep (2026-11-11, #4274). The assistant's `fakeHub` runs app answers under its lock, so a held answer blocks every other fake call: close the channel before the test touches the hub again, and register the close with `t.Cleanup` so a failed assertion cannot leave it blocked (`heldAnswer` in `server/internal/assistant/feishu_answer_ended_test.go`). Then wait for the last effect of the late path (its note), not for a fixed time.

- `synctest.Wait` is not a general goroutine barrier (#1836): mutex contention and real socket I/O, including an `httptest` server's accept loop, do not count as durably blocked. A test waiting for quiescence can hang even though its target operation is waiting as intended. For these integration tests, use explicit entered/release/completed channels and bounded waits; reserve `synctest` for fixtures whose blocking operations it can track.

- A count that must not grow after a call waits for the pushes the call set off, not for a fixed while (2026-11-14, #7419). The manager hands each push to the hub on a goroutine (`deliver`), and a card's push first waits up to `linkRangWait` for Persona Link on another (`notifyInteraction`), so a count read when the call returns misses them, and a 50 ms sleep is both a guess at the scheduler and shorter than that wait, and waiting for the count to reach the expected number stops at the first push and passes over a later one (a mutant that pushed the card again on detach passed 33 times in 40 that way). The ask rig hangs in a `synctest` bubble (its fake model is an `httptest` server, as in #1836 above), so `pushesUnderWay` in `server/internal/assistant/ask_release_test.go`, taken just before the call, reads the goroutine list for the goroutines those two functions started and the ones the calling goroutine started since: without the latter, a mutant that pushed from a goroutine of its own passed 28 times in 100. A "created by" line names the function with the go statement, which the compiler never inlines, and the two are named through method expressions, so a rename fails to compile instead of counting nothing. A push armed on a timer is not a goroutine yet and is not seen.

- Test authoring realm comparisons as a matrix, not just default-export shape (#1200). `structuredClone`, Response JSON, iterators, streams and WebCrypto can return host objects during initialization; compare Object/Array, containers, regexps, dates and binary families in both native ESM and the probe. Preserve cycles, transfers, real subclass negatives and native null-prototype records. State the supported `instanceof` contract separately from exact cross-realm prototype identity. EventTarget listener methods must be synchronous and return undefined: observe nonconforming ordinary promise/thenable rejections before returning OPTIONS, and keep secondary cleanup errors alongside the primary failure. If caller constructor/species machinery prevents handler installation, report the observation error and attempt owned-worker cleanup, but do not claim that the original rejection was handled or alter caller promises/global rejection policy. Ready must precede `module.evaluate()`, which can block synchronously forever; recorded SIGINT/SIGTERM exit priority survives a late or failed cleanup without inventing reaping evidence.

- Keep the source authoring suite inside its portable budget with bounded parallelism, not a larger timeout or retries (#1200). Run only cases that neither stall the parent event loop on purpose nor count process-wide resources together, give each owned worker or child one slot sized from `availableParallelism()`, and spawn children asynchronously: a `spawnSync` in one case stalls the parent loop and can push another case's library result past its absolute deadline.
- A source authoring verdict must match SDK property lookup, not just own descriptors (#1200). Inspect inherited data members without invoking accessors and allow legitimate class/prototype Programs. Reread the live default export and its members after the observation window. Shared host Web APIs need matching binary-data and WebCrypto constructors for ordinary Node initialization semantics. Recheck an absolute monotonic deadline when admitting every result, including after worker cleanup; an event-loop stall can deliver an over-budget success before the timer callback. Convert worker setup/permission denial into structured library failure without granting the denied authority. Signal tests wait for a real handler-ready IPC handshake, including under startup stalls, never a fixed delay or extra stdout lines.

- Source authoring must distinguish host intrinsic prototypes from user prototypes (#1200). Snapshot both intrinsic Object.prototype descriptor baselines before evaluation; inspect added/changed contributions while excluding unchanged native members and preserving nearer data shadowing. Web APIs return host-realm plain objects, so inspect that realm too and still follow explicit SDK member lookup. Exempt only a prototype's own `constructor` back-reference to its class or function: a hidden inherited `constructor` is a member the SDK dispatches (`h is not a function`). Reject directly recognizable class constructors in handler slots without invoking them or claiming to predict opaque bound/proxy behavior. Use a curated, version-gated Web API list with constructors matching native instances, including Performance and CustomEvent. Direct literal global-loader destructuring is the same policy as a literal property read, not arbitrary alias analysis. Canonicalize the tool entry for npm/symlink invocation without permitting input links. Accept canonical disabled capabilities (`http: false`), not enabled or malformed ones. Validate structural AbortSignals and option getters before creating a worker; listener registration/removal failures must produce a structured failure and never bypass termination. Test real library calls under strict unhandled-rejection handling and compare owned-worker/MessagePort counts to the baseline.

- Offline source authoring is not production admission (#1200). Keep parsing separate from an explicit trusted-code probe; VM/worker heap guards are not OS security or hard RSS bounds. A successful module with unclosed MessageChannel ports or intervals must not hang the caller: emit the verdict only after terminating and waiting for its owned worker. Keep the control port referenced while top-level await is pending so Node cannot abandon evaluation before the external deadline. Observe initialization rejections before success, test real SIGINT/SIGTERM and pipe EOF with a neighboring sentinel, and do not claim verification of arbitrary future handler errors or real confined-Deno acceptance.

- Transport fixtures must not repair the protocol they test (#1227). A registry fixture that silently rewrites a malformed manifest route can validate an incorrect OCI client. Preserve the exact request path and the real registry response. Separate the client's direct-source requests from a mirror's own upstream fetches: only the former prove fallback. Record observed response bytes and hashes, not copies of the expected metadata, and require uncached download evidence. A negative case must reach its intended injected fault and leave no installed extension or contributions; an unrelated preview error is not a pass. A positive case requires explicit enabled, installed-source and running states in both detail and list responses, plus the expected contributions. Fixture self-checks, HTTP 200 and a `starting` state are preparation, not installation acceptance.
- Mock mutation responses must use the same session decoration and manager ownership as HTTP lists and WebSocket deltas (#1124). An incoming reply returned the undecorated ownership envelope while its delta included `manager`; reuse `stamp_row(decorated_session(...))` rather than weakening full-session equality assertions.

- Run release privacy preflight with the actual selected build config (#1245): a scan without `PERSONA_CONFIG` can miss account-specific inputs. Generic provisioning profile labels already published in the default mapping or deployment fixture are not private merely because an account selects them. Classify only those exact literal labels under `app_store.provisioning_profiles`, reading the already published `origin/main` definitions rather than allowing a working-tree or unpublished commit to approve its own new label; keep custom/near-match labels, values under another key and explicit `private-words.txt` entries protected. Parse historical fixture data without executing it, and never work around the failure by dropping signing overrides, renaming registered resources or disabling the source/bundle privacy gates. Compound release commands use `set -euo pipefail` and stop on a rejected preflight before commit, push, archive or upload.

- Signing a raw release binary does not update an archive made before signing (#10128). Recompress every signed binary before calculating `SHA256SUMS` and the manifest. Regression tests must decompress the artifact selected for upload and compare it with the signed raw bytes; command ordering and matching manifest hashes alone can still publish the pre-signing executable.
- Compare compatibility fixtures with the actual old wire value, not an engine-specific assumption (#6192). The Echo runtime's session origin flag is false even when opened through the hub; treating every created session as a herdr-origin true caused an otherwise successful old-hub integration run to fail. Assert field presence and exact old/new value equivalence; test true, false and conflicting aliases separately with synthetic unit fixtures.
- An embedded SDK runtime may not keep a standalone fixture process alive before its node is ready (#6192). Its retry timers are unref'd: launching the Echo example before the runtime socket exists can end the process before reconnection. Wait for the socket and keep a referenced lifetime handle in the lab wrapper; retain bounded startup checks and stop only the lab's process/container.
- Go `-overlay` paths must match the source paths used by the build (#4889). When a checkout is reached through a symlink, mapping only its resolved path can leave the original file in use. Map both the logical and resolved paths, then confirm the mutation actually makes the regression test fail before treating it as evidence.

- GitHub's GFM Markdown API inserts breaks between separate badge source lines (#6182). Put a badge group on one source line to keep it horizontal. When a private repository is unavailable to the browser, label API-rendered screenshots as previews, use GitHub styles, and resolve `<picture>` sources and badges from the verified branch assets.

- Regenerate README assets before checking them (#6182). A committed Android 11 badge looked correct while the generator had no API 30 mapping and regenerated it as API 30. Compare every emitted badge and icon with its committed SVG, not only localized banners, and check HTML image/link paths as well as Markdown links.

- A restart is a separate bounded test phase (#6168). Do not reuse the initial activation context after RPC checks and a graceful stop: stop can consume its remaining deadline before restart begins. Give restart a fresh context with the same eight-second budget, retain all isolation/cleanup assertions, and verify both ordinary and race runs; increasing the timeout would hide the coupling.

- A web test that times out only in the full suite is usually loading or reading something inside its body (2026-11-06, #4955). Vitest reports the timeout as `STACK_TRACE_ERROR` at about 5000 ms (or the test's own budget). On unchanged master with `--maxWorkers=96`, 10 of 10 runs were red. `catalog.test` re-read every language's catalog files and loaded the admin catalogs in each test, `busy-motion.test` walked the whole source tree in each test, and `highlight-lazy.test` did its first `import("rehype-highlight")` inside the test, while the checks themselves take milliseconds. Load or read once at the top of the file instead (collection is not covered by the test timeout), and don't raise the timeout; #5326 (inlined catalogs under Vitest) and #5729 (Persona's warm-up) fixed other cases of the same kind. Two of the failures were not load. `initI18n` applied whichever overlapping call finished last, so a test that timed out mid-switch changed the next test's language (`a、b、c`); now only the latest call applies. `formatList` follows the system region, which under Node is the machine's locale, so on an en-SG machine `a, b and c` failed every run; tests that format set `navigator.languages` themselves.

- A web test that waits a fixed number of `act` turns can still lose to a lazy chunk (2026-11-10, #2874). `RelayAccounts.test` pushed an account's page whose first item is a header, drawn by the `CodeViews` chunk under `lazyPage`; its first draw waits for a dynamic import, and under Vitest even importing a module that is already loaded takes event-loop turns, not just microtasks (2 to 8 `setImmediate` turns in three quiet runs). In one of three loaded full-suite runs that outlasted `settle`'s eight `act` turns and the header was missing; delaying the chunk by 300 ms failed the test every time. Load the chunks first with `beforeAll(() => loadPages(), 60_000)` (as `CodeViews.test` and `ime-enter.test` do), or wait for what the test looks for, rather than adding turns. When a mocked lazy module is the boundary, delaying its real factory makes the gap deterministic; await that import inside `act` and then assert the mounted element with a bounded wait, so module resolution does not escape `act` (#7156).

- A concurrency test that passes with `-race` can still fail without it (2026-11-06, #5908). `-race` slows the instrumented code, so the goroutines meet in a different order. `TestInstallClaimsOperation` passed 20 of 20 runs with `-race` and failed 13 to 29 of 40 without it. Without `-race` its second install usually ran only after the first had committed and used up the preview, and got a 403 from the market check that the PR's own `-race` runs never reached. Run a new concurrency test with `-count`, both with and without `-race`. For the interleaving that matters, set up the state directly instead of hoping two goroutines meet there (here: claim the operation, drop the preview, then install again).

- Goroutines that outlive a test must not call `t.Logf` or read global switches changed after the test ends (2026-11-06, #4822). `computer.nolock` / `computer.cursor` send in their own goroutines; late logging panics (`Log in goroutine after ... has completed`). A fake Mac node reading global `fakeNodesBuiltin` per request sees changes from the preceding test's cleanup, letting late requests fail an already-finished test. Guard logging with a lock and `over` flag set by `t.Cleanup`; fake nodes capture the switch once at creation.

- Assistant tests swap a service's hub with `setHub`, never `s.hub = h` (2026-11-11, #4006). The inbox's "Needs you" teller (`tellInboxNeedsYou`) reads `s.hub` on a goroutine of its own, and a test's service starts one at its first inbox read, so a plain assignment raced with it under `-race`. The teller reads the hub only under `needsYouMu`, and `setHub` swaps it under that lock. The teller is not the only reader: a timer armed while the service is set up (`directionsSoon`, which reads `s.hub` through `hello`) can fire during the test, on a `synctest` bubble's clock too, and nothing orders it after a swap. Where a test only needs a fake behavior, the fake hub does it under `f.mu` (`hangChannel`) and the hub is never swapped. A test with two racing readers can report one in a combined run and the other when run alone, so check each test in separate processes of its own. In the same run, two leader retry tests ended before their retry ran because `sendsTo` also counts sends the fake refused; the retry goroutine then read a package timing their cleanup restored. Such timings belong on the `Service` (`leaderResume`, like `taskRetry`), and a wait must see the effect it names (`deliveredTo`, the sends the fake took).

- Prepared approval result counts need a resumable fake conversation and settled delivery lanes (#10455). Listing `s1` is not enough: the fake also needs `known["s1"]` for `resume_only`, or it records a refused direct try and its asynchronous queued retry as two sends. Mark the conversation known and wait for approval delivery plus relay draining before asserting exactly one terminal result; waiting for the first visible result hides the duplicate.

- A snapshot observer must not own a prepared approval's lifecycle (#13303). `askOnPhone` replays connection snapshots and withdraws observers when they disappear. If it reattaches an unanswered prepared card through `awaitPrepared`, cancelling that observer takes the shared caller-cancellation path and removes the original card while its tool call is still waiting. Keep every active prepared card out of snapshot waiters, before clarification as well as after it; its original caller and prepared timer own cancellation, expiry and final delivery. A regression that only observes the flaky `pending = 0` outcome is not enough: assert that no observer is created, then clarify and resolve the original card exactly once.

- Shortening a shared input window for one event must not expire another event's admission (#10480). `TestAvailabilityReplyWaitWithoutQueue` set `availInputFor` to 1 ms before starting its supervision round; an `inOrder` delay then expired the supervision input, recorded the round as `other`, and timed out the fixture. Scope the shortened window to the owner message and restore it immediately. Do not lengthen the assertion deadline or change production round attribution for a race the fixture created.

- Waiting for an asynchronous event needs a threshold, not an exact global count (#10456). `TestReadinessAuthStatusRetry` waited for exactly two auth-status requests, but prefetch and the second readiness read could legitimately start a third first, so the check missed `==2` forever and timed out. Require the second request (`>=2`) and keep the old-node one-request control exact. Do not increase the deadline for a valid extra request.

- A fake's one-shot hook keyed only by machine runs for whoever lists that machine next, a background goroutine included (2026-11-18, #10331). `TestACardMessageReopensOnlyItsOwnTask` armed `fakeHub.afterList["box"]` for the card message's own look at the task's agents. The acceptance before it had started the todo-token sync (`syncTodoTokens`), which lists the same machine; on a busy machine that list was still out when the hook was armed, took it and ran it in the background, so the card message never did (6 of 1000 runs at `-cpu 4`). Tie such a hook to the call under test (a context value the test sets, `listHooked`), not to whichever call comes next. To find who took one, print `debug.Stack()` where the hook fires and run until it fails.

- The assistant bus writes `m.sessions` and `h.clients`; tests modifying them need the same lock (2026-11-06, #4822). `takeFleetSessions` writes `fleet.sessions` under `m.mu`. Direct test assignment to `n.m.sessions` produces DATA RACE with `-race -count=30` and can be overwritten by an old list still in transit. Use `editSessions` (first `settleAssistant`, then edit under `m.mu`) and `appClient(h)`, not direct assignment.

- When multiple goroutines independently generate and send the same state, generation and sending need one lock (2026-11-06, #4822). On assistant connection, `FleetUp` sends each machine's list, while `Resync` snapshots also resend lists. Previously each called `decorated()` before leaving its lock, then `Tell` afterward; a stale list lacking the snapshot's "Persona" title sometimes reached hub last and stayed, failing `TestTheMainConversationIsAlwaysPersona` about 3% of the time. `publishList` now generates, deduplicates, and sends under `sentMu`. Waiting in tests for "the new list arrived" is insufficient: it may be that stale list.

- Waiting for one effect does not wait for another made on a different goroutine (2026-11-09, #2152). A source page's tree changes first and its frame is sent later by the delivery goroutine (`ui_page_delivery.go`), so `TestPageNativeFiniteLoadStillStartsWithLoading`, which waited for the tree to say `loaded` and then counted delivered frames, failed only on busy CI. Wait for each effect you assert on (`pageAwait` for the frames too). To reproduce such a gap, slow the receiving side in a scratch copy (a fake `Notify` that sleeps 20 ms failed 10 of 10 runs), or run `go test -cpu 1 -count=60` with every core busy (4 of 60 failed); an idle machine passed 40 of 40.
- A cancel on a fixed deadline counted from the start races the fake's own startup (2026-11-10, #2674). `TestToolCheckTimeoutStopsItsHelpers` gave the call 300 ms, but its fake had to start a shell and write its helper's pid first; on a busy machine the cancel came before there was a helper, and the test failed (1 of 30 runs with every core busy) without checking what it was about. Cancel once the fake shows it started (the pid file), and time the return from the cancel. Do not lengthen the deadline instead: it only makes the race rarer.
- A short deadline meant to run out in one step races every step before it (2026-11-18, #10332). `TestRuntimeWorktreeCreateHasOneDeadline` and `TestRuntimeWorktreeDeleteHasOneDeadline` cut the node's one deadline to 600 / 500 ms and expected it to run out in `workspace.create` / `workspace.archive`. The git checks before those steps take longer the busier the machine is, so the deadline ran out in a check or in prepare instead (34 of 200 runs at `-cpu 1,4` with another test binary running). Run the deadline out on cue: pass the call a context whose `Done` the test closes (its `Err` `context.DeadlineExceeded`) when the fake receives the step under test, and time the return from that moment. Check that the node's own deadline ends a call nothing else ends as a separate step, with an upper bound only.
- A listener's `Accept` returning does not mean the client's `DialContext` has (2026-11-14, #7574). `TestDialSocketCancel` cancelled as soon as its fake node accepted, to check that `nodeclient.Dial` keeps waiting for the answer; but Go checks the ctx once the connect completes, so a cancel in that gap failed the dial itself with `operation was canceled` (30 of 100 runs on Linux with 4 CPUs). To cancel "after connecting", wait for something the client sends only after connecting: the fake reads the request line first.
- A child's `/proc/<pid>/environ` can read empty right after `exec.Cmd.Start` returns (2026-11-14, #7028). `Start` returns once the new program's close-on-exec has run, but the kernel lays out that program's environment later, in the ELF loader; reading it at once came back empty 1835 times in 2000 on the hub machine. `TestCodexTakeoverUsesOnlyTheSelectedWritersTerminalBridge` started `sleep 60` as its stand-in Codex writer and the takeover read the writer's environment a few calls later, so it refused with `codex_takeover_unavailable` about 3% of the time per subtest. A fixture whose code under test reads a child's `/proc` environment (or anything else set up after exec) waits until the child runs its own code: `/bin/sh -c 'echo ready; read -r line'` with stdin held open, then read that line. Keep the blocking step a builtin, since dash execs a last simple command such as `sleep` in place, which opens the same window again. A real process found by a scan has long been running.
- A recovering assistant conversation's `busy` alone does not mark the end of its turn (2026-11-10, #3815). When recovery moves to the next model, `assistantSend` registers the replacement process (`spawnAssistant`) before it marks it busy and sends the retry, with the context snapshot and the mode sync in between. `sdkFixture.idle` waited for the registered process's `!busy`, so `TestAssistantRecoveryStopsAfterBothModelsAndTheirRetries` counted 2 of its 4 requests (9 of 400 runs on a busy Mac). The node publishes that gap as working (`busy || recovering` in `assistantSessions`); a wait for the end of a turn must use the same rule. To find such a gap, log the internal state at the moment the wait returns in a scratch test: slowing the fake CLI's `initialize`, the first guess, did not widen it.

- Fixtures must use real response shapes (2026-11-06, #2938 / #2017). Assistant-feed `view_actions` is an object indexed by card key, empty as `{}` too. When both web and acceptance drivers mistook it for an array, array fixtures kept unit tests green while the real model lost answered state after reload. Use protocol objects covering empty objects, answered snapshots, and pagination; in a real browser also reload and check latest version, answered state, and no repeat sends.

- For autonomous rendering acceptance, specify the user scenario without secretly prescribing tools (2026-11-06, #2017). "Update progress in the same location" lets the model show a selection card in the body, then a progress card after selection; a driver requiring the first card to be live will not select it. State a same-interface-throughout requirement in natural language, without schemas, tool names, or prefab cards. Preserve the original failure; an available tool list does not prove the model called a tool.

- Launch an actual browser once before spending real model calls (2026-11-06, #2017). `python -I` omits user sites, so an installed Playwright may not import. Retain import isolation and explicitly add only trusted installed dependency paths, not working/download directories or `sitecustomize`. Resume after dependency failure only with private failure records, a same-version test hub without a main chat, and a session with no submitted actions; never click already-submitted actions again.

- Progress changes do not prove end-to-end completion (2026-11-06, #2017). After checking the same DOM and View ID, wait for completion, preserve answered state, restore after reload, and read-only verify the unique outbox entry and chat reply in the test database. The model may remove selection buttons during updates; removed or disabled controls must not resubmit, but answered state must still restore.

- Check pinned-source test evidence separately from production downgrade prevention (2026-11-06, #2017). Further undeployed mainline commits neither erase passed evidence for pinned source nor let old evidence cover new source. Controlled forward updates must confirm the candidate belongs to official history and contains the actually installed version as an ancestor, then verify production web provenance, input tree, build output, configuration, and hash; never overwrite web assets of unknown provenance. Preserve backups and CAS rollback. If release windows conflict, continue isolated validation only, without defaulting to production updates again.

- A guard-rejected launch is not successful restricted execution (2026-11-06, #2017). Once verified that rejection returns before native CLI execution, classify it separately as blocked and preserve logs. Still require an actual restricted launch, tool allowlist, and real chat tool-call evidence. No successful launch or any unknown launch means failure; never delete rejection logs to manufacture a pass.

- `-race` reports reads/writes without a happens-before relationship; a test's own lock can hide a race (2026-11-05, #2955). `startAsync` reads the launch record's session after `go func()`; a fast background failure changes it in `finish` under a lock. Existing tests read, then wait through the `startLog` lock; `finish` acquires that lock next, so the detector sees the read before the write. It reports only if the caller is preempted just after `go` (one occasional full-suite `-race` failure). Regression `TestStartAsyncAnswersBeforeAFastFailure` waits only on the record's `done` channel and reports every time before the fix. Race-test result waiting must not share a lock with the code under test.

- Taking a sleep out of a race test can leave the detector with nothing to see: a channel orders two goroutines as a lock does (2026-11-14, #1273). `TestSearchesWhileTheIndexIsUpdatedAndRemoved` (`server/internal/kb`) is there for a data race between a search that reads the index again and an update under way. Its first version without sleeps stopped the update at a hook, removed the directory, waited for a search to run and only then let the update go on: every wait was a channel between the two sides, so the update's read came after the search's write for the detector too. With the race put back in a scratch copy, that moment reported nothing in 30 runs of 4 removals each; the test with the sleeps reported it in 20 of 20. Keep the two sides unordered at the moment under test: let the stopped side go on before the other starts, and make whatever comes next wait instead (here the following update), which reported 40 of 40 runs with one removal each. A test whose assertion is the `-race` run is checked by putting the race back and counting runs, per moment, not by passing.

- A fixture goroutine that reads a shared field when it runs races a test that swaps the field, and a socket orders nothing for the race detector (2026-11-11, #3918). `withFakeHub` started `go func() { _ = n.peer.Serve(…) }()`, which reads `n.peer` whenever the goroutine gets to it; `TestCodexRemoteTurnBegunBeforeJoining` then put its own hub in front with `n.peer = …`. `TestOutsideClaudeSendNotTaken` set the fake inbox's `mode` without its lock after the accept loop had started; the node connecting over the Unix socket afterwards gave the connection's goroutine no happens-before edge to that write. Both failed only under `-race`, which the CI node shard does not use. Have a goroutine serve the value the fixture created (a local variable), and change state a running fake reads through a setter under the fake's own lock (`fakeInbox.setMode`).

- When testing fixed interactive-shell scripts, isolate `HOME`, `USERPROFILE`, and `ZDOTDIR` first (2026-11-05, #3120). Replacing only the update command still reads the user's shell startup files, mixing toolchain notices into `CombinedOutput` and failing exact-output assertions. Point all three directories at a test temp directory; retain success, failure, and unknown-action rejection assertions without changing production shell behavior or user configuration.

- Model stubs record only the calls under test (2026-11-05, #2682). Agent naming inherits the same test model through `kb` and can be miscounted as completion judging; GET probes with no body can also overwrite `js.input`, making successful real judgments appear to have empty input. Use a separate synthetic naming stub; judge endpoint accepts POST only. Regressions verify naming and GET before/after judgment do not change counts or input. On interrupt, assert zero judge calls directly, not mixed counts after sleep.

- Model rate-limit fixtures count only model POSTs (2026-11-05, #2763). Counting every HTTP method in sent/inflight/most lets GET probes alter peak concurrency and consume the next fake response, measuring something other than the model concurrency limit. Non-POST returns 404 before counting; regress counts before/after GET and one real model POST. Preserve limit learning, recovery probes, max-two concurrency, and no-failure assertions. Do not alter the production limiter, loosen limits, or add sleep.

- Publish startup evidence only after dependent resources are ready (2026-11-05, #2504). nodeupdate fixtures wrote version/PID/environment evidence before listening on the Unix socket; re-exec tests connected immediately after seeing the new version and sometimes found no socket. Ten passing targeted repeats do not eliminate a confirmed race. Move evidence creation after successful net.Listen; retain connection timeout and actual re-exec assertions rather than hiding ordering errors with retries or longer deadlines.

- Full-script fixture timeouts are hang protection; leave load-dependent headroom (2026-11-05, #2264). Hub deployment fixtures run real `deploy.sh`, spawning about 48 Python processes plus bash for every fake ssh hop. CPU time is about 7 seconds; wall time grows with load (18-core Mac: load 35 ≈10 seconds, 45–50 ≈24–27 seconds, 80 >30 seconds). Timestamped `bash -x` shows time spread across steps, even fake `hostname -s` taking 1.7 seconds, with no single hang. Thus `timeout=30` occasionally raises `TimeoutExpired` on busy machines (40 failures in 50 runs with six concurrent tests). Use a named 120-second constant with rationale, preserving assertions. Diagnose by child-process rusage CPU far below wall time, `uptime` load far above core count, and no single long trace interval.

- First execution of newly written scripts/programs on macOS can wait nearly 10 seconds (2026-11-05, #2307). System/security checks precede execution; the first tens of seconds of `go test ./...` link and launch dozens of new test programs, queueing those checks.
  Executing a newly written two-line sh script every 0.5 seconds measured median 0.26 seconds, max 9.5 seconds, while existing `/bin/sh` and `zsh` stayed below 0.1 seconds. If a test passes alone but fails in `go test ./...`, check whether it waits on a new executable first.
  These are hang-protection deadlines; allow 30 seconds. This is not load-related: a separate `go test` at the same load stayed fast.

- A deadline that starts at a child's exec also times the child's start-up (2026-11-08, #1110). The path-access preflight tests start the node test binary as the check's child and cut it after a 400 ms stall. Under the whole server suite's load the binary took longer than one 400 ms stall to print its first line, which the old timers counted as a read, so the check read nothing and warned, while each read takes microseconds. Start-up now has its own budget (`preflightStart`: one stall in the product, so a run is still five stalls; 30 s in `TestMain`), and read stalls count from the child's first line. The hung-read test is timed from the moment the child runs (`NODE_TEST_PREFLIGHT_STARTED`) and keeps the short stall, so it still proves the hung read is cut. A child made to start late (`NODE_TEST_PREFLIGHT_SLOW`) fails on the old timers: test that way rather than by raising the stall.

- Hub message discovery scans literal `"type"` keys and `Type` fields throughout the package, including JSON Schema construction (#2597). A schema's `boolean` type can be mistaken for a WebSocket message. Give schema fields an explicit Go name such as `JSONType` with the same `json:"type"` tag, and run `TestBroadcastTypes` alongside the tool-schema golden. Do not add schema types to the broadcast allowlist.

- `.Local()` in timezone scanning is not always `time.Time.Local()` (#2239): Orca terminals have a same-named boolean method for local execution. Inspect AST and return types; exclude only calls explicitly returning bool, conservatively inspect unknown types, and add no whole-file exemptions. A test with boolean and time calls on the same line ensures the exclusion does not hide real timezone calls.

- Network-isolation regression listeners can also be probed by Agent port discovery (2026-11-06, #2025). `server/internal/node/ports.go` finds listeners in Agent process trees and sends `GET /`, `User-Agent: persona-ports` to actual bind addresses; another loopback address does not guarantee isolation. Attribute the noise only after an independent listener without WebView receives the same requests. Isolate only this task's test processes, retaining reachable-network controls and zero-new-connection assertions. Do not filter by UA or stop/change real node; preserve original failures.
- External watchdogs must cover the actual test process group (2026-11-06, #2025). If both outer driver and inner test use `start_new_session`, stopping only the driver's PGID misses the real test group. Record owned PID/PGID, TERM then bounded KILL and wait; print terminal state on success, failure, and timeout. Every listener/JS callback needs a deadline too. Fail immediately on load failure, rather than continuing and treating empty results as passes.

- After resolving rebase conflicts, `git diff --check` covers only uncommitted edits; a clean worktree does not prove commits lack conflict markers (#2195). Before push/merge also run `git diff origin/main...HEAD --check` over the full PR diff.

- macOS Unix-socket regressions need short canonical temp directories (2026-11-03, #277). Long system `TMPDIR` exceeds socket address length; `/tmp` can still resolve to `/private/tmp`, making a Codex fixture's original HOME differ from canonical HOME returned after initialization and appearing as handshake timeout. Use independent `/private/tmp/<short-directory>` as test `TMPDIR`, without changing lexical compatibility rules for runtime roots. Preserve failures first, retest pinned source separately; real `lsof` port-scanning defects belong to #541 / #728, not temp-directory issues.
- Go 1.27's `testing.T.TempDir()` prefers `GOTMPDIR` over `TMPDIR` (confirmed in the toolchain's `src/testing/testing.go`, #6223). Changing only `TMPDIR` leaves test fixtures under the old root. For Unix-socket and configuration-ancestor tests, set both variables to the same new, real, short system temporary directory and isolate `HOME` separately. A long root can exceed Unix-socket path limits; a symlink into a personal home can expose real Codex configuration to ancestor scans. Verify available disk space and effective roots, preserve build caches separately, and never weaken the security assertions to compensate for a misconfigured runner.
- A temporary directory can have a Git repository above it (#3017). Node polling tests that require the next push to reflect their own change use a fixture-owned repository and wait for `awaitGitPlace` before polling. Since #2758, cold Git labels arrive asynchronously: their `sessions.changed` can precede a transcript-only revision or a workflow cache refresh. Longer timeouts do not fix consuming that unrelated push. `readGitPlace` walks the filesystem directly, so `GIT_CEILING_DIRECTORIES` does not isolate its tests; an empty `.git` file bounds discovery for a non-repository fixture.
- A shared offline-session cache can satisfy a reconnected node's list without populating that node's transcript metadata (#3017). Carry tests must exercise the new node's first metadata use and check scan identity, rather than infer a missing carried scan from an empty per-node metadata map. Disable ambient agent-process discovery in the fixture and join `Attach` on failure before restoring global scan settings.
- The server test wrapper hides macOS `/var` path-spelling mismatches (2026-11-08, #1105). `scripts/test/server_test_env.py`, and so hosted CI, sets `TMPDIR` and `GOTMPDIR` to a resolved directory. A test that compares a path the product resolves with `filepath.EvalSymlinks` (such as `hubBinary`) against one built from `t.TempDir()` passes there and on Linux, but fails under a plain `go test` on a Mac, where `t.TempDir()` is under `/var -> /private/var`. Resolve the expected value at the assertion. Resolving the shared temp home instead would also hide a product that stopped resolving. To check a whole package under `/var` with the wrapper, reset the variables inside its command: `server_test_env.py --database-file <file> -- env -u GOTMPDIR TMPDIR=<macOS temp dir> go test ./internal/hub/`.
- The same `/var -> /private/var` mismatch hid a release bug in the Python scripts (2026-11-09, #1960). `prune_nodes` compared the resolved `bin/nodes` target with unresolved version directories, so under a root with a linked ancestor it deleted the version in use; `NodeRetentionTests` failed only on a Mac, and resolving the test's temporary directory would have made it pass. Fix such tests at the assertion (or in the product), and build the fixture root through an explicit link so Linux CI catches it too. Separately, `scripts/ci/python-tests.py` runs every `scripts/**/test_*.py` under `unittest`: an argument-driven script with that name runs no tests, which exits 0 on CI's Python 3.11 but 5 ("NO TESTS RAN") on 3.12+, so the runner skips files that are not `unittest` suites.
- On a Mac the first exec of a newly written executable is slow (2026-11-10, #2706): about 290 ms here, against 13 ms for the second, while the system assesses the new file; Linux CI never sees it. A test that writes a fake binary and gives its first run a deadline shorter than that (`versionWait = 100ms` in `TestBuiltinNewBinaryHangs`) kills the fake before it runs a line, so a count the fake keeps stays empty. Observe the call from the caller's side (wrap the package variable that runs it) and wait for the effect, not for a fixed time after the write. The same latency made the 300 ms deadline in #2674 fail 1 run in 5.
- A fake server that waits in `Accept` hangs the package when the code under test fails before it connects (2026-11-10, #2706): `TestAuthorityWireReadsPersistedIdentityWithoutMigrationOrWrites` ran into go test's 10 minute timeout on a Mac. Close the listener once the call returns, then read the fake's verdict, and report the call's own error with it.

- Validate display names separately from persisted identity during compatible renaming (2026-11-04, #1189). After `persona-link` / `Persona Link` shipped, compressed-install regressions still asserted old display names, causing full release exit 1. Update only display contracts, while verifying real `persona-link` files, legacy download names, old/new executable commands, and actual installer calls; never blindly rename compatibility identities. Script tests must discover `scripts/test`, `release`, `deploy`, `mock`, and `app` separately; rename-only subsets miss old assertions elsewhere. Preserve original failed freeze/exit code; after merge, rerun with a new build number.

- Time-sorted lists need deterministic ties (then sort by ID): two spaces with equal update times can swap on every render, causing UI tests to tap another row.
- Mock hubs must answer like real hubs, especially unready states: with Telegram disabled, real hub cannot list its capabilities (the program has never run; capabilities are self-reported),
  but mock always advertised `login`, leaving tests/screenshots green while the real page lacked a switch, said "Configure in hub.json", and could not enable it (2026-11-03, #622).
  Before writing a mock, inspect the real path's response in every state; start tests from what users first see (disabled).
- Run privacy scans after actual build/deployment configuration is complete: missing configuration means incomplete checks, and `clean` is not a full pass. Rescan after adding configuration.
  Fixture machine identifiers can accidentally match configured values; use neutral placeholders, not exemptions.
## Privacy checks and dependency download sources

- 2026-11-04, #1775: even unmodified Mermaid assets can coincidentally match short private terms inside public event names. Download the pinned package from npm first, verify registry SHA-512 integrity, then byte-compare repository assets with the archive. Never log private terms or matched text.
  `check_bundle_privacy.py` checks private terms of up to 4 ASCII letters as whole words only when both filename and pinned SHA-256 match original Mermaid 12.3.0 assets. Complete short words, nonletter values, longer private values, and generic email/private-address checks remain. Hashing and scanning use the same byte snapshot, not a mutable local allowlist.
  Modified, appended, truncated, renamed, or normalization-created fragments cannot receive this classification. Other app files retain strict substring checking; existing public Go mode is unchanged. Dependency upgrades require rechecking public provenance, negative cases, and pinned digests, not merely copying new hashes. Preserve the original failed archive read-only; release fully after merge with a new freeze/build number.

- On 2026-11-02, after configuring `app.push_relay`, the publisher's public relay URL had to enter Info.plist but also matched configured personal-value scans.
  App bundles recognize only the complete configured HTTPS `/relay` URL with no credentials/query. The same domain elsewhere, appended paths/parameters, emails, or credentials still reject;
  public Go binaries get no exception. Do not remove the whole domain from the private-term list; regress adjacent and extended URLs.

- On 2026-11-02, after fixing privacy scanning, public personad still failed to deploy: networking dependencies made raw binaries about 35–37 MiB, exceeding Workers' 25 MiB per-static-asset limit, not expired cf authentication.
  Publish `.gz` downloads (about 12–14 MiB), scan uncompressed binaries first, and verify compressed archives before installer decompression; check every asset size before upload. `NO_DEPLOY=1` must also check sizes; build/scan alone is not deployment validation.
  Do not suggest signing in again for every failure. Report successful TestFlight upload separately from failed server publication, and finish internal/external distribution of uploaded builds.
- On 2026-11-02, public personad privacy false positives included SSH global-request names mistaken for emails, official contact links in Tailscale embedded pages, complete RFC1918 ranges, and cloud-metadata constants mistaken for personal data.
  Go concatenates read-only strings, even one CIDR immediately after another, so printable-string boundaries are unreliable. Trace matches to dependency source using actual compiler inputs first; never log raw matches.
  `check_bundle_privacy.py --whole-short` classifies only exact protocol/HTML/network contexts in dependencies built with the `public` tag, without replace, and with checksums matching `server/go.sum`;
  `publish.sh` first runs `go mod verify`. Configuration/private-term matching still takes precedence on raw text. Missing dependencies, checksum mismatches, or changed context reject; no whole email-domain or private-range exemptions.
  Regressions: `python3 -B -m unittest discover -s scripts/release -p 'test*privacy*.py' && python3 -B -m unittest discover -s scripts/test -p 'test_privacy*.py'`, plus `NO_DEPLOY=1 ./scripts/release/publish.sh` for four platforms.
- 2026-11-03, #963: SSH algorithm and certificate key-type names in Swift archives also contain `@`, causing generic-email false positives. Check complete printable fields against public dependency source first; never log matches.
  `check_bundle_privacy.py` classifies only 7 verified fixed names in `swift-nio-ssh`, requiring complete original-field matches. Names formed after removing bundle/team IDs do not count.
  This is not an email-domain exemption and does not change generic-email TLD scope. Private terms still match normally; surrounding context, URLs, variants, and ordinary same-domain emails do not receive protocol classification.
  Validate field-level and actual-scanner regressions, then inspect failed archives read-only. Preserve original freeze, exit code, deployed/not-uploaded state; later releases use new freezes/build numbers, never reuse failed builds.
- 2026-11-04, #1026: full SSH name-list compatibility must not become removing names from arbitrary word-boundary contexts; that wrongly permits `mailto:` URLs, XML, and names formed only after normalization (#975 regression).
  Classify only fixed names in complete original fields, or comma lists whose every item is in the fixed set with no empty items/whitespace. Unknown items, URLs, context, and fragments formed after bundle/team-ID removal still scan.
  First fail synthetic counterexamples on the old implementation, then verify field-level, actual-scanner, and private-term precedence. Retain positive full-name-list cases; a boundary fix must not break prior compatibility.
- Watch Release binaries can be arm64 + arm64_32 universal Mach-O. Certificate email/personal names in signatures are not business data; privacy scans exclude only embedded signature blobs verified structurally and by bounds in every slice.
  `LC_CODE_SIGNATURE` offsets are slice-relative; never truncate the entire file at the first slice's signature, or remove private terms because certificate false positives occurred in a universal file.
  Scan both architectures before/after signatures, unused signature-placeholder bytes, and file tails normally; malformed signatures and out-of-bounds/overlapping slices are not excluded. Regress thin/universal 32-/64-bit and both endiannesses, plus a real Watch archive.
- `privacy_check: clean` does not prove a complete private-value dictionary. Without local config/private terms, registered matches may include only local paths. npm lockfile download hosts need independent public-source rules, not only private terms; public package names do not prove public `resolved` URLs.
- Staged-content checks must read the Git index, not only the worktree. If fixed in the worktree but still old in the index, commit hooks must reject; conversely, a safe index must not be rejected because subsequent worktree edits are unsafe.
- Check lockfiles as complete JSON before ordinary text scans. NUL/binary detection must not skip them, and duplicate keys/JSON escapes must not bypass checks. Errors report only location/type, never blocked addresses.
- Existing hook files do not mean enabled hooks: `core.hooksPath` is clone-local configuration. After enabling, verify real `git commit` in an isolated temporary repo, never rejection-test the main worktree. Web builds check too, preventing hook-disabled clones from direct publication.
- Recursive-failure tests must explicitly control or simulate recursion limits, not assume a fixed depth fails on every Python version. Run Python regressions separately; `npm test` does not cover them automatically.
- If `npm ci` returns 404 because a mirror lacks a pinned version, inspect the command registry first. Even public lockfile sources can be redirected by local registry configuration. Retry with explicit public registry and locally configured proxy; do not change lockfiles, versions, or test baselines to hide environment failures.
- Async refreshes triggered by config hot switches need a generation, checked under the state lock before committing cache. Comparing only captured booleans cannot prevent out-of-order returns or old requests overwriting after "on → off → on". New-connection handshake refreshes use the same commit check.
- Static-card previews distinguish reply from push/pop/open. Action IDs are opaque, not message bodies when reply is absent. Regress with shared-example detail buttons; navigation only demonstrates locally or accurately explains its target, never calls a real chat.
- Background builds/tests require pinned source snapshots; do not merge branches or edit their worktree while running. On 2026-11-05, targeted Go regression enumerated old files before a merge introduced a new caller, producing undefined-method compilation errors. Mixed snapshots cannot judge commits or count as passes. Freeze a commit in a separate detached worktree and rerun.
- A Go test wrapper embedding a narrow `Store` interface does not gain methods by assigning real Postgres to it. Once card handlers checked optional persistence interfaces first, admin-page tests returned 503; old handlers rejected bad JSON first and hid the fixture gap. Real bus regressions explicitly pass the full DB to the assistant, while hub admin login keeps its wrapper. Retain status assertions, not longer waits or 503 as a pass.

## A test that loads a native library must declare the library as an input (2026-11-13, #6871)

`:core:rustcore`'s tests load a host build of the shared Rust core. They find it through a system property that holds its
path, and Gradle treats that path as the only input. A change to the Rust code rebuilt the library at the same path, and
Gradle then called the tests up to date and skipped them. A deliberately broken rule still "passed". The fix:

- The task that builds the library declares it as its output (`HostCoreLibrary` copies cargo's result into the build
  directory, with the core's sources, manifests, lockfile and toolchain file as its inputs; #10712).
- The test task takes that output as an input by its contents (`FileContentProperty`, `PathSensitivity.NONE`), which
  also passes its path to the test JVM.

**To check the setup**, break one rule on purpose and confirm the binding tests fail before trusting them.

## Go's test cache can repeat a pass the change broke (2026-11-22, #12724)

Every Go test command the repository's scripts run passes `-count=1`
(`docs/operations/testing.md`, "Why tests stay uncached"), and dropping it to save time keeps coming up (#10613).
The reason: Go reuses a test result while the test binary and what the test process itself recorded opening are
unchanged, and it rechecks a recorded file only inside the module root (`computeTestInputsID` in `cmd/go`: "Do not
recheck files outside the module, GOPATH, or GOROOT root"), so for `server/` the rest of the repository is
invisible. Those inputs are exactly what check-fast sends to the packages that read them (the `spec/fixtures/*` rule in
`scripts/ci/check_fast_map.py`, and `server_readers` in `scripts/test/check-fast.sh`), and what a child process reads,
like the `go list` that `internal/archguard` runs, is never recorded at all.

Reproduced on 2026-11-22 with Go 1.27.3 at `origin/main` a133f8cce9, from `server/` (`-run` keeps other tests out of
it; a package with any failing test is never cached):

```sh
go test -run '^TestScriptOrStreamGoesByTheBytes$' ./internal/proto/           # ok 0.101s
cp ../spec/fixtures/file-types/typescript.ts ../spec/fixtures/file-types/segment.ts
go test -run '^TestScriptOrStreamGoesByTheBytes$' ./internal/proto/           # ok (cached)
go test -count=1 -run '^TestScriptOrStreamGoesByTheBytes$' ./internal/proto/  # FAIL: segment fixture is 1018 bytes, not whole packets
git checkout -- ../spec/fixtures/file-types/segment.ts
```

Inside the module the guards go stale the same way: after `go test -run '^TestContractsArePure$' ./internal/archguard/`
passed, a new one-line package under `server/internal/` whose name ends in `wire` (which makes it a contract package
the guard's list must name) still got `ok (cached)`, while `-count=1` failed with "… looks like a contract package but
testdata/contracts.txt does not list it".

What a hit would save, measured the same day with the build already cached (110 s to warm it on a loaded machine): the
guard packages check-fast runs on every server change took 12 s uncached and 1 s cached, and its guard tests 10 to
14 s uncached. Only packages the change did not reach can hit at all: the edited package and every package that imports
it get a new test binary and run again. Caching also changes the run itself: `go test` passes `-test.testlogfile` only
when the result may be cached, and `internal/office` failed only in that mode, because a test that lowered the file size
limit also cut off that log (#8962, fixed by #8968).

- Keep `-count=1` in every command whose result is reported as verification, and in any new entry point. A plain
  `go test` while editing is fine, but its `ok (cached)` says nothing about the change.
- A test cannot tell Go's cache about a file outside `server/` or about what a child process read, so a reader of
  `spec/`, `schema/`, `sdk/go/`, `docs/` or client sources stays correct only while it runs uncached.

## Running the hosted CI lanes on a Mac (2026-11-09)

The `scripts/ci/` lanes are written for the hosted runners; on a Mac developer machine:

- `scripts/ci/server.sh` (all / `node`) first runs "Browser runner resources" (`google-chrome --version`, `free -m`, `df -h /dev/shm`), a Linux runner diagnostic that exits 127 on macOS before any node test. Run `server.sh build`, `managers` and `other`, and the node shard's own step (`scripts/ci/server-shards.py node --database-file …` against the same Postgres 18 fixture); every Go test still runs.
- `server/internal/extension/denohost` needs `bun` on PATH at the version `.github/workflows/server-test.yml` pins (1.4.2); without it `TestTrustedAdapterMatchesOfficialSDK` fails with "Bun is required", and with another version the committed `adapter.mjs` looks stale. A pinned copy outside the global environment works: `npm install --prefix <dir> bun@1.4.2`.
- `scripts/ci/scripts.sh` expects Python 3.11 or later with `scripts/ci/requirements.txt` (PyYAML, Pillow) and `actionlint` (CI: `go install github.com/rhysd/actionlint/cmd/actionlint@v1.7.12`). Xcode's `/usr/bin/python3` (3.9, no PyYAML) fails about twenty suites for reasons unrelated to the change; use a venv first on PATH.

## Test database setup (2026-11-10, #2786)

- `TRUNCATE` gives every table it names new files, whether it had a row or not, so clearing two dozen mostly empty tables before each test cost more than many of the tests. Check the tables the cascade reaches in one statement and truncate only those with a row (`db.truncateNonEmpty`). Don't replace it with `DELETE`: it does not cascade, and `devices` has a `BEFORE DELETE` trigger (`devices_guarded_delete`, hub 0435) that refuses an unguarded delete with "push retirement requires a guarded writer".
- A database copied with `CREATE DATABASE ... TEMPLATE` keeps none of its template's `ALTER DATABASE ... SET` settings: set `synchronous_commit` on every copy (`dbtest.create`). Nothing may be connected to the template; Postgres waits up to 5 s on its own for backends that are closing, so no sleep after `Close` is needed. Use `STRATEGY wal_log`: `file_copy` checkpoints the whole server twice.
- Since Postgres 15, every `DROP DATABASE` forces a checkpoint of the whole server, which writes whatever other tests left dirty. On a loaded machine, one drop took 0.07-3.8 s. Count that before giving more tests a database of their own.

## An edit a watcher must see once is written whole (2026-11-13, #2174)

- `TestFsWatchGitStatusChanges` failed its final quiet check under the deploy's test load: one more Git `fs.changed`.
  The watcher was right. `os.WriteFile` truncates and then writes, and a look in between sees an empty file, a real
  change of its own; the finished write is a second one. A trace of every Git look (the status text, each listed
  file's size and time) showed `a.txt` at size 0 in every failing run. The window is well under a millisecond, so
  about 400 runs at the test's own intervals never hit it, with CPU burners or fsync writers alongside too. With the
  watcher looking continuously, plain writes failed 8 of 200 runs and whole writes none.
- Write such an edit beside the watched place and rename it in (`writeWhole` in
  `server/internal/node/fswatch_test.go`), staged outside a watched Git repository: a staged file inside it is an
  untracked change. Keep the quiet check; don't lengthen waits or debounce the watcher to hide a state that is real.
- To catch a narrow window, shorten the watcher's interval in a scratch copy and print the trace only on failure,
  rather than loading the machine and waiting.

## "TempDir RemoveAll cleanup: directory not empty" after an assistant test (2026-11-12, #5510)

- A test passed and then failed its own cleanup: something wrote into its `t.TempDir()` after the test returned. In
  `internal/assistant` the writer was the service's debounced timers (`directionsSoon`, `directionTreeSoon`,
  `meetingLiveChanged`, an attending look): they fire up to a second later, call `kbGet`, and that opened the
  knowledge base again after the test's cleanup had closed it, putting back `.lock`, `chats/`, `people/` and the rest.
  The same happened to a stopping manager (a late timer took the data directory's lock back), so it was a code bug:
  `kbEnd` closes for good, and test fixtures register `t.Cleanup(s.kbEnd)`, not `kbClose` (which stays reopenable,
  for the disconnected case and restart tests). A timer that already holds the engine still writes: the meeting
  fixture runs the attending feed on real timers, so its cleanup waits for the looks already running and lets none
  start after (`trackAttendFeed` in `server/internal/assistant/meeting_live_test.go`).
- It shows only under load (one in four shuffled package runs on a busy Mac; six processes of `-run '^TestMeeting'
  -count=20 -cpu=1` reproduce it every time). To find such a writer, give the fixture a directory whose cleanup, when
  `os.RemoveAll` fails, lists what is left (with modification times against the cleanup's start) and dumps every
  goroutine with `runtime.Stack(buf, true)`: the stack that calls `MkdirAll` or opens the lock names the timer.

## Green here, red on the hub: judge server tests on the hub machine (2026-11-12, #5182, #5188)

- A test that needs Postgres skips without `PERSONA_TEST_DATABASE_URL`, and a plain `go test` prints `ok` for its package. #5031 changed what `task_plan` answers and checked `internal/hub` without a database, so `TestSupervisedTaskOverTheBus`, which drives that answer end to end, never ran and went red on `main` (#5182). Before a change that touches what a database test covers, run its package with `scripts/test/test-server.sh <package>`, or `go test -v` and check for `SKIP` lines.
- Allocation budgets are set from numbers measured on the hub machine (linux/amd64). The same tests read about 2 KB lower on an arm64 Mac, so #5150 passed `TestReindexOneRevAllocation` and `TestIndexWholeListAllocation` there and failed them on the hub (#5188). Judge a budget only from a `test-server.sh` run. To find where a regression comes from, compare `allocated(...)` in a probe at both commits; the Mac shows the same difference, so a `pprof -sample_index=alloc_space` profile of the measured call can be taken there.

## Settle task outcomes and query review history by ref in Postgres tests (#13231)

- Accepting a task review starts `goRecordTaskOutcome`, which asynchronously writes a `DecisionOutcome` receipt. Call `s.tasks.outcomeRuns.Wait()` before inspecting the task's decisions, then keep rejecting every non-outcome row; neither deleting the assertion nor treating that receipt as an owner click tests supersession. Cancelling a review does not start this writer.
- `DB.Feed` returns a bounded, oldest-first conversation page. In a shared Postgres shard, older rows can fill all 500 entries and push the review written by the current test out of that page. For a to-do's review audit, query `FeedByRef` with `feedReview` and retain the conversation and `superseded_reason` assertions; increasing the page size only moves the intermittent failure.

## "Not listed" holds for a transcript the list never shows (2026-11-12, #5054)

The node's list leaves out offline Claude transcripts under 2 KB (a hook probe's, `scanOffline`), and the fixtures of
the launcher switch tests (`finishedTurn`) are about 400 bytes. A test that a session is left out of the list once it is
no longer running passes with such a transcript whatever the code does: the engine switch's "the old session is not
listed" did, and its title check (the row takes the old session's title, read off that session's row) failed until the
transcript was long enough to be listed (`longTurn`). Before asserting a row is absent, assert it is there in the same
setup.

## A throwaway hub started from an Agent's terminal uses the real state directory (2026-11-17, #10096)

An Agent that Persona started inherits `PERSONA_HOME`, and `identity.DataDir` takes it before `HOME`: a throwaway
`personad hub run` with only `HOME=<tmp>` reads and writes the machine's real `~/.persona/hub.json` (a first run writes a
default one), and `personad hub setup` then creates the shared `persona-postgres` container and writes its address into
that file. Set `PERSONA_HOME=<tmp>/.persona` (and `HOME`), give the throwaway `hub.json` its own `database_url` and
`listen` instead of running `hub setup`, and list the real state directory and `docker ps -a` afterwards. If it happened,
remove only what the run created (check creation times first), never a file that was there before.

## A made-up recording under a fixed id must never reach the hub, its delete included (2026-11-19, #10589)

The replayed meeting (#9417) used one fixed id and was kept from uploading, but its Delete still queued
`DELETE /api/v1/recordings/<that id>`. The hub answered `{"deleted": false}` and kept a tombstone for the id anyway; from
then on every replay's entry was deleted by reconciliation while it played. Guarding the obvious calls (upload, the
live read) is not enough for such an id: keep it out at the client (`ReplayedMeeting`: `APIClient` refuses any path or
query naming it), make its delete local, and have reconciliation ignore the hub's word on it, since an older app may
already have left the tombstone.

## Live-only Go tests need their own compile pass (2026-11-11, #4654)

Ordinary `go test` and CI do not include `//go:build herdrlive`, so a node helper signature change can leave live-only callers uncompilable while every default test stays green. After changing such a signature, compile the whole node test package with `go test -c -tags herdrlive -o /tmp/node-live.test ./internal/node`; `-run` selects which test executes but does not reduce which source files must compile. Run the live tests themselves only on demand in an isolated herdr session, never against the working one.

## Dead-code reports depend on the selected build (2026-11-07, #5856)

- `deadcode -test ./...` only analyzes the selected platform and build tags. A default Linux run reports `sysmon.ptr`, the Windows tray notification helpers and `DevelopmentLimits.valid` as unreachable, but macOS, Windows and the `persona_dev_resources` build respectively call them.
- Before deleting a reported function, use repository-wide `git grep` to check callers, tests, scripts, docs and string-based references, including files excluded from the current build. Keep candidates whose reachability is uncertain; a clean build on one platform cannot establish that they are unused everywhere.

## Keep service proxy routing out of server fixtures (2026-11-08, #1175)

- Server test children must not inherit service proxy routing (#1175). An ambient HTTPS proxy redirected a local Git clone and synthetic voice DNS fixtures, and remained visible outside an extension sandbox. A local rejecting proxy reproduced all four failures; isolating only the test child's upper/lowercase HTTP, HTTPS, ALL and NO proxy variables made the unchanged fixtures pass. `server_test_env.clean_environment` removes that routing at launch; explicit fixture proxy setup still works. Preserve the parent's route for toolchain resolution, dependency preparation and publishing, rather than changing production proxy settings or increasing test deadlines.

## A shell proxy sends the mock suites' loopback calls away (2026-11-18, #10465)

- With `http_proxy` (or `HTTPS_PROXY`, `ALL_PROXY`) set in the shell, about 30 suites under `scripts/mock/` run by hand failed with socket timeouts: their `urlopen` calls to the mock on `127.0.0.1` went to the proxy. CI never sees it, since `scripts/ci/python-tests.py` starts every suite with the environment `server_test_env.clean_environment` cleans. By hand, unset the proxy variables or go through `python3 scripts/ci/python-tests.py`; a new suite opens loopback URLs with `build_opener(ProxyHandler({}))` (as `scripts/mock/test_todo_view_mock.py` does) and needs neither.

## A moved file or block needs a test that fails when the old copy comes back (2026-11-20, #11476, #11543)

When a file is split or a block moves to another file, branches written before the move still edit the old copy. Their
merge stops on a modify/delete conflict (the old file is gone) or a content conflict (the block is gone), and the quick
resolution keeps their side, so the old copy returns beside the new one and nothing fails at once. After #11288 split the
MCP tool goldens into one file per tool, three PRs from before the split recreated the whole-list `mcp_tools.*.golden`
files, and the per-tool files went stale until the prompt size ratchets failed on main (#11476). A domain block put back
into `scripts/mock/mock_hub.py` after it moved into a module of its own would exist twice: the module's routes answer
first, and the old copy's state goes its own way.

- Ship the guard in the same PR as the move: a test that fails as soon as the old copy is back, by its file name or by
  the names the block defined, and that names the file to edit instead. `TestNoWholeListMCPToolGoldens` in
  `server/internal/hub` does it for the goldens; `MovedBlocksTests` in `scripts/mock/test_mock_domains.py` does it for
  the mock hub's domains (a new move lists its names in that file's `MOVED`).
- Leave a one-line pointer where the block was. It is what the person resolving the conflict sees on the other side of
  the markers.

## When Go on the hub is older than the repository (2026-11-03, #984)

- `test-server.sh` only says "server test environment preparation failed; tests were not started". `server_test_env.py` first runs `go env`;
  if hub Go is older than `server/go.mod` requires, it downloads a toolchain, but `go env -w` previously set `GOSUMDB=off` on that machine, preventing verification of the downloaded toolchain and failing `go env`.
  Not a database-config issue. Download the toolchain first in the repo with `GOPROXY=<go-proxy> GOSUMDB=sum.golang.google.cn go version`,
  then put `~/go/pkg/mod/golang.org/toolchain@v0.0.1-go<version>.linux-amd64/bin` in PATH and run the same command with `GOTOOLCHAIN=local`, `GOPROXY=<go-proxy>`.
- Only hub-cached dependencies build: if `proxy.golang.org` is unreachable, other packages report `[setup failed]`, making it look as though half the tests are broken.
- If hub Postgres is system-installed (no `persona-postgres` container), `db.sh` cannot create `persona_test` (its output is discarded), and role `persona` lacks CREATEDB:
  `internal/db`, `internal/db/dbtest`, `internal/hub` report `database "persona_test" does not exist`, while others pass.
  Run these three packages against temporary local `postgres:18-alpine` (`-p 127.0.0.1:<port>:5432`) with `PERSONA_TEST_DATABASE_URL`, then remove the container.

## Web grouping and collapse

- CSS `grid-template-rows: 0fr` and opacity only hide collapsed content, not remove links from keyboard focus/accessibility. Add `inert` to sidebar collapse containers. Status dots in group headings already have equivalent text; use `aria-hidden` to avoid reading "Working" twice.
- Web screenshots wait for menu items to render, not capture on the opening frame (which may show only the shell). Store theme preference in localStorage for light/dark checks; changing only the root class reverts after reload.

## WebSocket handshake tests

- Hub tests must register and remove clients through `addTestClient` / `removeTestClient`, or hold `h.mu` when already updating related state (#2855). A hello starts `fleetClientsChanged` in a goroutine, which iterates `h.clients` under the mutex; an unlocked test write can race with it even after the hello handler returns. Reads and iterations also need the mutex when clients can connect or disconnect concurrently.
- `Connection` is a comma-separated token list, so `Contains(..., "upgrade")` is not a valid upgrade check: it also matches `notupgrade`. Iterate `Header.Values`, split/trim, and `EqualFold` whole tokens. Test case, multiple fields, and substring rejection (#692).
- Go printable rodata can concatenate independent constants, making artifact scans see short terms across boundaries (2026-11-03, #692: protocol-header token adjacent to a VirtualBox NIC prefix). Locate sanitized context and check source first; do not mistake this for actual personal-value insertion, remove sensitive terms, or add exemptions. Store brand prefixes in display case and normalize comparisons; regress unchanged NIC detection, then build/scan actual artifacts for four platforms. Header validation alone does not remove this adjacency.

- Node 22's built-in `WebSocket` may emit only `error`, not `close`, when HTTP 401 / 400 rejects a handshake, leaving `readyState: CONNECTING`.
  Rejection tests must not wait for `close` or treat arbitrary network errors as correct authentication. Make HTTP upgrade requests and assert rejection status directly (wrong/missing token 401,
  wrong/missing subprotocol 400, wrong path 404); unexpected 101 or connection errors fail. Set an overall timeout and reclaim sockets. Still verify successful handshakes with a real WebSocket.

## Script-directory migration

- Moving scripts into subdirectories requires more than text replacement: check root derivation via `__file__` / `$0`, sibling Python imports, cross-directory subprocesses, Go path construction, and script transfer over SSH.
  Shared config and privacy scanning stay top-level; privacy scanning locates the root from its own repository, not the caller's cwd, which might select another checkout.
- `unittest discover -s scripts` does not discover subdirectories without package markers automatically. Discover `test/`, `release/`, `deploy/`, and `mock/` separately and check counts.
  Compatibility shell entry points only `exec` forward. Test paths with spaces, empty arguments, stdin, environment, and nonzero exits; do not actually publish or restart services to verify forwarding.
- After adding old/new environment-variable aliases, subprocess fixtures clear both before injecting config. Overriding only `PERSONA_CONFIG` inherits higher-priority `PERSONA_CONFIG`,
  accidentally reading release configuration. Cross-directory import regressions cover new variables, legacy fallback, and new-variable precedence on conflict; all paths target synthetic fixtures, never change production precedence for tests.
- Server-test launchers restore the service root only to prepare databases. Before Go, clear all `PERSONA_*` / `PERSONA_*` runtime variables and explicitly inject only both variable sets for the same dedicated test DB,
  with independent temporary `HOME` / `TMPDIR` and cleared Agent / XDG external-config roots. Preserve build caches/toolchains separately, preventing tests without fixtures from reading real defaults.
  `server_test_env.py` parses URLs to confirm `persona` → `persona_test`; custom DB names, identical targets, or parse failures fail closed, never assume isolation from string replacement.
  URL paths are not necessarily the driver's final database: pgx query `dbname` / `database` (including encoded keys) overrides them. The test launcher permits only explicit TLS/connection parameters, rejects unknown/duplicate keys, service/credential-file options, and invalid queries; clears all `PG*` environment; explicitly disables implicit password/TLS files in OS user directories. Anonymous `pgconn.ParseConfig` regressions check actual `Database` exactly, without connecting; do not infer past real configurations were affected.
  Validate the launcher itself with fake resolver / SSH / Go, not just cleanup functions. Production-root inheritance on 2026-11-03 opened the real DB and started sources/knowledge cleanup before the test hub failed to listen,
  deleting at least 14 old single-post JSON files. Healthy status, occupied ports, or eventual test failure do not prove no data changes. Preserve protected original logs, investigate read-only, never automatically restore/delete production data.

## Disconnection does not mean refusal logging finished (2026-11-05, #2243)

`TestProxyProtocolEndToEnd` checked refusal logs immediately after the client saw TLS disconnection, occasionally reporting `refusal not logged`. `proxyproto.Conn` closes before calling `Report`; when the client sees disconnect, the server callback may not have logged yet. The buffer mutex makes reads/writes safe, not callback completion guaranteed.

Tests check target text and subscribe to write notifications under the same lock, waiting for actual logs with an overall deadline. Unrelated logs cannot reset the deadline; missing logs still fail. Pure waiting regressions use `testing/synctest` to control timing, covering existing text, delayed/segmented writes, multiple waiters, unrelated writes, and timeout. Real end-to-end validation still checks rejection of headerless connections, then refusal logs. Do not change production close ordering, remove logging assertions, or hide races with fixed sleeps.

## Go tests requiring macOS built-in commands (2026-11-02)

- The Messages connector reads `chat.db` with macOS `/usr/bin/sqlite3` (personad has no cgo). The Linux hub lacks `sqlite3`, so these tests skip there.
  If the development Mac has no Go, cross-compile on hub: `GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go test -c -o x.test ./internal/connector/imessage`,
  copy to Mac, and run **in the package directory** (`testdata` is relative). Use the same approach for macOS-dependent `node` tests.
- `sqlite3 -json` prints nothing for no rows, not `[]`; check empty before parsing.

## Go tests dependent on time of day (2026-11-03, #656, #664)

- Tests deriving "an hour ago" / "now" from `time.Now()` and asserting by day/hour inevitably fail for a few minutes daily: messages cross local midnight into the next daily file,
  or hardcoded daily 09:00 tasks really become due just after 09:00 and run again. Both failed full hub tests at 01:00 UTC (09:00 Shanghai), blocking deployment.
  Fix test times within a day ("yesterday at noon") or far from trigger times (12 hours ahead).
- Reproduce without waiting: write a TZif file with one offset making its "now" the desired time. `TZ=/path/to/file` controls `time.Local`,
  `ZONEINFO=<directory>` containing `Asia/Shanghai` controls `time.LoadLocation`. Round offsets to minutes; second-level offsets mismatch times written back at minute precision,
  failing unrelated tests such as `TestBackfill` and appearing like regressions.

## Hub end-to-end waits must exceed managers' fixed delays (2026-11-05, #2960)

- Managers have fixed delays: `reconnectGrace` keeps disconnected nodes online for 5 seconds (`devices/warm.go`); repeated route failures back off 2, 4, 8 seconds, etc. in `pathEnded` (`devices/paths_choose.go`).
  `eventually` windows only a second or two longer time out under load (`TestCenterServiceConnector` waits 10 seconds for an 8-second backoff). Leave ample margin based on the delay, with comments explaining the wait.
- Running 8–16 copies of the same test binary on two cores via `taskset -c 0,1` exposes these more readily than serial `-count=50`.
  It also exposed a real `TestCenterServiceConnector` race (late probe reply overwrote 4002's "Pair again"), so timeouts are not always tight windows; inspect whether logged state is stuck first.

## Remote test snapshots must include every input outside `server/` (2026-11-03)

- `scripts/test/test-server.sh` synced only `server/`, but #630's `spec/fixtures` tests (`TestWireSamples`, resumption, resolution) read `../../../spec`,
  failing full hub tests with `open ../../../spec/fixtures/wire: no such file` and looking like your regression. The script now syncs `spec/` too (#642).
  Add any future test dependency outside `server/` to the script likewise (2026-11-03).
- Local Go-module `replace` is part of remote test snapshots too (#2028). After `server/go.mod` added `../sdk/go`, copying only `server/` failed setup with `replacement directory ../sdk/go does not exist`.
  Sync complete `sdk/go/` (including embedded schema) to `sdk/go/` in the same checkout's independent cache, retaining relative paths and `--delete` for stale SDK files. Do not share source directories with other test/deployment lanes.
  Extension conformance tests also read source-root `schema/extension/examples` (valid samples and `invalid/` cases); SDK embedded schema is not a substitute. Sync `schema/extension/` with the same relative-path/stale-file rules.
- Docs count too: `connector/conformance` previously compared an integration guide with fixtures (#2725); omitted `docs/` broke full predeployment tests. `docs/**/*.md` now syncs too (#2769).
  Synthetic rsync must do more than create empty directories: fake Go reads current SDK source, embedded files, and valid/invalid examples. Test cold start, updated source, and stale-file removal, preventing false passes with missing sync inputs.

## Sandbox tests use independent synthetic protected directories (#2052)

Isolated test launchers place HOME and TMPDIR under temp directories, which can also contain packages/data/runtime directories. bubblewrap's private `/tmp` creates parents for these mount points; ReadDir or writes against launcher HOME may touch newly created empty sandbox directories, not host-home content. Use an independent unbound synthetic directory with preset synthetic files. Inside the sandbox, verify files unreadable and directory unwritable; on the host, verify no write marker and original files unchanged. Never use a real personal home or treat directory existence as host-content leakage.

Retain allow/deny assertions for data directories, private tmp, and proxy. Negative controls deliberately bind only this synthetic directory: read-only binding must expose reads, writable binding reads and writes, causing the original tests to fail. Do not change production sandbox policy, remove read-only root mounts, or skip real tests.

## Wire samples carry a fixed feature list (2026-11-03)

- A new or removed feature flag no longer changes the shared hello and health wire samples: they carry a fixed list (`wireFeatures` in `server/internal/hub/wire_samples_test.go`, #11022), and `TestHelloAndHealthCarryTheHubsFeatures` checks the real one. Other new hello or health fields still need `go test ./internal/hub -run '^TestWireSamples$' -update`; check the diff contains only the intended fields.

## Starting sshd for tests (2026-11-03, #891)

- macOS `/usr/sbin/sshd` can run without root: `sshd -D -e -f <config>`, with `UsePAM no`, `StrictModes no`, and owned `HostKey` / `PidFile` / `AuthorizedKeysFile`; only the current user can log in. OpenSSH 10 needs `PerSourcePenalties no`, or repeated connections from one source incur delays.
- Other processes connect as soon as ports open (local port probes send `GET / HTTP/1.1`); many `invalid format [preauth]` logs are normal. Filter them before inspection.
- Privilege-separated sessions have two processes (`sshd-session: user [priv]` and its child), with sockets in the child. Kill listener descendants too when simulating disconnection; killing only `[priv]` leaves connections alive. Stopping sshd without descendants leaves orphan sessions parented to 1 indefinitely.

## Narrow-screen headless Chrome screenshots (2026-11-04, #1208)

- `Google Chrome --headless=new --window-size=390,844 --screenshot=…` does not lay out at 390px: minimum window width is about 500px, so a 500px page gets cropped to 390,
  looking like horizontal overflow (admin devices/plugins pages) but not a page bug. Use `--window-size=500,…` for narrow captures; true 390px needs DevTools device emulation.
- Admin screenshots need no hub: `cd web && npx vite --port <free-port>`, then `/admin/<page>?mock=app&theme=dark` (`?dialog=…` opens dialogs directly; `web/src/admin/mock.ts`).
  Point `--user-data-dir` at your own directory under `/tmp`, never the user's Chrome profile.

## Local port discovery probes fake model servers in tests (2026-11-04)

`TestInboxTriageWaitsOutAnExhaustedQuota` occasionally reports "20 items used 21 requests". The extra request is `GET /`, `User-Agent: persona-ports`,
from the local development node finding Agent listeners (`docs/operations/ports.md`) and probing the `httptest` port. Count only your own API
(`POST /v1/messages`); return 404 without counting other paths (`assistant/inbox_limit_test.go`). Use this pattern for similar fakes or they become flaky on node-running machines.
`TestOpenMacLeavesAgentAlertsToTheApp` also occasionally got an extra "push" with token `/` for the same reason;
`TestGateProxyReportCutsTheText` counted the same probe as a third request (2026-11-06, #6057). Its fake API (`fakeAPI`, `node/gateproxy_test.go`) has to accept any path the gate forwards, so it can't answer 404 to other paths. Instead it leaves out exactly the probe's shape: `GET /` with a User-Agent from `portProbeAgents`. That list holds today's `portProbeAgent` and the original installed-node User-Agent, which older nodes keep sending until they are replaced. When #6079 renamed the User-Agent, nodes already on the machines kept the old one, and the flake came back (#6225). Use such a filter only for a fake that must accept any path. Counting your own paths and answering 404 to the rest stays the default. Neither replaces real isolation in network-isolation or zero-connection tests: see the #2025 bullet on network-isolation listeners near the top of this file.
`TestAIElsewhereAsksNothingForOrdinaryMessages` counted it as a call to Lark (`calls: GET /`, #6274). The bot tests' fake open platform (`fakeOpenAPI`, `extmgr/bots_test.go`) now records only `/open-apis/` and `/callback/` and answers 404 to anything else: the default, which also holds for a node of any version.
A sweep then fixed every other plain-HTTP fake in `server/` that recorded, counted or failed the test on any request, so the next one doesn't have to flake first (#6282). Each now answers 404 to the probe's request before anything else: `GET /`, or for a proxy fake any request that isn't a proxy request (`r.URL.Host == ""`). The APNs fakes keep to `POST /3/device/…`. A new fake that records requests starts with the same guard. TLS fakes never see the probe, which speaks plain HTTP. `TestProbePanesKeepsEveryPanesProbes` (`node/ports_test.go`) counts the probe itself, so it can't filter it out and is left as it is.

## Fake model servers also receive the hub's own model calls (2026-11-04)

On its first run, `TestModelCallStopsWhenTheManagerStopsWaiting` never reached its fake server: after assistant connection, hub features (Agent-stage judging, etc.)
sent calls with the same model configuration, which the fake server blocked. Same-address/key requests share one gate (`ai/limit.go`), so the test request timed out waiting before being sent;
the stuck request also blocked `srv.Close()`, exhausting the package's 10-minute timeout. Identify the test's own request by a unique phrase in its body,
return a valid answer immediately for all others, and bound blocked requests too (10 seconds here) so cleanup cannot hang (formerly hub inbox_test.go; removed after #2856 moved model configuration to managers).

## Tests must provide data available only under particular MOCK_* flags (2026-11-05, #2334)

`PhotoSaveTests` needed a chat with a screenshot that existed only when mock launched with `MOCK_PHOTO=1`, documented only in a test comment. Full-suite and other-case mocks omitted it,
so the app opened a nonexistent empty chat ("No messages yet") and both cases always failed after "no button within 20 seconds", looking like broken Photos/save. Tests now add it themselves in setUp with `GET /mock/photo?on=1`
and restore the prior state in tearDown (`MOCK_PHOTO=1` still works). New UI tests prepare data through `/mock/…`, not mock-launch environment dependencies. If an environment variable is unavoidable, check in setUp and `XCTSkip` with an explanation when absent.

## For intermittent unique violations, find the two transactions touching the same row first (2026-11-05, #2704)

`TestTodoBotmuxChatMajority/store` failed about one in three runs with unique constraint `todo_agents_active` (23505). Initially blamed on shared test DBs, but 30 isolated repetitions reproduced 11 failures:
node-reported sessions automatically linked b2 to big in the background, immediately followed by the user manually assigning it to small. Transactions locked different to-do rows, never waited on each other, and neither saw the other's uncommitted link.
The later insert waited for the first commit, then collided with the unique index. Link/remove operations now first take an Agent-keyed advisory lock, then lock related to-do rows by ID (#2742 `lockTodoAgentMembership`), without affecting other Agents.
Assistant automatic links use `AutoLinkUnlinkedTodoAgent`: if already linked elsewhere at DB write time, do nothing instead of overriding manual choices (direct `AutoLinkTodoAgent` still moves it). Also remove unpersisted automatic links from memory, preserving user-removed ones; rollback recognizes only its own link (same LinkedAt), clearing "current to-do" only when that link is removed, never dropping a user's coincidental manual relink to the same to-do.

For intermittent constraint violations, reproduce with `-count=30`, enumerate transactions writing the same row, then check whether they lock the same thing. Do not pass through reruns, longer timeouts, or skipped store checks.

## Node tests time out in terminals: interactive zsh stopped in a background process group (2026-11-05, #3265)

In real terminals (hc / herdr panes, Terminal), `go test ./internal/node` leaves `TestLauncherRepair` stuck until the 10-minute timeout. CI, `test-server.sh` (ssh without `-t`), and Claude Code's Bash tool lack terminals and do not hang.
node launcher commands use interactive login shells (`zsh -ic`), previously placed in their own group with `Setpgid`. With a controlling terminal, interactive zsh tries to claim it but its group is not foreground,
so SIGTTOU / SIGTTIN stops it forever (goroutine stack at `launcherRepair` → `Cmd.Wait`). Foreground `personad node` in production terminals likewise hangs launcher probes, health checks, and repair until timeout.
`killGroupOnCancel` now uses `osproc.SessionGroupAttr()` (`Setsid`): child in a new session, no controlling terminal, with whole-group killing unchanged.

On test hangs, send `kill -QUIT` to the test process first, identify the test and blocked syscall, then judge environment issues. Interactive-shell children need either a new session or no separate process group.
## Testing extensions with a test hub on a Linux development machine (#3136, 2026-11-06)

- Without `machines` in hub.json, hub treats startup as first run and auto-connects local node (probing local herdr/claude). Test hubs set `"machines": []` and launch with `PERSONA_LOCAL_NODE=0`.
- Extensions are sandboxed by default, outbound hosts limited to the manifest through hub's proxy, so they cannot reach a local `127.0.0.1:<port>` proxy. If GitHub extension Proxy points at a local proxy, run `personad extension sandbox github off` on the test hub.
- Extension package.sh outputs packages to `dist/`; a private extension repository's `packages/` contains published packages, so do not install those by mistake (same-version packages do not reinstall either).
- Empty SDK `ext.Page.Action` results mean "redraw entire page". UI version 5 `lines` / `items` already count as a response; when nothing needs drawing (marked Seen), return non-nil empty `Items`.

## Built-in executable extensions run the test binary itself in tests (2026-11-06, #3500)

When a built-in extension's `runtime.command` is `personad`, the manager launches `os.Executable()` with manifest `args`; inside `go test`, that is the test binary (`extmgr.test voice`).
The test binary does not recognize `voice`, reruns the whole package recursively, and times out. `extmgr` `TestMain` therefore checks `os.Args[1]`: if `voice`, run as `personad voice`
(`voice.Run`). Future built-in executable extensions (not declarations only) need the same `TestMain` dispatch, so tests actually run them through hub forwarding.

## Stop test programs before their temporary directories go (2026-11-11, #4104)

`t.TempDir` removes all of a test's temporary directories in one cleanup, added at its first call, and cleanups run last added, first called. A test that first calls it after starting extension programs (`extensionTestHub` adds the cleanup that stops them) has its directories removed while the programs still run. A program the test's last change restarted (a provider switched back on restarts the extensions holding its references) then writes into a directory being removed, and the run fails with only `TempDir RemoveAll cleanup: … directory not empty`, about one run in three. `startTestExtensions` therefore calls `t.TempDir` before adding its cleanup. Anything else that starts processes writing into a test's directories must also add its stop cleanup after the first `t.TempDir` call.

## A stub PATH and HOME do not hide fixed paths a script checks (2026-11-12, #5434)

`install.sh` and `ensure-herdr.sh` look for herdr in `PATH`, `$HOME/.local/bin`, `$HOME/bin` and the fixed `/opt/homebrew/bin` and `/usr/local/bin`. Their tests set a stub `PATH` and a temporary `HOME`, so they passed on hosted runners but failed on any Mac with herdr from Homebrew: the scripts found the real one and never ran the fake installer. A test of a shell script that looks at absolute paths runs a copy pointing those paths into its temporary root, asserting the rewritten text is found exactly once so a moved path fails loudly instead of quietly testing the real machine again (`InstallHerdrTests` in `scripts/deploy/test_compressed_publish.py`). Keep anything the script finds relative to itself (`$(dirname "$0")/../persona_identity.py`) at the same relative place in the copy.

## Web screenshots on Linux unsupported by Playwright (2026-11-06, #3140)

- Symptom: `npx playwright install chromium` on Debian 11 says "does not support chromium on debian11-x64"; ubuntu20.04 packages also fail. Playwright MCP looks for `/opt/google/chrome/chrome` and cannot launch either.
- Download Chrome for Testing `chrome-headless-shell-linux64.zip` (version from `last-known-good-versions.json`), inspect missing libraries with `ldd`. Without sudo, `apt-get download` their debs, `dpkg-deb -x` into your own directory, and set `LD_LIBRARY_PATH` when launching.
  Extract Chinese fonts (such as Noto Sans CJK) likewise, add the font directory in `fonts.conf`, and set `FONTCONFIG_FILE`, or Chinese renders as boxes. Use installed `playwright-core`: `chromium.launch({ executablePath, env })`. Put each download in a new directory, outside the repository.
- Waiting: pages maintain WebSockets, so `waitUntil: "networkidle"` may never settle. Use `load`, then wait for specific page text. After fake-hub login with `/?token=mock`, wait for the sidebar before navigating to the screenshot target, or capture the login page instead.

## Making fake Claude call Persona tools in isolated-hub e2e tests (2026-11-06, #5073)

- Inbox handling chats submit results only through the MCP tool (`inbox_suggest`); a fake Claude returning plain text cannot test this. Read `mcpServers.persona` command / args / env from fake Claude's own `--mcp-config`
  (the JSON node passes to headless assistant chats), launch `personad mcp`, and send `initialize`,
  `notifications/initialized`, and `tools/call`. The tool identifies the chat by `PERSONA_SESSION_ID`, following the real node → hub → manager path. A test-written plan file decides which tool to call by matching a phrase in the message.
- `prompts.TestEveryTemplateIsUsed` searches Go and templates for quoted full template names (`"inbox/handling/sent"`); names built by concatenation (`"inbox/handling/" + name`) look unused. Write full names in constants.
- The old Inbox e2e cases in `run.py` for "hub keeps reading sources after the manager stops" no longer apply after sources moved into managers on 2026-11-05. The same six cases failed on master at the time too; when comparing, run them on `main` first.

## 后台测量停滞时，先区分未完成测量与残留管道（2026-11-06，#5766）

性能批次停在某一轮，不等于 App 仍在测量。本次 App 的 `frames-page-idle.json`、`frames-page-glide.json` 已生成，但前台测量 shell 已退出；后台 mock 的残留 shell 仍持有输出管道，父批次的 `grep` 收不到 EOF，一直等待。

- 先核对进程命令、PPID、工作目录、日志时间及 App 文档目录中的 JSON。已有完整读数另存并记录来源，不重跑、不覆盖。
- mock 直接在当前 shell 后台启动，保存 `$!`，重定向 stdin/stdout/stderr；用退出 trap 处理这个已确认的子进程。不要用后台子 shell 启动常驻服务后丢失 PID。
- 恢复前确认后续脚本有超时、失败结论和断点跳过。需要清理残留进程时，重新核实 PID；优先 TERM 本任务的 mock，让父进程自然解阻。不要按宽泛进程名或端口清理。
- 模拟器操作整段持有 `/tmp/playbox-sim.lock`；锁等待和单次测量都设超时，成功、失败、中断均有明确结果。近零或不一致的前后差异不能写成显著性能收益。

## macOS bash 3.2 can report an executable failure as exit 1 under `set -e` (#4467)

The Mac-only `test_hub_restart_mac.py` fixture used an empty, non-executable `personad` file. After blue-green switching was added, `hub-systemd.sh` executed it before restarting launchd. With `set -e`, macOS bash 3.2 returned 1 for `"$f" … || rc=$?`, which the script interpreted as a failed switch, rather than 126 for an executable failure. Bash 5 returned 126.

Use an executable fixture that logs calls and returns the documented result: 0 for a successful switch, 3 for no edge to switch. Assert that success avoids launchd and that the no-edge case restarts only the hub agent, skipping node agents. Run this Mac-only suite on macOS; Linux CI skips it: `cd scripts/test && python3 -B -m unittest -v test_hub_restart_mac`.

## macOS awk and sed stop on bytes that are not UTF-8 (2026-11-10, #2722; #11250)

In a UTF-8 locale, `/usr/bin/awk` on macOS (version 20200816) stops at the first invalid byte sequence with `towc: multibyte conversion failure`, prints the record's raw bytes and exits 2; gawk and mawk on Linux read on, so CI never sees it. `install.sh` read every launchd file with awk, and vendors often ship binary property lists: each printed its bytes, and the Python tests reading the script's output with `text=True` crashed with `UnicodeDecodeError`. An awk that reads files it did not write runs with `LC_ALL=C`; plain substring matches mean the same in bytes. The Mac in hand may have no binary plist at all: the regression test writes one with `plistlib` (`FMT_BINARY`) into its own home.

macOS sed (`sed 's/^/  /'`) stops the same way, with `sed: RE error: illegal byte sequence` and nothing after the bad line: check-fast's summary showed no log tail for a failed check whose output held such a byte (#11250). GNU sed reads on, and macOS `grep -q` still finds a match on another line. A sed over a command's output runs with `LC_ALL=C` too.

## mawk holds a pipe's lines until 4 KiB or EOF (2026-11-19, #11017)

mawk, the awk of Debian and Ubuntu (and so most likely of the hosted Linux runners, which install no gawk), reads a
pipe in 4 KiB blocks and handles no line until a block fills or the input ends; `fflush()` only flushes its output.
macOS awk, busybox awk and gawk handle each line as it arrives, so a filter meant to show a line live works on a Mac
and stays silent on Linux until the producer exits: check-fast's "waiting for a go slot" line came out after the whole
wait. `mawk -W interactive` fixes mawk but other awks reject it. A filter that must react to a line at once reads it in
bash (`IFS= read -r`, which takes a pipe a byte at a time) and hands the rest to `cat`; check the live behaviour in a
Debian container, not only on the Mac.

## Import guards read source files, not `go list` (2026-11-06, #5814)

`go list -deps` resolves imports for the host's GOOS / GOARCH and no build tags, so a `server/internal` import in a `_darwin.go` file, a `//go:build public` file (release builds use the tags of `server/build-tags.txt`, `public` among them) or a `//go:build ignore` generator passed the plugin guard on Linux. A guard that must hold for every build parses each `.go` file's imports with `go/parser` (`parser.ImportsOnly`), whatever its constraints, and keeps `go list` as a second view of indirect dependencies, run for every GOOS / GOARCH pair the server ships and every set of build tags it is built with (with `CGO_ENABLED=0`, as the builds are made): an `_arm64.go` file is as invisible to an amd64 host as a `_darwin.go` file is to Linux, and a `// +build !public` line still counts.

Walking files has pitfalls of its own. Reviews of `server/plugins/guard_test.go` found each of these: the first six let plugin or internal code into a binary unseen, the last two failed on code that is fine.

- The go tool leaves `testdata`, `_x` and `.x` directories out of `./...` patterns, but a package there can still be imported by its explicit path: scan them too.
- `filepath.WalkDir` does not follow a linked directory: a link to a plugin from `internal/` builds the plugin into the hub unseen. Check where every link to code leads, including into a nested module the walk skips (the go tool builds it as the server's through the link).
- A module prefix written with a trailing slash (`…/server/`) never matches the module's root package.
- A package in the directory that holds the plugins (`server/plugins` itself) is a way around "only programs import a plugin".
- Go's `internal` rule limits only who imports the internal package itself: a package next to it can re-export it to anyone. Hold a program's whole tree to itself instead: no package outside `cmd/<x>` imports one of `cmd/<x>`'s, and no link from outside leads into it.
- A package the guard lets through by its path (the SDK) must be read the same way: a link or a tagged file in it reaches past the guard. Likewise a local `replace` or a `go.work` module is this repository's code under another path: judge `go list` results by `.Dir` as well as by import path, for local code only (the main module and directory replacements: a module cache may sit inside a checkout). A `replace` by a published version of this repository's own module has no local directory to judge: refuse it in go.mod and go.work. And since a published module could import a plugin by its path, the programs that run no plugin are held to `go list -deps` too.
- Decide what a directory is (a plugin's program or not) from its non-test files: a test importing a plugin is not linked into a program.
- Skip files whose names start with `.` or `_` (the go tool never builds them; editors leave lock files so named), but not directories so named.

## Playwright's "visible" counts a drawer parked off the screen (2026-11-09, #1643)

- At phone width the web sidebar is a drawer moved off the screen with a transform, not hidden: `getByRole(…).filter({ visible: true })` and `isVisible()` still count its buttons, and a click on one waits out its timeout. Reach a control at that width from the page that shows it (the Agents page's New agent, `web/e2e/launcher-health.mjs`), or scope the locator to `main`.
- `?token=mock` pairs asynchronously: wait for something only a paired page has (`main textarea`) before setting `localStorage` and reloading, or the reload lands on the sign-in page.

- Real WebSocket nudge tests need observable ordering (#2297): wait until all input audio frames arrive and the answer reaches the app before sending a late caption. Answer text/result frames on receipt rather than after an arbitrary delay. A negative nudge assertion still needs its full observation window; watch incoming frames and fail immediately on an unexpected count, then check the count at the deadline. Do not replace a sleep with an unconditional timer wait or use `synctest.Wait` around real sockets.

- Tests that isolate the runtime home must clear both supported environment names through `compat.EnvNames`; child environments use `compat.ReservedEnv` to filter the reserved prefixes. Reuse the compatibility boundary rather than copying legacy literals into new test carriers (#2347).

- Manager hello also runs on Hub instances without an updater, including the mesh harness. Withhold node deployment until the policy reader is available; do not make hello initialize an updater or assume a startup opt-in overrides an unread persisted choice (#2616).

- In the mock hub's request handler, a local binding can shadow a module helper even in a route that never runs: delayed closures capture the local cell and fail before publishing. Name inbox records explicitly so the transcript item factory remains callable, and retain subprocess stderr when diagnosing missing asynchronous pushes (#2523).

## Warm allocation measurements need one processor (2026-11-10, #3390)

- Disabling GC preserves `sync.Pool` contents but does not keep a goroutine on the processor whose private pool was warmed. A switch during a byte-allocation measurement can charge a cold JSON encoder's buffer growth to the operation. Measure an explicitly warmed steady state with `GOMAXPROCS(1)` during the window and restore it afterward, as `testing.AllocsPerRun` does. Check warm-up errors and retain the original byte limit; a historical extra-copy implementation measured by the same helper must still fail that limit. This isolates the measurement from processor migration, not from unrelated allocating goroutines: do not run these process-wide measurements in parallel.

## Measuring latency on the shared Mac (2026-11-10, #3067)

- The Mac other lanes build on runs at a load average of 100–260 on 20 cores. The same path's small-message round
  trip moved between 6 and 47 ms (p50) within minutes, and a hub redeploy by another lane can land mid-run (check the
  hub processes' `etime`). Compare by interleaving the variants, several rounds, medians, with the load average logged.
- A result from a loaded machine can reverse once it is quiet: at a load of ~400, asking for VNC updates early looked
  239 ms faster for typing (3 of 6 pairs); at 25–80 it was slower in all 5 pairs (median +81 ms). Decide on runs made at
  a low load, interleaved (ABBA), and say the load next to the numbers.
- Driving the real desktop (`server/cmd/vncbench run`): other lanes' windows and dialogs open on top of yours at any
  time. Send a click, a wheel notch or a drag only where your own page is seen on the screen (vncbench's target
  checker), type only while your page says it has the keyboard, and bring your browser forward with `open -a`, never
  by clicking. A window's rounded corner clips a marker drawn in it: keep markers inset.
- An agent's shell is not the login session: the login keychain is locked there and `security … -w` exits 36 (no user
  interaction allowed). A secret the user stored in the keychain can't be read; use a 0600 file outside the repository.

## A test binary that is a container's PID 1 never reaps orphans (2026-11-11, #4363)

- `docker run --entrypoint <test binary>` without `--init` makes the test binary PID 1, and every orphan is reparented to it. Go waits only for the children its `exec.Cmd`s started, so a background child killed after its shell exited stays a zombie, `kill(-pgid, 0)` still finds its group, and process-group tests fail. On `main`, `TestSecretRunTimeoutKillsTheGroup` and `TestSecretRunTimeoutEndsWhatIgnoresSIGTERM` failed in a `postgres:18-alpine` container this way and passed with `--init`.
- Run such tests with `docker run --init` (Docker's own init, no image to pull), and compare with `main` in the same container before blaming a change.

## A throw-away HOME also hides the Go settings (2026-11-12, #5191)

- `testhome.Main` gives a test binary an empty `HOME`. The Go defaults hang off the home directory: the env file
  `go env -w` writes (`GOENV`, where settings such as `GOPROXY` live), `GOPATH` and so the module cache, and the build
  cache. A `go list` the hub's architecture guard started lost all of them, went to the default proxy with an empty
  cache and failed after a minute with only `exit status 1`, on `main` too, so it looked like someone else's breakage.
- `testhome` now resolves `GOENV`, `GOPATH`, `GOMODCACHE` and `GOCACHE` with the real home before swapping it and pins
  them. Anything else that reads the home directory for its own config (git, ssh, a CLI's login) has the same problem
  under testhome: pin what the child needs, or give the test its own config.

## A throw-away HOME also hides Docker's CLI plugins (2026-11-13, #6131)

- The Compose config checks run `docker compose` under a temporary `HOME`, so Docker reads an empty config directory and
  finds no `docker-compose` plugin where Docker Desktop and Colima register it (`cliPluginsExtraDirs` in the user's
  `~/.docker/config.json`). The checks skipped only on the text `is not a docker command`; Docker 29 prints
  `unknown shorthand flag: 'f' in -f` for the same missing plugin, so they failed instead, on `main` too.
- Don't detect a missing tool by one wording of its error: ask it directly in the test's own environment
  (`scripts/test/compose_cli.py` runs `docker compose version`) and skip with how to enable the check
  (`PERSONA_TEST_DOCKER_CONFIG`, `docs/operations/hub-update.md`).

## A throw-away HOME also hides the login keychain from codesign (2026-11-13, #6368)

- `codesign -s <certificate>` looks for the identity in the keychains of `$HOME`: under the temporary `HOME` the
  nodeupdate tests set in `TestMain`, a certificate that signs fine from the same shell says `no identity found`. The
  real-certificate test (`TestRealCertificateSigning`) sets `HOME` back to the user's home (`os/user`) for itself.
- And the keychain must be open to the process: run from a `Background` session (an ssh login, a terminal an Agent
  drives) codesign fails with `errSecInternalComponent`. Run such a test where the resident node runs, in the login
  session: a one-shot job in `gui/$(id -u)` (`launchctl bootstrap`, then `bootout`); `launchctl managername` says `Aqua`
  there.

- A temporary HOME does not keep a real node to its own transcripts (#8129). Besides its own config directory, a node takes the `CLAUDE_CONFIG_DIR` of every running claude process of the user (`server/internal/node/claudedirs.go`), so an end-to-end check that runs `personad node --stdio` beside the developer's own claudes lists their transcripts too, read-only, and can match sessions it never made. Pick out the run's own sessions (by ID, folder or engine, as `integrations/paseo/e2e/run.mjs` does), or run the node with the session scope (`PERSONA_NODE_SESSION_DIR`, as `integrations/paseo/e2e/codex.mjs` does) when every session it should see is under one folder. Keeping the node from that scan is no fix either: tried for the Paseo e2e (an environment switch, since dropped), it changed the node's timing enough that races elsewhere (the node tying a message's echo before the runtime's list named its transcript, a check reading the list before the node pushed it after the bridge left) failed four runs in five on Paseo 0.15.4, where the same bridge with the scan on passed five of five.

## Workspace naming review reproductions (#7100)

- Exercise a task save with Assistant defaults changed by another editor. Check the placeholder against the actual preview after the save, and keep the explicit draft separate from the refreshed saved baseline/revision. A failed follow-up read must not replay an acknowledged write or reset the preview computer/layout; include an empty computer choice as well as a different valid one.
- A lazily imported editor can still add first-screen bytes through shared modules and preload metadata. Keep the general repository API free of React and reuse the existing editor-owned request-context guard when that avoids an extra shared chunk. Compare a normal build with one fixed clean main; retain the startup budget and the guard's credential/manager lifetime checks.
- Tailwind's global stylesheet can also include utilities used only by a lazy editor. Keep that editor's unique rules in its own imported stylesheet, applying the existing utilities through a reference to the shared design tokens. This preserves its exact layout without charging those rules to every first screen.
- Seed legacy PRs in persisted task progress without first reading the repository API: that read imports them. Pause the synthetic Node after source capture and inspect the real PostgreSQL association set and import receipt. Naming already imports before capture; a link-only edit changes the association revision but not the selected preparation digest. Changing a selected ref must still conflict, and a saved operation must survive restart without resolving again.
- A Windows-looking `NodeContext` does not make a Linux process use Windows `filepath` semantics. Run the Windows-specific naming tests on Windows, or explicitly label any extracted lexical-path fixture and its substitutions. Cross-compilation checks compilation only. Keep settings syntax, target-native resolution, Node authorization and actual filesystem behavior as separate claims; never widen path permissions to make a host-mismatched reproduction pass.
- Windows namespace prefixes can make `filepath.Rel` reject an equivalent ordinary path as a different volume. Test both workspace and private-state spellings, including drive and UNC forms; a failed containment comparison is not proof that a path is outside private state. Naming rejects these namespaces rather than stripping their special semantics.
- A lazy settings subtree needs its own `PageBoundary` inside the surrounding settings page. Reproduce a chunk failure by blocking the browser's actual module request, then verify that sibling settings remain editable and their drafts can still save. Restore the resource and use the existing Reload action to verify recovery of the module and persisted rules; a mocked component or a JSX-shape assertion cannot establish that behavior.

- An export-only change in a deferred editor need not reduce the first screen. Measure it, revert an ineffective trial, and inspect the shared code that startup actually ships. Remove duplicated work there while retaining validation and request-lifetime guards; keep the original budget and verify the normal app/public build.

## Raw TCP test listeners get the port probe too (2026-11-19, #10659)

`TestProxyRouteCarriesTheLinkAndItsStreams` saw one more "direct attempt" than the link made. The extra connection was the
local node's port probe (`GET /`, `User-Agent: persona-ports`), which finds any new listener, plain TCP included, not only
HTTP fakes. A raw listener that counts connections (`gatedDirectListener`, `link/ssh_test.go`) now counts only those
that open with a TLS record (first byte `0x16`) and replays that byte before forwarding. Two traps on the way:

- The first guess, a Happy Eyeballs second dial for a name with both address families, was wrong. Read the stray
  connection's first bytes before explaining it.
- Wrap the replayed connection by embedding the `net.Conn` interface, not `*net.TCPConn`. Embedding the concrete type
  promotes `WriteTo`, which `io.Copy` uses instead of the overriding `Read`, so the replayed byte is silently dropped
  and every forwarded TLS handshake fails.

## A test repository's first branch follows the machine's `init.defaultBranch` (2026-11-19, #11017)

A fresh test repository's first branch is whatever the machine's `init.defaultBranch` says: `main` on one machine,
`master` on another. A test that then runs `git branch main` or `git checkout -b master` fails only where the setting
matches. Name the first branch (`git init --initial-branch=<name>`, or `git branch -M` after the first commit), and run
the suite with `GIT_CONFIG_GLOBAL` pointing at a file that sets `init.defaultBranch` to `main`, then to `master`.

## Signal tests: a command that outlives the wait, a runner that ignores the signal (2026-11-19, #11017, #11026)

- A test that waits for a signal to end a command must give the command far longer to live than the wait (#11017).
  `test_heavy_slot.py`'s command slept 20 s under a 20 s wait. A lost signal could not pass (the command then exits
  with its own status and catches nothing, which the test checks), but a timeout said nothing: the command never got
  the signal and was still sleeping, or it got it and its parent had not exited yet on a slow machine. Let it sleep ten
  times the wait, keep the wait generous, and on a timeout say which side stalled.

- A signal test passes alone and hangs when its runner started it with that signal ignored (#11026). A non-interactive
  bash starts its background jobs with SIGINT and SIGQUIT ignored, and `nohup` or a detached shell ignores SIGHUP; a
  signal ignored when a shell starts can never be trapped there, so the script under test never sees it. GNU `timeout`
  (coreutils 9.12 measured) passes an ignored SIGINT and SIGHUP on to its command and resets only SIGTERM, so a check
  run under it keeps them ignored. `test_check_fast.py`'s SIGINT and SIGHUP tests timed out when check-fast ran that
  suite (each check is a background job under `timeout`; 4 of 6) and passed on their own. They now start check-fast
  with SIGINT, SIGHUP and SIGTERM at their defaults (`preexec_fn`, as a terminal would), and one test runs them with the
  runner ignoring the signals (#11250). Do the same in a new signal test before blaming the change.

## A script started with its stderr closed (2026-11-20, #11250)

A caller can close a script's stderr (`2>&-`, some service managers). Every later redirection that copies it (`4>&2`)
then fails, and whatever the script runs that way does not run: check-fast's jobs all ended "no status". Worse, the
next file or pipe the script opens takes the free fd 2, and stderr writes land in it: macOS bash 3.2 leaves a process
substitution's pipe there (`lsof` shows fd 2 as a PIPE after `done < <(…)`), which hid the failure in a run whose plan
had used one. A script that copies stderr checks it at the start (`: 9>&2 || exec 2>/dev/null`) and goes on without
what it would have shown there.

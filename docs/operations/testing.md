# Testing

## Commands

Run the fast local checks from the repository root.

```bash
scripts/test/check-fast.sh
```

The migration freeze compares the working tree and index with the common ancestor of the task and `deploy`. An explicit base can be passed as the first argument.

Build and test on the Linux devbox as described in [the agent guide](../../CLAUDE.md#build-and-verify). Use the isolated database profile and the partitioned runner with `-p 12` for large server packages. Use `npm ci`, `npx tsc -b` and `npx vitest run` in `web/`. The web browser runners use a mock hub on a dedicated port.

## Bounded server package verification

Use the partitioned runner for bounded local verification. Diagnostic runs with `-json` or `-v` keep their normal event stream. The runner can verify either package set under a 600-second deadline:

```sh
cd server
timeout --kill-after=5s 600s python3 ../scripts/test/go_test_partitioned.py go test -count=1 -p 12 \
  ./internal/assistant ./internal/assistantskills ./internal/assistantwire ./internal/hub ./internal/node
timeout --kill-after=5s 600s python3 ../scripts/test/go_test_partitioned.py go test -count=1 -p 12 \
  ./internal/apierr ./internal/assistant ./internal/assistantskills ./internal/assistantwire \
  ./internal/hub ./internal/i18n ./internal/node ./internal/prompts
```

Use an isolated test environment, with a disposable database when database coverage is required. The deadline includes discovery, compilation and execution. Exit 124 is a timeout; another
nonzero exit is a failure, even when every partition finishes. Report existing opt-in/platform skips separately.
The runner preserves `-skip` and `-race`. Use `-run` for a focused test selection.

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

Set `GOFLAGS=-trimpath` for the local build, vet, and test runs. The test launcher passes the value on explicitly because its temporary `HOME` would hide a `go env -w` setting.

Tests find repository files from the working directory (`go test` runs in the package directory), never through
`runtime.Caller`: under `-trimpath` its file names are module paths (#1133).

On a machine with many worktrees, set it for every Go command:

```sh
go env -w GOFLAGS=-trimpath
```

The trade-off: binaries and stack traces then name source files by module path
(`github.com/nwverse/persona/server/...`) instead of the local directory, so a debugger (Delve, an IDE) needs a
source path mapping to open them.

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

## Mock hub data

Screenshots must contain no real business data. `scripts/mock/mock_hub.py` returns only made-up data (three machines, Agents in several projects, assistant chat,
one question, one plan approval, Lark + mail Inbox), enough for main pages.

Real-hub Inbox end-to-end tests (triage, event synthesis, AI twin decisions) use `scripts/mock/mock_model.py`: a fake Anthropic Messages endpoint (`POST /v1/messages`),
returning fixed JSON according to prompts (event synthesis merges the batch into the first open item in the input, or creates one; question marks count as Needs you). `POST /_mode` switches to failure, exhausted quota,
or invalid output; `GET /_calls` inspects received calls. Point the manager's `manager.json` `inbox.triage_ai` (also used by event synthesis) and `judge_ai` at it; usage is at the file top.

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
- Remote copy / compress progress (#6113): `MOCK_FILE_OP_DELAY=<seconds>` holds copy and zip POSTs open while `GET …/files/operations/{id}` reports synthetic file / byte progress; disconnecting the POST marks the operation cancelled and leaves no output. `testRemoteCopyProgressResumesAfterEarlyReturn` leaves before the first delayed row appears and requires progress and Cancel after returning; For the real web panel, start Vite with `PERSONA_HUB_URL` pointing at that mock and run `node web/e2e/file-operations.mjs http://127.0.0.1:<web port> --hub http://127.0.0.1:<mock port> --shots <directory>`; add `--large-text` to repeat it with the app's largest text setting. The browser check also leaves and returns to the operation's folder to verify restored progress and Cancel, and completes an operation while away to verify that its workspace notice survives returning. Restart the mock between runs so earlier synthetic outputs cannot contaminate the cancellation assertion.
- Temporary-code pairing (#2766): `MOCK_PAIR_TOKEN=jp_…` behaves like `personad hub pair`: usable everywhere before `POST /api/v1/auth/tokens` exchange, then for `MOCK_PAIR_GRACE` seconds (default 10 instead of hub's 2 minutes); a second exchange returns 401. Exchanged device tokens (`jd_mock_<n>`) remain valid; every 401 logs a line. Paired sockets close after grace like hub; `ws open host=… token=…` identifies handshake tokens. `MOCK_PAIR_TRADE_DELAY=<seconds>` delays exchange, `MOCK_PAIR_DROP=<seconds>` drops the first temporary-token socket once. Add `MOCK_HUB_NAME` and `MOCK_MESH_PEERS` with `center: true`, address `http://localhost:<port>`, to reconnect through the group's center (another address). Correct only if no 401 after grace and title isn't "Connecting…".
- Browser (docs/extensions/computer-use.md): `MOCK_BROWSER=missing|setup|ready` enables `ego` and MacBook (not installed / installed without first setup / ready, analytics on; `MOCK_BROWSER_ANALYTICS=off` disables analytics); Mac Studio disabled/uninstalled. "Install ego lite" / "Open ego lite" advance every `MOCK_COMPUTER_STEP` seconds. With `MOCK_EXTENSIONS=1`, list includes "Browser (ego lite)".
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
- Chat search (#6281, `docs/assistant/assistant.md`): `MOCK_CHAT_SEARCH=1` turns the long Persona conversation into one whose marker words each sit in one place only (title, the user's words, a reply, a thought, a tool output folded and not, early history, a quoted context, a card's answer, an injected message) and pages its history by turns, `/mock/reset` included. Web: `node web/e2e/chat-search.mjs`. `GET /api/v1/assistant/search` answers over the mock's items by the same rule as the manager (#6282,; `MOCK_ASSISTANT_SEARCH=0` is a hub from before it (no `assistant_search` feature, 404).
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
- New Agent's attachments: `MOCK_UPLOAD_KBPS=96 PORT=<dedicated port> python3 scripts/mock/mock_hub.py` makes an upload take half a minute. `/mock/uploads` also names each upload's computer), another computer taking the files out with its note, a Codex launcher carrying a photo on its start request, and a to-do handoff keeping its context block first. With `MOCK_OMP=1` too, the mock lists an `omp` launcher and `testOmpStartsWithItsFirstMessageAndNoOptions` checks that omp gets no Permissions & Thinking and no attachments, and that its start carries the first message and no mode, effort, model or `async` (`docs/integrations/omp.md` "Starting from the apps"); without it that test skips.
- An offline machine beside an online one (#11283): `MOCK_OFFLINE_MACHINES=<id>[,<id>…] PORT=<dedicated port> python3 scripts/mock/mock_hub.py` takes those machines offline and keeps what they run (`studio` offline beside the online MacBook, both macOS; `server`, the assistant's own computer, turns the persona's face offline).
- `/mock/reset` takes it offline again), reopens New Agent and checks the three cards (the offline one not pickable), a machine card restoring its model, two cards filling every field with nothing started (`GET /mock/started`), Remove from Recent and Clear Recent Setups.
- Away recap in message flow (#1360): `MOCK_RECAP_LONG=1` adds eight turns before "Review PR #42 changes", taller than one screen, ending in a recap. `MOCK_RECAPS=1` keeps the earlier two as small bars when three are folded to one line.
- Stable content when scrolling up (`ChatView` freeze): `MOCK_STREAM=1` adds a step/reply every 2 seconds after opening first session, 12 times;
  in App container `Documents/freeze.txt`; final `RESULT:` should be 0pt movement.
- The running work line naming its step: `MOCK_LIVE_STEP=bash|command|reads|parallel|thinking|thinking-folded` puts the weather-widget chat a minute into a new turn whose newest step is still going; `bash` a command Claude described (its description alone, cut at its end), `command` a long command with no description (cut in its middle, the clock whole), `reads` three files read at once ("Reading 3 files"), `parallel` a search with two reads beside it ("+2"), `thinking` a thought after the calls came back, pushed `MOCK_LIVE_STEP_PUSH` seconds (default 3) after the chat's latest page (a page folds finished work, a push never is); `thinking-folded` has that thought in the page from the start, inside the fold (`fold.latest_thinking`).
- New Persona chat (#511): mock pushes session list immediately, transcript starts empty, first sentence appears/pushes after `MOCK_ASSISTANT_TRANSCRIPT_SECONDS` (default 1).
  After sending, chat retains that sentence (never "No messages yet"), selects new sidebar chat with Working spinner. `MOCK_ASSISTANT_START_SECONDS=<seconds>` delays POST;
  inspect bubble and "Persona is getting ready…" while waiting.
- A new chat's first message whose start failed (#1368): `GET /mock/start-fail?n=<count>&code=<code>&delay=<seconds>` cuts the next n starts off (`502`, default `assistant_in_doubt`, delivered unknown) and delays every start by `delay`; `n=0&delay=0` puts it back.
- Lark multi-account (#1321): `MOCK_LARK_ACCOUNTS=1` adds "Me", "Work account", one signed-out account to `/api/v1/lark/accounts`; `ol_account=2` reads Work chats, top Inbox item is from Work.
- Messaging sources (`messaging` extension point, #2138): `MOCK_MESSAGING=1`, `GET /api/v1/messaging` lists `lark` (extension `larkext`); `/api/v1/messaging/lark/…` follows `docs/extensions/extension-messaging.md`. `scripts/mock/lark_messaging.py` reads mock `/api/v1/lark/…` and converts, applying extension display rules for encrypted external/unlocked display, recall/edit history. Settings is full extension UI `POST /api/v1/extensions/larkext/-/ui/display`; hello/health add `messaging` feature, and every `{type: lark}` push also emits `{type: messaging}`. Can combine other fixtures (`MOCK_LARK=1`, `MOCK_LARK_TRAINER=1`, `MOCK_LARK_NATIVE_OLK=1`…).
- Hub without Lark gateway extension (#1530): default features include extension-provided `lark`, `lark_moments`; `MOCK_NO_LARK=1` removes them and `lark_attachments`, all `/api/v1/lark/…` return 404, no Lark connector. Messages, Settings › Connectors › Lark, translation's Lark chat option vanish; Lark docs/Moments links open as regular web pages.
- Lark /agent context (#876): `MOCK_LARK_AGENT=1` turns the "Schedule Friday…" request into Lark /agent (instructions + 7 context items in `<persona-context>`, only request outside).
- Conversations an extension opened (#3758): `MOCK_AUTO_CONVERSATIONS=1 MOCK_EXTERNAL=1` gives the Lark /agent auto chat a first message of context blocks and a request, and adds one opened by another extension (`…000000000310`, origin `extension`). Their rows carry the source mark and their first message "From Lark /agent" / "From an extension". With `MOCK_EXTENSIONS=1 MOCK_OPENER_ICONS=1` too (#10358), the two extensions that opened them are installed with icons on the hub's icon route (`larkext` as a made-up third-party "Chat Bridge" that reuses `lark.agent`, `example-ext` as "Example Docs"), so the apps draw and name the opener.
- Background supervisor (#4927, #5082): `MOCK_BACKGROUND_SUPERVISOR=1` adds the built-in conversation (origin `task`, no `origin_ref`) and names it in `GET /api/v1/assistant` (`background_supervisor`); with `MOCK_TASKS=1` the reviews in the main chat's feed carry `by: "background"` (with `MOCK_TASKS_REPORTS=1` an accepted one and a rework, else one accepted). Without the flag the mock still has the switch (`main_supervises_own_tasks`, saved by `PUT`) and answers `GET/PATCH /api/v1/model-config/purposes/assistant.supervise` (never in the catalog); `MOCK_LEGACY_HUB` has neither.
- Availability follow-ups for the clients (#5802 to #5804, #5806): `MOCK_RULE_SUGGESTIONS=1` attaches a suggested standing rule to the PR check's decision (`ask.suggestion`, with `MOCK_TODO_ASKS=1`), to the network tests' question card (`interaction.suggestion`) and, with `MOCK_TASKS=1`, to the open plan (`plans[…].suggestion`, filled in where the plan is read), and answers `POST /api/v1/assistant/rule-suggestions/{id}/always` and `…/not-now` once (then 404, as for an id it never offered); `MOCK_ASK_HELD=1` holds the PR check's decision for the briefing (`ask.for_briefing`, with `MOCK_TODO_ASKS=1`); `MOCK_TODO_ASK_LONG=1` gives that decision a question and a recommendation of several lines and 12 hours of waiting, for checking the Needs you tray's decision card with long text (with `MOCK_TODO_ASKS=1`); `MOCK_BRIEF_SETUP=1` adds the recommended-setup lines to the morning briefing's `brief` item (with `MOCK_TASKS=1`); `MOCK_SUPERVISE_LISTED=1` lists `assistant.supervise` in `GET /api/v1/model-config` as the last built-in purpose and gives the built-in purposes a `description`. Without the flags none of the fields is sent. Always on the decision's suggestion also makes a grant that `GET /api/v1/assistant/standing-authorizations` lists and `DELETE …/{id}` revokes (only with this flag; the web's Settings › Assistant profile reads it). `GET /mock/reset` offers the answered suggestions again and takes back what Always made. The web's check and screenshots are `web/e2e/rule-suggestions.mjs` (`docs/web/web.md`).
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
- Standalone terminal (web, `docs/integrations/terminal.md` section 19): `MOCK_TERMINAL_PANES=1` (hello `terminal_panes`, `GET/POST /api/v1/machines/{mid}/panes` returns fake spaces/tabs/panes; `pane:` opens fake zsh; `panes/agent` in a listed shell adds an idle session bound to it and answers its id, which `/mock/reset` removes).
- Phone terminal takeover: `MOCK_TERMINAL_CONTROL=1` (hello `terminal_control`, fake Claude arranged to App columns × rows, typed text in input,
  `GET /mock/terminal_input` inspects bytes); `MOCK_TERMINAL_BUSY=1` shows "Another program on the computer is using this terminal".

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
answers as before.

The opt-in SSH → Persona
Link scenarios live in `scripts/mock/link_switch_mock.py` (#10671): `MOCK_LINK_SWITCH=1` makes `server` an eligible Linux SSH
computer, and `GET /mock/link_switch?state=available|checking|installing|connecting|rolling_back|succeeded|failed|unconfirmed|unavailable|refused|forbidden|uncertain|hold|old`
changes the fixture and pushes the machine list. `hold` keeps the POST answer out until `?release=1`; the no-query response reports
the state and start-attempt count. With the switch off, existing mock screens do not change.
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

## Throwaway hubs and herdr (#10114)

A throwaway hub (`personad hub run` under an isolated `HOME` / `PERSONA_HOME`) whose machine is `kind: local` starts a
node, and that node starts herdr's headless server under the test `HOME` when none answers there (`herdr_autostart`).
Give such a machine `"herdr_autostart": false` (and `"botmux": false`) unless the check needs herdr, as
that has no panes (`StopUnusedHerdr`, `server/internal/node/herdrstart.go`); a server with panes stays. A cleanup
still ends by checking `ps eww` for processes whose `HOME` is the test's, and stops any it finds through their own
socket (`HERDR_SOCKET_PATH=<test socket> herdr server stop`), never the user's herdr.

## Privacy scan

Run `python3 scripts/privacy_check.py` to scan tracked worktree files, or use `--staged` for the index. The scan checks private values, private addresses, and npm lockfile sources. It reports the location and type of a hit without printing its value.

Install the hooks with `git config core.hooksPath scripts/hooks`. The commit hooks scan staged files and the recorded commit message. The push hook scans each pushed branch's changed files and commit messages, starting at the previous remote tip or `deploy` for a new branch. Use `--range <base>..<head>` to run that scan directly.

Use placeholders such as `<hub address>` and `me@example.com` in examples. To allow an ordinary word that coincides with a local machine name, put a `!word` entry in the local `private-words.txt`. To test the scanner and hooks with isolated Git repositories and configuration, run `python3 -m unittest discover -s scripts/test -p test_privacy_check.py`.

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

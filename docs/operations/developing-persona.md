# Developing Persona: recording and exporting a device's diagnostics

**Who it serves**: people developing or investigating Persona (the owner, testers), not everyday Assistant users. So it
lives only in Settings › About, is off by default and shows nothing anywhere else (#9460).

A switch, **Developing Persona**, in the web client's About. While it is on, the device records its own logs, errors and
energy use; **Export Diagnostics…** packs them into one zip the owner can share or send to an Agent. Nothing goes to the
hub or anyone else unless the owner sends the file, and then only as an ordinary file in one of their own chats.

| Client | State |
| --- | --- |
| Web | #9617: switch, recording, export with Send to Agent and Download |

## The switch

- Settings › About, under Version. Stored on the device, never
  synced. Footer: what it records, that the data stays on the device, that turning it off deletes it.
- While on, the group adds **Recorded** (since when, how much) and **Export Diagnostics…**.
- Turning it off deletes everything recorded. When something is recorded it asks first: **Export First…** opens the
  export page, **Turn Off and Delete** goes ahead.

## What is recorded (web)

The same lines and limits in spirit, kept in IndexedDB (`persona-dev-mode`) at most 24 hours and 5 MB. The switch is
`localStorage` `persona.devMode` (when it was turned on) in Settings › About Persona; turning it off deletes the records,
asking first when something is recorded.

- `minute` (a minute while the page is visible, one per hidden stretch): hub requests by route pattern (count, bytes as
  declared, median and longest time to the answer, status codes), hub WebSocket frames by type, bytes, connects and drops,
  seconds per page (the address's first word: `s` for an Agent's chat, `settings`, `home`), `online`, `memory_mb` where the
  browser tells it.
- `foreground` per visible period; `app` (recording, visible, hidden), `page`, `network`, `socket`, `request_error`.
- While the hub lists `todo_view`, from the To-dos view's shadow (`docs/web/web.md` "Developing Persona"): `todo_view`, at
  most a minute apart, counts the rows with a view: `compared`; `same`; `by_design` (apart by a decided difference, by
  which); `unknown` (a newer hub's section or reason); `client_gap` (a run whose session this page lacks); `boundary`
  (the view changes by time alone within 2 minutes); `stale_view` (a run moved since the list was read: the list is read
  again); `unsettled` (apart, until a run 10 s later still sees it); `mismatch` (apart, nothing explains it); `errors`
  (runs that failed). `todo_view_mismatch`, one per unexplained row an hour: its id, its manager's id, and both sides'
  section and reason words. Drawing the To-dos sections from the view also needs `localStorage`
  `persona.todos.viewDraw` = `1` (no settings row; reload the page after setting it).
- `errors.jsonl`: uncaught errors, unhandled rejections and freezes of 5 s or more (`lib/freeze-watch.ts`), whether or not
  the hub takes error reports. `log.txt`: `console.error` / `console.warn` lines, scrubbed (an error a boundary caught is
  logged there too).
- The manifest names the platform (`web`), the system and browser, the web build (its
  entry script's name) and the hub's version.

**No first-screen cost.** Everything lives in chunks that load only while the switch is on (`lib/dev-mode.ts`,
`lib/dev-package.ts` with jszip, `views/settings/DevExportDialog.tsx`). The start-up check and the WebSocket wrap are in
`lib/dev-probe.ts`, loaded by `lib/error-reports-start.ts`, which runs before the app opens its socket and is outside the
first screen's budget. The recorder hooks `fetch`, `history`, `popstate`, `error`, `unhandledrejection`, the console and
the freeze watch itself and puts them back when the switch goes off; it imports nothing from the first screen's modules
that they do not already export, so its route word list is a copy of `lib/error-reports.ts`'s (`dev-mode.test.ts` keeps
them equal). A socket already open when the switch is turned on is counted from its next connection. The To-dos view's
shadow (`web/src/features/todos/model/view-shadow.ts`, a chunk of its own) reads the triage through App's module: the
recorder can load before App, so importing `lib/todo-triage.ts` would move it out of App's chunk into one of its own.

## The package

`persona-diagnostics-<platform>-<yyyyMMdd-HHmm>.zip` (local time), a folder of the same name inside:

```
README.md          for the Agent reading it: what each file is, how to read it, what was scrubbed
manifest.json      format "persona-diagnostics", version 1, platform, os, os_version, device_model, app_version, build,
                   commit, utc_offset_minutes, recording_since, exported_at, window {from, to}, categories, counts
events.jsonl       the timeline above
errors.jsonl       error reports, in the hub's error report format
log.txt            this launch's log lines
```

The export page lists four categories; the first three can be left out: **Logs** (`log.txt`, the event kinds
other than the energy ones, beside the socket's own lines), **Errors and Crashes** (`errors.jsonl`), **Energy and
Performance** (`minute`, `foreground`, `thermal`, `power`, `battery`, `path`), and **Device and App
Version** (`manifest.json`, `README.md`, always).

## Send to Agent

The export page's first button. Send To lists Forward to's targets with its rows (exported from
`views/chat/ForwardDialog.tsx`), and the zip goes into the chosen chat's composer files (`lib/composer-files.ts` `addFiles`,
uploaded to that chat's computer) with the note put in the box before any words it holds (`sendDraft` `unsent`).
**Download** saves the zip.


## What is never included

Message text, knowledge base content, keys, tokens, passwords, cookies, pairing links, the device's name, the hub's
address, machine names, accounts. Recorded lines carry no free text; free text that does go in (error messages, log
lines) passes the scrubber (`web/src/lib/dev-scrub.ts`):

- a URL keeps its path only (no scheme, host, query or fragment, so no pairing code or token);
- `Bearer` / `Basic` values, `token=`, `key=`, `secret=`, `password=`, `cookie=`, `authorization:` values, JWTs and runs of 32
  or more letters and digits become `<redacted>`; e-mail addresses `<email>`; IPv4 and IPv6 addresses `<ip>`; host names
  `<host>`; `/Users/<name>` and `/home/<name>` `~`;
- each text is cut to 1 KB.

Route patterns are the web error reports' (`pathPattern` in `web/src/lib/error-reports.ts`): every path segment that is
not one of the API's words is `:id`.

## Code (web)

`web/src/lib/dev-probe.ts` (the switch's key, the WebSocket wrap), `web/src/lib/dev-mode.ts` (recorder, store, hooks),
`web/src/lib/dev-scrub.ts`, `dev-package.ts`, `DevModeSettings.tsx` (About Persona's
rows), `DevExportDialog.tsx` (export, Send To, Download), `web/src/features/todos/model/view-shadow.ts`
(the To-dos view's shadow). Tests: `web/src/lib/dev-mode.test.ts`, `DevModeSettings.test.tsx`,
`web/src/features/todos/model/view-shadow.test.ts`.

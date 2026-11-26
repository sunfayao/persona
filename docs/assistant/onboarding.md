# New-user onboarding

An onboarding path for ordinary users who do not know the command line: after seeing an ad and downloading the app (phone, computer, or web), how do they install it, connect, add a model, and get an answer to their first message? Related to #287.

Code baseline: origin/master `59e8bae9a`. All `path:line` references in this document were checked against that commit. The current-state research was a fresh walkthrough on 2026-11-06 (code reading + testing an isolated hub + web screenshots). Conclusions are in section 2; raw records are not committed.

## 1. One sentence and the mental model

**In one sentence**: today's first screen asks users to "Run `personad hub pair` on the hub machine," which ordinary users cannot get past. The goal is "install one thing, scan one code, tap a few times, and get an answer to your first message." If anything is missing, the UI shows a card saying "What's missing; tap here," not a technical error in English or a blank screen.

User-facing explanation (use the same passage on the first screen's "How does this work?", the website, installer, and documentation):

> **Persona has three parts.**
> - **hub**: your home switchboard. Install it on a computer or NAS that stays on. Your messages, settings, and memory live here, only on your own machine. One is enough for the whole household.
> - **Your computers**: where Persona does the work—reading files, running programs, and writing code. The hub computer automatically counts as one.
> - **Phone / web**: the remote control. See what it is doing and tap "Allow" before it acts. Scan once to connect; it reconnects automatically after that.
>
> You also need a **model**, Persona's brain. If you already use Claude Code, use it directly; if you have an API key, paste it in; if you have neither, we show you where to get one.

```
   Phone / web ──scan once──▶ hub (always-on machine) ◀──Persona Link── Your laptop
   (remote control)          storage, messages, dispatch                (does the work)
                                      │
                            This also counts as a computer
                                      │
                            Model (Claude Code or API key)
```

Terminology rules (enforced by the copy guard tests in section 8.1):

- Onboarding, empty states, and error cards must not mention node, herdr, launcher, TLS, token, hub.json, zsh, ports, IPv6, `~/.` paths, or command names. Put them in "Details" or "Advanced" when needed.
- For ordinary users, call launchers / providers "models" (which provider, which key). Put run mode, kind, protocol, and Bearer / x-api-key in "Advanced".
- The user decides what hub is called in the UI ("hub", "host", "center"; decision 13 in section 6.3). Until then, keep "hub" and explain it as "your home switchboard" on first use.

## 4. Design

### 4.1 Installer: `personad hub setup`

Same namespace as `hub pair` and `hub admin`. Implemented in Go and available in `personad`.

```
personad hub setup [--yes] [--listen :18660] [--tls :18662] [--outside] [--no-service] [--manager-service] [--json] [--lang en|zh-Hans|ja]
```

Run in order, printing one localized conclusion per step; on failure, provide the next action and stop:

1. **hub.json**: if missing, write a complete new file (new defaults below). If present, add only fields such as `token` that are required to run. **Never change existing values**.
2. **Database**: if `database_url` is empty and Docker is available, create a container following `scripts/deploy/db.sh` (same `persona-postgres` name, image, and volume, still recognized by `deploy.sh`), generate a password, and write it to hub.json. Without Docker, probe local Postgres (the #321 approach). If neither exists, stop and print "This computer still needs a database: install Docker Desktop or OrbStack, then rerun this command." Also change the error at `db/db.go:37` to "Run personad hub setup first."
3. **Persistent service**: on Linux, install socket-activation units following `hub-systemd.sh` (the same units, not a second set). If `loginctl enable-linger` needs sudo, ask once and explain why. On macOS, write a LaunchAgent (new implementation; `hub-systemd.sh` currently only finds existing ones). If another hub occupies the port, automatically choose another and save it.
4. **Start and self-check**: wait for `/api/v1/health`, have the built-in manager bound and installed as a service of its own (step `manager`, below), run doctor, and print only problematic items; do not print excluded virtual network interfaces.
5. **Finish** (localized): a pairing QR code for the phone (reuse `hub pair`'s terminal QR code); "Open `http://<LAN-IP>:18660/admin#setup=<setup-code>` in your phone browser to set the administrator password." The installer obtains the setup code through loopback + the main token with `POST /api/v1/admin/setup-code` (`admin_setupcode.go:94-99`), without asking the user to inspect journalctl. `#setup=` is a fragment, not sent to the server; the admin page reads and prefills it.

**Write new-install defaults only into new files; never add them to `defaultConfig()`.** `LoadConfig` deserializes an existing hub.json over `defaultConfig()` (`hub/config.go:241-278`). `Public` is a value type (`config.go:169`), so adding `TLSListen` to `defaultConfig` would enable TLS for every old hub.json that omits `public`. Also, `db.sh` writes a hub.json containing only `database_url` before hub's first run, so the file already exists in the deploy.sh path. Approach: explicitly write these fields when `hub setup` creates a file, or only in `LoadConfig`'s `os.IsNotExist` branch. Both share `newInstallConfig()`, with a golden test proving that loading old files gives the same result.

Fields written to a new file:

| Field | New installation | Why |
| --- | --- | --- |
| `public.tls_listen` | Bind only the LAN IPv4 address by default: `"<LAN-IP>:18662"`; change to `":18662"` only with `--outside` or when "Use away from home" is enabled later in the app (decision 8) | Persona Link requires TLS (`linkPairable`); mDNS broadcasts only with TLS. Binding only the IPv4 LAN address avoids exposing the port on public IPv6. `tls_listen` accepts an address (`public.go:84` uses `listenTCP`; `mesh_discovery.go:39` parses host). DHCP may change the LAN address; if binding fails at startup, hub must fall back and notify the app. Add this during implementation |
| `public_url` | `http://<LAN-IPv4>:<listen-port>`; for multiple interfaces, choose the default-route interface, unless it is a VPN tunnel (a full-tunnel VPN, whose address changes on reconnect; #11125): then the first private IPv4 on an interface that is not virtual. `public.tls_listen` uses the same address | Currently uses the full hostname (`config.go:215`), which phones often cannot resolve |
| `admin.first_setup` | `"code"`: setting the first administrator password from the LAN also requires a setup code | Currently only loopback requires a code (`admin_setupcode.go:52`), so someone else on a shared LAN could set the password first. Old hub.json files omit this field and keep their behavior |
| `machines` | Keep local `kind: local` if herdr or claude exists; remain empty in Docker | Same as today |
| `initial_locale` | The language the installer ran in: `--lang`, else `PERSONA_INSTALL_LANG` (which `install.sh` sets from its own language); omitted when neither gives `en` / `zh-Hans` / `ja` | The hub's user language until a device reports one, so a new English install's first pushes and assistant replies are in English (#5519, [i18n.md](../protocol/i18n.md) "User language") |

Implementation (#3349): `server/internal/hub/setup.go`, `setup_config.go`, `setup_database.go`, `setup_service.go`. Only `hub setup` writes the new defaults (`LoadConfig` still writes `defaultConfig()` on first run). A "new file" means a hub.json without `token` (missing, or prewritten by `db.sh` / `hub admin access`); fill missing fields from `newInstallConfig()` and leave existing fields alone. `--outside` is the only option that changes an existing value (`public.tls_listen` becomes `:<port>`). Without Docker, check for Postgres on port 5432; if present, ask the user to put its address in `database_url` (port check only; connecting to local Postgres and creating the persona user and database as in #321 is follow-up work). If the default ports are occupied, a new installation uses the pair +100 (18760 / 18762). If the LAN address bound by `tls_listen` disappears, hub starts normally without TLS and logs a line. The socket unit has `FreeBind=true` (also updated in `hub-systemd.sh`), so listening still starts under socket activation; hub logs a line if the address is not local (the app notice is follow-up work). The Mac LaunchAgent is `com.persona.hub`; if another agent already runs `<root>/bin/personad hub`, use it (like `hub-systemd.sh`, resolve symlinks and `~` in `ProgramArguments` for comparison; also recognize `~/.persona` pointing to `~/.persona`). If hub is already running without an agent (for example, in a terminal), do not install an agent that cannot acquire the port; ask the user to stop it first. Restart hub when `bin/personad` differs from the binary used to start the service last time (`install.sh --hub` replaced it on upgrade; recorded in `bin/.personad.setup-sha256`): Linux uses `restart personad`, seamless because the socket owns the port; Mac uses `launchctl kickstart -k`.

Implementation (the hub/manager split): with services (not `--no-service`, not in a container; Linux with a systemd user instance, or a Mac), a new `hub.json` gets `assistant.mode: service`, and the `manager` step after the start runs `<root>/bin/personad manager install --builtin --join http://127.0.0.1:<port>` with a one-time code in its environment. The manager's own program binds it and installs `persona-manager-assistant.service` (Mac: `com.persona.manager.assistant`); the hub writes none of its files. Then the step waits for it on the hub's bus. An existing `hub.json` keeps its mode, and the step only says how to switch (`--manager-service`, which checks first and undoes a switch it cannot finish). Details: [managers.md](managers.md) ("The built-in manager as its own service").


Implementation (#4481, #4484): select the final admin UI address from hub's actual `listen`. If it listens only on loopback (`127.0.0.1` / `localhost` / `::1`), print a loopback address, explain "Only accessible on this computer," and show how to make it accessible from the phone (include the actual hub.json path, change its `listen` to `:<port>`, and rerun `hub setup`); the final `--json` line adds `admin_local_only: true`. If bound to a specific address, use that address; use the LAN IP only when listening on all addresses. On reruns, explain "Existing values are not changed" for `--listen` / `--tls` only if explicitly supplied and different from hub.json, showing both values. `--tls :<port>` and the new-install `LAN-IP:<port>` count as equivalent. Commands in the admin UI, hub logs, `hub doctor`, and usage / errors for `hub admin` / `hub peer` all use `personad` (the `personad` alias still works; guarded by server `command_name_test.go` and web `web/src/admin/command-name.test.ts`). The systemd unit remains `personad`; where the admin UI mentions `journalctl --user -u personad`, explain "The service retains its old name." The admin extension page matches **Settings › Extensions**: when the official marketplace is unreadable, show muted text "Official extensions are temporarily unavailable. Try again later." (reason on hover). When the marketplace is unreadable, the catalog must not say "No extensions here yet."



### 4.2 Pairing

**Principle**: initial trust comes only from a channel the user sees firsthand: scanning the on-screen QR code, pasting a link they copied themselves, or the hub's local terminal. Never put long-term credentials in a QR code, link, or log; use only one-time codes (`jp_…`, 10 minutes, single-use).

#### Phone / web ↔ hub

| Scenario | Source of the first code |
| --- | --- |
| Just ran `hub setup` | Terminal QR code; or open the printed `/admin#setup=…` in a phone browser, set a password, then "Pair phone" |
| Already has a logged-in device | **Settings › Devices › Connect new device** on that device (already on web and iOS; Android added this round) |
| Same LAN, hub has TLS enabled (P1) | "Find nearby hubs" on the app's first screen browses `_persona-hub._tcp` and lists "Home hub 'xxx'". **mDNS provides discovery, not trust**: broadcast `fp` cannot establish initial trust (`docs/operations/network.md:75`). A one-time code is still required after discovery; the benefit is telling the user "Hub is installed; here is where to get a code" |

On the iPhone, a pairing link never pairs by itself (#9929): whether it comes from the camera, another app or the pairing page's scanner, the app first asks **Connect to <host>?**, naming the hub's address as the link gives it (and saying when that hub is already paired), and pairs only after Connect. Cancel leaves everything as it was; typing an address and token and tapping Connect is already the user's own step. The launch arguments `-pairURL` and `-openPairLink` (DEBUG automation) still pair directly.

The browser pairing page leads with a field for the pairing link. Scan opens the phone approval flow on demand, Help explains how to obtain a link, and Key opens the advanced master-key form with its warning and terminal commands. When signing in again in the same browser, reuse or replace the old device, with a date in the device name, rather than accumulating identically named "Web (Chrome · Linux)" entries. Implementation (#3356): web sends `replaces` (the previous device ID) when exchanging credentials; hub revokes only that `kind: web` device (`docs/protocol/protocol.md`, "Authentication"). "No hub yet?" is an explanation within the page; web cannot obtain the website URL.

#### Your computers ↔ hub

This round's goal: **"Add computer" in the app is enough**, without `/admin`, enabling TLS first, or manually adding it beforehand.

1. A logged-in full device calls `POST /api/v1/auth/pair-codes` with `add` (section 5.3). Hub adds a `kind: link` entry using the same logic as `/admin` (extract a shared function; `addMachineLive` needs no restart), and returns the computer pairing link.
2. The computer stays **pending** until the user taps "This is my computer" in the app. Why: Persona-managed API keys currently go to "every node" (`assistant/models_launchers.go:14-18`; `pushManaged` sends to every online machine, `assistant/models_launchers_push.go:84-112`). When a Docker hub has no local computer, `defaultAssistantMachine` falls back to the first machine (`hub/assistant_profile.go:117-118`). Without confirmation, a forwarded or screenshotted link would leak keys and run main chat on someone else's computer. A pending computer:
   - receives no Persona-managed keys (`pushManaged` filters it);
   - is excluded from assistant-machine selection (`defaultAssistantMachine` skips it; assistant `effective()` rejects it too, `assistant/profile.go:139-142`);
   - is absent from computer pickers for new Agents, dispatch, and to-dos;
   - is automatically deleted after 24 hours without confirmation (both hub.json entry and Postgres record).
   Confirmation belongs on the "Waiting for this computer to connect" screen. Once connected, show "A computer wants to join: <name> (macOS). Is it yours?" with one button, not another screen.
3. **Compatibility**: store trust state in a new Postgres table; **no record = confirmed**. Existing machines, ones added through `/admin`, and ones added through loopback + main token (`hub pair --machine`) therefore remain confirmed on upgrade, with no behavior change. For old apps, a pending machine's `error` says "Confirm in a newer app or on the web."
4. **Visible receipts**: when a device generates a computer code, a new device redeems an app code, or a new computer connects, notify other paired devices with "Not you? Revoke" (revocation endpoint already exists). Today this only appears in hub logs (`pairing.go:187`).
5. Send the link to that computer, with a screen for its OS:
   - **Linux**: a command containing the link (`persona-link pair '<link>'`), "Copy" + "Open a terminal on that computer, paste, and press Enter."
   - **Mac**: "Install Persona Link" ([installation guide](../operations/link.md#pairing-and-managing-the-mac-connector), which honestly says when a local build is required) + "Send this link to that computer" (system share). The independent Link service is paired with the optional desktop setup or the `persona-link pair` command; the old menu bar pairing window is retired. Hub still sends `persona://link?…` (`pairing.go:57`), recognized by Persona Link and node.
6. `personad hub pair --machine <id>`: if the machine does not exist, `--add [--name]` adds it automatically; without it, ask "Add --add," rather than returning 400 `hub.json has no machine …`.
7. Old hubs without TLS: 409 `tls_required` + action `enable_tls`; the app says "Hub has not enabled secure connections, so computers cannot join." If managed, enable and restart in one tap.
8. **Member hubs in linked hubs**: app codes are currently forwarded to the primary hub (`pairing.go:161-164`); the computer flow is undecided. This round, adding a computer from a member-hub device returns 409 `mesh_member`, "Add it on the primary hub." New users will not reach this.

P1: **computer initiates, phone allows**. Persona Link's first screen shows a QR code with its one-time public key and LAN address. The phone scans it and sends the full link to the computer over LAN, avoiding manual transfer of a long link. Do not implement "phone shows a 6-digit code; computer enters it": a short code needs LAN discovery to find hub, and mDNS can be spoofed by someone on the same network.

### 4.3 Connecting a model

#### No dependency on zsh

- Do not mention "~/.zshrc" in onboarding. In **Settings › Model providers** the import is a plain **Import** button beside **Add provider** (#5024, earlier under "Advanced"): the shell files are named only inside its dialog, which explains what to do when there is no computer yet instead of spinning.
- Existing developer aliases / functions are still discovered and selectable (`mergeLaunchers` in `node/launchers.go`); the onboarding wizard does not depend on them.
- The default `claude` launcher currently always starts through `zsh -ic` (`assistantCLICommand` in `node/assistant_start.go`), causing `command not found` for users whose PATH is in `.zprofile`. Prefer the absolute path found by `claudeExecutable()` (`node/managed.go:104`), then fall back to the shell. This overlaps with #2854 (bash launchers / bundled claude, claimed by someone else); coordinate in #2854 first rather than duplicating the work.

#### Detect first, then ask

After pairing (or when main chat's first screen lacks a model), check readiness first (section 5.1). Users see conclusions:

| Detected | User sees | Button |
| --- | --- | --- |
| Online computer, Claude Code installed and logged in | "Claude Code on your MacBook is ready" | "Use this" → try a message |
| Installed, not logged in | "Claude Code is not logged in yet" | "I have an API key" / "How to sign in" (illustrated: open Terminal on that computer, enter claude, and follow the sign-in instructions; the only step this round still requiring a terminal) |
| Computer exists, Claude Code not installed | "This computer does not have Claude Code yet" | "How to install" (official instructions); P1 "Install for me" |
| No computer | "Add a computer first; once connected, it can do the work" | "Add computer"; P1 "Chat only for now" if a key is available |
| A working keyed provider already exists | Skip this step | — |

Free sign-in detection: add a node RPC running `claude auth status --json` (no model request). Two caveats:

- It must run in **the launcher's actual environment**: the default launcher uses the same shell (`zsh -ic`) or absolute path; Persona-managed launchers use the environment of `personad launch <name>`. Testing with an empty HOME showed that an environment API key makes it report `loggedIn: true, authMethod: api_key`. Running outside the actual environment can falsely report "ready" or "not logged in."
- Node does not currently use this command anywhere. Check versions; an old version without the subcommand, or unreadable output, reports `unknown`, not failure. With `authMethod: api_key`, display "Using a key from the environment," record the status as "Unverified," and wait for a test message.

#### Three routes

**A. Already has Claude Code**: do not change configuration (the default is `claude`, `assistant/profile.go:148-150`); try a message directly. P1: node runs a headless sign-in command such as `claude setup-token` and pushes the sign-in link to open on the phone. Test this first and confirm this use of subscription sign-in complies with Anthropic's terms (decision 10).

**B. API key only**:

1. Choose a provider (built-in presets below), paste only the key; generate the name automatically, allowing a Chinese display name.
2. Test automatically (`POST /api/v1/launchers/test`; if no model is supplied, use the preset's default rather than requiring a model). On failure, retain the existing plain-language explanations (401 / 403 / 404 / 429 / timeout, `server/internal/i18n/locales/zh-Hans/assistant-models_probe.json`) and add a "Where to get a key" link. Testing costs a little money; keep the existing notice.
3. For a Claude-compatible preset, include `assign` on save (section 5.4): **change main chat / Agents only if they are still at their untouched defaults**, and return `assigned`. The wizard's final screen explicitly says "Main chat and new Agents will use this." If readiness reports working Claude Code, default this switch to off; do not silently replace a working default. A Responses or Chat Completions direct API cannot run those purposes: hide the switch, omit `assign`, do not wait for readiness and skip the Claude Code warning, and show the direct-API note on both key and done steps.
4. Computer exists, Claude Code missing: say honestly "Your key is saved. Install Claude Code on the computer before main chat can use it," because `finishModel` still starts claude on a computer. P1: node's `claude.install` (follow `herdrinstall.go` for download, verification, installation to `~/.local/bin`, and progress reporting), triggered only by the user's button tap. Accept download URLs only on official domains; report `download_blocked` if downloading fails on networks in China.
5. No computer: P1 "Chat only" mode (4.4) uses this key directly.

Implementation (#4482, `assistant/models_probe.go`): distinguish Anthropic regional blocking within 403 responses (response `message` is `Request not allowed`, `error.type` is `forbidden`; blocking happens before the key is checked. If `error.type` is empty, count it only when the address is the official Anthropic endpoint; the same generic phrase from another gateway remains an ordinary 403). Change the copy to "Anthropic's official service is unavailable in your current network's region; changing your key will not help," followed by the built-in providers not limited to overseas service (names in the request language), without quoting the gateway's raw wording. Test and model-list responses add `code: "region_blocked"`; the three clients currently only show the copy (the wizard already lets users go back and choose another provider). A code-based switching entry point is follow-up work (#4526). Anthropic `permission_error` and other providers' 403s retain "No permission (403)" plus the original message.

Web implementation (#4037, `views/settings/ProviderWizard.tsx`): "Working Claude Code already exists" requires `ok` for all three readiness steps: computer, Claude Code, and model (an untested `unknown` model does not count; a new key still defaults to assignment). Hide the switch until the first result arrives; saving also waits for it before deciding whether to include `assign`.
For the step 4 notice, check readiness both before saving and after refetching: show it if Claude Code was `claude_missing` in either result and is not `ok` after saving. Checking only after saving is insufficient: once main chat switches to a Persona-managed provider, hub returns `unknown` for this step (`readiness.go` asserts absence only for the default `claude` launcher).

**C. Has neither**: one screen, "Where to get a model." Sort built-in presets by request language (Chinese lists providers available directly in China first; other languages list Anthropic first). Give each a one-sentence explanation and "Sign up" (open `signup_url` in the in-app browser); on return, proceed to B. Leave out direct-API presets (Grok): signing up there gives no main chat. Beside it, offer "Try demo first." No affiliate links; recommendations and order are decision 11.

#### Built-in presets

- Add an embedded presets_builtin.json under `server/internal/assistant/`, using `assistantwire.LauncherPreset` (`assistantwire/models.go:61-75`), `plugin: "builtin"`, and IDs `builtin/<vendor>`. Add two optional fields: `signup_url`, `name_zh` (or an i18n key).
- `launcherPresets()` (`models_presets.go:20`) returns "built-in + plugins"; a plugin overrides a built-in with the same ID. Leave `presetFill` unchanged.
- Initial candidates: official Anthropic, DeepSeek, Kimi (Moonshot), Zhipu GLM, Alibaba Bailian, and MiniMax's Anthropic-compatible endpoints. **Addresses and model names were not verified while drafting**. Before adding any provider, check its official documentation and pass a one-word request test, with sources in the PR. Include only vendors' public addresses, not relay services or company gateways. Tests ensure every entry has all required fields.
  Grok was added later (#2204) as the first direct-API built-in: xAI deprecated its Anthropic-compatible endpoint, so it is on Responses and serves only direct-API categories (`docs/integrations/launchers.md`, "Built-in presets").
- The first wizard step on all three clients lists presets directly, with "Custom compatible gateway" last. Base URL and authentication method appear only in the custom flow. Replace hardcoded "Official Anthropic" in client wizards (`web/src/lib/providers.ts`, `ProviderWizardModel.swift`) with preset data. Add "Test connection" and "Get model list" on Android (currently does not call `/launchers/test` or `/discover`).

### 4.4 Main-chat readiness and degradation

Hub computes main-chat status (`mode` in `GET /api/v1/assistant/readiness`, section 5.1); all four platforms present it consistently:

| `mode` | Conditions | First screen | Sending |
| --- | --- | --- | --- |
| `full` | Confirmed online computer + Claude Code can start + no model error | Current appearance; example questions reflect connected capabilities | Normal |
| `setup` | Missing computer, offline computer, missing Claude Code, not logged in, or invalid key | Keep the greeting. Above the composer, show one card for only the first missing step (title + one-sentence reason + one button). Replace example questions with "What can Persona do?" and "How do I connect a model?"; taps open local help, not a model request. Stop saying it "can also do work on your computers" | Allow typing and preserve the draft. Replace Send with "Connect a model first" (or "Add a computer first"), opening that step. **Do not queue messages**, avoiding a flood of late answers once setup finishes |
| `chat_only` (P1, decision 9) | A keyed provider hub can call directly, but no usable computer | Persistent top notice: "Chat only for now. Connect your computer so Persona can do the work" + "Add computer" | Hub answers through the API directly |

This round, hub returns only `full` / `setup`. Add `chat_only` to the protocol now; clients handle it as "top notice + normal sending" so P1 will not require another app release.

Failures are cards, never toasts:

- **Failure discovered after sending** (stale readiness): pin a card below that message (copy from 4.6 + button + "Details"); offer "Retry" on the message.
- **Computer asleep / offline**: "Your MacBook is offline, possibly asleep. Open the lid to continue." Do not describe this as failure to connect to hub (Android currently does).
- **Model errors**: Claude Code's synthesized replies such as "Not logged in" and "Invalid API key" no longer appear as ordinary bubbles. Show a "Model not connected: not logged in / incorrect key" card + "Fix it." Add `Not logged in` to `APIErrorMarkers` (`assistantwire/handler.go:123-124`); use the existing `user` for `turn_error_kind` (`proto/session.go:271-275`; `auth` is a health-check category, not a turn kind). `CutOff` also drives `TurnError` in hub's session list and scheduled-task / to-do handling (comment at `handler.go:118-122`), so this PR needs scheduled-task and to-do tests, not just a main-chat card.

**Scheduled tasks**: new hubs enable a weekday 09:00 morning briefing by default (`assistant/schedule.go:212`); with zero computers, it will always fail. Change this to skip while readiness is not `full`, record "Skipped: computer or model setup is incomplete," and send no failure notification. Mention it in main chat before the first run after setup. Do not change the default itself; old hubs are unaffected.

Differences and additions in the implementation (#4253, `assistant/schedule_ready.go`):

- Rather than "skip whenever `mode` is not `full`," skip only if the step blocking `mode` is needed by this run. A follow-up sent to a chat on another computer does not wait for the assistant's computer. A task using another launcher waits only for its computer, not main chat's Claude Code or model. This preserves tasks that can run even in `setup` mode (anything that ran before can still run).
- The run record has `status: skipped` and an `error` saying "Computer or model setup is incomplete; skipping this run" (same phrasing as "The previous turn is still running; skipping this run"). No push notification or retry after 3 minutes. Do not block "Run now" or one-time tasks (reminders and assistant-created follow-ups): they disable themselves at their due time, so skipping would lose them. Send as before, and on failure push "Scheduled task failed" and retry once.
- Readiness skips also store `not_ready: true` (#1857), including when the computer is temporarily asleep or offline. They neither count toward nor reset the failure streak, so even repeated readiness skips send no streak notification. Other skips and failures retain their existing counting rules. The marker survives restarts; older unmarked history is left as recorded, without guessing from localized error text.
- Use the non-probing readiness path (node's two checks are cached), wait at most 12 seconds for node, and spend no tokens.
- The "mention" is a new `notice` entry in main chat's activity feed (`{"text"}`, `docs/protocol/protocol.md`, "Tasks and the chat feed"). Hub writes the copy without model cost. Emit it only after a skip caused by a step requiring user setup or repair (`todo`, login, key, or model problems), and only once (persist `routines_waiting` in `assistant.readiness`, surviving restarts). Sleeping / offline computers (`node_unreachable`) still cause skips, but do not set the flag, avoiding "Setup complete" on every wake. None of the four clients renders `notice` yet (unknown entries are skipped); each needs one centered line of small text. Until then, users can see skips and recovery only in run history.

**After setup**: when "Try a message" succeeds, main chat shows a locally written welcome (no model cost) and three small tappable tasks, chosen from connected capabilities: "Find the largest folders on this computer," "What's on my calendar today?" (request phone calendar permission only on tap), and "Help me connect email."

Web implementation (#3847; details in `docs/web/web.md`, "What the main chat still needs") differs as follows: the message that failed stays in the chat as a failed bubble with Retry and Delete, as in the apps (#1122), and the card's Retry retries it. `add_computer` opens the Machines page's add flow in place when hub has `machines_add`; old hubs show an explanatory dialog with "Open admin UI" (hub's `/admin/machines`). `confirm_computer` opens that pending computer's confirmation UI in place (#4037). The zero-computer Overview empty state is separate work in #3729.

#### "Chat only" mode (P1, a separate issue, decision 9)

For people with only an API key and no computer (Docker / NAS / work computers that prohibit installation), main chat is permanently empty today. This round provides a guidance card rather than a blank dead end; P1 adds a limited mode:

- Build on hub's existing direct-model client (`server/internal/ai/ai.go:110`, `Complete`, used for information processing / filtering). Select a provider using `fallbackAPIProvider` rules (`assistant/models_resolve.go:122`); support only the Messages protocol.
- Plain-text chat, with read-only context hub can answer locally (connection status, knowledge base summaries, Inbox counts). No computer tools, phone writes, or tool-call loop.
- Store chats in a dedicated Postgres table (its migration named by the UTC second it is written, `docs/server/server.md` "Migrations"), separate from main chat on node. Show a persistent "Chat only for now" notice. After connecting a computer, switch back to normal main chat and include a summary of this segment in the new process's first context message.
- Put prompts in a new assistant/chat_only directory under `server/internal/prompts/`, with golden tests. When asked to act, answer "I can do that once you connect a computer."

Risks (why a decision is needed): memory, transcript, and permissions split into two systems; the work lies in storage, streaming, and client rendering.

### 4.6 Error copy

Principles:

1. Every failure = an error code + one plain-language sentence + one button. Put original text and request ID in expandable "Details" for screenshots when asking for help.
2. Clients use `code` for their own copy and buttons; do not implement four separate literal-text matchers. Hub's `error` also provides plain-language text localized to the request language, so old clients can at least understand it.
3. Say which computer, what is missing, and where to tap. Do not say "Operation failed, please retry," or describe an offline computer as inability to connect to hub.
4. Automatically retry self-healing failures (assistant process restart) without interruption.

| code | When | Plain-language copy | Action |
| --- | --- | --- | --- |
| `no_machine` | Main chat has no usable computer (currently 502 `internal`, `assistant/start.go:29,157-158`; memory at `profile.go:465` uses the same English sentence) | "Persona needs one of your computers to do work. No computer is connected yet." | `add_computer` |
| `node_unreachable` (existing) | Computer offline | "<computer-name> is offline, possibly asleep or with Persona Link closed." | `wake_computer` (instructions page) |
| `assistant_offline` (existing, 503) | Assistant process just restarted | "The assistant is restarting. It will recover in a few seconds." | Automatic retry |
| `machine_pending` | Dispatching work to a pending computer or selecting it as the assistant machine | "You have not confirmed this computer is yours yet." | `confirm_computer` |
| `claude_missing` | Exit code 127 with command `claude`, or `claudeExecutable` cannot find it | "Claude Code is not installed on <computer-name> yet." | `install_claude` (instructions page this round) / `connect_model` |
| `launcher_missing` | 127 for a missing custom launcher from `~/.zshrc` | "The model launcher '<name>' was not found on <computer-name>. It may have been deleted." | `connect_model` |
| `not_logged_in` | `auth status` reports no login, a new conversation's automatically selected private profile is explicitly signed out, or the reply begins "Not logged in" | "The Claude Code CLI isn't signed in. The main chat runs it, and the Claude desktop app's sign-in doesn't count for it: run `claude auth login` in a terminal on <computer-name>, or use an API key." For a private-profile start refusal, name the selected profile and keep its dedicated-profile command in `detail`. Where the computer offers `claude-desktop`, also: an Agent started with it runs in the desktop app on the app's own sign-in (#9366). | `login_claude` (illustrated) / `connect_model` |
| `auth_failed` | "Invalid API key", "Failed to authenticate", etc. (already in markers) | "The model's key is incorrect or expired." | `edit_provider` |
| `model_route` / `rate_limited` | Wrong model name / 429 (health-check categories already exist) | "This model is currently unavailable / Requests are too frequent. It will retry later." | `connect_model` |
| `tls_required` (existing) | Adding a computer when hub has no TLS | "Hub has not enabled secure connections, so computers cannot join." | `enable_tls` |
| `mesh_member` | Adding a computer from a device on a member hub | "This hub is a group member. Add the computer on the primary hub." | — |
| `pair_code_expired` / `pair_code_used` | Pairing code expired / used (iOS currently shows English `invalid token`) | "This code expired / has already been used. Generate another." | Generate a new code |
| `hub_identity_changed` | Pinned fingerprint mismatch | "Hub's identity changed (reinstalled or moved to another computer). If you reinstalled it, scan a new code." No "Ignore and continue" option | Scan again |
| `node_service_down` (machine `error_code`) | Link works, persistent node is not running (current source text `…node-attach.sock 连不上：persona-link node install…`, meaning cannot connect to node-attach.sock) | "Persona's background service is not running on this computer. Open Persona Link and let it reinstall the service." | — |
| `link_offline` (machine `error_code`) | Persona Link disconnected (current text `link closed: failed to get reader…`) | "This computer is offline: Persona Link is not running, or the computer is asleep." | — |
| `db_missing` (CLI) | Empty `database_url` | "Hub's database is not set up yet. Run personad hub setup to create it." | — |
| `port_in_use` (CLI) | Another program occupies the port (today `hub pair` may hit another hub and return 401) | "Port <n> is in use by another program, possibly another Persona." | Installer chooses another port automatically |

HTTP status: new codes use `rpc.Error{Code}`. `apierr.Call` returns 409 for codes other than `internal` / `node_unreachable` / `timeout` (`apierr/apierr.go:117-131`), so `no_machine` and similar errors must not reuse those three names. Note that existing coded `node_unreachable` is 502; only `ErrOffline` is 503.

### 4.7 Defaults

| Item | Today | Beginner default |
| --- | --- | --- |
| Main-chat model | App-stored value > `assistant.launcher` in the manager's `manager.json` > `claude` (`assistant/profile.go:139-150`) | Unchanged. Use working Claude Code when detected; a key connected during onboarding is assigned through `assign` only while the choice is still default |
| Main-chat permissions | Default mode: approve Bash and similar tools on the phone each time; Persona's own tools are already allowed through `--allowedTools` (`docs/assistant/assistant.md`, "Current structure") | Unchanged. Improve approval-card explanations; onboarding says "It will ask before doing work on your computer." "Read without asking" cannot safely be expressed for Bash (read-only commands cannot be distinguished). First count approvals during a real beginner setup, then decide (decision 14) |
| Writes on the user's behalf | Hub asks first for email sending, phone writes, and Lark writes | Unchanged (hard boundary) |
| New Agent sessions | User-selected | Default "Manual approval"; no beginner preset uses "Allow all" |
| Launcher health checks | Off by default; when enabled, makes a real request every 30 minutes (`launcher_health.go:58`, costs money). Repair Agents default to bypass (`:68`, user decision 2026-11-05) | Unchanged. Readiness uses only free checks, independent of health checks |
| Scheduled tasks | Morning briefing enabled by default | Skip before readiness (4.4) |
| Example questions | Web has 4, iOS 6, maintained separately | Hub supplies one set in readiness (`suggestions`), selected by connected capabilities; all four clients only render it |
| Built-in providers | None | Built-in presets in 4.3 |

## 5. Protocol

General rules:

- All new fields are optional and ignored by old clients. Register new endpoints in `features` of `hello` / `GET /api/v1/health` (`Features` in `proto/proto.go`; hub adds extension features in `hub/extensions_link.go`). Clients enter new flows only when advertised; on old hubs, fall back to current pages. New registrations in this section: `assistant_readiness`, `machines_add`, `machine_trust`, `builtin_presets`.
- Enums are closed: unknown `state` displays "One step left + `title`"; unknown `action.id` displays text without a button; unknown `code` displays `error`.
- The server localizes `title` / `detail` / `error` using `Accept-Language` (`server/internal/i18n/locales/`). Clients may override with their own copy, but must work using server text alone.
- Never include secrets; keys return only `has_key`.

### 5.1 Main-chat readiness

```
GET /api/v1/assistant/readiness            Logged-in full device (not answer-only watches or extension tokens)
GET /api/v1/assistant/readiness?probe=1    Same, plus a real one-word check (costs money; repeated calls within 30 seconds return the previous result)
```

Lives under the assistant service, which already knows machine, launcher, and model configuration (`assistant/profile.go`, `models.go`); hub only forwards `/api/v1/assistant/*`. If the assistant process is disconnected, return 503 `assistant_offline`, like other assistant endpoints.

```json
{
  "version": 1,
  "mode": "setup",
  "ready": false,
  "next": "model",
  "machine": {"id": "mbp", "name": "Your MacBook"},
  "launcher": "claude",
  "steps": [
    {"id": "computer", "state": "ok", "machine": "mbp"},
    {"id": "claude", "state": "ok", "version": "2.1.308"},
    {"id": "model", "state": "problem", "code": "not_logged_in", "machine": "mbp", "launcher": "claude",
     "title": "Claude Code is not logged in yet", "detail": "Sign in on your MacBook, or use an API key.",
     "action": {"id": "connect_model"}, "alt_actions": [{"id": "login_claude"}]},
    {"id": "first_message", "state": "todo", "action": {"id": "test_assistant"}}
  ],
  "suggestions": [
    {"id": "what_can_you_do", "title": "What can you help me with?", "local": true},
    {"id": "today", "title": "What's on today", "prompt": "What's on my calendar today?", "needs": ["phone_calendar"]}
  ],
  "probe": {"ok": false, "millis": 2140, "error": "…"},
  "dismissed": false,
  "checked_at": "2026-11-06T09:00:00Z"
}
```

| Field | Values |
| --- | --- |
| `mode` | `full` / `setup` / `chat_only` (only appears in P1) |
| `ready` | `mode == "full"` |
| `next` | ID of the first step not `ok` / `skipped`; `null` if all ready |
| `machine` | Computer main chat will use, or the pending computer to confirm when none is available; `null` if neither exists |
| `launcher` | Effective launcher name (after `effective()`, overridden by main-chat purpose in model configuration) |
| `steps[].id` | Fixed order: `computer`, `claude`, `model`, `first_message` |
| `steps[].state` | `ok` / `todo` / `problem` / `checking` / `unknown` / `skipped` |
| `steps[].code` | Supplied for `todo` / `problem`, using codes in 4.6. `computer`: `no_machine`, `node_unreachable`, `machine_pending`, `node_outdated`; `claude`: `claude_missing`, `launcher_missing`; `model`: `not_logged_in`, `auth_failed`, `model_route`, `rate_limited`, `model_fallback_unavailable`, `tool_search_unsupported`, `upstream_unavailable`, `model_unverified`; `first_message`: `never_tried`, or the failure code |
| `steps[].action.id` / `alt_actions[].id` | `add_computer`, `confirm_computer`, `wake_computer`, `install_claude`, `login_claude`, `connect_model`, `edit_provider`, `test_assistant`, `enable_tls`, `open_admin` |
| `suggestions[]` | `local: true` opens client-local help without sending to a model; `needs` lists system permissions requested only on tap (client explains beforehand) |
| `probe` | Present only with `?probe=1`; the `{ok, model, answer, millis, error, assistant_turn?}` of the node's `launchers.check` run as the main chat's next turn (below) |
| `dismissed` | User tapped "Later" |

Evaluation order and data sources (all existing, plus one new node RPC):

1. `computer`: device list, online status, trust state (5.3). Later steps are `unknown` until the computer is `ok`.
2. `claude`: `claude-version` from node self-check (`node/doctor/doctor.go`), cached by the assistant service for 10 minutes.
3. `model`: new node RPC `claude.auth_status` (4.3, runs `claude auth status --json` in the launcher's actual environment, cached 10 minutes; a signed-out answer is asked again after a minute and waited for, an answer that said nothing — timeout, unreadable output — is asked again after a minute in the background while the last one is used); the most recent `?probe=1` or "Try a message" result; `turn_error_kind` of main chat's last turn. **No proactive token spending** by default. If never verified, use `unknown` + `model_unverified`, not `problem`; `mode` can still be `full`.
4. `first_message`: whether main chat has ever had a successful reply or a successful probe check (`?probe=1`). `todo` does not affect `mode`.

Storage: the latest check result and `dismissed` live in Postgres settings (one key, `assistant.readiness`), not files.

```
POST /api/v1/assistant/readiness/dismiss   {"dismissed": true}  →  {"ok": true}
```

`POST /api/v1/assistant/warm` keeps returning `{"ok": true}` and adds `readiness` (same structure, no probe). Clients refetch on entering the Persona tab, successful pairing, adding / confirming a computer, saving a provider, and receiving a `machines` change. No new push event in the first version.

The Apple clients also ask again when the app comes back to the front (iOS and visionOS: the scene becomes active; Mac: the app is activated), and while a page that shows readiness is in front and one of computer / Claude Code / model is `todo` or `problem`, or any step is `checking`, they poll at 5, 10 and 20 seconds, then every 30 seconds, starting over when the open steps change (`ReadinessPoll`, #1893). Installing Claude Code, signing in to it or waking a computer happen on that computer and change nothing in the app; the hub keeps a negative Claude Code answer for a minute, so the 30-second cap finds the change within about a minute and a half. `first_message` and `unknown` steps are not polled: only the user's own try moves them.

On old hubs, this endpoint returns 404: clients fall back to current behavior (no first-screen card; failures show cards by `code`).

Implementation (#3710, `server/internal/assistant/readiness.go`; endpoint details are authoritative in `docs/protocol/protocol.md`, "Main chat readiness") differs from or adds to the design:

- Full devices only: probing costs money, and hub does not expose launcher-running endpoints to answer-only devices; Watch does not need this currently.
- `model` takes the newest evidence from the last probe, main chat's last turn, launcher health records, and `claude.auth_status` (#4048). An unclassified failure is also `unknown` + `model_unverified`, not `problem`.
- A probe of a launcher whose health record on the main chat's computer is failing also updates that record (#10369): a pass clears it, as Settings' Test does, so the setup card goes at once and Agents' starts stop routing around it; a failure other than a single timeout updates the check time of a record still in force. Without a failing record nothing is kept (`docs/server/launcher-health.md`, "States"). The health rounds and check now also recheck a failing record of the main chat's launcher when a runtime runs it (agy, DSH…), which they otherwise skip.
- `claude.auth_status` (#4048): node runs `claude auth status --json` with the same command as chat creation (`launcherCommand`: the default is `claude` in the login shell, with this computer's `claude_start_dir`; Persona-managed launchers use `personad launch <name>`) and the same `--settings` (a managed launcher's gateway overrides env in the user's global settings). Put `--settings` before the subcommand (`auth status` itself does not accept it). Version gate: 2.1.41, when `claude auth` first appeared; do not run on older versions. Codex / runtime launchers, unreadable JSON, or no answer within 20 seconds return `unknown` + `reason`. Return only `state`, `auth_method`, `api_provider`, and the variable name identifying the key source; account email and organization never leave the computer. Default-launcher claude discovery is unchanged (#2854).
- New-conversation preflight (#4633): before an automatically selected private profile starts its first process, the node runs the same status command with that profile's `CLAUDE_CONFIG_DIR`. An Agent SDK start checks its resolved Claude executable directly, without also invoking the login shell's CLI. Only an explicit `loggedIn: false` refuses the start, as 409 `not_logged_in` with `delivered: "none"`, `connect_model`, a localized summary and the profile-specific command in `detail`. The model-provider action offers the API-key alternative without opening instructions for the wrong (default) profile. Unknown, old-version, unreadable and timeout results fail open. Explicit profiles, resumes, scoped nodes, launcher-owned profiles and managed providers do not run the preflight. No credentials are copied and no profile is changed automatically.
- Readiness interpretation: no login means `problem` + `not_logged_in`. A `claude.ai` login only disproves "not logged in": mark the model `ok` only with no other evidence or if the latest evidence said not logged in. It does not override main-chat, probe, or health-check `auth_failed` / `model_route` / `rate_limited` (being logged in does not mean the token is unexpired or the plan includes the model). Keys, gateway tokens, and Bedrock / Vertex (`api_key`, `api_key_helper`, `oauth_token`, `third_party`) are not proof; keep `unknown` + `model_unverified`, with `detail` saying "Using a configured account or key; not tested yet." Unlike the design, logged-in results cache for 10 minutes; not-logged-in or unanswered results (RPC failure, `timeout`, `unreadable`) cache only 1 minute. Old-node `unknown method` caches 10 minutes without logging (in `setup`, Send is replaced, so no other action will reveal that the user has signed in). Request alongside version self-check, sharing GET's 12-second / warm's 2-second budget; incomplete work still caches for the next request. Old nodes without this method behave as before.
- `rate_limited` is `problem`, but does not change `mode` to `setup` (self-healing; should not replace Send).
- The readiness payload's `launcher` names Main chat's launcher (Model configuration); its `claude` and `model` steps evaluate the launcher the main chat's next turn runs, and say which in the step's `launcher`: Main chat's, or the main chat's kept pick's when that is on another launcher (#10365, `readinessTurnLauncher`; not an external adapter's, which no probe can try). A pick of another model on the same launcher, or another conversation's pick, does not clear that launcher's limit. A scheduled run waits only on a step of the launcher it runs on (`readyFor`), so the main chat's pick on another launcher neither holds a routine on Main chat's launcher nor frees one. On iOS, the main-chat rate-limit card explains when the default model connection differs from the one this conversation uses. Its existing Later control dismisses only that failure occurrence in local presentation state: repeated reads stay dismissed, recovery resets the dismissal so a later genuine failure appears, and neither quota evidence nor the checklist's persisted `dismissed` choice is changed (`AssistantReadinessModel`, `ReadinessHomeCard`). The model step's optional `occurrence` uses the main turn's time when available, a health-only subscription reset window otherwise, or the newest evidence time for other failures. A settled health record keeps the turn's identity, repeated health checks stay dismissed, and another turn in the same reset window is still a new occurrence; older hubs without it retain the field-based fallback. The scope sentence explains that the warning concerns the default model connection while this conversation uses another one, without exposing raw launcher IDs.
- `claude` also reads two things: the code from the last rejected chat creation (`claude_missing` / `launcher_missing`, cleared after one successful open, a post-rejection self-check finds the version, or the launcher reappears—`setup` prevents sending, so clearing cannot depend only on success), and whether the launcher still exists on this computer. Codex or runtime launchers use `skipped` for this step. If node self-check does not finish in time, use `checking`. A self-check that finds no `claude` on a computer whose node reported a Claude Code version when it connected gives `unknown`, not `claude_missing`: Claude Code updating itself through npm removes and relinks its package, so a check caught mid-update would otherwise block Send for minutes. A launcher health record with a single "command not found: claude" on such a computer defers to the self-check the same way; a second such miss in a row gives `claude_missing`. The record counts these misses apart from its other failures (`missing_fails`): an earlier auth failure or timeout does not make the first miss a second one. A real removal still shows once a start is refused.
- `node_outdated` checks only whether node understands model configuration: every main-chat creation includes `model_fingerprint` (`finishModel`), which old nodes cannot open.
- Warm computes readiness for at most 2.5 seconds; on timeout, return the last recorded result (omit `readiness` if none), and finish / persist in the background. Opening the tab does not slow down for this.
- With no confirmed computer selected, readiness names a pending computer owned by this manager (the smallest ID when several wait): `computer` is `todo` + `machine_pending`, with `confirm_computer` and that computer's ID. The top-level `machine` carries its ID and display name. Main-chat creation returns 409 `machine_pending` with `confirm_computer` and `delivered: "none"`; it does not prepare or start a conversation on the pending computer. A selected confirmed computer, including one that is offline, keeps its existing readiness state. `node_outdated` has no button.
- `?probe=1` does not run unless computer is `ok`, and the response omits `probe`. A probe with no computer reply (offline, timeout) returns only `error`, is not recorded, and is not evidence.
- The probe takes up to 20 seconds to work out the next turn (`readinessTargetWait`), up to 170 seconds for the check (`healthCheckTimeout`; the node's own bound is 150 seconds) and then GET's 12-second self-check, 202 seconds in all. It does not stop when the client stops waiting: its result is recorded and the next try within 30 seconds gets it (#1892). Clients wait past that limit (web: `pastHub(PROBE_HUB_MS)`), and a try that gets no answer says so beside the button instead of showing an earlier try's result.
- The shared Apple client gives a probe 202 seconds, the hub's whole bound (an ordinary readiness read keeps its 30-second budget), so a slow try no longer ends on the client while the hub is still checking (#10452). Starting another try removes the previous try's result, and only the latest try may land or end its spinner. The checklist keeps the result beside the row whose button started it. A timeout, offline failure, or non-404 HTTP failure leaves its reason there instead of silently restoring an older result; cancellation stays silent, and 404 still means an older hub and clears every probe display (`APIClient.assistantReadiness`, `AssistantReadinessModel`).
- `dismiss` accepts `{"dismissed": false}` to undo; an empty body means `true`.
- Once `first_message` succeeds, it stays `ok` (stored in `assistant.readiness`, even after main chat changes).
- Completed main-chat success is retained in `assistant.readiness.success`, keyed by machine, launcher and model-binding fingerprint, with the original reply timestamp (#8480). The settled-turn callback records it even if no client is polling; readiness also records a successful live reply it observes. Releasing the process or restarting the manager does not revive an older failed probe. A superseded failed probe for the same machine and launcher is retired. Newer authentication, routing, sign-out and rate-limit evidence still wins; changed model bindings do not inherit the old success. Offline text alone is not success evidence, and ordinary readiness reads start no model probe.
- Model problems `auth_failed` and `model_route` also offer `test_assistant` as an alternative to editing or connecting the provider, so a repaired model can be verified without replacing the current chat after its first answer (#1894).
- The probe checks what the main chat's next message would run, not the launcher's default (#10365; `assistant/readiness_probe_target.go`). It used to send `launchers.check` with the Main chat launcher alone: the check answered on that launcher's default model, in the login shell's profile, with no tools, and its pass cleared a `model_route` the chat itself kept hitting. Now the send path's own resolution (`sendModel`: the chat's kept pick, else Model configuration › Main chat, a launcher that can't run it given way) picks the launcher, model and effort, read-only within 20 seconds; a pick the message would be refused on is the probe's error, with nothing run or recorded. The node runs the check as a turn of the main chat (`assistant_turn`, `node/launchercheck_assistant.go`): the chat's profile (`CLAUDE_CONFIG_DIR`, only read), `PERSONA_ASSISTANT=1` and the turn's `ENABLE_TOOL_SEARCH`, the launcher started as the assistant starts it, Persona's own MCP server alone with the chat's `--tools` / `--allowedTools`, `--model` / `--effort`; it stays a check (one word, one turn, nothing saved, no hooks), and a tool call the one turn did not run counts as an answer. The record keeps the launcher, model and effort; evidence and reuse match machine, launcher and the model the next turn asks for there, so a pass on another model, or one an older hub kept without a model, clears nothing. The model step is judged on the same target: with a kept pick on another launcher than Main chat's, the step is that launcher's, and the main chat's failed turn on Main chat's launcher (a pick only records the launcher, the next turn moves the chat there) stands against it until a newer pass of the picked model there, as a failed turn does after a pick on the same launcher; a pass of Main chat's own model, or of another model of the pick's launcher, clears nothing. A pass from a node that ran the plain check (no `launcher_check_assistant_turn`, no `assistant_turn` in its answer) does not clear the main chat's own failed turn on a Claude Code launcher the node runs itself; failures count either way. Codex, other agents and a runtime's launcher (the runtime's own probe) only ever get the plain check, so its pass of the turn's model clears the failed turn as it did before, or "Try it" could never unblock such a chat (`readinessCheckShaped`). A kept pick on an external adapter's launcher is not checked at all (the node refuses every external name, `adapter_probe_unsupported`): the probe's `error` says the connection can't be tried on its own, nothing is recorded and the first message is not counted as answered. Settings › Models' Test and launcher health keep the plain check.

### 5.2 Structured main-chat errors

On failure from `POST /api/v1/assistant/conversations` or a session-send endpoint:

```json
{"code": "no_machine", "error": "Persona needs one of your computers to do work. No computer is connected yet.",
 "detail": "assistant machine is not configured", "action": "add_computer", "request_id": "…"}
```

- `no_machine`, `claude_missing`, `launcher_missing`, and `machine_pending` return 409; `node_unreachable` is unchanged (coded 502, `ErrOffline` 503); `assistant_offline` is unchanged (503).
- `detail` contains the original text; clients show it only in "Details."
- For turns with a recognized cause, sessions carry `turn_error_code` (`not_logged_in` / `auth_failed` / `model_route` / `rate_limited` / `model_fallback_unavailable` / `compaction_sealed` / `tool_search_unsupported` / `upstream_unavailable`), which clients and server lists use for fix cards or stall visibility.
- `upstream_unavailable` identifies an explicit upstream account-pool outage (`no eligible upstream account`, `no other eligible account`, `upstream attempt budget …`) or `>= 2` consecutive upstream `5xx` failed turns with no assistant progress in between (#13372). Nothing on the user's computer is broken, so readiness offers `test_assistant` with plain-language copy (`assistant-readiness.upstream_unavailable_*`) and never puts readiness into `setup` mode or blocks sending.
- A `model_route` card's "Fix it" opens the chat's own model picker (client action `pick_model`), where another model is picked for the chat; where the page has none it opens model settings, never the add-provider wizard, which only fits `not_logged_in` (#10365). While the chat keeps a pick, the card also offers the picker's way back (follow the default again, or undo the pick). A gateway that refused on-demand tool loading (`tool_search_unsupported`) gets a card saying so with Retry, worded to promise only the retry (the card can't tell whether the chat's computer runs a node that now sends every tool there); clients also read it from the 400's words for a hub that does not code it. Web, Apple and Android do both. On Android the turn card opens only the conversation's existing picker, never also the Mode sheet; a conversation without its own picker uses Model settings, and a kept pick exposes the picker's existing **Use the default** action rather than writing a redundant pick. On the web and Apple the readiness card's and the blocked send button's `connect_model` for a `model_route` model step opens the same picker (`pick_model`; the hub's action is unchanged for the other clients), and once another model is picked there, `test_assistant` becomes their primary action, since only a pass of the picked model clears the main chat's failed turn, on the launcher it was picked on, whichever that is (the model step is judged on the next turn's launcher and model, 5.1; `docs/web/web.md`, "What the main chat still needs (#3847)").
- `model_fallback_unavailable` identifies a model-route failure after Claude Code switched the session following an upstream refusal (#2203). Readiness explains the chain and recommends a new conversation or `/model` in the terminal, with no model-settings or test-message action. This session-only failure does not put readiness into setup mode or prevent creating a new conversation. Existing client error cards still need to adopt this code and hide their old Retry/Continue actions; the ordinary refusal notice already carries localized recovery guidance.
- `tool_search_unsupported` identifies a gateway that refused Claude Code's on-demand tool loading (tool search, #10365) after the node's one automatic resend with every tool loaded. It is not the user's to fix and not a model route: the node starts that launcher and model with every tool from then on, so readiness offers `test_assistant` and the step does not put readiness into setup mode. Words: "The gateway refused on-demand tool loading" / "Try again" (`assistant-readiness.tool_search_unsupported_*`); only for a computer whose node announces `assistant_tool_search_fallback` (the resend and the remembered refusal) does the detail say "Persona sends every tool to this gateway from now on, so try again" (`tool_search_unsupported_fallback_detail`): an older node keeps tool search on, and a retry there is refused the same way.
- `images_too_large` identifies a turn that failed because accumulated pictures in the conversation exceeded the model API's limit ("Request too large", "could not be processed and was removed", "many-image requests", #13045). It suppresses automatic Continue (`turn_error_kind: "user"`) and does not put readiness into setup mode or offer a model-settings action; clients explain it in one plain sentence, keep the raw error under Details, and offer **Compact and send again** where the session supports compaction.
- `errNoAssistantMachine` (`start.go:29`) and `memory()` (`profile.go:465`) use the same code; do not let them drift apart. Update voice-prompt goldens (`server/internal/voice/testdata/prompts/refused.failed.*.golden`, all three languages) too.
- Old clients still display `error`, now localized rather than raw English. Check that iOS `isQuiet` does not treat 409 as a silent error.

### 5.3 Logged-in devices add and confirm computers

Extend the existing pairing-code endpoint, rather than creating another system:

```
POST /api/v1/auth/pair-codes      Full device (not a temporary pairing-code identity), or loopback + main token
{"add": {"name": "Your MacBook", "os": "mac", "icon": "laptop"}}
or {"machine": "example-macbook"}            Regenerate a code for an existing link computer
```

```json
{
  "token": "jp_…", "expires_at": "…",
  "machine": "example-macbook", "trust": "pending",
  "pair_url": "persona://link?url=…&token=…&machine=example-macbook&hub=…&fp=…&tls=…",
  "command": "persona-link pair '<pair_url>'",
  "install": {"linux": "curl -fsSL <site>/install.sh | sh -s -- --node --pair '<pair_url>'"}
}
```

- `add.name` may be Chinese; generate the ID from the name (ASCII conversion + deduplication, satisfying `/admin` ID rules). Do not ask users for an ID that "cannot be changed later." `add.os` only determines which installation instructions the client shows.
- With `add`, hub writes hub.json and calls `addMachineLive` using the link branch of `POST /api/v1/admin/machines`, while writing a `pending` trust record to Postgres. Loopback + main token writes `confirmed` (or no record).
- Reuse an existing same-name link computer that has never connected, generating a new code.
- `<site>` comes from configuration, not a hardcoded value.
- Errors: 403 (not a full device, or a device obtained from a pairing code and not yet confirmed); 409 `tls_required` (`action: "enable_tls"`); 409 `mesh_member`; 429 `too_many_pending` (at most 3 computers with unredeemed codes or pending confirmation at once).
- Remove the device restriction at `pairing.go:132-135`, but allow only `add` / existing link computers.
- Implementation (#3735) differs from the design; `docs/protocol/protocol.md` ("Computer pairing codes") is authoritative. Devices may generate codes only for **pending** computers (`machine` or same-name reuse); confirmed ones return 409 `not_pending`.
  Thus device-generated codes always go through "pending → confirmed." Re-pair confirmed computers in the admin UI or on the hub machine (client M3 cannot regenerate codes for confirmed computers in the app). This is not a security boundary: full devices can already use
  `POST /api/v1/auth/tokens` (`kind: link`) to issue credentials for any link computer; tightening that is separate work. Before regenerating a pending computer's code, revoke its existing Persona Link credentials (credentials redeemed from the old code must not become confirmed alongside the new ones). `add` may include `id` (for `hub pair --machine <id> --add`). The response always includes `trust`
  (`pending` / `confirmed`), plus `name` and `added`. If hub does not know its release website (local build, Docker image), omit `install`. A main-token request with nonexistent `machine` returns 400 `unknown_machine`.
  Receipt notification text includes the computer name: "<device> generated a pairing code for computer '<name>'." The notification's "Not you? Revoke" button is not implemented yet (requires notification categories on each client).


Add two optional fields to every machine in the list (`GET /api/v1/machines` and hello machines):

| Field | Values |
| --- | --- |
| `trust` | `pending` / omitted (= confirmed) |
| `error_code` | `node_service_down`, `link_offline`, `never_connected`, `node_outdated`; retain original `error` text |

```
POST /api/v1/machines/{id}/confirm    Full device   →  {"ok": true}       pending → confirmed, then push keys
POST /api/v1/machines/{id}/reject     Full device   →  {"ok": true}       Delete hub.json entry and record; revoke link credentials
```

Pending computers: skipped by `pushManaged`, `defaultAssistantMachine`, and assistant machine candidates; absent from computer pickers for new Agents, dispatch, and to-dos; automatically rejected after 24 hours without confirmation. Code generation, redemption, confirmation, rejection, and expiry deletion all create Postgres audit records and notify other paired devices ("<device> generated a computer pairing code," "New computer <name> wants to join").

`personad hub pair --machine <id> [--add] [--name <name>]`: uses loopback + main token; `--add` creates a missing computer directly as confirmed.

### 5.4 Providers: adding from presets and `assign`

`GET /api/v1/launchers/presets`: returns "built-in + plugins," sorted by request language. Add optional fields to `LauncherPreset`:

| Field | Description |
| --- | --- |
| `plugin` | `"builtin"` for built-ins |
| `signup_url` | Page for signing up / buying a key; clients open it in the in-app browser (set only for built-in presets) |
| `display_name` | Localized display name (such as Zhipu GLM in Chinese) |

Two additions to existing `POST /api/v1/launchers`:

```json
{"preset": "builtin/deepseek", "api_key": "…", "name": "", "display_name": "",
 "assign": ["assistant", "agent.new"]}
→ {"launcher": {…, "name": "deepseek", "display_name": "DeepSeek", "has_key": true},
   "assigned": ["assistant"], "kept": [{"purpose": "agent.new", "reason": "customized"}]}
```

- With `preset`, `name` may be empty: use the vendor slug, adding `-2` on collision, satisfying `managedLauncherName` (`models_launchers.go:98`, ASCII only). If `display_name` is empty, use the preset's value. Without `preset`, `display_name` may also be Chinese.
- `assign`: after saving, PATCH each listed purpose, **only if that purpose remains at its untouched default**. Split results into `assigned` and `kept`. Clients do not need their own "read revision, then PATCH" sequence.
- With `preset`, existing `POST /api/v1/launchers/test` uses the preset's default model if `model` is omitted.

### 5.5 Command line

| Command | Output |
| --- | --- |
| `personad hub setup [--json]` | One localized line per step; with `--json`, one `{"step","ok","detail","next"}` per line, followed by `{"done":true,"pair_url","admin_url","admin_local_only","setup_code","expires_at"}` for Persona Link / all-in-one Mac callers |
| `personad hub pair --machine <id> --add` | Same as today, with an extra "Added: <name>" line |

## 9. Compatibility

- New defaults go only into newly generated hub.json; no field changes in existing files, and no new defaults in `defaultConfig()` (4.1).
- `deploy.sh`, `db.sh`, `hub-systemd.sh`, socket activation, and handwritten hub.json work as before. The database container created by `hub setup` has the same name and volume as `db.sh`.
- `~/.zshrc` launchers are still discovered and selectable; onboarding simply does not mention them.
- The `personad` command, `persona://pair` / `persona://link` links, and `PERSONA_*` environment variables remain; Persona Link recognizes both schemes.
- Errors change from 502 `internal` to 409 + code: old clients already display non-2xx `error` as text; only the language changes.
- Old node and Persona Link do not need to understand pending; filtering lives entirely in hub and the assistant service.
- New endpoints are registered in `features`; new clients fall back to current pages on old hubs.
- Name each new Postgres migration by the UTC second it is written (`docs/server/server.md` "Migrations"; the trust table needs one; the readiness key needs no new table).

# CLAUDE.md
## Product positioning: an Agent product for everyone

Persona is an Agent product for everyone, not a tool built only for software developers.

- **Assistant** is for everyday people: plain language, simple defaults, no engineering jargon.
- **Agents** is for people who collaborate directly with tools such as Claude Code; specialist workflows, engineering terminology, and advanced configuration belong here or in advanced settings.

Every design names its intended audience and answers "who does this serve?". Public-facing experiences stay simple; complexity lives in Agents or advanced settings.

**Fast, local-first and stateful:** show local feedback immediately and Processing as soon as the hub receives a message,
not after scheduling or model output. Keep receipt, delivery and execution distinct, make failures recoverable, and measure
feedback latency separately from execution latency (`docs/design/PRINCIPLES.md`, principle 7; super rule 5).


## Highest-priority rules

Read [the super rules](docs/SUPER_RULES.md) before acting. They take precedence over this file, `AGENTS.md`, and all
other repository documentation. Changes to the super-rules file, including additions and deletions, require a
GitHub issue and explicit approval from administrator `chyroc` in that issue before editing or opening a PR.
The PR must link the approved issue. No Agent or delegated task may bypass this requirement, even to fix a failing check.

**Product positioning: Persona is an Agent product for everyone, not only software developers.**

- **Assistant** is for everyday people, with no programming or Agent-tool knowledge required.
- **Agents** is for people who want to collaborate directly with Claude Code and other Agents.
- Before every design decision, ask **who it serves**. Keep Assistant and other everyday-facing interfaces simple and
  understandable; do not let developer terminology, engineering details or accumulating options make them harder to use.
  Put specialist workflows in Agents or advanced settings, and disclose details only when needed. For example, asking
  Assistant to organize a trip should not require choosing an Agent runtime or a repository.

Persona is a self-hosted personal SuperAgent. A Go server, `personad`, runs as the **hub** on an always-on machine and holds all state;
the web app (served by the hub) talks only to the hub. Each of your computers runs a **node**
(connected to the hub through Persona Link), where coding Agents (Claude Code, Codex, pi, opencode and other runtimes) run in an engine
(herdr, Orca, Tern or Paseo); the web app can watch them live, answer their questions and approve plans and tools.
The assistant's main chat (the **Persona** tab) hands work to those Agents and draws on the knowledge base (`~/.persona/data`), the Inbox,
mail and extensions (connectors, channels, voice). Overview: `README.md`, `docs/server/architecture.md`.

Values specific to your machines and accounts go in `CLAUDE.local.md` (not committed); without it, set up from `docs/operations/config.md`.

**Names**: the product is Persona; the binary is `personad`, the connector `persona-link`, the runtime directory `~/.persona`, the build
config `~/.config/persona/config.json`, environment variables `PERSONA_*`. Older names still work so old and new versions interoperate,
but their literals live only in each platform's compatibility files (`docs/protocol/rolling-upgrade-compatibility.md`); everything else
uses Persona.

## Conventions (mandatory)

### Workflow

- **Every change goes issue → claim → branch → PR → merge after local verification passes** ("Development workflow" below). Code, docs and config alike;
  never commit or push to `main` directly.
- **Old issue numbers**: `#N` in lines written up to 2026-11-07 (`git blame`) refers to the previous tracker; new text cites current numbers (`docs/operations/collaboration.md`, "Rules").
- **Write lessons down as you go**: pitfalls into `docs/lessons/<topic>.md` (index `docs/lessons/README.md`), design trade-offs into the
  module doc. Only the cross-module pitfalls people hit most get a line in "Most common pitfalls" here; keep this file short.
  One-off plans, acceptance reports and audits go to `docs/archive/` when done. New maintained docs go into the Docs index below
  (`scripts/test/test_doc_paths.py` checks the index and every quoted path).
- **Quality bar**: check every changed screen in a browser screenshot against the mock hub, dark and light, and actually look at it;
  a passing build is not enough (`docs/operations/testing.md`).
- **Screenshots go in the PR, never in the repository**: review evidence (light / dark, before / after, UI test captures) is shown in the
  PR body or a comment, not committed to `main`, docs included, and never pushed on a branch of their own (`artifacts/*`, `pr-assets/*`:
  every clone fetches them; `docs/operations/collaboration.md`, "Screenshots in PRs"). Only the README images
  (`docs/assets/`) and the web app's and website's resources are committed. `scripts/test/test_no_committed_screenshots.py` blocks new ones.

### Public repository

This is a public open-source repository; everyone and every Agent maintains it to that standard.

- **Write as if anyone can read it**: code, docs, comments, test fixtures, file names, commit messages, issue and PR titles, bodies and
  comments. Edit history is public: changing the text later does not remove it, so don't write it in the first place.
- **No internal names**: internal company services, internal repositories, internal tools and intranet access setups never appear.
  Capabilities that need them are extensions kept in a private extension repository; this repository has only generic extension points and names.
- **Issue scope**: do not open issues here for changes to a private extension repository. If this repository also needs changes,
  describe only the work needed here in its issue, not the work in the private repository.
- **No private information**: no intranet domains / IPs / URLs, host names, real people's names or messages, personal accounts, mail
  addresses, tokens, keys or project IDs. Examples use placeholders (`<hub address>`, `me@example.com`). `scripts/privacy_check.py` checks
  (`git config core.hooksPath scripts/hooks` runs it on commit and push; `docs/operations/testing.md`, "Privacy scan").
- **Make it configurable**: machine names, domains, accounts, paths, launchers and preferences come from config (build / deploy:
  `~/.config/persona/config.json`; hub runtime: `~/.persona/hub.json`). The UI says "the hub machine" and "your computers".
- **Secrets never enter the repository**: the hub token and database password live only in `~/.persona/hub.json` (0600); mail
  credentials are AES-GCM encrypted in Postgres with the key file kept out of the database.

### Language

- **English for everything people read in the repository**: code comments, docs, commits, PR titles and bodies (a squash merge makes them
  the commit on `main`), issues and comments. Natural, concise English, not word-for-word translation. `docs/` is English only.
- **The README is bilingual**: `README.md` and `README.zh-CN.md` carry the same content and change in the same PR.
- **User-facing text always goes through localization resources** (`docs/protocol/i18n.md`):
  web `web/src/i18n/locales/` and hub `server/internal/i18n/locales/`. Every new string ships in `en`,
  `zh-Hans` and `ja` together; the checks catch gaps.
- Chinese stays only as data that must match something real (hub or third-party output, prompts sent to a model, fixtures), marked
  `// i18n-ignore` where the checks ask.

### Server and data

- **All server state lives in the Postgres container** `persona-postgres` on the hub machine (`docs/server/server.md`); no JSON state files
  in `~/.persona/`. The exception is the knowledge base `~/.persona/data`: files first, the database as the index (`docs/assistant/knowledge.md`);
  its full-text search index (`.search/`) is a cache built from the files that can be deleted at any time (`docs/assistant/knowledge.md`, "Search index (#1273)").
- **The hub is only a communication center**: new business logic (endpoints, tables, model calls) goes into the managers
  (`server/internal/assistant`, `server/internal/devices`, `server/internal/extmgr`). `server/internal/hub/architecture_guard_test.go` pins the hub's routes,
  dependencies and Store; when it fails, move the logic as its hint says (`docs/server/architecture.md`).
- **Prompts live in `server/internal/prompts/`** (MCP tool descriptions under `mcp/`): one prompt per file, composed with templates, never
  built with `fmt` / `+` in Go. Update the golden tests and read the diff (`docs/assistant/prompts.md`).
- **Prompt language**: user- and agent-facing templates carry `Language` / `Lang` from the caller and add en / zh-Hans / ja goldens. Preserve source data and existing empty-language bytes; follow `docs/assistant/prompts.md` ("Language migration"). The prompt-language CI inventory may only shrink.
- **Browsers reach the hub directly over the internet** (TLS, preferring IPv6; VPNs are the user's own business), with the LAN kept
  compatible (`docs/operations/network.md`).

### Product and UI

- **Links open inside the app**: any link in a conversation opens in a card rising from the bottom that fills the screen; dragged down to
  0.6 it stops blocking the chat, further down it closes. Speed first (preloading, caching, reused WebViews). Selected text can go to the
  Agent as **AI edit** or **Use as context**, carried in the message as XML tags.
- **Never change how an existing screen looks directly**: a different visual style is a new **style**, a different color set a
  new **theme color**; the default and every other option stay pixel-identical. Bug fixes and changes the user asked for
  don't count.
- **The UI uses only the design system**: font sizes, spacing, radii, sizes, animation, colors, icons and brand marks come from tokens;
  pages never hard-code numbers or hex colors. Missing something? Add a token first.
- **Button text never wraps**: one line (truncate, shorter text or an icon); what doesn't fit in a row
  of buttons goes into a menu.
- **Persona Link is installed separately**: `persona-link` (the connector's CLI/service) is installed only on computers whose Agents the
  hub should reach; it owns node connectivity. The web app needs neither a local node nor Link (`docs/operations/link.md`).

### Design spec for UI changes

- **Design principles**: `docs/design/PRINCIPLES.md` holds the seven product principles (defaults first, progressive disclosure,
  user control, consistency first, model and product separate, checkable rules, fast/local-first/stateful), each with how to check it. They apply to UI,
  settings and defaults, and how numbers are counted; acceptance criteria and the PR template's self-check are written against them.
- **Read the spec first**: before adding or changing any UI or interaction, read
  `docs/design/PRINCIPLES.md` and the sections of `docs/design/DESIGN_SYSTEM.md` your change touches. Follow them; they win over
  habits copied from other screens.
- **Structure rules** (`docs/design/DESIGN_SYSTEM.md` sections 3.4, 4.8 and 5.6): in every grouping a session that needs the user is
  visible without opening anything; each pending request has one home, and every place that collects requests shows the same
  number; primary screens use the session's title and plain state words, never raw model IDs, session IDs, paths or engine names;
  a large UI change needs a person to look at its screenshots (section 1.4).
- **Say what you followed**: a PR that changes UI or interactions lists the principles and rules it followed, and for every
  deviation the reason. Run the "UI change self-check" in `docs/design/DESIGN_SYSTEM.md` and put the result in the PR.
- **Spec and code change together**: a new component, token or interaction pattern updates `docs/design/DESIGN_SYSTEM.md` in the same PR.

## Development workflow

People and Agents follow the same steps, each with their own GitHub account. Templates and copyable `gh` commands: `docs/operations/collaboration.md`.

1. **Look for duplicates**: `gh issue list --state all --search "<keywords>"` and `gh pr list --state all --search "<keywords>"`, closed
   and merged ones included.
2. **Claim the existing issue, or create one** (`enhancement` for features, `bug` for fixes) saying what and why.
3. **Claim before you start**: comment `Claim:` / `Plan:` / `ETA:`; an Agent adds `Agent: yes (<machine id> / <branch or session>)`,
   using the config's machine id, never the host name. The author of the latest claim owns the work; nobody is assigned.
   Work that touches UI, settings or defaults, or how something is counted adds `Acceptance:`, criteria written against
   `docs/design/PRINCIPLES.md` (which principles, and what evidence shows each is met).
4. **Claimed by someone else**: if the owner updated it in the last 24 hours, leave it alone (adding information is fine). After 24 hours
   without an update, comment `Take over:` with the reason. Renew your own claim with `Progress:`.
5. **Work on your own branch / worktree** named with the issue number (`fix/123-xxx`). Bring it up to date by merging `origin/main`
   (preferred over a rebase; never force-push). Giving up: comment `Abandon:` with how far you got.
6. **Open one PR per issue**, in English, with `Fixes #N` / `Closes #N` (or `Refs #N` to link without closing) in the body. Fill in
   `.github/pull_request_template.md`, including the principles self-check when the change touches UI, settings, defaults or counts.
   `.github/workflows/pr-needs-issue.yml` closes PRs that link no issue and reopens them once the link is added.
7. **Merge after local verification passes**: run the checks in "Build and verify" for every affected area, including workflow lint for
   CI changes, and look at screenshots for UI changes. Opening or updating a PR does not run hosted CI. A scheduler dispatches full CI
   on `main`; its failures are fixed separately and do not replace the PR's local verification (`docs/operations/testing.md`, "Hosted CI").
   Then `scripts/hooks/merge-pr.sh <number>` (scans the PR with this machine's private values, which CI does not have, then squash-merges
   and deletes the branch). Make sure the issue is closed.

## Build and verify

- Portable CI: `scripts/ci/server.sh`, `scripts/ci/web.sh`, `scripts/ci/scripts.sh`;
  runtime/SDK checks: `scripts/ci/runtimes.sh`. Same scripts locally and on hosted runners, no private config or signing secrets.
  Dependencies, optional path filters, coverage and cost: `docs/operations/testing.md` ("Hosted CI"). Manual dispatch defaults to all
  platform checks; no hosted repository guards run automatically on PRs.
- Server: `scripts/test/test-server.sh` (real Postgres on the hub machine); portable CI creates its own disposable Postgres 18 container.
  Deploy: `scripts/deploy/deploy.sh [machine id…]`.
- Web: `cd web && npm test && npm run build`, the build again after merging the latest `origin/main` (it prints the first screen's room under
  `FIRST_SCREEN_BUDGET`; changes in flight share it); deploy with `scripts/deploy/deploy.sh web` (`docs/web/web.md`).
- Docs: `python3 scripts/test/test_doc_paths.py`, `python3 scripts/test/test_doc_go_paths.py`, `python3 scripts/test/test_doc_sections.py`
  (a section name quoted after a doc path names a current heading) and `python3 scripts/test/test_doc_wire.py`
  (every WebSocket type, feature flag, extension point and manifest field has its row). Every change: `python3 scripts/privacy_check.py`.
- Portable count ratchets (fixed sleeps, web font sizes, prompt language, hard-coded text, prompt sizes) fail
  only when a count rises, so a PR need not lower a baseline; `python3 scripts/ci/lower_baselines.py` is tidying anyone can run
  (`docs/operations/testing.md`, "Count ratchets").
- Releases: `docs/operations/release.md`.

## Most common pitfalls

The full list is in `docs/lessons/`; read the file for the area you touch first.

- **Several Agents share this repository**: work in your own worktree from `origin/main`, stage only your files, and check that
  `git diff origin/main...HEAD --stat` lists only them before pushing. Use your own mock hub port (check with `lsof`; stop a
  mock by its port, never `pkill -f mock_hub.py`). Name a new database migration by its UTC second, `$(date -u +%Y%m%d%H%M%S)_<name>.sql`,
  never a hand-picked number (`docs/lessons/workflow.md`).
- Scripts that wait for background jobs or logs always have a timeout and end with a one-line verdict on success, failure and timeout.
- Someone else's redeploy can wipe the hub's memory at any time: flows that span requests must be self-contained or persisted.
- The hub's port belongs to `personad.socket` (socket activation) and versions switch blue-green with `personad hub switch`. Before going
  back to a binary that doesn't understand `LISTEN_FDS`, run `PERSONA_HUB_SOCKET=0 scripts/deploy/hub-systemd.sh`; to stop the hub, stop the
  socket too (`docs/operations/hub-restart.md`).
- Behind a container port mapping every connection can look like the bridge gateway, and a same-machine reverse proxy makes internet
  traffic look like loopback: never trust the source address alone (`containerGateway`, `security.proxy_protocol`; `docs/operations/network.md`).
- Claude Code's config directory isn't always `~/.claude` (`CLAUDE_CONFIG_DIR`), and one session id can exist in two; take the most
  recently modified. Verify transcript or sync changes against real transcripts
  (`docs/lessons/agents.md`, `docs/lessons/transcript.md`).
- Claude Code's transcript and hook `cwd` follow the model's Bash `cd`: place assistant and track conversations by the directory they
  started in, never their latest cwd (`docs/lessons/transcript.md`, #2368).

## Layout

| Path | Contents |
| --- | --- |
| `server/` | Go module: `personad` (hub / node / hook / mcp and helper commands), `persona-link`; `docs/server/server.md` |
| `web/` | Web app (Vite + React + Tailwind) served by the hub (`docs/web/web.md`) |
| `integrations/` | Agent runtime integrations and the runtime SDK (`docs/integrations/runtimes.md`) |
| `native/iphone-bridge/` | Optional Rust bridge a node runs to operate a paired iPhone, built on its own (`docs/extensions/iphone-bridge.md`) |
| `sdk/`, `schema/`, `spec/` | Extension SDK, extension JSON Schemas, shared behavior specs and fixtures |
| `scripts/` | Build, test, deploy, release, mock hub and config tools (`scripts/README.md`) |
| `deploy/docker/`, `Dockerfile` | Docker deployment of the hub (`docs/operations/docker.md`) |
| `site/` | Website, privacy policy and terms (`docs/operations/site.md`) |
| `docs/` | Design docs (`docs/README.md`, index below) |

## Docs index

Every maintained doc under `docs/` (lessons excluded) must be listed here.

| Area | Docs |
| --- | --- |
| Agent-to-Agent communication | `docs/integrations/a2a.md` session addresses, durable delivery, and the A2A JSON-RPC interface |
| Super rules | `docs/SUPER_RULES.md` highest-priority rules and prior administrator approval for changes |
| Start | `docs/README.md` navigation · `docs/operations/getting-started.md` setup without a terminal · `docs/assistant/onboarding.md` new-user onboarding · `docs/operations/config.md` config.json and hub.json · `docs/operations/collaboration.md` claims, PRs, templates · `docs/protocol/i18n.md` localization |
| Feature map | `docs/feature-map.md` source-verified capabilities, public entries, implementation, test and deployment evidence, gaps and maintenance protocol |
| Architecture and protocol | `docs/server/architecture.md` · `docs/protocol/protocol.md` HTTP / WebSocket API and node RPC · `docs/protocol/file-transfer.md` receive folder and computer to computer transfers · `docs/protocol/agent-states.md` · `docs/protocol/deep-links.md` one route table for links to Persona's pages: web paths, `persona://open<path>`, older forms, security · `docs/protocol/multi-hub.md` one device, several hubs · `docs/protocol/connector-protocol.md` · `docs/protocol/rolling-upgrade-compatibility.md` old / new name interop · `docs/operations/rename.md` naming contract · `docs/operations/persona-cutover.md` approval-gated naming cutover tool |
| Server | `docs/server/server.md` modes and operations · `docs/server/mesh.md` linked hubs · `docs/server/machines.md` monitoring · `docs/server/usage-windows.md` subscription usage · `docs/server/launcher-health.md` · `docs/server/agent-sharing.md` trusted ownership and public sharing contract · `docs/server/agent-sandbox.md` macOS agent sandbox |
| Operations | `docs/operations/network.md` direct connection, TLS, pinning · `docs/operations/docker.md` · `docs/operations/hub-restart.md` seamless restarts · `docs/operations/hub-update.md` · `docs/operations/upgrade.md` moving an install to the split hub and manager · `docs/operations/push-relay.md` · `docs/operations/admin.md` admin UI · `docs/operations/ports.md` port forwarding · `docs/operations/clipboard.md` clipboard sharing with the phone · `docs/operations/link.md` Persona Link and the node service · `docs/operations/persona-link-install.md` · `docs/operations/node-update.md` · `docs/operations/mac-node-signing.md` signing a Mac's node with its own certificate so privacy permissions survive updates · `docs/operations/windows.md` · `docs/operations/auth-relay.md` · `docs/operations/site.md` website · `docs/operations/testing.md` tests, mock hub, privacy scan · `docs/operations/developing-persona.md` the Developing Persona switch: a device's logs, errors and energy data, the package format and scrubbing · `docs/operations/pr-review-automation.md` maintainer-side semantic PR review and exact-head merging · `docs/operations/main-verification.md` independent main build/vet/rule verification and bounded repair PRs · `docs/operations/release.md` releases: publishing, release PRs, Darwin CLI signing |
| Agents and runtimes | `docs/integrations/engines.md` herdr / Orca / Paseo · `docs/integrations/claude-integration.md` · `docs/integrations/codex.md` · `docs/integrations/codex-app-server.md` · `docs/integrations/runtimes.md` runtime spec · `docs/integrations/daemons.md` · `docs/integrations/dsh.md` · `docs/integrations/managed-agents.md` Managed Agents sessions (Ark cloud or self-hosted, your own key) · `docs/integrations/pi.md` · `docs/integrations/omp.md` Oh My Pi (pi family) · `docs/integrations/opencode.md` · `docs/integrations/paseo.md` · `docs/integrations/agent-goal.md` · `docs/integrations/terminal.md` · `docs/integrations/launchers.md` model providers · `docs/integrations/script-cli.md` `personad ask` / `notify` / `decision` |
| Assistant | `docs/assistant/assistant.md` main chat · `docs/assistant/approval-clarification.md` owner questions on prepared approval cards · `docs/assistant/managers.md` · `docs/assistant/assistant-views.md` rich UI · `docs/assistant/unified-entry.md` · `docs/assistant/agent-work.md` work state and triage · `docs/assistant/north-star.md` interruption and availability metrics · `docs/assistant/todos.md` · `docs/assistant/todo-status-sop.md` to-do status sections · `docs/assistant/voice.md` · `docs/assistant/urgent-call.md` urgent calls: when the assistant rings the owner's iPhone, the gates, the call and its fallbacks · `docs/assistant/tracks.md` · `docs/assistant/knowledge.md` knowledge base · `docs/assistant/meeting-minutes.md` minutes from recorded meetings · `docs/assistant/day-recap.md` all-day recording and the daily recap · `docs/assistant/inbox.md` · `docs/assistant/model-config.md` · `docs/assistant/model-profiles.md` · `docs/assistant/prompts.md` · `docs/assistant/office.md` Word / PowerPoint / Excel |
| Extensions and sources | `docs/extensions/extensions.md` extension system · `docs/extensions/source-api-handlers.md` proposed authenticated source API, assistant handlers, bounded media and durable confirmation, with independent credential/session subcontracts (#1185) · `docs/extensions/volcengine-speech.md` the built-in Volcengine speech extension: meeting transcription on the hub and answers read aloud with the owner's key, one switch, connection test · `docs/extensions/extension-settings.md` host-owned settings layout · `docs/extensions/telegram-mtproto-session-transport.md` proposed controlled MTProto sessions and synthetic-only design evidence · `docs/extensions/secret-providers.md` secret values from provider extensions (references, resolution, records) · `docs/extensions/repository-providers.md` code-hosting sources from provider extensions (hosts, `repo.*` calls, when the provider is gone) · `docs/extensions/connectors.md` · `docs/extensions/subscriptions.md` · `docs/extensions/channels.md` · `docs/extensions/extension-messaging.md` · `docs/extensions/computer-use.md` · `docs/extensions/iphone.md` phone control · `docs/extensions/iphone-bridge.md` operating a paired iPhone · `docs/extensions/iphone-control.md` element numbers, results, limits and approval for the iPhone · `docs/extensions/iphone-pairing.md` iPhone control: pairing and sessions · `docs/extensions/mail.md` · `docs/extensions/wechat.md` · `docs/extensions/bots.md` Lark bot · `docs/extensions/feishu.md` · `docs/extensions/lark-agent.md` · `docs/extensions/lark-migration.md` · `docs/extensions/lark-attachments.md` · `docs/extensions/lark-encryption.md` |
| Design | `docs/design/PRINCIPLES.md` the seven product design principles, how to check each, cases · `docs/design/DESIGN_SYSTEM.md` tokens, components, interaction and content rules (read before any UI change) · `docs/design/DECISIONS.md` the history of the design rules: what was decided when, and what it replaced · `docs/design/proposals/owner-at-a-glance.md` proposal: secretary → leaders (directions) → tasks in one view, New chat as a direction, leaders' reports to a work-log extension, one Needs you · `docs/design/proposals/continuous-improvement-loop.md` proposal: Opus plans and verifies, cheaper execution models by task type, the relay's hybrid mode, cadence and rollback · `docs/design/proposals/main-chat-availability.md` proposal: a background supervisor for the main chat's tasks, the owner's message first, `ask_user` without holding the turn, reply-wait metrics, fewer asks · `docs/design/proposals/hub-token-boundary.md` proposal: a token on the hub machine is not the owner's approval; what the hub's account can reach, agent isolation vs a terminal-bound token vs person-confirmed changes · `docs/design/proposals/assistant-instructions.md` proposal: one home for each assistant instruction (shared core, role blocks, tool descriptions, skills), behaviour checks before cuts · `docs/design/proposals/inbox-sources-migration.md` proposal: built-in Inbox sources become an extension's read tools plus a default subscription; what subscription items lack, the per-source switch, rollback and order · `docs/design/proposals/assistant-search.md` proposal: find an assistant chat by what was said in it (a manager-side index of the user's messages and the replies, `GET /api/v1/assistant/search`, opening at the hit) and one rule for search in chat (no hidden tags, quoted contexts, injected lines as shown, folded work) · `docs/design/proposals/todos-redesign.md` proposal: the To-dos redesign (paper and pane), its domain model and the architecture convergence plan · `docs/design/prototypes/todos/README.md` clickable To-dos prototypes (V1–V3), each variant's notes in `docs/design/prototypes/todos/v1/notes.md` · `docs/design/prototypes/todos/v2/notes.md` · `docs/design/prototypes/todos/v3/notes.md` · `docs/design/proposals/team-project-spaces.md` proposal for versioned documents, membership and execution grants in opt-in team projects |
| Web | `docs/web/web.md` · `docs/web/review.md` reviewing a session's changes · `docs/web/preview.md` dev server previews · `docs/web/screenshots/task-workspace/README.md` workspace policy browser fixture and captures · `docs/web/screenshots/task-execution/README.md` scheduled execution form fixture and captures · `docs/design/task-cards.md` task cards: the merged design, a shared token table and the product calls open (#5569) |
| Plans (open decisions) | `docs/plans/plan-inbox-vertical-slice.md` the Inbox as the first vertical domain slice: what it owns, target boundaries on the server and every client, small PRs with conflict cost, rollback and evidence, metrics and the decided list (#11309) · `docs/plans/plan-derived-counts.md` the counts people see (Needs you, Inbox, To-dos) computed once in the managers and relayed by the hub: inventory, disagreements, target, small PRs with conflict cost, gates, rollback and evidence, metrics and the decided list (#11679) · `docs/plans/plan-extensions.md` · `docs/plans/plan-connectors.md` · `docs/plans/plan-orca-support.md` · `docs/plans/plan-tmux-support.md` · `docs/plans/plan-lark-cli.md` · `docs/plans/plan-discord.md` · `docs/plans/plan-x.md` · `docs/plans/plan-ego-browser.md` · `docs/plans/plan-antigravity.md` · `docs/plans/plan-agent-dispatch.md` · `docs/plans/plan-session-search.md` searching Agent sessions by content across computers (#757): each node searches its own transcripts (a bounded scan, mnemo where installed), the manager fans out, redacted snippets, open decisions |
| Inbox UI evidence | `docs/web/screenshots/lark-inbox/README.md` synthetic Lark Inbox review captures |
| Lessons and archive | `docs/lessons/` pitfalls by topic (index `docs/lessons/README.md`) · `docs/archive/` dated records, evidence only (index `docs/archive/README.md`) |

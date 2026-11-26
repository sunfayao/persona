# hub versions, update checks and automatic updates

The fork deploys the hub and nodes directly with `scripts/deploy/deploy.sh`. This reference also describes the update and rollback behavior available in the hub.

## Development updates

### Web development updates

For operators running the Web client with the built-in child manager, Settings › hub offers
**Update source › Web development**. This feature is scoped to the Web development UI and
its Hub/Node backend update chain. It does not add or validate native macOS, iOS, Android,
or other client update flows. A Linux/macOS **Hub host** and registered computers' service
binaries are backend installation targets, distinct from native client adaptation.

This uses the existing update endpoints, hourly scheduler,
idle checks, binary verification, blue-green switch/watchdog, confirmation and node deployment
transactions. Development automation is
off until explicitly enabled and is turned off whenever the source changes. An update already
accepted keeps its fixed candidate; source changes are refused until it ends. Repeated triggers
join that attempt. A rolled-back development commit is never automatically tried again.

Development updates always use `https://github.com/nwverse/persona.git`, branch `main`.
They work out of the box on a Linux or macOS Hub that starts the built-in child manager: the
Hub pauses its own writers for the snapshot, so no hook script is needed. **Settings › Hub ›
Update source › Web development** offers **Backup folder** and **Snapshots to keep**. The Web
settings are declarative only: a folder and a count, never a shell command, credential or
helper path.

The recommended folder is `<runtime>/update/development-backups` (where the Hub has always
kept these snapshots); it is shown as the placeholder and used when no folder is set. **Use
recommended** and an empty field are an explicit Web choice of it, so they win over a
`directory` in `hub.json`; leaving the field out of a request keeps whatever is set. A
custom folder is validated on the Hub with the backup command's rules (`internal/operations/backup`):
it must be an absolute path (a leading `~` is expanded), may not be inside data a snapshot
collects (the knowledge base, `managers/assistant`, extensions, skills or
`hub.machines_kept`), may not be the runtime directory or contain it (or an external
knowledge base) — a snapshot leaves its own store out of the archive, so such a store would
drop what the snapshot is for — it may not be a symbolic link, and it must be private
(`0700`; an existing folder readable by other users is refused with the `chmod` to run). A
folder chosen inside the runtime is never archived into the snapshot. The folder cannot be
changed while the current one holds a `coordination-pending.json` marker or an
`.incomplete-*` snapshot: those fail-closed gates stop any retry until someone inspects them,
and a new folder would otherwise pause and dump writers again past them. The retention count
defaults to three and may be 1–50; an explicit `0` is refused (omit the field to inherit).

For installations with writers outside the Hub's control, `update.development.backup.quiesce`
and `resume` in private `hub.json` remain an optional advanced override. Both must be set
together; a half-configured pair disables the channel rather than silently using the built-in
pause. The Web settings cannot supply a shell command, credential or arbitrary path:

```json
{
  "update": {
    "development": {
      "backup": {
        "quiesce": ["/absolute/path/to/pause-persona-writers"],
        "resume": ["/absolute/path/to/resume-persona-writers"],
        "directory": "/absolute/path/to/snapshots",
        "retain": 5,
        "extra_roots": [],
        "extra_databases": []
      }
    }
  }
}
```

These hook paths are placeholders, not installed programs. The operator must implement and
verify them for that installation. `quiesce` must return only after **all** writers of the
inventoried databases and files have paused at a consistent boundary, including background
ingestion, extensions, uploads and manager/node children. Keep the Hub updater itself running.
`resume` must safely release even a partially completed pause. Hooks have a 60-second deadline;
every failure attempts resume, and a failed resume blocks adoption and subsequent attempts.
Stopping a service that the Hub immediately respawns does not establish this contract. Do not
use no-op hooks on real data. Missing coordination disables installation and automatic opt-in.
`directory`/`retain` in `hub.json` are read when the Web UI does not set them; a Web choice
wins.

### Built-in writer pause

Without hooks the Hub pauses the writers itself for the whole snapshot: request writes are
refused with `503` until resume — the apps' API, signing in with a password or a browser, a
manager trading its install code, and the admin pages — writes already under way finish, and
the built-in child manager is stopped and kept stopped until resume. Background writers (app
registrations, extension reports, device-session rows, heartbeats and pruning, the
computer-trust sweep) leave their work pending and write it after resume; the pause is lifted
before those pending workers are woken, so the wake they use is not consumed by a refused
write. The Hub then checks the databases with the backup command's own `Quiesced` rules,
counting only its own connection-pool sessions as its own (`backup.QuiescedExcept`), and
refuses the snapshot when another session, or one it cannot see, is still connected. A
built-in manager whose tables still share the Hub's database (before
`moved_to_own_database`) needs only that database checked; one moved to its own is checked
separately (`backup.DiscoverDevelopment`, the same rules as the helper's inventory). Resume
runs on every failure path, including cancellation; a Hub process that dies mid-snapshot
leaves the incomplete directory in the store, which the next attempt refuses until it is
inspected. Snapshot JSON, SQL dumps and file history keep their existing privacy rules.

The host needs Git, Python 3.11+, Node matching `web/package.json`, Go matching `server/go.mod`,
and compatible `pg_dump`/`pg_restore` tools with access to every included database. Their libpq
shared library must be discoverable by Python. The helper parses URI and keyword connections
with libpq, then passes their explicit fields through libpq environment variables. It does not
put connection strings or passwords in process arguments or receipts. Invalid connections and
parameters without a safe environment binding fail before pausing writers. Existing private
`PGPASSFILE`/`PGSERVICE` settings remain usable; credentials in a URI are kept in the private
child environment, which is not protection from the installation user or privileged processes.
Git uses the
installation user's existing HTTPS credential mechanism, with interactive prompts disabled.
Updates execute build code from this project's repository.
Docker Hubs and Windows Hubs retain their original update paths.

The helper resolves the full commit of `refs/heads/main`; a missing `main` branch is an error.
Settings shows `main` and the installed/target commits. A detached private checkout fetches that
exact object even if the remote advances while building. There are no repository/branch overrides
or default-branch mappings.

For Web development only, **Update version** combines the latest `main` candidate and retained
program backup versions in one selector. **Current commit** and **Latest commit** remain two rows,
each with its update date in `yyyy-mm-dd` before the hash. The current/backup date comes from
successful installation; the latest date is the pinned Git commit's committer date, not check time.
Unknown dates stay unknown. Backup creation time is shown separately. Selecting a backup exposes **Apply backup**:
after confirmation it disables development auto-update and installs the saved programs and embedded
Web, keeping the **current database, configuration, keys and history data**. It does not check out
Git, rebuild, extract the history archive or restore a database dump. Preparation verifies the
snapshot, stages private program copies, then reuses the ordinary idle wait, fresh pre-install
backup, binary checks, service switch and confirmation/rollback. A snapshot rotated during the new
backup does not invalidate those staged copies. Missing/damaged selections fail before adoption.
`GET /api/v1/hub/update/backups` lists program-version metadata;
`POST /api/v1/hub/update/backups/{id}/apply` accepts an owned retained ID, never an arbitrary path.
Only snapshots with complete program version/build/commit/platform metadata are selectable.
Startup recovery errors stop both standalone and edge-slot startup before configuration/database
initialization. Failed program restoration or exec retains the development journal and staging
for operator repair; the attempted build cannot continue as an ordinary startup.
Edge restoration also stops on a failed program/Web restore, retaining both pending and
adoption journals and required staging. The callback distinguishes a failed restore from no
matching update; activation and confirmation-timeout paths never restart a fallback executable
after partial restoration. Repair the retained preimage/conflict before retrying. The backup
list/apply endpoints are Hub installation administration, declared with the other owned routes.
Listing validates the full manifest schema, typed hashes and exact artifact mappings, without
rehashing payloads. Verification and preparation additionally check the physical inventory,
compressed and native program hashes and history members. Staged programs get an independent
native-hash check after copying. Malformed metadata is rejected as `ValueError`; unknown or
damaged snapshots stay on disk for manual inspection and are not pruned as verified material.
Invalid schemas are omitted from listings; payload damage is checked before application.

Source settings accept only the exact values `release`, `beta` and `web-dev`; invalid supplied values
are rejected before changing any preference or selection. A manual request overtaken while
probing services is rejected without transferring its interruption consent to the accepted
operation. Explicit Update now for an already accepted waiting attempt still requires confirmation.
The install script's independent release-checksum refresh loads the saved source first;
a late release response cannot overwrite a development selection or active install progress.

Ordinary automatic/manual updates wait for the shared Hub idle check before backup and switching:
working turns, conversations awaiting blocking user input, and active redeploys can hold the update.
Non-waiting `ask_user` cards alone do not hold it; running turns and blocked terminals still do.
A manual attempt stops waiting after two hours; it does not force a restart at that deadline.
Choosing **Update now** explicitly bypasses this Hub wait and can interrupt local or connection-owned
conversations. Independent remote resident-node conversations can survive a Hub reconnection.
The idle check is not a lock against new turns: a turn starting during the subsequent backup/switch
can still overlap the restart. This path does not guarantee interruption-free session execution.
Link/connector node updates retain their own idle and commit checks; direct SSH deployment instead
replaces the program and restarts the node, so it can interrupt work. See [node-update.md](node-update.md).

The compile gate runs `npm ci` and `npm run build:development` for Web, generates runtime packages,
then runs tagged `go build` for `personad` on the Hub's platform and each registered computer's
last-reported OS/architecture, deduplicated, plus
`persona-hub`/`persona-manager` on the Hub's platform. Web assets
are embedded and precompressed. No full tests, vet, mobile builds, release publication or hosted
CI are required. The development command retains TypeScript, Vite, WebAssembly, localization
and asset-graph checks, but skips the release's first-screen/idle performance budgets. The
normal `npm run build` budgets are unchanged. Bootstrap and target source must contain this
development build command; an older target without it fails before adoption. All components
share the same commit, version and build identity; automatic
adoption also retains the existing ancestry guard. Fetch depth and the returned ancestor list
are capped at 1024; if the installed commit cannot be proved within that window, automatic
adoption stops and a manual update is required. An unknown/dirty installed commit also requires
a manual first update. Verified programs and Node packages are copied into independent updater
staging before the disposable checkout is removed; staging survives the idle wait and process
switch until confirmation or rollback. The binaries retain the `release` ABI/channel for existing self-checks;
the independent persisted `source: web-dev` identifies this update channel.

After compilation and the existing idle wait, before changing executables, the helper makes
a recovery point in the configured backup folder (the recommended default is
`<runtime>/update/development-backups`). It includes:

- Custom-format PostgreSQL dumps of the Hub database and every local manager database found
  in `managers/*/manager.json` (shared databases deduplicated), plus `extra_databases`.
- The runtime tree's configuration, encryption keys, knowledge/history, attachments and other
  ordinary data files, plus configured external knowledge directories and `extra_roots`.
  Only `update`, `bin`, `cache`, `logs`, sockets and FIFOs are excluded. Escaping/absolute
  symlinks fail closed; they need an explicit storage layout change before this path is usable.
- Separate compressed copies of the current `personad` and existing Hub/Manager programs,
  with hashes, original paths, installed version/build/commit and installation/backup times.
  All matching locally available Node packages are saved, including platforms with no currently
  registered computer; symlinks and packages marked as another version are excluded. Each snapshot
  retains its own matching programs rather than depending on the single latest `.prev`.

Writers remain paused across **all** database dumps and the file archive. A dump must have its
PostgreSQL header and pass `pg_restore --list`; archives and dumps have SHA-256 checksums.
Each private snapshot directory is synced, published and verified before cleanup. Credentials
remain inside private config/key backups (0700 directories, 0600 files), never in receipts or
the API. `database-0.dump` is the Hub; subsequent dumps follow sorted manager config paths,
then configured extra databases, with identical URLs deduplicated. The archived configuration
preserves that mapping. `effective-databases.json` privately retains the effective DSNs,
including `PERSONA_DATABASE_URL` overrides; no DSN is written into the manifest or API.
The built-in manager's shared database remains included until its own-database move commits.
After that move, `database_name` selects a database on the effective Hub server before a
saved `database_url`, matching the built-in manager's startup selection. Other managers use
their own configured URLs. A stale URL cannot replace the named manager database in a snapshot.
Protect these snapshots like the original data.

Only verified directories with this helper's exact name and ownership manifest are rotated,
oldest timestamp/name first, retaining the newest **three** by default (the **Snapshots to
keep** setting, 1–50, replaces that count). Unknown directories, changed manifests, symlinks,
additional user files, damaged archives and other backup systems are left alone. A
failed/incomplete snapshot is not counted and removes no valid recovery point. Cleanup failure
stops adoption; it can temporarily leave more valid snapshots than the configured count rather
than discard recovery evidence. The API retains the last snapshot/rotation receipt; the Web selector
lists available program versions. Private phase logs are `update/development/<operation>.log`.

The data archive intentionally captures the existing standalone `web/` override before it is
set aside in the same update transaction so the new embedded UI actually serves. Failed adoption
restores that override and all swapped programs. Explicit **Apply backup** installs saved programs
and their embedded Web only; it never extracts this data archive or its standalone override. After
Hub confirmation, compiled node packages are installed locally even with automatic fleet updates
off, so **Redeploy** can use the matching build. Fleet installation still requires the separate
`auto_update_nodes` choice. Applying a backup with fleet auto-update enabled requires saved packages
for all currently known platforms; otherwise it stops before adoption. With fleet updates off,
remote Nodes keep their current programs and available saved packages are installed locally.
A backup replaces only its staged platforms. Other regular local packages remain usable at
their normal paths; an unlabelled retained package receives its **pre-rollback** `.version`
identity, while an existing valid explicit identity is preserved. An invalid retained version
marker stops package adoption and keeps staging for inspection.
Online/offline Link nodes, SSH/tailcat, busy checks, old capabilities,
fresh ownership and guardian rollback retain their original behavior in [node-update.md](node-update.md).
There is no platform configuration or fixed platform matrix. Offline computers with a known
platform are included. A computer that has never reported its platform supplies no build target;
once it connects, the next development update includes its platform. With no known remote
platforms, only the Hub's platform is built. For a freshly compiled development update, old
unversioned packages are set aside before installation so omitted platforms are not mistaken
for the new version. A saved-version application instead retains and labels omitted packages
as described above. Any set-aside or version-label failure stops fleet deployment.
Independent managers have their own binaries, data and package sources. Update them separately.
The local build path also refuses to overwrite a `bin/nodes` symlink.

#### Development recovery

Build, backup and known adoption failures leave the running build unchanged or restore it
through the existing transaction. A migration/startup failure uses the existing binary rollback;
**it never automatically overwrites databases with a snapshot**. Database migrations may require
the following operator recovery. **Apply backup** is program-only and does not perform this data
recovery procedure; use the offline procedure only when data recovery is actually intended.

1. Disable development automation. Stop every data writer and the Hub through their original
   service controls (including its socket/edge), and preserve the failed attempt's markers/logs.
2. Verify the chosen snapshot offline:
   `python3 server/internal/update/webdev.py verify /absolute/path/to/snapshot-<id>`.
   Do not continue on a failed checksum or archive check.
3. Extract history offline with
   `python3 server/internal/update/webdev.py extract-history /absolute/path/to/snapshot-<id> /absolute/path/to/empty-recovery-directory`.
   The destination must be absent or empty and cannot be a symlink. This command verifies the
   snapshot and applies the same canonical member/link checks before extraction on Python 3.9+.
   A restricted writer creates private directories, exclusively creates regular files, then
   creates validated symlinks after all file writes. It preserves timestamps and safe file
   permissions, removes special and group/other write bits, and never applies archive ownership.
   It rejects absolute, drive/UNC, backslash, noncanonical
   and duplicate paths, hard links and non-directory parents. A relative symlink must point
   directly to a file or directory actually inside this archive; dangling links, link chains
   and links used as parent directories are rejected. Snapshot creation also refuses hard links.
   `manifest.json` maps `root-0`, `root-1`, etc. to their original roots.
   Inspect the archived configs privately to identify each database. Restore the dumps with
   `pg_restore` into fresh, empty databases, using a private `PGSERVICE`/`PGPASSFILE`; then select
   those databases in the restored configuration. Preserve the failed databases until recovery
   is confirmed. Put the matching config, encryption keys, history and attachments back together.
4. Decompress the snapshot's `program-*.gz` files to the recorded program paths, verifying
   their native hashes in `manifest.json`; the immediate previous transaction also retains
   `.prev` files. Restore `web_prev` when present; do not overwrite a subsequently changed Web
   directory. Start the original version, check history/data and node connectivity, then enable
   only the automation you want.

`coordination-pending.json` means a pause/resume outcome needs inspection: run the original
resume procedure and verify the writers before removing that exact marker. `.incomplete-*`
directories remain for inspection. Normal cancellation signals the helper and its active child
group with SIGINT, then escalates child termination within bounded waits. The resume hook gets
its own 60-second deadline; additional SIGINT/SIGTERM signals are deferred until it finishes.
A failed/timed-out resume or a force-killed helper retains its coordination evidence and blocks
adoption. An interrupted snapshot is never published or rotated, even after successful resume.

`update/development/adopting.json` reserves an attempt, then records every program preimage
before replacement (`recovery_ready: true`). The Web rename intent is synced there before moving
the override. Startup of the previous or attempted build restores this journal when `pending.json`
is absent; recovery can repeat after a partial restore. Failed recovery retains the journal,
pending marker when present, and independent staged programs/Node packages across startup cleanup.
Only a complete restore permits cleanup; the standalone watchdog also retains the development
journal until restarting the restored program succeeds. A failed restart leaves the journal
and staged packages available to the next startup. An incomplete reservation (including a crash while
preparing preimages), unreadable journal or older fence remains blocked for manual inspection;
no live replacement occurs before the new journal is ready. Inspect the installed binaries,
`.prev`, Web recovery directory and backup, complete rollback
or confirmation, and only then clear the matching fence. Regular confirmation/rollback clears
its own matching fence. Never delete these records merely to make Retry available.

Local evidence: `scripts/test/test_web_development_update.py` uses controlled Git, compiler/DB
tool fixtures and a real synthetic writer process; `TestDevelopment*` exercises the existing
HTTP settings, shared install/confirmation, failure rollback and local node staging without
touching a running Persona installation. Database-tool fixtures do not prove restoration on a
particular deployment's PostgreSQL version or validate its operator-supplied coordination hooks.

### Node upgrade policy

`hub.json`'s `auto_update_nodes` defaults to **false**. This applies to reconnect
upgrades, automatic fleet deployment, resuming queued transfers, downloading node
platforms and installing node packages after Hub confirmation. Explicit **Redeploy**
continues to work; an interrupted queued transfer requires another explicit
Redeploy while the policy is off. Already installed candidates still receive their
confirmation, so disabling upgrades cannot strand them.

A confirmed update to another version sets aside the node builds the Hub leaves in
`bin/nodes/`: each `personad-<os>-<arch>[.exe]` with no `.version` file beside it
becomes `<name>.prev`. The manager takes an unlabelled personad there for a build of
the Hub's own version, so left in place it would put the previous version back on
every computer that Redeploy reaches (#1825). Set aside, Redeploy fetches the Hub's
version from the release (`docs/operations/node-update.md`, "Node packages"). A build
with a `.version` file says which one it is and stays; the directory is never
created. With the policy on, this happens too, before the release's builds go in:
a platform the release does not carry would otherwise keep the old build. Releases
up to v0.9.1 carry no Windows `personad` in `files`, so a Windows build stayed
behind and every Redeploy to a Windows computer failed its self-check (#12958).

The manager receives this policy in `devices.config`. An old external manager that
does not understand it must be updated separately before relying on that gate;
the built-in manager changes together with the Hub. Existing package containment
must remain intact during a Hub-only rollout.

A Hub-only edge switch may proceed while sessions on proven resident nodes or remote herdr/herdr-core hosts run:
their processes belong to the terminal host and survive the transport reconnect.
Local and unknown/direct assistant sessions still wait for idle. Fleet upgrades
retain the conservative wait for every working session.

## 1. Skills move out of the binary

- **The user's skill space**: `~/.persona/skills/<name>/` on the machine that runs the assistant.
  - Each directory is one skill (with a `SKILL.md` inside); users can add and edit them. Updates never overwrite them;
    an explicitly confirmed Web ZIP import can add a new name, with a narrow Undo for that new installation.
  - The machine that runs the assistant is usually the hub machine's local node; when the assistant is configured on another machine, it is that one.
- **Syncing into the assistant directory**: every time node starts the assistant process (both Claude and Codex assistants; `/agent` and the AI twin also use the assistant directory):
  1. For each skill under `~/.persona/skills/`, create a symlink at `<assistant dir>/.claude/skills/<name>`.
     When the user edits a skill, it takes effect the next time the assistant starts; Claude Code only reads skill files, so symlinks are fine.
  2. Delete links that point into `~/.persona/skills/` but whose target is gone; real directories the user placed directly in the assistant’s skill directory are not touched.
  3. Built-in skills shipped in the binary (only ones we may redistribute; currently none) are still copied in by version stamp; a same-named skill in the user space wins.
  4. The gate command is only put on PATH when a skill that carries it is installed; the section about it in the assistant's prompt (`prompts/assistant/claude_md.md`) is also only written in when it is installed.
- **Migration**: two ways, each done only once.
  - **Automatic**: when node starts the assistant, each real directory under `<assistant dir>/.claude/skills/` installed by an old personad (with a `.persona-skills` version stamp next to it) that has no same-named entry in `~/.persona/skills/` yet
    is moved to `~/.persona/skills/<name>`, replaced in place with a symlink, and one log line is written; if a same-named user skill already exists, the user's wins.
  - **Manual** (on our hub, done by the main session): copy from `<assistant dir>/.claude/skills/<name>` to `~/.persona/skills/<name>`.
    (Since #1571 such skills are provided by the Lark extension, see the top of `docs/extensions/lark-agent.md`; no more manual copying.)
  - That way the assistant still has its original skills when the new version starts. The copy of the upstream skill was moved under the repo's `skills/` at the time (the upstream sync script stayed, now writing there), and does not go into any binary;
    it does not matter that the public build lacks it: users put it (or their own skills) in `~/.persona/skills/`.
- **Drop `-tags public`**: there is no difference between the two anymore; `files_public.go` / `files_bundled.go` merge into one embed holding only built-in skills.
  - The privacy scan before release stays, and also checks that every skill in the binary is registered as redistributable (`assistantskills.Redistributable`);
  - A test guards it: the built-in skills directory may only contain skills registered as redistributable.
- **App**: Settings › Assistant › Skills lists name, description and user / built-in / plugin source.
  `GET /api/v1/skills` remains read-only and compatible with older clients; the hub asks the current assistant's node.
  The Web additionally offers **Upload ZIP**, described below. Native clients are unchanged.

### Web skill ZIP installation

For people adding a trusted skill package, Settings › Assistant › Skills has one **Upload ZIP** action.
Choose a file, wait for checks, review the plain-text name, description, actual destination and computer, then
**Install skill** or **Cancel**. Selecting a file never installs it. No shell, dependency installer, HTML renderer
or model is used to check a package. The feature changes no model/profile setting.

The manager authorizes the current assistant and its execution computer using the existing full-access route,
then sends `skills.import` to that node. The browser cannot select another machine or supply a path.
The node uses `assistantskills.UserDir`: normally `~/.persona/skills/<name>/`, respecting `PERSONA_SKILLS_DIR`
and the existing `PERSONA_HOME` resolver. This store is shared by assistants running as that machine's user;
it is not silently changed to a private manager store. A changed computer, root identity, expired reference,
offline machine or old node fails explicitly. Open the page again after changing environments.

Package contract:

- One ZIP with exactly one uppercase `SKILL.md`, either at the root or inside one enclosing directory; all
  resources must be in that same package. Nested skills, several manifests and outside-wrapper files are refused.
- The entire manifest is UTF-8, at most 64 KiB, with YAML frontmatter between `---` lines. `name` and `description`
  are non-empty YAML strings. The name (not the ZIP filename) is at most 64 ASCII lowercase letters/digits with
  single separating hyphens, not a reserved device name. Description is at most 4096 UTF-8 bytes.
  Duplicate YAML keys, aliases/anchors and custom tags are refused.
- At most 4 MiB compressed, 16 MiB expanded, 4 MiB per file, 512 archive entries and 12 path levels; maximum
  expansion ratio 200 per entry. Both declared and actually read sizes/CRC are checked. Only Store/Deflate,
  ordinary files/directories are accepted. No links, special files, traversal, absolute/drive/UNC/backslash paths,
  reserved names, duplicate entries, case/Unicode-normalization collisions or internal provisioning stamps.
- Preview extracts into a private temporary sibling and removes it before replying; only bounded, expiring metadata
  receipts remain (32 operations per node, ten minutes, with lookup aliases for completed installs). Install rechecks the exact same ZIP, current environment and conflicts,
  then publishes by an exclusive atomic rename. No existing directory, built-in, plugin or local runtime skill is
  overwritten or shadowed. Unsupported exclusive-rename platforms fail closed. Failure/cancellation removes staging.

After installation the existing list refreshes. A ten-minute **Undo installation** receipt exists only on this
connected page/node lifetime; reloading, switching assistant or node restart can invalidate it. Undo only detaches the
same unchanged new directory, with identity/content/mode checks; modified or replaced packages are refused. It moves
files to a hidden `.undone-…` sibling and reports that recovery path, rather than deleting them. This also protects a
local writer holding an open file during the move. There is no arbitrary-path delete or edit API. Recovery is retained
for the user; no background job deletes it or reinstalls an undone package.

A canceled preview has no durable staged files. Cancellation before the atomic publish cleans staging; a lost response
after publish may mean the whole installation completed, so refresh the list before retrying. Writes are never
automatically retried. Repeated/concurrent installation cannot replace the winning name.

Assistant-only MCP `skill_import` exposes `preview`, `install`, `status`, `cancel` and `undo` through that same manager
service and importer. It accepts base64 ZIP bytes, not download URLs or local paths. Preview returns an environment
and receipt; install sends the identical bytes with both. Before install or undo, the manager reads the exact node
receipt and shows a localized permission card naming the validated skill, computer and shared-account impact.
Only the user's own approval counts; no archive bytes or skill instructions appear in the card. The settings lock
is released while waiting and reacquired to recheck the target before the node call. New tools remain subject to
existing assistant/meeting restrictions; ordinary coding Agents do not receive them.

`status` accepts the original preview or install reference and environment. It records `previewed`, `installed` or
`undone` and recovers the installation's undo receipt or retained recovery location after a lost response. It does
not replay the effect or claim to be a live inventory. Receipt metadata survives only for its ten-minute node
lifetime; expiration or node restart requires inspecting Settings › Skills instead of assuming failure. Older
nodes without receipt status require an update before MCP installation. HTTP clients keep their existing flow and
may also query status; the additive result fields are backward compatible.

Discovery uses existing provisioning: `assistantLarkEnv` links the user store into `.claude/skills`, and
`assistantRuntimeSkills` links it into `.agents/skills`. Claude reads the existing assistant directory; Codex's
existing `skills/extraRoots/set` points at `.claude/skills`. No new discovery engine or `@skill` behavior is added.
The settings list is the Persona-provisioned inventory, not every skill a runtime might discover. Refresh lists
immediately; start a new chat to safely pick up installation/Undo, as already-running sessions may retain old context.
Tests exercise actual import/list/provisioning entry points and resource reads, not a paid Claude/Codex turn.

`TestImport*`, `TestSkillImport*` and `Skills.test.tsx` cover archive safety, atomic races,
environment/credential changes, strict server-derived targeting and the confirmation/Undo states. The opt-in
`TestSkillImportBrowserHarness` uses the real manager handler and archive store with synthetic device transport
and a temporary directory; unrelated routes use `scripts/mock/mock_hub.py`. It is UI integration evidence, not a
production or live-model test. Its header documents loopback ports and shutdown; run with `SKILL_IMPORT_QA=1`.

The Skills group keeps only the list in its card; where the skills live, who shares them and when changes apply are
its one footer line. The upload dialog reuses existing web patterns: LookUpload's drop zone (choose or drop a ZIP, the
check's progress in its place), ProviderImport's preview card and Cancel/primary footer, and `ErrorNotice` for a refused
package.

The 2026-11-15 capture run uses `web/e2e/skill-zip-visual.py` through Browser Use against the opt-in Go fixture above
and Vite. Its 22 captures were opened and visually inspected by Codex: text and status marks remain readable in both
appearances, the long install and recovery paths wrap, localized buttons stay on one line, and the dark preview has a
visible keyboard focus ring. The local browser check records its results outside the checkout.

All Skills actions use `touch-hit`: the drawn buttons remain 32 px tall while their targets expand to at least
44 x 44 px on coarse-pointer devices. The browser run measures the pseudo-element's target bounds and hit-tests
points across that area, including the separate post-install Undo row. Native touch events below the painted button
activate Upload ZIP, Choose ZIP, Cancel, Install, Close and both Undo actions. The file input's click is intercepted
to keep the operating-system picker out of the automated run; CDP supplies the real ZIP afterwards.

To reproduce, start an exclusively owned mock on `<mock port>`, run the fixture from `server/` with
`SKILL_IMPORT_QA=1 SKILL_IMPORT_QA_ADDR=127.0.0.1:<manager port> SKILL_IMPORT_QA_MOCK=http://127.0.0.1:<mock port>
go test ./internal/assistant -run '^TestSkillImportBrowserHarness$' -count=1 -v -timeout=30m`, and start Vite with
`PERSONA_HUB_URL=http://127.0.0.1:<manager port>`. Then, from the repository root:

```sh
PERSONA_SKILL_ZIP_ORIGIN=http://127.0.0.1:<web port> \
PERSONA_SKILL_ZIP_SHOTS=<evidence directory> \
PERSONA_SKILL_ZIP_WORK=<scratch directory> \
browser-use < web/e2e/skill-zip-visual.py
```

After inspecting the PNGs, record the observations in `verification.json`; the capture script initially marks visual
inspection pending. POST `/_qa/stop` to the manager fixture, then stop the owned Vite and mock processes. The browser
check delays one real preview response to exercise the busy state; native CDP file drag events must still cancel the
browser defaults without another preview request. The real importer supplies all validation, install and Undo replies.
Native link and plain-text drags also cancel `dragover` and `drop` without navigating, highlighting the zone or
starting an upload; only file drags can highlight it.
Browser observations: Cancel initially owns preview focus; Tab reaches Install with a visible ring; Escape closes
and restores Upload ZIP focus. No horizontal overflow at 390 px, including Large. An empty description shows an
actionable validation error. Installation creates `example-visual` in the real fixture store/list; Undo removes its
list entry and retains its resources in the reported recovery sibling. These are local fixture results, not live
Claude/Codex discovery.

## Direct deploys

Build from a checkout that contains `origin/deploy` and run `scripts/deploy/deploy.sh`. The script checks the remote `deploy` branch before building and exits 2 if the commit is missing or the branch cannot be read. Use `PERSONA_DEPLOY_ANY_COMMIT=1` for a deliberate rollback.

- WeChat and Telegram are installed and upgraded from the extension marketplace; `deploy.sh` does not build their programs.
- **Update a hub with `deploy.sh <hub machine id>`, not by copying a binary and running `personad hub switch` by hand.** `deploy.sh` first puts this build's node packages for every platform in the hub's `bin/nodes/`, then switches the hub blue-green (no restart while the socket units are unchanged, `docs/operations/hub-restart.md`).
  A hand switch leaves `bin/nodes/` on the previous build. The manager takes an unlabelled `personad-<os>-<arch>` there for its own version only when the hub's version string is in the file (the node's self-check would refuse it otherwise, #12958), so the old builds are no longer sent as the new version; the computers fall back to the release of the hub's version, or wait until the right builds are in place (a changed file is looked at again at once). Before that check, the new hub pushed the old builds to every computer the moment they reconnected.
  To check: every `bin/nodes/persona-node-*.version` on the hub machine equals the hub's `/api/v1/health` version.
## 12. API (draft)

| Method | Path | Description |
| --- | --- | --- |
| GET | `/api/v1/hub/update` | `{version, build, channel, can_update, latest?{version, build, released, notes, bad}, state, progress, busy[]{machine, session, title}, error, auto, auto_update_nodes, auto_notice, last_check, update_url?, update_url_from?, update_proxy?, source_behind?}` (`source_behind` only when true) |
| POST | `/api/v1/hub/update/check` | Check immediately |
| POST | `/api/v1/hub/update` | Start update (409 with a reason for non-release builds); `{"now": true}` is **Update Now** after the second confirmation, skipping the idle wait |
| PUT | `/api/v1/hub/update/settings` | `{auto}`; `{auto_notice_seen: true}`; `{auto_update_nodes: true\|false}` |
| GET | `/api/v1/hub/update/history` | Update history, newest first |
| GET | `/api/v1/skills` | Skills on the machine running the assistant: `[{name, description, source: user\|builtin}]` |
| WS event | `hub_update` | Push to open Apps when state or progress changes |

`state`: `idle` / `checking` / `available` / `downloading` / `waiting_idle` / `restarting` / `confirming` / `updated` / `rolled_back` / `failed`.

### Runtime node update policy

`PUT /api/v1/hub/update/settings` accepts `{"auto_update_nodes": true}` or
`false`. The choice is persisted in Postgres under `hub_update`, independently
of `auto` (Hub self-updates). An omitted or null field preserves the choice;
until a choice has been saved, `hub.json`'s `auto_update_nodes` is the fallback
(default false). GET and the successful PUT response always include the
effective boolean, including false. A failed read returns 503; a failed save
does not change the running choice or notify managers.

After a successful choice, existing `devices.config` broadcasts and reconnect
handshakes carry the same effective policy to managers without changing machine
ownership. An explicit true PUT also requests `devices.update_nodes` after the
configuration is sent, even when the value is unchanged. This requests a package
check; it is not an installation receipt. Matching Hub versions, usable packages,
busy checks, ownership, installation health and rollback still govern deployment.
An offline manager receives the policy on its next handshake; node reconnects
retain the existing automatic update path.

If `bin/nodes` is a symlink, the Hub updater leaves the target and its version markers alone.

This policy updates node executables, not an independently installed manager.
The latter still needs its own fixed candidate, original manager/unit/config
identity, complete activity evidence, transaction lock, guardian, health
confirmation and rollback. A node capability or a staged manager binary alone
does not satisfy that contract.

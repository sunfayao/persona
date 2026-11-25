# Releases

How the hub, node and website are published: the release PR flow, the unattended default-branch worker, `scripts/release/publish.sh`, version-tag publication and Darwin command-line signing.

## Unattended default-branch releases

`scripts/operations/autorelease.py` is a standard-library worker for a user-level systemd timer, not an Agent-session task.
Install the worker in `~/.local/lib/persona-autorelease/`, its private JSON configuration in
`~/.local/state/persona-autorelease/config.json`, and the supplied `persona-autorelease.service` and
`persona-autorelease.timer` from `scripts/operations/` in `~/.config/systemd/user/`.
Enable linger for the user if the service must survive logout. The timer checks every ten minutes and coalesces intervening commits.

CI admission belongs to the PR's local verification: run every affected platform check and `scripts/privacy_check.py` before
merging. PRs start no hosted checks. A scheduler explicitly dispatches full CI on `main`. Failures are fixed separately rather than being treated as local PR verification.
The unattended worker consumes the newest already-merged `origin/main`, then applies path selection and local build/release verification.
It does not wait for the scheduled main CI result before releasing; the two checks are separate operational gates.

Each target has its own last-success revision, path patterns, command argument vector, and timeout. Worker events are appended to
`worker.log` in the private state directory; `log_output: true` additionally retains command output in mode-0600 per-step files
under its `logs/` subdirectory. Do not enable shell tracing or print credentials from action commands. Recommended triggers:

| Target | Paths |
| --- | --- |
| Hub | `server/**`, `integrations/**`, `sdk/go/**`, `schema/**`, `spec/**`, `scripts/deploy/**`, `scripts/operations/**`, `scripts/persona_config.py`, `scripts/persona_identity.py` |
| Web | `web/**`, `schema/extension/**`, `spec/fixtures/injected/**`, `scripts/deploy/**`, `scripts/operations/**`, `scripts/persona_config.py`, `scripts/persona_identity.py` |

A missing baseline runs that target once. Later checks compare the last successful commit directly with the newest branch HEAD,
including deleted paths. Failure does not advance that baseline; two failures at the same HEAD suspend that target until another
HEAD appears. Lock contention exits 75 and is a deferral, not a failure. The deploy script's explicit checkout-behind-origin
message with exit 2 also defers the target without notification or a failure-budget charge; the next round fetches the newest HEAD.
Other exit-2 errors still count as failures, and the freshness guard is never bypassed.

Failures invoke the configured notification command once per target and consecutive failure cause, even across new commits.
The worker hashes the step/result and first diagnostic line (or first nonempty output line), normalizing revision identifiers and
timestamps; raw summaries never enter notification payloads or state. Each target's successful action clears its remembered failure, so the
same cause notifies again if it recurs after recovery. Successful actions only log, including recovery; there is no extra recovery
push. Preflight failures have a separate worker identity, cleared when preflight completes. Lock contention and stale checkouts
neither clear incidents nor create them. Failed notification deliveries remain in the durable at-least-once outbox, retried without
queuing another copy of a continuing failure. There is no periodic reminder for an unchanged cause.

For a worker upgrade, pause future timer runs and let any active service finish, copy the verified main worker to its installed
path with mode 0600, and resume the same timer. Keep configuration, targets, retry state, checkpoints, adapters and unit files intact;
record the source revision and installed file hash in the private STATE document.

Use a dedicated detached clean worktree on the hub and another on the build computer. The worker refuses to discard dirty files.
`scripts/operations/hub_action.py deploy` retains the deployment lock through a live API and serving-binary verification;
Do not disable the deploy script's freshness check.

Manual `deploy.sh` now takes the hub's existing `releases/hub/.publish.lock`, shared with release promotion; callers on another
computer retain it through an SSH connection. Older checkouts that predate this lock must be updated before manual use.

Failure notifications use hub-local `POST /api/v1/notify`, reading the runtime token inside the process. No credentials enter
arguments or logs. `sent:true` is hub acceptance, not a phone-display receipt; verify a controlled failure on the phone during installation.

```sh
systemctl --user daemon-reload
systemctl --user enable --now persona-autorelease.timer
systemctl --user list-timers persona-autorelease.timer
systemctl --user start persona-autorelease.service             # one immediate run
systemctl --user disable --now persona-autorelease.timer       # pause future runs
systemctl --user stop persona-autorelease.service              # also stop an active run
systemctl --user enable --now persona-autorelease.timer        # resume
journalctl --user -u persona-autorelease.service -n 100 --no-pager
python3 ~/.local/lib/persona-autorelease/autorelease.py --config ~/.local/state/persona-autorelease/config.json --inject-failure
```

Keep a private STATE document alongside the configuration, recording the actual paths, notification receipt, last successful revisions,
and release evidence. Runtime/build configuration and signing keys remain outside the repository.

## Publishing personad (`scripts/release/publish.sh`, 2026-10-31)

Let others install the server without getting the source: prebuilt `personad` and the installer are hosted on the `site/` Worker (configured `publish.site_url`).

```sh
curl -fsSL <publish.site_url>/install.sh | sh   # install to ~/.persona/bin/personad; verify against SHA256SUMS, refuse if wrong
```

- On the computer that runs the hub (`--hub`, a `hub.json`, or the two there already) it installs `persona-hub` and `persona-manager` with `personad`, the same release, each checked before any is put in place. A release without one of them leaves the one there as it was, unless a systemd unit or launchd agent runs it: then it is refused before anything changes ([../server/server.md](../server/server.md) ("Programs"), [hub-update.md](hub-update.md) ("A release without `persona-hub` or `persona-manager`")).
- After installing `personad`, if herdr can't be found on this computer (PATH and directories node searches, such as `~/.local/bin`), install it with herdr's official installer (`https://herdr.dev/install.sh`)
  too, so sessions appear in Agents after the first install. Failure only prints one warning and doesn't affect `personad`; computers using Orca / Herdr App can skip it with `PERSONA_SKIP_HERDR=1`.
  `deploy.sh` does the same when installing `personad` on each computer (`scripts/deploy/ensure-herdr.sh`; keep the two sets of steps aligned).

- No publish happens without `publish.site_url` in config. Run: `./scripts/release/publish.sh`; `NO_DEPLOY=1` only builds and checks (outputs in `build-publish/`). When the tag coordinator supplies `PERSONA_RELEASE_VERSION`, `publish.sh` instead uses that exact tag version, and the coordinator rejects a different manifest version before uploading assets.
- Website-only changes (`site/`): `SITE_ONLY=1 ./scripts/release/publish.sh` skips a new build, fetches the small files of the currently published version (`VERSION`, `SHA256SUMS`, `latest.json`, `SLOT`, `install.sh`) and deploys them with the committed `site/`, leaving archives in R2 unchanged. Afterwards it checks the live small files are unchanged and every archive still exists (`HEAD` size). It refuses if there's no live `dl/SLOT` yet (a release from before R2); do one full publish first. **Don't run `wrangler deploy` directly in `site/`**: it makes `/dl/` and `install.sh` return 404 (`docs/lessons/server.md`).
- Web dependencies always install from the official npm registry (`npm ci --registry=https://registry.npmjs.org/`, overridable with `PERSONA_NPM_REGISTRY`), ignoring local npm config. `package-lock.json` already points to the official registry; a locally configured mirror may lack packages (on 2026-11-06 it lacked `elkjs`, stopping release before web bundling).
- Build four platforms (linux / darwin × amd64 / arm64), with `-tags "$(cat server/build-tags.txt)" -trimpath -s -w`, embedding `channel=release`, build number (UTC `YYYYMMDDHHMM`) and `updateURL`
  (a non-empty environment `PERSONA_UPDATE_URL`, then the config's `publish.update_url` as written, e.g. a GitHub repository's `releases/latest/download`, else `<publish.site_url>/dl`; the script notes when the binaries look somewhere other than the site it publishes to; a hub's `hub.json` `update.url` replaces it at run time, `docs/operations/hub-update.md` ("Update source")). `install.sh` is rendered from that same update base, so its downloads and the binaries' update checks cannot silently select different feeds. For GitHub's moving Latest base, the installer reads `latest.json` once and pins `SHA256SUMS` and every archive to that version's immutable tag:
  - **Web bundled into the binary** (`server/internal/hub/webdist`, `go:embed`): the hub uses it when the hub machine has no `~/.persona/web`; otherwise it still uses the directory.
    The plain copy is privacy-scanned first, then its hashed assets and `third-party-licenses.txt` are kept gzipped (`go run ./internal/webasset/precompress`); the script stops if none are, or if a compressible asset of 1 KB or more is left plain. The first screen's brotli copies (`name.br`) come from `npm run build` and stay as they are (`docs/web/web.md` "Precompressed assets").
  - **No skills included**: the skill connecting to the Lark gateway was copied from a private upstream repository (GPL-3.0) and cannot be redistributed. Skills live in `~/.persona/skills/` on each machine,
    so there is only one binary, used by our own hub too (`docs/operations/hub-update.md`). The script ensures the binary only contains skills registered as redistributable (`scripts/release/check_bundle_skills.py`).
    The tags are the one line of `server/build-tags.txt`. No persona file builds differently with any of its tags (`TestOneBuild`). Its `public` marker only tells the privacy scan the binary is a release build, and the scan accepts exactly that line.
    Its `ts_omit_` tags leave out the tailscale.com features persona never uses (SSH server, web client, debug pages and metrics, and others), which makes `personad`, `persona-manager` and `persona-node` 4.6–4.8 MB smaller on linux/amd64 and leaves less to initialize at each start.
    `server/internal/buildtags` computes the line from tailscale.com's feature registry; after a tailscale.com bump, `TestTagsFileInSync` asks for `cd server && go run ./internal/buildtags/printtags > build-tags.txt`.
    The image, `deploy.sh`, `mac-link.sh` and the test scripts build with the same line (`scripts/test/test_build_tags_builds.py` checks every `go build` of the image, `deploy.sh`, `mac-link.sh` and `publish.sh`), and CI builds the programs and runs the tunnel tests with it (`scripts/ci/server.sh`).
  - **Version manifest** `dl/latest.json` (each platform's archive and unpacked sha256, build number, commit), unsigned (the user's decision; trade-offs in `docs/operations/hub-update.md` ("Security"));
    before publishing, check that the build number is greater than the live one, the live version's commit is an ancestor of this one, and server/ and web/ have no uncommitted changes.
    `release manifest` refuses binaries of another build than the one it writes, and `personad release verify` reads the written `latest.json` back against every download before the first upload. It runs this machine's binaries (`version -v` must say the version and build) and finds the build number in the others. A manifest written over an earlier run's binaries (v0.2.1, #4522) stops there (`docs/operations/hub-update.md` ("Version manifest")). The manifest includes all commits in this release (`ancestors`):
    hubs read it for auto-update, but only install automatically if it includes the commit they're running (`docs/operations/hub-update.md` ("Preventing rollback")).
  - **Programs published together**: `persona-hub` and `persona-manager` (`docs/server/server.md`), built for all four platforms with the same
    `-X` flags, privacy scan and size check as personad; its `version -v` must match personad's. The manifest's `programs` lists them, and hub auto-update replaces them together.
  - Before upload, run `check_bundle_privacy.py --whole-short`: binaries must contain none of the configured values (except Go module paths; values of four characters or fewer match whole words,
    since Unicode tables and compressed JS contain short letter combinations everywhere). A few protocol names, official contact links and standard network constants in public dependencies are classified by exact context, without allowing whole domains or private network ranges;
    it requires the binary's `public` marker, unreplaced dependencies and checksums matching `server/go.sum`, and `go mod verify` before building. Registered personal values always take precedence and are blocked; see `docs/lessons/testing.md`.
- Build the iPhone helper `persona-iphone-bridge-<os>-<arch>` for the four platforms when this computer can (`native/iphone-bridge/release.sh`; macOS needs `rustup target add aarch64-apple-darwin x86_64-apple-darwin`, Linux needs `zig` and `cargo install cargo-zigbuild` plus `rustup target add x86_64-unknown-linux-gnu aarch64-unknown-linux-gnu`). Cargo keeps its build directory at `PERSONA_BRIDGE_TARGET` (default `~/.cache/persona/iphone-bridge-target`). A platform it can't build, or whose build fails, is left out with a `note:` line; the release goes on without it. `latest.json` lists only the builds that are there under `helpers`, and they are kept with the node builds under `dl/<version>/` ([iphone-bridge.md](../extensions/iphone-bridge.md), "Releases"). A version-tag upload should carry `persona-iphone-bridge-<os>-<arch>.gz` with the other downloads; when it doesn't, `tag_release.py` warns and uploads `latest.json` without those helpers instead of failing.
- Build the connector `persona-link-<os>-<arch>` for the same four platforms too (2026-11-02, node service, `install.sh --node`, see `docs/operations/link.md` ("Installing the node service")), plus `persona-link-windows-amd64.exe` and `persona-link-windows-arm64.exe`, with the same privacy scan and gzip compression, included in `SHA256SUMS`. `latest.json` lists all six connectors under `links`; hub updates never swap them. `persona-link version -v` reports the same version, channel and build as personad so the manifest verifier can identify every connector. Windows download and scheduled-task installation commands are in [windows.md](windows.md) ("Install and start at logon"). The POSIX `install.sh` remains Linux/macOS-only.
- Build the device's own program `persona-node-<os>-<arch>` (`server/cmd/persona-node`) for the same four platforms, scanned, compressed and included in `SHA256SUMS` the same way (it must report the same `version -v` as personad). `install.sh --node` installs it at `~/.persona/bin/persona-node` and the node service runs it ([link.md](link.md) "The device's program"). `latest.json` lists it under `nodes` (old hubs ignore that): hub self-update still brings `personad-*` alone, and the manager fetches persona-node from `dl/<version>/` for a device that runs it (`docs/operations/node-update.md` ("Node packages")).
- Downloads are `personad-<os>-<arch>.gz`: scan the raw binary first, then compress with `gzip -n -9`; `SHA256SUMS` verifies the archive. The installer verifies it before unpacking and installing; failed verification / decompression never overwrites the old version. Public artifacts and their index use only Persona names, including `persona-link-<os>-<arch>.gz`; old-name aliases remain inside installed runtime compatibility, not new downloads. The installer still reads historical manifests with older names.
  **Archives go in R2 bucket `persona-dl`, not the site** (#2541): Workers static assets have a 25 MiB per-file limit ([official limit](https://developers.cloudflare.com/workers/platform/limits/#static-assets));
  after #1002 embedded tiered avatar images, compressed personad exceeded it. The Worker binds the bucket as `DL` (`site/wrangler.jsonc`); `worker.mjs` serves `/dl/*.gz` from it (`GET` / `HEAD` / `Range` / `If-None-Match`),
  while other `/dl/` files remain static. The bucket has slots `a` and `b`: publishing uploads archives to the inactive slot (`<slot>/<filename>`), then deploys the site, switching `dl/SLOT` and `latest.json` together.
  The other slot keeps the previous version until the next publish, so downloads in progress at deployment can finish.
  Every release is also uploaded to `release/<version>/` (its node builds, `personad-<os>-<arch>.gz` and `persona-node-<os>-<arch>.gz`, and `latest.json` as `manifest.json`), and to `release/X.Y.Z/` when the commit carries the tag `vX.Y.Z` at publish time (tag before publishing; looked up with `git tag --points-at`, as the commit has its `testflight/<build>` tag too; never for a `-dirty` build); these are kept (about 160 MB a release, twice for a tagged one: remove old ones from the bucket by hand once no hub of theirs is left), and `worker.mjs` serves them as `/dl/<version>/<file>`. A hub's manager fetches the node build of its hub's version there (`docs/operations/node-update.md` ("Node packages")); publishing checks the served copy too. The site only has small files now; `check_asset_sizes.py` still checks the whole site directory. Local `build-publish/bin` keeps uncompressed binaries for debugging but doesn't upload them.
  Regression: `python3 -B -m unittest discover -s scripts/deploy -p 'test_compressed_publish.py'`.
- Only the latest version is hosted (R2 also retains the previous one, as above). Bucket uploads and deployment use configured `publish.wrangler` (otherwise `npx wrangler`). Cloudflare OAuth expires after one hour;
  on an auth error, run `cf auth login` and retry. After deployment it reads back `dl/VERSION`, `dl/SHA256SUMS`, `dl/latest.json`, `dl/SLOT` to confirm the just-built version is live, and checks every archive's size with `HEAD`.
  The push relay is in the same Worker (`/relay/*`) and is redeployed on each publish too: when `/relay/v1/health` answered `ok` and `apns` before the deploy,
  the publish fails unless it still does within about two minutes after it (#1263; a site without a working relay only gets a note).
- Push relay deployed (2026-11-02): resident `personad relay` + Cloudflare Tunnel + website Worker forwarding; public health, origin protection, registration and authentication verified.
  See `docs/operations/push-relay.md`.
- Not yet available (others can install, but it's still awkward; see `docs/operations/config.md`):
  Developer ID notarization of the macOS binary (browser downloads get blocked by Gatekeeper; `curl` doesn't).

## Release PRs and GitHub Actions publication

**Every new formal or beta release is an explicit release PR.** The PR title is `release: vX.Y.Z` for a formal release or `release: vX.Y.Z-beta.N` for a beta. It targets `main`, originates in this repository, and is authored by administrator `chyroc`; an ordinary feature PR, fork PR or branch is not a release request. Do not create a local version tag to start a new release.

1. `release-schedule.yml` runs stable selection at 12:00 in the operator's configured UTC+08:00 zone (04:00 UTC) and beta selection every two hours (never every two minutes). It skips an unchanged source and opens a release PR with an unused immutable version. After any protected-environment approval, it fetches and detaches the then-current `origin/main` before selecting the source, so the pre-approval checkout cannot become a stale release. A feature since the previous stable tag increments minor; a fix-only history increments patch. The subject classifier recognizes `feat:` / `feature:`, an imperative `Add …`, and explicit additions, support or implementation under `Android:`, `iOS:` / `iPhone:`, `Watch:` / `watchOS:`, `Web:` and `Desktop:`. Fixes, tests, documentation and refactoring stay on the patch path; subjects outside these conventions require review rather than an assumption that every change is a feature. A replacement for a failed, unmerged candidate keeps that candidate's branch and source unchanged and uses a fresh `-rN` branch; it never rewrites a failed candidate or creates its tag before the replacement passes. It uses `RELEASE_PR_TOKEN` in the protected `persona-release` environment so the PR has the administrator's identity. Do not replace it with the workflow's ordinary `GITHUB_TOKEN` or run a second local release scheduler.
2. `release-pr.yml` first checks out the PR's proposed merge result, not an arbitrary branch or an earlier PR head. CLI/Web and Windows produce one exact-source verification plan. If a required build or check fails, repair the release PR and run it again; do not merge a failing release PR or substitute an older artifact. The aggregate runs with `always()` for every non-empty release plan and checks out the fixed source before its platform gate, so its failure-conclusion script is available even when a dependency failed. A failed or skipped platform makes both the aggregate and the exact merge-result check fail, rather than leaving the check pending forever.
3. Only after every required platform check passes does the protected merge job squash-merge that release PR with the administrator's `RELEASE_PR_TOKEN`. The ordinary PR watcher never merges a `release:` PR, and the main ruleset requires `All available release-PR checks passed`; an unrelated or direct merge cannot bypass the platform gate. The merge gate also requires the exact successful check and unexpired verification artifact; the merge job is restricted to an administrator-authored, same-repository release PR and verifies its unchanged head again. A different merger is a hard publication failure, not an identity to whitelist. Squash creates a new main SHA: the `push` run of `release-pr.yml` then resolves the merged PR and **rebuilds and reverifies that actual main commit on every platform**. A successful PR head is never treated as proof for the squash result.
4. Only after the post-merge run succeeds does `release-publish.yml` start through `workflow_run`. It rereads the merged PR, requires its verified merge commit to remain an ancestor of the current `main`, and verifies the new plan and every artifact digest. Ordinary PRs may merge during that build: they do not replace the release source, and the tag remains bound to the verified merge commit, never to a later `main` tip. It then creates the immutable annotated tag once at that exact commit. An existing tag for another source is a hard failure, never moved. The existing tag-triggered `release.yml` builds the container and creates the stable/prerelease GitHub Release. The publication job uploads the verified artifacts, compares remote SHA-256, independently downloads and compares every artifact, and only then advances the beta pointer. Stable `releases/latest` and stable metadata never come from a beta PR.
5. Release attachments are separate results. A completed release PR does not deploy the production Hub/site. Repeating a failed publication is an administrator-only `release-publish` dispatch for the same merged release PR and its verified merge commit, which must remain an ancestor of current main; it never selects another source.

### Protected environment

`persona-release` holds the release-PR token and the signing/build values used by these jobs. `RELEASE_PR_TOKEN` creates release PRs as `chyroc`; after all checks pass, the same protected identity performs the release merge, creates the immutable tag and marks its Release as owned by that PR. The last two operations are required so the tag-triggered `release.yml` can verify its administrator actor and the signing jobs can recognize the verified release-PR artifacts. Prefer a GitHub App installation token for that account, restricted to this repository with only **Contents: read and write** and **Pull requests: read and write**; a short-lived fine-grained token with exactly the same repository and permissions is the alternative. It needs no administration, packages, signing, Actions or organization access. The scheduler exchanges it only through the protected environment and never writes it to the repository or logs. Do not reuse the current interactive `gh` credential as a permanent release token. Windows signing uses the existing `PERSONA_DESKTOP_APP_ID`, `WIN_CSC_LINK` and `WIN_CSC_KEY_PASSWORD` prerequisites. Never commit these values, put them in PR text, or include the temporary configuration in an artifact.

An unconfigured required signing platform blocks the release PR; it is not a successful skip. Windows amd64 native verification runs on `windows-latest`; its three verification reports are included once, and a platform seeing the same download again with identical bytes is idempotent (different bytes still fail). The combined receipt checks only the runner's actual architecture: amd64 evidence never marks ARM64 verified, and ARM64 remains blocked until a matching native ARM64 runner supplies its reports. HarmonyOS production HAP still requires DevEco and production signing (#6734). No debug IPA/APK, unsigned HAP, development Electron installer or ad-hoc signature is an acceptable replacement. Release-PR jobs use only their required protected credentials; the unprivileged CLI/Web job has no signing secrets.

### Historical local tag consumers

The local tag-consumer section below documents the pre-release-PR pipeline and existing private recovery tools. It is not a second scheduler to run alongside release PR publication. Existing tags and Releases remain immutable historical records.

## Publishing a release (`.github/workflows/release.yml`)

A `v*` tag publishes the hub container image and creates a GitHub Release. **Only `chyroc` may create, update or delete a `v*` tag, dispatch an image release or rerun its jobs.** The repository's `Release tags: administrators only` ruleset permits only the administrator role to bypass its create/update/delete restrictions; other collaborators cannot publish a version tag. The workflow independently requires both the run's actor and its triggering actor to be `chyroc` before any checkout, build or upload, including manual dispatches and reruns. The Release's `github-actions[bot]` author identifies the workflow that created it, not the person who pushed the tag; the Actions run records that triggering actor.

This boundary covers automated publication; collaborators retain their existing write permissions. GitHub does not offer a separate release-only permission on this organization, so those permissions still allow a manual GitHub Release from an existing ref. Restricting manual releases too would require a separate collaborator-permission change, outside this policy.

Docker hubs (`docs/operations/docker.md`) use the ghcr image, linux/amd64 + linux/arm64:

```sh
git tag v1.2.0 && git push origin v1.2.0
# -> ghcr.io/<owner>/<repo>:1.2.0 and :latest
# -> GitHub Release v1.2.0 with generated notes
```

- Smoke-test first (amd64 + compose), then build and push both architectures only after it passes. Once image publication succeeds, the same job creates a GitHub Release named for the full tag with automatically generated notes. Stable Releases are created with `--latest=false`: the complete verified hub consumer marks one Latest only after its assets and immutable tag manifest pass publication. Prerelease tags containing `-` (`v1.3.0-rc1`) only push the version image, without touching `latest`, and mark the GitHub Release as a prerelease. Creating the Release does not push another tag.
- To get an image of the current branch without a tag or GitHub Release, `chyroc` can manually run release in Actions (`edge`, `sha-<commit>`).
- Image smoke tests run through an explicit workflow dispatch; only the documented `v*` release-tag event starts image and GitHub Release publication automatically (`packages: write` and `contents: write` are granted only to the publish job). This exception supplies the version and `latest` images used by Docker installations; no branch push or PR starts it. A manual dispatch never creates a GitHub Release.
- If Release creation fails after the images were pushed, rerun the failed job for the same tag. Rebuilding and pushing the same immutable image tags does not advance `latest` to another source revision; fix any reported tag or Release conflict rather than creating a replacement tag.
- The image's personad uses the tag minus `v` as its version (`personad version`); when node differs, the App prompts to redeploy, using the image's own builds or the release's (`docs/operations/docker.md` ("Node builds")).
- The image's personad is on the `docker` channel: no skills (like all binaries), no self-update; a new image prompts you to pull it (`docs/operations/hub-update.md`).
- When the repository is private, the package is private too; NAS installs need `docker login ghcr.io` (`docs/operations/docker.md` ("Image")).

### Darwin command-line programs on version tags

The `darwin-cli-sign` job signs `personad`, `persona-node`, `persona-link`, `persona-hub` and `persona-manager` for both darwin architectures on stable and prerelease version tags. Its `darwin_cli` availability check needs only `MAC_CSC_LINK` and `MAC_CSC_KEY_PASSWORD` in the protected `persona-desktop-release` environment.

The job builds the tag's programs, signs them under fixed identifiers, verifies each Developer ID signature, recompresses the signed binaries, rebuilds `SHA256SUMS` and `latest.json` from those downloads, verifies that manifest, and attaches the darwin archives and metadata to the same Release. If either certificate secret is missing, the gate reports `Darwin CLI signing blocked` with the missing secret names and the job does not build or upload; there is no ad-hoc fallback. A missing Developer ID identity also stops the job before upload.

Prerelease CLI artifacts do not change GitHub's `releases/latest` selection. The `latest.json` attached to a prerelease is that Release's version manifest, not a move of the stable Release pointer.

### Private version-tag consumers

A version tag starts the public container/GitHub Release workflow only. It does
**not** copy signing keys, private publication endpoints, or
broad personal API tokens to GitHub Actions. Each release computer instead runs
its existing Claude loop with the prompt printed by:

```sh
python3 scripts/operations/tag_release.py --config <unused> --repository <owner>/<name>
```

The loop calls `scripts/operations/tag_release.py --once` for every private
platform configuration installed at `~/.config/persona/tag-release/<platform>.json`
(0600). It does not run an Agent or build on every poll: the coordinator skips
a completed platform/tag pair and invokes an optional per-tag Agent only for a
new one. The same prompt file also describes the platform-specific publication
and receipt contract (`scripts/operations/tag_release_prompt.md`). Configure the
loop's own normal scheduler on each machine; do not start a second scheduler
for the same platform.

A private configuration has only these fields, with no secrets:

```json
{
  "repo": "/absolute/path/to/shared/repository",
  "worktree": "/absolute/path/to/dedicated/detached/worktree",
  "state_dir": "/absolute/path/to/private/platform-state",
  "repository": "<owner>/<name>",
  "platform": "hub",
  "command": ["/absolute/path/to/existing/platform-publisher", "--source", "{commit}", "--version", "{version}"],
  "receipt": "/absolute/path/to/private/platform-state/artifacts.json",
  "agent": ["claude", "--print", "--permission-prompts", "host", "{tag}"]
}
```

- `command` is required and is an argv array, never a shell string. Allowed
  placeholders are `{repo}`, `{worktree}`, `{state_dir}`, `{tag}`, `{commit}`,
  and `{version}`. The coordinator runs it only after checking the immutable
  remote tag and preparing its exact detached worktree. The command reuses the
  platform's existing signing, release lock, and publication receipts.
- The optional `agent` argv array additionally accepts `{prompt_file}` and
  `{log_file}`. Its prompt and tag/commit environment let the local Claude loop
  select already-supported platform steps, not discover or distribute secrets.
  The coordinator retains its exclusive lock across the Agent and the command.
- The private command writes the receipt only after its platform verification
  succeeds. The receipt has the exact `commit`, `store_verified: true`, and an
  `assets` list of absolute regular files with safe names and SHA-256 digests.
  An empty list is valid only for verified store-only publication.
- `hub` uses `publish.sh`, its publication lock, and live download verification;
  `web` is carried by that verified artifact. GitHub asset upload does not
  independently prove a production deployment is done.
- The coordinator never creates or moves a tag. It requires the corresponding
  public (not draft) GitHub Release to exist, then uploads verified assets and
  compares their remote SHA-256 digests. A failed platform does not complete
  that platform's checkpoint, and does not claim that another platform passed.
- A receipt that carries `latest.json` (the hub's) is checked before anything
  is uploaded. It must list only files in the receipt, at their sizes and
  sha256s. Every binary must be its build, and this machine's `personad
  version -v` must say its version and build. Otherwise the platform fails
  (#4522: v0.2.1's manifest named another build than its binaries). Every
  `personad` must contain the exact GitHub update base used by the Release's
  `install.sh`, and the installer's own `DL` default is checked too;
  `publish.sh` renders that installer from the same base it compiles into the
  binaries. Tagged builds use the Release's UTC `createdAt` minute through
  `PERSONA_RELEASE_BUILD`, so CI and local verification share one build identity
  even for backfills and retries. When Darwin signing is enabled in CI, the coordinator waits for that job, downloads its staging
  `ci-latest.json` and `ci-SHA256SUMS` with digest checks, and non-Windows artifacts, and merges them with the locally
  verified Windows archives and reports into the final manifest and checksums;
  a skipped job keeps the private build, and a failed job stops publication. The downloads go up first; `VERSION`, `SHA256SUMS`
  and then `latest.json` go last, each on its own, so a hub reading
  `releases/latest/download` never sees a manifest whose files are not there
  yet. After remote digest verification, stable hub publication fetches the
  immutable tag's `latest.json`, requires the tag's version and build, and only
  then marks the Release Latest. A backfill of an older stable tag does not
  move Latest backward. Prereleases and non-hub receipts do not move the
  Latest pointer. The uploaded checkpoint retains the exact receipt and asset
  digests, so a feed or promotion retry reuses it and reruns only remote
  verification and promotion instead of rebuilding conflicting assets. Every
  resume repeats the same deterministic receipt validation before upload. The
  primary release-PR pipeline calls `scripts/operations/stable_release.py` after
  independently downloading every uploaded artifact: it verifies the immutable
  tag manifest, refuses to move Latest to an older stable tag, and requires the
  final `isLatest` readback before declaring publication complete.
- Windows CLI bundles (`Persona-<version>-windows-<arch>.zip`) and each current
  `persona-link-windows-<arch>.exe.gz`, `personad-windows-<arch>.exe.gz` and
  `persona-node-windows-<arch>.exe.gz` require native verification before upload (#8338).
  On Windows of that architecture, run `python scripts/operations/windows_release.py --archive
  <archive.zip-or.exe.gz> --version <version> --build <build>` for each artifact. It runs the actual
  `version`, `version -v`, and for `personad` / `persona-node` `node-update-check` with time limits,
  without installing or starting services. Link's native report cannot stand in for a node's.
  Success writes `<artifact-stem>.verification.json` beside the artifact; failure removes a previous
  report. Include the artifact, report and `BUILD.json` or `latest.json` in the private artifact
  receipt. Node programs also require the receipt's `latest.json` to list their exact platform, name
  and compressed and unpacked hashes. The coordinator checks the requested version, build, command
  results, PE architecture, archive digest and executable digests before any upload.
  This report is output from the trusted release pipeline, preventing stale or mismatched verification;
  it is not a signed attestation against a malicious publisher, nor an Authenticode signature.
  Cross-compilation or a matching build number alone does not prove the Windows programs report the
  intended version. A missing Windows runner is an explicit publication prerequisite, not a passed
  or skipped check.

For the first release, merge this consumer's PR before pushing `v0.1.0` from
the merged `main` commit. The public workflow creates the Release and image;
the separately configured private loops then publish their platforms and
attach verified artifacts. Inspect the Release assets and every platform's
actual store/publication receipt before declaring the whole release complete.

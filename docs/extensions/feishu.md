# Lark's role in Persona

> **Since 2026-11-04 (#1520, plugin step 3c)**: the entire Lark-gateway path (app `/api/v1/lark/…`, Inbox / knowledge source, Moments, assistant ol gate / credentials, `/agent`, personal Agent) moved out of open-source repository into Lark extension (`docs/extensions/extensions.md`, `docs/extensions/extensions.md` “Step 3”).
> Extension preserves behavior below; referenced `hub/lark*.go` / `internal/lark` sources removed. Open-source keeps official `lark-cli`, Lark bot, Lark mail.

**Persona is not a second Lark.** It gathers life / work information for AI processing; Lark is one source.
(User correction 2026-10-27: previous Lark-like Workbench—search, cloud docs, calendar, meetings, Minutes, tasks, approvals, OKR, contacts, Moments feed, mail, favorites, topics, chat info, Today—entirely deleted.) Do not restore those pages.

**Explicit #392 exception**: off-by-default Show Messages on Home in Lark settings adds Messages for browsing all direct / group chats. Shared accounts / chat pages / outbox / sync with Inbox, no second implementation; other Lark pages stay out. [Lark capability consolidation](feishu.md).

Three optional hub integrations: Lark gateway (third-party user-authenticated web client through private extension), official `lark-cli` (`docs/extensions/feishu.md`), and a private Lark Agent extension. This document covers gateway; its deployment / variables / commands in extension instructions.

## What stays

| Capability | Location | Notes |
| --- | --- | --- |
| Messages | Inbox (`docs/assistant/inbox.md`), optional Messages | Unread / mentions / direct chats mixed with mail, event-grouped / AI-sorted |
| User-related Moments | Inbox via extension, formerly hub inbox_moments.go; knowledge | Comments on own post, replies / mentions: one event per post, AI sorting / comment drafts, Send posts comment back (`docs/assistant/inbox.md` “Moments”) |
| AI Lark tools | Hub, `server/internal/lark` / assistant `lark_api` | Read-only search / read messages, docs, Moments, calendar, Minutes as context |
| Persona in Lark | Bot (`docs/extensions/bots.md`) | DM self-built bot = main chat; extra Persona entry, not copied Lark feature |

## Data paths

- `GET /api/v1/lark/raw/<path>` = gateway `GET /agent/v1/<path>`, read-only; JSON `/api/...` media rewritten to hub. POST only `search/page` / `search/mention-history` (large-body read search). Assistant `lark_*` MCP same `lark.Read` allowlist.
- **Assistant calls gateway itself** (2026-10-30), gateway skill from extension since #1571, no repo copy. Commands directly call agent API for reads / writes; node gate confirms externally visible writes on phone. `docs/assistant/assistant.md` “Lark: skill supplied by the Lark extension”. Quick reference below supplements local tests; gateway skill references more complete and authoritative.
- Chats / messages / read / send / reactions / card buttons use dedicated hub routes (`docs/protocol/protocol.md` Lark APIs).
- **Without gateway, official `lark-cli`** (`docs/extensions/feishu.md`,#273): assistant integration and the `larkcli` connector are available, with `lark.cli` in the manager's manager.json, assistant PATH, and the same write gate (`docs/assistant/assistant.md` “Lark: `lark-cli`”). Watched document comments enter the Inbox as `kind: "doc"` (#2691); chat Inbox / knowledge / `/agent` / app Lark pages are added progressively in the L4 branch and later plan steps.

## Gateway and hub on separate computers

Gateway Agent API remains source-loopback-only. If source can reach hub, dedicated SSH user reverse-tunnels hub loopback port to source Agent API. Source user systemd service `Restart=always`, `ExitOnForwardFailure=yes`, SSH keepalive, linger, independent of developer machine. Pin hub SSH host key.

Hub tunnel user public-key / remote-forwarding only: `GatewayPorts no`, PermitListen 127.0.0.1:<tunnel port>, `PermitOpen none`, `MaxSessions 0`; disable TTY / agent / X11 / Unix forwarding. Key restrict,port-forwarding,permitlisten="127.0.0.1:<tunnel port>", ssh -N -T. Check `sshd -t` / `sshd -T -C` user=<tunnel user>,host=localhost,addr=127.0.0.1 before reload, preserve admin account.

The manager's `manager.json` lark.base_url=http://127.0.0.1:<tunnel port>/agent/v1, `lark.token`,0600. Transfer token securely from source env, never print / commit, backup config. Verify loopback listener, unauth health 401, authorized health / chat reads, `/events` WS, repeat after restart. No sending / read-state changes required.

Same-machine Docker gateway / hub may use configured `lark.container`, no default, reading agent token from container env rather than config; recreated-token 401 rereads. Now extension owns this, see instructions.

Distinguish two 401 types (2026-10-31): gateway lost Lark login returns `AUTH_SESSION_EXPIRED` / `AUTH_*`, valid agent token. Old hub discarded configured token on every 401 then inspected default Docker container, breaking all requests / live events until personad restart. Now `AUTH_*` gives Gateway Not Signed In to Lark (API 502 `lark_not_logged_in`, logs / Inbox / app; extension logic). Never discard configured token. Event reconnect backoff max 1 minute, same error logged once, auto-recovers on user login.

## Multiple accounts

Gateway x-multi-account `GET /agent/v1/accounts`; choose account via gateway header / ol_account. Account number is accounts.id, not Lark user ID; added-account resource URLs include ol_account. Persona reads all (#1321, previously `lark.account` only):

- **Default account**: `/api/v1/lark/*` requests without `ol_account` use the default account from the extension's own Lark-gateway configuration. Hub does not pass `manager.json`'s `lark.account` to the extension. The Inbox also follows the extension's default: the manager reads `default` from `GET accounts` on the extension messaging source `lark`. If unavailable, it uses the last successful value; only if none was ever obtained does it fall back to `lark.account`, or primary account 1 when omitted (#5098). Old Android / web clients without `ol_account` still see only the default account. Assistant `lark_*` tools / `ol`, knowledge, `/agent`, personal-Agent settings, and document reading also use it; `lark_messages` can pass `account` to read other accounts, and the extension documents account selection for its commands.
- **Per-request**: all Lark APIs accept `ol_account`=<number>, extension-provided, formerly lark_accounts.go per-account clients. Invalid 400, nonexistent gateway rejection, never default fallback. Preserve media ol_account. Raw proxy forwards query, not client account header; never query in base_url.
- **Account list** `GET /api/v1/lark/accounts` fetches at most once/minute, selecting logged-in nonquarantined Inbox / events accounts. Old gateway without accounts treated default-only.
- **Events**: default connection, others reconciled every 2 minutes. App lark events carry account, nondefault `ol_account` for cache targeting. `/agent`, personal Agent, knowledge only default events.
- **Inbox**: sequential each signed-in account unread / mentions. Default errors fail round; other errors log only. `lark.account_id`, old 0=1; `lark.ol_account` for nondefault app open / reply. Same group in two accounts distinct events. Multiple sources “Lark · <name>”; reply as item's account. Never change Lark read state.
- **Web**: `web/src/lib/lark-accounts.ts` list / browser selection / `larkPath` query. `LarkChatRef.account` follows reads / read reports / sends / replies / topics / pins, accepts same-account pushes only. Messages account buttons below search, lists `larkChatsFor`, heading Group Chat · <name>. Inbox uses `lark.ol_account`; settings lists accounts. E2E `scripts/test/web_lark_accounts_e2e.py`, MOCK_LARK_ACCOUNTS=1. Android no switch, default only.

## API quick reference (tested on hub; assistant uses extension skill, gateway references authoritative)

### Search
- `search/page` POST body `{tab,query,cursor,limit,senderIds,chatIds,feedId,startTime,endTime,attachmentTypes,chatType,docTypes,ownerIds,sortType}` → `{items,nextCursor,total}`. Tabs from `search/tabs`: messages / cloud docs / apps / contacts / groups / mail / help desk / events. Query nonempty or 400; Unix seconds; `nextCursor` several-KB token.
- Quick search: `search?q=` messages, `search/people?q=`, `search/documents?q=`, `calendar/search?q=`, `mail/search?q=`, `minutes/search?q=`, `todos/search?q=` with token pagination; search/in-chat?feedId=&q=.
- Highlights UTF-16 offsets; negative message position means topic reply: `GET messages/:id` for `threadId` before navigation.
- Event hit ID <eventId>:<instance start>; `calendar/event/:id` only prefix before colon.

### Documents
- `documents/read?url=&format=markdown`;403 `DOCUMENT_EXPORT_FORBIDDEN` /422 `DOCUMENT_FORMAT_UNSUPPORTED` → `format=json` ProseMirror tree. Base / files / boards Open in Lark. Also `documents/header`, `documents/reader/info`, `documents/ai-summary` (read-only cache), `documents/comments`.
- Markdown export fixed start # Title + `[Source](<url>)`; images absolute gateway URLs rewritten to `/api/v1/lark/raw/documents/…`.
- Each list item separate `bulletList` / `orderedList`, continue numbering across siblings. Table colwidth pixels; implement rowspan placeholders.
- `authority.copy=false` disables selection.

### Calendar / meetings / Minutes
- `calendar/week?start&end` UTC …Z; `+08:00` gives 400. `calendar/event/:id` recurring uses sourceId.
- `meetings?count&cursor`, `meetings/:historyId`, `meetings/:historyId/insights` AI notes.
- `minutes?scope=all&cursor`, `minutes/:token`, `/transcript` `/summary-items` `/media`, hub Range forwarding for streaming.

### Tasks / approvals / OKR
- `todos?count` no cursor max 200; `todos/completed`, `todos/:guid/detail` lacks title, lookup list; task-lists. All-day due date UTC midnight.
- `approvals?tab=todo|done|initiated|ccUnread|ccRead&cursor&q`, `approvals/detail?instanceId`; external-system 502 APPROVAL_DETAIL_UNREADABLE.
- `okr/people`, `okr/owners/:ownerId/periods`, okr/:okrId.

### People / Moments / Lark mail
- `users/:id` profile, `contacts/org`, `contacts/departments/:id`, search/people?q=.
- `moments/posts/:id`, `/comments` dedicated hub route, `moments/feed?tabId=`, moments/notifications.
- `mail/threads?label=INBOX&cursor…`, required label; mail/messages/:id(/html). Base64 ID encoded exactly once in path, %2F.

### Chats
- `conversations/:feedId/settings` announcement, `/pins` `/resources?kind=docs|files|media` `/todos` `/members`; favorites; threads/feed.
- List pitfalls `docs/lessons/lark-mail.md`: unreliable `isDone`, `onTopRankAt` pins, unknown scope treated as inbox.

<a id="话题分页限制h06"></a>

### Topic pagination limit (H06)

Topic replies (`GET /agent/v1/messages/:messageId/thread`) page only backward: the gateway returns the latest page or the one before `beforePosition`, never a newer page.

Hub topic route therefore has no after yet. Ordinary messages support `afterPosition`, but topic replies use independent `threadPosition`; cannot borrow ordinary cursor or append latest page as continuous. Add newer pagination once upstream fetch / query / cursor contract exists. No real Lark / live hub accessed; conclusions from source review.

### lark-cli Inbox conversation details

The lark-cli connector exposes read-only `Threads` pages for the conversation keys
produced by its Inbox source (`larkcli:chat:<chat>` and the optional `:thread:<topic>`).
The CLI supplies each message ID, sender, source time, direction and older-page
cursor; legacy collected context is never split into guessed messages. When the
shortcut omits the conversation kind, the web page keeps the header neutral.

The web page renders the CLI's textual card envelope and safe Markdown using the
existing message renderer. Known merge notifications put the linked change and
description first; author, repository and original text remain available in
details. Long descriptions expand and collapse without discarding their content.
Unknown or incomplete envelopes remain literal text. A reply destination shows a
friendly name and a preview only when its message is present; its routing ID stays
in details and is unchanged.

**Open in Feishu** (or **Open in Lark**) is available beside AI draft reply when
the CLI supplies the original message's AppLink. Its chat or topic positioning
parameters are preserved. Missing or unrelated links do not produce an action;
opening the source does not send a message or replace the existing draft action.
For previously collected items, the loaded original message can supply the link
without a history backfill. A different message is never used as its substitute.

Conversation reads distinguish unsupported capability, missing access, rate limits
and transient failures, retaining collected content and previously loaded pages.
This is the text/card portion of #856 L6. Collection remains L4; attachments,
reactions and new write actions are not provided by these pages. Existing Inbox
reply actions and their confirmation gates are unchanged.

## Lark image and file sending

> **Since 2026-11-04 (#1520, plugin step 3c)**: the entire Lark-gateway integration (app `/api/v1/lark/…`, Inbox and knowledge sources, Moments, assistant `ol` gate and credentials, /agent, personal Agent) is no longer in the open-source repository. It is provided by the Lark plugin (`docs/extensions/extensions.md`, `docs/extensions/extensions.md`, “Step 3”).
> The plugin preserves the behavior below; referenced hub source files (`hub/lark*.go`, the `internal/lark` client…) have been removed. Open-source retains official lark-cli, Lark bots, and Lark mail.

The existing `LarkChatView` composer's “＋” selects photos or files, with preview and removal before sending. Previews reuse the image viewer with pull-down dismissal and system Quick Look; messages retain native Lark formatting.

### Sending and confirmation

- Text, images, and files submit in order through one session send queue. Waiting for an echo does not block the next submission.
- Each message has a stable `client_id`. File selection saves a private snapshot; retries do not reread originals that may change or lose access. Startup cleans selection copies left by the previous process; durable media uses a separate directory excluded from cleanup.
- Successful requests save `message_id`; echoes replace local messages by `client_id` / `message_id`. Do not remove attachments based on empty text, filename, or timeout.
- Failures retain attachments for retry, restoration to composer, or deletion; leaving and reopening chat restores them. Queues isolate hub group, actual Lark account, and session.
- Quoted text still uses the main reply API; topic replies retain existing behavior. Text and attachments send separately.
- Images and files preserve quotes too (#1035): each attachment quotes the same message rather than silently sending normally. Requires hub extension capability `lark_attachment_reply`. Without it, older extensions would ignore quotes and send, so block and explain “Update the Lark Extension on Your Hub to Send Images and Files Quoting Messages”; text, attachments, quote remain in composer. Outbox stores quote, topic flag, and `threadRoot`, preserving routing on restart / retry. Topic sheets opened from a chat also select images / files (#1035 part 2), using **that chat's same durable outbox** with `thread` + `threadRoot`. Recovery, account isolation / verification remain. Chat pending rows omit topic replies; topic page displays only its own. Topic refresh reconciles against topic replies; before retry reread latest topic replies to avoid duplicates. Unknown-result files offer verification only, never reupload. Topics opened without chat outbox, or unsupported hub extension, have no attachment controls. Topic text retains original send path.

### Hub APIs

Dedicated `images` / `files` bind the configured gateway account, reuse device authentication / roles, never expose a generic write proxy. Images validate encoded size and actual MIME; files stream with Content-Length limits. Preserve gateway login-expiration codes. Old hubs require upgrade before attachment sending.
Quotes / topics (`lark_attachment_reply`): images JSON adds `reply_to` and `thread`; files query adds `reply_to` / `thread_root`. Upstream maps to images + `replyTo`, `image-uploads` + `messages/{id}/replies` with `replyMode: thread`, files + `replyTo` / threadRootId. Files bypass gateway outbox and lack idempotent retry; unknown outcomes require history checks, no automatic reupload.

### Limits and regression coverage

At most 4 attachments per batch. Photos convert to JPEG, longest edge 2560 px, strip location metadata; originals / animated images may be sent as files. Files 1 byte–100 MiB.
Normal-chat text, quotes, attachment drafts, outbox persist together (#688). Files in Application Support, excluded from backup, system file protection. Records isolated by digest of hub group / actual Lark user ID / chat; no credentials or original paths. Same-group hub address / device-token changes reuse draft; account switches use separate drafts. After initial online account verification, local drafts open offline; before sending reverify actual account using existing read-only accounts API. Failure retains content and sends no write. Legacy text / current-and-backup-address outboxes lack account identity, requiring user attribution confirmation; never auto-import across accounts. Topic replies retain original implementation.

Before send, atomically save “clear draft + add outbox”; only then clear input and request network. Persistence failure retains draft. Persist attempt marker before network. Restart marks only attempted submissions Unconfirmed Send Result; unsent queued items allow explicit retry, none automatically resend. Reconcile receipts by client_id / message_id; older messages outside latest page checked read-only by original ID, not retained forever at bottom.

Gateway file uploads do not use text / single-image outbox retry. Definitive rejection or confirmed failure to connect allows retry. Timeout, disconnect, server errors, damaged receipt offer Check Send Result / Remove Local Record only—no upload or composer restoration generating a new ID. Successful file receipt may omit message_id: retain Sent, no retry; Remove Local Record removes only phone record, not sent Lark message, and never infer echo from same filename. Client_id alone does not prove idempotent file retry. Upstream read-only send-status work is tracked in gateway issue referenced by extension instructions.

Restart recovery loads / reconciles only on opening that chat, no background bulk resend. Corrupt / newer-version state stops sending. Restore Input → confirm Clear Draft and Start Over deletes chat's local records / media and legacy recovery backups. Storage failures offer retry, not false version-incompatibility errors. Temporary selected copies release with references; durable copies cleaned after atomic new-state write.

`LarkAttachmentTests` covers snapshot isolation, size bounds, original request bytes, retry IDs, exact echoes, quote routing / account isolation, attachment quote / topic fields, capability gates, restart-preserved quote / root. `LarkAttachmentFlowTests.testQuotedPhotoFollowsWhatTheHubCanCarry` runs current / old mock (`MOCK_LARK_OLD_ATTACHMENTS=1`): preserves quote or warns / retains draft.
`LarkAttachmentFlowTests` / `LarkSendTests` use local mock hub for previews, pull-down dismissal, failure / leave / reopen, retry, text / quotes / topic replies.
Screenshots: Attachment regressions, Persistence regressions. Real accounts inspected read-only, no test messages sent.

Read-only gateway verification 2026-11-03: `GET /agent/v1/messages/{id}` for two synced messages, one own / one other, returned `item.id/feedId/isMine/localState` matching identity / chat / ID. Hub forwards fields; mock adds exact message route. No bodies / IDs / credentials recorded, no messages / read receipts sent.

Upload total budget estimated at 128 KiB/s plus one minute processing, minimum three minutes, ~14 minutes for 100 MiB; phone and hub use same budget. User cancellation propagates immediately. Gateway pending / failed local copies do not display as sent.

Text edits coalesce into disk transaction once per second; attachment changes, page exit, background save immediately. Send / restore cancel old delayed saves. Composer locks only during local commit, not network send / reconciliation / latest fetch. Disconnect hub / remove group invalidates queue / cancels tasks first, then deletes group drafts / attachments / account bindings; late HTTP / disk callbacks cannot resurrect cleaned data.

Read-only `/agent/v1/accounts` verification 2026-11-03: `activeId` / account id are strings, selected account unique, boolean `signedIn` / nonempty string userId. Match hub-request-selected account, not first primary. No real IDs / accounts / credentials recorded.

## Encrypted Lark messages

### Integrated: reading and display

The Lark gateway Agent API retains old `parts` / `textPreview`, adding a versioned `olk` display projection. Hub forwards it and rewrites media URLs; iOS distinguishes decrypted body, public display content, and group-key distribution notices. If decryption fails, show public content labeled "Encrypted message · Public display"; before cache revalidation, label "Encrypted message · Public content only," never presenting public display as the body. Ordinary text containing `加密群密钥分发消息` (encrypted group-key distribution message) is not mistaken for a system notice.

Uses existing vault and Agent plaintext-access authorization, no new decryption permission. If authorization is revoked or account changes, upstream omits plaintext display fields. Unknown / missing protocol versions never use plaintext fields. Missing / null public content becomes an empty array; an incomplete optional projection in one message does not block an entire ordinary-message page. Images still use upstream covers; encrypted-image keys / decrypting downloads are out of scope here.

Quoted messages carry the same display information. Chat / composer quotes label "Public display" or "Public content only" while locked, offline-cached, or restoring drafts, never treating public display as quoted original text. Copy uses only currently visible body; encrypted messages do not auto-translate. When iOS persists messages / quotes, encoding retains only public content, clearing old text fields, body parts, and plaintext inside structured projections. Reopening rechecks permission on the server before reading. **This protects only iOS message-snapshot and quote-draft encoding paths**, not a claim that all storage is plaintext-free. Lark Inbox sources and Agent context pipelines may still flatten old `textPreview` into persisted bodies (formerly hub lark*.go / lark_agent.go; since #1571 both paths live in the Lark extension, and open-source server code no longer reads `textPreview`). Audit other consumers separately. This applies to messages carrying new projections; encryption cannot be inferred from plain text already flattened by old servers.

### Phone vault unlock and encrypted text sending (#689)

Only chats entered through home Messages gain "Ordinary / Encrypted message" selection. After choosing encryption, select historical content for public display; candidates match the Lark gateway web UI (historical text within one day two weeks earlier). Without candidates, retain the draft, never silently substitute a fixed placeholder. If the vault is locked on first send, enter a password on phone: gateway login password when unified password is enabled, otherwise vault password. Password exists only briefly in the unlock request, never drafts, outbox, logs, configuration.

Inbox keeps the original composer. Both entries share drafts / outbox; opening an unsent encrypted draft from Inbox retains it and guides back to Messages, never sending plaintext. First version supports text / quoted replies in ordinary private and group chats; encrypted attachments, topic groups, thread composers remain future scope.

#### Deployment and authorization

Upgrade both the Lark gateway with native encryption APIs and Persona Hub; a new app alone does not enable it. Verify deployment separately, not from app installation or merged PRs.

1. Create a vault in the gateway web UI. Agent authorization for decrypted-body reads stays in existing web settings; native-send authorization does not enable it.
2. Set independent native-encryption credentials / account in gateway deployment (variable names in the Lark extension docs). Generate new 43–128-character base64url credentials; never reuse ordinary Agent tokens. Account is the actual Lark user ID, not a gateway slot number.
3. Set extension secret `native_olk_token` to the same independent credential (since #1571 hub no longer reads `lark.native_olk_token` from `hub.json`; see `docs/operations/config.md`). `lark.account` still selects a logged-in gateway slot. Restart both services. Never put credentials in app, repository, PR, assistant environment, or QR codes.
4. Revoke by removing / rotating gateway native credentials and restarting. Old hubs can no longer unlock, prepare ciphertext, or submit new encrypted sends. Upstream-accepted messages are unaffected. Ordinary Agent access and decrypted-reading authorization are independent.

Missing independent authorization, account changes, unsupported interfaces, wrong passwords, or encryption failures explicitly fail and retain input, never fallback to plaintext. Vault creation, recovery after password resets, authorization configuration remain web / deployment tasks. Phone only unlocks / sends, never exports private keys. Public access follows Hub TLS requirements.

#### Unsupported versions

Before selecting encryption or opening historical public-display content, fetch native encryption state for the current chat's hub / account, without version-string comparison. Hub advertises `lark_native_encryption` in handshake, but clients still request state for versions supporting the endpoint without advertising it. Missing hub endpoint prompts a Hub upgrade; supported hub with missing gateway endpoint returns `lark_gateway_upgrade_required` (426), prompting a gateway upgrade. Missing authorization remains `VAULT_NATIVE_NOT_AUTHORIZED` (403), not a version error. Network / login / session errors do not masquerade as upgrade requirements.

Offer "Check again." Preserve encryption intent and input; failures never clear drafts or silently switch to ordinary sending. Normal sending rechecks account / authorization too. Old-interface errors in historical candidates, unlock, ciphertext preparation / submission all map to readable notices, avoiding `not found` when restoring old drafts or switching deployments.

#### Retries and local storage

Generate ciphertext in the vault, atomically persist draft clearing and pending-send record, then submit. Each ciphertext has a stable UUID. A logical message may include preceding group-key distribution; the final message uses the original UUID and preceding IDs derive deterministically from it. Retry after interruption with identical ciphertext / IDs, never reencrypting and duplicating. Recheck account at send boundary; gateway `expectedSenderId` guards login switches. Ordinary Inbox sending unchanged.

If any encrypted draft / pending send exists, encrypt the entire local outbox snapshot with AES-GCM, key only in device keychain. Temporarily unreadable keychain must not overwrite an old snapshot. Ordinary drafts retain old-format compatibility. This protects user-entered pending bodies, not all server consumers (reading boundaries above). Encrypted-send echo merging uses only server message ID / client ID, never same-body matching or timeout removal.

#### Sending validation

- 62 Swift tests / 8 suites passed: unavailable device key, whole-snapshot encryption, old drafts, stable multi-ciphertext IDs / quote positions, account / session-change rejection, no timeout-based echo disappearance, type scale, icons, design tokens.
- `go test ./internal/lark ./internal/hub -count=1` passed against isolated Postgres: native authorization, no credential redirects, no password echoes in errors, read-only write rejection, no plaintext fallback, idempotent receipts after Hub reconstruction, account-change rejection. `go vet` and server build passed.
- Additional whole-package `-race` was not fully green: 5 existing Link / Update tests reported races, reproduced individually on unchanged baseline `7a519db7`. Targeted feature tests passed; do not report whole-package race success. macOS Unix-socket failure from long temporary paths was ruled out with short isolated-runner scratch paths, without product-path changes.
- Xcode 26.0 iOS (Watch / extensions included) / Mac built, Mac signing disabled. All sends, unlocks, passwords used isolated mocks, no real-chat sends; deployment and real end-to-end encrypted sending remain unverified.

- iPhone 18 Pro: 3 UI flows each in light / dark passed; 16 final screenshots inspected individually. Covers unlock failure then success, retry, cross-entry / force-quit recovery, ordinary Inbox composer unchanged.

Version compatibility regression (#1131): 7 Swift tests passed (old routes 404 / 405 and 4 error-code representations); 4 UI flows passed light / dark, with 8 screenshots individually inspected. Go lark / proto tests, isolated-Postgres native-encryption / handshake tests, and matching `go vet` passed. Initial UI regression found mock generic `not_found` unconverted; added product handling and parameterized cases, rerun passed.

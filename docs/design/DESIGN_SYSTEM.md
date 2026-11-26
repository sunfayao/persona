# Persona design system

Reference for the web app in `web/`, the only client. It describes what the code does; where the two differ, the code
wins. The binding rules are in `AGENTS/07-frontend-development.md` (design rules) and `AGENTS/08-text-and-languages.md`
(text); this document gives the values and patterns behind them. Product reasoning is in `docs/design/PRINCIPLES.md`.
"Principle N" below refers to its numbering.

## Reading route

| You are building | Read |
| --- | --- |
| A list of sessions or pending requests | 3.3, 5.6 |
| A pending request: a question, a plan, a tool approval | 4.5 |
| A button or a row of buttons | 1.2 rule 3, 3.5 |
| A chip, a badge, a count, a state mark | 3.6; principle 4 for what a number counts |
| A card, a dialog or another surface | 3.2 |
| An empty, loading or failed state | 3.8 |
| A confirmation or an Undo | 4.3 |
| Words: labels, errors, notifications | 5 |
| A colour, font, radius, size or animation | 2 |
| A field for a secret | 1.2 rule 4 |
| Any UI change | the self-check at the end |

## 1. Sources of truth and rules

### 1.1 Where each thing lives

| Concern | Where |
| --- | --- |
| Tokens, keyframes, utilities | `web/src/styles.css` (hand-written; nothing generates it) |
| Shared components | `web/src/ui/` (`atoms.tsx` holds `Button`, `IconButton`, `Dialog`, `Menu`, `Switch`, `Collapse`, `toast`) |
| Screens | `web/src/views/<area>/` |
| Theme choice | `web/src/views/theme.ts`; `web/index.html` sets the class before first paint |
| Text catalogs | `web/src/i18n/locales/<lang>/<namespace>.json` |

### 1.2 Rules

1. UI code uses tokens: role colours, `rounded-chip|control|card`, `text-card-title|meta|label|submeta`, the shadow
   tokens. A literal pixel value is for lines of 4 px or less and for the few sizes this document names.
2. Never restyle an existing screen directly. A change that brings a screen in line with this document is a fix; a new
   look is a new, opt-in style.
3. A button label stays on one line at every width. When it would not fit, in order: (a) shorten the copy in every
   language; (b) ask whether a button is the right control, since long or user-supplied text belongs in a row, a list
   or the body, and a set of actions that does not fit goes into a menu; (c) stack the whole group vertically, primary
   first, each label still on one line, where a label would not fit its share of the row (decided by measured width).
   An owner-decision row on a narrow container puts its text on the full width, its secondary buttons together in one
   row below it and the primary button alone on the last, full-width line; it has no actions menu.
4. Tokens and keys use a secret field, not a plain one (`SECRET_INPUT` in `atoms.tsx`). A key that a secret provider's
   reference can replace uses `KeyField` (`views/settings/`): its **Use a reference** switch sits in the key's own label
   row, and a reference is typed in plain text, never masked. The client never guesses reference mode from an `op://`
   prefix.
5. Documents are English. Visible strings go through the catalogs (5.5).

### 1.3 Enforcement

| Test | What it guards |
| --- | --- |
| `web/src/ui/theme-contrast.test.ts` | 4.5:1 for the text tokens on every background in both schemes; the tints stay pale; error and selection fills stay neutral; bubble, quote and `--danger-wash` text |
| `web/src/lib/busy-motion.test.ts` | every looping busy animation carries `busy-motion` |
| `web/src/ui/select-chevron.test.tsx` | the dropdown arrow's colour equals `--ink-3` |
| `web/src/ui/quiet-surface-scope.test.ts` | quiet surfaces stay in their scope |
| `web/src/i18n/catalog.test.ts` | catalog agreement and hard-coded text (`AGENTS/08`) |

Nothing tests hit-area size or spoken labels; check them by hand.

## 2. Design tokens

Colours are light/dark pairs. The scheme is a `dark` class on the root element (`@custom-variant dark`). The user's
choice is System, Light or Dark (`persona.theme`, `views/theme.ts`); `web/index.html` applies it before first paint and
sets `color-scheme`. A terminal pane can hold one scheme whatever the page's: `[data-terminal-scheme]` redefines the
tokens on that element.

### 2.1 Colour

Roles (`:root` / `.dark` in `styles.css`; each is a Tailwind colour through `@theme inline`, such as `bg-surface`):

| Token | Light / dark | Use |
| --- | --- | --- |
| `--page` | `#fafafb` / `#17181a` | page background |
| `--canvas` | `#f1f2f3` / `#1c1d1f` | sidebar and recessed areas |
| `--surface` | `#fff` / `#232427` | cards, dialogs, raised controls |
| `--inset` | `#f7f8f9` / `#1f2022` | wells, code blocks, list columns |
| `--hover`, `--hover-2` | `#f4f5f6`, `#e7e9eb` / `#2a2b2e`, `#313236` | hover and pressed fills |
| `--field` | `#f2f2f3` / `#2b2c2f` | secondary buttons, chips, input fills |
| `--ink`, `--ink-2`, `--ink-3` | `#1f2124`, `#62656b`, `#66696f` / `#f2f3f4`, `#a5a8ad`, `#979aa0` | primary, secondary, quiet text |
| `--line`, `--line-strong` | `#ecedef`, `#e0e2e5` / `#2e3033`, `#3a3c40` | hairlines |
| `--accent` | `#0285ff` / `#3d9aff` | fills, rings, selection |
| `--accent-ink` | `#0166cc` / `#7ec0ff` | accent text, filled accent controls |
| `--accent-tint` | `#e7e9eb` / `#313236` | selection fill (neutral by design) |
| `--green`, `--orange`, `--red` | `#107638`, `#a64c00`, `#bd3038` / `#6dd697`, `#eea66d`, `#ff9499` | positive, caution, failure; each is text-safe |
| `--green-tint`, `--orange-tint` | pale / `#3dbb7224`, `#f68f3c24` | status tints |
| `--red-tint` | `#f7f8f9` / `#1f2022` | stays neutral: error fills are not red |
| `--danger-fill` | `#bd3038` both | the danger button |
| `--danger-wash` | `--red` at 12% | the failed and interrupted rows; words on it are `--ink` |
| `--attention` | `#8f5c00` / `#f1b969` | what waits for the user |
| `--ai` | `#5b4fd6` / `#9c94ff` | what the AI runs on its own (workflows) |
| `--tooltip-bg`, `--tooltip-fg`, `--tooltip-muted`, `--tooltip-border` | dark in both schemes | tooltips |

Text tokens (`--ink`, `--ink-2`, `--ink-3`, `--accent-ink`) keep 4.5:1 on every background token; a new text role is
added to `theme-contrast.test.ts`. Red is for failures and destructive actions only, and never fills a whole card:
`ErrorNotice` is a neutral card with a red mark.

Other families:

| Group | Tokens |
| --- | --- |
| Agent state | `--status-needs` (= `--attention`), `--status-working` (= `--orange`), `--status-waiting` (working mixed with `--ink-3`), `--status-unread` (= `--green`), `--status-idle` and `--status-offline` (= `--ink-3`). They never follow a style |
| Hues, for categories on marks only | `--hue-red`, `-orange`, `-amber`, `-yellow`, `-green`, `-teal`, `-blue`, `-indigo`, `-purple`, `-pink`, `-gray` |
| Permission modes | `--mode-plans` (teal), `--mode-acts` (= `--hue-purple`), `--mode-allows` (= `--red`) and their 12% tints; none is a state colour |
| Bubbles | `--user-bubble` `#dceaff` / `#1d3557`, `--neutral-bubble` `#f0f1f3` / `#2c2d31` |
| Trace and diff | `--tool-ink`, `--tool-rail`, `--thinking-ink`, `--tool-hover`, `--status-accent`, `--diff-add-fg|bg|word`, `--diff-del-fg|bg|word` |
| Where an agent was opened | `--source-terminal`, `--source-desktop` |
| Token usage | `--usage-input`, `--usage-output`, `--usage-cache-write`, `--usage-cache-read` (checked for colour-blind readers; the legend always writes the numbers) |
| Switch | `--switch-on` `#34c759` / `#30d158`, `--switch-off` translucent grey, `--shadow-knob` |
| Segmented control | `--seg-track` |
| Image markup | `--mark-red|yellow|green|blue|white|black`, the same in both schemes |

Agent states and their marks (`StatusMark`, `lib/state-labels.ts`, `docs/protocol/agent-states.md`):

| State | Mark | Colour |
| --- | --- | --- |
| Needs you | glowing dot with a ring (`alert`) | `--status-needs` |
| Working | turning arc | `--status-working` |
| Waiting in the background | clock (`wait`) | `--status-waiting` |
| Unread | glowing dot | `--status-unread` |
| Awaiting reply, Just finished, Finished not opened | plain dot in the unread colour | `--status-unread` |
| Idle | plain dot | `--status-idle` |
| Offline | hollow ring | `--status-offline` |

The shapes differ so a state never rests on colour alone. State words come from `stateName` in
`web/src/lib/state-labels.ts`; no screen invents its own. Quick filters and a heading's counts (`StateTally`) show
Waiting in the background with the clock and leave it out of Working.

### 2.2 Typography

| Token | Size | Use |
| --- | --- | --- |
| `text-card-title` | 14.5 px | card and row titles |
| `text-meta` | 13 px | meta lines, buttons |
| `text-label` | 12 px | chips, labels |
| `text-submeta` | 11.5 px | secondary meta |

They set the font size only; line height comes from the context. The chat reads at `--chat-body` 15 px and
`--chat-secondary` 14 px, 16 and 15 px under 640 px (`text-[length:var(--chat-body)]`); Markdown replies use `.prose-j`
(14 px, line height 1.7). Fonts are `--font-sans` and `--font-mono`. Older code still has many `text-[Npx]` sizes; do
not add more. On touch screens every input is at least 16 px so the browser does not zoom (`styles.css`). There is no
text-size setting.

### 2.3 Spacing and layout

Spacing is Tailwind's 4 px scale (`--spacing`). Values this document names:

| Value | Where |
| --- | --- |
| `--sidebar-row` 32 px | a sidebar row (`sidebar-row` utility); fingers get 44 px |
| `--sidebar-heading` 24 px, 44 px on coarse pointers | a top-level sidebar heading (`sidebar-heading` utility) |
| `--sidebar-indent` 12 px | one level of nesting (`sidebar-indent` utility), applied once per parent boundary |
| `--panel-gap` 6 px | gutter between the shell's columns from 64 rem |
| `--chat-scrollbar-fade` 220 ms | the chat scrollbar's thumb |

Breakpoints: `xs` 340 px (`max-xs:`; the composer's mode chips turn into an icon and a short word), `sm` 640 px
(phone below it: `PHONE`), 64 rem / 1024 px (the shell's side-by-side columns; `NARROW` below it). Hover-only
behaviour is also gated on `(hover: none)` and `(pointer: coarse)`.

### 2.4 Radius

| Token | px | Use |
| --- | --- | --- |
| `rounded-chip` | 6 | chips, badges, focus ring |
| `rounded-control` | 8 | buttons, inputs, code blocks, wells |
| `rounded-card` | 10 | cards, groups |
| `--radius-nav-preview` | 16 | the navigator's preview |

Notices, popovers and pills also use Tailwind's `rounded-xl` and `rounded-full`.

### 2.5 Sizes

- Hit target: 44 px. `touch-hit` adds an invisible 44 px square centred on a control on coarse pointers without
  changing how it looks; `touch-hit-corner` does it for a ✕ in a tile's corner, growing out of the corner and at most
  `--hit-in-x` / `--hit-in-y` (6 px) into the tile. Icon buttons packed 4 px apart take a real 44 px box on coarse
  pointers instead, since centred `touch-hit` squares would overlap.
- `Button` is 32 px tall, 28 px with `size="sm"`. `Switch` is 38 × 22 with an 18 px knob.
- Status marks: dot 7 px (`sm` 6), turning arc 9 px (8), clock 12 px (11), set by `--mark-*` in `styles.css`.
- Icons are 15 px by default (2.3).

### 2.6 Shadow and elevation

`--shadow-hairline` and `--shadow-card` (a 1 px `--line` ring), `--shadow-btn` and `--shadow-inset-field` (an inset
1 px `--line-strong`), `--shadow-raised`, `--shadow-overlay`, `--shadow-nav-preview`, and `--shadow-knob` for the
switch. Static cards use a single hairline; editable fields have a flat inset edge and take the accent on focus;
elevation marks popovers, menus and other floating surfaces. A few inline shadows remain in older code.

### 2.7 Motion

| Where | Value |
| --- | --- |
| Button and icon-button state | 150 ms |
| `Switch` knob, `Segmented` thumb (`--seg-dur`) | 200 ms |
| `Collapse` | 300 ms on `EASE` (`cubic-bezier(0.23, 1, 0.32, 1)`, also `--ease-out-strong`) |
| Sidebar group folds | `--sidebar-fold-open` 250 ms ease-out, `--sidebar-fold-close` 200 ms ease-in |
| Exits (`EXIT_MS` in `atoms.tsx`) | card 160, sheet 220, toast 160, glide 220 ms |
| Reply bar after the pointer leaves | stays 250 ms, fades over 250 ms (`REPLY_BAR_LINGER_MS`, `REPLY_BAR_FADE_MS` in `ui/motion.ts`) |

There are no springs. Under `prefers-reduced-motion` every animation and transition drops to 1 ms and one iteration,
except an element with `busy-motion` (`lib/busy-motion.ts`): spinners, the working mark and the loader keep turning
at 2.4 s, because frozen would not read as busy. Spinners use `spinStyle`, which turns 100 times per cycle so React's
root listener is not woken every turn. Scripted scrolling goes through `scrollBehavior()` and expanding through
`Collapse`.

### 2.8 Icons

`Icon` in `web/src/ui/icons.tsx`: stroke glyphs on a 24-unit viewBox with round caps, `currentColor`, default size
15 px and stroke 1.9. One glyph per meaning; beside text, size an icon with the text. Sidebar page icons are neutral
ink (`--ink-3`; `--ink` for the open page). Decorative marks are hidden from screen readers when adjacent text names
them.

## 3. Components

Use the design-system component, not a lookalike. Features keep a few local pieces of the same family (`Tag`, `Chip`,
`Pill`, `Segmented` in `views/assistant/parts.tsx`, `views/inbox/InboxParts.tsx`, `views/settings/SettingsParts.tsx`);
a new screen reuses the one beside it.

### 3.1 Chat surfaces

- Composer (`views/chat/Composer.tsx`): one text field that grows with the draft. A short draft in a composer at least
  40 rem wide shares a row with its actions; longer drafts, and narrow composers, put the controls below. Its overlays
  are `ComposerPanel.tsx` (`+` and `@`), `SlashPalette.tsx` (`/`), `PermissionPicker.tsx` and `ModelPill.tsx`.
- Bubbles: the user's own message is `--user-bubble`; a relayed message and a tool's inline detail stay
  `--neutral-bubble`. Replies are unboxed.
- Reply bar (`ReplyBar.tsx`): only under a finished turn's last reply, never under narration in a work summary or a
  reply still streaming. The pointer over the reply or keyboard focus shows it, and it lingers 250 ms; touch screens
  keep a copy and ⋯ row instead. A reply cut short by an error carries a line saying so.
- Dividers: `ModelChangeDivider`, `EngineSwitchDivider` and `ModeChangeDivider` are hairlines either side of one line
  of words, `role="separator"`, not controls.
- Run status bar (`RunningStatusBar.tsx`): the active turn's state and elapsed time above the composer, in
  `--status-accent`.
- A Persona conversation names no tool and no model id in primary text (5.6).

### 3.2 Cards and surfaces

| Surface | Look |
| --- | --- |
| Card | `rounded-card bg-surface shadow-card` |
| Inset well | `rounded-control bg-inset shadow-hairline` (code, briefs, review checklists) |
| Notice | `ErrorNotice`: `rounded-xl border border-line bg-surface p-3`, a 16 px red `alertCircle`, the message in 13 px `--ink`, help in 12 px `--ink-2`, the caller's actions, then **Error details** for the raw text. Optional ✕ (`onDismiss`) |
| `Dialog` | a centred card (440 px default) that becomes a bottom sheet under 640 px (`SHEET_QUERY`); focus is trapped and returns to the opener |
| Popover, menu | `--shadow-overlay`; `Menu` (208 px default) |
| Side panel | `PanelHeader` plus a body; opened by hand |

Cards that carry one request or one fact keep the same grammar: `CloseoutCard` (a stopped session's own report, with
what the hub checked beside each claim), `TurnChanges` (changed files of a finished turn), `TaskCard` (a subagent or
workflow call), `PlanCard`. A status tint never covers a whole card: the status is a mark and a word.

The shell, from 64 rem: sidebar column, main and optional side panel as rounded `--page` panels on `--inset` with a
6 px gap. Under 64 rem the sidebar is a drawer and side panels cover the page.

### 3.3 Lists and rows

- Row height is at least 32 px on desktop and 44 px on touch.
- A section's rows show only while there are three or fewer; from four the section folds to one summary row with its
  count and the one or two facts that need attention. Long lists of records show the first five with **Show all N**.
- Sidebar groups have folding, sticky headings (`.sidebar-group-heading`); nested headings stack below their parents.
  One chevron button (`FoldAllButton`, `GroupFoldButton`) is the list's only Collapse all / Expand all and shows the
  next action. A flat list gets no invented heading. A fold is a button with `aria-expanded`; a destination is a link
  with `aria-current` when selected.
- Titles wrap in their own column with the hosting context and time below; a very long title has a full-title
  disclosure on touch.
- A row shows its most urgent state once. A session that needs the user is visible in every grouping without opening
  anything: in a section, or by its mark with a folded group counting it (`StateTally`). Grouping by location never
  hides it. A child or side session folded under its parent is never a row or a count of its own; the parent carries
  its state.
- The work-item row: a source mark with the state mark on its corner; line 1 the title and the time; line 2 what the
  session waits for or its latest line (a failed run says what failed, in red; `lib/row-line.ts`); line 3 optional
  metadata. A row never shows raw model ids, session ids or engine badges, and carries at most one count badge.
- Row actions live in a context menu or a row-end ⋯ `Menu`, never a button on every row. Several at once: checkboxes
  with a select-all box.
- One grouping control per list, with the same quick filters and title whether the list is paged or complete. A thing
  has one name in every list.
- List controls are four kinds, each looking like itself:

| Control | Decides | Rule |
| --- | --- | --- |
| Scope | which computers or hub | a menu that names what is shown |
| Filter | which states, sources or dimensions show | each active filter is a chip that removes it; Reset clears all |
| Grouping and display | how rows are grouped, sorted, drawn | a grouping control or the ⋯ menu, never a filter chip |
| Navigation entry | opens another page | a row or the sidebar, never chip-shaped |

- A filter hides rows; rows that need the user must stay visible. Display choices such as "Active within" fold rows
  into an Earlier group instead of hiding them.
- A list keeps no standing search field beside the app's search (⌘K). A row found by what is inside it shows the
  matching sentence in place of its second line.
- To-dos are filed by what the user should do next (Needs you, then unfinished work, then what just finished; only
  Shelved and Done start folded). `useOwnerNeeds` supplies the "Up to you" mark so the sidebar badge and the rows never
  disagree.

### 3.4 Navigation

- Assistant and Agents are the primary destinations; other pages open from the sidebar or drawer. Settings opens from
  the sidebar.
- Where a list and its detail take turns in one column, opening a row by keyboard moves focus to the detail's title or
  Back, Esc goes back, and back on the list restores scroll and focus to the opened row (`Todos.narrow-keyboard.test.tsx`).
- The assistant switcher heads the sidebar when the hub supplies a current assistant: its avatar, name and status
  open a roster menu (`views/AssistantSwitcher.tsx`).

### 3.5 Buttons and controls

- `Button` tones: `primary` (`bg-ink text-surface`), `secondary` (`bg-field`), `quiet` (text, fill on hover), `danger`
  (`--danger-fill`, white text), `accent`. Props include `icon`, `busy` (a spinner, `aria-busy`), `href` (opens a new
  tab) and `size="sm"`.
- A group of actions has at most one solid primary button. A recommended choice is outlined and tagged
  (`RecommendedTag`), not a second solid button. Red is for failure and destructive actions only.
- `IconButton` frames a smaller glyph; icon-only controls always have a spoken label.
- A tool approval's buttons are Deny (quiet), Always allow (secondary, only where offered) and Allow (primary).
  High-risk actions offer no "always".
- `Switch` (role `switch`) is the only on/off control: green when on (`--switch-on`), a translucent grey track off, a
  white knob with `--shadow-knob`; the focus ring follows the pill and shows on keyboard focus only. Pages never draw
  their own track and knob. Pills that toggle a mode are not switches.
- `Segmented` has one thumb that slides under the chosen segment. `SearchField` is the one search box (`surface` on a
  page, `inset` in a list column); Esc empties a field with text and is spent on that.
- `Menu` shows one state mark per item; long names wrap on touch.

### 3.6 Chips, badges, marks

- `CountBadge` (`ui/CountBadge.tsx`): tones `needs` (`--attention` on `--field`), `neutral` and `accent`; nothing at
  zero, "99+" past 99, 18 px high (16 px `dense`); the exact number is spoken unless the host's label already says it.
- `StatusMark` is the one place an agent's state is drawn; `StateTally` is a folded group's tally.
- `AgentMark`, `BrandMark`, `AssistantAvatar` (a disc with the name's first letter).
  An agent named by its kind reads the same everywhere (`spec/fixtures/agent-names/names.json`). Permission mode names
  come from `spec/fixtures/permission-modes/names.json`.
- Chips are text-only and read as their text.

### 3.7 Code blocks, inline code and quotes in replies

| Piece | Look and behaviour |
| --- | --- |
| Code block | a title bar with the fence's language and Copy, then the code in the mono face on `--inset`, scrolling sideways (`ui/ReplyBlocks.tsx`, `ui/Markdown.tsx`) |
| Inline code | no background or border; mono at 0.94 em in an ink a step quieter than the body; a click copies it (`copyInlineCode`), except inside a link, where it follows the link |
| Quote | an accent bar on a faint accent card; longer than about four lines it folds (`ReplyQuote`); a reply still being written never folds |

### 3.8 Empty, loading and error states

| Case | Component |
| --- | --- |
| Loading | `Loading` (a pixel grid and a shimmering label, `role="status"`); `Spinner` inside a busy button |
| List read failure | `ListError`: a red `role="alert"` line saying what could not be read, with **Retry**, in place of the rows. A failed read never leaves a list blank or on Loading |
| A short server-driven process | `Steps`: one line per step, the current one with a spinner and `aria-current="step"`, done ones ticked, a stop crossed. The step comes from what the hub pushes, never a local timer |
| Chat failure | `ErrorNotice` |
| Offline assistant | `OfflineState` replaces a merged list's empty state |

The error the chat stopped on is the banner over the composer with its actions (Retry, Fix it, Edit last message) and a
✕ (`role="status"`); an older error is the same card as a record in the chat, without those actions. While a model
request is retrying, the run bar shows the plain reason and the attempt count instead.

There is no shared empty-state component outside the admin app; an empty state says what is missing and what to do
next in one or two lines.

Toasts (`toast(text, tone, action?)`): one stack of at most three; 2.8 s, 5.2 s for an error, 6 s with an action; a
mouse pointer or focus on one holds its time. On a narrow screen they sit in the part the soft keyboard leaves visible
and rise clear of the dock or an open sheet's footer (`--toast-lift`, `ui/toast-lift.tsx`).

## 4. Interaction patterns

### 4.1 Gestures

Every gesture-only action has a plain alternative for screen readers and keyboards: a visible control, a menu item or a
key.

### 4.2 Message feedback

A send shows **Sending** at once from local state, then **Processing** when the hub has it (`message_send_state`),
without waiting for the model's first token. The state is said once, by the status line under the last message; the
pending bubble only dims. A failed send stays with Retry, and an uncertain one is never repeated as a new message.
Receipt alone enables no Stop. Wire and reconnect rules: `docs/protocol/protocol.md`. Principle 7 says how to measure
it.

### 4.3 Confirmation and undo

1. Reversible: do it and offer Undo in a toast (`ToastAction`).
2. Destructive with one object: a confirmation `Dialog` whose destructive button repeats the verb, a message that says
   what is lost, and Cancel.
3. Needs extra choices (keep or remove the worktree): a dialog with those choices, kept branch by default.
4. A short prompt or text entry: a small dialog.

A busy Agent's **Close…** is not greyed out: the confirmation starts with one line naming what closing interrupts and
the button reads **Close anyway**. Actions that cost nothing (sending a queued message now, Esc, Stop) act at one
press with a tooltip that says what happens. Clearing the main chat's context is on the reversible rung without a
toast: the chat empties in place and the old chat is archived.

### 4.4 Tool execution collapsed

Tool execution is one summary line that never opens by itself and expands by hand (`WorkGroup.tsx`, `Collapse`). The
opened summary is a flat run of narration and action groups on one rail; earlier live groups start collapsed, the
current one opens inside the trace the reader opened, and the final reply appears once. Rows lead with the file, query
or command; a call's own elapsed time is a quiet suffix on wide rows and the row's title on compact ones (nothing for
sub-second or unfinished calls). A failed call's red ✕ and output stay; a call that was denied,
refused or interrupted gets a gray mark with its word (Denied, Did not run, Interrupted) because it is not an error.

Opening or closing anything in a chat keeps the tapped row where it was on screen (`anchoredScroll` and the rules in
`lib/disclosure.ts`): in the middle of a conversation the row stays put and what opens grows below it; at the newest
end the list stays at the end and the row rises until it is under the top bar, then stops following and ↓ shows. After
that the list holds still while the conversation goes on until the user scrolls, taps ↓ or sends. A new expander uses
this; it never animates its own height or scrolls the list itself.

Edit diffs open in the Changes panel at that file, not in a modal.

### 4.5 Pending requests

Kinds: questions, plan approvals, tool approvals, secret entry, MCP forms, a terminal prompt, waiting for a reply.

- Home: a stack of cards above the composer, which stays usable. A request that blocks the Agent stays visible until it
  is answered or folded, and says why the Agent waits. Cards stack while they fit in half the page, then scroll under
  "N pending". Each folds to one line; any can be folded to answer later. A card the conversation's own turn waits on is
  never folded.
- A card its session does not let this client answer is disabled with one line in the attention tone saying why; it
  never greys silently (`CardBlockedLine.tsx`).
- One count, per session: Needs you, its filter and the badge count the sessions that wait, each once.
- Card text: the title names the request and what happens next, in the words of 5.6; the body gives only what the
  decision needs. Commands render formatted, never as raw JSON. A file edit's approval shows the change as the chat
  draws the call.
- Questions are one at a time with single choice, multiple choice and Other. An unsent answer is kept as a draft per
  request. A single-select pick moves to the next question after a short pause, once, and Previous / Next cancel it.
  Dragging through choices only scrolls.
- Rows that wait for the owner on a narrow container stack: the text on the full width, the secondary buttons in one
  row, the primary alone on the last line.
- The first decision on any surface wins; every other entry updates at once.
- A record of something that went wrong says what became of it once known, in a gray line; the danger colour is only
  for what is still pending.

### 4.6 Keyboard and overlays

- Esc undoes one thing at a time, innermost first: a search field with text clears, a menu's rename returns to the
  menu, a menu closes onto its button, then the topmost overlay closes (`useEscapeClose`). A modal closes on Esc from
  anywhere inside it and returns focus to what opened it.
- An open overlay closes on the browser's Back before the page changes (`useCloseOnBack`).
- Tab stays inside a modal and reaches every control (`trapTabIn` in `ui/overlay.ts`). One modal at a time from the keyboard.
- A menu is as tall as the window allows; one taller is cut, with a fade at the cut edge (`data-menu-more`) and the
  keyed row scrolled into view.
- ⌘↩ / Ctrl+↩ submits a dialog or box from its multi-line field; Enter alone is a new line, and the Enter that ends an
  IME composition does neither.
- Date and time fields save once the value is entered (on leaving or Enter, or at once for a picker pick), never per
  typed segment, and never disable the field while saving.
- A choice kept in the browser (theme, language) reaches every open tab through the `storage` event.

### 4.7 Sharing a conversation publicly

Everything that widens exposure (more messages, work details, future messages, a longer lifetime) goes through a
preview and an explicit confirmation; narrowing and revoking need no preview, and revoking confirms as a destructive
action. Defaults are the narrowest. The link is shown once with a copy action. The public viewer (`web/public-share/`)
is a separate read-only document with the same palette tokens. Code: `web/src/sharing/`.

## 5. Content

### 5.1 Voice and tone

Calm, direct, second person, like a capable teammate. Contractions in English ("can't", "didn't"). No exclamation
marks, emoji, "Oops" or "Sorry". The assistant says "I" only in its own chat surfaces; elsewhere the product is
"Persona" and agents are named in the third person.

### 5.2 Capitalization and punctuation

- Sentence case for buttons, titles, menu items, descriptions, empty-state lines, section headings and accessibility
  labels. State names in prose: "Needs you", "Working", "Unread", "Seen".
- One ellipsis character, for actions that open another step and for in-progress states.
- Complete sentences end in a period; labels and titles do not.
- A heading that holds a name (a space, a computer, a project) is drawn as written: `uppercase` is for label headings
  only (`Menu`'s `headerAsWritten`, the palette's `sectionAsWritten`).

### 5.3 Error messages

1. What happened: "Couldn't + verb + object". "Can't" only for something not possible.
2. What is safe: "What you've typed is kept."
3. One next step: "Check the connection and try again."

No HTTP codes, key algorithms, CLI flags or raw error text in the primary line; raw detail goes under **Error
details**. One generic fallback: "Something went wrong. Try again."

### 5.4 Buttons, confirmations, notifications

- Verb first, one to three words. "Retry", "Deny".
- Delete destroys data; Remove detaches from a list; Discard throws away unsaved edits; Forget removes a pairing or
  credential; Clear is a bulk reset.
- A confirmation's title is a question ("Delete this chat?"); its body states the loss and whether it can be undone.
- Notification titles name the actor and the need ("{name} needs your permission", "{name} has a question"); one
  wording per event on every surface. Never include secrets or commands.

### 5.5 Glossary and localization

| Term | Rule |
| --- | --- |
| Hub | the product term |
| Computer | the user's own machine; never "Node" in user-facing English |
| Agent | lowercase mid-sentence; "Agents" for the page |
| Session | what an Agent runs; "task" names the work Persona hands out |
| Assistant | one of the hub's assistants (`manager` in code); zh-Hans 助手, ja アシスタント; never "manager" in user-facing text |
| Persona | the product and the main assistant; never translated |

The full glossary is `docs/protocol/i18n.md`. Languages are en, zh-Hans and ja; do not concatenate sentences
(`AGENTS/08`).

### 5.6 Objects, titles and state words

| Object | Called | Title |
| --- | --- | --- |
| Assistant conversation | conversation | its title, generated or renamed |
| Agent session | session | what it is doing, from the first message or a rename; never the repository or tab |
| Pending request | Needs you | the thing wanted: the plan's title, the command or the question |
| Inbox item | message | the sender or group, and a summary |
| Computer | computer | the user's name for it |

Titles of requests are the same on every screen that names one:

| Request | en |
| --- | --- |
| Plan | Plan awaiting approval |
| Tool or command | Waiting for your approval |
| Question | {agent} has a question |
| Terminal prompt | Terminal needs you |
| A kind the client does not know | Needs you |

- Primary screens (list rows, top-bar titles, the Assistant home, notifications) never show raw model ids, session
  ids, paths or engine names; a row may name the model by its short name.
- Views grouped by location may show workspaces, tabs, worktrees and branches; each level says something its heading
  does not.
- An Agent page says the agent's state once: the running status bar while it works, the pending cards while it needs
  you, its row in the sidebar always. The top bar shows no state.
- A failed run has no state of its own; its row's second line says what failed.
- A waiting to-do is named by what it waits on (**Waiting on you** in the attention colour only when it waits on the
  owner; other waits neutral).

## 6. Accessibility baseline

Every component meets these:

1. Hit area at least 44 × 44 px, including icon buttons whose visual is smaller (`touch-hit`).
2. Every control has a spoken label; icon-only buttons never rely on the glyph name.
3. Selected, expanded and busy state is exposed with roles or values, not only colour or shape.
4. State never rides on colour alone: pair it with text or a mark shape.
5. A gesture-only action has a visible control or an alternative.
6. Transitions and press effects respect reduced motion; every loop except a busy indicator stops.
7. Text contrast at least 4.5:1 in light and dark (3:1 for non-text marks); composite cases need a test.
8. Transient messages are announced; headers carry the heading role; modals trap focus.
9. Layouts survive 200% zoom and a phone's width without clipping or hover-only controls.

## UI change self-check

For a change that adds or changes UI or an interaction, mark each item done, not applicable or deviated with the
reason, and attach the evidence principle 6 asks for.

**Principles**

- [ ] Defaults first: works on a fresh install with untouched settings.
- [ ] Progressive disclosure: advanced options are off by default and primary screens are unchanged when off.
- [ ] User control: a new behaviour-changing setting is visible, changeable and reversible; an Agent that can change it
  explains, confirms and offers a rollback.
- [ ] Consistency: reuses existing components, patterns and count definitions; anything new has its reason written
  here.
- [ ] Model and product stay separate: names which of speed, model, presentation or features it touches.
- [ ] Fast, local-first and stateful: local Sending is immediate, hub receipt shows Processing, failure recovers.

**Rules**

- [ ] Only tokens and `src/ui/` components: no raw hex, no new `text-[Npx]`, no lookalike components.
- [ ] Existing screens keep their look except to bring them in line with this document.
- [ ] Controls are at least 44 × 44 with a spoken label; nothing shows state by colour alone or depends on hover.
- [ ] Destructive actions confirm if irreversible and offer Undo if reversible (4.3).
- [ ] Motion respects reduced motion; busy animations carry `busy-motion` (2.7).
- [ ] Opening or closing in a chat keeps the tapped row where it was (4.4).
- [ ] A session that needs the user is visible in every grouping; no raw model ids, session ids or paths on primary
  screens (3.3, 5.6).
- [ ] New copy follows section 5 and ships in `en`, `zh-Hans` and `ja`.
- [ ] Looked at in light and dark at 390 and 1440 px wide against made-up data (`AGENTS/07`).

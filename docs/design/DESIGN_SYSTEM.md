# Persona design system

Snapshot commit `738ba824c`, date 2026-11-10. All line numbers refer to that commit and will drift. Verify before relying on one.

**Before you add or change UI or an interaction** on any client, read `docs/design/PRINCIPLES.md` and the parts of this document your change touches, and run the "UI change self-check" at the end. The PR description names the rules it followed and gives a reason for every deviation (`CLAUDE.md`, "Design spec for UI changes").

## Reading route

This table only points the way. Values and rules live in the sections it names, each in one place. Read `docs/design/PRINCIPLES.md` first, then the rows for what you are building.

| You are building | Rules |
| --- | --- |
| A list of sessions or pending requests (Agents, Inbox, the Assistant home) | 3.4: "Sessions that need the user", "The work-item row", "List controls"; 5.6 |
| A pending request: a question, a plan, a tool approval | 4.8 |
| A page title or a session's details | 5.6 |
| A button or a row of buttons | 1.2 rule 3, 3.7 |
| A chip, a badge or a count | 3.8; principle 4 for what a number counts |
| A card or another surface | 3.3 |
| A page, a grouped page or a row | 3.3, 3.4 |
| A sheet | 3.6 |
| An empty, loading or failed state | 3.9, 3.10 |
| A confirmation or an Undo | 4.4 |
| Words: labels, errors, notifications | 5 |
| A color, font, spacing, radius, size or animation | 1.2 rule 1, 2 |
| An icon or a brand mark | 2.8 |
| A field for a secret | 1.2 rule 4 |
| A key that may be a secret provider's reference | 1.2 rule 4 |
| A new setting | principles 2 and 3 |
| Any UI change | 1.4 |

## Product design principles

The seven product design principles, with their rules, how to check each one and recent cases, live in `docs/design/PRINCIPLES.md`: 1 defaults first, 2 progressive disclosure, 3 user control, 4 consistency first, 5 model and product are separate, 6 rules are checkable, 7 fast, local-first and stateful. They decide product questions before any rule in this document applies. "Principle N" below refers to that numbering. Apple's Human Interface Guidelines are a source for this document, not its authority: what Persona takes from them is rewritten here as Persona rules (sections 2 to 6).

## 1. Purpose, scope and sources of truth

This is the project-level design specification. It describes what the code uses today. The web app is the only client. Where the code and an older doc disagree, the code wins and the disagreement is listed in section 8.

Scope: tokens, components, interaction patterns, content, dark mode, text size and the accessibility baseline. Out of scope: server-rendered copy beyond the voice rules in section 5.

### 1.1 Where each thing lives

| Concern | Where |
| --- | --- |
| Tokens | `web/src/styles.css` (hand-written; nothing generates it) |
| Components | `web/src/ui/atoms.tsx`, `web/src/ui/` |
| Written rules | `docs/web/web.md` |

Relationship to other docs:

- `CLAUDE.md` ("Design spec for UI changes") is the short rule list that agents follow. This document is the longer reference behind it.
- `docs/web/web.md` covers web layout and conventions.
- `docs/SUPER_RULES.md` holds the highest-priority rules, including the product audience foundation and the interaction rules cited in section 4. Changes there need administrator approval.
- `docs/design/DECISIONS.md` is the history of these rules: what was decided, when, and what it replaced. It holds no rules; where it and this document differ, this document wins.

### 1.2 Rules

1. UI code uses only tokens (section 2): font level, spacing, radius, size, motion, role colors and icons. A plain pixel value is for lines of 4 px or less.
2. Never restyle an existing screen directly. A new look is a new style or theme color. A change that brings a screen in line with this document is a fix, not a new look; when it changes what a primary screen shows, it is a large change (section 1.4).
3. Button text never wraps (decided 2026-11-10). A button label stays on one line at every text size. When a label would be truncated, work through these in order: (a) shorten the copy, in every language; (b) ask whether a button is the right control here, since long or user-supplied text is content and belongs in a row, a list or the body, and a set of actions that doesn't fit goes into a menu (a decision row's actions are not hidden in a menu: see the exception below); (c) stack the whole button group vertically, primary action first, each label still on one line, at any size where a label would not fit its share of the row (decided by measured width, not by text size, #6151). Exception, decided in `docs/design/task-cards.md` P1 (#5569): on a narrow width the owner-decision row's text takes the full width and its buttons go below it, the secondary buttons together in one row (wrapping onto the next line whole when they do not fit) and the primary button on its own full-width line last, at every text size; there is no Actions menu. Shrinking the text is a last resort for small overflows such as long translations, never the fix for a label that is too long.
4. Use a secret field, not a plain text field, for tokens and keys. A key a secret provider's reference can stand in for uses
   `KeyField`: its **Use a reference** switch stays with that key (at the end of the key's label row, so in a list of
   computers' keys each switch sits with its own), and a reference is typed in plain text, never masked. The client does
   not guess reference mode from an `op://` prefix (`docs/extensions/secret-providers.md` "In the apps").
5. Docs are English only. User-visible strings go through the string catalogs (section 5.5).

### 1.3 Enforcement today

| Test or script | What it guards |
| --- | --- |
| `web/src/ui/theme-contrast.test.ts` | AA 4.5:1 for text tokens on seven backgrounds, both schemes. |

Not enforced anywhere: hit-area size, accessibility labels and shadows (inline literals).

### 1.4 Who looks at a UI change

Every UI change attaches light and dark screenshots (UI change self-check), the evidence principle 6 asks for. Who looks at them depends on the size of the change, because the mistakes in a large change are the kind that an Agent checking its own work tends to miss.

- **Large**: a person looks at the screenshots before merge, the issue's author or a maintainer. The Agent that made the change does not count. A change is large when it changes a default (a grouping, a style, what a primary screen shows first), a page's structure or navigation, what a primary screen contains, how a pending request looks or works, or a rule in this document.
- **Small**: the Agent that made the change may check its own screenshots. A change is small when it brings a screen back in line with this document, changes copy within section 5, fixes tokens or spacing, or builds a secondary page from existing components.
- The PR says which one it is, and why. When unsure, it is large.

## 2. Design tokens

Colors are written light/dark. Sizes are at the default text size.

### 2.1 Color

#### Roles

| Token | Value | Web |
| --- | --- | --- |
| `accent` | from the theme color; blue `#007AFF`/`#0A84FF` | `--accent` `#0285ff`/`#3d9aff` (differs; preset "blue" matches) |
| `danger` | `#D93A3A`/`#FF5F57` | `--red` `#bd3038`/`#ff9499` |
| `danger` wash | `danger` at 12% (the failed and interrupted conclusion cards) | `--danger-wash` = `--red` at 12% (#5569); `--red-tint` stays neutral |
| `positive` | `#248A3D`/`#30D158` | `--green` `#107638`/`#6dd697` |
| `caution` / `cautionText` | `#F08A00`/`#FF9F0A`; `#B35400`/`#FF9F0A` | `--orange` `#a64c00`/`#eea66d` (one token) |
| `ai` | `#5B4FD6`/`#9C94FF` | `--ai` same |
| `countBadge` | `#636366` both modes, white text at 4.5:1 | accent fill (differs) |
| `onSolid` | white | `text-white` |
| `Reply.secondaryInk` | `#636366`/`#AEAEB2`; Markdown H4–H6 in replies (#3863), ≥ 4.5:1 on every page, card and the reply box, where the plain secondary ink is about 3.4:1 | `--ink-2` `#62656b`/`#a5a8ad` (h4) |

`persona` is the persona's own primary (falls back to the accent). `pageBackground`, `card` and `cardStroke` come from the active style.

#### Agent state colors

State colors never follow style.

| State | Dot | Text | Notes |
| --- | --- | --- | --- |
| Needs you | system blue `#007AFF`/`#0A84FF` | `#0064C8`/`#409CFF` | blue since #6231 |
| Working | `#E8890C`/`#FFB224` | `#A35A00`/`#FFB224` | amber |
| Unread / awaiting reply / just finished / finished, not opened | `#30A46C`/`#4CC38A` | `#1D7A4C`/`#4CC38A` | green; awaiting reply, just finished and finished, not opened (#14447) are a plain dot on the web, not Unread's glow |
| Waiting in the background | working dot at 0.6 | secondary | clock mark; its soft fill the working dot at 0.08 |
| Idle / Offline | secondary at 0.55 / 0.3 | secondary | gray |

Needs you is blue (`docs/design/DECISIONS.md`, 11-11); the web still uses amber (`web/src/styles.css:233`), DS-01. Waiting in the background is a count of its own: the quick filters and a heading's state counts (`StateCounts`: a computer's or a space's) leave such an agent out of Working and show it with the clock.

#### Fills, diff, hues, shell neutrals

| Group | Values |
| --- | --- |
| `Fill` | `well` quaternary system fill, `chip` tertiary, `pressed` secondary, `scrim` black 0.45 dark / 0.2 light |
| `Diff` | add `#1D7A4C`/`#4CC38A`, del `#C62B33`/`#FF6369`, changed `#8A6A00`/`#E3B341` |
| `Hue` (categories only) | blue, orange, green, purple, indigo, pink, gray; with red, amber, yellow, teal, also an extension's 11 named colors (UI version 6, `ExtensionUIStyle.color`), on marks only, never text. The one exception is a `scene`'s shapes (UI version 11, #12544): they are content, like a picture's pixels, so the extension colors them (`#RRGGBB`); its pins and every control around the model keep these colors and the app's ink |
| `ModeTone` (permission modes, #3719) | asks first: secondary ink; plans: teal `#0E7C86`/`#4FD1C5`; acts within limits (Auto-Edit, Auto): `Hue.purple`; allows everything: `danger`. None is an agent state's color |
| `Shell` (same in every style) | composer `#FFFFFF`/`#1F1F21`, stroke `#E3E3E3`/`#3A3A3C`, ink `#0D0D0D`/`#F2F2F2`, segment `#E6E6E6`/`#3A3A3C`, drawer `#F9F9F9`/`#171717` |
| `ChatBox` | user `#E3EDFD`/`#1C2F47`, persona `#F0F0F3`/`#2A2A2E` |
| `UsageColor` | input, output, cache write, cache read; color-blind checked |
| `Syntax` | eight colors, each 4.5:1 on `codeBackground` |

#### Theme colors and palettes

Theme colors are style, persona, blue, coral, lavender, teal, lark and extension. The web has no theme color.

| Preset | Accent light/dark | Bubble |
| --- | --- | --- |
| Blue | `#007AFF`/`#0A84FF` | `#DCEAFF`/`#1D3557`, primary text |
| Coral | `#D4683F`/`#CD6E45` | `#B24C25`/`#AF4C28`, white text |
| Lavender | `#7A5AF8`/`#8979EB` | `#6759D3`/`#6756D9`, white text |
| Teal | `#27747F`/`#2A9798` | ink-based |
| Lark | `#3370FF`/`#4C88FF` | `#D1E3FF`/`#133063` |

Readability floors: white on accent 3.5:1, link 3.0:1, bubble text 4.5:1. A persona or extension color that misses a floor is darkened or lightened automatically. Known exception: Lark dark, 3.35:1. Web enforces 4.5:1 instead and uses a darker `--accent-ink` for filled controls.

#### Dark and light

Every role color is a light/dark pair. The user choice is System, Light or Dark. Web uses a class on the root element set before first paint (`web/index.html:31`). See section 6.1.

### 2.2 Typography

Levels.

| Level | Size (pt) | Line height (x size) | Web equivalent |
| --- | --- | --- | --- |
| `display` | 28 | 1.2 | none; 20-28 px literals |
| `title` | 22 | 1.25 | none |
| `heading` | 17 | 1.3 | 13-14 px semibold literals |
| `body` | 17 | 1.45 | `--chat-body` 15 px (16 on phones) |
| `secondary` | 15 | 1.4 | `--chat-secondary` 14 px (15) |
| `caption` | 13 | 1.35 | 11-12.5 px literals |
| `sectionHeader` | 13 | n/a | 10.5 px uppercase in lists |
| `label` | 12 | 1.3 | 11-11.5 px literals |
| `code` | 13 | 1.35 | 12.5 px mono |

Notes:

- Emphasis tokens (`bodyStrong`, `secondaryStrong`, `captionStrong`, `labelStrong` and so on) are the only way to change weight.
- Web's shared task-card type tokens in `web/src/styles.css` are `text-card-title` (14.5 px), `text-meta` (13 px), `text-label` (12 px), and `text-submeta` (11.5 px) for secondary meta. They set font size only and leave line height to the context. Elsewhere, about 3,000 `text-[Npx]` uses remain across 20 sizes, some below 11 px.

Text size behavior:

- The user's Small, Standard or Large setting shifts the size by -1, 0 or +1. Web maps the ratios (12/13, 1, 14/13) through a build-time rewrite.
- Shapes scale with the text size, capped at 1.6x. Touch targets never go below 44 px.

Web Appearance also offers opt-in interface/code font choices (#10243), independent of text size. Reuse settings rows and native selectors; reveal a single installed-family field only for Custom. Both Default values preserve the original stacks, and resets restore each family independently. User choices precede theme defaults while retaining the existing CJK/symbol fallbacks. No remote fonts are added; terminal size and CJK width correction stay separate. Details: `docs/web/web.md`, “Theme color and text size”.

### 2.3 Spacing

| Token | px | Meaning |
| --- | --- | --- |
| `flush` | 0 | parts that touch |
| `xxs` | 2 | |
| `xs` | 4 | `lines`: between lines of one row |
| `sm` | 6 | |
| `s` | 8 | `titleToContent` |
| `m` | 12 | |
| `l` | 16 | `block`, `page` (side margin) |
| `xl` | 24 | `section` |
| `xxl` | 32 | |

`sm` (6) is smaller than `s` (8), which reads backwards (DS-17). Web has no spacing tokens (Tailwind 4 px steps; page margin 20/32 px). Off-grid values in use: card padding default 14 (`CardStyle.padding`, #10233), chip vertical 2.5 (DS-09).

### 2.4 Radius

| Token | px | Use | Web |
| --- | --- | --- | --- |
| `thumbnail` | 3 | file thumbnail | literals |
| `badge` | 6 | badges, small marks | chip 6 |
| `control` | 10 | code blocks, segmented controls | control 8 |
| `card` | 14 | cards, groups | card 10 |
| `bubble` | 20 | message bubbles | 14 literal |
| `panel` | 22 | floating panels | composer 14 |
| `page` | 55 | page pushed aside as a card | none |

Outside the scale: composer 32, sidebar rows 6-10, chart 4. Styles differ in color and surface, not in corners (only preview tiles vary).

### 2.5 Sizes

Sizes scale with the text size.

- Hit target: `tapTarget` 44. A line of text that is a link in a row under a card, where a taller row would push the card's buttons under the composer, grows its touch area `Space.l` above and below, not its layout (#5392). A glyph or a small well that must stay where it is drawn (a field's clear ×, a card's corner ×) grows its touch area `Space.l` on every side (#6577). An outset is hit only inside what holds the control and where no later element takes the touch: check it with a click there, not only with the element's box. Same number under other names: `searchField`, `switchHeight`, `Sidebar.row`, `voiceThreadRoom`.
- Dots 6-10, marks 12-18, icons 20-26, wells 28-38, avatars 40-48. Neighbors differ by 1 to 2 px (DS-10).
- Named by value: `box50` to `box380` (DS-10).
- The conversation kept in view over the pending cards: `voiceThreadRoom` 44 in a voice call, `cardThreadRoom` 72 over a lone question card (section 4.8, #6277).
- Widths: `readable` 560 (web chat column 880, pages 760), sidebar 340/400/480.
- `cardColumn` 330 is the minimum logical column width of an adaptive machine card.
- Sidebar rows: web 32 (touch 44 via a hit-area expander). The web's top-level headings (Pages, Persona chats, Personal Agents, Agents) take `--sidebar-heading`, 24 (touch 44), the height of the tallest control a heading carries, the Agents title row's fold-all button (#10637).

### 2.6 Shadow and elevation

Web has seven shadow tokens (`web/src/styles.css:68-74`) plus `--shadow-knob` for the switch knob. Other shadows are inline literals. See DS-08.

### 2.7 Motion

| Token | Value | Use |
| --- | --- | --- |
| `state` / `standard` | snappy 0.25 s | a state change snaps |
| `layout` | smooth 0.3 s | a layout change glides |
| `expand` | smooth 0.4 s | |
| `reveal` / `conceal` | easeOut 0.28 s / easeIn 0.2 s | `reveal`: what opens in the chat fades in over all its space; `conceal`: a menu or an inbox event fading away. What closes in the chat goes at once, its space with it (#3005) |
| `drawer(velocity:)` | interpolating spring 0.32 s, no bounce | drawer and pager |
| Needs-you breath | 1 s, scale 1.2 | the only attention loop; the dot stays still in quick filters, a heading's counts, badges, a request's card and panel, and sidebar rows |

Web: 150 ms state, 220 ms glide, 300 ms collapse, one easing curve, no springs; the reply bar (#5314, cindy's) stays 250 ms after the pointer leaves and fades over 250 ms (`REPLY_BAR_LINGER_MS`, `REPLY_BAR_FADE_MS` in `web/src/ui/motion.ts`). Web sidebar group folds use `--sidebar-fold-open` / `--sidebar-fold-close` (250 / 200 ms) and `--sidebar-fold-open-ease` / `--sidebar-fold-close-ease`, shared by the clipped, fully visible content and its chevron. When an Agent deletion succeeds, its visible row fades in place and the visible rows below glide into their new positions. The sidebar keeps its scroll position, including when the deleted Agent was open and the workspace returns to Home. Reduce Motion removes the row immediately.


Reduce Motion: web strips all durations to 1 ms but keeps busy spinners turning (`web/src/styles.css:296-306`).

### 2.8 Icons

- One glyph per meaning. Beside text, size an icon with the text's size.
- Web draws about 80 stroke glyphs (`web/src/ui/icons.tsx`, 15 px, stroke 1.9).
- The web sidebar's page icons are the neutral ink: `--ink-3`, the open page's `--ink` on its fill, never a page's own color. **Colored** sidebar icons are an opt-in (Settings › Appearance › Sidebar icons, #10822) that gives each page a named color; Monochrome stays the default, pixel-identical. The map and its contrast: "Sidebar icon colors" below.
- `sideSessions` (#7944) is a session's side sessions: Lucide's gallery-vertical-end on the web.
- Catalog package logos (#4780) replace only the generic extension glyph inside its existing leading tile in the market list, market detail, installed list and installed detail. A shared logo component keeps the tile's geometry and fallback glyph. Loaded artwork has a fixed light neutral backing (`--extension-logo-backing`) so transparent dark marks remain legible in either appearance. Missing, invalid, loading and failed artwork retain the earlier role-tinted generic mark; a changed URL can retry. The image fits without cropping and is decorative beside the package name. Bundled catalog artwork (#5461) is a square PNG inside the versioned extension package; the hub verifies/reads it and returns an authenticated relative `icon_url`. Web uses its selected hub connection to fetch bytes and displays a local Blob URL. Missing, stale or unavailable bundled files keep the same fallback. Legacy absolute HTTPS sources still load anonymously without hub or market credentials. Publisher limits and authenticated image routes are in `docs/extensions/extensions.md` ("Bundled catalog artwork"). Downloads, when supplied, use secondary text (`--extension-meta-size` on web, scaled by the existing text-size transform) and the same GHCR package total across versions on every surface, never an install/user count; unknown counts add no line. This adds package identity to an existing component, not a new style or theme. Other marks, actions and primary screens stay unchanged.

`Theme.Icon.denied` is a tool call the user, a permission rule or a hook turned down (#4303). It is
drawn in gray at the end of the call's row: the call did not run, so it is not an error and never takes the triangle.

### 2.9 Styles and theme colors

Web has one look (the Beautiful style). Style and theme color are independent axes.

## 3. Components

For every component: use the design-system one, not a lookalike. Counts are approximate uses at the snapshot. 

### 3.1 Composer

The web model panel's current Codex row keeps the session's recorded thinking effort, independently of whether it can
change the model. When only effort can change, tapping the row opens its existing setup flyout instead of presenting
a grey, inert choice. Unsupported effort has a visible reason (offline, read-only, parent-controlled, terminal-only,
or unsupported by the model); supported changes say they apply to the next turn. These details belong to the Agent's
on-demand model panel, not the everyday Assistant home (#6887).

- Use: the one message input for Assistant and Agents chats.
- Anatomy: collapsed line `[+] field [mic] [voice/stop]`; expanded card with the field on top and a row `[+] ... [model/effort] [mic] [send]`. Both shapes share one text field so focus and IME text survive the change, and one +, mic, voice and stop (#7278): they sit at the box's bottom corners and move with it between the shapes, in the box's own motion, rather than one copy leaving as another arrives. Only what the card alone has (Hide Keyboard, model, send) fades in and out where it stands.
- Tokens: radius 32, min field height 44. Fill and stroke come from `Shell`.
- States: expanded when focused, non-empty or holding attachments; send hidden when empty and replaced by voice or stop; send gate button when the main conversation is not ready.
- Platforms: web composer (14 px radius, 28 px buttons, `web/src/views/chat/Composer.tsx:648`).
- When an external terminal or Codex Desktop owns the conversation and the web can only show its history, a quiet
  inset notice occupies the composer slot. It names where the person can continue. Codex Desktop offers **Open in
  Codex** when the native thread ID is available. For a released native Desktop thread the same notice offers
  **Continue in Persona**, opening its composer only after the native server confirms the original thread. Use the
  same spacing, colors and wrapping rules in both themes.
- Text growth (owner-requested correction, #5645 / #5648): grow with the draft to six visible lines,
  then scroll inside the same field. Keep the caret visible when typing or moving it; unrelated updates never take
  over a manual draft scroll. Web reserves room for its toolbar in exceptionally short visible viewports (35% input
  share); that safety cap is not the old 22% cap that stopped an ordinary keyboard-sized phone at three or four lines.
  The existing phone-only full-screen editors are unchanged; their control colors and text placement were inspected,
  not redesigned in this correction.
- Web bottom row (#5324, cindy's toolbar): `[+][permission picker] … [model pill][voice][send / queue / stop]`.
  The + and the picker are bare 30 px round
  controls that show the model pill's frame (`line-strong` border, `hover` fill) on hover or while open; the action is one
  dark 28 px circle (`ink` fill, `surface` icon, 40% when it can't be pressed): a filled plane for Send (**Queue message**
  while the Agent works or needs you), a 10 px square for Stop (never red), round voice in an idle empty box, the main
  conversation's not-ready button as a pill in the same fill. A draft uses the round action for sending or queueing.
  An empty composer uses it for Stop while a foreground turn can be interrupted. Typing @ or / opens their menus.
  The model pill is the `trailing` slot and the
  picker the `leading` one, in an Agent chat and in a Persona chat (#5033; the new-chat page has no picker). The card is the
  `composer` container whose width sets the pill's and the picker's modes ("Model pill and unified model panel" in 3.8).
- Web + / @ panel (#5324, `ComposerPanel.tsx`): one panel for both entries, 480 px and never wider than the composer
  (full width on phones), 12 px corners, `line` border, raised shadow, at most 400 px tall. From the + it covers the box
  from its bottom left with a pill search box on top. Typing @ anchors it above the box when space permits. A 24 px
  header (12 px medium, `ink-3`) names each populated group. Rows are 36 px tall (44 px on touch), with a 16 px icon,
  a 14 px name and 12 px secondary text. The slash menu's glide fill marks the active row. A quiet status line reports pending data while local options stay usable. An empty search shows "No matching items".
- Web permission picker (#5324, `PermissionPicker.tsx`): the mode's symbol (14 px), its name (13 px, truncates) and a
  chevron; Full access tints the words `red`, Auto-review `mode-acts`; under a 300 px composer (a 320 px phone) only
  the symbol. A mode not reported yet is named by no value: the raised hand and "Permission mode", no tooltip (#5756). Its menu is the shared `Menu` (300 px) with each mode's symbol, name and check; descriptions are tooltips
  (`MenuItem.tip`), except a disabled row's reason and a row asking again, which stay under the name.
- Permission mode names on every client (#10363) come from `spec/fixtures/permission-modes/names.json`: Default
  permissions, Auto accept edits, Plan mode, Auto-review and Full access in en, zh-Hans and ja. Pickers, settings,
  planned tasks, answered-plan labels and confirmations use those names; approval-action phrases, launcher-default
  choices and unset/default-behavior labels stay distinct. The fixture README names each client's drift check.
- Web under the box (#5324): the source and folder line at the left (12 px `ink-3`, a folder mark, truncating, full
  values on hover; an ⓘ menu at phone width), the context meter at the right.
- Web `/` palette (#5325, `web/src/views/chat/SlashPalette.tsx`; cindy's slash command palette, followed at the owner's
  request, 2026-11-12): a 320 px panel anchored at the composer's left edge above it (`rounded-xl`, `bg-surface`,
  `shadow-overlay`, 6 px padding, at most 400 px or half the visible height, scrolling), 36 px rows (44 on touch) with
  the command in 14 px `ink` (medium on the focused row, under the gliding `bg-hover` highlight) and its source in 12 px
  `ink-3` on the right (none for Persona's own, the agent's name, an extension's name). The focused command's description
  is a 280 px card (`rounded-xl`, `shadow-overlay`, 14 px padding; title 14 px medium, text 13 px / 1.5 `ink-2`, what it
  types in `ink-3`) 8 px beside the panel, right or left, or a line under the rows when neither side has room. Empty:
  **No matching commands**, 40 px, centered, `ink-3`. No key hint line. Ranking, keys and deviations: `docs/web/web.md`
  ("Input (Prompt Bar)").
- Web skill selection (#11247, #12527): the Assistant and Agent composers use the same up-to-480 px menu for `@` and keyword-filtered `/<keyword>` queries; bare `/` stays command-only without dumping the skill list.
  Group headings, scrolling, the glide highlight, skill rows and the detail card come from the same picker. All enabled
  skills in the scoped catalog can be browsed in `@` or searched via `/<keyword>`. The slash menu keeps up to 25 ranked commands first.
  A skill row shows its name and short description. The focused skill's detail shows the full description and the source
  supplied by the node, including any visible directory. It fits beside the list where space permits and scrolls beneath
  the list on a phone. The list fits the space around the composer and the keyboard. Typed @ selections keep the
  chosen label at the caret in the draft and in the sent sentence. Removing the label removes its reference.
  Explicit attachments through the + menu and slash-selected skills can still use context chips.
  Opening either picker refreshes the inventory and can display the composer's last same-scope result
  while waiting. Fresh results preserve the selected identity while it is available.
- Editing state (web, #4503): editing the last message puts its words and quotes back into the box under a one-line bar at the box's top, above the context chips and quote bars. The bar has an accent pencil, **Editing last message** (`ink-2`, medium), a truncating hint (`ink-3`, precise pointers only) and a × with the quote bar's 24 px look. It is the only sign of the state, and the text field stays the same field. The irreversible step (the rewind) waits for an explicit send, which the bar's hint names. Esc or × leaves with nothing changed (`docs/web/web.md` "Pages", **Edit last message**).
- Queued messages (web, #5327): drawn as cindy's pending queue at the owner's request (where cindy and the older design
  differ, cindy wins for this area). They sit in the composer's `top` slot, first in the card, flush with its border and
  edges, with a `line` hairline between them and the input; the card's own border and focus ring hold both. List: at
  most 196 px, then it scrolls; padding 4 px sides, 6 px top. Row: 8 px radius, 2 px vertical padding, 6 px gap, one
  line of 13 px / 1.25 text, `ink-2` at rest and `ink` with the `hover` fill while hovered or focused; no visible
  number (labels carry the index). Left, a 20 px drag handle (`grip` icon, 12 px, `ink-3`) shown with the row's actions;
  right, 20 px round buttons with a `line` ring on `surface` (↑ Send now 12 px, pencil and trash 11 px), shown on hover
  or focus and always on a screen without hover, a spinner in their place while the row's request is out. Send now acts
  at one press (4.4), its tooltip saying what happens. Messages from
  other conversations come last, folded into one 32 px line: a `chat` mark in the rows' 20 px handle column, **N messages
  from other chats** (12 px, `ink-2`, medium) and "· Arrives when this chat is free" (`ink-3`), a chevron at the end.
  Opened, each row names the chat it came from once (medium, `ink`, at most 40%; the hub's 「来自「…」：」 prefix taken off)
  and then its words, with no mark, handle or buttons; a line cut short has a chevron and opens in place to its whole
  text (at most 132 px, then it scrolls, selectable). Only the groups that are there are drawn: with none of the owner's
  rows the relayed line is the card's first thing, with no hairline over it (#9749). Four rows or fewer show in full; from five, three and a centered 12 px **N more** / **Show less**
  with a chevron under a `line` divider. On a phone on its side the queue is one "N queued" line beside the run's status
  that opens the same rows. Spec: `docs/web/web.md` ("Queued messages").
- Duplicates: four composer implementations exist (DS-19).

**Run status bar** (web, #5312; the owner asked on 2026-11-12 that it follow cindy's `RunningStatusBar` and
`RunningTokenRatePopover`, and cindy wins over what this document said before; `web/src/views/chat/RunningStatusBar.tsx`,
`docs/web/web.md` "Live status and streaming"). The active turn's compact work summary and elapsed time appear in the
transcript. Until the transcript has a live summary or send feedback, the run bar above the composer supplies progress.
Background work and model retry messages have their own information in that bar. The row uses 13 px medium type,
8 px side and 6 px vertical padding, with its two sides 12 px apart:
- Left: a 14 px mark and a short state in `--status-accent`, truncating. The sparkle (layers while compacting, activity for
  background subtasks, a 2.5-stroke check for **Done**). An Agent session says what the turn does in cindy's words
  ("Thinking…", "Generating…", "Editing files…", "Searching the web…", "Spawning a subagent…", "Bash running…",
  "Compacting context…", "Working…"); a Persona conversation says it in cindy's plain phases ("Thinking…", "Replying…",
  "Reading files…", "Updating files…", "Searching files…", "Searching the web…", "Reading web pages…", "Organizing the
  conversation…", "Working…") and never names a tool (super rules 0 and 4, section 5.6).
- Model retry: while the screen reports one, the left leads with the existing classifier's plain reason and localized
  `Retrying N/M`; the observed error is secondary detail beneath it. It is never retained from the last status: ordinary
  work, `live: null` and turn end remove it before the usual linger/fade (#887).
- Right, in `--ink-3`, tabular figures: the time in the work summary's format (`0s`, `1m 0s`, `1m 5s`, `1h 0m`; section 4.7),
  then in an Agent session either `· 42 tok/s` or `· ↓ 1.2k tok` (a 13 px arrow mark), never both, and no speed or tokens
  before there is a measurement (principle 5). The time and speed together are a 24 px round button with `hover:bg-hover`
  (a 44 px touch target, `touch-hit`).
- Speed card: 320 px wide (the screen less 16 px each side), 8 px above the button and right-aligned with it, a `rounded-xl`
  `bg-surface` card with the hairline border and `shadow-overlay`, 16 × 12 px padding. "Latest speed" (12 px `--ink-2`, the
  activity mark), the number 28 px medium with "tok/s" 12 px `--ink-2`; beside it a 128 × 48 sparkline in `--ink` (a 12 %
  baseline, an 8 % area, a 1.5 px line and a 2.5 px dot at the latest sample); below, three 12 px columns, label in
  `--ink-2` over a medium value: Turn average, Output, Peak ("—" until measured). Hover shows it after 500 ms as a
  tooltip; a click pins it with a 24 px round ✕ in its top right corner (44 px to a finger); ✕, Esc and a second click
  close it, a click outside does not. While pointed at, focused or pinned the button stays when the speed goes. It opens
  with the cards' `pop-in` and goes with their fade.
- Done: when the turn finished (not waiting on you, stopped or cut off by an error, which keep their last words), the
  linger second shows the check and **Done** with a 0.4 s pop (scale 0.85 → 1.08 → 1, transform only, none with
  reduced motion); then both sides fade out over 0.4 s, except a pinned card's button. The row breathes once (opacity to
  0.45 and back over 1.5 s) when its words or count move.
- Kept from Persona where cindy differs: the background workflow line ("Background workflow · Verify · 169/185+", timed
  from the run's start, the workflow mark in `--ai`), the still waiting line (#6045) and **Disconnected ·** in front of the
  last state. Left out: cindy's **Stop all** for background subtasks (#5044) and its host-measured speed (Persona measures
  from the status line's count instead, `web/src/lib/token-rate.ts`). New mark: `activity` (Lucide's activity, as cindy's).

### 3.2 Message bubbles

- User bubble: trailing, radius 20, padding 16 x 12, `ChatBox.user` fill in the unified layout, otherwise the theme's bubble gradient.
- Assistant text: unboxed in Agents chats; boxed in `ChatBox.persona` in Assistant chats. Streaming text re-parses only the tail.
- Quotes sit above the words and jump to the original on tap.
- Your own long message folds (#5676; iOS `BubbleFold`, web `BubbleText`): past 14 lines as drawn it shows 10, the last
  line fading, with Show More / Show Less (展开 / 收起, 展開 / 折りたたむ). iOS puts it inside the bubble at its bottom, in
  the bubble's link color (`FoldToggle`), folds past 9 lines to 5 at the accessibility text sizes, opens a folded bubble
  on a long press too (section 4.2), keeps open or not per message, and holds the bubble's top in place while it opens
  or folds. What opens fades in under the lines already shown with `reveal`, as a tool's details do (`.unfolding`,
  #6481); the fold's fade mask grows open with it. The lifted copy is always its whole height, also after Show More
  (#6481). The web puts the button under the bubble and its fold scrolls inside; it has no long press to open and
  does not keep open or not (noted in #5676 for a web issue).
- Everything in the user's bubble takes its colors from the bubble as drawn (`Theme.UserBubbleInk`, #5204): the
  unified layout's box (primary words) or the theme's bubble, never the style alone. Blocks at its top (quotes, forwards,
  text to change, comment blocks) draw their rule, heading and words in the bubble's words color, on the accent's tint
  in a pale bubble or the box, and on a 12% black shade in a colored bubble: a veil of white words would lighten what
  they sit on. Floors (`StyleSpecTests.whatSitsInTheUsersBubble`): the block's words 4.5:1 on its fill over the
  bubble's top and deepened bottom, linked paths 3:1.
- Forwards (#3807) use the quote card with the forward arrow before the heading ("Forwarded from Claude · chat"), a line
  for the computer and time, and four lines of the original; a tap opens the original chat at that message. Over the
  composer the same row adds a line saying where it comes from and goes. Forward takes Share's place in the message bar
  (the bar keeps its width); Share as Image is the first row of the Forward To sheet. Web (#3899): Forward takes Hand to
  Persona's place in the message actions; the card over the composer (`ForwardBar`) and in the bubble (`ForwardBlock`)
  are the quote bar's and quote card's shapes (`views/chat/Quote.tsx`) with the `forward` icon, and Forward to is a
  `Dialog` (a bottom sheet on phones) with palette-style rows; no Share as Image row, as the web has no share card.
- Forwarding several at once (#3838): the entry is a row at the top of Forward To, "Select Messages…" (「多选…」), never
  a bar item next to Select Text, and the message bar keeps its width. Selecting in a chat is the Inbox Select's pattern:
  a round mark on each message's leading edge (filled accent when picked), a tap anywhere on the message toggles it, the
  composer gives way to a bar with Cancel, "N Selected" and Forward N (disabled at 0), and Cancel leaves the chat as it
  was. Several forwards of one chat are one row over the composer (a line each, one ×) and one card in the bubble (each
  message's author, time and two lines, each opening it; past three, "N More"). Web (#3838): "Select messages…" is the
  first row of Forward to; while selecting, each message with a Forward has a checkbox (`ui/CheckMark`) in its first
  place, a click anywhere on it toggles it and nothing inside it answers (no hover bar or menus), and the bar in the
  composer's place reads Cancel, "N selected" and Forward N; Esc cancels too. The row over the composer is
  `ForwardGroupBar` and the bubble's card `ForwardGroupBlock` (`views/chat/ForwardGroup.tsx`), past three "N more".
  Android (#5184) opens selection from Forward to. Its Inbox checkmarks take the whole message as a target, and the
  target composer and conversation use the same three-message group preview as the web and iOS.
  Web (#9469): the message menu ends in "Select messages" (「多选」) under a divider with the `check` icon, as iOS; the
  bar reads Cancel, "N selected", the Include questions chip (a round `CheckMark`, the accent tint when on) and Forward,
  Copy and Export image (Export image the primary button); on a phone the three are a card under that line, an icon over
  each word, as the iPhone's long-press bar. A question a picked reply brings has a lighter tick (`bg-accent-tint`).
  Export image is a `Dialog` with its ✕ (a bottom sheet on a phone): the preview, then the switches, then Copy image and
  Download PNG in the footer; the picture's foot is the assistant's face and name with "(Persona)", as iOS's
  `PersonaBrandLockup` `.mark`.
- Share as Image (iOS `MessageShareSheet`): a full-screen page with the message as a card (who said it, where and when, the
  words with code blocks that wrap and have no Copy bar, a quiet Persona mark, on a pale blue-to-lilac gradient), Light /
  Dark at the top, and Share (the picture), Save, Copy and Share Text along the bottom; the card is 390 pt wide, wider for
  tables, code and very tall cards, at most 520 pt (`ShareCardWidth`). Android has no Forward: on Glass the message
  menu's Share opens the page (`MessageSharePage`, Light / Dark in the top bar, the picture drawn from the card's
  `GraphicsLayer`); Brief and Board share the words. What you see is what you share: a reply's cards are drawn as the
  chat draws them, with nothing to use, and a card that only draws live is its fallback, never its JSON (#7261,
  `docs/assistant/assistant-views.md` "Sharing a reply as a picture").
- A message sent from here and not in the transcript yet (iOS `PendingBubble`): drawn as a sent bubble at once, with a
  mark before it, level with its last line: a quiet clock while it sends; a clock while it waits for the network, whose
  small menu is Retry Now and Don't Send, with "Sends when connected" under the bubble; a red mark once it failed, with
  Resend and Delete. Waiting or failed, the bubble is at 0.6. The long press ends in the mark's own word (Don't Send or
  Delete). A failed one with messages sent after it sits before the first of them, at the end of the exchange it was
  written in (`ChatLines.placeFailed`; Android in every style, `placeFailed`). Android draws the bubble so on Glass
  (`OutboxBubble`, #4339 C9) and keeps its status line for a delivery the phone has to check.
- Files sent with a message (iOS `AttachedFileCards`, #8361; web `FileCards`, Android `FileCards.kt`): above your bubble,
  right-aligned, one card each, at most `box280` wide: a video or a recording plays (`AttachedVideoCard`), any other file
  opens the file viewer (`AttachedDocumentCard`: an `avatarL` tile of its kind in the accent's tint, the name in
  `secondaryMedium` truncated in the middle, its size in `label`; Copy Path on a long press). More than four show three and
  an "N More Files" row (`tapTarget` tall) in the accent color that opens the rest in place (Show Less folds them). A file the computer no
  longer has is dimmed to 0.6 with the missing-file mark and "No longer on the computer", and takes no tap.
- The images a turn's work looked at (iOS `ViewedImagesRow`, docs/protocol/protocol.md "Images viewed this turn"): bare
  thumbnails between the work line and the answer, `box64` tall in a strip that scrolls sideways, one alone `box80`, each
  as wide as its picture (¾ to 1.6 of its height), with a small mark for a screenshot or a web page and nothing written
  around them; a tap opens the viewer on the turn's images. Android draws it on Glass (`ViewedImagesRow`, #4339 C9).
- Pictures in replies (#9474; web #9475, Android #9476; iOS `ReplyPictureRun`, rules in PersonaKit `ReplyImages` and
  `ReplyImageLayout`, tokens `Theme.Size.replyPicture` / `replyPictures` / `replyPictureFailed`): a web picture on a
  line of its own, however it sits in its paragraph, is a block picture; block pictures with only line breaks or blank
  lines between them are one run. One picture is `replyPicture` (240 pt) tall and as wide as its shape from 3:4 up to
  the column (wider or narrower ones cut around their centre), never larger than itself; two are squares side by side;
  three or more are squares three to a row (four as 2 × 2), at most two rows, the sixth dimmed with "+N" for those not
  drawn; a run is at most `replyPictures` (480 pt) wide. Same left edge as the text, `Radius.control`, the hairline
  `cardStroke`, `Fill.well` while it loads. The box is its final size before the picture arrives: only a lone
  picture's width changes, so the text below never moves. One that did not load is a `replyPictureFailed` tile with
  the retry mark (in a run, that square), a tap retries; a long press offers Open Original. A picture inside a line of
  text is the line's height. A tap opens the viewer on every picture of the reply. They load through the hub's remote
  image route (`remote_images`, docs/protocol/protocol.md "Remote images (#9473)") at the size drawn; what other
  people wrote (the Inbox's Lark, GitHub and connector threads, `asksBeforePictures()`) keeps each run its size with
  Load Image over it until tapped, since a picture there may be a tracking pixel.
  A machine-local Markdown image uses the session's machine and cwd and the same layout. A local image inside a line
  of text is lifted into its own block so the machine-file loader and any failure are visible. A failed local image
  keeps Retry on its tile and shows the complete reason below the tile or run, including at accessibility text sizes;
  recreating the tile does not retry, and Retry runs only after a tap. The full-screen viewer shows the same reason
  and offers Retry. Local file links open the same file viewer as bare paths, including missing or denied files.
  The web (#9475) draws the same rules: `ui/ReplyImages.tsx` (a chunk of its own with the step that lifts a run out of
  its paragraph, `ui/reply-images-plugin.ts`, loaded with a reply's first picture), rules in `lib/reply-image-layout.ts`;
  a lone picture is 300 px tall (240 px in a narrow window), a run at most 480 px wide, the turn images' 10 px radius
  and `shadow-hairline`, `bg-field` while it loads, sizes as inline styles (no new utility classes on the first
  screen). Markdown rendered with `askImages` (Inbox connector and Lark threads) keeps a run its size with Load image
  over it; a hub without `remote_images` keeps every picture behind a click (#8162's gate), loaded from its host with
  no Referer.
  Android (#9476) uses a 240 dp single and a 480 dp maximum column in Glass, Brief and Board. Inline pictures use the
  measured text line height, including at large system text sizes. A tap opens that reply’s pictures in the existing
  viewer, including inline and overflow pictures. Each third-party thumbnail and previously unopened gallery page
  asks before loading. Mail keeps its Show images action. A pairing that has yet to advertise `remote_images` waits
  for a tap and uses a separate HTTPS reader. WebView link preloading skips picture-bearing replies.
- No shared bubble component exists. Messages, Tasks and Lark rebuild it with different paddings and gutters (DS-20).
- Web: user bubble 14 px radius; replies unboxed. Android: 20 dp in Glass, max width 86%.
- Web reply bar (#5314, `ReplyBar`; the owner's requested alignment with cindy's `MessageActionBar`, 2026-11-12): only
  under a finished turn's last reply, never under narration in a work summary or words still streaming. Copy message ·
  Start a new session ("Start a new chat" in Persona conversations) · ⋯ More message actions (Quote, Forward, Use as
  context) · the relative time (12 px `--ink-3`, the exact time as its tooltip). 24 × 24 round buttons 2 px apart, 14 px
  icons, `--ink-3`, `--ink` on `--hover-2` under the pointer. Shown on hover or keyboard focus, it stays 250 ms after
  the pointer leaves and fades over 250 ms; an open ⋯ keeps it. Touch screens keep the copy + ⋯ row under that reply;
  the right-click / long-press menu keeps every action. A reply cut short by an error or an interruption carries
  cindy's "This reply did not complete" line above it (13 px `--ink-2`). After the time (#5426, `TurnUsage`, cindy's
  usage cell): what the exchange cost, 6 px on, 12 px `--ink-3`, half a pixel down, "Worth $0.12" in an Agent session
  with cindy's usage card (320 px, `--surface`, `--line` border, 12 px radius, `shadow-overlay`, 16 px padding, 12 px
  `--ink-2` lines; under the text from its start, above it when the chat has no room below; 250 ms into a hover, held
  on the card, gone 220 ms after the pointer leaves, pinned by a click, Esc or a press elsewhere closes it, the bar
  stays while it is open), or without a cost "23.4k tokens" with their lines in the dark tooltip (`--tooltip-bg`,
  13 px, at most 420 px, 500 ms into a hover, never pinned by a pointer). A Persona conversation shows only "≈ $0.12"
  and one plain line, and nothing without a cost (super rules 0 and 4). On touch screens the text ends the copy + ⋯
  row: a tap holds the card or the tooltip, and a second tap on the text or a press elsewhere closes it.
  Translate (#8650) loads a hub-paged reply's full text before requesting its translation: the original and its
  full-text loading/retry control stay visible until the complete translation succeeds. Show original restores
  the cached complete reply, not an unmarked prefix; failure to load the full text never sends a partial translation.
  Details: `docs/web/web.md` ("Reply bar").
- Web hover bar beside your own bubble (pointer only: the time, Copy message, Quote, Edit, ⋯): left of the bubble, 6 px
  away, where it fits inside what shows the chat. Where a narrow window leaves a wide bubble no room there, the same
  buttons sit under the bubble, right-aligned, as a small floating bar (`rounded-control`, `bg-surface`,
  `shadow-hairline`) over what comes next; a transparent bridge over the 4 px gap keeps the hover, and hidden it takes
  no clicks. Where it goes is measured when the pointer or the focus comes into the bubble's row, and only its classes
  change, so the button Tab reached keeps the focus (`docs/web/web.md`, "The hover bar beside your bubbles").
- Android Markdown tables in chat and file previews (`ReadableMarkdownTable`): a table that fits the upstream renderer stays unchanged. In a wide one, short columns keep their natural width, long columns wrap between the font-scaled `RichTextTokens` minimum and maximum, and only the table scrolls sideways. Share pictures keep the unchanged static table.
- Web Markdown tables in replies: a column is as narrow as its words. Headers never wrap and have no minimum width; a
  cell whose words fit one line in about 5.5em stays on one line with no minimum; only longer cells keep 7em, so a
  squeezed table never stacks a sentence a word per line. The copy button (hover or keyboard focus; always on a screen
  without hover) sits on a cap as tall as the header row in its `--inset` color, with a short fade on its left, and the
  last header cell keeps room for it, so it never covers header words (`docs/web/web.md`, "User bubbles").
- Model change divider (web, #5034, `ModelChangeDivider`): where an exchange's reply came from another model than the
  previous visible reply, between that exchange's message and its reply: two `--line-strong` hairlines and a centered
  pill chip (`⇄` 12 px, "Opus 5.5 → Sonnet 4.5", 11 px `--ink-3`, `--line-strong` border, `bg-page/50`, padding 10 × 4),
  `role="separator"` with "Model changed from … to …"; not a control. Only the transcript's record of the replying model
  counts, never the picker (`docs/web/web.md`, "Model change divider"). Follows cindy's model-only switch chip at the
  user's request (2026-11-12), with model names instead of raw ids.
  iOS (#5055, `ModelChanges.mark` and `ModelChangeDivider`) applies the same latest-assistant, unknown/synthetic,
  folded-round (open or closed), canonical-name and loaded-page rules after its folded rounds are resolved. Photo-only
  and wake inputs open exchanges too: the divider follows their photos or visible notice, or precedes a wake's first
  work/reply line when no notice is drawn. It draws `switchAgents`,
  `Theme.Font.label`, `Theme.cardStroke` and a capsule at 50% of the page background's opacity in every style, with the stable
  `chat.modelChange` accessibility element and the same spoken sentence; the raw ids stay in its pointer tooltip.
  It changes no model picking or engine switching.
- Engine switch divider (web, #5054, `EngineSwitchDivider`): above the first message a conversation's other engine got
  (Claude Code ↔ Codex), the model change divider's row with a chip "⇄ Switched from Claude Code to Codex · <model> ·
  <badge> ›": the badge "session resumed" when the engine's earlier session was resumed, else "Continued from summary".
  When the switch carried a handoff the chip is a button (tooltip "View handoff context", `aria-expanded`) that opens a
  panel under the line: `rounded-card`, 1 px `--line-strong` border, `bg-inset`, padding 16 × 12, an 11 px medium `--ink-3`
  title, the handoff in 12 / 1.55 monospace `--tool-ink`, at most 16 rem high, scrolling, selectable; opened or not is
  remembered per divider. `role="separator"` is the left hairline, labelled "Switched from … to …". Under 640 px the chip
  reads "Claude Code → Codex · <badge>". That exchange draws no model change divider. Follows cindy's agent switch card
  at the user's request (2026-11-12), with the model's name instead of its raw id and the badge always shown
  (`docs/web/web.md`, "Engine switch divider").
- Mode change line (web `ModeChangeDivider`, iOS `ModeChangeLine`, Android `ModeChangeDivider`, #10297): a permission mode change the hub recorded
  (`docs/protocol/protocol.md` "Session mode changes"), at the end of the exchange it happened in, in an Agent's chat
  only. The compaction divider's look: hairlines either side (web `--line-strong`, iOS `Theme.cardStroke` / separator,
  Android `PersonaTheme.palette.line` / `Tokens.Size.line`) of one line of words (web `text-submeta` `--ink-3`, iOS
  `Theme.Font.label` secondary, Android `PersonaTheme.type.micro` `ink3`), not a control. Words: who (the
  device's name; "The hub"; "A device"), the modes in quotes (zh-Hans and ja 「」, en “ ”), a pick that waits for a
  restart says so, and what became of the restart says why in plain words from the node's reason code; no reason, no
  colon. Hairlines only where the words fit on one line with at least 16 px / `Theme.Space.l` / `Tokens.Space.l` of
  rule each side; otherwise the words alone, wrapped and centered (the owner, 2026-11-18: rules beside the first of
  two lines read as crooked, rules a pixel or two long as dots). The web loads its code and words (`chat-modes`) with
  an Agent's chat, off the first screen. A frozen window shows only changes up to its last shown item; newer changes wait
  with the newer transcript. The record is read when the chat opens and when its mode or waiting restart
  changes, never on a timer, a refresh or the app back in front.

### 3.3 Cards and surfaces

| Component | Use |
| --- | --- |
| `NoticeBanner` | persistent in-page notice, neutral, caution or danger, one action; optional `dismiss` puts the design system's close ✕ at the trailing end (accessibility label Hide, hit as a finger-sized square) for a notice the person may not be able to act on, and its owner decides how long the ✕ is remembered (#12319) |
| `ScreenMobileDataCard` | the live screen's one question on mobile data (#6396): a `.surface` card under the bars, covering none of the picture, no wider than `Size.readable`: the network in its title (mobile data or a personal hotspot), one line, two choices with about how much data an hour each takes (prominent Clearer, plain Data Saver; side by side where they fit, stacked at large text, never wrapped) and ✕. A `NoticeBanner` has one action; this is a choice between two. The web's (#6558, `MobileDataCard`) has the same parts under the page's bar, no wider than 420 px, in the stats panel's surface; it stacks the choices where they don't fit side by side (measured, as `ViewThatFits`), and ⋯ holds the choice, since the web has no Settings row for it |
| `DispatchHandoffCard` | one card per dispatched task in the chat: ↳ well, title, New / Already Active / Resumed badge, last-active time and state, ↗; a failed start says why in red; one tappable element (`dispatch.task.<id>`) |
| `PlanBriefView` | a plan task's brief before Start: who asked (two lines), one summary line, Show All for the goal, criteria, time and limits |
| `CardForm`, `CardFold`, `CardCode`, `CardPlan` | a pending card's form: what it asks in one scroll area over its answers, which stay in view; a long command, terminal text or plan folded to its first lines (fewer when the card is short), the last one fading, Show All N Lines / Show Full Text opening it whole (section 4.8, #6605) |
| `ReadinessCard` | what the main conversation still needs, and coded turn / start remedies; its optional `ReadinessChoice` list is for offered answers, not a new page-specific button stack |
| `ShellQuestionCard` | a failed start's live terminal question: the question and offered choices, neutral tone, terminal output under Details, one busy choice, and a refusal retained beside the question |

A `ReadinessCard` with `choices` puts those choices in a stacking button row, each on one line; cards using the existing action / alternate branch keep their previous layout. Missing or malformed expiry keeps the question open; a valid past `kept_until` hides it.

Web has no shared card outside the admin app.


#### Adaptive card grids

Machine cards use one adaptive row grid. Measure the actual pane's available content width after page and reading insets, never the physical display or a device-model bucket. With minimum column `cardColumn` 330 and gap `Space.m`, the column count is `floor((contentWidth + gap) / (cardColumn + gap))`, clamped to at least one; insufficient space stays one column instead of squeezing a card. Empty slots in a short last row keep their weights. Cards in a row have equal height and their charts align at the bottom. Reordering is a separate concern projection: equal ranks retain source order, so heartbeat and sample changes at the same severity do not move rows or reset reading position.

An extension's dashboard page (web, #9158; UI version 8's `columns` layout, `web/src/views/extensions/ViewBody8.tsx`) is the same kind of grid on the web: the cards are an overview card's surface (`rounded-card`, `bg-surface`, `shadow-card`) with its head inside (the `text-meta` medium title, an optional 14 px icon in its named color before it, the section's link in `text-label` accent with › at its end), `gap-3` between them, and every card in a row as tall as the tallest. The columns follow the page content's width with container queries: one column, two once each card keeps 24rem, three once three do, so 1440 × 900 with the sidebar open has two. 24rem rather than `cardColumn` (21rem): these cards hold lists whose rows carry a value or a badge at their end, and at 21rem a third column cuts their titles short at desktop widths. A card that spans the row takes all columns. Inside a card that asks for it, rows go two a line from 36rem of card (three from 54rem) without hairlines between them, and stack with hairlines below that. Why a grid and not masonry: the reading order stays the narrow screen's order, left to right then down. Android (#9158) draws the same page with the native rule above (`cardColumn` and `Space.m`, at most three columns, a phone one, drawn as the stack): the page measures its pane, a columns view takes the pane's whole width (`JListPage` `fullWidth`), and each row of cards is one item of the page's lazy list, every card the section's group panel (`JGroupItem`) with its head inside (an icon in its named color, the `headline` title, the link a quiet `JButton`) as tall as the row's tallest. A section's rows go side by side where each keeps `cardColumn`.


An extension's gauge tile (web, #9337; UI version 9's `Tile.gauge`, `web/src/views/extensions/ViewBody9.tsx`) is a grid tile whose icon becomes a ring, for a quota or a usage: 96 px across on a page (`size-24`) and 80 px in a card (`size-20`), the track the named color's 14% tint (`hueTint`), the arc in the named color (`hue`) from the top clockwise with round ends, easing to a new ratio and snapping with Reduce Motion. The tile's value sits in the ring's centre in `text-xl` semibold tabular figures (`text-lg` in a card) with the unit under it in `text-submeta` `ink-3`; the title (`text-meta` medium) and subtitle (`text-submeta`) are centred below. The tile's surface, badge, press, switch and the grid's columns are a tile's as ever. A tile, not a new component, so the grid, its counting and badges are reused and an older app draws the same tile with the value and the percentage; a ring rather than the `progress` bar, because limits side by side read at a glance as shapes of one size with the amount in the middle, which bars cannot do in a tile's width. The color is the extension's named color (green while there is enough and amber once low is the expected use), never a hex. It reads out as "title: value unit, N%", a meter, or a button when the tile has an action.

An extension's graph (UI version 10's `graph`, #9727; `docs/extensions/extensions.md`, "UI version 10") is a canvas of nodes joined by edges, for work split into parts and plans whose steps wait for others. Each node is one box of the graph node token's size (about 224 × 64 at the default text size, growing with the text, never with the content): a card surface (`surface`) with the card radius and hairline border; the icon at the start in the quiet ink (the node's color when it has one); the title one line in the body's medium weight; the subtitle one line in the meta size and quiet ink; the badge as a chip at the end of the title line; progress as a 3-tall bar along the foot in the accent (the node's color when it has one). The node's `tone` (role color) or `color` (named color) is a 3-wide bar on its leading edge, never its text and never a fill. A selected node has a 2-wide accent ring. Edges are 1-wide curves in the hairline ink; a group's frame is a `Fill` tertiary rounded rectangle behind its members with its title above in the caption style. The fold control is a 24-high capsule (a circle for the chevron, widening for `+N`) on the children side (44 px hit target): a chevron, or `+N` once folded. The canvas is the page's background with no grid; its floating buttons (Zoom out, Zoom in, Fit, Full screen) are the design system's icon buttons in one glass/surface group at the bottom end. Layout is in slots shared by every client (`spec/fixtures/graph-layout/`), so the same graph has the same shape everywhere; only the node pitch is each client's token. A canvas, not a longer list: a list shows the parts but not the shape, and a 300-part tree is a long scroll; native drawing, not an extension's picture or HTML, so nodes can be pressed, read aloud and drawn in every style. On the web, a page, Full screen and a card's Full screen additionally carry the control-room chrome around that canvas, while a compact card preview stays a still 200 px thumbnail: a collapsible outline of the visible nodes down the leading side, an in-place node inspector beside the canvas (facts, detail, links opened in the side panel), and a collapsible parallel wall-clock timeline beneath it, all in the design system's surfaces, rows and tokens. A wide run of sibling leaves packs several to a column instead of one long line, and a graph whose fit is below 65% opens at 75%, centred on the selected or first running, caution or danger node, so the opening view stays readable; Fit still goes down to 25%.

An extension's 3D scene (UI version 11's `scene`, #12544; `docs/extensions/extensions.md`, "UI version 11") is an orthographic cutaway model of simple shapes (`box`, `cylinder`, `prism` in metres) with room labels on the floor and device pins where each device stands, for a home whose rooms, lamps and air conditioners are easier to find by place than in a flat device list. An `image` cannot be turned, zoomed or tapped by device, `graph` is 2D node-and-edge layout, and extension HTML runs in a sandboxed web view rather than the app's own chrome and accessibility tree. The web (#12641) follows separately and receives `downgrade11` until implemented.

The web task plan's workspace row (#3659) reuses the assistant form `fieldClass`, native labelled selects/inputs, and
the existing plan Edit dialog. The compact policy choice stays on the plan card because it authorizes where a task
will write; the optional branch and existing checkout picker are contextual fields. A server-backed, live-announced
path preview uses no browser path convention. Settings › Assistant reuses `Group`/`Row` and the manager-scoped
save-confirmation pattern for the default. Task details reuse the same read-only policy/path line. The workspace
controls and the plan Edit dialog load on demand with `lazyPage`, keeping the policy/path summary while the controls'
chunk arrives and the existing loading indicator for Edit. Neither editor adds controls to the cold first-screen
bundle. No new look,
theme, motion, primary navigation, or destructive action is introduced.

**Team panel** (web, #5180, after cindy's Team tab, `OrcaWorkerPanel`; `web/src/views/chat/TeamPanel.tsx`). It is a lead's side panel, opened from the **Team** button in the lead's header (Lucide's users-round, `users`). The button is shown at every width, with the attention dot while a worker needs you and the panel is shut. The panel has three parts:
- the side panel's header, titled Team, with "Workers: N · Running: M";
- a 32 px strip of the running workers in pane order. Each is a 24 px round pill, 11 px, with its `StatusMark` and title. The pill shown is filled `--ink` on `--surface`, as cindy's accent pill is; the others are `bg-field` with the hairline border. The strip scrolls sideways, with ‹ › and faded ends while there is more that way, and arrow keys, Home and End move between pills;
- the shown worker's own conversation, the chat page without its header.

With no worker running, the panel says "Waiting for worker". It opens and closes by itself with cindy's timing (`docs/web/web.md`, "Team"); by hand it opens at every width, and a narrow screen draws it over the page as every side panel. Left out from cindy, because Persona has no such actions on a herdr tab: closing the panel does not end the team, and there is no create, edit or archive on a pill and no dropdown layout.

**Side sessions panel** (web, #7944; `web/src/views/chat/TeamPanel.tsx` `SidesPanel`, `web/src/lib/side-panel.ts`). The Team panel's layout over a session's side sessions (3.4), opened from the **Side sessions** control in the parent's header: the `sideSessions` icon (Lucide's gallery-vertical-end, the web's match for SF Symbol `rectangle.stack`) and how many sides there are, one line in the header's end slot, 28 px tall, 12 px medium tabular figures, the hover fill and the open state as `IconButton`'s, with the attention dot (8 px, `bg-attention`) while a side needs you. It is shown only while the session has sides, and the chat's ⋯ has the same thing as "Side sessions (N)". In zh-Hans the feature is 分支会话 (the owner's word, 2026-11-15), in ja 子セッション; neither is 子任务 / 子 Agent / サブタスク, which name child Agents and tasks. The panel has:
- the side panel's header, titled Side sessions, with the shown side's title under it;
- the Team panel's strip, with the sides flat (sides of sides too): needs you first, then working, then by last activity, newest first, stopped ones last. Each pill is the `StatusMark` and the session's title; its tooltip and spoken name add the plain state word. No ids, paths, engine or model names;
- the shown side's own conversation, the chat page without its header (its composer, its cards and Stop), landed at the message a link asked for.

It never opens by itself: by hand, or for a link to a side (a deep link, a notification, a card in Needs you or the Inbox, a search hit), which opens the parent with that side shown, at any width. With no side left it says "No side sessions". There is no action that starts a side session (the desktop app makes them). A session with both workers and sides shows both controls, each with its own panel. Why the Team panel's layout and not a new one: it is already the web's way to read and answer another session from a session's page, and two layouts for the same job would differ in their keys and their ends (principle 4).

The closeout card (web, #3612; `web/src/views/chat/CloseoutCard.tsx`) is a stopped Agent session's own account of its work, drawn above the transcript and never inside a message: the card surface (`rounded-card`, `bg-surface`, `shadow-card`) at the chat column's width, a head that folds the details (`Collapse`), and label / value rows. It says it is the Agent's own report. A claim the hub checked shows the finding beside it in one of three states that differ in words, mark and color: confirmed (`--green` with the check), contradicted (`--orange` on `--orange-tint` with the warning mark) and not checked (`--ink-3`). Why a new card: no existing surface sets a claim against a finding, and a status tint over the whole card would read as an error, which a closeout is not (`docs/web/web.md`, "Color hierarchy and quiet surfaces"). Its head is one button the height of a tap target at every text size (`tapTarget`, section 6.3), the card's own top and bottom padding inside it. Verified commands named by inline code or recognizable command text add factual command evidence beside the claim, matched only against this closeout exchange's normalized tool calls and results. The evidence shows the actual recorded command and, as recorded, a numeric exit, `is_error`, background or running state, no match, or unavailable evidence; `is_error: false` is not an exit 0 and no match never means the command definitely did not run. Folded records load on demand through the transcript's existing fold reader; a failed or capped read stays unavailable with Retry, not not-found. This adds no green verification badge, model judgment or approval.



The brief box (web, #3822; `web/src/views/assistant/TaskBrief.tsx`) shows what a task is held to. The web draws it on the plan card before **Start** and on the to-do's page above its criteria. It adds no surface: it is the inset well the review checklist already uses (`rounded-control`, `bg-inset`, `shadow-hairline`), with the closeout card's label / value rows. Whose words a line holds is never left to styling: the owner's own are introduced as theirs with the date, work they did not ask for names who started it, and a brief made later says it was reconstructed and labels the hand-over prompt as that. Long text is held to a few lines with **Show all** in place (principle 2). On the plan card the whole brief is folded to who asked and one line that counts the rest, behind that one control, and the card's buttons stay at the bottom edge of the tray it scrolls in: added detail never puts a pending request's answer out of reach (section 4.8). The criteria keep one list, the review checklist's own (met, missing, open, not checked); under it a finished task adds only the criteria a scope change dropped (`--ink-3`, the undo mark, the criterion struck through, the word and the change that dropped it) and what was done beyond the brief. Overdue is a caution chip (`--orange` on `--orange-tint` with the clock) plus a sentence, not a tint over the box.

**Background tasks panel** (web, #5421, the owner's requested alignment with cindy's Background tasks tab, `BackgroundTasksBody`; `web/src/views/chat/BackgroundPanel.tsx`). An Agent's side panel, opened only by hand (the top bar's button, the loop mark, a workflow card, `/workflows`). Under the side panel's header (Background tasks, list mark):
- sections **Running** and **Completed**, 11 px medium headings in `--ink-3`;
- rows of a 20 px round kind mark on the chip fill, a 13 px status mark and the 13 px title, with an 11 px meta line ("status · n/m Agent · duration · tokens · tool uses"); only a failure is red; only rows that open something are buttons;
- the loop as the first Running row (Persona's own; cindy lists none), its Stop confirmed inline;
- a run's detail: Back, the run's mark and name, then the run's card;
- a command's title, status and mono command line share one rounded hover / focus target (#9169). The independent Copy command icon appears on hover, keyboard focus and touch, without opening the output;
- a command's detail opens inside the same panel (`BackgroundCommandDetail.tsx`), reusing the run's Back bar and existing Button / IconButton controls. Status, runtime and start time are label / value rows. Running time updates; an ended command shows its retained result, known exit code and fixed runtime. The full command is wrapped monospace, folded to three lines with **Show full command**; its Copy command icon sits beside it. Output has a label and line count, follows new text until scrolled up, and retains the last good text with Retry on a failed read. An open detail follows the command into Completed. On an older hub / node without retained outcomes, removal from Running says **Not listed as running**, without an inferred outcome or a still-ticking clock. A shell command's or Monitor's row opens this detail even before the hub knows its output file (a Monitor's arrives with its first event, #10852): the rows and command are the same, Output says **No output yet** (**No output** once it ended) and there is no **Open as file**; when the file arrives the detail reads it in place, keeping the focus and the command's fold. A subagent's row opens nothing without an output file, since its work is its own conversation. For a detail opened from the Background tasks list, Back restores list scroll and row focus, and Escape returns to the list first, then closes, except while a menu, dialog or IME owns it. For one opened from Session overview, Back or Escape closes the panel and restores the mounted overview, its scroll and the originating row's focus. **Open as file** remains a secondary action. Why a command-specific detail: the file viewer does not follow running output or preserve the Background tasks list; a generic panel-history stack is unnecessary here.

The status words are the task cards' (`taskStatusWord`), and the count on the top bar's button is the running background tasks and runs (`backgroundCount`), the same number the panel's Running section shows without the loop.

**Hand-off cards** (web, #5096; the owner asked that this part of the main chat follow cindy's `SessionHandoffCard` exactly, which replaces the "Dispatched: <title> · Open" line). Each Agent a dispatch started or a task took over is one full-width card in the chat (`views/assistant/Handoff.tsx`, #5110):
- **Frame:** a bordered `rounded-xl` surface, 16 px padding and 14 px gaps (12 / 12 at phone width), whose border turns `--ink` on hover.
- **Mark:** a 36 px round `--field` well with the ↳ mark (`cornerDownRight`), which inverts on hover.
- **Title line:** the title (14 px semibold, one line) and a 10 px uppercase pill badge: New, Resumed and Continuation are filled `--ink`, Already active is outlined. A plan that carries on an existing to-do says Continuation, including when its Agent fails to start. Resumed means the same Agent was reopened.
- **Second line** (12 px): the history mark, "last active …" (the time alone at phone width), then ` · ` and the Agent's state from `StatusMark` and `lib/state-labels.ts`, in the attention color when it needs the user.
- **End:** ↗ (`arrowUpRight`).
- **Open:** the card opens the Agent where Settings › Agent preferences › "How Agents open" says (#5424): the side panel beside the chat (the default; over the chat on a phone-sized window), or the Agent's page. The card looks the same either way. Before an Agent is up, the card has no ↗ unless the task can be opened, and its second line says the Agent is starting, why it did not start (`--red`), or that the task closed or is gone. On iOS and web, a failure with no readable cause says "Didn't start." and offers details only when the card can open them. If the card cannot open, its failure line wraps so the explanation can be read on the card.

Two departures from cindy, both from Persona's rules:
- cindy shows a short session id on the card. The Assistant's chat keeps ids out of sight (super rule 4, section 5.6), so here it is only in the card's tooltip.
- The state word is added, so an Agent that needs the user is visible without opening anything (section 3.4).

Set to Full page, the Agent's page opened from a card shows cindy's way-back strip above the chat (`views/chat/HandoffBack.tsx`): a 40 px `rounded-xl` bordered row at the chat column's width, ↰ (`cornerUpLeft`) and "Came from <conversation> · click to return", with ✕ to put it away. Returning goes back in the history, and the chat opens where it was left (`lib/handoff.ts`).

No new token. The two icons are added to `ui/icons.tsx` in the existing stroke grammar. iOS and Android draw these cards in the chat, one per Agent, and a card with an Agent opens that Agent's page (#5110).

**Changed-files card** (web, #5157, `web/src/views/chat/TurnChanges.tsx`; cindy's `TurnChangesCard`, at the owner's request, 2026-11-12), Agents only (super rules 0 and 4): the last thing in a finished Agent exchange that edited files.
- **Frame:** a `rounded-xl` `bg-surface` section with a 1 px `--line` border and 4 px above and below.
- **Head** (at least 64 px, 16 px across, 12 px down, 12 px gaps): a 36 px `--field` tile (`rounded-control`) with the 18 px `fileDiff` mark in `--ink-2`; **Changed N files** (14 px medium `--ink`, one line) with `+N -N` under it (12 px mono, `--diff-add-fg` / `--diff-del-fg`, ASCII minus); **Review**, the standard secondary `Button`, on the right.
- **Files** (under a `--line` hairline, 12 px across, 8 px down): 36 px rows (44 on a touch screen), `rounded-control`, `hover:bg-hover`: the 15 px `file` mark in `--ink-2`, the path relative to the session's folder (13 px `--ink`, truncated, the full path as its title) and its own `+N -N` (each only above zero). Three rows, then **Show N more files** ⌄ / **Show less** ⌃ (13 px `--ink-2`, 32 px, hover `--ink`).
- **Counts** are the exchange's own successful edits, the numbers each edit's row shows, counted without drawing a diff; folded work counts by the changes its fold carries, so a reload keeps the card. A fold from an older node carries none, and an exchange whose start is not on the page is missing edits: neither has a card (principle 5: no count that cannot be known).
- **Review** opens the branch's review (commits and uncommitted work); a file row goes to its file there, or opens the file itself when the review does not list it or the file is outside the session's folder.

Deviations from cindy: no Undo / Reapply (Persona cannot apply or revert a turn's patch), the plain `file` mark instead of file-type icons, rows a finger tall on touch screens (6.3), and 评审 for Review in Chinese (the Review panel's word). The `fileDiff` and `alertCircle` marks are added to `ui/icons.tsx` (Lucide's file-diff and circle-alert, as cindy's); no new token.

#### Connected Inbox message cards (#856)

Lark CLI Inbox items with a source AppLink offer **Open in Feishu** (or **Open in Lark**)
while **AI draft reply** remains available. This secondary Button opens the source client at the original
message or topic using the CLI's unmodified link. Missing links have no opening action;
the existing in-app Original action remains available.
For older items without a stored link, the loaded original message supplies the
same action next to the reply-target details; a stored header action is never duplicated.

The web Inbox uses the existing connector conversation for source-provided messages;
legacy collected text is never split into inferred messages. A supported card uses
`bg-surface`, `rounded-card`, the existing stroke icons and the Markdown/link reader.
Its linked subject and description come first; author, repository and original text
are disclosed in details. Long descriptions have a preview, reversible expansion
and a bottom collapse action that restores focus to the expansion control.
Reply targets show a friendly name and a source-backed preview when available;
opaque routing IDs and read diagnostics stay in details. The layout changes no
sending identity or confirmation behavior.

The web Agent conversation header's **Session overview** list button toggles a panel from the right side of the toolbar (`web/src/views/chat/ChatView.tsx`, `web/src/views/chat/SessionOverview.tsx`). The panel uses the app's surface and keeps its own scroll. It sits beside the conversation when there is room and above the transcript in a narrow conversation. The reader can keep it open while working in the chat. The environment and context sections use the current session's metadata. Reported context detail reuses the Session Insights breakdown. Outputs groups distinct background output files as rows that open in the existing file viewer. The subagent row shows running and ended counts and opens the tree on demand. Background activity uses the Background tasks panel's list rows, workflow details, output viewer and loop controls. A detail opened from the overview hides the mounted overview instead of squeezing it; Back or Escape closes the detail and restores the overview's scroll and originating row's focus. While the overview stays open, Cmd+B can close and reopen its detail. Turning the overview off or leaving its conversation closes the dependent detail and removes it from Cmd+B memory, so a later toggle cannot expose a stale origin. List-origin details keep ordinary Cmd+B memory. The finished task disclosure counts distinct terminal notices in the loaded transcript. The overview and its disclosures start closed.

### 3.4 Lists and rows

- Web History (#10240) is an Agents destination for non-running sessions, grouped by local activity day. It shares folded sidebar state and visibility before filtering; day counts equal the displayed filtered rows, including only loaded content-search matches. The primary switch stays Assistant / Agents. Agents + owns New Agent, New terminal and Open terminals; History never starts on a remembered terminal inventory (details in `docs/web/web.md`).

- **Native workspace pins** (#9300): the Agents workspace tree shows a host-pinned workspace with the existing pin mark,
  ordered before its unpinned siblings (newest pin first, then the existing stable order). Its repository moves ahead
  of unpinned repositories. Match the exact machine/source/workspace identity, never the folder or project name; empty
  pinned workspaces and their unloaded, unarchived conversations remain available. Search and state filters still apply
  and explicit folds remain under the user's control. The source app changes these pins; Persona's session/project
  preferences and Assistant directions stay independent (`docs/integrations/paseo.md`, "Pinned workspaces").

- Tiles (an extension's `grid` and `carousel`, `ExtensionTiles.swift`, #827): `Theme.Radius.card` on the chip fill, `Theme.Size.tileMin` / `tileHeight` / `tileNarrow` / `tileRegular`; one that is on takes its named color's tint and a ring of it, the words never take the color.
  A carousel scrolls to its section's edges and is cut there. On a page's list its row spans the section, its first tile at `Space.page`; inside a card (a section of `ExtensionViewStack`, a code page with tabs, a columns card) it takes the card's width, its first tile at `CardStyle.padding` in line with the card's other content, cut at the card's edges and never drawn over them (#10233).

- Page chrome: `.themedPage()`, `.themedGroupedPage()`, `.themedRow()`. Settings uses `SettingsForm` and `SettingsRow`.
- Row height at least 44.
- Chip family: `Chip` (label), `FilterChip` (toggle), `LinkBadge`, `CountBadge`, plus 50+ lookalikes in feature code (DS-21). A compact `Chip` draws its symbol before its words (#6578; before, it dropped it); one that is a button in a row (the minutes' time, a to-do's Got it) takes a 44 pt hit area outside what it draws.
- Status: `StatusDot` (fixed size 9, labelled), `AgentStateBadge`.
- Row actions: a context menu.
  - An outside reporting direction (#8236) has no Persona conversation, so it inherits none of those conversation actions. Its Pinned row keeps the same name, tree counts and running treatment; a complete Agent target opens that Agent, while a targetless row is not a button and has no chevron. Neither outside kind gets a conversation menu or swipe action, and neither fabricates a conversation route from its opaque direction id.
- Web rows: 32 px desktop, 44 px touch.
- Web sidebar hierarchy (#7609): peer section titles such as **Persona chats** and **Agents** start at the same inset. They also stand evenly apart (#10637): each top-level heading's title row is one box, the `sidebar-heading` utility (`--sidebar-heading`, 2.5), with the controls beside its title centred in it and never setting its height, so Pages with only a count, Persona chats with New chat and Agents with its counts, fold all, Filter, ⋯ and + are equally tall, folded or not (`Section`'s `top`; the Agents list's title row, complete and paged). A group's heading inside a list keeps its own row. In a nested list, use the shared `sidebar-indent` utility (`--sidebar-indent`, 12 px) once at each parent boundary. In Persona chats, **Pinned**, **Recent**, and **All Persona chats** sit one step in. Conversation rows and Show more sit another step in under their subgroup. A conversation row there keeps its chat icon at rest and draws its state in that icon's slot: the working arc, or a reply nobody has opened (Unread's glowing dot with the unread title weight, Just finished's plain dot), opened away at once; folded, **Persona chats** and **Recent** count the Unread chats they hide with the Agents list's folded tally, spoken as chats (#13425). The same unit indents children in the sidebar's nested trees. Indent the content wrapper so the right edge stays available for counts and menus. A fold is a button with a chevron and `aria-expanded`. A destination uses a link with `aria-current` when selected. Keep help beside the control in an InfoTip when the scope or count needs explanation.
- Web sidebar groups share visible disclosure chevrons and sticky, interactive headings in every grouping (to-do, computer, repository, project and state). Nested group headings stack below their parents and the measured Persona row; the next sibling replaces the current heading as it scrolls past. The Agents title, grouping segments and quick filters form one sticky toolbar below Persona (at the viewport top on the phone Agents page). One chevron button (`GroupFoldButton`, drawn by `FoldAllButton` in `web/src/ui/FoldAllButton.tsx`) is the list's only Collapse all / Expand all; the list menu does not repeat them, and the keyboard shortcut stays (#6447). Its icon shows the next action like its label: corners pointing in (`foldToggle`) for Collapse all, pointing out (`unfoldToggle`) for Expand all, so a folded list's button reads as "open". It acts on the list under the controls only: the Agents title, grouping segments, quick filters, search and Filter never fold with it. Its action follows what is on screen, so a click always changes something (`agentsFoldAction`): Collapse all while any visible group is open folds every group to its heading, and Expand all when they are all folded opens them; with the list folded under the Agents title (the title's own click), Expand all opens the list and every group in it. The chat-list view, and a list with no groups at all (a grouping still loading, nothing listed), have no headings to fold to, so the button is not shown: folding a flat list's rows would leave a blank under the search that reads as an empty list, and a flat list gets no invented heading (below). A first fix folded the whole block there, taking the controls with it; the owner rejected it. Its spoken label names the next action, and the same words show in a tooltip under it on hover after 300 ms and on keyboard focus (a dark tooltip in the `--tooltip-*` colors, no native `title`), with no second line explaining them: the owner found Collapse all / Expand all needs none. Why: before #6447 it acted on the groups alone, so in the chat-list view and under a folded block a click only flipped its own label. The toolbar stays above the section headings on an opaque theme background, with no fog over the first heading. A heading pinned over its group's rows, the innermost one only, draws the Persona row's divider under it (a 1 px `--line` hairline at the same inset), and no heading does at rest: a row half under a pinned heading on the same background, with no edge, read as text run into it (audit item 51 of the 2026-11-14 web usability audit). A hairline, not a fade, for the same reason the toolbar has no fog. Individual folds remain independently reversible; flat conversation lists have no invented group headings.
- A grouping's top level is one kind of row (#6458): in the web's **By to-do** that is the to-do's row (chevron, mark, title, count, state word, each in the same place), and the groups beside the to-dos (Not on a to-do, Recent (not running)) are rows of that kind, never a heading of another look. Where an Agent came from is its row's source mark (Terminal, Lark, Desktop app), not a group of its own: the Agents opened by hand in a terminal are in Not on a to-do (#6591). A group's state is the most urgent of its sessions', after its count, as a to-do's is; a to-do's word is the one its own page uses (5.6). No grouping nests a location (repository, folder) under a grouping that is not by location. What a group holds, when its name does not say, is an ⓘ beside its title (3.8 "Info tips") and the group's empty state, never a line always shown under it. Rows that fold groups select no text (`select-none`): a drag over them painted the grey `::selection` behind each line.
- **A location grouping on several computers** (#13522; the web's paged By project, as iOS's By Project has had it since #2108): when the list has Agents on more than one computer (the whole list's, so a quick filter moves no level), each computer is a group around its places, in the hub's computer order, its places a level in (`sidebar-indent`). The computer's heading is the complete list's By machine heading in the list's own tokens: a green dot online or a grey one, the name the rest of the app gives the computer, **Offline** or **Not connected yet** beside one that is away (which starts folded), then its needs-you count and its count, the sums of its places'. Its sessions in no place are its own last group, not one group of every computer's. A place under its computer's heading names no computer, and its Agent rows leave the computer out. Two of one computer's places with the same name (ignoring case) each add their parent folder after the name, in the heading's regular weight (`app · ~/Desktop`), as desktop coding-agent apps tell same-named folders apart; nothing is added where the folder is not known. With one computer the places stay the top level.
- **A place heading's +** (#13523, web By project): a heading that stands for a folder on one computer may end with the section's **+** (`SectionAdd`, its label naming the place: "New agent in app · ~/Desktop") that opens the one New agent sheet there, never a second start flow. It is revealed as the heading's ⋯ is (on hover with a pointer and on keyboard focus, always on a touch screen), and the heading's numbers step aside for it instead of being covered (a touch screen keeps its room). No **+** where there is nowhere to start: a group of sessions in no place, a computer that is away, a place whose folder is not known.
- A long list of records on a detail page (a to-do's Decisions, web and iOS, #6447): the first five show, with **Show all N** under them and **Show the first 5** to go back. The header carries the fold-all button with the Agents title row's meaning (the web's chevron icon, corners in to collapse and out to expand, and its name as the tooltip; iOS's Collapse All / Expand All symbols, `Theme.Icon.collapseAll` / `expandAll`): Collapse all while any record is shown folds the list away, Expand all shows it whole. The header's count is unchanged by what is shown. Nothing is stored: each to-do opens at the first five, and recording or revising one shows the whole list so the new one is in view. No button while the list is empty.
- **Inbox matter detail** (#6611; web `InboxDetail.tsx` and `InboxEvent.tsx`): the title's small line says where and who, then the current state, any suggestion, the background and the timeline. The state sentence keeps the processing-conversation link and offers **Say something** when no suggestion is waiting, or **Try again** after a failure; the instruction field reuses the suggestion editor's input and standard small buttons. The timeline starts with the three newest steps and expands on request. A `reported` step always uses the reported icon and is distinguished from program-confirmed steps by the existing neutral **Tag** ("Assistant says") and secondary text, not a new surface, tint or token. Source and Agent states reuse the existing brand marks and shared state words; missing fields keep the old presentation.
- Status sections (web To-dos, #3156): a list grouped by what the user should do next uses folding section titles in a fixed order (what the user has to act on, then the unfinished work, then what just finished; only Shelved and Done start folded, Done last, #5707), one row per piece of work with related items behind an "N related" disclosure, and stale items folded under Shelved with a one-tap Restore instead of being hidden or deleted. On the web, **Parked** is a peer of Needs you and In progress (#9325): it combines explicit external holds with review/merge waits backed only by fresh matching PR facts. The tray, To-dos status list and All board use the same wait classification, one task and one open-count contribution even with multiple PRs or Agents. PR waits keep their links and concise review/check/conflict blockers. A parked tray row uses a neutral clock, not a finished-turn check; an elapsed fallback stays visible until the hub changes its state. The Parked entry appears while it has rows or its tray remains open. Selecting an entry reveals its whole capsule and count in the horizontal scroller, including at the largest text size. Why one group: both are waiting rather than active work; the existing PR predicate and rule still distinguish merge waits from unrelated holds. A task waiting for its supervisor's review has an entry of its own there too, **Persona reviewing** (#8678), shown only while one waits and drawn neutral (grey `ink-3` dot, `ink-2` label, no attention or working colour), because nothing in it is the user's to do; Needs you keeps only what is. A row that is only worth a look (Just finished) has a one-tap **Got it** at the end of its status line, under the time (#6578, iOS #7148; Android: the end of its last line), which files it under Done at once with an Undo toast (4.4, reversible rung); opening the item counts as seeing it, without a word. It is a small capsule, never a line of its own: the row is exactly as tall as without it and the title keeps its room. The web draws it 20 px tall with the check and the word in `--ink-2` on `bg-surface` with `shadow-btn` (hover `bg-hover`), hanging over the 18 px line by negative margins, a 44 px finger target through `touch-hit`; iOS draws it as a compact accent `Chip` with the check, its capsule hanging over the line by `Chip.compactInset`, hit like a row through `tapTargetOutset()`, and keeps the leading swipe action after the full swipe's own. They share one line through xxxLarge, the interface-row text cap (#6813), when they fit whole; on a narrow row with a long status, `ViewThatFits` puts only Got It at the right of the next line, and `MeasuredOnce` keeps row movement from remeasuring the candidates. Its press stays its own: it never opens the item. Why not under the words: a button line made these rows taller than every other and left the room under the time empty. Rules in `docs/assistant/todo-status-sop.md` ("Seen"). Why not the folder groups: they say where work lives, not who has to act. Why unfinished work above Just finished: what still has to be done should not take a scroll and a tap while finished work sits above it.
  - Parked wait metadata (web): render one shared, lazy `TaskWaitInfo` through `ParkedTaskRow` in the Assistant Parked tray, To-dos, All board and task details. The caller still decides membership and preserves owner-decision/live-work precedence; the row adds no grouping or status rule. At narrow widths, wrap the wait target, age, valid last contact and wake on separate readable lines; keep the full request, reason and deadline in expandable details. Say a sent request is not recorded only where one could be (person reply, review/merge, legacy and implicit PR waits); an external event or a date never carries one, so it has no request line (#10527). When external-event evidence carries a hub-verified `interaction_id`, show that exact pending request once through the existing Needs you / `InteractionCard` flow, whether its worker is active, idle or released; an already-associated active card is not duplicated. The interaction's own resolve and explicit defer actions, schedule and risk stay authoritative. A worker shared by several to-dos keeps the existing one-home routing: the session's tied to-do wins, else the first listing; non-owners stay Parked. Once the exact card resolves, its stale `work.interaction_id` session state does not create a synthetic ask, while a different current card remains actionable. A missing, resolved or wrong-worker ID stays an ordinary Parked wait with its original fallback, and target text or a source URL never becomes an owner request. Known wait starts sort oldest first, unknown starts last, with a stable to-do-ID tie-break. A valid sent-contact start reads **Waiting**; a review-time fallback reads **Parked**. A due label is informational: `until` wakes the supervisor, while only an aged valid contact enables the editable local reminder draft; neither asks the owner to act or says work resumed. Copy is local, never a send. Successful copy dismisses the editor; failed copy and text edited during clipboard permission remain recoverable. Display projection: `web/src/lib/task-wait-info.ts`; validation/order helpers: `web/src/lib/task-wait.ts`; UI: `web/src/views/assistant/TaskWaitInfo.tsx`, `web/src/views/assistant/ParkedTaskRow.tsx`.
  - Parked source names (web, #9730): use compact object names (PR/issue number, comment on that number, or matching recorded Lark topic/channel), never numbered source placeholders. Preserve the full source URL for inspection. Explicit wait evidence takes precedence over task PR associations; only a wait without explicit evidence falls back to all eligible merge PRs. Match topic metadata to the source chat/thread before naming it, do not invent a missing name, and render no source for an unusable URL. Use the existing link layout, colors and touch targets; naming adds no fetch or task-state write.
  - Section titles are the Agents list's state headers (`SectionHeader`: the name and a `CountBadge` in the state's tone; `FoldButton` for the folding ones), and the count is the items listed under the title. No separate summary line repeats them (#3712).
  - On the V1 To-dos list (below; iOS, hubs with `todo_view`, from #10465's switch-on) there is no Got it: opening a to-do counts as seeing it; Shelved becomes the Inactive mark inside To do.
  - "N related" sits inside the row it belongs to, under its words; opened, the related rows come under it a level in, with a line down their side.
  - A filter and a sort for such a list go in one button's menu next to the view switch (on the web, next to the search field), the active filter also as a chip that clears it ("List controls" below); the sort orders rows inside each section and never moves a section (To-dos on iOS, Android and the web, #3740). The V1 To-dos list on iOS (below) keeps the filter at the foot toolbar and the sort in ⋯.
- One grouping control per list (#6662): the web sidebar's Agents block shows the same bar (To-do / Machine / Repo / Project / Status), the same quick filters under it and the same title whether its list is paged or complete, so a pin or an unpin changes nothing above the list; a thing has one name in every list (Machine, never Computer beside it; Project is the workspace, Repo the repository). A grouping that repeats a section above it is not offered (the Agents list's By pinned, which the 常驻 rows draw).
- A paged list drawn by group (web paged Agents list, #6348): choosing a grouping in **Browse by** (Computer, Project, Space; Account for personal agents) draws the list as the sidebar's folding sections, one per group, computers in the hub's order and projects and spaces in a fixed name order, with the group's count on its heading and, while any do, how many of its sessions need the user in the attention tone (the Earlier header's badge). The first three start open on their first six sessions in the list's presentation order (the sidebar's stable computer → workspace → tab → pane order, #11801; newest first elsewhere) and **Load more**, a quiet line in the look of the sidebar's "Show N more" (`ShowMoreRow`, #6662), never a button pill. Activity does not choose row placement. Retained rows and admitted identities are sorted together by current structural placement: full and paged reads admit every new transport identity, while deltas admit identities marked created and previously presented identities re-entering a filter. A section that has read all of its current members also admits existing sessions that enter the group, even when its count stays the same. An older hub that omits `added` keeps legacy admission of every new transport identity. Previous positions and transport timestamps do not participate, real machine, workspace, tab and pane changes apply directly, and identical loaded identity sets reproduce their order after a reload (#11801). A project that is checked out in several places lists them under its heading as quiet lines: its remote, then each directory with its computer and its own count; the same lines sit under the project's name in the value pill's menu, and each of its rows says its computer before its title. A space names its computer beside its own name when the list spans several. The sessions without the attribute are one last section (**No folder**, **No space**). Picking a value in the pill narrows the list to that group and draws it flat. Why sections and not only a filter: a grouping control says how the list is arranged ("List controls" below), so choosing one has to rearrange it; a pill alone left the list as it was until a second choice. Why only a few open: an open section keeps its own list up to date, and a hub can have dozens of projects.
- Side sessions live in their parent's chat (#7944, the owner's request of 2026-11-15, replacing the 11-13 rule that drew them under their parent, always open): a session started from another one that stays a session of its own (a Claude desktop app's side chat, /btw question or spawned session, `parent_agent_id`) is never a row or a count of its own in any list, grouping, filter, Pinned, the dock, Needs you or search results. The hub folds it under its topmost listed parent (`folded_as: "side"`, a side of a side under the same row) when hello lists `sessions_side_fold`, and the parent shows and counts in its folded state: it needs you if a side does, works if one works, and is unread if one is unread while it does not work itself (sides only; a Codex child or a worker does not make it unread). The parent's chat reaches its sides from one control ("Side sessions panel", 3.3). Why not keep them in the list: they made a list of conversations the user started into a list of every question asked on the side, and the parent's state already says when one of them needs the user. Without the feature (an older hub) the web keeps the 11-13 arrangement: drawn right under their parent, a level in (`SESSION_LIST_STYLE.nestedRow`), with their own rows and counts.
- Older rows folded by activity (web paged Agents list, #4236): an "Active within" menu beside the list's count (Any time, the default, then 24 hours, 7 days, 30 days) folds the rows last active before the window into one Earlier group at the list's end, folded by default, with how many it holds and how many of them need the user on its header. It is a display choice ("List controls": grouping and display), not a filter: no row leaves the list, no count changes, and Any time undoes it. Why not a filter chip: a filter hides rows, and sessions that need the user must stay visible (below).

#### Long lists on a detail page

A section of a page about one thing (a computer's page) lists its items in place only while there are **three or fewer**. From four, the section folds to one summary row: how many, then the one or two facts that need attention, with a disclosure chevron (#5821, Browser Sign-ins on iOS `BrowserLoginsSection` and the web `BrowserLogins`: "48 sites" over "First expires 11/24/2026 · 2 expired", the expired count in the caution color). The section's own header keeps its title, its count and its main action (Import); nothing else of the list stays on the page.

- **Where the list opens**: iOS pushes a page of its own (`BrowserLoginsListView`, a `List`); the web opens it in place under the summary row (`Collapse`, the list held to 60% of the viewport and scrolling), the machine page's way of showing more (Node health's "Show all checks").
- **The whole list**: a search field that filters by name, applied 150 ms after the last key; an order, by name by default, with the one that matters for the list as the other choice (Expiring first: expired on top, those without a date last); a mark on the items that need attention (an Expired chip in caution, as the brief box's Overdue in 3.3; not red: nothing failed).
- **Row actions**: a swipe and the long press on iOS, a row-end ⋯ menu on the web (and on iOS's in-place rows), never a button on every row. Several at once: Select (iOS edit mode, Select All / Deselect All and "Clear N" in the bottom bar) or checkboxes with a select-all box (web). Every destructive action asks first (4.4, rung 2), naming the item, or how many.
- Why three: the session's file card shows three rows before "Show N more files" (3.3), and on a phone a fourth row with its date already pushes the sections below a screen away. The chat's queued messages show four in full because each is one line without actions.

#### Sessions that need the user

In every grouping a list offers, a session that needs the user is visible without opening anything: in a section at the top, or by the state mark on its row, with a folded group counting it on its header. Grouping by location (computer, repository, worktree, tab) never hides it, and nobody has to pick a computer or a repository first to see their own work.

The web's To-dos page (#11838) files to-dos by their state, so one the sidebar's To-dos badge counts can sit outside Needs you: a to-do no Agent is on, under Other open. Each row the badge counts carries the badge's mark, **Up to you** (`Tag` in the accent tone the badge is drawn in), why it counts in its tooltip and spoken label, and a folded heading over one says how many ("1 up to you"). The marks and the badge come from one hook (`useOwnerNeeds`), so they never disagree. Why a mark and not a section: what the badge counts spreads over several sections (Needs you, Parked, Other open, Just finished, Working for work held for the user's OK), and Other open also holds to-dos it leaves out (a goal its pieces carry). Why not Needs you as the mark's words: that is a section of its own with a narrower rule (principle 4).

A child session (an Agent another Agent started, folded under a listed parent; web, #5120, after cindy) is never a row or a count of its own: its parent's row carries it, in the parent's folded state (a waiting child makes it need you, a working one makes it work), its second line saying what the child waits for. The child's need is visible on that row, counted once under the parent.

A side session folded into its parent (#7944, `parent_agent_id`, with `sessions_side_fold`) is the same: its parent's row carries it in the folded state, and the parent's chat header carries a needs-you dot on its Side sessions control while one waits, so a waiting side is visible without opening anything. An older hub that does not fold sides leaves each a row with its own state mark, counted on its own.

A herdr tab's workers are the same (web, #5180; the owner asked on 2026-11-12 that this follow cindy's Team exactly). In a tab with several Agents the lead is the Agent in its first pane and the tab's other running Agents are its workers, folded under it by the hub. A worker is never a row, dock entry or count of its own; the lead's row carries it as above. The lead's chat shows no strip of pane tabs any more: its workers are in the **Team** side panel (section 3.3). A tab whose first pane is a shell has no lead, and its Agents stay rows of their own.

#### Finished work nobody has seen

With Session Triage on, a turn the triage found done leaves Unread, but while it is new it is not filed with the
seen ones (#9751, `docs/protocol/agent-states.md` ("Just finished")): for three hours, or until it is seen, it has a
By Status section of its own after the work still going (Working, Waiting in the background) and before Waiting,
Muted and Seen, and it keeps its space, repository and worktree out of the idle folds. Its mark is the unread green as
a plain dot, its state word "Just finished" (the To-dos' section word, `StateName`), its second line the triage's
"✓ conclusion". A muted one is plain Seen. Why not Unread: Unread stays what needs a look. Why not plain Seen: work
that ended minutes ago sank under sessions seen days ago. Why after the work still going: as in the To-dos' order
above, what still runs comes before what is done. The web and Android (#9786; there the dot is Unread's, as Awaiting
reply's) have it; Apple and HarmonyOS follow in their own issues and show these rows as Seen until then.

Past those three hours, and for a turn the triage found waiting on something the hub ends itself (a wake, an automatic
retry), the row keeps the same plain dot under its own state word, **Finished, not opened** (#14447,
`docs/protocol/agent-states.md` ("Finished, not opened")), and the same second line, until the owner opens it, marks
it seen, or its host shows it. It is the hub's own Unread rule read before the triage (the host's `done`, `seen_at`
before `updated_at`), so it needs no new field. Like Just finished it keeps its space, repository and worktree out of the
idle folds, but it ranks and counts as Seen: By Status has no section for it, and no number changes meaning. A muted
one, and one a usage limit stopped, stay plain Seen. Why a mark and not Unread or a section: the triage decided these
need no look, and a section without a time limit would bring back the pile the three hours ended; but a row that says
Seen for work nobody opened hid finished work from its owner. The web has it; Apple, Android and HarmonyOS show these
rows as Seen until they follow.

#### The work-item row

The Agents list, the Inbox's Agent section, the Assistant home and history show a session with the same content, in every Agents row style (Avatar, Two Lines, State Cards, Compact List; iOS's ⋯ › Display and Settings › Appearance › Agents List Layout, Android's ⋯ › Display and Settings › Interface style on Glass, where the parts each style draws come from `rowParts` in core/domain, #4338 A9):

- Leading: one source mark, the Agent's brand or a contact's avatar. The row's one state sits on its corner, chosen by the order in section 5.6. iOS (`AgentAvatar`) and Android's Glass row put the mark in a round well with the state's dot on its corner, rimmed apart from the well.
- Line 1: the session's title, with the time on the right, localized ("6 min ago").
- Line 2: when the session needs the user, what it waits for (the plan's title, the command, or the question itself); otherwise its latest line, in the secondary color. For a failed run, what failed, in red.
- Line 3, optional: computer · project, leaving out what the grouping already shows, then the model's short name. iOS's Avatar row and Android's Glass row (#4338 A9) draw it as the source tag, the tab (space › tab outside the tree), the computer only while the list mixes computers, the model, the to-do as an outlined chip and the muted mark; Android's words come from `rowContent` in core/domain, its model's short name from `modelShortName` (the web's `modelChipLabel`; iOS's row still shows the raw string). On the web, an associated terminal tab is secondary metadata too: a localized **Tab {name}** chip in the complete Agents list, and a visible part of the sidebar row's secondary text even when age, source or to-do marks would hide an ordinary hint. When the tab is primary, it takes the title line without a duplicate chip. The localized **Tab {name}** prefix reads `slot_name` only beside a `slot_id` / `tab_id`; without that association it can be a workspace name and never gains the prefix. Existing structural tab fallback text is unchanged. Search reads the same `slot_name`, so a rename changes both recognition and the words that find the row.
- Never in a row: raw model IDs, session IDs or engine badges. At most one count badge.
- Answers in the row: a question's choices, and a tool approval whose full command is shown and whose action is low-risk (Deny, Allow Once). Plans, high-risk actions and Always Allow for This Session open the session, where the context is.
- Views grouped by location draw these rows too (#3753): By Repository's computer, repository and worktree are lines of their own, each worktree's sessions in these rows under its line. The Mac's By Repository cards (frozen) keep their rows (`WorkspaceAgentRow`). On iOS and on Android's Glass style each repository is a card: its header has the folder, the bold name and the chevron, no counts, with its actions in its long press (`RepoCardHeader`, Android `AgentRepoHeader`); a worktree's line has its mark, its name, a Primary chip on the main checkout, the branch when the name is not it or Detached HEAD as a caution, "N agents" or Idle at the end, dimmed while its computer is offline (`RepoWorktreeLine`, Android `AgentWorktreeLine`, #4338 A7). A computer's repositories with nothing going on follow its busy ones under Idle Repositories with their count, one card with a row each: the folder, the name, "N agents · tab names", when it last moved and a chevron, a tap opening its agents under it (`QuietSpaceRow`, Android `AgentIdleRepoRow`). A busy worktree's idle agents fold behind one line, "2 idle · review · research", and once opened "Hide N Idle" after them folds them again (`RepoIdleLine`, Android `AgentRepoIdleLine`, #4338 A8). By Project's spaces fold theirs behind one "N more idle" row the same way: it opens them and, once open, "Hide N Idle" follows them and folds them again (`collapseRow`, Android `AgentIdleRow`'s `open`, Glass).

#### List controls

A list's controls come in four kinds. Each kind looks like itself, so that narrowing a list never looks like leaving it.

| Control | What it decides | Rule |
| --- | --- | --- |
| Scope | which computers or hub | a menu that names what is shown |
| Filter | which states, sources or dimensions show | each active filter shows as a chip that removes it; Reset clears them all. A filter of states that exclude one another, on a list that lands on all of them (iOS To-dos; iOS, hubs with `todo_view`, from #10465's switch-on), is one pop-up that names its choice, with its own ×, and counts in its menu; a list that lands on one section and reaches the rest through the control (the Inbox) draws it as a bar |
| Grouping and display | how rows are grouped, sorted and drawn | a grouping control or the ⋯ menu, never a filter chip |
| Navigation entry, such as personal Agents | opens another page | a row or the sidebar, never chip-shaped |

Search may take query operators and scopes; they live inside the search field.

On the web a list keeps no standing search field of its own beside the app's one search (#6589, the web's Agents list; the native clients are left out, `docs/design/DECISIONS.md` 11-13). On iOS a list may carry the system's search field (`pageSearchable`), scoped to the list, with one row at the end of its results that takes the words to the app's search, never a hand-built field (the To-dos list, 3.4; iOS, hubs with `todo_view`, from #10465's switch-on). On the web: **Search chats** (⌘K) finds its rows with the list's operators (`machine:`, `project:`, `harness:`, `model:`, `state:`, `is:pinned`, `since:`), a match on the row's name first, then its project or computer, then what is inside it, and its **Filter Agents list** (⇧↵) puts the query on the list in place, as a filter chip whose text reopens ⌘K. ⌘K's own **Filter** button, at the end of its field, offers the list's filter choices (computer, project, status, harness, model, Pinned, updated within) and writes each into the query as its operator, the number of operators on it, so a choice stays text to read and change and no hint line has to name the operators. The list's filter button sits on the list's title row, with the list's other menu buttons, and says how many filters are on, as iOS keeps its Agents filter (the computer) in the top bar's list menu with the chosen one a chip over the list.

- **Inbox section bar** (#9053, `docs/assistant/inbox.md` "Section bar"; the To-dos list draws the same states as a Filter at its foot, 3.4, since it lands on all of them): a filter of states, drawn with each client's quick-filter component (iOS `CapsuleFilterBar`, Android `QuickFilterBar`, the web Agents filter bar's look) with Needs you, In progress, FYI, Done and All. Needs you and All always show; the others while they have rows or are chosen; Collapsed stays under All. Needs you carries the `.needs` `CountBadge`, In progress and FYI the neutral one, Done and All none. It opens on Needs you on each entry and is not kept across visits; All is the stacked page as before, the filter's reset. Grouping sits in the bar's trailing menu, not in a row of its own; on Android it is "Group by" in the Inbox's ⋯ sheet, since `QuickFilterBar` has no trailing control and its five choices already run past a phone's width (hidden while In progress or Done is chosen, as the menu is). Why: the stacked page put FYI and Done several screens below a long Needs you; a bar that lands on Needs you keeps what needs the user first and one tap from everything else (3.4 "Sessions that need the user": the Needs you count stays in sight from every choice).


A list row found by what is inside it, not by its own name (a Persona chat found by a message, #6283), keeps its title and puts the sentence that matched in place of its second line, the match marked as search in chat marks it (the accent tint), the line starting a few characters before the match so a narrow row still shows it (each client cuts for its own width, #7045); opening it goes to that message, ringed as a search hit. A row found by its name keeps its usual second line and opens as usual. Local matches show at once; what the server finds merges in, and a short line under the field says when the server's results may be incomplete.

- Section headers (`JSectionHeader`) measure the complete title, count and ordinary actions in one layout pass. Only when they do not fit, the actions move into a `JMenu` and the title wraps as needed so the title and count stay fully visible; fitting layouts stay pixel-identical. The overflow decision is made during measurement, never by switching after a truncated frame. Button labels still never wrap.
- Command rows in the composer's "/" list (`ComposerSuggestions`) measure the complete command and detail. Only when they do not fit, the detail moves below the command and wraps without a line limit; fitting rows stay pixel-identical. Quick-message and @ rows keep their single-line layout (#7322).

### 3.5 Navigation

- Shell (compact width): Assistant and Agents as pages on a strip, a drawer sidebar, no tab bar. Re-tapping Assistant opens its menu. Explicitly selecting Agents opens its list, clearing any retained detail navigation; re-tapping Agents from a detail returns to the list. Re-tapping Agents while already at the list opens the existing list-options menu. A card, notification or session link remains a targeted open and goes directly to that Agent, not through the list. This list-first rule serves people moving between their everyday Assistant conversation and Agent work, without a setting or a new visual style.
- A page's top-leading button follows how the page was reached, not what it is (#8603): a page picked in a sidebar or list is a root and shows the button that opens the sidebar ("Show Sidebar"; the persona's face in a phone's drawer), never a ‹; a page opened from inside another shows ‹, which goes back to it. The same Agent's chat is a root picked in the sidebar and a drill-down opened from a waiting Agent on the main conversation or in Inbox, a task's card, a Brief card, a to-do, a knowledge project or a meeting (#9971, #12006): it never moves the sidebar's selection.
- **Usage details** (#13173, web `UsagePopover`): one card per provider (`border-line`, `rounded-control`, 10 px padding) with the
  provider's name (13 px semibold `--ink`), the account count and the newest sample age (11 px `--ink-3`); every account inside,
  soonest 7d reset first, each with a `5h` and a `7d` row (bar with pace tick, percent, reset text). Under the accounts, **one
  reset timeline per card**: a shared axis (45% of the width for the first day, 55% for days 1-7), a row per account in the
  same order, a teal (`--hue-teal`) 5h capsule over a blue (`--hue-blue`) 7d capsule from the `--ink` now line to the reset, a
  hollow end dot (`--surface` fill) and the time left (9.5 px semibold, white inside the capsule past 80% of the width). A
  window's `protect_percent`, when the hub sends it, adds a 2 px `--orange` notch on its bar, a 9 px "protect N%" flag and a
  hatched `--orange` zone to 100%. Colors and sizes are existing tokens
  (`docs/server/usage-windows.md`, "Provider cards and the reset timeline").
- Web list and detail in one column (the Inbox on a narrow page, web usability audit item 74; `docs/web/web.md` "Pages", **Inbox**): where an opened item takes its list's place, the keyboard goes with it. Opening a row by keyboard puts the focus on the item's title, a heading with `tabIndex=-1` that shows the accent focus ring (the item's pane while it loads), so a screen reader names the item. **Esc** goes back to the list as ‹ does, except over a dialog or a menu, inside a text field, and where the list is beside the item. Back on the list the focus returns to the row the item was opened from; when that row has moved (the item marked done from its own header), it goes to the row now in its place, and the list does not scroll after the item. A wide page, with the list beside the item, leaves the focus where it is both ways. Why a rule: the list's row is hidden with its list, so without it the focus fell to the page's body each way; another list-and-detail page that folds to one column (the To-dos) follows the same rule.
- Web assistant identity appears in navigation whenever the hub supplies a displayable current assistant, including with one installed assistant. The desktop sidebar and phone drawer head show the current assistant's name, first-character avatar with its status dot, and status/computer line, and open the roster menu. Web remembers the last header id and name per hub, including the default assistant's, for display only: a reload or transient roster-read failure keeps that identity instead of flashing the product header, while a genuinely unsupported roster route (404) clears it and returns to the product header. An empty roster or no displayable current identity also restores AppMark, Persona, connection/computer status and the original Appearance/other-hubs/Settings/Disconnect menu. `ASSISTANT_MENU_WIDTH` in `web/src/ui/menu-model.ts` is 264 px for the assistant menu and 220 px for the product menu. With one installed assistant and a roster, Settings' first group keeps "<name>'s settings" and **Switch**, and Settings › Assistants still provides New assistant. Web conversation titles and message identity still follow `multiManager`: with one assistant or no roster they keep their existing product/session titles and messages add no roster identity. Showing this identity does not enable filtering, badges, extra requests, manager headers/query parameters or `needs_you` reads; those remain under `multiManager`. The user's chosen Persona look stays unchanged.
- Assistant switcher (a displayable current assistant identity, fresh or cached): the head of the sidebar is the current assistant (avatar, name, status line, a chevron) and opens the roster: installed assistants as a single-choice list (the default tagged only when more than one assistant is installed, the current one checked, a count on the others, an offline one's whole row faded to 55% and still selectable), the ones still installing, then New assistant and Manage assistants… (`docs/assistant/managers.md`, "The current assistant"). Web: `web/src/views/AssistantSwitcher.tsx`, a `Menu` whose rows take a lead mark, a tag, a count (with the words a screen reader says for it, `badgeLabel`) and a faded state (`dim`); the row's earlier entries (appearance's three rows under a heading, other hubs, Settings, Disconnect) follow under a divider. With one installed assistant, web shows the same head and a one-row roster with no Default tag; New assistant and Manage assistants… remain in the menu, so creation is not confined to Settings. The menu scrolls, and no submenu opens over its last rows, so every row stays reachable at 200% zoom and the largest text size. Settings' first group ("<name>'s settings") titles itself with the same avatar and name, and its **Switch**, a text button at the title's end, opens the same rows (web: `AssistantSwitchButton`, `views/settings/SettingsGroups.tsx`); the rows of that group read and write the current assistant only. Why a new pattern: the assistant is a scope for the whole app, not a page, so it sits above the pages rather than in the assistant page's top bar where the segmented switcher was.


### 3.6 Sheets

- Busy sheets disable interactive dismissal. Detents and drag indicators are inconsistent (DS-22).
- Web: dialog is a centered card, becomes a bottom sheet on phones.

### 3.7 Buttons

- Icon buttons use a 44 px frame around a smaller visual (composer round button, viewer chrome).
- Web buttons are 32 px tall (hit area expanded on touch). The web's touch target is `touch-hit` (an invisible 44 px square centred on the control); a ✕ in a tile's corner (an attachment's remove) uses `touch-hit-corner` instead (#11442), whose area grows out of the corner and only `--hit-in-x` / `--hit-in-y` (6 px by default) into the tile, so the tile's middle keeps its own tap. Icon buttons packed 4 px apart in a row (the Agents title row's Filter, ⋯ and +) take a real 44 px box on coarse pointers (`[@media(pointer:coarse)]:size-11`), as the fold-all button does (#10637), since centred `touch-hit` squares would overlap and the later sibling would take the earlier one's taps (#11511). A row that folds (By to-do's branch rows) is 44 px there, and its chevron, unpositioned, lets its `touch-hit` take the whole row; controls with their own action stay positioned above it. Android: Large 48, Medium 40, Small 32 dp drawn, each taking taps across 48 dp: Compose extends a smaller control's touch target to 48 dp unless a clipping parent cuts it off, so a frame around small controls clips with `clipDrawing`, not `clip` (DS-06; `TouchTargetTest` checks buttons, chips, switches and segments in every style).
- A group of actions has at most one solid primary button. A recommended choice has an outline and a "Recommended" tag, not a second solid button. Red is for failure and destructive actions only.

### 3.7.1 Switches (web)

- One component: `Switch` in `web/src/ui/atoms.tsx`; the assistant pages' `Switch` (`web/src/views/assistant/parts.tsx`) wraps it and only stops the click from reaching the row under it. Pages never draw their own knob and track.
- Look, iOS-style (#3675): 38 x 22 track, `--switch-on` (iOS green, `#34c759` light, `#30d158` dark) or `--switch-off` (translucent grey, 20% light, 32% dark, so the off track shows on every background); an 18 px white knob with `--shadow-knob`, a soft drop shadow with no ring. The knob never takes `shadow-btn`, whose 1 px ring reads as an outline on the track.
- Focus: the `switch` class rounds the focus outline to the pill and offsets it 2 px; it shows on keyboard focus only (`:focus-visible`), never after a click. Forced colors draw the track border and knob in system colors.
- Why green and not the accent: an on/off switch reads as iOS's, and one color on every page; the accent stays for selection and links. Pill-shaped chips that toggle a mode (botmux, find bar's match case) are not switches of this kind.

The web Recordings retention setting (#3780 / #3862) reuses the settings select and numeric input: minutes show
**Forever** by default, or a finite duration with editable days. The finite option names the duration it will save;
returning to Forever sends `0`. Only the hub-confirmed value is shown after a save, and transcript days stay separate.
This is a requested default and interaction change, not a new style or component.

The web Agent naming settings (#4812) reuse Settings › Assistant's Agents group, `Row`, `Switch`, the numeric
settings input and `SettingsHint`. The default corrects only recognizable placeholder or instruction-template
names, once per Agent; periodic updates default off. The per-Agent limit counts successful name changes, including
periodic refreshes. The periodic switch reveals hours (24 when first enabled) and a plain note that refresh needs
new output and remains subject to that limit; a limit of one shows why another refresh cannot happen. Turning off
automatic naming hides the advanced controls without losing their values. Saves use the selected assistant and
show only confirmed values. Native custom titles and manual renames are protected. This is a requested feature,
with no new visual style, component or motion.

For people who manage Agents in herdr, Settings › Agents uses the same `Row` and
`Switch` for **Use the herdr tab name as the session name** (#11576). It is off
by default and uses the hub-confirmed value. When the tab name is primary, the
sidebar and chat header keep the CLI title in the hover text, and search can
show it on the secondary line. The existing Rename dialog uses one **Name**
field for these sessions.

### 3.8 Chips, badges, avatars, marks

`Chip` (look neutral, tint or outline), `CountBadge` (99+ cap), `LinkBadge`, `Monogram`, `ChatAvatar`, `AgentMark`, `BrandMark`, `SessionAvatar`, `ThreadAvatar`. An agent named by its kind reads the same on every client, using the product's own spelling (TRAE CLI, with opencode and pi in lower case): `spec/fixtures/agent-names/names.json`, which the web's `agentName` is tested against (#10042). Chips are text-only and read as their text. Decorative marks are hidden from screen readers when adjacent text names them.

The web has the same `CountBadge` (`web/src/ui/CountBadge.tsx`, #4538) in three tones: `needs` for what waits for the user (for now the web's other Needs-you counts' look, `--attention` on the neutral field as the sidebar's badges; #4653 turns all of them to the Needs-you blue at once), `neutral` for a passive total, `accent` for a selected item or a mention. It shows nothing at zero and "99+" past 99, is 18 px high (16 px `dense`), and its exact number is spoken unless the host control's label already says it (`label={false}`). The Inbox section bar (#9053) uses it first, so it is a module of its own, off the first screen, while only that page draws it; the web's other counts keep their own pills until #4538 moves them.

Apple counts and row marks (#4538): `CountBadge` takes the web's three looks by name. `.needs` (Needs you's text on its soft tint) is for what waits for the user, `.neutral` (white on `Theme.countBadge` gray, the default) for a passive total such as unread messages or items in a group, and `.accent` (white on the accent) for a mention or the selected item. It shows nothing at zero and "99+" past 99 (`CountBadge.text`, which the tab and pane badges that must be plain strings also use); VoiceOver reads the exact number unless the host passes its own `label`. A tab bar's count is the system's: white on red (`Theme.tabBarBadge`) at its item's symbol's top trailing corner, the iPad's tab bar and iPhone Duo's (#4231, `TabBarBadge`); the same number the row and the list's Needs You show, only drawn as a tab bar draws it. On iPhone Duo's tab bar it is `TabBarCount`: `Theme.Size.badge` (18 pt) tall, round for one digit and a capsule for more, its number `Theme.Font.labelHeavy` (12 pt) white, ringed `Theme.Shell.tabBarBadgeRing` (1.5 pt) in the page's color so it stands apart from the chosen item's circle and the symbol, and centred `Theme.Shell.tabBarBadgeInset` (2 pt) inside the top trailing corner of the symbol's 32 pt well (`tabBarBadgeSpot`): its centre is off the circle and a third of it at most lies over the circle's edge. Centred on the corner itself its ring reached into the name of the item above; inside the corner, as before 11-17, it lay over the circle and most of the symbol. A dense row's state at its end is `RowStateMark`: Needs you a still blue dot, Unread a green dot, Working the amber spinner (`WorkingIndicator`), nothing otherwise, used by the Assistant sidebar's rows (the main chat and the Directions) and by All Chats (`AutoConversationsView`). Why: the sidebar drew Unread in the accent, the same blue as Needs you, so the two states looked alike, and its Needs you counts were white on solid blue while every other Needs you count was blue on the tint. The To-dos list's Working mark (iOS, hubs with `todo_view`, from #10465's switch-on) is `WorkingArc` instead, the same amber turning: a Core Animation arc at a text mark's size (`markXS`, scaled) that stops off screen, on another tab and with the scene away (`VisibleWork`) and holds still with Reduce Motion, which the system's mini spinner does neither of, on a list that can hold many working rows (proposal 3.4).


"On another computer" (web, #5421, cindy's `RemoteSignalOverlay`): `AgentMark` `remote` adds one arc and a dot off the mark's upper right, in the mark's text color, overhanging its box without taking room (a 16-unit canvas whose lower-left 12.5 units are the mark). The web chat's top bar draws every Agent's mark with it, since an Agent always runs on one of the user's computers: the mark's hover and accessible name say "Agent runs on {computer}", and while that computer is offline the mark is at 75 % opacity and says it continues once the computer is back.

Web chat top bar (#5421, the owner's requested alignment with cindy's `SessionContentHeader`, 2026-11-12; `docs/web/web.md` "The top bar"): one line. At its start, in order: the pin mark and the loop mark (13 px, `--ink-3`, only when they apply), the Agent's mark (14 px, `remote`), the title (14 px medium, truncating; a double-click edits it in place, Enter or blur saves, Esc cancels), ⋯ (28 px, 15 px icon, `--ink-3`) and the git branch (12 px, `--ink-3`, from 800 px of header width, not interactive). At its end, the side panels' buttons (Background tasks, Team, Side sessions, Files) and the one button to the other view. It shows no state, model, permission mode, subtitle, goal or space button: the state is the running status bar's and the row's, the mode the composer picker's, the goal a bar over the composer. The bar stays 56 px tall (cindy's 46) so it lines up with every other page header and side panel header. A Persona conversation keeps its face before the title and the same order. In a task's or an Agent's side panel (#5424, while Settings › How Agents open says Side panel) the same bar is the panel's only header: the task's title in place of the Agent's (its state and place in the title's tooltip, no in-place rename), a task with several Agents adds ⌄ to switch after the title, ⋯ starts with the panel's own rows (Open in new window, Remove task…), the same space's other Agents are left out, and × at the end closes the panel. Before ×, the chat bubble (`chat`, the icon the agent page's view button shows for Chat; "Open Agent page") opens the Agent shown as its own page with the way-back strip and closes the panel; it is absent while the task has no Agent up.

Assistant avatar: a disc with the first character of the assistant's name. The default assistant's is the AI purple; any other takes one of five hues (blue, green, orange, pink, indigo, in that order) by the sum of its id's UTF-8 bytes modulo 5, so every client colors the same assistant alike. A status mark sits at its foot (online green, offline neutral, installing orange) and, on the sidebar's head only, a mark at its head when another assistant has something waiting. Web: `web/src/ui/AssistantAvatar.tsx` with the `--ai` and `--hue-*` tokens. The letter is sized by the disc, not by the system's text size, so it never outgrows it. Android: `JManagerAvatar` (the status mark's ring is cut out of the disc, so the surface behind shows around it), with `JAssistantSwitcherHeader` and `JAssistantSwitcherRow` for the sidebar's head and the roster's rows (an offline one's row faded with `HomeTokens.assistantOfflineAlpha`), and `JAssistantSettingsHeader` for the head of Settings' first group (the monogram at `HomeTokens.assistantAvatarHeader`, the title, and a Switch text button opening the same roster). Apple: `AssistantMonogram` (`Monogram` drawn on the assistant's hue, its letter in the panel's color and capped at the largest standard text size) with `AssistantStateDot` (ringed in the color it sits on), an offline row faded with `Theme.Fade.offline`; the sidebar's head keeps the Persona face with the same dot, and `AssistantWaitingDot` at its head. Why not the person avatar (`Monogram`, Android `JAvatar`): that one sits on the neutral surface, and here the color is what tells the assistants apart.

The task stage strip (#3380) reuses `TodoStageBar` on the task detail, home card and briefing, and iOS and Android draw it by the same rule (#11590). `stage_kind: general` omits the strip: the existing task status already describes non-code progress. Code tasks keep the six steps and `aria-current="step"`; unknown kinds retain the legacy display. Work closed with nothing left to deploy (`stage_done`, #11590) ends the strip with a Done step after the steps it went through: every dot passed (the done style), none current, the steps it never took left out rather than ticked, and never Verified, which stays the confirmed check of deployed work (#3240). The Done step reuses the passed dot, its connector and the label tokens; it adds no token. `plan` is current only for a pending plan-approval card before review, not guessed from prose or a running state. GitHub links retain `PR #N`; GitLab-style merge request paths on any host read `MR !N`, with the existing link behavior and tokens unchanged. No new setting, visual style or motion is introduced.

On iOS, `TodoStageBar` goes by the measured width, not the text size (the same rule as `StackingButtonRow`): while every step name fits its sixth of the width whole, at the normal type size and never scaled down, the bar is one element saying the stage (#5892). When one does not (HIG-027: names ended in "…" or ran together at the larger sizes), it draws its six dots and their lines without the names. One line under the dots names the current step and its place, "Step 2 of 6 · Develop", in the current name's style, and wraps if it has to; a no-break space before the "·" keeps it at the end of the first line, so the second line never starts with it (in every language). VoiceOver reads each dot in full (its name, its place, and done, current step or still ahead), and the line, which says the same, is hidden from it.

Model configuration reuses settings `Group` / `Row` and neutral `Pill` components for execution-form groups and chips
(`docs/assistant/model-config.md`, "Execution form"). Long explanatory pills opt into `wrap` on narrow rows; other
pills keep their existing single-line layout. A separate, labelled 44 px **?** control expands help inline on
hover, focus or click, with Escape/outside-click dismissal. Why inline: explanations include environment and configuration
details that must remain readable on phones and at larger text sizes; a short tooltip or a nested button would not fit.

The Main-chat agent-kind notice (#4662, `MainChatAgentNotice`) reuses the compaction strip's neutral surface, spacing,
caption text and info icon; its explanation wraps, and the quiet **New main chat…** button stays on one line, opens
the existing dialog and has the existing `touch-hit` 44 px coarse-pointer target without changing its drawn height. It appears only when the loaded launcher list confirms that the current main chat and its configured
default use different agent kinds, with no conversation pick. The Models editor uses the same rule in an inset explanation
that follows the provider draft. Same-kind, unknown and unloaded states add no warning or new action.

Assistant In progress activity marks (#3588) reuse the existing Working spinner (`TaskMark` on web, `JSpinner` on
Android) for active background commands, Monitor, workflows and subagents. A waiting parent is not an idle task.
The subtitle, not another icon vocabulary, explains what is running and its counts/phase; only a real wait with no
background activity keeps the clock. Needs you and completion precedence stay unchanged. The compact Session
projection and cross-platform rules are in `docs/assistant/unified-entry.md`, "Background activity in In progress".
This is a requested status correction, not a new style or setting; the existing reduced-motion behavior applies.
Workflow phase text may ellipsize, but the done/started count stays visible; simultaneous command/subagent counts
use another subtitle line rather than pushing the workflow count out of the row.

Where a message goes in Persona's conversations (iOS `RouteChip` and `TaskMentionList`, web `RouteChip`, Android
`TaskRouteChip`; #4339 C15): while the next message goes straight to a task, one accent-tinted chip sits right above the
composer, under the action or tray bar: the send mark, "Message “task”", and a × of its own read "Hand to Persona" that
gives the message back to Persona. The route never shows only inside a tray. It is behaviour, so every style shows it,
each with its own chip (Android `JChip(accent, onRemove)`). @ lists the conversation's open tasks (not done or failed,
newest first) with their state's dot and where they run, and a pick takes the @word out and sets the same route a task's
Message sets. iOS draws them as a panel of their own above the action bar ("Message a Task", or "No tasks in this chat
yet"); Android puts them first in the composer's one @ list under "Message a task", the files and sessions after them
under "Mention a file or session", the typed word narrowing all of them, so the list takes no more room above the
keyboard than before.

New chat's @ (iOS `StarterComposer` with `MentionSource(machine:)`, Android `starterMentionSource`; #4339 C16, workspaces #2599): a
conversation that doesn't exist yet has no tasks, files or folder, so on iPhone and iPad the list offers Persona's workspaces,
then the agents' sessions on every computer (newest first, at most eight, Persona's own conversations left out) and, once a
typed word is an absolute or `~` path with a slash, that folder alone, read from the assistant's computer: folders first, then
files, names that start with the word first, hidden ones only after a typed dot. A picked folder stays typed with its slash and
the list browses it; a picked file types `@path `; a picked workspace or session waits over the composer as a context card and
goes in front of the first message as a `<persona-context>` tag. While the list is open, what needs the user over the composer
steps aside (iOS `MentionMenuOpen`): above the keyboard the lines, the list and the composer don't fit together. They fade and
fold away as the list comes in and back as it goes, in the state motion (`Tokens.Motion.State`; iOS
`withAnimation(Theme.Motion.standard)`; Android `NeedsStepAside`, the list growing in with the same spring on Glass), and the
dock keeps the page's own backdrop meanwhile, so the things to ask fade out above it instead of being cut at the composer.
Same rows and words as a chat's @ list ("Reading folder…", "Nothing matches in this folder" while browsing); every style draws
it in its own list, since it is behaviour.

A chat's @ list (iOS `ComposerMentionMenu` with `MentionSource(machine:cwd:files:)`, Android `mentionRows`; #4339 C16) reads
its session's computer: after the files its tools used it lists the session folder's own entries (folders first, then files,
hidden ones only after a typed dot, at most twelve, none a used file already is; "Folder" for a folder and "Session folder"
for a file beside the name), then Persona's workspaces on iPhone and iPad, then the other sessions; a word with a slash lists
that folder alone, relative to the session's, and a picked folder keeps the list browsing it. The + menu has the same list's
first step, "Mention a file or session", in every composer that has an @ list: it types the `@` at the caret, as the @ button
beside + does.

Workspace rows (#2599) are the existing resource row with the folder mark, title and a second line computer · shortened folder
(· branch), at most eight after the folder's entries and before sessions. They match title, raw folder, computer or branch,
case-insensitively; title prefixes come first, then available before unavailable, stable in the hub's order. The first row for
one computer and folder wins (trailing `/` ignored), and folderless rows are omitted. Unavailable rows stay pickable. Persona's
conversations and the ordinary new conversation read `GET /api/v1/workspaces` once per opening; Agents, Health, Mac and
visionOS don't offer them. The active hub link holds only its last good in-memory snapshot: later openings show it before
the request answers, a different answer replaces it, and an old hub or unavailable manager keeps it. A hub that has never
answered has none, so only that first opening can shift when the response arrives; there is no prefetch. Removing the hub
group drops the link and its snapshot. A pick takes the @word out
and adds a removable Workspace context using the existing `QuoteBar`; send writes `<persona-context kind="workspace"
machine="…" cwd="…" branch="…">` before the message (`branch` when known), which the manager expands as for the web. No new
component, token, style or setting is introduced. The separate legacy BeautifulUI `PromptComposer` @ menu is unchanged.

Full access by a restart and `/permissions` (#6513, the web's #6476): where the node restarts a terminal Claude Code into Allow Everything / Full access, the mode menus enable that row with the restart note under it (the confirmation repeats it). While the restart waits (`bypass_pending`), the mode buttons keep the mode the agent runs in, so nothing is drawn before it happens, say the session restarts when the turn ends (in the pointer's tooltip and a line over the composer menus' modes), and picking the checked mode sends it without a confirmation so the node calls off the restart directly. `/permissions` in the composer's "/" list opens the mode list with all five modes, nothing sent.

Conversations an extension opened (#3758, `origin` `lark_agent` or `extension`) carry a source mark beside their title wherever they are a row, in the slot and size of the channel mark (web `OriginMark` in `ChannelMark`'s): Lark's `BrandMark` for Lark /agent, the extensions symbol in the secondary color for any other extension. On the web (#10358) a conversation whose opening extension is known (`opened_by`, else `origin_ref`) carries that extension's own manifest icon instead, in the same size and corner as Lark's tile (`OriginMark`'s `session`, drawn by the lazily loaded `OpenerTile` from the installed extensions, the icon asked of the extension's own manager); while the icon loads, and when the extension has no icon the hub serves, is not installed for the conversation's manager or its icon fails, the origin's mark stays. While that icon is drawn, the mark's name and tooltip, the row's tooltip and the line under the opening message say "From <the extension's name>" with the existing "From {name}" words (the name from the installed extensions; none keeps the origin's words), and only then, so the words never disagree with the mark: loading, a fallback or a failed icon keep "From Lark /agent" / "From an extension". The extension the origin's mark already stands for, one that brings a connector or a messaging source of that brand (the Lark extension's `lark`), keeps the origin's mark and words, so Lark /agent is unchanged, while a third-party extension that reuses `lark.agent` is neither drawn nor named as Lark. A message with a channel of its own keeps its channel line. A row has one source mark: the origin's replaces the channel's. No text tag: the row's accessible name carries "From Lark /agent" / "From an extension" instead. The message that opened the conversation gets the "From …" line channel messages already have (the channel line in `SegmentView`), on that first message only and only once the page has loaded back to it; the chat's header stays as it is. Why not a new badge or a pinned line: the owner chose the existing channel patterns so the same place says the same thing for a channel's message and an extension's conversation.

The web main conversation has one compact header line on the welcome page and in the open main chat (#4572).
It uses the same neutral text in both places and reveals the full line on hover, focus or tap when space is tight.
The summary describes the observed harness and execution environment. The new conversation's launcher remains a
separate composer setting. A missing execution report, a proxy node and the last known location each have distinct
copy; none is presented as the current location.

**Model pill and unified model panel** (web, #5032; `docs/web/web.md` "Model pill and model panel"). This part follows
Cindy's model selector at the user's request (2026-11-12): where an earlier rule here differs, this entry wins for the
pill and panel, built from Persona's tokens. Who it serves: people working with coding Agents, and everyone in a Persona
chat (#5033), where the panel only offers models and says plainly which one the chat uses.

- Pill (`web/src/views/chat/ModelPill.tsx`): 30 px tall capsule, 10 px side padding, 4 px gaps, 13 px text
  (`ink`), no frame at rest; hover and open draw a 1 px `--line-strong` border on `--hover`. Contents in order: the
  mark of the Agent the launcher runs (#5497: `AgentMark` at 15 px, `ink-2` for the one-colour marks, 4 px before the
  name; an Agent with no mark of its own gets the generic `agent` icon, never a letter, unless its runtime names it
  (#8142), when it is drawn as the Agents list draws it), the model name (truncates), the
  effort label 4 px after it without a
  tier colour (English "Extra" for "Very high"; the full name on hover), a filled bolt for Fast and the workflow
  `UltraMark` (`ink-2`), a 14 px chevron (`ink-2`). Widths by the composer card (`@container/composer`): ≥ 600 px 72–320 px;
  < 600 px the width its words need (name ≤ 108 px without a trailing bracketed qualifier, a context size kept as a short
  mark, "Opus 5.5 1M"; the whole name in the tooltip;
  shrinking only when the row is short, the name truncating first, the effort only once the name has no room); < 520 px 12 px text, marks a step smaller; < 420 px 64 px, the
  mark and chevron only. States: loading (a 13 px `Spinner` and "Loading model…"), failed (the `warning` icon in `--red`),
  pending (#5035, a switch to another launcher that the next message carries out: a 12 px clock in `ink-3` first, then
  the target engine's mark and the launcher's name in `ink-3` up to 96 px, hidden under 600 px, before the target's model and
  effort; the engine's name in its place when the engine changes; the tooltip "Current: …" over "Next message: …"),
  disabled (50% opacity, no pointer), and a label (not a button) where nothing can be switched. The click keeps the caret
  in the message box.
- Agent launcher label (#10248): when the session reports its launcher, the existing pill says **launcher · model**,
  using the same launcher name as its model-panel row. Both the launcher and abbreviated model stay visible at phone
  width, truncating within the existing capsule; the full names remain in its tooltip and spoken label. The existing
  engine mark stays. A queued switch shows its target launcher with the clock, while the tooltip retains current and
  next setups. An unknown launcher keeps the model-only label and compact fallback. This label correction applies to
  Agents; Persona's pill keeps its existing presentation.
- Panel while a switch waits: one warning line at the top of the list (the panel's `note`: `warning` icon, 11 px,
  `--orange`, wrapping), "Switches on your next message: the Agent restarts on {launcher} and keeps this conversation"
  for another launcher of the same engine, "Switching engines rebuilds context and may lose content" for the other engine
  (#5054); a recorded pick is confirmed by a toast ("Switch scheduled: …"). A launcher switch keeps the conversation and
  its engine, so it asks nothing.
- "Switch Agent?" (web, #5054, `web/src/views/chat/EngineSwitchDialog.tsx`, `docs/web/web.md` "Switching an Agent between
  Claude Code and Codex"): a pick of the other engine's model asks first, unless that engine is already the waiting
  switch's (Cindy's rule). Persona's `Dialog` with no title bar (named by `labelledBy` / `describedBy`), 400 px wide,
  padding 16: the title 18 / 28 medium `ink`, the text 16 / 24 `ink-2` 8 px under it, a "Don't remind me again" checkbox
  (13 px, a 14 px native box in the accent) 16 px under that, the footer 24 px lower, right-aligned with a 10 px gap:
  **Switch Agent** (primary) first, then **Keep Current Agent** (secondary), each at least 96 px and one line. A sheet on
  phones; Escape, the backdrop and Back keep things as they were. The model panel stays open under it (held below the
  backdrop) and closes once the switch is recorded. The pending pill names the engine and the target model with that
  model's own effort.

- Panel (`web/src/ui/model-panel/`): opens above the pill, right edges aligned, 6 px gap, 8 px from the window's edges;
  300–460 px wide with a 428 px scrollable body, search on top and "Configure models" at the bottom.
  One shared column in Agent, handover, Assistant and New Agent pickers (#10249): Follow model configuration where
  supported, up to three distinct saved setups (Favorites first, then Recent), then every launcher in catalog order.
  Only the current launcher starts expanded; with no current launcher all start collapsed. Each launcher summary names
  the launcher, Agent kind, known default model, catalog model count (excluding a synthetic Default row, a model's
  thinking levels counted once), Current or configured Default status, known weekly quota and any supplied failure
  reason; the current launcher's summary drops the "Agent kind · model · N models" line, which the pill, the header and
  its lit row already say, and a folded launcher's reason is an ⓘ with a tooltip, the line only once it is opened
  (#12315). No launcher is hidden in another bucket.
  Disclosure uses the existing chevron, the launcher's Agent mark (#5497), role colors and text tokens.
  Expanded launchers reuse model Row, family folds and Flyout, including Favorite, effort, Fast and engine controls.
  Search is flat: matching launcher summaries, then matching model rows naming their launcher even on touch screens;
  typed-ID rows explicitly name the launcher they send. Why: identical letter tiles and a separate rail hid the account
  and model choices from people comparing several launchers. The single column exposes that information progressively.
  Rows have two lines led by the Agent's mark (16 px, the launcher summaries too; the right side holds only effort and
  Fast) with hover-revealed Favorite and Customize; the model that runs has a static selected fill, no
  check. One row per model: a model listed once per thinking level (Antigravity) is one row named without the level,
  its level at the right side and the levels on the effort control (#12315). A name has a tooltip only when cut
  short. At 768 px and wider the panel is three columns (#12315, `web/src/ui/model-panel/Columns.tsx`): launchers
  (160 px; entries 32 px high with the mark, the launcher's own name, an accent dot on the current one, the quota
  percentage and an ⓘ whose tooltip says why one can't take a pick), the models (single-line rows, a check on the one
  that runs, no effort word), and the thinking efforts of the model picked (156 px; one 32 px button per level, a check
  on the one in use, "Recommended" after the default). The footer gives that model's speed as a two-segment Standard /
  Fast control on `--hover-2` and Claude Code's ultracode where offered. A pick there keeps the panel open; outside or
  Esc closes it. The reason a launcher can't take a pick shows once, above its models, never as a line under each one. Customize opens a 264 px flyout to the panel's left: the effort slider, Fast, the engine, an exact catalog
  reference-price line where known, and a footer that says whether the setup is the recommended one, customized or
  saved. A launcher summary adds its weekly-remaining bar only when the existing launcher quota answer supplies a
  weekly ratio; its tooltip names the window and reset time.
- Provider identity on model rows (#7360): every row in New Agent and in saved setups and flat search results names
  its owning launcher in its model label or at the start of the second line, on web, Android and Apple. The rest of
  that line keeps the model ID, resolved model, description or typed-model explanation; an expanded or filtered
  launcher's own model rows may keep the description alone. A sign-in, key or model-availability error that offers a
  repair names the affected launcher in its title, and the repair opens that launcher's provider settings. This is a
  content rule, not a new badge, token or visual style.
  Preset descriptions and a truncated model's full-ID line retain the launcher name too.
  Launcher summaries retain each provider's full name as their accessible name and tooltip; the former provider rail
  and its shortened captions are replaced by the shared column (#10249).
- Model families (`web/src/lib/model-families.ts`, `docs/web/web.md` "Model pill and model panel"): inside a provider
  group, each family's name is a 11.5 px `text-submeta` line in `ink-3` (12 px side padding, 6 px above, 2 px below)
  over its rows; its right end holds the fold, a text button in the same size and colour ("Earlier versions · N" with
  an 11 px chevron, "Hide earlier" with the chevron turned up), a `--hover-2` fill and `ink` on hover, one line, its
  accessible name naming the family. The newest row says "Latest" after its name in `text-submeta` `ink-3`, no chip.
  Rows outside any family (the Default entry, unknown models) have no header. Why: one family's versions read as one
  choice and the newest is the one most people want, while older ones stay one tap (or a search) away (2026-11-17).
- Usage placement (#5056): the Agent's context meter is a separate, right-aligned row below the composer card, not part of
  the header or the model-pill container. Unknown context mounts no row. Quota bars use the established accent / orange /
  red quota tones (≤ 30% left warning, ≤ 10% or exhausted critical); price marks use neutral `ink-2`, one to three `$` by
  standard output price, and the flyout's values use tabular numerals. Unknown data never creates placeholder marks.
- Header strip (Persona chats, #5033, `PickStrip` in `web/src/views/chat/AssistantModelPanel.tsx`): one line above the
  search, 14 px side and 8 px vertical padding, a `line` rule below, 12 px `ink-2` text that truncates. It says where the
  chat's setup comes from in plain words ("Following the default · …", "Picked for this chat · …", "For this new chat ·
  …") and, when the chat has its own pick, ends with a text button in `accent-ink` (medium weight, one line):
  **Follow default** in the main chat, **Undo pick** in another. Providers that can't run Persona chats on that computer
  are the panel's last groups: the header and the hub's reason, no rows.
- Tokens: the effort levels have one colour each in both themes (`--effort-low` / `--effort-minimal` #2AAE5B,
  `--effort-medium` #14B8A6, `--effort-high` #3B82F6, `--effort-xhigh` #4F46E5, `--effort-max` #8B5CF6, Codex's ultra on
  max) and `--fast` (#3B9EFF), used inside the flyout only: the pill never colours its effort or its bolt.
- Deviations from Cindy: the Agent's mark instead of model-vendor logos (launchers of one Agent share it; #5497); price and quota appear only where the catalog / launcher
  answer supplies them; ↑↓ also move between rows; providers and engines that can't be switched to (an older hub, a chat
  that can't switch) are listed with their rows off and the reason; a pending switch to another launcher of the same
  engine names the launcher rather than the engine; the engine warning line wraps rather than truncating; "Switch Agent?"
  sits high on a computer and is a sheet on a phone, with no close ×.

### 3.9 Empty states

**Web skill ZIP import** (#5478): the advanced Settings › Assistant › Skills group reuses `Group`, `Row`,
`SettingsHint`, `RowFailed`, `Pill`, `Button`, `Dialog` and `Loading`. **Upload ZIP** opens the existing dialog with a
labelled shared Button opening the native file picker. Checking is announced; the preview shows plain-text metadata and a wrapping real path.
Cancel receives focus at preview; closing restores focus to Upload ZIP. The existing dialog owns focus trapping and
Escape. Install/Undo are single-flight and cannot be dismissed halfway through a write; failures are actionable
localized text, never raw archive HTML or filesystem errors. Buttons have a 44 px target; actions wrap on narrow
screens. Semantic tokens supply light/dark colors. All content is English, Simplified Chinese and Japanese.
Explicit review-before-install is intentional even with Undo: uploaded instructions may later influence an assistant,
so selecting a file must not silently change behavior. Success refreshes the list and offers a bounded, unchanged-new-
package-only Undo with retained recovery files; `docs/operations/hub-update.md` defines the state and discovery limits.

**Web to-do images** (#11084): the existing New to-do `Dialog` takes JPEG/PNG images through the shared attachment
picker and `AttachmentTray`; selection shows local thumbnails immediately, with an accessible Remove on each. Create is
one single-flight busy action, with its failure inline in the dialog. Retry keeps every field, selected file and the
operation ID; cancel releases the previews. The to-do detail uses the same tray and existing `ImageViewer` for zoom and
download, not another gallery. Private image bytes are fetched with the to-do's credentials and manager, three at a
time, with localized Retry on failure and object URLs released when replaced or closed. The no-image dialog and detail
keep their previous layout and behavior. When `todo_images` is absent because any manager is too old to persist images,
the picker is hidden and an image-less create is never sent. All controls use the existing 44 px touch expansion and en, zh-Hans and ja
copy. The assistant manager owns persistence and handoff; the Web client does not invent a temporary upload or a local
machine path.

Web has no shared empty state outside admin. An offline current assistant (several assistants) takes the place of a merged list's empty state with its own: the assistant avatar faded with its offline mark, "<name> is offline" and when it last synced (web `OfflineState` in `web/src/views/ManagerParts.tsx`; `docs/assistant/managers.md` "Clients").

### 3.10 Loading and error states

| Case | Component |
| --- | --- |
| List read failure (web) | `ListError` (`web/src/ui/ListError.tsx`): a red `role="alert"` line saying what could not be read and why, with **Retry** under it, in place of the rows; the paged session lists and the sidebar's Agents list (its complete list, and By to-do's unread to-dos) draw it, so a failed read never leaves a list blank or on "Loading…" |
| A short server-driven process (web) | `Steps` (`web/src/ui/Steps.tsx`): one line per step, the current one with a spinner and `aria-current="step"`, done ones ticked green, a stop crossed red, the rest numbered and quiet. The step comes from what the hub pushes, never a local timer. Used by the alert sheet's Redeploy and by **Switch to Persona Link** on the machine detail and the admin Machines row (#10662: confirmation dialog, "Starting…" at once, then the steps, then the result in plain words; nothing shown for an older hub) |
| Chat API failure | Web: `ErrorNotice` (`web/src/ui/ErrorNotice.tsx`), a neutral card with the red mark only and **Error details**; the error the chat stopped on is its banner over the composer with its actions (Retry, Fix it, Edit last message) and ✕, an older one the record in the chat. While a model request is still retrying, the web's run status bar instead shows the plain classified reason, observed error as secondary detail and localized current/max attempt, cleared on recovery, turn end or unwatch (#887) |

**Web complete Agents list loading** (#12231): while the first complete baseline is still being read, an unfiltered grouped sidebar may show the first authoritative summary page as neutral rows under the existing **Loading…** line, once the default grouping's to-dos are available. These rows have no provisional group headings, group counts, Pinned or Recent sections, or empty-filter result; they do not seed filter membership or saved workspace placement. The complete snapshot replaces the preview atomically with the ordinary groups and counts. Filtered and chat-list views, failures, and older hubs' pushed rows keep their existing loading behavior.

Toasts: error, success or info; one slot, so a new toast replaces the old. Loading and error shapes are inconsistent across screens (DS-15, DS-16). Web toasts are dark capsules at the bottom (2.8 / 5.2 / 6 s). On a narrow web screen (#11442) they sit in the part the soft keyboard leaves visible and rise clear of the chat's dock or the open sheet's footer (`--toast-lift`, `web/src/ui/toast-lift.tsx`), so a toast never covers a control; only a mouse pointer holds one, so a tapped toast still runs out, and an action's touch target meets the close button's instead of being covered by it.

**Error notices** (web, #5157, #5313; cindy's `ErrorNotice`, `ErrorBanner` and `ErrorMessageCard`, which the owner asked this part of the chat to follow, 2026-11-12): a model error in the chat, and the subscription usage limit that stands in for one (with the same red mark: cindy shows a usage limit as an error banner), is `ErrorNotice`: a `rounded-xl` card, 1 px `--line` border, `bg-surface`, 12 px padding; a 16 px `alertCircle` mark in `--red`, 2 px down; what happened in 13 / 20 `--ink` (it wraps); what helps in 12 / 20 `--ink-2`; then a wrapping row (8 px above and between) of the caller's buttons, 28 px tall (`Button size="sm"`: a height in `className` does not win over the button's own 32 px) with a 44 px touch target (`touch-hit`), followed by **Error details** / **Hide details** (a quiet button with a 12 px chevron, `aria-expanded`) when there is something to unfold; the details 8 px under it, the raw text in a `bg-inset` well with `rounded-control`, 8 px padding, 12 / 20 mono `--ink-2`, every character kept (`pre-wrap`, `break-all`) and scrolling past 160 px. No red fill and no red ring: red is only the state's mark, per 5.3 and "Color hierarchy and quiet surfaces" in `docs/web/web.md`. The status code and how many times in a row moved from the head into the details, so the head says only what happened and what helps. The card is drawn with 12 px padding, cindy's live banner's, also for an older error: cindy's in-stream record uses 8 px, but here one card shape serves both.

**The error the chat stopped on** (web, #5313; cindy's `ErrorBanner`, the owner's requested alignment, replacing the deviation #5157 recorded): while a chat's last line is a model error (the Persona conversation's last chat line, whatever feed lines follow), not dismissed, with nothing on its way and the Agent not working, it is drawn over the composer as the dock's last item: the same `ErrorNotice` with what can be done now (**Fix it**, **Retry**, **Edit last message**) and a ✕ (`onDismiss`): cindy's 28 px round button in the card's top end corner (`-mt-1 -mr-1`, so it sits on the first line of text), a 14 px x in `--ink-2`, the `hover` fill and `--ink` on hover, the accent focus ring, a 44 px touch target, spoken and hovered as **Dismiss error**. The chat leaves the line out meanwhile, so the error is said once. ✕ (remembered per conversation in this browser) and **Retry** (at once, before "continue" reaches the transcript) put the banner away; the same error again after a retry is a new banner. Dismissed, followed by anything, or while the Agent works, the error is the record in the chat: the same card without Retry, Edit last message or ✕, as cindy's `ErrorMessageCard` has none (a Persona chat's record keeps its Fix it: the hub's word on the turn's error while it is the last line, else what its own text names). A usage limit is the banner too, with ✕ and no Retry. The banner is `role="status"`; ✕ and Retry hand the keyboard back to the composer with a fine pointer only; a new banner is scrolled into view, once, in the box over the composer when that box scrolls; a Retry that did not go brings it back. The host's failure note (`FailureNote`, a botmux turn stopped on an error) and the Persona conversation's refused-send card (`FailureCard`) are the same notice, no longer red-tinted boxes: the reason with Retry and Open in Lark, or the hub's sentence with its buttons, the original under **Error details** and ✕; while the failure note shows, it is the one card over the composer and the model error stays in the chat. `ModelFallbackCard` is not a failure and keeps its look. Deviations from cindy: Esc does not dismiss the banner (Esc twice in the composer already edits the last message), and Retry's hover text says it continues the current task rather than that it resends the last message, which Persona's Retry does not do.

**Failed-start remedies** (web #10365): a terminal `launcher_missing` and a `worktree_path_taken` use their coded short line before raw-text classification; on detail surfaces the full hub failure stays behind Details, never as the primary text. When `task_retry_launcher` and the failed plan task's `retry_with` offer an alternative, **Use <launcher> instead** precedes ordinary **Retry**, then the surface's existing destructive/other actions; ordinary Retry still works without `plan_id`.

### Web skill mentions (#5357)

Reuse the composer's existing `ComposerPanel` surface for `@` and `SlashPalette` surface for `/`, their neutral sliding
highlight, icon set, keyboard/IME/Escape behavior and context chips. A skill row has a readable name, source and
description; `@` uses two lines, while `/` puts them in its own 36 px row (44 px on touch). The source
distinguishes equal names. Selecting either row adds the same removable skill chip and opaque reference rather than hiding an invocation in display text.
Skill rows and the skill chip's remove target meet the Web 44 px touch size. The menu's loading, partial-inventory and
failure status stays secondary and never disables ordinary chat. No new primary screen, setting, model choice or
native client pattern is introduced. Use the existing surface/hover/ink/accent tokens in light/dark and narrow layouts.
Ordinary Claude Code and Codex Agent chats use this same picker and chip. The catalog belongs to the actual session;
unsupported or unavailable environments show the existing quiet failure status while text and other references remain usable.

### 3.11 Code blocks, inline code and quotes in replies

Drawn as Telegram draws them (#2984). Inline code has no gray box (#9477). There was no component for these: replies used the Markdown renderer's defaults (a plain code box with Copy on hover, a thin bar), and the one code block with a title bar (`LarkCodeBlock`, Inbox) is the template, not a reply's.

| Piece | Look | Interaction | Source |
| --- | --- | --- | --- |
| Code block | a title bar on top with the fence's language (none when it names none) and Copy (icon and word); the code under it in the monospaced font on the code well (web `--inset`), scrolling sideways | Copy takes the whole block, with the "Copied" notice. Copy's hit area is the touch size (web `touch-hit`, 44 px). A sideways drag on a wide block still goes to the code | web `CodeBar` (`web/src/ui/ReplyBlocks.tsx`), `CodeBlock` (`web/src/ui/Markdown.tsx`) |
| Inline code | no background, border or padding (#9477): a monospaced face at 0.94 em, CJK inside it in the body's face (the web lists the body's CJK families before the generic `monospace`), in an ink a step quieter than the body (web `--ink` and `--ink-2` mixed 55:45, ≥ 4.5:1 on every page and card); words in an ink of their own (a quote, a quieter heading, an agent's quiet words) keep it, and in a written link it is the link's. About 0.12 em of room where it meets a CJK letter directly (a margin, `data-gap`), none beside a space or CJK punctuation | a click or tap copies it ("Copied"); so do Enter or Space when it is focused, and a pale wash shows under the pointer; a click ending a drag that selected words doesn't. Blue is kept for what opens: code that is a path there (inside a link) opens the file, not a copy | web `copyInlineCode`, `rehypeLinkedCode` |
| Quote | the accent bar on a faint accent card (web `--accent` at 8%), a quote mark at the top end | longer than about four lines it folds with its end faded; a click or tap opens it, its chevron (at the touch size) folds it again. A reply still being written never folds its quotes | web `ReplyQuote`, `ReplyStreaming` |

### 3.15.2 Subscription usage (Web and Desktop)

The sidebar footer uses the existing Agent marks, secondary text size and thin meters to show usage on one line. The percentages stay visible when the mini meters collapse, with the warning color starting at 80%. Prepaid amounts retain their currency. A count opens the full list when the footer cannot show every account. The anchored, keyboard-dismissable popover orders providers by their highest current quota use and has an in-place refresh with feedback. Detailed shows all accounts. Compact gives one summary per provider and discloses the selected provider's details beside the list on wide screens or below it on narrow ones. Both use the same account cards and the same quota and reset rules. See [Subscription usage](../web/web.md#subscription-usage-bottom-of-the-sidebar-2432).

## 4. Interaction patterns

### 4.1 Navigation hierarchy (super rule 3)

`docs/SUPER_RULES.md` rule 3: Assistant and Agents are the only primary home destinations. Settings opens from the sidebar. Everything else (Recordings, Inbox, To-dos, Scheduled Tasks, Knowledge, Machines, Health, extension pages) is pushed from the drawer.

A dispatched task's Open link (#3711) is a compact row using existing typography, fill, radius and 44 px touch-target tokens, not a filter or a link to the whole In Progress list. One task shows its title and Open; a plan lists only its own tasks in order. Before an Agent is available it opens that task's starting/failure detail. This targeted navigation correction reuses existing pages; no new visual style or setting. Details and wire contract: `docs/assistant/unified-entry.md`, "What a dispatch opens".

### 4.2 Gestures

| Gesture | Behavior |
| --- | --- |
| Drag in the image editor | Box and Pen draw (Box is the default tool); in Crop, a corner or edge within 22 px resizes the crop, inside moves it. Crop's shapes, Rotate and Reset are the controls that need no drag (#4963). A mouse's cursor shows which handle it would take (#4991) |
| Tap on a saved place's map | moves the place there (#4248); search and Use Current Location do the same without the gesture |
| Hold a to-do on the calendar | after 0.35 s held still it follows the finger (a pointer: once it moves) to another time, day or All Day row; saved on release with Undo in the toast; "Change Due Date…" for screen readers (`docs/assistant/todos.md`, "Calendar") |

Every gesture-only action needs a plain alternative for screen readers and switch control.

The web user-input navigator (#4836) initially exposes the latest 50 genuine inputs, or all inputs in a shorter
conversation. Upward transcript loading extends that range when it reaches earlier inputs; global ordinal labels
still count from the full history. Reuse the existing rail, hover preview, keyboard controls and mark geometry.
This requested pagination correction does not add a setting or change placement.
When a web selection needs older messages, keep its loading status at the rail while the current transcript stays in
place. Only the latest selection can move the transcript. If it fails, keep the marks available so it can be selected again.

The web Changes panel's secondary file tree (#10477) reuses the existing `ResizeHandle` separator from the sidebar and side panel: pointer drag, arrow keys, Home/End, and double-click/Enter to reset. Column dimensions live in `web/src/lib/layout.ts` (`CHANGES_TREE_DEFAULT`, `CHANGES_TREE_MIN`, `CHANGES_TREE_MAX`, `CHANGES_DIFF_MIN`). Its default remains 192 px; the browser remembers a user resize and temporarily fits it to the panel while keeping space for the diff. At narrow widths the tree stays above the diff. File names and indentation may truncate, while each row's unchanged addition/deletion counts remain whole and the full path remains on hover.

### 4.3 Feedback and haptics

Message feedback (#5258, principle 7, super rule 5) reuses the chat's status line and pending bubble, not a new card or
style. Show Sending immediately from local state, then Processing on the hub's `message_send_state` receipt; do not wait
for a model token or overwrite `Session.status`. The pending-send overlay is scoped to the conversation and message,
survives stale snapshots, and hands off to actual execution or queued state on delivery. Failure removes only its own
overlay and keeps the existing draft, failed-bubble and uncertain-delivery recovery. On the web the state is said once,
by the status line under the last message (Sending, Processing, delivery unknown); the pending bubble only dims, with no
second line under it (a queued message's bubble keeps its Queued). The full wire and reconnect rules
are in `docs/protocol/protocol.md` ("Message feedback (#5258)"). Apply the same definition in Assistant and Agent chats.

| Message state | en | zh-Hans | ja |
| --- | --- | --- | --- |
| Local input pending | Sending | 发送中 | 送信中 |
| Hub received; execution may still be preparing | Processing | 处理中 | 処理中 |

These are message states, not answers to an approval or a claim that the model is already working. Receipt alone does
not enable Stop or promise cancellation of the pending HTTP request. Keep existing answer
labels and the authoritative Working/Needs you/queued state words. Verify dark/light in the existing styles and measure
send-to-feedback separately from send-to-input delivery; a transport receipt's queue timestamp is not a client paint.

### 4.4 Confirmation and undo

For an existing Assistant conversation on a hub with logical-segment switching,
choosing a different engine or runtime connection in the existing model panel
opens the shared `Dialog`/sheet with **Switch connection** and Cancel. The audience
is people deliberately changing an advanced conversation connection. The text
explains that recent messages and a summary continue while the conversation and
tasks stay, and the next message activates the new connection. Selection alone
sends nothing. The underlying model panel is held beneath the confirmation.
Submit shows a pending action immediately and uses a distinct switch operation;
it never writes an ordinary model pick across engines. An uncertain response
keeps the exact operation for retry, says its result is unconfirmed, and labels
dismissal Close. Actual session updates, rather than an optimistic engine rename,
confirm activation. Older hubs keep their existing unavailable-engine explanation.


Severity ladder to adopt:

1. Reversible: do it and offer Undo in a toast (inbox bulk actions, remove from list, folder trash do this).
2. Destructive with one object: a confirmation dialog with a destructive button that repeats the verb, a message that says what is lost, and Cancel.
3. Needs extra choices (keep or remove the worktree): a content-height sheet.
4. `.alert` for short prompts and text entry, except where the text needs what the alert would cover (below).

**Send now** on the web's queued messages acts at one press, with no confirmation (#7155; it used to open a `Dialog`, and an in-place second press was tried and dropped),. On this ladder it is not destructive: nothing is lost, since the messages go now rather than at the Agent's next step, and only the step under way is cut short. Next to it, Delete (which does lose the message) and the composer's Stop and Esc (which cut the whole turn) act at one press, and Claude Code's own send-now key asks nothing, so a confirmation here was the odd one out. What the dialog said (what the step under way does) is the button's tooltip and its accessible description on the web, and its VoiceOver hint on iOS; the toast that follows says they went (on the web, to whom).

The web Agent close action uses `capabilities.close`, with terminal-only closing retained for older nodes. A runtime that can close a conversation uses the existing close confirmation even when it has no terminal controls. The confirmation checks the current row before sending, so an already offline session simply dismisses it.

An Agent has one close action in the web's menus, **Close…** (#7125), where **Close agent…** and **Archive and close…** used to sit side by side. It is rung 2: not busy, it archives and closes, the button says **Archive and close**, and the toast offers Undo. A busy Agent's Close… is not greyed out: the confirmation starts with one line in `--red` naming what closing interrupts (an answer it waits for, its work, a loop, a server), and the destructive button reads **Close anyway**. **Archive** stays separate and waits while the Agent is busy, saying why, because it only hides the Agent. A pane with an Agent closes through the same confirmation, so no second path closes a working Agent without the warning; Close pane sits with the pane's own actions (New pane, Git), not in a danger group of its own.

Clearing the main chat's context (**Clear context** in its ⋯ menu and `/clear`, #5095) is on the reversible rung without an Undo toast, a user-requested alignment with Cindy's `/clear`: it runs at once from the menu or the command, and its only feedback is the chat emptying in place (the same top bar, an empty middle, the composer still at the bottom; no jump to the centered new-chat page). Nothing is deleted: the old chat is archived as "Old main chat · until <date>" and comes back when it is sent to. Failure is a toast with the reason, and the `/clear` goes back into the box. **New main chat…** keeps its sheet, since it has a choice to make (carry a summary).

Deleting a Persona chat (#6172, web first) serves everyday people and reuses Menu, Dialog and Button without new tokens. **Delete…** appears in sidebar and History menus. The destructive one-object dialog has **Delete chat** and Cancel, with copy stating that Persona history is removed, task and Agent references and previously saved knowledge remain, and the computer transcript is retained. No physical-file checkbox is offered. It appears for ordinary and archived former main chats when the hub supports it. The main chat uses the existing Clear context action before its archived predecessor can be deleted. Cancel and repeat submission are disabled while the request is pending. Busy or waiting work is refused inline without removing the row; success removes it, while an already open reference shows **Conversation deleted** without a composer.

Removing a link between two Knowledge entries serves everyday people. The web dialog (`Relationships.tsx`, #10333) names both entries and say what stays out.
**Also stop automatic changes to both lists** starts off each time. Turning on the `Switch` or `Toggle` names both lists
it will protect. The link disappears immediately on submit, then the app checks both entries.
**Removed by you** follows the related entries and offers a secondary **Allow again** action on each row. It makes the
narrow removal reversible by letting Persona consider the link in a future update.

A blocked terminal card on the web offers **Close Agent**, including when no choices can be parsed from the screen.
Its footer reuses `Button`, explains that the pending confirmation is cancelled and the conversation kept, and
requires **Close and archive** or **Cancel**. Close controls use the existing 44 px grid minimum and single-line labels.
The archive toast offers Undo; Undo restores visibility without reopening the Agent or granting its request.

Task-progress reply drafts (#3743) use the reversible rung: **Send** is primary, **Later** and **Don't handle** are
secondary. Don't handle removes only the exact draft from Needs you, sends nothing and leaves its task, Agent and
Inbox source open. It persists a task decision and offers receipt-scoped Undo in the web's existing six-second toast
or Android's toast (`JToastHost`, which draws the button in the accent, as iOS). A newer progress draft
remains eligible; Undo cannot overwrite it. Web uses `Button` with
44 px grid-based minimum targets and stacks the action group at narrow widths. Android reuses `DraftDecisionButtons`
with the existing 48 dp button token and stacks the entire group at large text sizes or narrow widths, primary first.
A message card in Persona's conversation on iOS and Android's Glass style adds **Edit** beside Send: the drafted reply
reads as text until Edit opens it for typing, and **Done** closes it (iOS `MessageCard`, Android `DraftDecisionButtons(edit)`).
The original card's not-sent receipt remains inspectable. This deliberately does not reuse Inbox completion, which
would suppress future progress replies for the same source. The API contract is in `docs/assistant/agent-work.md`.

A hand-off write-back card (#5932, web `HandoffCard`, iOS `HandoffCardView`) is the same card for an Inbox item the
user handed to the conversation: the item's summary over the reply, **Send** primary (**Got It** for a conclusion),
**Edit** on iOS beside it (the web edits in place) and **Later**, which folds it to one line. It has no Don't handle:
the item is still in the Inbox, where it can be turned down. Its state comes from the item, so it is never a second
count of the same request. The main conversation's **Wrap up: <title>** chips over the composer follow `RouteChip`'s
capsule, one per open item handed to it, at most two.

Today there are seven mechanisms and the same action can use different ones (DS-24). Archive in the sidebar, to-do swipe delete and mail-account delete have no confirmation and no Undo. Only the Mac discards edits with a prompt.

Saved Agent searches (#7753) reuse the command palette and one replacement `Dialog`, switching between its list and
editor instead of stacking modals. Empty-query shortcuts follow recent chats; applying one fills the search field
without executing an Agent action. A version conflict keeps the editor draft and shows the current saved values;
replacing them requires explicit review. Delete is reversible through a revision-guarded Undo, while ordering uses
visible up/down controls as a keyboard alternative. Returning restores the prior query and focus. These secondary
controls and their code load on intent, not as another primary destination.

The web command palette's runtime-Agent search (#7624) adds a status line and More/retry controls outside the
listbox. Local results appear immediately; bounded server pages supply authoritative matches and evidence. New
results preserve the selected identity rather than the previous numeric row position; a removed selection requires
a new keyboard choice before Enter opens anything. Loading, failed, expired and loaded-only fallback scopes are
stated explicitly. This reuses the existing palette, controls and localization without a new search destination.

The optional web workflow board's manual view supports bulk metadata edits (#7583). **Select** reveals native
checkboxes and a selection bar using the existing `Button` and `Menu` components. **Select all visible** counts only
cards in the current filters and, on a narrow board, the displayed column; hidden cards are not silently included.
The bar wraps whole controls without wrapping button text. Movement, pinning and archive/restore use the reversible
rung: immediate feedback, one atomic write, and **Undo last change** while its acknowledged board revision is current.
A conflict reloads without replaying the operation or its inverse. This pattern belongs to the optional Agents board,
not Assistant's primary chat. Older managers and view-only connections retain the existing single-card experience.

The optional workflow board's **Activity** view (#7599) is a read-only `Dialog`, with a matching expandable section
inside workspace details. It fetches compact revision summaries only after opening and paged before/after changes
only after expanding. A new-activity hint preserves the reading position until the user requests the latest page.
Historical names remain as recorded; current workspace navigation appears only for an existing card. An update that
involves several cards stays one group, including when reached through a card's history. This uses the same buttons,
loading/retry states and timestamps as other secondary views, and adds no primary destination or rollback control.

### 4.5 Push, notifications and reminders

- Reminders are agent tools and Scheduled Tasks, not a UI object.
- Wording differs per surface for the same event (section 5.4).

### 4.7 Tool execution collapsed (super rule 2)

Tool execution is one summary line, never opens by itself, shows the latest text segment while running and the result when done, and expands by hand. Opened by hand, a step (and the summary itself) changes in place: the tapped row stays under the finger, what opens fades in, what closes goes at once, and the list holds still while the turn goes on (new steps, the answer, the next turn, the footer under the list changing) until the user scrolls, taps ↓ or sends (#3005). The "latest segment only" half was not confirmed from code reading and needs a screenshot test.

**The opened summary** (web, #10247) is a flat sequence of narration and action groups, sharing one rail and indentation. The outer summary keeps the whole-exchange clock and collapsed default. Action headers name at most three kinds in first-seen order, with the first kind's icon; read/edit/search/command kinds have localized counts, and other kinds retain their existing labels. Earlier live groups start collapsed, the current one opens within the trace the reader manually opened, and a superseded run closes; all groups can be opened and closed by hand. Newly loaded completed groups start open; remembered disclosure choices remain available. Latest narration stays only in its text slot until superseded, and the final reply appears once. Rows lead with the file, query and target, or command; command descriptions remain in tooltips and expanded detail. Reliable per-call durations appear only from 30 seconds; the total exchange clock stays on the outer summary and run bar. The current run previews edit diffs using the existing diff renderer. The flatter layout replaces repeated time headings and nested rails so the inspected work is scannable, while preserving the top-level progressive disclosure required by super rule 2.

A long finished summary draws its first 60 steps with the tap and adds idle batches of 60. Live summaries, search hits and jumps draw all requested steps immediately. Search, jump-to-step and folded history continue to open the groups holding their target (`docs/web/web.md`, "A long summary opens a screenful at a time"). A thought with words is one row in the tool rows' chrome (16 px status slot
with the sparkle, italic `thinking-ink` words on one line, the 18 px chevron slot), opening in place only when it does
not fit or has a line break, its opened words selectable; a thought without words is no step, so it neither makes a
summary of its own nor splits a run of rows. The live "Thinking" line with dots and a clock ends
the innermost live summary, once. Durations read `6s`, `1m 5s`, `2m` in every language and, from an hour, the
language's form that always keeps the minutes (`1h 0m`, `1 小时 0 分`, `1時間0分`). Copy follows cindy's catalog
("Work details" without times, "Searched once", "Listed files once"). A diff count reads `+12 -3`; the in-place
detail's divider is the tool ink at 20 %. The chat's items stand 14 px apart under 28 px of top padding. Held back by
super rule 2: cindy's running preview of the latest five activities, its narration in the open while running, and its
inner summaries folded by default (exception requested in #5162). Kept from Persona: a failed call's red ✕ and red
output (#4303 outcomes, below), which cindy's rows lack.

Subagent launch receipts, interim reports and final results belong under their launching tool, not in independent
parent-chat message bubbles (#3378). Empty completion pointers update status without creating another report. The
shared wire contract is in `docs/protocol/protocol.md` ("Subagent deliveries as tool results").

**Task cards** (web, #5030; the owner asked that the subagent part of the chat follow cindy's `AgentTaskCard` exactly,
which replaces the earlier grid, recent-steps list and side panel). A subagent call and a workflow call are each one
full-width card where they were launched (`TaskCard`: a bordered `rounded-xl` surface, 12 px side padding): a 20 px
round avatar, a small kind word ("Subagent"), the title on one line, the status word with its mark (Running, Completed,
Failed, Stopped; only Failed is red), and a second line in the tertiary color: what ran it, a model chip (the model it
actually ran on, short; never the requested one), tokens, tool uses, the time or "Running for …" each second. The head
opens a subagent card in place (prompt, the latest result folded past four lines, last tool, output file), remembered
per card; a workflow card's head opens the chat's Background dialog at its run. A subagent is never a conversation of
its own (no panel, no page, no sidebar row). Like every tool call the cards sit inside the work summary, running or
not (super rule 2; cindy's running cards outside it wait on #5047). Nested subagents are flat cards by start time. A card offers Stop only
where the node can stop that task. 
A child session (a Codex subagent, an Agent a Paseo Agent created; #5120) is the same card in its parent's chat, built
from its own row and placed by when it started, since no call in the parent's transcript names it: what runs it, its
model and effort, its status and time; opened, its prompt, latest words and last tool.

**The plan card and pill** (web, #5156; the owner asked on 2026-11-12 that the plan follow cindy's `InlinePlanCard` and
`TodoListCard` exactly, which replaces the one-line card and the side panel's Plan). One card per exchange, where its
latest TodoWrite of two or more items was written (`PlanCard`; one rule, `isPlanList` / `planCardStep` in
`lib/work-groups.ts`, for the card and the pill); earlier updates and lists of one item are ordinary rows (cindy drops
them; super rule 2 keeps every call inspectable), and a plan update keeps its group from being titled as reading only.
A `rounded-xl` (12 px, as `TaskCard`), 1 px `line`-bordered `surface` card; its head (`px-3.5 py-2.5`, 8 px gaps) is a
14 px chevron in `tool-ink` that turns a quarter when open, a 16 px `todo` icon, done / total in 13 px monospace medium
(as a tool row's counts), a `tool-ink` dot and the step in progress (else the last) on one line in
`--chat-secondary`. It starts open on the whole list, which opens and closes with `Collapse` like every fold in the
chat (rows at least 30 px, 10 px gaps, `--chat-secondary` at 1.45 that wraps):
done steps an 18 px check in a circle with their text in `tool-ink`, the step in progress an arc on the same circle in
`ink` with medium text, pending steps an empty circle in `ink`. The arc turns only while the turn runs and, unlike the
spinners, never with reduced motion (its shape already says "in progress", as in cindy). Closed, a 2 px bar under the
head: `line` track, `ink` fill of done / total, moving over `MOVE_MS` with `EASE`. The card stays inside the folded
summary (super rule 2; cindy draws it in the open, #5162).
While that card is out of view a pill sits above the composer: rounded-full, 1 px `line` border, `surface`, 14 × 6 px
padding, a still 16 px progress ring (stroke 2, `tool-ink` at 45% under an `ink` arc of step / total) and "Step 3 of 5"
in 13 px. Hover shows a flyout and leaving hides it; a click pins it, and a second click, a press outside or Esc unpins
it. Pinned, it is a non-modal dialog on the overlay Escape stack (`useEscapeClose`): Esc closes only the topmost
overlay, and the chat's Esc, which stops a running turn, leaves it be. The flyout is a new pattern: a `rounded-xl`, bordered
`surface` panel centered on the composer column, 8 px above the pill, 220–420 px wide (and the screen less 32), its
list scrolling past 280 px (30 px rows in `--chat-secondary`, the step in progress semibold), coming in with `pop-in` (160 ms, `EASE`, as the other popovers) and
fading out over `EXIT_MS.card`; it is drawn on the page's body so the scrolling box over the composer does not clip it,
with a 12 px band over the pill's top so the pointer can cross the gap. Its 28 px round ✕ (**Close Plan**, `tool-hover`
on hover) hides the pill until the plan changes. The pill also goes once every step is done or a later message of the
user's starts another turn.

A tool row names its tool in words (#2986): Claude Code's tools have verbs of their own, another
server's MCP tool reads "<Server> · <Tool>" ("GitHub · Create issue"), and any other name is humanized. What the call is
about follows, picked from its input by the field that says the most . Two subjects are words of their own: several
findings by their count ("2 findings") and a wake-up with no reason by its delay ("5 min"). A computer tool names the Mac
as the user knows it ("MacBook", not the `macbook` the tool wrote). Opened, a tool
no table knows shows its arguments as `key: value` lines, and a tool that operates a Mac its steps in words ("Click AC →
6 → ×") with what the driver answered and the picture it brought back, each with the JSON one tap away under Raw Input.

A tool row says what its call came to when the node sends `outcome` (#4303, `docs/protocol/protocol.md` "Tool
outcomes"). Only a call that ran and failed turns the row and its chip red, and its result says "Exit code N". A call
that was denied, refused before it ran or interrupted gets a gray mark with its word (Denied, Did not run, Interrupted)
for screen readers, as section 4.8.1 keeps red for what is still wrong; one that timed out says Timed out. Under an output the
page cut, the result shows the output's end (End of output), where a build or test run says why it failed, before View
full text. Without an outcome (an older node) a row draws as before.

### 4.7.1 Opening and closing in a chat keeps the tapped row in place

Anything that opens or closes inside a chat, in Persona's conversations and Agent chats alike (work lines, tool steps,
thoughts, notes, task cards, long quotes, a report's Expand, and so on), keeps what was tapped where it was on screen,
so the conversation reads on without a jump (#3757). The reference point is the tapped row's top edge, or the tapped
control when it sits lower in the row.

- **In the middle of a conversation**: the tapped row stays at the same screen position, within a point or two, from
  the tap until everything it opens has arrived, including steps fetched from the hub a moment later. What opens grows
  downward under it; what closes takes its space with it at once.
- **At the newest end, following**: the list stays at the newest end and what opens grows upward, the tapped row rising
  with it, until the row's top reaches just under the top bar. There the list stops following and ↓ shows, so the opened
  row is never pushed off the screen. Closing there brings the newest end back: the row comes down by what closed,
  gliding, not in one frame.
- **At the oldest end**: what opens grows downward and nothing moves above it.
- **Your own long message** (#5676, `BubbleFold`): its top stays where it is when it opens or folds, at the newest end
  too, with no rise to the top bar: the owner asked that the bubble never move (2026-11-12). What opens runs on below
  the screen and ↓ shows; folded from its end, what was under it comes up (`toggleInPlace(rises: false)`).
- **Afterwards**: the list holds still while the conversation goes on (new steps, the answer, the next turn, a strip
  appearing under the list), until the user scrolls, taps ↓ or sends (section 4.7). A row opened at the newest end
  takes what comes in next the same way it took what opened: it rises until it is under the top bar, then stays.
- **One mechanism**: rows report their top (`chatPinned`, `WorkHeadReport`) and change through `toggleInPlace` or
  `toggleWork`; `ChatPin` keeps the tapped row in place, waiting for what comes from the hub (`ChatPin.awaiting`), and
  `ChatFollow.toggledRow` decides how it is held. A new expander uses these; it never animates its own height or
  scrolls the list itself.
- **Tested** by `KeepTappedRowTests`: in the middle, at the newest end and at the oldest end, one open and one close
  each, in both chats, asserting the tapped row's screen position frame by frame.

- **Edit diffs use the existing side panel (#9160)**: an edit row opens a Changes panel at that file and call, not a modal that hides the conversation. The panel gives the exchange's edited files a tree and keeps the selected diff beside a scrollable conversation on wide screens; a scope switch reads the whole session, including folded history. Live edits never move the reader to another file: the current diff appends in place, and other files use a new-changes pill. The old modal had no room for that navigation and could not leave the explanation and diff visible together; the existing panel already supplies the container, narrow-screen return and session memory, so no second viewer remains.

### 4.8 Pending requests

Task decisions on the web reuse the task-routing chip for **Answer in chat** (#5929). It identifies the task and
states visibly that the next message is saved as the answer; its existing cancel action switches back to ordinary
discussion. The original decision round travels with the answer. Only a delivered and recorded answer removes the
pending card; neither navigation, an unrelated chat message, nor an answer to a different Agent question does.

Expired assistant questions (#9731) keep one home on their question to-do. The shared `ask` payload puts them in **Needs you**, not **In progress**, without inventing an Agent or supervisor. On the web, the existing question card displays the original choices and descriptions inside that row: a single choice sends on one tap, while several questions or multiple selections keep the existing complete-form submission. Its answer goes through the to-do's late-answer route, not the expired interaction. **Later** defers only this browser's current question round; opening the to-do preserves its existing explicit close/drop controls. A failed answer keeps the original form usable. This adds no style, typography token or pending-request list.

An irreversible deadline only reminds the owner: the question stays in **Needs you**. A reversible default reserves its answer while being delivered or recovered, so it never offers a second decision; a failed default returns that decision to the owner. A rejected inline answer retains the same selections for retry and never shows **Answer recorded**.

Kinds: questions, plan approvals, tool approvals, secret entry, MCP forms, and waiting for a reply.

- **Home**: the bottom of the session page, a stack of cards above the composer, which stays usable. A request that blocks the Agent stays visible there until it is
  answered or folded, and says why the Agent waits ("Answer this so Claude can continue"). A card its session does not
  let the app answer (no `capabilities.approve`) is greyed with one line under it in `cautionText` saying why: where the
  session can be attached live (a Codex thread on a shared app-server not joined yet), that attaching answers it here,
  with the ⋯ menu's **Attach Live** beside it; otherwise, to answer it on the computer (the web greys it without a word). The cards stack as they come
  while they fit in half the page; past it they scroll on their own under "N pending", permissions first and then the
  newest, and a new card brings the stack back to its top. "N pending" counts the cards in the stack. With the keyboard
  up the cards take the conversation's room above the rest of the footer, the composer staying over the keyboard, and
  "N pending" is left off a lone card. Each card folds on its own: a tap on its header folds it to one line, its title and
  what it asks, and another opens it; any can be folded to answer later. A message typed while a card waits is sent as
  any other, also while a permission holds the Agent.
  A Persona conversation's own requests (its model's tool approvals, `ask_user`) are the same: the stack above its
  composer, under the Needs you / In progress entries and the route chip, on every client (#4883: on the web
  they sat only in the closed Needs you tray, and an owner waited minutes on an approval no screen in the chat showed).
  Needs you still counts them, and its row for one closes the tray onto the card instead of drawing it a second time.
  On the web, a card the conversation's own turn waits on (its own session's request, not one of a worker or side
  session folded under it: `lib/turn-asks.ts`, #11794) is never folded: each stands open above the Needs you / In
  progress capsules, all of them at once, and Needs you neither counts nor lists it again. The cards of sessions folded
  under the conversation keep the stack and the tray as above.
  The stack holds the conversation's own cards alone, as one slot on the web: its first card with "2 / 6" (where it stands in the run), the next taking its place when it is answered, and what was just answered one line over it that goes 3 seconds after the last answer (details with the interaction cards in `docs/web/web.md`).
  What the conversation's tasks ask is a card of its own above the entries: the oldest
  question of their Agents, under its task's name with the ask mark in the needs-you tone and, at the end of that line,
  "N more" for the tasks' other questions; it is answered there, and the next comes up once it is. The three numbers
  count three things and say so: "N pending" the conversation's own cards in the stack, "N more" the tasks' other
  questions, and Needs you its rows (one for each task or Agent that asks, plus the conversation's own cards, plans and
  drafts). An open plan is also a card among the conversation's lines (web `FeedLineView` #8193), at the
  end of the exchange it happened in: its tasks, where each would run, "Start these directly", then Start, Edit and Don't start;
  once started the hand-off cards take its place, and a dismissed one reads "Not started · N tasks". A task's or an Agent's row in the tray opens that task's or Agent's detail, where its page
  and its cards are; it never brings a card into the stack, which is the conversation's own.
  A question card folds from its title row (the whole row, and the fold button at its end, `interaction-fold`) to one
  line: its title, the question and, for a card of several questions, "N left". Folded, it keeps its page and its picks
  and gives the conversation its room. Where the chat finds the message the card is about (the agent's words before the
  call that asked it, `CardSource`), the open card's title row has **View in Chat**: the card folds and the chat goes to
  that message and lights it up, as a quote's jump does.
  A child session's request (web, #5120: `folded_under`) has its parent's page as its home: shown there with the
  parent's own, "From subagent · <title>", counted once under the parent, and every link to it opens the parent.
  A lead's worker's request (web, #5180) has the lead's page as its home too. While the Team panel shows that worker,
  the request is in the worker's conversation there and the lead's dock leaves it out, so the page never shows it twice.
  Otherwise it is in the lead's dock, "From worker · <title>". It is counted once under the lead, and a link to it opens
  the lead's page with the panel on that worker.
  A side session's request (web, #7944) has its parent's page as its home the same way: while the Side sessions panel
  shows that side, it is in the side's conversation there and the parent's dock leaves it out; otherwise it is in the
  parent's dock, "From side session · <title>". It is counted once under the parent, and a link to it, its
  notification and its Needs you row open the parent's page with that side shown. A request of a row that belongs to a
  side (`folded_side`, its own Codex child) goes with that side.
- **One count, per session**: the Agents list's Needs you section, its Needs you filter and the tab's badge count the sessions that wait for the user, each once, its requests in its row oldest first, the way `docs/assistant/managers.md` ("Counts") counts an assistant's waiting agents (principle 4). The Inbox's Agent section and the Assistant's composer entries open the same requests; the composer's entries count tasks (`docs/assistant/unified-entry.md`).
- **Background rounds and supervisors** (#4314, `docs/assistant/assistant.md` "Background rounds"): in the main conversation a round another conversation, a scheduled task or a follow-up started reuses the quiet supervision record, one muted line `@<source> · Background round` that always opens, never a new component; a round that asked or told the user stays shown. While that switch is on, the composer's Needs you groups rows under the conversation supervising them, this one's first, with a plain caption header and no other change: the same rows, order within a group and count.
- **Cut off** (web, 2026-11-14): the dock's cards over the composer keep to their cap (55% of the visible height, #5375) and
  scroll; a card whose answer row stands at the box's bottom edge while the end of its body is under that row or below
  the box says so in that row: its title and what it asks (the command) beside the answers, the whole of it as the
  tooltip, a click bringing the whole card into view with the focus on it. A card that asks only its title (a question)
  says it only while the title is out of view too: the line never repeats what is in view. While any card of several is
  cut off, the first says **N pending** at the end of its kicker row (the room that row already has), on a narrow screen
  once the stack is opened in full (folded, its "N more" row says it), so the stack never hides how many it holds.
  Wherever an Allow is in view, what it allows is in view too.
- **Room** (web, #4656): an open tray of requests takes the room its cards need, up to most of the visible height (the assistant's Needs you / In progress tray lets the dock grow to 82% of it while open), never a fixed short box. Its header (title, All, Expand, Close) stays and only its list scrolls; a card's answer row stays at the list's bottom edge with a hairline over what scrolls under it. **Expand** in the header fills the chat column for going through many requests, the composer still on screen; the same button, **Restore size**, or closing the tray gives the room back.
- **Card text**: the title names the request and what happens next, in the same words on every client: the request titles in section 5.6. The body gives only what the decision needs: the command, the plan's summary, the question and its choices. Commands render formatted, never as raw JSON. A script ask whose interaction has `format: "markdown"` draws its permission summary or question with the existing reply Markdown renderer, including inline styles, lists and short tables; missing and unrecognized formats keep the literal card text (#10857).
- **A plan's recorded following mode** (#3784): when the plan interaction carries `plan_then`, the existing approval card names that mode with the general permission-mode label and offers one approval, not a competing set of modes; the recorded mode came from the task's start, so it wins on older clients too. Send Back stays. A card without the field keeps the existing approval-mode choices and default. This changes only the affected approval's content, reusing the current buttons, menu-free action row and localized labels.
- **Editing before Allow** (web Lark send/reply): the default card remains read-only with Allow primary. **Edit text** and **Deny with note…** progressively disclose their fields only when the server declares the capability. An actual edit hides Always and makes Allow apply to the reviewed replacement, never the original command; blank text disables Allow. Deny remains an explicit action, with its optional reason secondary to the decision. Other clients and other permission kinds keep the unchanged Allow/Deny layout.
- **A file tool's change** (#4621): an approval for Edit, MultiEdit or Write shows the file and the change as the chat draws
  the call once it runs (the file header with +N −N, then each edit's diff), eight rows of each
  diff until Show All and four at the accessibility sizes, so the card's choices stay in view. A card that carries only the
  path (a Codex edit read off its terminal) shows the path as before.
- **A command asked about on the terminal** (#5266): a permission prompt the hub reads off an Agent's screen (Claude
  Code's own safety check in bypass mode, "Bash command … Do you want to proceed?") is a tool approval, titled as one
  ("Claude is waiting for your approval"), with "Claude wants to run a command" in place of the terminal's question. Its
  body: the agent's own words for the command, the command in code type (the web shows four lines, and the toggle only when the rows cut something), the warning in plain words ("Can't confirm this command won't
  delete files", a generic line for a warning there are no words for) with Claude Code's own warning and countdown under
  Show original, and the countdown as a line that runs ("Denied automatically in 1:44", then what happens when it ran
  out). The web redraws the line once a second while it is shown. The prompt's Yes / No are Deny /
  Allow (primary); a prompt with more choices keeps their labels as numbered rows. Beautiful UI's card keeps its choice
  rows and Send (as its permission card) and is tighter (`compact`, #5556), so Send stays in the half-page pending
  area's view: the agent's words in the secondary type close over the command (as that card's reason), two rows of the
  command, the countdown beside the warning where the row has room. Components: `CommandScreenCard` (web
  `web/src/views/chat/CommandScreenCard.tsx`, its own chunk loaded when the first such card shows; until it is in the
  card is the terminal's question as before); the rules in `web/src/lib/command-ask.ts`
  and `web/src/lib/command-ask-lines.ts`.
- **Buttons**: for a tool approval, in this order: Deny, Always Allow for This Session, Allow Once (primary). High-risk actions offer no "always" and ask twice.
- **Secret hand-off (#3962)**: the card that hands a provider's secret values to one Agent run is a permission card with the tool
  approval's title ("Waiting for your approval") and buttons (Deny, Always allow, Allow); its body names each variable with the
  item it comes from, the Agent and its computer, where it runs and what it is asked, never a reference or a value. Like the
  password card it is answered on the card only: no quick answer on a row or a notification,
  and no key approves it (`docs/extensions/secret-providers.md` "In the apps"). Only the card's actions send the
  request-specific `secret_handoff` feature.
- **A command waiting for input (#14527)**: a permission card with one secret field (`command_input`,
  `docs/protocol/protocol.md` "Commands waiting for input"), so it is a request with one home that counts once in Needs you
  and has no quick answer anywhere. Its title says what waits in plain words ("Waiting for your input"), never the engine or
  the task's id; its body is the command on one line, the lines it printed before the prompt (a web address there is a link;
  a long output scrolls in its box, its end in view) and the prompt as the field's label. The field follows the password card's input rules (no form, no name, one-time-code
  autocomplete, Enter only puts the keyboard away) and is hidden while typed when the prompt reads as secret, with Show /
  Hide beside it (an icon button, `eye` / `eyeOff`); the buttons are **Not now** (quiet) and **Send** (primary, off until
  something is typed). Web `CommandInputCard` (drawn by `PermissionCard`, in its chunk). The terminal's key bar has
  the matching **hidden line** key (web `LineBox`): one line typed into the pane with Return, the same input rules.
- **Task-review decisions (web, #4808)**: reuse the answerable decision row, its recommendation tag, the `Button` action group and the draft's Later / Don't handle labels. The two secondary actions stay on a separate footer so they never displace the recommended choice; the footer follows the tray's scrolling edge and uses the existing 44 px button minimum. Later defers only this browser session and this review round. Don't handle persists a dismissal of this exact review, offers receipt-scoped Undo, leaves the task open and takes no option; a dismissed review cannot take its timed default. A new review is visible again. The web hides the row and updates its count optimistically; a confirmed same-round dismissal stays hidden across reads and reloads, while a failed write restores the row with an error. An old Agent runtime error does not override dismissal or Later for that review; an actual Agent input request keeps its home visible. Old reviews without an acceptance ask initially show their original explanation and these same controls, never invented choices or an acceptance button with a made-up round. Once a stale decision goes back for rechecking, or an undecidable old review goes back for restatement, that round leaves Needs you and cannot be answered until a fresh supervisor review re-issues it; a fixed-response round keeps its existing visibility. This is a requested correction to the existing row, not a new style or setting.
- **Independent session-review results (web, #1808)**: an advanced Agents action, explicit and progressive: ⋯ reads availability only when opened, and the existing screen is unchanged without the `session_review` feature. Starting gives immediate local feedback before the hub's result. The finished review reuses the pending-card area and question wire shape, with `tool_name: "persona_session_review"` so older clients need no new kind. Its kicker is the number of findings parsed from the reviewer (not the number of changed files); the full report uses the existing Markdown body and one bounded scroll, and the existing `Card` / `Button` patterns provide **Send to agent** (primary) and **Dismiss** (quiet). There is no Other field, notification quick answer, or hand-back action. Send resolves the source card and the manager forwards the persisted report; Dismiss resolves without a message. Failure leaves the same card answerable. The result alone does not disable a later review; another pending answer does.
- **Questions**: one at a time, with single choice, multiple choice, Other and voice. An unsent answer is kept as a draft per request. Dragging through choices only scrolls and never picks one. When the interaction advertises `can_cancel`, a visible **Skip** text action with at least a 44 px target leads the action row, on the review too. It is not the card's fold mark. Skip sends only `{"cancel":true}`, is disabled while an answer or another Skip is in flight, and leaves the card and its picks in place with the error and Retry on failure. Success removes the card and gives the receipt **Question closed without an answer**. Persona's own main-conversation `ask_user` has no terminal handoff; an Agent's question keeps it. Cards without `can_cancel` have no Skip and keep their existing layout.
- **A card this client can't answer** (web #7497): the session's
  capabilities withhold `approve` (a Codex thread Persona hasn't joined, a subagent its parent drives, an unknown
  session), so the card's answers are disabled. It never greys silently: one line under the card in the attention tone
  says why. Where the session offers a live link: "Link it live to answer it here, or answer it on the computer", with
  the ⋯ menu's **Live link** (same label, icon and action) beside it; once linked the card answers and the line goes.
  Otherwise: "This session can't be answered from here. Answer it on the computer." An answer on its way is a different
  state and keeps its own "Sending" line in the card's answer row. A manager work card (`persona_triage`) has no line:
  its local defer and dismiss still work there. Web: `answerBlock` (`web/src/lib/format.ts`); the line and Live link
  load in their own chunk, off the first screen (`web/src/views/chat/CardBlockedLine.tsx`).
- **A question the assistant sees first** (`delegating`, then the same `id` with `delegate`): while the assistant decides, the question stays answerable but makes no arrival sound, haptic or notification; the handover replaces it by `id` and announces once. The card shows what the assistant filled in (`what`, `why_now`, `recommend`, `cost`, `reason`, `deadline`, in that order). A recommendation in an option's own words tags that option **Assistant's pick** without selecting it; another recommendation stays in the context. Web `QuestionCard`.
- **A question the assistant answered** (web `Delegated.tsx`): no longer a pending request, so it is counted nowhere and has no Needs you row. The newest one for the Agent is a card over its chat's composer, after the Agent's own cards, until its × puts it away: "Answered by the assistant", each question with the answer as it is now, "Why: …", and Change Answer while the chat takes messages. Change Answer's sheet starts from the answers so far and sends only what changed, as a message to the Agent. Session Info lists every one with the same body and Change Answer.
- **Everywhere at once**: the first decision on any surface (web, notification) wins. Every other entry updates at once and offers Undo where the action can be undone.

### 4.8.0 Task cards (web, #5569)

**Personal verification** (#10047): a supervisor's `accepted` review with `user_check: true` is not a completed
to-do. Reuse the review chip and caution tone for **Awaiting your verification** (zh-Hans **待你验收**, ja
**あなたの確認待ち**), keep one actionable home in Needs you, and count it as open Waiting on you even when deployed.
The existing Agent state column continues to describe the Agent, not the task. Only the owner's Pass completes the
task; change feedback returns it to ongoing work and invalidates that verification round. Reuse the existing
verification card, Pass action and chat feedback path; no new setting, component, group or polling loop.

The task cards follow `docs/design/task-cards.md`, which holds the state tables and the reasons.

- **One card per task, when it is handed out** (#8237, replacing #5569's conclusion cards): the hand-off card is a task's only card in the chat, and it updates in place: its state word follows the task, and a start that failed or a turn the model service cut off reads as such on it, in the danger color, with why in plain words (a raw error stays in the task's details; one naming no known cause reads "Didn't start. Open it for details."). Receipts (done, stopped, failed to start, interrupted), reviews and reports are not drawn, old ones in the history neither, and nothing in the chat says where progress went. The task's row says it: In Progress and Needs You below. iOS, the web and Android since #8237 (Android: #8312). A failed start's and an interrupted task's Needs you row keeps `--danger-wash` with `--ink` words, as the conclusion cards had (contrast in D10 of `docs/design/task-cards.md`).
- **Rows that act** (#8237): a failed start's row in Needs You carries Retry and Remove, an interrupted one Continue (so does a usage limit nothing continues by itself, in In Progress), right under its words and aligned with them, as buttons of their own, never inside the row's tap. An unrecoverable conversation is the exception: its row says Invalid conversation, shows the full new-conversation guidance and has no Continue. A tap elsewhere on the row opens the task.
- **Finished** (#10922, replacing #8237's Just Finished group under In Progress): an entry of its own after the unfinished work's (on the web after Parked and Persona reviewing; zh-Hans 已完成, ja 完了, the To-dos page's word for Done) listing the handed-out tasks that finished in the last 24 hours, seen or not, newest finished first: a row each with the done mark, the title, Done and what it did (its accepting review's note, why it closed, its receipt's summary, else its Agent's last words). A row nobody has opened since it finished (the To-dos page's Just Finished) is new: the unread green as a plain dot and the state word Just finished, as the Agents list's Just finished rows. Opening a row opens the task and counts as seeing it; it stays, as Done, until its 24 hours pass. The entry is there only while it has rows or its tray is open, drawn quiet (secondary words; the done check's green as its mark on the web and Android), and its number is its rows: finished work, never added to the open-work counts of section 4.8. Why not keep the group: it listed only the unseen rows, so opening one took it out of the tray and only the board had it. Why 24 hours: the window the To-dos page keeps Just finished by, so tasks have one "recent". The entries' row scrolls sideways when they do not fit, on every client (iOS since #10922).
- **Accept and decision rows** stack when their container is narrow (a container query, not the viewport): the words take the whole width, the secondary buttons one row, the primary its own full-width line. Never a menu.
- **Progress rows.** The status word is its own right-aligned column, then a fixed end slot: `›` where the row opens something, a check once finished, an info mark for a usage limit. A task whose Agent is not up says Starting. A usage limit's second line says when it resets. A stopped task that is not done says "Stopped, not done yet · <why>" from its completion check (#3180; iOS too since #8237), else its Agent's last words. The column and the row's leading mark say only what the task's Agent itself is doing (#6492, `web/src/lib/task-row-status.ts`): Working, Needs you, Done not seen, Idle, Stopped (its lead session is gone), Interrupted, Usage limit, Couldn't start, Starting (only while a start is in flight, #7594; a to-do nobody dispatched says Not started); the mark is that Agent's `StatusMark`, the one mark set for Agent states. What the to-do itself is at (parked, waiting to merge, awaiting review, rework, the owner's call, accepted, cancelled, waiting for Start) is a chip after the title, "To-do: Parked"; a to-do with nothing to add has none. Before, one column mixed both, so a parked to-do whose Agent had stopped read Parked.
- **Agent metadata on task rows** (#10904): where a row already counts Agents (the web Overview and the native To-dos lists), a known linked session replaces that count with its computer's display name, its model's existing friendly short name and any reported effort, with "+N" for the other unreleased Agents. The working Agent leads (the most recently moved when several work), otherwise the most recently moved by `max(updated_at, state_since)`. While no linked row is known, the localized "N Agents" fallback stays. Existing delivery/hold text remains before this metadata, and no per-row request is started.
- **Needs you rows with something to answer are folded to one line** (mark, title, what it waits for, the kind) and open in place on a click of the line or its chevron (`aria-expanded`); the card leaves with the answer, and every row is folded again when the tray changes. The conversation's own cards stay in the chat, and their rows close the tray onto them.
- **Persona reviewing rows.** The supervisor-review tray names whose turn it is: its status stays "Supervisor reviewing", even after the task's Agent stops. Its neutral follow-up mark is not an Agent-stopped mark. Finished rows keep their completion mark and label; open rows in In progress use the Agent-only column described above. The separate Waiting for merge group keeps its PR blockers.
- **To-do state chips** use at most half the title area and truncate, so their text cannot hide the task's title on a narrow screen.
- **Hard-coded font sizes.** `text-[13px]` and its kin may only fall: `scripts/ci/web_font_ratchet.py` (`docs/operations/testing.md`).

### 4.8.1 Records of interruptions

A record of something that went wrong (an interrupted task, #3834) says what became of it once that is known: a grey line with the result ("Cancelled", "Dispatched again", "Resumed", "Accepted", "Done"), never a red card that outlives the problem. Several records with one cause in a short time are one card that counts handled and pending and opens to each item. The danger color is only for what is still pending. The hub works out the result and the grouping; the clients only draw them (principle 4; `docs/assistant/unified-entry.md`, "Interrupted receipts after the fact"). In an assistant conversation an interrupted task is not a record in the chat any more (#8237, section 4.8.0): its hand-off card says it, and its row in Needs You offers Continue.

### 4.8.2 Guided setups (step by step)

A setup that takes the user through several things, some of them outside the app (a cable, a restart, a computer), is a guided setup (#8199), built from the same
pieces as the computer page's install guides (cards, buttons, numbered steps):

- **An overview first**: what it does in one sentence, what it needs, each step with its state, the safety rule, one
  button (Set Up, or Continue once some steps are done).
- **One screen per step**: a progress bar of the steps, "Step n of N", the step's title and one sentence, a card with its
  state, and Details folded for the technical words. One primary button at the bottom; a step that waits on the
  world has none, or a secondary one (Cancel, Check Again).
- **State words from `StateName`**: Done, Working (`taskWorking`), Needs You, as chips. Every state comes from the hub's
  answer, never from a tap: a step done elsewhere is skipped, and a screen moves on by itself when its step is done.
- **Asking again only while waiting**: every two seconds while a step waits on something outside the app, and only while
  the page is in front (`.tabActiveTask`).
- **The page the user comes back to** is the same page: its switches (turning it off there), a way to try it, and at
  most one optional switch.

### 4.9 Sharing a conversation publicly

Sharing an Agent session follows the user-control rules: everything that widens exposure (more messages, work
details, future messages, a longer lifetime) goes through a preview and an explicit confirmation of possibly
sensitive content, plus a transport-risk confirmation on an HTTP sharing origin; narrowing and revoking need no
preview, and revoking is a destructive confirmation. Defaults are the narrowest: future messages and work details
off, three days. A paused publication can retain its future-messages preference; resuming it is an explicit action
that requires a fresh preview and sensitivity confirmation, plus transport-risk confirmation on HTTP, even when
the preference is already on. Sharing lists describe effective publication: new messages are added only while
the link is active, publication is live and future messages are enabled; paused, revoked and expired links say
that no new messages are added. The editor retains the configured preference separately from publication state.
The link is shown once with a copy action and is never listed again. The public viewer is a
separate, read-only document with the same palette tokens, plain text only and work details collapsed (section 4.7).
Web: `web/src/sharing/`, `web/public-share/`; server contract in #3604.

### 4.10 Web workspace selection and management

In Agents' repository tree, the workspace title and its leading disclosure button toggle the same remembered fold at every depth. A title also selects the exact workspace, including an empty one; opening an Agent uses that Agent’s own row, never the workspace title or a descendant chosen by proximity. A visible overflow button offers the same actions as the context menu, including on touch devices. The desktop sidebar and phone Agents page share these rows and actions rather than adding a management destination.

The existing `Dialog`, `Button`, `Menu`, `Collapse` and status components carry workspace management. Creation shows its source and starting branch, keeps additional choices in the advanced group, and starts no Agent automatically. An optional Agent start names the exact existing workspace. Creation acknowledgment shows the actual resulting path and branch; an interrupted request offers reconciliation of that same intent.

The workspace catalog, selection and management actions follow the current assistant. Switching assistants retires outstanding catalog reads, clears the selection and invalidates pending mutation previews, including a switch away and back. Management loads on the first workspace action; its loading menu keeps the same dismissal and phone Back behavior, and operation receipts survive menu reopening.

Removal uses the confirmation sheet pattern in section 4.4 because it needs branch and uncommitted-file choices. The dialog identifies the workspace and explains which files and running sessions will be lost. Keep the branch by default; require an explicit discard choice for uncommitted files. Confirmation stays unavailable until the computer supplies a valid preview. A changed target requires another preview, and an uncertain dispatched removal offers status reconciliation rather than another destructive submission. Renaming uses the existing text-entry dialog pattern and preserves workspace, branch and Agent identities.

Backend naming (#7100) is a single collapsed row inside Assistant settings’
Starting tasks group, directly after the workspace preference. It appears only
for a confirmed matching assistant with `prefer-new`; missing or unknown values
keep it hidden. Hiding never clears saved rules. Todo details retain their own
independent advanced entry. Both reuse `Button` and the standard settings input.
Each mount confines import/render failures to a local `PageBoundary` around its
`Suspense`, keeping sibling editors mounted and saveable. Recovery retains the
existing one-time page reload and explicit Reload behavior.
The editor pairs each label with a wide input and small, always-visible template
guidance; labels stack above inputs on phones. It serves people configuring
coding tasks; ordinary tasks need no new fields. Unset fields stay empty and
show their system default in a placeholder; Todo overrides show the effective
Assistant default instead. Clearing a field and saving removes that override
using the existing null patch, without saving a fallback as a custom value.
There are no inheritance status labels or reset buttons. Preview options remain
folded until requested. The preview result shows the task folder once and then
either the root repository's labeled branch or each container repository's
folder-to-branch mapping, never the internal `.` path and never the same absolute
path twice. The computer and layout selectors and the preview action form one
compact row, stacked full-width on phones; a task's repository choices sit above
that row, and the result carries no routine planning time, time zone or
disclaimer text. A failed save retains its draft; reloading
updates the saved baseline without replacing local edits.
After a task save, refresh its saved revision and effective Assistant defaults
without replacing the draft, computer, layout or remaining repository selection.
If that read fails, keep the acknowledged save and offer the same read recovery.
Changing a field, computer or assistant retires earlier previews. The panel
changes no creation mode and starts nothing. This is a secondary settings
surface, not another workspace management destination.

### 4.11 Questions of several

A card that asks several questions at once shows where it is and lets the answers be checked before they go (a pending
request, section 4.8). That is a terminal dialog of several (#2970), and a hook question of several:
Claude Code's AskUserQuestion, the assistant's ask_user and Codex's questions.

- **Previous / Next** move one question at a time. A question gone back to shows the answer it had.
- **A single-select pick** moves on to the next question by itself after a short pause (250 ms), once, and never past
  the last one. A second pick within the pause starts the pause again, so the card moves a whole pause after the last
  pick and its tick is seen; Previous / Next meanwhile cancel the move (#6549; two taps within the
  pause used to skip a question).
- **The plan pill folds** while a card asks something, so it never sits over the card's buttons.

### 4.12 Keyboard in web lists and forms

For people who work from the keyboard on the web (web audit, 2026-11-14). Every list-and-detail page and every form
follows these; the To-dos page is the reference (`web/src/views/Todos.tsx`, checked by `Todos.narrow-keyboard.test.tsx`,
`Todos.plan-fields.test.tsx` and `web/e2e/todos-keyboard-layout.mjs`).

- **A list and its detail at a phone's width** take turns (the list is hidden under the detail). A row opened with Enter
  or a click moves the focus to the detail's Back, as a page pushed on iOS does. Esc goes back as Back does, except from a
  field being edited or under a dialog or a menu, which own Esc. Back on the list, by Back, Esc or the browser's Back,
  the list is scrolled where it was and the focus is on the row that was open, a detail opened by a link too. Side by
  side, nothing moves: the focus stays on the row and Esc does nothing. Why: hiding the list with `display: none` drops
  the focused row's focus to `<body>`, and the next Tab starts from the top of the page.
- **Date and time fields** (`<input type="date">`, `time`, `datetime-local`) save once the value is entered: on leaving
  the field or on Enter, once however soon the field is left after Enter, and at once for a pick from the browser's
  picker. Never per typed segment (the "1" of a day "15" is already a valid date), and never disable a field while it
  saves, which takes the focus away mid-edit (`PlanField`). A half-typed field goes back to the saved value.
- **⌘↩ / Ctrl+↩ submits** a dialog or a box from its multi-line field, as the composer and New Agent do; Enter alone is
  a new line, and the Enter that ends an IME composition does neither.

### 4.13 Keyboard and overlays (web)

- Esc undoes one thing at a time, the innermost first: a search field with text clears it (`SearchField`; an empty one lets Esc go on), a menu's rename goes back to the menu, open with its Rename row highlighted and the focus on what opened it, a menu closes onto its button, and then the topmost overlay closes (`useEscapeClose`). A modal closes on Esc from anywhere inside it, not only from its first field, and gives the focus back to what opened it.
- One modal from the keyboard at a time: the ⌘K palette and the ⌘/ shortcut help replace each other.
- Tab stays inside a modal and reaches every control in it, including the ones after a scrolling body the browser lets the keyboard focus (`trapTabIn` in `web/src/ui/overlay.ts`).
- A menu is as tall as the window allows and shows whole when the window can hold it, over its button if need be; only one taller than the window is cut, opening beside its button on a wide screen. A fade at a cut edge says there is more (`Menu`, `data-menu-more`), as the team panel's list does, and a row the keys move to past the cut scrolls into view.
- A choice kept in the browser (appearance, theme color, text size, language) reaches every open tab: the others follow through the `storage` event. A language needs a reload, as in the tab that chose it, and that reload never lands on a draft or an open dialog or menu: it waits for a calm moment, as a new build's does, with a toast that offers it meanwhile.

### 4.14 Starred messages (web, #7524)

People who want to keep a useful message choose **Star** in the existing message menu. A small accent star at the
message edge shows that it was saved without moving the row. While the save or removal is pending, the same position
shows a spinner. Failed actions keep the prior state. **Unstar** removes the snapshot and offers the shared Undo toast.

**Starred** is a secondary page under **Pages**. Its badge counts every saved snapshot for the selected assistant.
The page uses the existing search field, neutral cards, Markdown, and action buttons, with the sidebar button on small
screens. One card contains a title, speaker, timestamp, saved text, and **Open original**, **Copy**, and **Unstar**
actions. Long saved text folds in the card. The original uses the existing transcript jump. The saved text stays usable
when the original cannot be opened.

## 5. Content

### 5.1 Voice and tone

Calm, direct, second person, like a capable teammate. Contractions in English ("can't", "didn't"). No exclamation marks, emoji, "Oops" or "Sorry". The assistant says "I" only in its own chat surfaces (greeting, intro, sample prompts). Elsewhere the app is "Persona" and agents are named in the third person. "Please" only when asking for something burdensome. The hub's HTTP errors break these rules (uncontracted, "please" heavy).

### 5.2 Capitalization and punctuation

- Web: sentence case for buttons, titles, menu items, descriptions, footers, empty-state lines, section headings and accessibility labels.
- Agent states: "Needs you", "Working", "Unread", "Seen" in prose; Title Case only as a button or title.
- One ellipsis character. Use it for actions that open another step and for in-progress states.
- Complete sentences end in a period; labels and titles do not. Settings paths use the single angle quote.
- Apostrophes and quotes: pick one style (Apple en is 94% straight apostrophes with curly quotes; DS-28).
- A heading that holds a name (a space, a computer, a project, a pane) is drawn as written. The web's small-capital label style (`uppercase`) is for label headings only; on a name it rewrites what the user typed ("weather" as "WEATHER"). Web: `Menu`'s `headerAsWritten`, the palette's `sectionAsWritten`.

### 5.3 Error-message structure

1. What happened: "Couldn't + verb + object". Use "Can't" only for something not possible, not a temporary failure.
2. What is safe: "What you've typed is kept."
3. One next step: "Check the connection and try again."

No HTTP codes, key algorithms, CLI flags or raw error text in the primary line. Do not append raw detail after a colon without a plain lead. One generic fallback: "Something went wrong. Try again."

### 5.4 Buttons, confirmations, notifications

- Verb first, one to three words. "Retry" everywhere (not "Try Again"). "Deny" for permissions.
- Delete destroys data; Remove detaches from a list; Discard throws away unsaved edits; Forget removes a pairing or credential; Clear is bulk reset.
- Confirmation: title "Delete this chat?", body states the loss and whether it can be undone.
- Notifications: title names the actor and the need ("{name} needs your permission", "{name} has a question", "{name} finished"); body is a short call to action. One wording per event on every surface, kept in the hub catalog. Never include secrets or commands.
- Permission purpose strings: "Persona does X when you do Y, for example Z." One or two sentences, no CLI text, correct device name, all three languages.

### 5.5 Glossary and localization

| Term | Rule |
| --- | --- |
| Hub | product term; "the hub machine" for the machine |
| Computer | the user's own machine; "Machine" only for list and monitoring entries |
| Node | never in user-facing English |
| Agent | lowercase mid-sentence; "Agents" for the tab |
| Session | what an Agent runs, titled by what it is doing. Not "task", which names the work Persona hands out |
| Persona | the product and the main assistant |
| Assistant | one of the hub's assistants, each with its own chats, inbox and computers (`manager` in code, the protocol and docs); zh-Hans 助手, ja アシスタント. Never "manager" in user-facing text |
| Lark | in en and ja, except where Feishu is a separate option |
| sign in / sign-in | verb / noun |

Full glossary: `docs/protocol/i18n.md`. Languages: en, zh-Hans, ja. Hub strings are in the server locale files. Keys with a bracketed suffix are translator-context tags; the English value strips them. Japanese uses no space between Japanese and Latin text (the catalogs do not follow this yet; DS-29). Do not concatenate sentences.

### 5.6 Objects, titles and state words

| Object | Called | Home | Title |
| --- | --- | --- | --- |
| Assistant conversation | conversation | the Assistant page | its title, generated or renamed |
| Agent session | session | the session page | what it is doing, from the user's first message or a rename; never the repository or tab |
| Pending request | Needs You | its session's pending area | the thing wanted: the plan's title, the command or the question |
| Inbox item | message, mail | the Inbox | the sender or group, and a summary |
| Computer | computer | the Machines page | the user's name for it |

Titles of plans, permissions, questions and terminal prompts, the same on every client and screen that names one (cards, list rows, the Inbox, the chat panel's label, the hub's push titles). iOS writes them in title case, the web, Android and the hub in sentence case. A notification may put the agent's name in front; a label that cannot name the agent reads "Has a question" (在问你, 質問があります). The line under a title says what is asked (the command, the plan's title, the question) and keeps its own words; a row with one line puts the title first ("Waiting for your approval: swift test").

| Request | en | zh-Hans | ja |
| --- | --- | --- | --- |
| Plan | Plan awaiting approval | 计划等你审批 | プランの承認待ち |
| Tool or command | Waiting for your approval; {agent} is waiting for your approval | 在等你批准；{agent} 在等你批准 | 承認を待っています；{agent}が承認を待っています |
| Question | {agent} has a question (Claude by default, Persona for its own) | {agent} 在问你 | {agent}から質問があります |
| Terminal prompt | Terminal needs you | 终端等你处理 | ターミナルで対応が必要です |
| A kind the client does not know | Needs you | 需要你 | 要対応 |

Notification titles for a tool approval (the hub's pushes, the web's browser notifications) still read "{agent} needs
your permission" (section 5, "Notifications"); moving them to the card's words is a follow-up of #4883.

- A session's title is its top-bar title and its row's first line.
- Primary screens (list rows, top-bar titles, the Assistant home and notifications) never show raw model IDs, session IDs, paths or engine names (herdr, Orca, Paseo); this is principle 2 applied to Agent work. A row may name the model by its short name.
- An agent an engine runs under the engine's own id (Paseo's providers: Grok, Kimi, Cursor…, whose `agent` is `paseo`) is called by its provider's label (`agent_label`) and drawn with the mark the client has for that provider, else its label's letter; cards, pushes, filters and model pickers name it the same way (#8142).
- Views grouped by location (By Repository, By Machine, By Project) may show workspaces, tabs, worktrees and branches. The web Agents row may also carry an associated terminal tab as the secondary **Tab {name}** metadata described in 3.4; it is not the session title and requires a real tab association. Other location details stay in the session's info sheet, opened from its title, and in tool views (terminal, files, changes).
- Each level of such a view says something its heading does not. A level that would only repeat the name above it is left out, and the heading carries what it had: its marks, its ⋯ and its right-click menu. By Repository lists a lone workspace named after its repository's folder under the repository's heading, as By Project's space headings carry a space's menu (#13454). A row under these headings leaves out the place they name (3.4).
- State words come from one place per platform, `StateName` on Apple and `web/src/lib/state-labels.ts` on the web; no screen invents its own.
- An Agent page says the agent's state once. On iOS and Android that is the header's title line (dot and words); other
  marks in the header count or describe (the loop and background pill is a count by a neutral glyph, #3719), and only the
  space's tabs color each agent by its state. The web's top bar says no state (#5421, cindy's): the running status bar
  over the composer says it while the Agent works, the pending cards while it needs you, and its row in the sidebar always.
- A row shows its most urgent state, in the order `AgentListOrder` ranks them.
- A waiting to-do is named by what it waits on, never one "Waiting" for all (#11090, web): **Waiting on you** in Needs you's attention color only when it waits on the owner by the sidebar badge's predicate, every other wait and an unknown one ("Paused (nothing needed from you)") in a neutral tone; the words are in `docs/protocol/i18n.md` ("Agent states").
- A failed run has no state of its own. It shows its state like any other run, and its row's line 2 says what failed, in red.
- A Codex opened with no first message (`awaiting_first_message`) has no state of its own either. It is idle, never Needs You or unread, and sends no push.
  Its row's line 2 says "Waiting for its first message" (web `sidebar.codex_waiting` through `rowLine`, Apple `Session.listPreview`).
  Its chat says once, while it has no messages, that Codex is open on the computer and takes its first message below or in its terminal (Apple: the chat's empty state, in place of its generic line; the web: the chat's `codex-waiting` note).
  Once the message goes in, the page moves to the thread's session in the same pane (`docs/integrations/codex.md` "Starting without a first message").

## 6. Dark mode, Dynamic Type, accessibility baseline

### 6.1 Dark mode

- Every color is a light/dark pair; tokens are dynamic colors.
- The Light & Dark choice sets the interface style on every window, so sheets, alerts and web views follow. Code colors are the same in both modes.
- Web: class on the root, set before first paint, plus a `color-scheme` meta.
- Web terminal: Terminal settings › Appearance (Match page by default, Light, Dark) gives the terminal's screen one scheme's page tokens with `[data-terminal-scheme]`, theme color and extension theme included, never a palette of its own (`docs/integrations/terminal.md` "Appearance").

### 6.2 Dynamic Type

Text size on the web is the Small / Standard / Large setting (section 2.2).

- Text uses the font levels only. Shapes scale with the text size.
- Layouts survive the largest text size without clipping. Never shrink text to hide truncation of body text. For button labels, follow rule 3 in section 1.2 (shorten the copy, reconsider the control, stack the group when it does not fit).
- Caps are allowed for glyphs in fixed-size buttons and for top bars that must fit a small screen. They must be named, and the control must keep a 44 px hit area.

### 6.3 Accessibility baseline (the contract every component meets)

1. Hit area at least 44 x 44 px, including icon buttons whose visual is smaller. This is Persona's rule and is stricter than Apple's minimum: Persona uses the 44 x 44 default as its floor.
2. Every control has a spoken label; icon-only buttons never rely on the glyph name. Decorative marks are hidden when adjacent text says the same.
3. Selected, expanded and busy state is exposed with roles or values, not only color or shape.
4. State never rides on color alone: pair with text or a symbol.
5. A gesture-only action has an accessibility action or visible control.
6. Animations: transitions and press effects respect Reduce Motion; every loop stops with it.
7. Text contrast at least 4.5:1 (3:1 for text of 18 px or larger, bold text, and non-text marks), verified for each style and theme color. Composite cases (text on a tinted fill) need a test.
8. Layouts survive the largest text size without clipping (section 6.2).
9. Transient messages are announced to screen readers.
10. Headers carry the heading role; modal sheets trap focus.

No test enforces items 1, 2, 7 (composites) or 8.


## 8. Inconsistencies and duplicate definitions

| ID | What | Where | Impact | Recommended unification |
| --- | --- | --- | --- | --- |
| DS-01 | Needs-you color: blue by the decision of 11-11, amber on the web | `web/src/styles.css:233` | The web shows another color for the same state | Move the web to the blue (its own issue) |
| DS-31 | Web working and caution are one color: `--status-working` is `var(--orange)`, the caution role, so a working task and a stuck or awaiting-review one differ only by their word | `web/src/styles.css:264` | Working and stuck read as the same urgency at a glance | Give the web an amber working token of its own (with DS-01's move of needs-you to blue), then point `--status-working` at it |

## UI change self-check

Run this before opening a PR that adds or changes UI or an interaction, and paste the list into the PR with each item marked
done, not applicable, or deviated (with the reason). It is short on purpose. The sections cited hold the detail.

**Principles**

- [ ] Defaults first: works on a fresh install with untouched settings, and the default behavior fits in one plain sentence (principle 1).
- [ ] Progressive disclosure: advanced options are off by default and live in Settings or an advanced group, and the primary screens are unchanged when the feature is off (principle 2).
- [ ] User control: every new behavior-changing setting is visible, changeable and reversible. If Persona or an Agent can change it, the PR names the explanation, the confirmation and the rollback (principle 3).
- [ ] Consistency: lists the existing components, patterns and count definitions reused; a number shown under an existing name uses the same rule. Anything new has its reason written down and an update to this document (principle 4).
- [ ] Model and product separate: names which of speed, model, presentation or features it touches, and changes none of the others (principle 5).
- [ ] Checkable: each item above marked done carries its evidence (screenshot, test name, command output) in the PR (principle 6).
- [ ] Fast, local-first and stateful: local Sending is immediate, hub receipt shows Processing, failure recovers; before/after feedback and execution timings are separate (principle 7).

**Rules**

- [ ] Only tokens and design-system components: no literal sizes, colors or fonts, or lookalike components (sections 1.2, 1.3).
- [ ] Existing styles and theme colors look pixel-identical, except for a change that brings a screen in line with this document (section 1.2).
- [ ] Hit areas at least 44 x 44 px; every control has a spoken label; gesture-only actions also have a visible control or an accessibility action (section 6.3).
- [ ] Destructive actions: confirmed if irreversible, offer Undo if reversible (section 4.4).
- [ ] Motion respects Reduce Motion (sections 2.7, 6.3).
- [ ] Anything that opens or closes in a chat keeps the tapped row where it was, at the newest end and the oldest too (section 4.7.1).
- [ ] New copy follows section 5 and ships in `en`, `zh-Hans` and `ja`.
- [ ] Screenshots of each changed screen in light and dark, at the default and at the largest text size, are shown in the PR, not committed.
- [ ] Structure: in every grouping a session that needs the user is visible without opening anything; rows follow the work-item row; primary screens show no raw model IDs, session IDs, paths or engine names (sections 3.4, 5.6).
- [ ] Review size: says whether the change is large or small (section 1.4); a large one has had a person look at its screenshots.

## Configuration forms (web, #9877)

A module's settings described as a configuration Document (`docs/web/web.md`, "Configuration forms") are drawn by one
generic form, never a page of their own: the document's title as a group heading (`text-[12px]` `ink-3`) with its
description under it, then each group as a settings `Group` (its title above, its description as the footer). A field
is a row: the title in the row's label type, its description in the hint type, the control, any error adjacent under it,
any notice (orange for a warning, `ink-3` for info), the disabled reason or "Read only", and the module's details as a
two-column definition list in `text-[11.5px]` (`ink-3` terms, `ink-2` values, the page's existing `grid-cols-[auto_1fr]`).
Every field ends in one footer row: the default value, the scalar count where a text field has a maximum, the polite save
state (**Saving…** with a spinner, then a green **Saved**) and the quiet **Reset** / **Cancel** / **Save** actions that
apply, so the actions never sit between the control and its explanation and a short default keeps them on one line.
The footer reserves the small-button height when no action is shown, and longer content can still wrap.
- Controls, all existing: a toggle is the `Switch` at the row's end; a select is the `Segmented` control at the end
  (`segmented`), a native select in the settings field style at the end (`dropdown`) or `ChoiceRows` under the text
  (`list`, the default); a multiselect is a list of `role="checkbox"` rows with `CheckMark`; text is the field style's
  input or textarea, a number a narrower input with a decimal keyboard. Text and numbers are drafts, and a multiselect's
  checks are the same kind of pending edit, until **Save**; a multiselect whose maximum is one is drawn as a single choice
  (`role="radio"` rows with round marks and arrow keys), with exactly one Tab stop — the chosen option, or the first when
  none is chosen (no radio is marked chosen) — and choosing another option replaces the old one in one click (the wire
  value stays an array), while a larger maximum keeps ordinary checks and can pass through a temporary count. **Reset**
  appears where the value differs from the default.
- Whole-form lines (a version it can't write, a conflict or an uncertain write with **Reload**, a refresh that failed with
  **Try again**) are one card above the groups.
- An unknown kind or invalid definition keeps its title, description and details, with an orange line instead of a
  control. A known field whose saved value is missing or unreadable shows an empty input or unselected choices, including
  an On/Off choice for an unreadable switch. The hint is part of the control's accessible description. Choosing or
  entering a valid value repairs the field, and **Reset** returns the default.
Why one form: the user asked for settings that modules inject with one portable contract and shared styles, so a new
module adds data, not a page, and the same states (pending, saved, failed, conflict, read only) look the same everywhere.

**Labs** (`docs/web/web.md`, "Labs") is a sidebar group of its own without a title, with the existing `sparkles` glyph (a
dedicated one would not fit the first screen's budget); its page is an introduction card with a note under it that each
item states its own scope, then one configuration form per module. Where a feature also has its own options menu
(Terminal settings), it shows the current value read-only with a button that opens the same Labs page in a `Dialog` over
the page (never navigating away, so nothing open behind it, such as a chat's unsent draft, is unmounted) instead of a
second switch, so there is one place to change it.

An experiment can supply a permanent note under its introduction or beside a field, using the existing info and warning
hint styles. A choice with consequences can request confirmation for a specific change. Use the shared Dialog, with the
affected scope, impact, timing and an action-specific button. All dismissal paths preserve the edit. On success the
field’s polite save status can explain when the choice takes effect. Show this receipt once, in the field footer.

## Extension entry lists

A declared list of extension accounts, gateways or similar entries (#9504) belongs in the existing **Settings** group; it does not create a page, tabs or an extension-owned layout. The audience is an everyday owner managing a small list, so rows use the entry's name and plain state words, never its host-owned ID, array index, secret name or engine terminology. Reuse settings `Group`, `Row`, `Button`, `IconButton` and `Dialog`, the existing radius, surface, hairline, spacing and type roles; do not introduce a second card style or new tokens for one extension.

- Each entry is one surface with a title and secret state, then its declared flat text fields. Prefer its `name`, then its first nonempty field, then a localized numbered title. **Secret** opens the shared write-only secret dialog and names both entry and secret. Before the entry is saved, the button is disabled and the reason says to save first. Entry secrets never also appear in the fixed Secrets group.
- **Add entry** appends one empty entry and is disabled at the manifest's maximum. All field edits and additions remain drafts until the page's existing **Save**. The host owns saved entry identity; an unsaved browser key must survive immutable field edits without becoming a visible value or part of the JSON payload.
- Removal uses the shared confirmation `Dialog`, names the entry, and says that the entry and its saved secret are deleted **when Save succeeds**. Cancel leaves the draft untouched. A required list keeps only its declared minimum; an optional list can always return to its valid absent state even when a present array would have a positive minimum. If an address participates in configured-network permission, a nearby hint says that saving a changed address clears that entry's secret. These are consequences of the pending configuration write, not claims that merely opening or dismissing the dialog changed hub state.
- Text and explanations wrap at phone width and the largest accessibility text size; buttons stay on one line using the existing button behavior. Destructive actions remain reachable without horizontal scrolling. Dark and light themes use the same semantic roles as every other settings card.
## Sidebar icon colors (#10822)

Who it serves: anyone whose sidebar lists many pages and who finds it quicker to scan when each page has a color. It is
an option, off by default (principle 2): Settings › General › Appearance › **Sidebar icons**, **Monochrome** or
**Colored**, kept in this browser as the theme color is (the web has no such option). Monochrome is the sidebar as
it was, pixel for pixel: every page's line icon in `--ink-3`, the open page's in `--ink`, and nothing is stored.

With Colored:

- Each built-in page's icon takes the fixed named color below (`Color`, `schema/extension/view.schema.json`), drawn in
  the web's existing `--hue-*` tokens through the same `hue()` a row's `icon_color` uses
  (`web/src/lib/extension-ui-v6.ts`). No color value is added.
- An extension's `nav_page` takes the `icon_color` its manifest names (`docs/extensions/extensions.md` "UI slots"); a
  page without one, or with a name no client knows, stays neutral.
- The open page keeps today's rule: its icon is the open row's `--ink`, not its color, so the open page stands out as
  before. Under the pointer the color stays and the hover fill shows.
- Everywhere the sidebar draws its pages: the Pages list and the groups, More, the folded icon rail and a phone's grid
  of pages. The More menu, ⌘K and Settings › Sidebar pages keep neutral icons; the main chat's row, All chats and the
  Agents' rows are not pages and are unchanged. State marks, dots and counts never take these colors (#4538).
- Theme colors and extension themes move neither the `--hue-*` tokens nor the sidebar's backgrounds, so the colors and
  their contrast are the same under each of them; the text size does not scale icons.

| Page | Icon | Color | Light: on `--canvas` / `--hover` | Dark: on `--canvas` / `--hover` |
| --- | --- | --- | --- | --- |
| Overview | `home` | amber | 3.26 / 3.35 | 9.05 / 7.59 |
| Inbox | `inbox` | orange | 3.01 / 3.09 | 7.16 / 6.01 |
| Health | `heart` | pink | 3.82 / 3.93 | 6.44 / 5.41 |
| Messages | `chat` | green | 3.25 / 3.34 | 6.87 / 5.76 |
| Workflow board | `layers` | yellow | 3.25 / 3.34 | 10.89 / 9.14 |
| To-dos | `todo` | blue | 3.23 / 3.32 | 5.84 / 4.90 |
| Scheduled tasks | `clock` | purple | 4.25 / 4.37 | 6.53 / 5.48 |
| Knowledge | `book` | indigo | 4.79 / 4.92 | 5.80 / 4.87 |
| Machines | `machine` | teal | 3.65 / 3.75 | 7.15 / 6.00 |
| Settings | `gear` | gray | 3.54 / 3.63 | 6.27 / 5.26 |

Each page has a color of its own. Red (3.55 / 3.65 light, 5.09 / 4.27 dark) is left to extensions: on a built-in page
it would read as an error. An icon is a graphic, so the floor is 3:1; `web/src/ui/theme-contrast.test.ts` checks all
eleven named colors on both backgrounds in light and dark, since an extension may name any of them, and the closest is
light orange at 3.01:1. Why a new pattern: one ink let only the open page stand out; Colored keeps that and adds a
second cue for scanning, so it is an option and never the default. iOS and Android follow with the same setting, the
same map and the same `icon_color`, so a page has one color on every client (#10822 tracks them).

## A chosen page that is not there now (#9071)

A setting that names something which can come and go (the web's **Open on**, Settings › General › Start page: a page of
the sidebar, an extension's included) keeps the user's choice when its target goes away, and the product uses the default
meanwhile, without a toast or a dialog at the moment it happens. The setting itself says so: its menu keeps the choice
selected under a plain name (**A page that isn't available now**, not its id), and the row's line under the label, the
`Row` hint in `--ink-3`, changes from what the setting does to why it falls back and until when (**…opens on the main chat
until it's back**). Choosing anything else in the same menu replaces it. Reason: the target is usually away for a while
(an extension turned off, another default assistant), so clearing the choice would undo the user's decision behind their
back (principle 3), and a toast at every opening would be noise for something they can read where they would change it.
The same rule as a chosen look whose extension is switched off ("the choice stays: the default only shows meanwhile",
`server/internal/hub/looks.go` `looksGone`). It reuses Settings' `Row`, its hint line and a native select; no new component.

## Arranging cards in place (#9745)

Overview's extension cards (web) are arranged where they are drawn, not in a settings list: the result is a layout (which
card is wide, which sit side by side), and a list would show neither widths nor neighbours. Overview's top bar has
**Arrange Overview** (`IconButton` with the `grid` icon, `aria-pressed` while on) beside **New agent**, always: Overview's
own sections are arranged with the cards (#10905). The edit state reuses the sidebar pages editor's pieces (`SidebarPages.tsx`): the `grip`
handle and an HTML drag onto another item, which takes its place; ↑ / ↓ `IconButton`s at 26 px for the keyboard and touch;
a native select for the width (**Half row** / **Full row**); `Switch` for shown or hidden; the hub footer's words for where
it is kept; and **Reset** with Undo in a toast (4.4). Each card keeps its surface and its content, made `inert`; its head
becomes the controls, which wrap under the name on a narrow card. A hidden card is drawn only while arranging, as its
hairline outline (`shadow-hairline`, no surface) with "Hidden: not loaded" in `--ink-3` under its name; the card a dragged
one would land on has a 2 px inset `--accent` ring. No new token or component (`web/src/views/extensions/OverviewArrange.tsx`).

Overview's own sections (#10905) take part in the same edit state, in their places among the cards: each is drawn as a
bar the row's width, on the card surface (`rounded-card`, `bg-surface`, `shadow-card`), with the same `grip` handle and
drag, its name as its heading would be, and the same ↑ / ↓ and `Switch`; no width select (a section always takes the
row). Its rows are not drawn while arranging: a section can be long (every computer, every working agent), and its place
is what is being chosen; a section with no rows now gets its bar all the same, so it can be placed before it fills. A
section that holds what needs the user (Needs you, Stuck) has no switch and says "Always shown" under its name in
`--ink-3` (3.4: a session that needs the user is never hidden); a hidden one is the same hairline outline as a hidden
card, "Hidden" under its name. The greeting and its summary are not part of it and stay first. No new token or component.

## Arranging conversations in place (#12017)

The Assistant's ordinary conversations use the same progressive-disclosure pattern in the sidebar's Recent and in All
chats. **Arrange chats** lives in an existing row/display menu, so an untouched list has no extra handles or buttons.
While arranging, each row keeps its own content, made `inert`; a grip starts an HTML drag onto another row, and named
**Move up / Move down** `IconButton`s provide the same action to keyboard and touch users. The edit heading explains
that the order is shared, with **Use activity time** (Reset with Undo) and **Done**. The main conversation and the
Directions group stay fixed; a new or not-yet-loaded conversation remains visible in a deterministic place rather than
being discarded. Android uses the same edit state and actions with its native long-press drag pattern. With no saved
choice, the existing activity-time groups and rows are exactly as before. This is presentation only: it neither pins a
conversation nor changes supervision. The shared row and edit heading add no color, spacing or elevation tokens.

## Choosing conversation tag filters (#12018)

All chats keeps its automatic row of one chip per tag in use until the user chooses **Customize tag filters…** in the existing display-options menu. Editing happens above the same list, not in a detached settings page: an exact-tag field and **Add filter**, a small × beside each chosen chip, **Reset to all in-use tags**, and **Done**. Chips still filter the list by exact tag while editing. Removing the selected chip also clears that filter rather than leaving an invisible selection; Reset restores the automatic chips. A deliberately empty custom row says **No tag filters** instead of silently refilling itself. Saves are optimistic and a refusal restores the previous entries and reports the error. The hub stores the exact entries and their order for every client; null means automatic and an empty array remains a real choice.

The filter bar is a controlled component: its caller owns the entries, order, selection and editing state, and the bar owns only its draft and pending action. It can therefore move into another pane without moving conversation-list state with it. Default chips have no persistent editing controls, a fresh install with no tags has no empty row, and no sidebar structure, navigation pattern, token or dialog is added.

## Values a form fills in by itself (#4975)

Where a form can work out a value from what the user already gave it (the web provider wizard's Claude Code tiers, read
from the checked models), it fills the value in and marks it with a neutral **Auto** `Pill`, in the summary and on the
field, with one line under them saying that automatic values follow what they were read from until the user changes
one. A value the user picks is never filled in again, an empty one ("Same as default model") included. When what it was
read from changes (a model unchecked, another default), only values still marked Auto follow, and one that no longer
fits goes back to empty. Something already saved is never refilled when it is opened: when what it holds would read
differently now, one line under the summary says what it suggests and **Apply** (the status line's action) takes it.
The line warns (`--orange` with the warning mark) when the current value costs more for no reason, such as light
requests running on the heaviest model while a lighter one is checked, and otherwise suggests (`--ink-2`). The summary
is the inset well (`rounded-card`, `bg-inset`, `shadow-hairline`) with label / value rows; **Adjust** is an accent text
button with `aria-expanded` that swaps the rows for their fields in place, and rarely changed fields stay under
Advanced (principle 2). Reason: a default the product can work out should not wait behind a folded group (principle
1), and an automatic value the user cannot tell from their own, or one that changes after they set it, would take
control away from them (principle 3). This reuses the wizard's field, pill and status-line styles and adds no setting,
style or motion.

A value the product measures rather than reads follows the same rule with a line instead of a pill: the wizard's sign-in
method (#5329). On **Automatic**, the default under **More options**, the Connect step's own check finds whether the
gateway takes Bearer or x-api-key, and one quiet line in the check box names it (`--ink-2`, no icon, after the test
result and before the model count), with **Change** (the status line's action) opening More options and focusing the
choice; the `Segmented` there offers Automatic, Bearer and x-api-key. A saved provider's line names its stored method the
same way. Before the check has found anything the line is absent rather than a guess. Reason: a question with a right
answer the product can measure should not be asked (principle 1), and the line keeps the answer visible and one tap from
changing (principle 3).

## Codex speed controls (#3865)

An Agent's Codex thread sets its speed with Fast in the model panel's flyout of the model it runs (#5032, the model pill
and unified model panel in 3.8); a Persona chat running Codex does the same since #5033, its effort chip and Speed
submenu retired. Fast is a toggle showing the current value, and its label explains the increased usage. A change
waits for native acknowledgment and the session push; turning Fast off is the reversible choice. Settings > Agents >
Codex uses the existing agent-settings row for the native default. No account or session is automatically switched to Fast.

The iOS main Assistant conversation offers the same current-session choice inside its existing composer Model / Thinking
Effort menu (#10244), not in Settings and not on other chats. A native **Speed** section is present only when the session
advertises the known tiers; **Standard** and **Fast** are localized plain labels rather than native display metadata.
Fast's detail says **Uses more quota**, and a separate row says **Applies next turn**. Working leaves the choices
available; an offline or read-only action is disabled beside a one-line reason, and an outstanding change says **Changing Speed…**. A pick sends only `service_tier` in its own
request and never changes model or effort. The checked value is only the hub-acknowledged `service_tier`, never an
optimistic pick or an inferred Standard; summary updates preserve it and the advertised tiers. Refusal and timeout show a
visible error and leave the last acknowledged checkmark. The section reuses native menu switches and adds no token,
component, style or navigation pattern.

A new Persona conversation on the web reuses that same model flyout (#6240), inside Agents-facing customization rather
than adding an everyday-facing setting. While the default manager is in use, the hub feature
`assistant_start_service_tier` comes from that manager's hello, and a selected model whose native `service_tiers`
advertise `priority` has its Fast toggle available before the first message; other models, managers and older hubs do not
show it. The local Fast choice and reasoning effort are independent (principle 5), enabling Fast pins the displayed
launcher, model and effort, and an untouched start preserves the native default (principle 1). The first request carries
only an explicit choice, which must be natively acknowledged before the first turn; a refusal is recoverable and never
silently means Standard (principles 3 and 7). A successful start, Follow default, a dropped pick or a manager change
clears the one-off draft; a refused start retains it. New Agent has no start speed control.

## Hub connection password (#4474)

The administrator's Devices page owns the connection-password switch and registration form. It is off until an
administrator sets a password and enables it. The form uses the existing admin Card, Button, ErrorBox and toast
patterns, and explicitly offers revocation of existing device credentials. Disabling password sign-in or replacing
the password is visible here; neither action silently removes devices unless the administrator selects revocation.

The web sign-in page shows the password form only when the hub enables it, alongside the existing pairing entry.
Native pairing offers password sign-in with a hub address and keeps scan/link pairing reachable. The system browser
owns password entry so all clients use the same OPAQUE implementation; the callback carries a single-use code bound
to the initiating app rather than a reusable device token. Password sign-in requires trusted HTTPS, with QR pairing
remaining available for self-signed deployments. This changes connection features only, without selecting a model,
speed, persona, style or theme color. Android reuses PairingHeading, JCard, JTextField, JButton and SettingsCaption;
Apple reuses the pairing Form, Section, Picker, toolbar and Theme tokens.

## Checking typed credentials by themselves (#4881)

Where a form exists to connect to something with an address or key the user types (the web provider wizard's preset
key step and its custom gateway's Connect step), the check runs without a button: once what it needs is valid and has
stopped changing for 0.7 s, it runs once for that input. An address being typed counts only once focus moves from its
field to something else on the page (not when the window loses focus), the step is left, or a paste fills all of it,
so a key never goes to a half-typed host; until then the form says leaving the field starts the check. It never runs
on opening something already saved, only after the fields the form names as its connection change (the provider
wizard: address, key, sign-in method, request headers); fields that play no part in the connection (a name) never block or repeat it.
The optional request-header editor stays visible on the custom gateway's Connect step before the check, in add and edit,
with configured names but never saved values and with the selected runtime's support or limitation beside it. Reserved or
unsupported headers stop the check instead of letting it imply a launch would send them. A header-only edit invalidates
old results and starts the same 0.7 s check. Each part runs whatever an earlier part said, since one part failing does not prove another will (a key may chat
without listing models); a part that is running or passed is not sent again. A line says it will check by itself, then
shows progress with a spinner, then the result; a failure keeps the server's words and offers to run that part again
(Retry on the Connect step). Manual buttons, where the form has them, and the note that a test may cost a little stay;
pressing one counts as the check. Once the check found what the next step needs, that step's button is the one primary
action (section 3.7). Reason: the user's next question after pasting a key is whether it works, and the preset path
already answered it this way; two paths in one dialog should not differ. This reuses the existing Button, Spinner and
status-line styles and adds no setting.

## Provider display names (#5029)

Wherever the web names a model provider or launcher (New Agent's choices and presets, the model settings, the
profile's Agent presets, the work-session and to-do launcher menus, task and schedule cards, a computer's launcher
list, toasts and dialog titles), it leads with the provider's display name in the element's normal text style and shows
the startup ID as secondary text: small monospace in `--ink-3` where a row has room, the `title` tooltip on chips and
buttons, "Title (id)" in a native `<option>`. A launcher with no display name (an rc alias, plain `claude`, a provider
from an older hub) shows its ID in monospace as before. One module decides it, `web/src/lib/provider-names.ts`; a page
that holds only an ID looks the title up there, and a row that shows the ID beside the name draws both with
`ProviderName` (`web/src/ui/ProviderName.tsx`). The provider wizard asks for the display name first (any language, at
most 64 characters; left empty, the hub takes the address's host) and keeps the startup ID under More options, where
the hub makes one up when it is left empty and its format is checked only once something is typed. A saved provider can
be renamed; its ID can't change, and the edit page says why (launches, Agent presets and the model settings refer to
it). Reason: the ID is an engineering detail that a person should not have to invent or read (principle 2), and one
rule for every place keeps a provider recognizable wherever it is picked (principle 4). This reuses the wizard's
fields and More options disclosure and the existing row and chip styles; it adds no setting, style or motion. The chat
page's composer chip and model panel adopt the same module in their own change; iOS and Android have not caught up yet.

## New Agent's model choice (#5104)

Starting in a known project opens a chat draft beside the usual sidebar. The header identifies the destination, and the existing composer places attachments and first input at the bottom with the model and supported permission controls near Send. A folder draft keeps its inputs across ordinary navigation. Opening a draft creates nothing on the computer. Send hands off to the regular startup and retry states. The global opener keeps the destination picker when the folder is unknown.

A project folder offers the computer's terminal launchers even when its sidebar group contains desktop chats. A desktop project found for that folder supplies the native project ID when its launcher is chosen. Opening an explicit desktop workspace keeps the start in its app.

New Agent picks its model with the chat's model pill and unified model panel ("Model pill and unified model panel" in
3.8), at the user's request (2026-11-12) to match Cindy's new task: the user picks a model and the launcher and its
engine follow; the engine's mark stays on the pill and on every row. Who it serves: people starting coding Agents (the
Agents side). The pill sits at the right of the first message box's bottom row; the Preset, Launcher, Model and Thinking
effort fields are gone. What the panel gained for it, each part optional and absent from the chat's panel:

- Follow row (`follow`): on top of the list in no group, a 1 px `--line` rule under it; a two-line row (its label, then
  what the default runs in 12 px `ink-2`, after a 11 px `warning` icon in `--orange` when that can't start here), with
  the selected fill and a 14 px check while followed. Keys and the hover glide as on a model row.
- Presets (`presets`, `onPreset`): a group after Favorites, "Presets"; one row per preset named by the preset, its
  description as the second line, the launcher's tile and the `[engine] effort` setup on the right; no star, no Customize,
  never lit (a copy of a setup, like a favorite).
- `state.starting`: no Recommended group, and the row lit shows the form's setup rather than a saved customization.
- Engine pills in the flyout (`engines`): when the model runs with more than one engine on this computer, Cindy's engine
  pills replace the static engine block: 26 px tall, no border, a 12 px `AgentMark` and the engine's name at 12 px, the
  row's own on `--hover-2` with a check; another is a quiet pill that picks that launcher's row of the model, at 50 % with
  its reason when that launcher can't start here.
- "How to fix" (`onHelp`, a provider's `help`): after a launcher's reason under its group's name, 11 px medium
  `--accent-ink`.
- Effort with no level set (`EffortSlider` `unsetLabel`): no fill, the thumb a hollow ring (`--surface` with a
  `--line-strong` border, no glow) at the start, the bubble and accessible value "Model default"; a click or key picks a
  level.
- Lines under the first message box: one 11.5 px line each, a 12 px icon first: a pick dropped on another computer
  (`info`, `ink-3`), the computer's launchers that couldn't be read (`warning`, `--orange`, the reason in `ink-3`, Retry
  in `--accent-ink`; #10952), a list that couldn't be read (`warning`, `--orange`), what runs this time can't start here
  (`warning`, `--orange`, the "Used this time" pill, How to fix), the preset picked (`layers`, `ink-3`; `warning` in
  `--orange` when something of it can't be applied).
- Launchers that couldn't be read (#10952): the pill leaves its spinner for its failed state (the red `warning` and
  "Model loading failed"), and a click on it reads them again, as Retry on the line under the box does; Start still runs
  the computer's default.

Reason: one place to choose a model for chats and new Agents (principle 4), the engine visible without a field of its
own (principle 2), the default as one row that can always be gone back to (principles 1 and 3). The deviations from
Cindy are listed in `docs/web/web.md` "New Agent's model choice (#5104)"; iOS and Android follow in #5117.

## Permission modes a provider lists (#8261)

Who it serves: people starting and steering coding Agents (the Agents side) on providers other than Claude Code, such as
Paseo's Grok, OpenCode or Codex. Claude Code's five modes mean nothing there: the bridge refuses Plan, drops the rest,
and the provider's own settings decide what it asks. One rule on every client (web, Android, iOS), read from the wire
rather than from provider names:

- **New Agent**, for a launcher that lists `permission_modes` (`docs/protocol/protocol.md`, `launchers.list`): the
  Permission mode field offers **Launcher default** (the launcher's own approval, sent as no mode) and **Allow all**
  only where it lists both, in each client's existing words for its follow-the-launcher choice and for bypassPermissions
  (the web: Launcher default, Full access; Android: Follow launcher, Allow everything; iOS: Use Launcher's, Allow
  Everything); a launcher with its own approval alone shows no field. The pick is made on the sheet and never
  remembered, so a mode left from a Claude Code start never reaches such a start. The effort offers only the levels its
  model lists, none while that is not known. A Claude Code launcher keeps its five modes and remembered choice.
- **A running chat**, for a session that lists `capabilities.permission_modes` with two: the client's existing mode
  control (the web's composer permission picker, Android's and iOS's mode menu) with those two rows alone. "Launcher
  default" carries a neutral mark (the plain shield, never the raised hand of "asks first"), since the provider's own
  settings may approve everything; no mode reported is Launcher default. Going into Allow all asks first, as it does for
  Claude Code; leaving it does not, since it can be turned on again. No Plan mode row.
- **Session info** leaves out counts nobody gives (`usage_unknown`) rather than showing zeros.

Reason: the same start and the same running chat behave the same on every client and entry point (principle 4), the
default is the launcher's own and nothing is sent unless the user picks it (principle 1), and Allow all stays a
confirmed, reversible choice (principle 3). It reuses each client's mode field, picker, menu rows, warning text and
confirmation; it adds no component, token or motion. `capabilities.modes` stays false on these rows, so clients
older than the list show no Claude Code chips there.

## File page (#5827)

Who it serves: people working with their computers' files from the browser (an Agent's Files tab, a computer's Files). Where it lives and what it does: docs/web/web.md "Side panels".

- **Path bar**: Back, Forward and Up as the page's circle buttons (`IconButton`), then the path from the home or `/`, every folder a button, the current one on `bg-field`. The session's folder carries a compact accent chip "Session"; a folder outside the home ends with a compact chip "Read Only". Why not the breadcrumb alone: the page now leaves the session's folder, so the path is the whole one and the history is a browser's.
- **Find field** under the path bar, beside the page's tool buttons: a capsule at the buttons' height with the search mark and a clear button (the web's `SearchField` `sm`). Results replace the list while there are words in it.
- **Info**: a `Dialog` with Copy Path; permissions in plain words before the code (`rw-r--r-- · 644`), the code in the code font.
- **＋ menu** (#5828): the page's add button (`plus`) opens New Folder and New Text File, then the uploads, each section apart; a name is asked in a `Dialog`, the name before the extension selected.
- **Permissions** (#5831): a grid of checkboxes, a row per owner / group / everyone else and a column per read / write / run, each labeled "who: what" for screen readers; Save is the confirmation action and asks in a `Dialog` that names the change in words and codes (section 4.4, rung 2: not undoable from here).
- **Selection bar** (#5830): while selecting, five equal cells on a card under the list, each an icon over its word (one line), Trash in the danger color; disabled cells at 40%. The count sits where the find field was.
- **Editor** (#5829): the web edits in place of the viewer with Cancel and a primary Save, the title the file's name and "Edited" under it once changed. The conflict asks in a `Dialog`: Save as Copy first, Overwrite Anyway destructive and confirmed again.
- **Symlinks**: a link mark on the icon's corner (accent), an error mark in the danger color for a broken one, whose second line is the danger color too.


## Starting from what you already have (#5193)

Where a form sets something up that the user may already have working elsewhere (the web and iPhone provider wizards' first step),
the usual choices stay first and unchanged, and two more ways in follow them, each a row of the same list style:
**Paste a configuration**, and, only when there is something to show, a group **Found on your computers** listing what
the hub can import as it is, each row opening the import that already exists for it. The paste view is one monospace
text area with one plain line saying what it accepts and that nothing is saved until the user confirms; it is read by
itself 0.7 s after the text stops changing and at once on a paste, with the status line of "Checking typed credentials
by themselves" (a spinner while reading, then the result). What it found is a list of cards (a radio choice when there
are several): what it is, its address and how many models, then in secondary text what it brings along and what it
leaves out, by name and never by value. A draft that lands on something saved offers the existing choice as a radio
group (add its models to that provider, or replace only its key) beside adding a new one, and both open the existing
edit flow with the change filled in, so the user sees it before Save. Every part of the text that was not used gets a
line of its own (by line number or name), never dropped silently. The footer keeps one primary action, **Continue**,
enabled once something is chosen, with **Back** to the list. Reason: retyping an address, key and models the user
already has is slow and error-prone, and an import that fills the form instead of saving keeps the user in control
(principles 1 and 3); reusing the wizard's rows, status line, radio cards and the import dialog keeps one way to set a
provider up (principle 4). It adds no setting, style or motion.

An import link (#5420) is the same paste view opened by a link instead of typed into. Opening the link goes straight to
that view in its link mode: no text area, a row with the link icon saying it was opened from a link and, in secondary
text, that nothing is saved until Save, with **Paste something else** (the status line's action) to go back to the text
area. The link is read at once and its drafts, choices and notes look exactly like a paste's, so the wizard does not
grow a second way to review a provider; a refused link is one plain line about what is wrong with it, never its
content. Android uses this same link mode for `persona://provider/import` (#5428). iOS has no paste step, so the hub's
returned draft opens its existing connection form directly. On both phone apps the link itself is never shown and is
dropped when the wizard closes. Copying a link starts from the provider's dialog: a ⋯ `IconButton` before Edit opens a `Menu` (one row, **Copy
import link**), and the row opens a `Dialog` in place of the provider's (Edit does the same), with one line on what the
link brings, a `Switch` **Put a key in the link** that is off, and **Copy link** as the one primary button. Turning the
switch on widens what the link exposes, so, as in "Sharing a conversation publicly" (4.9), the key field comes with an
orange warning block (the warning mark, `--orange`) on where links travel, visible before Copy; the key is never
fetched from the hub and goes when the dialog closes. Reason: a link is how gateways and friends hand a setup over
(principle 1), the review and Save stay the user's (principle 3), and the risky part is opt-in and explained
(principle 2); it reuses the paste view, `Menu`, `Dialog`, `Switch` and the copy notice, and adds no setting, style or
motion.

## Project source on the web (#6570)

Audience: everyday users completing setup and chatting without promotional interruptions, and people who deliberately
look for the project's source. Settings > General ends in **About Persona** with one ordinary **Source code** row,
showing `github.com/nwverse/persona` as a link. Reuse `Group`, `Row` with its narrow-screen stacking, the existing
link color and touch-target class; no new token, button, animation, popup or preference. The link opens a new tab
only on activation. This project-information link is not a conversation link.

The web has no setup toast, empty-Agents pill or launch-count milestone asking for stars. The former browser-local
prompt state is ignored, not migrated or deleted; no hub data changes (principles 1–4 and 6).

## Knowledge map outline (#7251)

The web's knowledge map is a two-sided mind map; on the iPhone it is turned upright into an outline (`KnowledgeMapView`,
logic in `KBMap`), which the owner chose over a pinch-to-zoom map that, fitted to a phone, drew its words at 5–8 pt. Rows
are list rows: a topic (a dot in its branch's tone: accent, green, orange in turn), its subtopics and its points (the kind
as a `KBPill`, Pitfall and Open question in `Theme.caution`), each level indented by `Theme.Space.l` with a rail in the tone.
Folding uses the app's fold chevron (`Theme.Icon.disclosure`, turned when open, a 44 pt target) rather than the web's
round +/−, as every other fold on iOS. What starts open is the web's: the six biggest topics onto their subtopics, every
subtopic closed, more than three loose points beside subtopics under one closed Other. Tapping a point opens its sheet,
a topic or a project its page. From `Theme.Size.knowledgeMapTwoColumns` of width (any iPad in landscape, a 13-inch one upright beside its
sidebar) the outline sits beside a detail column for the selected node, the web map's side panel.
Text follows Dynamic Type (the rows are reading content) and every row reads as one VoiceOver element.
Android (#7678) draws the same outline with the same rules (`KnowledgeMapScreen`): rows of the design system's tokens, a dot and rails
in the branch's tone, a 48 dp fold target with Expanded / Collapsed read out, in all three styles, with no detail column.

## Repository link preview

Settings › Assistant › Repositories reuses the existing `Dialog`, form controls, inset preview and `Loading`.
A local parse of each link/provider change immediately replaces the provider, full path and canonical web root in
an announced status region. Invalid input clears that preview and disables Save; it never leaves the old identity
visible. The preview explicitly says remote existence is unconfirmed. Check and save is a separate explicit action,
keeps the draft on failure, and a browser timeout asks the user to refresh because persistence is unconfirmed.
This serves people binding a repository reference in advanced assistant settings; it adds no primary-screen
options, style, theme or motion. See `docs/assistant/managers.md` for provider semantics and verification boundaries.

A provider extension's source (#10330, `docs/extensions/repository-providers.md`) changes nothing on screen while
none is installed and no saved repository uses one: the list and the menu's three options stay as they were. The only
difference then is one more read, the sources beside the list (a 404 or a failure means the built-ins only).
Once there is one, the expanded area adds a **Sources** part, plain rows of each source's mark, name and either
"Built in" or its hosts in small mono text, with a neutral `Pill` **Unavailable** for one that is off. The provider
menu adds extensions by name after the built-ins, an off one disabled unless the edited repository uses it. The
preview of an extension's address waits for the source: the shared `Loading` line names it ("Reading the address
with Acme Code…"), then the inset preview names the source rather than its id; a host the source does not claim, an
unavailable source or its refusal (the hub's words) is one status line, and Save stays off until an identity
arrives. A refusal or failure is never final: a quiet **Try again** under it reads the address again, as does
editing the address. Until the sources are read (or when they can't be), the page claims no source is unavailable:
the repository being edited asks its own source through the hub, whose answer is shown. A saved repository of an
extension names its source under the address and, when that source is off or gone, adds the neutral **Unavailable**
pill and one `data-repository-note` line on what still works on this page (it stays saved and its details can be
edited). A source whose extension was renamed is still the saved repository's: the hub keeps the id it was saved
with, so the page matches it to the listed source with the same contribution that claims its host.

## Scheduled task execution options

The web task editor serves people scheduling specialist work as an optional **Execution options** group, closed by
default and placed before the prompt. Its title and configuration share one compact line, with ellipsis and a full
text tooltip when narrow. Its disclosure always shows the effective launcher name, agent backend,
model and thinking effort; repeated launcher/backend names are shown once. It reuses `Field`, native selects,
`ModelPicker`, semantic surface/text tokens and the existing dialog. Every untouched control explicitly follows
Assistant, with known inherited values shown. Selecting another launcher uses that agent's defaults for model and
effort. Empty choices retain dynamic inheritance rather than pinning a copied value. Task list rows use the same
formatter and resolved `execution` context, including inherited model and effort; older responses without that
context keep their legacy display and need no new fields. The selected computer is shown
inside the group. Launcher and model discovery use that computer's chat-compatible capabilities; the model catalog
is only requested when the group opens. A working directory
is an existing path on that computer, explained under the field. Switching execution context may start a new chat;
the hint says earlier chats are retained. Failed saves retain the draft, and saving/running disables the controls.
If Run now saves successfully but running fails, the open form accepts the saved execution pins and revision.
Fields edited while saving keep their newer input; untouched execution fields take the canonical saved values.
A later unrelated save stays sparse, and explicitly clearing a choice still restores inheritance.
Continuing the original chat requires empty task execution choices, including the computer and directory. The shared
form validation explains this and disables Save and Run now; clearing saved choices restores inheritance without
requiring that computer to be online. Toggling the original chat back on revalidates the current choices.
Launcher discovery failure or a missing saved launcher keeps the saved choice visible as unavailable, while the
Assistant inheritance option remains selectable. Selecting it explicitly clears launcher, model and effort together;
discovery alone never changes saved choices. Computer-scoped defaults come from that computer's launcher response.
A saved model alias uses its folded catalog row's capabilities and default thinking effort without rewriting the saved ID.
Defaults, model and launcher catalogs are keyed to the current assistant and their exact execution context; stale
responses cannot replace the current catalog. Discovery reads and the task save target the same assistant. Task
inheritance is requested explicitly, leaving ordinary Assistant picker defaults unchanged.
While model discovery is pending, the picker is disabled. A failed read leaves the saved selection visible and the
launcher inheritance recovery control usable, with no rows from the previous context.
Run history has a separate disclosure for its execution configuration, outside the button opening the conversation.
This is a secondary form extension; primary screens, themes and chat input remain unchanged.

## Optional repository associations

Coding-work metadata belongs in the Web to-do detail's **Repositories and
work** group; creation carries the same editor, folded, under its own optional
button. Ordinary to-dos still require only a title. On the detail the group is
read first and drawn in the existing grouped-row geometry: one row per saved
repository with its provider's own mark, project address and a `Pill` of its
link count, and one link row per issue or pull request. `RepositoryMark` reuses
the bundled GitHub asset and adds GitLab's mark without extending the unrelated
connector vocabulary. Repository names and addresses share one line; the
association store has no physical workspace path, so directory aliases are not
presented as filesystem paths. A link carries its complete source title as one
hyperlink, wrapping at narrow widths, followed by its number and an external
mark. An unknown title falls back to the localized kind and number. GitHub PRs
use the pull-request mark, GitLab MRs the merge mark and issues the existing
issue mark. A provider extension's repository uses the extension mark
(`Icon` `extension` in the secondary ink, `data-repository-mark="extension"`,
hidden from assistive technology; `git` would read as the branch mark of
**Source options** beside it). Its source's name, never the `ext:` id, follows
the address, so a narrow line cuts the name before the repository's path. Its
links open at their stored addresses, are numbered `#N` and take
the source's own `link_kinds` word for an untitled link. A pasted address of such
a repository is a pending link row ("Checked when you save") until the hub
reads it on save. When its source is off or uninstalled, the repository row adds
the neutral `Pill` **Unavailable** before its count and a `data-repository-note`
line beneath it; links keep opening and only adding one is turned off. The
picker shows the same mark and the source's name after the host. The
associations an agent keeps show without an editor. Editing is
the existing `Dialog` with a compact text button (**Manage links**), never an
always-open form. `TaskRepositoryBindingsEditor` is the controlled editor;
`TodoRepositoryBindings` owns existing-todo reads and writes and
`NewTodoRepositoryBindings` contributes a draft to the parent's single creation
request. These reuse `Group`, `Row`, `Pill`, `Button`, `IconButton`, `Menu`,
`Dialog`, `Collapse`, `SearchField`, `Loading`, `RowFailed`, shared settings
inputs, semantic tokens and toast Undo for draft removal. There is no new page
or workspace control.

Repositories are added through the search picker (name, path or host; ↑ ↓,
Home/End, Enter takes the first match, Escape closes and returns the focus to
its button), not a native `select`. Each repository's issues and PRs are link
rows with their own **Add issue or PR** field: Enter adds, Escape cancels, and a
URL of another repository is refused inline by `repositoryLinkView`, the same
rule the manager applies. Public HTTP aliases, scheme case and repository-path
case are normalized by the shared parser before that comparison; a self-hosted
instance's scheme and port must match exactly. Two links that normalize to the
same repository, kind and number are refused before create/save, with both
entries kept for correction. Directory names and source refs are validated with
the manager's rules before a create or save, and an invalid optional draft
shows the field error and cannot be submitted. Directory aliases and source
refs stay in the per-repository **Source options** disclosure; empty aliases
and default branches need no setup. Saved-source changes (`removed`, `identity_changed`) and
`unlisted` rows inferred from agent-added links are explained beneath their
repository while the saved source stays. A to-do's `pr_url` is drawn inside
this group as a link row with one line saying it predates the store, and never
beside it, so one place holds a task's issues and PRs. That row is a fallback
for a hub whose route is missing only: a successful store answer is
authoritative even when it is empty, because the manager imports the legacy
history once and honours an explicit removal, so an empty answer after the last
repository was removed must not resurrect the old PR.

Controls have labels, action groups wrap at narrow widths, and saving disables
edits. Failed saves keep input; conflicts offer explicit **Discard edits and
reload**, which reads the store again rather than reusing the stale draft, and
opening the editor always reads the newest store value and catalog because an
agent may have added a link. Manager/Todo/credential changes retire old results.
New copy ships in English, Simplified Chinese and Japanese. Evidence:
`web/src/ui/TaskRepositoryBindingsEditor.test.tsx` and
`web/e2e/todo-repositories-browser.py`.

## Lark topic group policy (#10238)

Settings › Connectors › Lark bot › Topic groups serves owners who want one group to require their explicit bot mention. It reuses Group, Row, Switch, Button, the native select and settings input styles; policy defaults off. Each group shows its saved switch. Enrollment allows choosing the policy before adding the group; a strict first enrollment stays separate from the home choice. The home select shows the effective home or the private-chat fallback. Both writes wait for the existing acknowledged save; failures preserve saved values and enrollment drafts. No primary chat screen or model setting changes.

## Shared botmux display setting (#10241)

For people using Agents, Web Settings › Agents reuses `Row`, `Switch`,
`SettingsHint` and `RowFailed` for **Show botmux sessions in Agents**. The row
shows the acknowledged shared value; a save disables the switch and shows Saving,
a failure keeps the last acknowledged value and offers Retry. A nearby Computers
link explains the independent per-computer inventory switch. The sidebar's
botmux chip is removed; its Lark group quick filter is absent when its count is
zero. Complete and paged lists exclude the same hosts before grouping and counts.
This requested interaction change adds no component, theme or motion pattern.

### Independent main conversation defaults (#11936)

Audience: people choosing the model for their main Assistant conversation. Reuse the existing model-configuration purpose row and editor, with **Conversation defaults** as the category and **Main chat** directly beneath it as its independently editable use. Do not add another primary settings page or put runtime implementation details in the chat flow. Show the inherited effective value and existing reset action; changing Main chat must not modify a new side conversation's defaults.

The existing main model pill and empty main starter read the main purpose's effective launcher/model/effort. A new side-conversation starter reads the shared default. An explicit conversation pick continues to win. Cross-engine changes use the existing explicit switch confirmation; saving a default does not silently start a second execution or optimistically rename the current engine. English, Simplified Chinese and Japanese purpose names and descriptions ship together.

Native main-chat model defaults (#11936): Apple AssistantModelSheet/composer and
Android ChatModelPill retain the shared eligible-launcher catalog but read the
independent `assistant.main` default for the main conversation. Its launcher,
model and next-message effort apply below an explicit conversation pick. Main
and ordinary-new-chat caches are distinct; no main read writes shared defaults.
An older hub with no main purpose retains shared defaults; a transient main read
failure keeps the last good main value. Reuse the existing capsule, panel and
feedback, without a new control or polling loop.


### Shared runner visibility

Computers includes an advanced Shared resources view for Agent operators. It uses
the existing segmented view picker, surface, button and loading patterns. Each
card counts the running tickets in the last external snapshot against that
snapshot's slot capacity; the manager's admission rows are listed separately.
Unknown freshness is always visible, retained data is labeled stale, duplicate
tickets name their shared result, and start estimates say unknown when no reliable
duration is available. Runner registration is editable only on explicit request
through Configure runners, and saving an empty list disables polling.

## First-run pairing

Center the Persona mark and title above the primary control. A phone opens its scanner. A browser takes a pairing link and submits it with the arrow inside the field group. Place the secondary actions in one centered row of icons with short labels. Keep the action and its label in a single tap target. Allow wrapping or stacking at larger text sizes. Help explains where to get a code or link. A warning about replacing an existing pairing stays beside the action.

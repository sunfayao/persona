# Front-end development

> How the web app in `web/` is built, and the rules that keep it fast and consistent. Read this before you add a
> page, a dialog, a component, a request to the hub, or any style. You usually arrive from core constraint 9 of
> `AGENTS.md`.

---

## Shape of the app

| Piece | Where | Notes |
| --- | --- | --- |
| Client entry | `web/index.html` → `src/main.tsx` → `src/App.tsx` | The import order in `main.tsx` is load-bearing: tokens in the URL are read before `App` |
| Admin pages | `web/admin/index.html` → `src/admin/` | Own router and own `admin-*` text namespaces; served only to allowed sources. Keep admin code and strings inside `src/admin/` |
| Public share viewer | `web/public-share/` | A separate document without React. It may import nothing outside its own allowlist; the build fails otherwise |
| Service worker | `src/sw.ts` | A classic worker with no imports that emit a chunk; never touches `/api` |
| Live screen | `src/screen/` | A branch of `main.tsx` that never loads `App` |
| Development pages | `showcase.html`, `assistant-cards.html`, `html-runtime-lab.html` → `src/showcase/` | Fake data for looking at components. Not shipped |

There is no router, state or test-rendering library. The router (`src/lib/router.ts`), the store
(`src/lib/store.ts`) and the test setup are written here; use them, do not add a library beside them.

## Directories

| Directory | Holds | Imports |
| --- | --- | --- |
| `src/views/<area>/` | Screens and their parts (`PascalCase.tsx`), with hooks only they use | `ui`, `lib`, `i18n` |
| `src/ui/` | Shared presentational components and design-system pieces | `lib`, `i18n` |
| `src/lib/` | Everything that is not visual (`kebab-case.ts`): store, API, router, drafts, formatting, per-feature logic | `i18n` |
| `src/features/<name>/model/` | A feature's model when it is large enough to stand alone | `lib` |
| `src/i18n/` | `t()`, formatters, catalogs | nothing above it |

Imports point downward: `views → ui → lib`. A few old files break this; do not add another.

## Pages, dialogs and the first screen

The first screen has a byte budget per language, and `npm run build` fails when it is exceeded. `tsc` and the tests
do not notice, so you must build.

- A new page is lazy: `lazyPage(() => import("./views/X").then((m) => m.X), { routes: ["x"] })` in `src/App.tsx`.
- A new dialog or popover that opens on a click is `lazyDialog` or `lazyOverlay` (`src/views/LazyOverlay.tsx`).
- Code reachable from `main.tsx` must not statically import a lazy page's module. A `X.lazy.test.ts` beside a page
  pins that; when you move a page or an import, run it.
- Strings of a lazy page live in their own text namespace, not one listed in `src/i18n/startup-namespaces.json`.
- Every path goes through `BASE` (`src/lib/base.ts`): the app can be hosted under a sub-path. You never write
  `/api/...` or `/assets/...` by hand.
- An open overlay closes on the browser's Back before the page changes: call `useCloseOnBack(open, onClose)`.

## Data

| Need | Use | Rule |
| --- | --- | --- |
| Read shared state | `useStore(selector)` | The selector's result is compared shallowly: return existing objects or a flat new array, not freshly nested objects |
| Call the hub | `api<T>(path, options)` from `src/lib/api.ts`, wrapped in a function of the feature's `lib` module | Path is relative to `/api/v1`; build path pieces with `enc()` and `sessionPath()`; show failures with `errorText(e)` |
| React to pushes | Handlers in `store.ts` `onMessage` | Pushes render once per frame. Code that needs the page right after its own request calls `flushPushes()` |
| Send a message | The outbox (`src/lib/outbox.ts`) | Never a bare `api()` post: the bubble shows Sending at once, Processing when the hub has it, and a failed send stays with Retry. An uncertain send is never repeated as a new message |
| Keep something in the browser | `storedValue` / `storeValue` from `src/lib/compatibility.ts` | Not raw `localStorage`: old key names are mirrored there |
| Types of what the hub sends | `src/lib/types.ts` by hand; `*-wire.ts` are generated | Change the Go type, the sample and `types.ts` together (`06`); never edit a generated file |

The hub's `hello` lists features. Where the store has a path for a hub without a feature, keep it: the page in a
browser can be newer than the hub.

## Design rules

The full reference is `docs/design/DESIGN_SYSTEM.md` and the reasoning is `docs/design/PRINCIPLES.md`; read the
relevant section before you change a screen. The principles in one breath: common tasks work with untouched
settings; advanced options are off by default and live in Settings; every setting that changes behaviour is visible
and reversible, a reversible action offers Undo and an irreversible one asks first; one label means one thing on
every screen; you can show evidence (a screenshot, a test, a command's output) for each rule you claim to meet.

What you must not break:

1. **Tokens, not literals.** Colours are the role tokens of `src/styles.css` (`bg-surface`, `text-ink-2`,
   `border-line`, `text-accent-ink`), radii `rounded-chip|control|card`, text sizes `text-card-title|meta|label`.
   No raw hex and no new `text-[13px]` in new code. Do not reformat the `:root {` lines of `styles.css`: a script
   reads them.
2. **Existing screens keep their look.** You do not restyle what you were not asked to change.
3. **A button label stays on one line.** Shorten the words in every language first, then reconsider the control.
4. **One solid primary button per group. Red only for errors and destructive actions.** Requests that wait for the
   owner use neutral surfaces with amber details.
5. **A control is at least 44 by 44** (`touch-hit`), has a spoken label, and never shows state by colour alone.
6. **Text contrast is 4.5:1 in light and dark.** A new text role is added to `src/ui/theme-contrast.test.ts`.
7. **Motion respects reduced motion.** Busy animations carry `busy-motion`; scripted scrolling goes through
   `scrollBehavior()`; expanding uses `Collapse`.
8. **Phones are first-class.** The layout is narrow below 1024 pixels and dialogs become bottom sheets at phone
   width (`SHEET_QUERY`); inputs use at least 16 pixel text, safe areas and the soft keyboard are respected, nothing
   depends on hover.
9. **Lists stay put.** Opening or closing something in a chat keeps the tapped row where it was. Tool activity is
   collapsed by default. Primary screens show no raw model ids, paths or session ids.
10. **Words follow the glossary** in `docs/protocol/i18n.md` (Computer, not Machine or Node; Assistant, not manager)
    and every visible string goes through `t()` in three languages (`08`).

Use the components of `src/ui/` (`Button`, `IconButton`, `Dialog`, `Menu`, `Switch`, `Segmented`, `StatusMark`,
`SearchField`, `ErrorNotice`, `Collapse`, `toast`) before you write a new one.

## Checking your work

```bash
cd web && npm ci --no-audit --no-fund
npx tsc -b            # unused locals and parameters are errors
npx vitest run src/views/<the files you touched>
npm run build         # the only check of the first-screen budget and the startup text list; needs Go and Python
```

- `npm run lint` covers a short file list with six rules. A green lint says almost nothing; the real guards are
  `tsc`, the build, and tests that scan the source (text catalog, contrast, motion, lazy boundaries).
- Tests run with the Simplified Chinese catalog, so assertions on visible text are in Chinese. Component tests use
  `createRoot` and `act` directly and mock `../lib/store` with `vi.mock`; a DOM test starts with
  `// @vitest-environment jsdom`.
- Look at the change in a browser against made-up data, in light and dark, at 390 and 1440 pixels wide:

```bash
PORT=18760 python3 -I scripts/mock/mock_hub.py                      # any token signs in; MOCK_* variables add scenarios
cd web && PERSONA_HUB_URL=http://127.0.0.1:18760 npx vite --port 5173 --strictPort
```

  `web/e2e/` holds scripted browser checks of existing views (`node web/e2e/<name>.mjs <origin> <output dir>`); run
  the one for the view you changed, and add one when you add a view.

## Traps

- A new static import or a new key in a startup namespace can break only `npm run build`.
- Deleting the last use of a text key fails the catalog test until the key is removed from all three languages;
  a key used only through a computed name is reported as unused.
- `eslint-disable react-hooks/...` comments are inert: the rules are not enabled. Do not rely on lint for hooks.
- An ancestor with `overflow-hidden` becomes the scroll container that `position: sticky` refers to; use
  `overflow-clip`. A collapsed grid row (`0fr`) still holds focusable elements: add `inert`.
- A focused terminal (xterm) swallows the keys it handles, so a window-level Esc or arrow listener never sees them.
- Do not put a `confirm()` in front of a step that can be undone, such as replacing a draft: do it and offer Undo
  for everything the step dropped.
- A form keeps what the user changed, not the whole form: store the edit with the configuration it was made on and
  re-apply only the changed fields, or Save silently reverts a change made on another device.
- What a control says and what it does come from one condition: when the composer says Working, Interrupt and Esc
  are derived from the same state as that label.
- Vitest stops at start when an installed dependency differs from `package-lock.json`: run `npm ci`.
- Comments in old files compare with the removed iOS and Android apps. They are history; do not add new ones and do
  not treat them as requirements.

# Links to Persona's pages (deep links)

One route table for every link that opens a page of Persona: the links the hub writes (Inbox rows, Lark cards,
notices in conversations, the assistant's answers), the links people paste, and the links other apps hand to the web
app. A link to one of the hub's own pages opens that page in the web app, never a browser page of the same thing.

## The two forms

**A page's canonical form is its path in the hub's web app** (`web/src/lib/router.ts`). Every page has exactly one
canonical path, and the same path has two forms:

| Form | Shape | Example |
| --- | --- | --- |
| Web link | `<public_url><path>` | `https://hub.example.com/persona/c-7f3a` |
| App link | `persona://open<path>` | `persona://open/persona/c-7f3a` |

The app link is the web link with `https://<hub>` (and any sub-path the hub is served under) replaced by
`persona://open`. Nothing else changes: the path, its query and its escaping are the same.

- **Links the hub sends out are web links.** Lark cards accept only http(s), and a web link opens in a browser.
- **App links are for the desktop shell and for links handed from one app to another.** The hub does not write them
  into content.

## Route table

The hub's builder is `server/internal/deeplink`, the web app's reader is `web/src/lib/deep-links.ts`. Query keys not
listed for a row are dropped (not passed on, not acted on). Fragments are ignored.

| Page | Canonical path | Query keys |
| --- | --- | --- |
| Persona (the assistant's main page) | `/persona` | |
| New conversation | `/persona/new` | |
| A Persona conversation | `/persona/<id>` | `machine`, `manager`, `at` |
| An Agent's conversation | `/s/<machine>/<session>` | `at`, `view` |
| Agents | `/agents` | |
| Inbox | `/inbox` | |
| An Inbox item | `/inbox/<id>` | `manager` |
| To-dos | `/todos` | `view`, `mode`, `date` |
| A to-do | `/todos/<id>` | |
| Computers | `/machines` | |
| A computer | `/machines/<id>` | `range` |
| Settings | `/settings` | `tab`, the tab's item key, `manager` |
| An extension's page | `/extension/<extension>/<ui>` | `state` |

- `at` names one message to land on (`?at=<item id>`; Claude's ids hold `#`, written `%23`). An id that holds `/` is
  one escaped segment (`%2F`).
- Settings tabs are `general`, `notifications`, `models` and `devices` (`web/src/views/settings/nav.ts`). `models`
  has the item key `purpose`. An unknown tab opens Settings.
- **Not in the table**: every other path on the hub (`/admin`, `/share/…`, the web's own pages such as `/overview`
  or `/history`). The web app treats them as its own pages.

### Older forms the readers keep accepting

These name the same pages and are read wherever links are read. Nothing writes them.

| Older form | Page |
| --- | --- |
| `/`, `/a` | Persona; Agents |
| `/t/<machine>/<space>/<slot>/<session>` | that Agent's conversation |
| `/todos?id=<id>` | that to-do |
| `/plugin/<extension>/<ui>` | that extension's page |
| `persona://open?route=<escaped path>` | the path |
| `persona://open?machine=<m>&session=<s>` | `/s/<m>/<s>` |
| `persona://session/<machine>/<session>[?at=<item>]` | `/s/<machine>/<session>[?at=<item>]` |
| `persona://task/<id>` | `/todos/<id>` |
| `persona://settings` | `/settings` |
| `persona://extension/<extension>/<ui>[?state=]` | `/extension/<extension>/<ui>[?state=]` |

The `jarvis` scheme and paths are accepted as these are (`AGENTS/06-wire-contracts-and-extensions.md`).

## Which hub

- **A web link names its hub by its address.** The web app opens it in place only when its origin is the hub's.
  A hub served under a sub-path (`https://<host>/<prefix>`) has its pages under that prefix. Any other address is an
  outside link.
- **An app link may carry `hub=<hub id>`** (the hub's id from its hello). Without it, the page opens on the hub the
  app is showing. `hub` is no page's query key: it is read and dropped before the path is handed on.

## Not routes: action links

These `persona://` links do something rather than open a page. They are not in the table, and each keeps its own
checks; none of them is written by the hub's link builder.

| Link | What it does | Checks |
| --- | --- | --- |
| `persona://pair?url=&token=` | pairs a client with a hub | asks "Connect to <host>?" first |
| `persona://link?…`, `persona://peer?…`, `persona://connector?…` | a computer's, a linked hub's, a connector's pairing | pasted where they belong |
| `persona://password-login?state=&code=` | finishes a sign-in | its `state` |
| `persona://ask?text=`, `persona://sources-graph` | the subscriptions pages' actions | only from those pages |
| `persona://inbox/<id>/source` | an extension's link back to the Inbox item it is drawn for | only that item (`checkOpenURL`) |

## Security

- **A link only opens a page.** Every query key a row lists is view state: which message, which tab, which view. Keys
  that do something (`token`, `hubs`, `import`, `add`) are in no row, so a link cannot sign in, switch hubs, import a
  provider or start adding anything. A fragment is ignored.
- **Only the hub's own address counts as the hub.** A look-alike host, another port, or a link with a user name or
  password is an outside link (a link with a user name or password is not opened as a page at all).
- **A malformed app link is refused**: a route that does not start with a single `/`, holds a backslash or a control
  character, or would leave the hub when resolved against it.
- **Extensions stay limited.** An extension's `open_url` is only https, its own settings or the Inbox item it is
  drawn for. An https link to the user's hub that is in the table opens that page, which only shows a page.
- **The assistant's links.** The model is told to copy the links it is given, never to build its own; an assistant
  view's `open` is checked against https and Persona's own forms (`CheckAssistantOpen`).

## Behavior of the web app

Links to its own hub go through the router's `parse` and open in place; app links and older forms in messages and
cards open the same pages. The desktop shell registers `persona://`, refuses malformed links and drops query keys no
row lists, in both app-link forms (`persona://open<path>` and `persona://open?route=`); the web app in the window
applies the table. Push payloads keep their typed keys (`machine_id`, `session_id`, `todo`, …), which the web app
maps to the same pages.

## Shared cases

`spec/fixtures/deep-links/cases.json` (rules in its README) holds the cases the hub and the web app check:

- `build`: a page to its canonical path, and the web and app links for a hub address (the hub's builder).
- `parse`: a link and a set of paired hubs to the page it opens, or to why it opens none (`external`, `not-a-page`,
  `unsafe`).

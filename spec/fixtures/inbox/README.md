# Inbox fixtures

This directory pins the inbox list behavior. The web app reads it, and the hub's ordering is checked by the Go implementation. Triage models, collection, sending and retention are out of scope; the input is the data the list has already received.

## Rules and cases

Every case has a `source` array.

| Rule | Cases |
| --- | --- |
| A missing, empty or unknown tier is shown by its action, but any non-empty tier counts as triaged. | `inbox_decode_triage_assertions`, `unknown_tier_is_triaged_action` |
| Only a non-empty `done_at` means done; `read`, `done_by` or an AI handling state cannot replace it. | `read_and_agent_do_not_change_tier`, `untriaged_and_done_test_assertions` |
| Non-empty sections are always ordered Needs you, FYI, folded, done. | `tiers_and_done_sections`, `empty_inbox` |
| `action` and `fyi` are expanded by default; when folded, members and the title count are kept. | `tiers_and_done_sections`, `expanded_noise_and_done` |
| The hub output is ordered by `updated_at`, newest first, and the triage tier does not change it. | `hub_orders_by_update`, `hub_order_reversed_input`, `hub_order_empty` |
| Items not yet handled keep the input order within a section; the list does not reorder them by `updated_at`. | `input_order_is_authoritative` |
| Handled items are shown by `done_at`, newest first. | `tiers_and_done_sections`, `expanded_noise_and_done` |
| The source filter compares `account`: `feishu` for Lark and the account id for a mailbox. | `inbox_filter_feishu`, `inbox_filter_mail_account`, `source_filter_compares_account` |
| Source and kind conditions intersect; `group` includes bots and `dm` does not. | `inbox_filter_group_includes_bot`, `inbox_filter_dm_excludes_bot`, `inbox_filter_mail_kind`, `inbox_filter_mention`, `inbox_filter_incompatible_source_kind` |
| Search ignores case and matches `title`, `sender`, `preview` and `summary`, not `context` or `reason`. | `search_four_fields_only` |
| Pending interactions that are not muted come before the `action` items, ordered by `created_at` ascending; assistant interactions take part too. | `waiting_agents_before_items` |
| Interactions show only when the source is unrestricted and the kind is all or agent; search matches `sessionTitle`, `summary` or the computed title. | `agent_only`, `source_hides_agents`, `kind_hides_agents`, `search_agents` |
| A space mute can be overridden by a per-session `allowed`. | `agent_mute_allow_override` |
| A section title's count includes interactions and items; `actionCount` counts every unhandled `action` item and is not affected by the page filters. | `waiting_agents_before_items`, `source_hides_agents`, `inbox_filter_mail_account` |
| The filter labels have no numbers, so `filter_counts` is an empty object. | `inbox_filter_feishu`, `inbox_filter_mail_kind` |
| With a `status`, sections follow it: working sits between Needs you and FYI and is always expanded; a working item does not count as Needs you (`actionCount` does not count it); a missing or unknown `status` falls back to the tier. | `status_sections_with_working`, `working_never_folds`, `old_hub_without_status` |
| Equal times are ordered by id ascending. | `equal_times_use_id_extension`, `equal_times_reversed_input` |

## JSON structure

The top level of a file is `{schema_version: 1, operation, cases: [...]}`. Every case has an English snake_case `name`, a `description`, a `source`, `input` and `expected`, and is marked `from_test` or `derived: true`. All business content and identifiers are made up, times are UTC RFC 3339, and a missing or null optional value is treated as the default.

| operation | File | Input and output |
| --- | --- | --- |
| `inbox_item_state` | `item_state.json` | Input `items`; output `id`, `triage`, `is_triaged`, `is_done`; `sessions` is empty. |
| `hub_inbox_order` | `hub_order.json` | Input unordered `items`; output the ids in the hub's list, checked directly against the Go implementation. |
| `inbox_list` | `sections.json`, `filters.json`, `agents.json`, `ties.json`, `status.json` | Input the loaded list, pending interactions and settings; output the sections, visible rows and counts. |

`inbox_list.input`:

- `items` is the wire JSON of InboxItem; the array order is the list order. A normal online list is already ordered by the hub, but a local cache or an optimistic update still follows the array.
- `sessions` is used only to decide whether the space of an interaction is muted.
- `interactions` is the projection of pending interactions: `id`, `machine_id`, `session_id`, `kind`, `created_at`, `session_title`, `summary`, `title`. `title` is the search title the UI computes, not a new wire field; interaction wording is not tested here.
- `mutes` uses `spaces`, `sessions`, `allowed`.
- `settings.source` is `""`, `feishu` or a mailbox account id; `kind` is `""`, `mention`, `dm`, `group`, `mail` or `agent`; an empty `query` means no search.
- `settings.expanded` lists the expanded `action`, `fyi`, `noise` and `done` explicitly, so that tests do not depend on UI state initialization.

`inbox_list.expected`:

- `groups` lists only non-empty sections, in the order shown; each has `id`, `title`, `count`, `rows`, `visible_rows`, `folded`.
- `rows` holds the full order the section has when expanded; `visible_rows` is empty when folded and otherwise equals `rows`; done projects its expanded order even when folded.
- A row reference is `{type: "item" | "interaction", id}`; the types are kept apart so that an item id and an interaction id never overwrite each other.
- `count` counts rows, not total messages; an item's own count does not enlarge the section title count.
- `action_count` is the number of all unhandled `action` items; do not take it for the filtered section title count or for the app badge that includes every source.
- `filter_counts: {}` says the UI has no filter numbers, and a consumer must not make up label counts from the section counts.

## The tie-break for equal times

`ties.json` carries `compatibility: "id_tiebreak_extension"`. Its expected is a deterministic rule: when `updated_at` of items, `done_at` of handled items or `created_at` of interactions are equal, they are ordered by their own id ascending. The other comparison keys and the section order keep the current behavior. An ordinary cached item without a timestamp keeps the input order, and a missing time is not disguised as one real time.

Known differences:

- The hub's `inboxList` compares only `updated_at`; its input comes from a map, so the order of equal times is not guaranteed.
- The expected of the two equal-time cases is identical; `ios_expected` in those cases records the result of a removed client that walked the input order and is read by nothing now. The deterministic rule checks the id order of expected and its invariance under reversed input.

"Newest first" is guaranteed by the hub's output; do not reorder a cached list by time because of it. Every other case takes the output of the existing implementation as expected.

## Checking and replay

`hub_order.json` is checked by Go: create a Service, call `inbox.init`, put `input.items` into `inbox.items`, call `inboxList` directly and compare the returned ids with `expected.ids`; there is no database or network request. The inbox belongs to the manager. The status sections (`status.json`) have an expected written from the contract.

## Adding cases

Append cases by topic and update the rule table; names must be unique across both domains. Make up the input and write the expected independently first, then check against the implementation; cover unknown tiers, empty lists, crossed filters, mute exceptions and equal times separately. When the current implementation conflicts with a new rule, mark `compatibility` explicitly and say who must follow up; never change an expected quietly. Before committing check UTF-8, two-space indent and the privacy scan.

## Hand to AI memory (`handoff_memory.json`)

Hand to AI remembers, per source, the target an item was last handed to . Every app follows the same rules:

- `keys`: the source key is the item's `source`, `|`, and the most specific non-empty mute option, in the order `repo`, `chat`, `sender` (`tag:value`). An item without one has no key and no memory.
- `resolve`: a remembered target is preselected and marked Last time.
  - A remembered Agent that is no longer listed resolves to the main chat, with `gone` set so the app says so once.
  - Nothing remembered, or an unreadable value, gives `null`: the platform's own default.
  - Cases with `needs_new_chat` apply only where New Chat is a target.
- `recency`: one entry per source, latest first, at most `limit` sources.

## Following up: state words, row line, timeline, showcase

These five files are the contract of following up an inbox item for the manager and the web app. The data is made up; `chat` is a placeholder connector id that the web app draws with their generic source icon. The showcase is in Chinese because its screenshots are read in zh-Hans.

| operation | File | Checked by | Input and output |
| --- | --- | --- | --- |
| `inbox_now` | `now.json` | Go (`expected.now`, `expected.section` from the item; the test comes with the manager's state-key change), web (`expected.text` in `en`, `zh-Hans`, `ja`) | `texts` is the one table of words; `compose` says how `waiting`, `check_due` and `failed` are put together; a case with `input.follow_by_lang` gives the model's `who` and `next` per language; `{time}` in the text is `check_at` in the app's own short format for a future moment, put in before comparing |
| `inbox_row_meta` | `row.json` | web | `expected.meta` per language is the row's third line and the detail's small line; `null` means no `where`, so the app keeps today's line |
| `inbox_timeline` | `timeline.json` | web | `folded` and `expanded` are the entry ids shown, in order; `live_state` lists the steps that show an Agent session's live state (only the newest step linking each session, from `input.sessions`); its `session_state` is the session's `state` key, which the web app puts in its Agents list's words; `steps[].icon` is `source` (the item's source mark), `reported` for every reported step, or an icon name for the kind, which the web app maps to the icon it draws for that kind today; `reported` steps are drawn apart: their `text` is the label and the words for apps from before, their `detail` the words alone, which an app that knows the kind shows under its own label (`text` only when `detail` is empty) |
| `inbox_follow_time` | `time.json` | web | `{time}` in a state line: `input.at` against `input.now` in the fixture's `time_zone`, `expected` per language |
| `inbox_showcase` | `showcase.json` | the web app's screenshot runs | the list, then the `detail` item with its timeline folded and expanded, at `now_clock`, in zh-Hans and en, light and dark; `sessions` are the linked conversations as the app's session list has them (wire sessions) |

Who may change what: the keys, sections and words are fixed with the contract; a change that needs a different input (how the manager marks a muted item, say) changes the case's input and says so.

## Section bar: choices, counts, landing and search

These three files are the contract of the section bar for the web app, which replays all three. The data is made up and the expected values come from the rules below, not from the web app.

| operation | File | Input | Expected |
| --- | --- | --- | --- |
| `inbox_bar` | `bar.json` | `inbox_list`'s input (items, interactions, sessions, mutes) with `settings.chosen` (`action`, `working`, `fyi`, `done` or `all`) in place of `settings.expanded`, and an optional `done_days_total` (the hub's day counts added up; absent on a hub without them) | `bar`: the choices shown, in order, each `{id, count, tone}`; `shown_sections`: the sections drawn (`inbox_list` group ids); `empty`: `null`, `caught_up`, `section`, `filtered` or `no_match` |
| `inbox_bar_landing` | `bar_landing.json` | one item opened from outside the list | `{choice, open_collapsed}` |
| `inbox_bar_search` | `bar_search.json` | `start` (`chosen`, `saved`, `query`) and `steps`: `{query}` (the search text becomes this), `{pick}` (a choice tapped) or `{enter}` (the Inbox entered with this search text) | `after`: `{chosen, saved}` after each step |

Rules the expected values follow:

- **Rows** are `inbox_list`'s: an item's section is its known `status`, else `tier` (fyi, noise), else Needs you; `done_at` wins. Waiting Agents count in Needs you when no source is chosen and the kind is all or Agent, muted ones never. Source, kind and search narrow every count.
- **Choices**: `action` and `all` always; `working` and `fyi` while they have a row or are chosen; `done` while it has a row, while the hub's day counts are above zero with no filter or search, or while chosen. Collapsed has no choice; it shows under `all`.
- **Counts**: `action` its rows with tone `needs`; `working` and `fyi` their rows with tone `neutral`; `done` and `all` `null`. A count of 0 stays in the data; the badge hides it.
- **Shown sections**: the chosen one when it has rows (Done also when the day counts name groups); under `all`, every section with rows in `inbox_list`'s order.
- **Empty**: Done chosen and empty is `filtered` with a source or kind, `no_match` with a search, else `section`. Otherwise nothing shown with rows somewhere else open is `section`; nothing open anywhere is `filtered`, `no_match` or `caught_up`, in that order.
- **Landing**: the item's section, Done for a done item, and `all` with `open_collapsed` for a collapsed one. When a list applies it (an outside opening, or a list shown beside the item) is the module doc's rule, not part of these cases.
- **Search**: text appearing saves the choice and shows `all`; a pick narrows; text cleared restores the saved choice (`action` when none). Entering the Inbox shows `action`, or `all` with `saved: action` when a kept search text comes with it.

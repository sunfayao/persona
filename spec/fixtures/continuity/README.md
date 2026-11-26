# Continuity of chat sync

This domain records the continuity behavior of chat sync, replayed step by step by the Go server. It follows `docs/protocol/protocol.md` ("Continuity of items", "Folded work"). The fixtures hold 38 cases and 94 event steps, all made up.

## Sources and rules

The abbreviation in the table is a read-only source in the repository:

- **Corpus**: [continuity_corpus_test.go](../../../server/internal/transcript/continuity_corpus_test.go).

| Rule | Source | Cases |
| --- | --- | --- |
| Consecutive batches merge by id; when `from_seq <= covered`, the new `covered` is `max(covered, to_seq)`. | Corpus `simClient.apply` | `append_then_fill_lost_batch`, `overlapping_batch_never_regresses_covered` |
| A row without items still counts toward `covered`. | the covering cases | `empty_batch_advances_covered` |
| With a baseline, `from_seq > covered` keeps the new messages without advancing the position and fills the gap at once from the current continuous position. | `docs/protocol/protocol.md`, "Continuity of items" | `append_then_fill_lost_batch` |
| A first push that continues from zero can establish the baseline directly. | the covering cases | `first_line_push_is_baseline` |
| With no baseline and the first page not yet arrived, wait for it; if it arrived and there is still no baseline, fetch the latest page. | the covering cases | `wait_for_first_backlog`, `lost_first_push_after_empty_backlog` |
| A subscription backlog with `more_after: true` keeps filling. | the covering cases | `capped_subscription_requests_rest` |
| A gap is filled at once; an in-flight fill is deduplicated, and changes heard while waiting are read once more when the round ends. | the covering cases | `fill_reads_current_covered` |
| If the eighth page still asks for more filling, fetch the latest page at once and send no ninth `after_seq` request. | Corpus `simRun.fill` | `eight_fill_pages_fall_back_to_latest` |
| When `history` changes, clear the old state; a complete first page or a batch that starts from zero is accepted directly, any other continuing batch asks for a reload. | the covering cases | `history_changes_reset_local_items`, `new_history_from_zero_is_self_contained` |
| When the first item of the latest page is beyond `covered + 1`, replace the cache; adjacent or overlapping pages merge. | the covering cases | `latest_page_replaces_distant_cache`, `adjacent_latest_page_merges` |
| A reloaded page keeps pushes of the same history that run past its end; with a different history it does not. | the covering cases | `reload_keeps_overtaking_pushes`, `reload_discards_other_history_pushes`, `eight_fill_pages_fall_back_to_latest` |
| A reconnecting subscription carries `since_seq=covered` only when `covered > 0`, and also requests `fold_work`. | `docs/protocol/protocol.md`, "Continuity of items" | `reconnect_subscribes_from_covered`, `reconnect_without_baseline_omits_since_seq` |
| A missing `has_more` means the response gave no information and keeps the local judgment. | web paging rules | `missing_has_more_stays_unknown`, `catch_up_cannot_end_history` |
| A batch that does not reach the oldest local message cannot turn `has_more` false, nor can a backlog before the first page. | the covering cases | `first_catch_up_does_not_override_cached_history`, `catch_up_cannot_end_history` |
| `merge` accepts a paging flag only for a backlog that is the first load or whose merged count does not exceed the batch's count; accepting false also requires covering the oldest message. | the covering cases | `page_reaching_oldest_can_end_history`, `smaller_page_cannot_overwrite_history_flag` |
| History paging merges with `prepend` and sets `has_more` directly, without changing `covered` or `history`. | the covering cases | `older_page_sets_has_more_without_moving_covered` |
| Without `to_seq`, merge by the rules of a hub that sends none and advance to the last item's position; `more_after` can only trigger a reload. | the covering cases | `legacy_batches_merge_without_gap_detection` |
| A full item with a repeated id uses the newest copy; duplicates in the cache and in one batch also keep the last copy, and an existing visibility mark is not downgraded. | the covering cases | `same_id_plain_item_uses_newest_copy`, `duplicate_cache_and_batch_keep_newest` |
| A new fold replaces the work in its range; a fold with the same id and a larger range also replaces the old one. | the covering cases | `fold_replaces_work_and_expands_in_pages` |
| A fold's closed range is judged by seq and the numeric block index; narration inside it is replaced, work outside it and the final text are kept. | the covering cases | `fold_range_uses_numeric_block_bounds`, `fold_preserves_steps_outside_range` |
| Expanding fetches `from_seq…through_seq` page by page, continues from the last seq plus one while `more`, and replaces the fold once when done. | the covering cases | `fold_replaces_work_and_expands_in_pages` |
| A fold in an expand response is no longer merged; expanding changes neither the continuous position nor the paging judgment. | the covering cases | `unfold_discards_fold_items_in_response` |

## JSON structure

Each topic file is `{"version": 1, "cases": [...]}`. A case has the common fields `name`, `description`, `source`, `input`, `expected`, and `from_test` or `derived`.

- `adjacency.json`: continuous position, empty-batch boundaries and a gap of exactly one.
- `append_and_gaps.json`: normal appends, gaps, the first-batch baseline, the fill limit.
- `history_and_cache.json`: history rewrites, cache replacement, reconnects.
- `pagination_and_legacy.json`: paging information, a hub that sends no range, repeated ids.
- `folded_work.json`: fold merging and expansion.

`input.initial` is the state before the event sequence; every field is required:

| Field | Meaning |
| --- | --- |
| `items` | The wire Item array, sorted by `(seq, the number after the last # of the id)`; keeps text, tool links and fold information for comparison |
| `covered` | The row up to which coverage is continuous from the locally loaded range; zero means no baseline yet and cannot be replaced by the largest `item.seq` |
| `history` | The transcript identity, a string or `null`; the examples `h`, `old` and `new` are made up |
| `has_more` | `true`: older messages exist; `false`: known to be none; `null`: no valid paging judgment accepted yet |
| `loaded_once` | Whether a backlog has been received; a disk cache alone does not make it true |

`has_more: null` is the fixture's record of an information state. The implementation's `hasMore` is a Bool, false initially and after a reset, with no separate unknown storage; when comparing, `null` is projected to false. A missing or `null` response field means "not provided" and never turns an existing true or false into unknown; only a history reset clears that judgment.

`input.events` and `expected` have the same length and correspond by index. Each step's expected holds the full state fields, `item_ids` in order, `gap` (`none`, `fill` or `reload`) and `actions`. `items` must be compared in full, so that a change in text or fold range is not missed by checking ids alone. `gap` is the return value of this step's `apply`; an event without `apply` is `none` and does not mean that an asynchronous task started earlier was cancelled.

### Events

| `kind` | Handling and fields |
| --- | --- |
| `http_latest` | The HTTP first-page response: `items`, optional `history/from_seq/to_seq/has_more`; `apply` is called with `backlog=true` |
| `ws_items` | A WS push or subscription backlog: `items`, optional `history/from_seq/to_seq/backlog/has_more/more_after`; a missing Bool parameter defaults to false, and `has_more` stays unknown |
| `continue_fill` | A gap found again while a fill is in flight: the fill gate sends no further request and does not wait for a timer |
| `http_after` | The response to the current fill request: `items`, optional range fields and `more_after`; `has_more` is not passed to storage |
| `http_reload` | The response of reloading the latest page: `items`, required `has_more`, optional range fields; calls `replace`, which is not the same as the first `apply` |
| `load_older` | The user asks for history; with local `has_more=true` and non-empty items, a `before_seq` request is sent |
| `http_older` | The history page response: `items`, required `has_more`; calls `prepend`, and range fields take no part in the continuity judgment |
| `reconnect` | A reconnect completed; the subscribe action is built from the current `covered` |
| `open_fold` | Takes an existing `fold_id` and starts a range request; this format expands one fold at a time |
| `http_range` | The range response of the fold being expanded: `items`, optional `more`; between pages only work accumulates, and `unfold` is called at the end |

These are replay events, not new wire envelopes. The virtual task counter of each case starts at zero; a response follows its request or is the result of a request already in flight. HTTP failures, task cancellation, parallel expansion and scheduling across sessions are outside this format.

### Actions

`actions` lists only the actions this step produced; an empty array means none, not a snapshot of every pending task.

| `kind` | Parameters and meaning |
| --- | --- |
| `fetch_after` | `after_seq`, the position to continue a fill from |
| `fetch_latest` | `fold_work: true`; fetch the latest page instead |
| `subscribe` | `fold_work: true`; `since_seq` when a baseline exists, otherwise the key is omitted |
| `fetch_older` | `before_seq`, `fold_work: true` |
| `fetch_range` | `from_seq`, `through_seq`, both inclusive; the next page continues at the last seq plus one |

Machine, session, authentication, `lazy_images` and other common request parameters that do not affect this domain's judgment are omitted. `from_seq` is a continuation cursor: the batch covers `from_seq + 1` to `to_seq` (rows without items included); `covered` is the position already covered continuously. A gap triggers `fetch_after` at once; there is no fill timer. Tests do not sleep; `continue_fill` expresses in-flight deduplication.

## Checking

```bash
cd server
go test -count=1 -p 1 ./internal/transcript/...
```

[continuity_fixtures_test.go](../../../server/internal/transcript/continuity_fixtures_test.go) reads every JSON in this directory, rejects unknown fields, unknown events, duplicate names and cases without step-by-step expectations, and compares the full items, position, paging information, load flags and actions at every step.

The Go test leaves the existing corpus test unchanged and additionally calls the existing `simClient.apply` for these seven scenarios: `append_then_fill_lost_batch`, `empty_batch_advances_covered`, `overlapping_batch_never_regresses_covered`, `first_line_push_is_baseline`, `wait_for_first_backlog`, `capped_subscription_requests_rest`, `latest_page_replaces_distant_cache`. The remaining semantics use a test model written from the current implementation, so the corpus's existing behavior is not changed.

## Differences from the shorthand rules

- A repeated id uses the newest complete copy for plain items and folds alike; an expanded fold and the supervision mark are kept separately. Comparing ids alone is not enough.
- The protocol flow chart draws every history change as a reload; the implementation clears and accepts a new first page, or a batch that starts from zero, directly when it is self-contained.
- "More than eight fill pages" means, in the implementation, that the eighth page still needing more reloads; no ninth page is requested first.
- An unknown `has_more` stays optional in the response, while internally it is a Bool; a missing field cannot erase an existing paging judgment, and a false in an empty batch does not prove that the start has been reached.
- `replace` does not set `loadedOnce` to true by itself; after a history reset followed by a reload it stays false, and the fixtures keep this detail.

All of these follow the current implementation.

## Adding cases

Locate the implementation branch and the existing test, then choose the topic file. Add an English snake_case name and a description, with a findable source; use `from_test` for a ported unit test and `derived: true` for an added branch. When a case reuses an existing test but adds timer, request or range-paging steps, `source` also lists the scheduling implementation.

Write an independent `input.initial` and the full expected of each event by hand, with example content that exists in no real conversation. Run the Go replay and update the rule table above; when adding an action that belongs only to the scheduling layer, state what cannot be verified on its own. Finally check the JSON layout, privacy and the commit message against the top-level conventions.

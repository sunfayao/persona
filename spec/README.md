# Shared behavior specifications

This directory holds the behavior specifications and fixtures that the Go server and the web app share. Both sides read the same JSON files and compare case by case.

**Behavior is defined by the current server implementation and its unit tests.** When a document and the implementation differ, keep the existing behavior and record the difference and its source in the domain README. To change behavior, update the specification and the fixtures first, then both implementations.

## Directory

Each domain is `spec/fixtures/<domain>/`, with a README that holds its rules and cases.

| Domain | What it pins |
| --- | --- |
| `continuity` | Chat sync: continuous positions, gap filling after a lost batch, history changes, the three paging states, reconnects, fold replacement and expansion |
| `answer` | Replies and interactions: how an answer is formed and gated |
| `approval` | The approval policy table: for an entry point, an interaction kind and a lock state, the available actions |
| `question-review` | The review step of a question card with several questions |
| `wire` | Wire JSON samples that the server decodes |
| `injected` | How the server recognizes the messages it injects for the model (task notifications, scheduled tasks, ...) |
| `comments` | The `> Comment N` / `Code comment N` / `Element comment N` blocks a message leads with, written and read back by the web app |
| `sync` | Stories of one chat on one or more devices (sends, cards, reconnects, hub restarts, wakes, long chats) with faults, checked at every step by Go, the web app and the end-to-end runs |
| `fold-changes` | The files a run of work changed, counted by the hub for a fold and by the web app for the same calls unfolded |
| `fold-sources` | The web pages a run of work used, read the same two ways |
| `graph-layout` | An extension view's `graph`: slots, edges, reading order, fold counts and group frames, written by `scripts/gen/graph_layout.py` and checked by the web app and the hub |
| `input-clip` | The whole-input bound after the hub's and the runtime SDK's string passes |
| `attention` | Persona Link's "Needs you" summary against the web badge |
| `entries` | Which of the Assistant's two composer entries (Needs you, In progress) each task, Agent, plan and message card goes to |
| `triage` | The To-dos page's sections (the status SOP), related rows and counts |
| `todo-view` | The view the manager works out for each to-do (phase, section, reason, line, asks), held to `schema/wire/todo-view.schema.json` |
| `todo-counts` | A To-dos list's counts, its places and the order within each section, with the same schema |
| `todo-rounds` | A to-do's conversation in rounds, its results and what changed since the owner looked |
| `todo-writes` | How a to-do's message or answer that carries the owner's words ends: the manager's status and code, the hub's final `todo_send_state` and receipt |
| `todo-push` | A decision's push: what the manager sends, and the push's category, `mutable-content` and `todo` |
| `provider-import-link` | Provider import links: what the web app builds and what the hub reads |
| `deep-links` | The route table of `docs/protocol/deep-links.md`: the paths the hub writes and the page each client opens |
| `agent-names` | What every client calls each coding agent |
| `permission-modes` | What every client calls each permission mode, in English, Simplified Chinese and Japanese |
| `chat-search` | What chat search and the assistant's chat index find of each item |
| `chat-export` | A conversation exported as Markdown |
| `session-search` | Ranked runtime-Agent search, query operators and shared local/server matching |
| `side-fold` | Side sessions folded under their parent's row |
| `inbox` | The inbox list behavior |
| `recents` | New Agent's recent setups |
| `forward` | Forwarded messages |
| `task-repositories` | Repository references of a task, shared by the manager and the web app |
| `screen-link`, `screen-udp`, `screen-video`, `screen-tiers`, `screen-input` | The live screen's session wire, video over UDP, video wire, quality tiers and pixel scroll record |
| `file-types` | A `.ts` file's type from its bytes: TypeScript or an MPEG transport stream |
| `review` | What the node's `git.review` answers (empty fields left out) |

`assistant-launchers`, `work-reports` and `workspace-naming` hold fixtures without a README of their own. A few files directly under `spec/fixtures/` (`agent-sharing.json`, `repository-remotes.json`) are single-purpose fixtures read by their tests.

## Fixture conventions

- Fixtures live in `spec/fixtures/<domain>/*.json`: UTF-8, two-space indent, a trailing newline, split by topic.
- Every domain has a README: one sentence per rule with its source and the names of the cases that cover it; the JSON structure, defaults, how results are compared, the verification commands and how to add a case.
- Every case has `name`, `description`, `source`, `input` and `expected`. `name` is unique within the domain, in English snake_case; across domains refer to a case as `<domain>/<name>`. `description` is one sentence.
- `source` records a findable origin for the rule: a Go or TypeScript file and function, a unit test, a document section. A case taken straight from an existing unit test adds `from_test`; a boundary case derived from the implementation adds `derived: true` and says how it was checked against the implementation. When a case extends an existing test scenario, keep the test source and say what was added.
- `input` and `expected` are defined by each domain and do not depend on Go or TypeScript runtime types. Keep the order of arrays and the difference between a missing field, `null`, `false` and `0`; any difference that does not affect behavior must be stated in the domain README.
- Wrap the cases of one topic as `{"version": 1, "cases": [...]}`; a domain that uses another top-level shape says so in its README. When a format changes, update every consumer, so that no reader silently skips an unknown event or an empty case set.
- All data is made up. Messages, identifiers, paths, addresses and times use plain example values: `/home/user/project` for paths, `example.com` for domains, documentation ranges for IP addresses. Nothing is copied from a real session.
- Where a Go implementation exists, a Go test reads the fixtures; where only the web app implements a rule, a web test reads them. List every assertion that an existing unit test makes and every verification limit that cannot be covered.
- Run `python3 scripts/privacy_check.py --staged` before committing and scan the commit message.

## Adding a case

Consumers discover the JSON of their domain recursively and run each case independently from its own initial state. An event sequence compares state and actions at every step, not only the final result, and never skips a failed step.

To add a case, find the server or web function or unit test it concerns, make up the smallest input and write an explicit expected value. Update the domain's rule index and run the existing implementation against it and the related tests. Never generate the expected value from the implementation under test and accept it as passing.

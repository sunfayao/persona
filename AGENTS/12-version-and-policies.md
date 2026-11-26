# Version control and policies

> How work moves through git here, what must never be committed, and the full prohibited and required lists. Read
> this before your first commit in a session and whenever you are unsure whether you may do something. You usually
> arrive from core constraint 11 of `AGENTS.md`.

---

## The workflow

| Step | Rule |
| --- | --- |
| Trunk | `main`, the only long-lived branch. There is no upstream to follow or merge from |
| Your branch | `task/<short-slug>` cut from the newest `origin/main`, in a worktree of its own. One task per branch |
| While working | Commit often on your branch. Merge `origin/main` into it when the trunk moves; do not rebase a branch someone else may have fetched |
| Verify | Build and test the pushed commit on the build machine (`09`) |
| Publish | Push your `task/*` branch. Merging into `main`, deploying, deleting branches and force-pushing happen only when the owner asks |
| Afterwards | Once merged, remove the worktree and the branch. Leftover branches and worktrees are clutter the owner has to clean |

There are no issues, claims, pull requests, reviews by other people, hosted CI lanes or releases. Anything in old
comments or documents that tells you to open one is history.

## Commits

- Conventional Commits in English: `feat(assistant): ...`, `fix(node): ...`, `refactor(web): ...`, `test: ...`,
  `docs: ...`, `chore: ...`. The subject says what changes for a reader of the log; the body says why, and what was
  verified when that is not obvious.
- No attribution or co-author trailers.
- Stage only your task's paths and read the staged diff before you commit. Never commit generated caches, build
  output, editor files or anything you did not mean to change.
- A commit builds. A commit that knowingly leaves a test red is `wip:` and says which.

## What never enters the repository

`scripts/privacy_check.py` runs on every commit and push through `scripts/hooks/` (enable once with
`git config core.hooksPath scripts/hooks`) and before every web build. It blocks private values taken from the
owner's own configuration, and generic secrets. Independently of it, you never write into a file, a test fixture, a
golden, a log line or a commit message:

- tokens, passwords, keys, cookies, connection strings with credentials;
- real machine names, host names, addresses, account names, e-mail addresses or message content of real people;
- absolute paths of the owner's home directory.

Fixtures use invented people and `example.com`. If the scan stops you, remove the value; do not weaken the scan or
skip the hook.

`.gitignore` already covers build outputs: `web/dist/`, `web/node_modules/`, `server/internal/runtimepkg/dist/*`,
`server/internal/hub/webdist/*`, generated WebAssembly under `web/public/pake/`, Python caches. If `git status` shows
one of those as new, something was built in the wrong place.

## Prohibited (the complete list)

| No. | You must not |
| --- | --- |
| F1 | put product logic, a product table, a model call or a manager import into `internal/hub` |
| F2 | add a feature as new files in `assistant`, `node` or `hub`; or import a root or a sibling domain from a domain package |
| F3 | add a line to an allowlist, baseline or golden to make a test pass |
| F4 | rename or reuse a route, RPC method, event type, tool name, JSON field, error code, text key in use, table, column or setting key |
| F5 | edit, rename or delete an applied migration, or put a migration in the wrong set |
| F6 | build prompt text in Go string literals outside `internal/prompts`, or write a prompt template in a language other than English |
| F7 | hard-code text a person reads, in any language, instead of using the catalogs; or use a catalog key in a prompt |
| F8 | edit a generated file, a golden, a wire sample or a baseline by hand |
| F9 | call `net.Listen`, package-level `regexp.MustCompile`, or `time.Local` / the machine's zone; start a child before `osproc.ReapInherited` |
| F10 | run a whole big package with plain `go test`, or report a run made without the test database as passing |
| F11 | send a chat message from the web app with a bare `api()` post, write `/api/...` URLs by hand, or use raw `localStorage` for a persisted key |
| F12 | statically import a lazy page from first-screen code, or add first-screen strings without building |
| F13 | read, edit or print the owner's live configuration, data directory or secrets; restart or signal a running service |
| F14 | merge into `main`, deploy, force-push, or delete branches without being asked; open issues or pull requests anywhere |
| F15 | commit a secret, a real name or address, or skip the privacy hook |
| F16 | add a compatibility path, a fallback or an option "in case" without a caller that needs it today |
| F17 | keep code, tests, fixtures, text keys or documents of a feature you removed |
| F18 | add a document, report or plan file that the task did not ask for |

## Required (the complete list)

| No. | You must |
| --- | --- |
| M1 | put a new feature in a domain package with one constructor, narrow interfaces it declares, no package state and an injected clock |
| M2 | register routes, node methods, capabilities, scopes and feature flags from the domain's own file |
| M3 | gate every new thing a peer must understand behind a capability or feature flag, and keep working when it is absent |
| M4 | bound request bodies, validate on the node, check the caller's role in the handler |
| M5 | return errors a person reads as `i18n.Err`, with a stable `code` when a client must branch |
| M6 | add text for people to all three languages, and delete a key when its last use goes |
| M7 | keep templates under `internal/prompts`, render through a typed function, and regenerate goldens with the documented switch |
| M8 | update a recogniser in the same commit as the injected message it recognises, keeping the legacy shapes |
| M9 | name a migration by its UTC second and make its DDL idempotent and backward compatible |
| M10 | change the Go type, the wire sample and `web/src/lib/types.ts` together when a client-visible shape changes |
| M11 | make new pages and dialogs lazy, use tokens and the components of `src/ui/`, and look at the change in light and dark on a phone and a desktop width |
| M12 | run build, vet, gofmt, the whole partitioned test run and `check-fast.sh` on the build machine before handing over; for web also `tsc -b` and `npm run build` |
| M13 | compare every failure with the trunk before calling it yours or not yours, and report results exactly |
| M14 | write code comments, documents, prompts and commit messages in English |
| M15 | update the rule file that owns a pattern when your change alters that pattern (the tables in `AGENTS.md` say which) |
| M16 | leave no branch, worktree, scratch file or build directory behind when the task is done |

## When a rule and the task collide

The owner's request in the conversation outranks these files. If the request needs something a rule forbids, say
which rule and why before you do it; do not quietly bend the rule, and do not quietly drop the request.

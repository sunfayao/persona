# This fork

This repository is our fork of [nwverse/persona](https://github.com/nwverse/persona). Two branches matter:

- `main` mirrors upstream `main` exactly. Pull request branches start from it, and nothing else is committed to it.
- `deploy` is what we build and run: upstream `main`, then this `fork/` folder as one commit, then each of our fixes
  that upstream has not merged yet, one squash merge per pull request branch, in the order `fork/patches` lists them.

Every fix still goes upstream as a pull request first. `deploy` only lets us run it before it is merged.

## Keeping up with upstream

From a clean checkout of `deploy` whose remotes are `origin` (this fork) and `upstream` (nwverse/persona), with the
GitHub CLI signed in:

```sh
fork/sync.sh --dry-run   # rebuild deploy locally and say what it applied; pushes nothing
fork/sync.sh             # rebuild deploy, then push main (fast-forward only) and deploy
```

The script fetches both remotes, resets the checkout to upstream `main`, commits `fork/` as it is in the checkout,
then squash-merges every branch in `fork/patches` whose pull request is still open, so a branch brought up to date by
merging `main` (as upstream asks) applies like any other. A merged pull request is already in upstream `main` and a
closed one is dropped; both are reported. When a branch does not apply, the script puts the checkout back where it was
and stops. `deploy` is pushed with `--force-with-lease`, so a `deploy` someone else pushed meanwhile is never
overwritten.

## Conflicts

- A fix that conflicts with upstream `main`: merge `main` into its branch (as upstream's workflow asks), push it, sync.
- Two of our fixes that touch the same lines (often one docs table): the script turns on `git rerere`, so resolve the
  collision once and every later sync replays it. From a clean checkout of `deploy`, squash-merge the later branch by
  hand (`git merge --squash --no-commit origin/<branch>`), fix the conflicted files, `git add` them and run
  `git rerere`, then `git reset --hard` and sync again. The recorded resolutions live in this clone's `.git/rr-cache`,
  so sync from the same clone.

## Adding or dropping a fix

Push the fix as its own branch from `main`, open its pull request upstream, then add `<branch> <pull request number>`
to `fork/patches`, commit that on `deploy` and run `fork/sync.sh`. To drop a fix before upstream decides, remove its
line and sync. A merged one drops out by itself.

When a maintainer continues one of our pull requests in a new one (a `pr-watch/repair-*` branch upstream), close ours
with a note pointing at the continuation, keep our branch at the continuation's head (merging main into it when it
falls behind), and change the line's number to the continuation's: the sync reads that pull request's state and
carries our branch while it is open, and drops it once it merges.

## Building

Build `deploy` the way upstream builds `main` (see `docs/` and `scripts/release/`). A build from `deploy` should say
so in its version (for example `-X main.channel=fork`), and its update address must not point at upstream's
releases, or a self-update would replace it with an upstream build that lacks these fixes.

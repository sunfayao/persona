#!/usr/bin/env bash
# Rebuild this fork's deploy branch from upstream main: fork/ as one commit, then every branch in fork/patches whose
# pull request upstream is still open, each as one squash merge (as upstream merges it). See fork/README.md.
#   fork/sync.sh            rebuild, then push main (fast-forward only) and deploy (--force-with-lease) to origin
#   fork/sync.sh --dry-run  rebuild the local checkout only
# Conflicts between our own fixes are resolved once by hand and replayed from then on by git rerere (fork/README.md).
set -euo pipefail

UPSTREAM_REPO=${UPSTREAM_REPO:-nwverse/persona}
dry=0
case "${1:-}" in
  --dry-run) dry=1 ;;
  "") ;;
  *) echo "usage: fork/sync.sh [--dry-run]" >&2; exit 2 ;;
esac

die() { echo "fork/sync.sh: $*" >&2; exit 1; }
cd "$(git rev-parse --show-toplevel)"
[ -f fork/patches ] || die "run it from a checkout of deploy (no fork/patches here)"
[ -z "$(git status --porcelain)" ] || die "commit or discard the checkout's changes first"
command -v gh >/dev/null || die "needs the GitHub CLI (gh) to see which pull requests are still open"
git var GIT_COMMITTER_IDENT >/dev/null 2>&1 || die "needs a commit identity (user.name and user.email, or GIT_AUTHOR_* and GIT_COMMITTER_*)"

git config rerere.enabled true
git config rerere.autoUpdate true
git fetch -q upstream main
git fetch -q origin
old=$(git rev-parse HEAD)
lease=$(git rev-parse -q --verify refs/remotes/origin/deploy || true)
# Commits a rebuild would drop: not on origin/deploy, not upstream's, not a fix this script squash-merged, and not fork/ only.
if [ -n "$lease" ] && [ -n "$(git rev-list "$lease..HEAD" --not refs/remotes/upstream/main --invert-grep --grep='carried on deploy)$' -- . ':(exclude)fork')" ]; then
  die "this checkout has commits outside fork/ that origin/deploy does not; push them as a fix branch and list it in fork/patches instead"
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
cp -R fork "$tmp/fork"
restore() { git reset -q --hard "$old"; }

git reset -q --hard refs/remotes/upstream/main
rm -rf fork
cp -R "$tmp/fork" fork
git add fork
git commit -q -m "chore(fork): deploy branch tooling (fork/)"

report=""
applied=0
while read -r branch pr _; do
  case "$branch" in ''|'#'*) continue ;; esac
  state=$(gh pr view "$pr" --repo "$UPSTREAM_REPO" --json state --jq .state) || { restore; die "cannot read #$pr's state"; }
  if [ "$state" != OPEN ]; then
    report="$report  skipped $branch (#$pr is $state)\n"
    continue
  fi
  git rev-parse -q --verify "refs/remotes/origin/$branch" >/dev/null || { restore; die "origin has no branch $branch (#$pr)"; }
  if ! out=$(git merge --squash --no-commit "refs/remotes/origin/$branch" 2>&1); then
    # rerere (autoUpdate) stages the conflicts it has a recorded resolution for; anything left is new.
    left=$(git diff --name-only --diff-filter=U)
    if ! printf '%s' "$out" | grep -q CONFLICT || [ -n "$left" ]; then
      restore
      die "$branch (#$pr) conflicts in: ${left:-?}. If upstream main moved, merge main into that branch and push it; if two of our fixes collide, resolve once by hand (fork/README.md, \"Conflicts\") and run this again"
    fi
  fi
  if git diff --cached --quiet; then
    report="$report  nothing new from $branch (#$pr)\n"
    continue
  fi
  title=$(gh pr view "$pr" --repo "$UPSTREAM_REPO" --json title --jq .title) || title="$branch"
  git commit -q -m "$title (#$pr, carried on deploy)"
  applied=$((applied + 1))
  report="$report  applied $branch (#$pr)\n"
done < "$tmp/fork/patches"

echo "deploy = upstream main $(git rev-parse --short refs/remotes/upstream/main) + fork/ + $applied fix(es) -> $(git rev-parse --short HEAD)"
printf "%b" "$report"
if [ "$dry" = 1 ]; then
  echo "dry run: nothing pushed; 'git reset --hard $old' goes back to the previous deploy"
  exit 0
fi
git push -q origin refs/remotes/upstream/main:refs/heads/main
if [ -n "$lease" ]; then
  git push -q --force-with-lease="refs/heads/deploy:$lease" origin HEAD:refs/heads/deploy
else
  git push -q origin HEAD:refs/heads/deploy
fi
echo "pushed main and deploy to origin"

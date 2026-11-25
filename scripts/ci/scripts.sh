#!/bin/bash
set -euo pipefail
source "$(dirname "$0")/common.sh"
# The Python suites take most of this lane; the other checks (about 70 s) run beside
# them, and their output follows the suites'. Every check runs even when another fails; the lane fails if any did.
others="$(mktemp "${TMPDIR:-/tmp}/persona-ci-scripts.XXXXXX")"
trap 'rm -f "$others"' EXIT
(
  rc=0
  ci_run 'Issue-link workflow tests' 120 node scripts/test/pr_needs_issue_test.mjs || rc=1
  ci_run 'Workflow syntax and shell lint' 180 actionlint || rc=1
  # One file per step: bash -n reads only its first file, the rest become that script's arguments.
  ci_run 'Site language check shell syntax' 30 bash -n scripts/release/site_language_check.sh || rc=1
  ci_run 'Publish script shell syntax' 30 bash -n scripts/release/publish.sh || rc=1
  ci_run 'Website worker, release and bilingual page tests' 180 node --test site/worker.test.mjs site/releases.test.mjs site/i18n.test.mjs site/home-layout.test.mjs || rc=1
  exit "$rc"
) > "$others" 2>&1 &
others_pid=$!
suites_rc=0
ci_run 'Python script suites and repository source/doc guards' 600 python3 scripts/ci/python-tests.py || suites_rc=$?
others_rc=0
wait "$others_pid" || others_rc=$?
cat "$others"
[ "$suites_rc" -eq 0 ] || exit "$suites_rc"
exit "$others_rc"

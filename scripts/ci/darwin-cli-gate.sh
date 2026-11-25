#!/bin/bash
# Decides whether a version tag signs the Darwin command-line programs.
#
# A release never ships an unsigned or ad-hoc program as if it were final: signing runs only when every credential it
# needs is configured. The caller passes "true"/"false" per credential (HAS_*), never a value. Missing credentials are
# not a failure: the hub image and GitHub Release are published, with the blocked signing result reported separately.
set -euo pipefail

tag="${RELEASE_TAG:?RELEASE_TAG is required}"
version="${tag#v}"
has() { [[ "${!1:-false}" == true ]]; }
out() { printf '%s=%s\n' "$1" "$2" >> "${GITHUB_OUTPUT:-/dev/stdout}"; }
say() {
  local message="$1" title="Darwin CLI signing"
  printf '%s\n' "$message"
  if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then printf -- '- %s\n' "$message" >> "$GITHUB_STEP_SUMMARY"; fi
  if [[ -n "${GITHUB_ACTIONS:-}" ]]; then printf '::notice title=%s::%s\n' "$title" "$message"; fi
}

missing=()
for name in MAC_CSC_LINK MAC_CSC_KEY_PASSWORD; do
  has "HAS_$name" || missing+=("$name")
done

darwin_cli=false
if (( ${#missing[@]} )); then
  say "Darwin CLI signing blocked: Environment secrets not configured: ${missing[*]}."
elif [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ || "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+-[0-9A-Za-z.-]+$ ]]; then
  darwin_cli=true
  say "Darwin CLI signing: Developer ID certificate is configured for $version."
else
  say "Darwin CLI signing blocked: tag $tag is not a semantic version."
fi
out version "$version"
out darwin_cli "$darwin_cli"

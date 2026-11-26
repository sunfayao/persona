#!/bin/sh
set -eu
cd "$(dirname "$0")/../.."

python3 scripts/ci/gofmt.py server sdk/go
if [ "$#" -gt 0 ]; then
  python3 scripts/ci/migration_freeze.py --base "$1"
else
  python3 scripts/ci/migration_freeze.py
fi
python3 scripts/ci/prompt_language_ratchet.py
python3 scripts/privacy_check.py

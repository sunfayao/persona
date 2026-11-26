#!/usr/bin/env python3
"""Keep CJK text out of the prompt templates: every prompt is written in English only.

A template that holds Chinese or Japanese text is listed, whether or not it tests .Language/.Lang: prompts carry
no language branches, and what the model writes for the owner follows the owner's language through the `lang`
template function (docs/assistant/prompts.md, "Language"). The inventory in the baseline may only shrink, and it is
empty now, so any new template with CJK text fails.

DATA names the templates that keep CJK text because it is data that must match something real: an earlier default
a hub may still hold byte for byte, identifiers a parser requires, literals another program writes. Adding one is a
change to this file, with its reason.

Only a new entry fails. A resolved template still in the baseline passes with a notice: lowering the baseline is
tidying anyone can do, with --update here or scripts/ci/lower_baselines.py for every count ratchet
(docs/operations/testing.md, "Count ratchets"). With a base given (--base, else CI_BASE_SHA), a template the
baseline still lists but the merge base already has clean may not gain CJK text again (scripts/ci/ratchet_base.py).
"""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import sys

sys.path.insert(0, str(Path(__file__).resolve().parent))
import ratchet_base  # noqa: E402


def _source_root():
    configured = os.environ.get("PERSONA_CI_SOURCE_ROOT")
    if configured:
        if not Path(configured).is_absolute():
            raise ValueError("PERSONA_CI_SOURCE_ROOT must be absolute")
        return Path(configured)
    return Path(__file__).resolve().parents[2]


ROOT = _source_root()
SOURCE = "server/internal/prompts"
BASELINE = "scripts/ci/prompt_language_baseline.json"
CJK = re.compile(r"[\u3040-\u30ff\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff]")
COMMENT = re.compile(r"{{-?\s*/\*.*?\*/\s*-?}}", re.S)
# Templates whose CJK text is data, not prompt wording: path under SOURCE -> why.
DATA = {
    "assistant/routine_morning_former1.md": "an earlier default routine, matched byte for byte against what a hub holds",
    "assistant/routine_morning_former2.md": "an earlier default routine",
    "assistant/routine_morning_former3.md": "an earlier default routine",
    "assistant/routine_morning_former4.md": "an earlier default routine (the last Chinese one)",
    "assistant/routine_evening_former1.md": "an earlier default routine",
    "assistant/routine_evening_former2.md": "an earlier default routine",
    "assistant/routine_evening_former3.md": "an earlier default routine (the last Chinese one)",
    "assistant/task_done.md": "quotes the delivery line older Agent replies end with",
    "tools/session_title.md": "the ten kind identifiers the title parser and the apps match",
    "inbox/group.md": "literals the Lark connector writes into the items",
    "inbox/tiers.md": "a literal the Lark connector writes into the items",
    "inbox/kinds/larkcli.md": "a marker older stored items hold",
    "mcp/imessage/imessage_read.md": "attachment placeholders the iMessage connector writes",
}


def scan(root):
    return scan_texts({p.relative_to(root).as_posix(): p.read_text(encoding="utf-8") for p in root.rglob("*.md")})


def scan_texts(texts):
    """The templates with CJK text among those given as {name relative to SOURCE: text}, by directory."""
    counts = {}
    for name, text in sorted(texts.items()):
        if name not in DATA and CJK.search(COMMENT.sub("", text)):
            counts.setdefault(str(Path(name).parent), []).append(name)
    return counts


def decode_baseline(data):
    if not isinstance(data, dict):
        raise ValueError("baseline must be a directory map")
    result = {}
    for directory, entry in data.items():
        if not isinstance(directory, str) or not isinstance(entry, dict) or set(entry) != {"count", "files"}:
            raise ValueError("invalid baseline entry")
        files = entry["files"]
        if not isinstance(files, list) or any(not isinstance(f, str) for f in files):
            raise ValueError("invalid baseline files")
        if type(entry["count"]) is not int or entry["count"] != len(files) or files != sorted(set(files)):
            raise ValueError("baseline count or ordering does not match files")
        if any(Path(f).is_absolute() or ".." in Path(f).parts or str(Path(f).parent) != directory or not f.endswith(".md") for f in files):
            raise ValueError("baseline path is outside its directory")
        if files:
            result[directory] = files
    return result


def encoded(counts):
    return {directory: {"count": len(files), "files": files} for directory, files in sorted(counts.items())}


def increases(actual, baseline):
    return sorted(f for directory, files in actual.items() for f in files if f not in baseline.get(directory, []))


def lowered(actual, baseline):
    added = increases(actual, baseline)
    if added:
        raise ValueError("prompt templates with CJK text (prompts are English only; data goes in DATA with a reason): " + ", ".join(added))
    return actual


def _git(*args):
    try:
        return subprocess.run(["git", *args], cwd=ROOT, capture_output=True, text=True, timeout=60)
    except subprocess.TimeoutExpired:
        raise ValueError("git " + " ".join(args[:3]) + " timed out")


def base_text(base, path):
    """The file's text at base, or None when base has no such file.

    Whether the file is there comes from the tree, not from reading it: CI's guards checkout is blobless (#12753), so
    the base's copy of a changed baseline is fetched on first read, and a failed fetch must not count as absence.
    """
    listed = _git("ls-tree", "-z", "--full-tree", base, "--", path)
    if listed.returncode:
        raise ValueError("cannot read the tree of ratchet base " + base)
    if not any(entry.partition("\t")[2] == path for entry in listed.stdout.split("\0")):
        return None
    shown = _git("show", base + ":" + path)
    if shown.returncode:
        raise ValueError("cannot read {0}:{1}: {2}".format(base, path, shown.stderr.strip()))
    return shown.stdout


def resolved(actual, baseline):
    """Files the baseline lists that are English now (or are gone): lowering it would drop them."""
    return sorted(f for directory, files in baseline.items() for f in files if f not in actual.get(directory, []))


def regained(actual, baseline, merge_base):
    """Listed omissions the merge base had already resolved, which the change brings back."""
    if merge_base is None or not any(path.startswith(SOURCE + "/") for path in merge_base.changed()):
        return []  # with no prompt touched since the merge base, it has the same omissions
    names = [name for name in merge_base.files(SOURCE) if name.endswith(".md")]
    texts = merge_base.texts(names, errors="strict")
    before = scan_texts({name[len(SOURCE) + 1:]: text for name, text in texts.items()})
    return sorted(f for directory, files in actual.items() for f in files
                  if f in baseline.get(directory, []) and f not in before.get(directory, []))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--update", action="store_true")
    parser.add_argument("--base", help="base ref (default: CI_BASE_SHA). The merge-base limit needs one; the "
                                       "check that the baseline was not raised falls back to origin/main")
    args = parser.parse_args()
    path = ROOT / BASELINE
    # Only a given base holds a changed prompt to the merge base: origin/main is as fresh as the last fetch, and
    # main verification leaves it behind HEAD (scripts/ci/ratchet_base.py).
    prior_ref = args.base if args.base is not None else os.environ.get("CI_BASE_SHA", "origin/main")
    try:
        baseline = decode_baseline(json.loads(path.read_text(encoding="utf-8")))
        actual = scan(ROOT / SOURCE)
        # Once the baseline exists upstream, editing its JSON cannot raise it.
        if _git("cat-file", "-e", prior_ref).returncode:
            raise ValueError("cannot read ratchet base " + prior_ref)
        prior = base_text(prior_ref, BASELINE)
        if prior is not None:
            lowered(baseline, decode_baseline(json.loads(prior)))
        lowered(actual, baseline)
        back = regained(actual, baseline, ratchet_base.resolve(ROOT, args.base))
        if back:
            raise ValueError("prompts the merge base already has in English gained CJK text again: " + ", ".join(back)
                             + "; the baseline still lists them, but a count may not rise into that room")
        if args.update:
            path.write_text(json.dumps(encoded(actual), ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        elif actual != baseline:
            gone = resolved(actual, baseline)
            print("prompt language ratchet: {0} resolved prompt(s) still in the baseline; lowering it is tidying "
                  "anyone can do, not this change's job: python3 scripts/ci/lower_baselines.py, or python3 "
                  "scripts/ci/prompt_language_ratchet.py --update: {1}".format(len(gone), ", ".join(gone)))
    except (OSError, ValueError, UnicodeDecodeError, ratchet_base.BaseError) as error:
        print("FAIL prompt language ratchet:", error)
        return 1
    print("PASS prompt language ratchet:", sum(map(len, actual.values())), "remaining templates in", len(actual), "directories")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

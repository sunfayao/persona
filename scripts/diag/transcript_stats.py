#!/usr/bin/env python3
"""Count record kinds in Claude Code and Antigravity transcripts and Codex rollouts.

    scripts/diag/transcript_stats.py [dir ...]

Defaults: ~/.claude/projects, $CLAUDE_CONFIG_DIR/projects, ~/.codex/sessions and Antigravity's full brain exports.
Prints counts, no content, and a sample file:line for Claude and Codex records. Antigravity is counts only.
Use --json to omit all paths.
"""
import argparse
import collections
import glob
import json
import os
import re
import sys

from assistant_token_report import transcript_files

SNAPSHOT_VERSION = 1


def files(dirs):
    return transcript_files(map(os.path.expanduser, dirs))


def agy_kinds(step):
    kind = step.get("type")
    if isinstance(kind, str) and re.fullmatch(r"[A-Z_]+", kind):
        yield f"agy {kind}"
    calls = step.get("tool_calls")
    if not isinstance(calls, list):
        return
    for call in calls:
        name = call.get("name") if isinstance(call, dict) else None
        if isinstance(name, str) and re.fullmatch(r"[a-z][a-z0-9_]*", name):
            name = re.sub(r"^mcp_persona_(?:[a-f0-9]{8,}_)+", "mcp_persona_", name)
            yield f"agy tool:{name}"


def collect(dirs):
    kinds, where = collections.Counter(), {}
    versions = collections.Counter()
    nfiles = nlines = 0
    for path in files(sorted(set(dirs))):
        nfiles += 1
        with open(path, errors="replace") as f:
            for i, line in enumerate(f, 1):
                nlines += 1
                try:
                    e = json.loads(line)
                except ValueError:
                    kinds["(bad json)"] += 1
                    if os.path.basename(path) != "transcript_full.jsonl":
                        where.setdefault("(bad json)", f"{path}:{i}")
                    continue
                if not isinstance(e, dict):
                    continue
                keys = []
                if type(e.get("step_index")) is int:
                    keys.extend(agy_kinds(e))
                elif "payload" in e:  # Codex rollout
                    p = e.get("payload") or {}
                    k = f"codex {e.get('type')}/{p.get('type') if isinstance(p, dict) else ''}"
                    if isinstance(p, dict) and p.get("type") == "item_completed":
                        item = p.get("item")
                        k += "/" + str(item.get("type") if isinstance(item, dict) else None)
                    version = p.get("cli_version") if isinstance(p, dict) else None
                    if isinstance(version, str) and version:
                        versions["codex " + version] += 1
                    keys.append(k)
                else:
                    t, st = e.get("type"), e.get("subtype")
                    keys.append(f"claude {t}" + (f"/{st}" if st else ""))
                    if t == "attachment":
                        attachment = e.get("attachment")
                        keys.append(f"claude attachment:{attachment.get('type') if isinstance(attachment, dict) else None}")
                    m = e.get("message")
                    if isinstance(m, dict) and isinstance(m.get("content"), list):
                        for b in m["content"]:
                            if isinstance(b, dict):
                                keys.append(f"claude {t}:{b.get('type')}")
                    version = e.get("version")
                    if isinstance(version, str) and version:
                        versions["claude " + version] += 1
                for k in keys:
                    kinds[k] += 1
                    if not k.startswith("agy "):
                        where.setdefault(k, f"{path}:{i}")
    return {"schema_version": SNAPSHOT_VERSION, "files": nfiles, "lines": nlines,
            "kinds": kinds, "versions": versions}, where


def read_snapshot(path):
    with open(path, encoding="utf-8") as stream:
        snapshot = json.load(stream)

    def count(value):
        return type(value) is int and value >= 0

    if (not isinstance(snapshot, dict) or type(snapshot.get("schema_version")) is not int
            or snapshot["schema_version"] != SNAPSHOT_VERSION
            or not all(count(snapshot.get(key)) for key in ("files", "lines"))
            or not all(isinstance(snapshot.get(key), dict) and all(count(value) for value in snapshot[key].values())
                       for key in ("kinds", "versions"))):
        raise ValueError(f"expected a version {SNAPSHOT_VERSION} transcript statistics snapshot with nonnegative counts")
    return snapshot


def print_changes(baseline, current):
    print(f"Files: {baseline['files']} -> {current['files']}; lines: {baseline['lines']} -> {current['lines']}.")
    for key, title in (("kinds", "Record kinds"), ("versions", "Runtime versions")):
        print(f"{title}:")
        old, new = baseline[key], current[key]
        changed = False
        for name in sorted(old.keys() | new.keys()):
            if name not in old:
                print(f"  + {name}: {new[name]}")
            elif name not in new:
                print(f"  - {name}: {old[name]}")
            elif old[name] != new[name]:
                print(f"  ~ {name}: {old[name]} -> {new[name]}")
            else:
                continue
            changed = True
        if not changed:
            print("  No changes.")


def main():
    parser = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    parser.add_argument("dirs", nargs="*", help="transcript files or directories (default: Claude Code, Codex and Antigravity)")
    output = parser.add_mutually_exclusive_group()
    output.add_argument("--json", action="store_true", help="write a baseline of counts without sample paths")
    output.add_argument("--compare", metavar="BASELINE", help="compare the current counts with a saved JSON baseline")
    parser.add_argument("--against", metavar="SNAPSHOT",
                        help="compare with a second saved JSON snapshot instead of scanning (requires --compare)")
    args = parser.parse_args()
    if args.against is not None and (args.compare is None or args.dirs):
        parser.error("--against requires --compare and no transcript directories")
    if args.compare is not None:
        try:
            baseline = read_snapshot(args.compare)
        except (OSError, ValueError) as error:
            parser.error(f"cannot read baseline: {error}")
        if args.against is not None:
            try:
                current = read_snapshot(args.against)
            except (OSError, ValueError) as error:
                parser.error(f"cannot read comparison snapshot: {error}")
            print_changes(baseline, current)
            return
    dirs = args.dirs
    if not dirs:
        brain = os.path.expanduser("~/.gemini/antigravity-cli/brain/*/.system_generated/logs/transcript_full.jsonl")
        dirs = ["~/.claude/projects", os.path.join(os.environ.get("CLAUDE_CONFIG_DIR", "~/.claude"), "projects"),
                "~/.codex/sessions", *glob.glob(brain)]
    snapshot, where = collect(dirs)
    if args.json:
        json.dump(snapshot, sys.stdout, indent=2, sort_keys=True)
        print()
        return
    if args.compare is not None:
        print_changes(baseline, snapshot)
        return
    print(f"{snapshot['files']} files, {snapshot['lines']} lines")
    print("versions:", ", ".join(v for v, _ in snapshot["versions"].most_common(8)))
    for k, n in sorted(snapshot["kinds"].items()):
        print(f"{n:8d}  {k:60s} {where[k]}" if k in where else f"{n:8d}  {k}")


if __name__ == "__main__":
    main()

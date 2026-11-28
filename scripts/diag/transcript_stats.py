#!/usr/bin/env python3
"""Count record kinds in Claude Code and Antigravity transcripts and Codex rollouts.

    scripts/diag/transcript_stats.py [dir ...]

Defaults: ~/.claude/projects, $CLAUDE_CONFIG_DIR/projects, ~/.codex/sessions and Antigravity's full brain exports.
Prints counts, no content, and a sample file:line for Claude and Codex records. Antigravity is counts only.
Use --json to omit all paths.
"""
import argparse
import collections
from contextlib import closing, contextmanager
import glob
import json
import os
import re
import sqlite3
import subprocess
import sys
from pathlib import Path

from assistant_token_report import transcript_files

SNAPSHOT_VERSION = 1


def files(dirs):
    paths = [os.path.expanduser(path) for path in dirs]
    for path in tuple(paths):
        if os.path.isdir(path):
            for pattern in ("*.jsonl.zst", "thread_history_*.sqlite"):
                paths.extend(glob.glob(os.path.join(path, "**", pattern), recursive=True))
    return transcript_files(paths)


@contextmanager
def transcript_lines(path):
    if path.endswith(".jsonl.zst"):
        with subprocess.Popen(["zstd", "-dc", "--", path], stdout=subprocess.PIPE,
                              text=True, errors="replace") as decoder:
            yield decoder.stdout
            if decoder.wait():
                raise ValueError("could not read a compressed transcript")
    elif path.endswith(".sqlite"):
        def rows():
            uri = Path(path).resolve().as_uri() + "?mode=ro"
            with closing(sqlite3.connect(uri, uri=True)) as db:
                for (raw,) in db.execute("SELECT item_json FROM thread_items"):
                    try:
                        item = json.loads(raw)
                    except (TypeError, ValueError):
                        item = None
                    yield json.dumps({"type": "thread_item", "payload": item})
        yield rows()
    else:
        with open(path, errors="replace") as stream:
            yield stream


def rollout_agent(path, payload):
    engine = payload.get("originator") if isinstance(payload, dict) else None
    if isinstance(engine, str) and "trae" in engine.lower():
        return "traex"
    parts = Path(path).parts
    if Path(path).name.startswith("traex-") or any(part.startswith((".trae", ".traex")) for part in parts):
        return "traex"
    return "codex"


# Record kinds handled by the source adapters. Internal-only rows are kept here with their drop reason.
CLI_RECORDS = {
    "agy": set("""
        CHECKPOINT ERROR_MESSAGE GENERIC PLANNER_RESPONSE SYSTEM_MESSAGE USER_INPUT
        tool:ask_question tool:edit_file tool:manage_task tool:read_file tool:run_command
        tool:schedule tool:search_web tool:update_steps tool:write_file
    """.split()),
    "claude": set("""
        ai-title assistant assistant:text assistant:thinking assistant:tool_use assistant:redacted_thinking
        attachment attachment:queued_command last-prompt mode queue-operation system/away_summary
        system/local_command system/turn_duration user user:image user:text user:tool_result
    """.split()),
    "rollout": set("""
        event_msg/item_completed/AgentMessage event_msg/item_completed/CommandExecution
        event_msg/item_completed/FileChange event_msg/item_completed/TaskNotification event_msg/item_completed/UserMessage
        event_msg/item_completed/Reasoning event_msg/item_completed/Plan event_msg/item_completed/ContextCompaction
        event_msg/task_complete event_msg/task_started event_msg/thread_settings_applied event_msg/token_count
        event_msg/turn_aborted event_msg/thread_goal_updated
        response_item/function_call response_item/function_call_output session_meta/None
        token_usage_record/None turn_context/None thread_item/userMessage thread_item/agentMessage
        thread_item/commandExecution thread_item/fileChange thread_item/reasoning thread_item/collabAgentToolCall
        thread_item/terminalInteraction thread_item/taskNotification thread_item/webSearch
    """.split()),
}
DROPPED_RECORDS = {
    "claude": {
        "atis-latch": "terminal UI state",
        "file-history-snapshot": "undo state",
        "tools-used": "duplicates tool calls",
        **dict.fromkeys((
            "attachment:agent_listing_delta", "attachment:command_permissions", "attachment:date",
            "attachment:deferred_tools_delta", "attachment:environment", "attachment:instructions", "attachment:model",
            "attachment:prompt_snapshot", "attachment:remote_session_change", "attachment:session_context",
            "attachment:skill_listing",
        ), "model input context or terminal state"),
    },
    "rollout": {
        "world_state/None": "runtime resource inventory",
        "response_item/custom_tool_call": "completion is represented by the corresponding display item",
        "response_item/custom_tool_call_output": "output is represented by the corresponding display item",
    },
}


def unmapped(kinds):
    for key, count in sorted(kinds.items()):
        engine, _, record = key.partition(" ")
        domain = "rollout" if engine in ("codex", "traex") else engine
        if record in CLI_RECORDS.get(domain, ()) or record in DROPPED_RECORDS.get(domain, ()):
            continue
        if engine == "agy" and record.startswith("tool:mcp_"):
            continue  # names are open within the MCP tool mapping
        yield key, count


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
        engine = rollout_agent(path, None)
        with transcript_lines(path) as f:
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
                    if e.get("type") == "session_meta":
                        engine = rollout_agent(path, p)
                    k = f"{engine} {e.get('type')}/{p.get('type') if isinstance(p, dict) else ''}"
                    if isinstance(p, dict) and p.get("type") == "item_completed":
                        item = p.get("item")
                        k += "/" + str(item.get("type") if isinstance(item, dict) else None)
                    version = p.get("cli_version") if isinstance(p, dict) else None
                    if isinstance(version, str) and version:
                        versions[engine + " " + version] += 1
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
    output.add_argument("--unmapped", action="store_true", help="list native kinds with no mapping or declared drop reason")
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
    try:
        snapshot, where = collect(dirs)
    except (OSError, ValueError, sqlite3.Error) as error:
        parser.error(f"cannot scan transcripts: {error}")
    if args.unmapped:
        missing = list(unmapped(snapshot["kinds"]))
        for key, count in missing:
            print(f"{count:8d}  {key}")
        print(f"{len(missing)} unmapped kinds in {snapshot['files']} files, {snapshot['lines']} records")
        return bool(missing)
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
    sys.exit(main())

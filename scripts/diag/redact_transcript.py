#!/usr/bin/env python3
"""Turn real Claude Code, Codex, TRAE and Antigravity transcripts into test fixtures with nothing personal left in them.

    scripts/diag/redact_transcript.py -o out.jsonl SRC.jsonl:1-60,155-175 [SRC2.jsonl:3895-3915 ...]

Whitelist, not blacklist: the JSON structure, keys that look like identifiers, enum-like values of known keys and a
small vocabulary of shell / Claude Code words stay; every other word becomes a random pseudo-word of the same length
and script (consistent within one run, so ids that appear in several places still match). UUIDs and message / tool ids
are remapped, timestamps shifted to 2026-01-01, paths moved under /work/project, base64 images replaced by a 1x1 PNG.
Antigravity steps keep native argument types and fixed parser markers. Use transcript_full.jsonl, not its cut export.
An enum-like value that names a host or URL (git.example.invalid:team/repo, https://example.invalid/) is redacted like text.

After running it, review the output by hand before committing: no internal domains / IPs, host names, people, emails,
repository names or tokens. `--words` lists every string kept as is: vocabulary words on the first line, then one line
each for object keys, enum values, versions, model names, the mcp__ prefix, tag and attribute names, file extensions.
"""
from __future__ import annotations

import argparse
import datetime as dt
import json
import os
import random
import re
import secrets
import sys
from urllib.parse import unquote

PNG_1x1 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
MAX_STR = 6000  # longer strings are cut (the parser's own clipping has unit tests)

ENUM_KEYS = {
    "type", "subtype", "role", "kind", "phase", "status", "level", "commandMode", "operation", "userType", "entrypoint",
    "permissionMode", "stop_reason", "stop_sequence", "media_type", "effort", "mode", "source", "hookEvent", "approval_policy",
    "change", "channel", "cronKind", "taskKind", "stop_type", "service_tier", "action",
}
VERSION_KEYS = {"version", "cli_version"}
MODEL_RE = re.compile(r"(claude|gpt|o\d|codex)[A-Za-z0-9._\[\]-]*")
ENUM_RE = re.compile(r"[A-Za-z0-9_.:/-]{1,48}")
DOTTED_RE = re.compile(r"[A-Za-z0-9-]+\.[A-Za-z0-9-]")
PATH_KEYS = {"cwd", "filePath", "file_path", "path", "filename", "notebook_path", "outputFile", "output_file"}
DROP_KEYS = {"signature", "encrypted_content"}  # opaque blobs
# Words that carry structure the parser or the apps look at, and are not anyone's data.
VOCAB = set("""
bin usr env zsh bash sh lc c ls cat cd git go rg sed grep echo true false null none ok
Request interrupted by user for tool use Async agent launched successfully agentId running in background with ID
Command Output is being written to Next wakeup scheduled moved the disable recaps config Image
model clear compact loop help resume rename init cost status
Bash Read Write Edit MultiEdit Glob Grep Agent Task TaskStop KillShell ScheduleWakeup CronCreate CronDelete TodoWrite
WebFetch WebSearch AskUserQuestion ExitPlanMode NotebookEdit ToolSearch Skill Monitor SendMessage TaskCreate TaskUpdate
exec wait spawn_agent request_user_input request_user_input_async update_plan
task id tool use status completed failed killed summary result output file notification
Background command event stream ended stopped was exit code finished started timeout with EOF gt lt amp quot
""".split())
UUID_RE = re.compile(r"[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}")
EXEC_ID_RE = re.compile(rf"\bexec-({UUID_RE.pattern})\b")
PREFIXED_ID_RE = re.compile(r"\b((?:msg|toolu|srvtoolu|call|req|fc|rs|exec|ws)_?-?)[A-Za-z0-9]{8,}\b")
IPV4_RE = re.compile(r"\b\d{1,3}(?:\.\d{1,3}){3}\b")
TS_RE = re.compile(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z")
TAG_RE = re.compile(r"</?([A-Za-z][A-Za-z0-9_-]*)((?:\s+[A-Za-z_-]+=\"[^\"]*\")*)\s*/?>")
WORD_RE = re.compile(r"[A-Za-z0-9_]+|[^\x00-\x7f\s\W]+", re.UNICODE)

AGY_TYPES = {"USER_INPUT", "PLANNER_RESPONSE", "GENERIC", "SYSTEM_MESSAGE", "CHECKPOINT", "ERROR_MESSAGE"}
AGY_MODEL_RE = re.compile(r"(?:gemini|gpt-oss)-[a-z0-9]+(?:[.-][a-z0-9]+)*")
AGY_PERSONA_RE = re.compile(r"mcp_persona_((?:[a-f0-9]{8,}_)+)([a-z][a-z0-9_]*)")
AGY_TOOLS = set("""
run_command read_file edit_file write_file update_steps manage_task manage_subagents schedule search_web ask_question
persona_call kb_list kb_search
""".split())
AGY_PHRASES = (
    "Created At:", "Completed At:", "Command exited with code", "Output:", "Log output:",
    "Tool is running as a background task with task id:", "Task Description:", "Task logs are available at:",
    "Task id", "finished with result", "was canceled", "was cancelled", "cancelled.",
    "Background command is still running after", "Reminder: work efficiently.",
    "All your subagents and background tasks have been stopped due to server restart",
    "The following is a <SYSTEM_MESSAGE> not actually sent by the user. It is provided by the system as important information to pay attention to.",
    "# Resuming from a compaction", "[Message]", "[Notice]", "Task:", "Status:", "Timer:", "Cron:",
    "No background tasks are currently running.", "model output must contain output text or tool calls",
)
# Ordered alternatives protect structure before pseudonymizing the words around it. Task suffixes are step
# indices, so only the conversation ID in a task handle changes.
AGY_TEXT_RE = re.compile(
    rf"(?P<stamp>\d{{4}}-\d{{2}}-\d{{2}}T\d{{2}}:\d{{2}}:\d{{2}}(?:\.\d+)?(?:Z|[+-]\d{{2}}:\d{{2}}))"
    rf"|(?P<id>{UUID_RE.pattern})(?P<task>/task-\d+)?"
    r"|(?P<file>file://[^\s\"'`<>\]\[()]+)"
    r"|(?P<path>(?<![\w/:])(?:~/|/(?!/))[^\s\"'`<>\]\[()]+)"
    rf"|(?P<ip>{IPV4_RE.pattern})"
    r"|(?P<marker></?(?:SYSTEM_MESSAGE|USER_REQUEST|ADDITIONAL_METADATA|USER_SETTINGS_CHANGE)>"
    r"|\b(?:timestamp|sender|priority|content|taskId|message)(?==)|\bMESSAGE_PRIORITY_(?:LOW|MEDIUM|HIGH)\b"
    r"|\b(?:task-\d+|\d+[hms]|\d+m\d+(?:\.\d+)?s)\b|(?m:^A\d+:)|\b(?:OK|RUNNING|DONE|ERROR|INVALID)\b"
    r"|(?<=Command exited with code )-?\d+|(?<=sender=)system\b|(?<=Status: )(?:COMPLETED|FAILED|CANCELED|CANCELLED)\b"
    r"|" + "|".join(map(re.escape, AGY_PHRASES)) + r")"
)


def names_a_host(v: str) -> bool:
    """A dotted name together with ':' or '/' (git.example.invalid:team/repo), or a URL scheme, is data, not an enum."""
    return "://" in v or (DOTTED_RE.search(v) is not None and (":" in v or "/" in v))


class Redactor:
    def __init__(self):
        self.salt = secrets.token_hex(8)
        self.words: dict[str, str] = {}
        self.ids: dict[str, str] = {}
        self.paths: dict[str, str] = {}
        self.kept: dict[str, set[str]] = {}  # why (word, key, enum, ...) -> strings kept as is, for --words
        self.shift: dt.timedelta | None = None
        self.rng = random.Random(self.salt)

    # -- leaves --------------------------------------------------------------------------------------------------
    def keep(self, s: str, why: str) -> str:
        """Return s unchanged and list it under why for --words, the review step before committing."""
        self.kept.setdefault(why, set()).add(s)
        return s

    def kept_report(self) -> str:
        lines = ["kept as is: " + " ".join(sorted(self.kept.get("word", ())))]  # the vocabulary line, as before
        lines += [f"kept as is ({why}): " + " ".join(sorted(ws))
                  for why, ws in sorted(self.kept.items()) if why != "word"]
        return "\n".join(lines)

    def pseudo(self, w: str) -> str:
        if w in VOCAB or (w.isdigit() and len(w) == 1):
            return self.keep(w, "word")
        if w not in self.words:
            if w.isascii():
                alpha = "abcdefghijklmnopqrstuvwxyz"
                out = "".join(
                    self.rng.choice("0123456789") if ch.isdigit()
                    else (self.rng.choice(alpha).upper() if ch.isupper() else self.rng.choice(alpha)) if ch.isalpha()
                    else ch
                    for ch in w
                )
            else:
                out = "".join(self.rng.choice("甲乙丙丁戊己庚辛壬癸") if not ch.isascii() else ch for ch in w)
            self.words[w] = out
        return self.words[w]

    def new_id(self, old: str) -> str:
        if old not in self.ids:
            if m := EXEC_ID_RE.fullmatch(old):
                self.ids[old] = "exec-" + self.new_id(m.group(1))
            elif UUID_RE.fullmatch(old):
                h = "".join(self.rng.choice("0123456789abcdef") for _ in range(32))
                self.ids[old] = f"{h[:8]}-{h[8:12]}-{h[12:16]}-{h[16:20]}-{h[20:]}"
            else:
                m = PREFIXED_ID_RE.match(old)  # only the known prefix: a greedy [a-z]+ kept "requirements" whole
                prefix = m.group(1) if m else ""
                body = "".join(self.rng.choice("ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz0123456789")
                               for _ in range(len(old) - len(prefix)))
                self.ids[old] = prefix + body
        return self.ids[old]

    def path(self, p: str) -> str:
        if p not in self.paths:
            ext = re.search(r"\.[A-Za-z0-9]{1,6}$", p)
            self.paths[p] = f"/work/project/f{len(self.paths) + 1}{self.keep(ext.group(0), 'ext') if ext else ''}"
        return self.paths[p]

    def ip(self, addr: str) -> str:
        if addr not in self.ids:
            self.ids[addr] = f"192.0.2.{len([v for v in self.ids.values() if v.startswith('192.0.2.')]) + 1}"
        return self.ids[addr]

    def ts(self, s: str) -> str:
        # Go writes 0–9 fraction digits; fromisoformat takes only 3 or 6 before Python 3.11.
        s = re.sub(r"\.(\d+)", lambda m: "." + m.group(1)[:6].ljust(6, "0"), s)
        t = dt.datetime.fromisoformat(s.replace("Z", "+00:00")).astimezone(dt.timezone.utc)
        if self.shift is None:
            self.shift = dt.datetime(2026, 1, 1, tzinfo=dt.timezone.utc) - t.replace(microsecond=0)
        return (t + self.shift).isoformat(timespec="milliseconds").replace("+00:00", "Z")

    def text(self, s: str) -> str:
        if len(s) > MAX_STR:
            s = s[:MAX_STR]
        s = TS_RE.sub(lambda m: self.ts(m.group(0)), s)
        s = IPV4_RE.sub(lambda m: self.ip(m.group(0)), s)  # before words: octets are short numbers
        ids = rf"{EXEC_ID_RE.pattern}|{UUID_RE.pattern}|{PREFIXED_ID_RE.pattern}"
        s = re.sub(ids, lambda m: self.new_id(m.group(0)), s)
        out, pos = [], 0
        for m in TAG_RE.finditer(s):  # tag and attribute names stay (their attribute values do not)
            out.append(self.words_in(s[pos:m.start()]))
            tag = m.group(0)
            self.keep(m.group(1), "tag")
            if m.group(2):
                tag = re.sub(r'([A-Za-z_-]+)="([^"]*)"',
                             lambda a: self.keep(a.group(1), "tag") + '="' + self.words_in(a.group(2)) + '"', tag)
            out.append(tag)
            pos = m.end()
        out.append(self.words_in(s[pos:]))
        return "".join(out)

    def words_in(self, s: str) -> str:
        # Keep remapped ids (already random) intact; everything else word by word.
        parts, pos = [], 0
        for m in re.finditer("|".join(re.escape(v) for v in self.ids.values()) or r"(?!x)x", s):
            parts.append(WORD_RE.sub(lambda w: self.pseudo(w.group(0)), s[pos:m.start()]))
            parts.append(m.group(0))
            pos = m.end()
        parts.append(WORD_RE.sub(lambda w: self.pseudo(w.group(0)), s[pos:]))
        return "".join(parts)

    def agy_path(self, path: str) -> str:
        if path.startswith("file://"):
            return "file://" + self.path(unquote(path[len("file://"):]))
        return self.path(path)

    def agy_text(self, s: str) -> str:
        out, pos = [], 0
        for m in AGY_TEXT_RE.finditer(s):
            out.append(WORD_RE.sub(lambda w: self.pseudo(w.group(0)), s[pos:m.start()]))
            if m.group("stamp"):
                try:
                    replacement = self.ts(m[0])
                except ValueError:  # a timestamp-shaped string in tool output need not be a date
                    replacement = WORD_RE.sub(lambda w: self.pseudo(w.group(0)), m[0])
            elif m.group("id"):
                replacement = self.new_id(m.group("id")) + (m.group("task") or "")
            elif m.group("file") or m.group("path"):
                replacement = self.agy_path(m[0])
            elif m.group("ip"):
                replacement = self.ip(m[0])
            else:
                replacement = self.keep(m[0], "agy marker")
            out.append(replacement)
            pos = m.end()
        out.append(WORD_RE.sub(lambda w: self.pseudo(w.group(0)), s[pos:]))
        return "".join(out)

    def agy_tool(self, name: str) -> str:
        if m := AGY_PERSONA_RE.fullmatch(name):
            # The server suffix identifies an installation, not a tool.
            key = m[1]
            if key not in self.ids:
                self.ids[key] = "".join(ch if ch == "_" else self.rng.choice("0123456789abcdef") for ch in key)
            return self.keep("mcp_persona_", "tool") + self.ids[key] + self.keep(m[2], "tool")
        return self.keep(name, "tool")

    # -- tree ----------------------------------------------------------------------------------------------------
    def value(self, key: str, v, parent: dict | None = None, *, agy: bool = False):
        if isinstance(v, dict):
            return self.obj(v, agy=agy)
        if isinstance(v, list):
            return [self.value(key, x, agy=agy) for x in v]
        if not isinstance(v, str):
            return v
        if key in DROP_KEYS:
            return ""
        if key == "data" and parent is not None and parent.get("type") == "base64":
            return PNG_1x1
        if v.startswith("data:image/"):
            return "data:image/png;base64," + PNG_1x1
        if agy:
            if key in PATH_KEYS or key in {"Path", "uri", "file_uri"}:
                return self.agy_path(v) if v else v
            if key == "model":
                return self.keep(v, "model") if AGY_MODEL_RE.fullmatch(v) else "model-placeholder"
            if ((key == "Action" and v in {"list", "status", "kill"})
                    or (key == "TimerCondition" and v in {"any", "never"})
                    or (key == "CronExpression" and re.fullmatch(r"[\d ,*/?\-]+|@(?:hourly|daily|weekly|monthly|yearly)", v))):
                return self.keep(v, "enum")
            if key == "name" and parent is not None:
                if "args" in parent and re.fullmatch(r"[a-z][a-z0-9_]{0,95}", v):
                    return self.agy_tool(v)
                if ("arguments" in parent or v in AGY_TOOLS) and re.fullmatch(r"[a-z][a-z0-9_]{0,63}", v):
                    return self.keep(v, "tool")
        if key == "timestamp" and TS_RE.fullmatch(v):
            return self.ts(v)
        if EXEC_ID_RE.fullmatch(v) or UUID_RE.fullmatch(v) or PREFIXED_ID_RE.fullmatch(v):
            return self.new_id(v)
        if key in ENUM_KEYS and ENUM_RE.fullmatch(v) and not names_a_host(v):
            return self.keep(v, "enum")
        if key in VERSION_KEYS and re.fullmatch(r"[0-9][0-9a-z.+-]{0,30}", v):
            return self.keep(v, "version")
        if key == "model" and MODEL_RE.fullmatch(v):  # other names than claude may be internal code names
            return self.keep(v, "model") if v.startswith("claude") else "gpt-5-codex"
        if key in PATH_KEYS and v.startswith(("/", "~", "file://")):
            return self.path(v)
        if key == "gitBranch":
            return "main"
        if key in ("name", "tool_name") and (v in VOCAB or re.fullmatch(r"mcp__[A-Za-z0-9_-]+", v)):
            if v.startswith("mcp__"):
                head, *rest = v.split("__")
                return "__".join([self.keep(head, "tool")] + [self.pseudo(p) for p in rest])
            return self.keep(v, "word")
        if key == "arguments" and v[:1] in "{[":  # Codex: tool arguments as a JSON string
            try:
                return json.dumps(self.value(key, json.loads(v), agy=agy), ensure_ascii=False)
            except ValueError:
                pass
        return self.agy_text(v) if agy else self.text(v)

    def obj(self, o: dict, *, agy: bool = False) -> dict:
        agy = agy or ("step_index" in o and isinstance(o.get("type"), str) and o["type"] in AGY_TYPES)
        out = {}
        for k, v in o.items():
            if EXEC_ID_RE.fullmatch(k) or UUID_RE.fullmatch(k) or PREFIXED_ID_RE.fullmatch(k):
                nk = self.new_id(k)
            elif re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]{0,40}", k):
                nk = self.keep(k, "key")
            else:
                nk = self.agy_text(k) if agy else self.path(k) if k.startswith("/") else self.text(k)
            out[nk] = self.value(k, v, o, agy=agy)
        return out


def parse_spec(spec: str):
    path, _, ranges = spec.rpartition(":")
    if not path or not re.fullmatch(r"[0-9,\-]+", ranges):
        return spec, None
    want = []
    for r in ranges.split(","):
        a, _, b = r.partition("-")
        want.append(range(int(a), int(b or a) + 1))
    want.sort(key=lambda interval: interval.start)
    return path, want


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("-o", "--out", required=True)
    ap.add_argument("--words", action="store_true", help="print the words kept as is, for review")
    ap.add_argument("--skip-type", action="append", default=["api-request", "api-request-blob", "api-request-shape"],
                    help="drop lines of this type (default: the big api-request blobs)")
    ap.add_argument("src", nargs="+", help="file.jsonl[:ranges] (1-based line numbers)")
    args = ap.parse_args()
    sources = [parse_spec(spec) for spec in args.src]
    if os.path.exists(args.out) and any(os.path.samefile(args.out, path) for path, _ in sources):
        ap.error("output must not refer to an input transcript")

    r = Redactor()
    n = 0
    with open(args.out, "w") as out:
        for path, want in sources:
            ranges = iter(want or ())
            active = next(ranges, None)
            with open(path, errors="replace") as f:
                for i, line in enumerate(f, 1):
                    if want is not None:
                        while active is not None and i >= active.stop:
                            active = next(ranges, None)
                        if active is None:
                            break
                        if i < active.start:
                            continue
                    try:
                        e = json.loads(line)
                    except ValueError:
                        continue
                    if not isinstance(e, dict) or e.get("type") in args.skip_type:
                        continue
                    out.write(json.dumps(r.obj(e), ensure_ascii=False, separators=(",", ":")) + "\n")
                    n += 1
    print(f"wrote {n} lines to {args.out}", file=sys.stderr)
    if args.words:
        print(r.kept_report(), file=sys.stderr)


if __name__ == "__main__":
    main()

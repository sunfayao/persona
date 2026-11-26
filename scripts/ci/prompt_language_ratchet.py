"""Keep CJK text out of English prompt templates (docs/assistant/prompts.md)."""
from pathlib import Path
import re
import sys


SOURCE = Path(__file__).resolve().parents[2] / "server/internal/prompts"
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


def main():
    findings = scan(SOURCE)
    for names in findings.values():
        for name in names:
            print(f"prompt language: {name} contains CJK text", file=sys.stderr)
    if findings:
        return 1
    print("prompt language: clean")
    return 0


if __name__ == "__main__":
    sys.exit(main())

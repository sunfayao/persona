"""redact_transcript.py runs on macOS's own Python 3.9 (#579): its datetime.fromisoformat takes only 3 or 6 fraction
digits, while Go writes RFC 3339 times with 0 to 9 (trailing zeros dropped)."""
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("redact_transcript", Path(__file__).with_name("redact_transcript.py"))
redact = importlib.util.module_from_spec(spec)
spec.loader.exec_module(redact)


class TimestampTests(unittest.TestCase):
    def test_any_number_of_fraction_digits(self):
        r = redact.Redactor()
        self.assertEqual(r.ts("2026-03-01T10:00:01Z"), "2026-01-01T00:00:00.000Z")  # the first time moves to 2026-01-01
        for stamp, shifted in [("2026-03-01T10:00:01.5Z", "2026-01-01T00:00:00.500Z"),
                               ("2026-03-01T10:00:01.12Z", "2026-01-01T00:00:00.120Z"),
                               ("2026-03-01T10:00:01.1234Z", "2026-01-01T00:00:00.123Z"),
                               ("2026-03-01T10:00:01.123456789Z", "2026-01-01T00:00:00.123Z")]:
            with self.subTest(stamp=stamp):
                self.assertEqual(r.ts(stamp), shifted)


class KeptAsIsTests(unittest.TestCase):
    """`--words` is the review step before a fixture is committed, so it lists every string the output keeps as is."""

    def test_every_verbatim_string_is_listed_with_its_reason(self):
        r = redact.Redactor()
        out = r.obj({"type": "user", "version": "2.1.0", "model": "claude-placeholder", "name": "mcp__srv__tool",
                     "filePath": "/home/someone/notes.cfgx",
                     "message": {"content": '<example_note kind="x">hello</example_note>'},
                     "toolUseResult": {"example_key": {"ok": True}}})
        self.assertEqual(out["toolUseResult"], {"example_key": {"ok": True}})
        for why, kept in [("key", "example_key"), ("key", "toolUseResult"), ("enum", "user"), ("version", "2.1.0"),
                          ("model", "claude-placeholder"), ("tool", "mcp"), ("tag", "example_note"), ("tag", "kind"),
                          ("ext", ".cfgx")]:
            with self.subTest(kept=kept):
                self.assertIn(kept, r.kept.get(why, set()))
        report = r.kept_report()
        self.assertTrue(report.startswith("kept as is: "), report)
        self.assertIn("kept as is (key): ", report)
        self.assertIn("example_note", report)

    def test_an_enum_value_naming_a_host_or_url_is_redacted(self):
        r = redact.Redactor()
        for key, value in [("source", "git.example.invalid:team/repo"), ("channel", "https://example.invalid/x"),
                           ("action", "host.example.invalid/path")]:
            with self.subTest(value=value):
                out = r.value(key, value)
                self.assertNotEqual(out, value)
                self.assertNotIn("invalid", out)
                self.assertNotIn(value, r.kept.get("enum", set()))
        for key, value in [("media_type", "image/png"), ("type", "user"), ("type", "response.completed")]:
            with self.subTest(value=value):
                self.assertEqual(r.value(key, value), value)
                self.assertIn(value, r.kept["enum"])

    def test_an_id_prefix_word_followed_by_letters_is_not_kept(self):
        # Ids keep only their known prefix (msg_, toolu_, ws, ...): a word that merely starts with one gets a new body.
        r = redact.Redactor()
        self.assertEqual(r.new_id("toolu_01AbCdEfGh")[:6], "toolu_")
        for word in ["requirements", "wsplaceholder"]:
            with self.subTest(word=word):
                self.assertNotIn(word, r.text(f"see {word} here"))


class AntigravityTests(unittest.TestCase):
    def test_native_arguments_keep_their_types_and_tool_names(self):
        r = redact.Redactor()
        calls = [
            {"name": "run_command", "args": {"CommandLine": "cat /sample/private.txt", "WaitMsBeforeAsync": 20,
                                             "toolSummary": "Private summary", "IsDaemon": False,
                                             "NotificationTimeoutSeconds": 5}},
            {"name": "read_file", "args": {"Path": "/sample/private.txt", "StartLine": 4, "EndLine": 7}},
            {"name": "write_file", "args": {"Path": "relative.txt", "Content": "q" * 6500, "Overwrite": True}},
            {"name": "edit_file", "args": {"Path": "file:///sample/private.txt", "Target": "secret", "Replacement": "value"}},
            {"name": "update_steps", "args": {"steps": [{"title": "Secret step", "status": "in_progress"}]}},
            {"name": "manage_task", "args": {"Action": "kill"}},
            {"name": "schedule", "args": {"Prompt": "Do something", "CronExpression": "1 */2 * * *", "MaxIterations": 3}},
            {"name": "search_web", "args": {"query": "private", "domain": "private.example.invalid"}},
            {"name": "ask_question", "args": {"questions": [{"question": "What?", "options": ["One", "Two"],
                                                            "is_multi_select": False}]}},
            {"name": "mcp_persona_deadbeef_agent_read", "args": {}},
            {"name": "mcp_persona_deadbeef_persona_call", "args": {"name": "agent_read", "arguments": {"limit": 4}}},
        ]
        out = r.obj({"type": "PLANNER_RESPONSE", "source": "MODEL", "step_index": 18, "status": "DONE",
                     "model": "gemini-2.5-pro", "tool_calls": calls})
        self.assertEqual((out["type"], out["source"], out["status"], out["step_index"], out["model"]),
                         ("PLANNER_RESPONSE", "MODEL", "DONE", 18, "gemini-2.5-pro"))
        actual = out["tool_calls"]
        self.assertEqual([c["name"] for c in actual[:9]], [c["name"] for c in calls[:9]])
        self.assertEqual([c["args"].keys() for c in actual], [c["args"].keys() for c in calls])
        command, read, write, edit, plan, manage, schedule, search, ask, _, wrapper = [c["args"] for c in actual]
        self.assertEqual(command["CommandLine"], "cat /work/project/f1.txt")
        self.assertEqual(command["WaitMsBeforeAsync"], 20)
        self.assertEqual(command["NotificationTimeoutSeconds"], 5)
        self.assertIs(command["IsDaemon"], False)
        self.assertEqual(read, {"Path": "/work/project/f1.txt", "StartLine": 4, "EndLine": 7})
        self.assertEqual(edit["Path"], "file:///work/project/f1.txt")
        self.assertIs(write["Overwrite"], True)
        self.assertEqual(len(write["Content"]), 6500)
        self.assertEqual(plan["steps"][0]["status"], "in_progress")
        self.assertEqual(manage["Action"], "kill")
        self.assertEqual(schedule["CronExpression"], "1 */2 * * *")
        self.assertNotIn("private", search["domain"])
        self.assertIs(ask["questions"][0]["is_multi_select"], False)
        self.assertEqual(wrapper, {"name": "agent_read", "arguments": {"limit": 4}})
        self.assertRegex(actual[9]["name"], r"^mcp_persona_[0-9a-f]{8}_agent_read$")
        self.assertEqual(actual[10]["name"], actual[9]["name"].removesuffix("agent_read") + "persona_call")
        self.assertNotIn("deadbeef", json.dumps(actual))
        self.assertIn("CommandLine", r.kept["key"])
        self.assertIn("agent_read", r.kept["tool"])

    def test_step_text_keeps_markers_and_joins_the_remapped_task_handle(self):
        r = redact.Redactor()
        task = "01234567-89ab-cdef-0123-456789abcdef/task-71"
        running = r.obj({"type": "GENERIC", "step_index": 71, "source": "MODEL", "status": "RUNNING",
                         "created_at": "2026-03-03T02:05:02Z", "content":
                         f"Created At: 2026-03-03T02:05:02Z\nTool is running as a background task with task id: {task}"
                         "\nTask Description: private summary\nTask logs are available at: file:///sample/private.log"})
        handle = running["content"].splitlines()[1].split(": ", 1)[1]
        self.assertNotEqual(handle, task)
        self.assertRegex(handle, r"^[a-f0-9-]{36}/task-71$")
        self.assertEqual(running["created_at"], "2026-01-01T00:00:00.000Z")
        self.assertTrue(running["content"].startswith("Created At: 2026-01-01T00:00:00.000Z\n"))
        self.assertIn("Task Description:", running["content"])
        self.assertIn("Task logs are available at: file:///work/project/f1.log", running["content"])
        end = r.obj({"type": "SYSTEM_MESSAGE", "step_index": 72, "source": "SYSTEM", "status": "DONE", "content":
                     f'<SYSTEM_MESSAGE>\n[Message] timestamp=2026-03-03T10:05:04+08:00 sender={task} '
                     f'priority=MESSAGE_PRIORITY_HIGH content=Task id "{task}" finished with result:\n'
                     "Completed At: 2026-03-03T02:05:04Z\nCommand exited with code 123.\n</SYSTEM_MESSAGE>"})
        self.assertIn(f"[Message] timestamp=2026-01-01T00:00:02.000Z sender={handle}", end["content"])
        self.assertIn(f'content=Task id "{handle}" finished with result:', end["content"])
        self.assertIn("Completed At: 2026-01-01T00:00:02.000Z\nCommand exited with code 123.", end["content"])
        for typ, marker in [("USER_INPUT", "<USER_REQUEST>"), ("USER_INPUT", "<ADDITIONAL_METADATA>"),
                            ("USER_INPUT", "<USER_SETTINGS_CHANGE>"), ("CHECKPOINT", "# Resuming from a compaction"),
                            ("GENERIC", "A1:")]:
            with self.subTest(marker=marker):
                self.assertEqual(r.obj({"type": typ, "step_index": 1, "content": marker})["content"], marker)
        for model, expected in [("gpt-oss-120b", "gpt-oss-120b"), ("private.name", "model-placeholder")]:
            self.assertEqual(r.obj({"type": "PLANNER_RESPONSE", "step_index": 1, "model": model})["model"], expected)
        self.assertEqual(r.obj({"type": "GENERIC", "step_index": 1, "status": "INVALID"})["status"], "INVALID")
        self.assertEqual(r.obj({"type": "GENERIC", "step_index": 1, "status": "ERROR"})["status"], "ERROR")

    def test_nested_codex_exec_id_keeps_its_prefix_and_joins_the_uuid(self):
        r = redact.Redactor()
        original = "01234567-89ab-cdef-0123-456789abcdef"
        remapped = "exec-" + r.new_id(original)
        self.assertEqual(r.obj({"id": "exec-" + original})["id"], remapped)
        self.assertEqual(r.text(f"result {original} exec-{original}"), f"result {remapped[5:]} {remapped}")


class RangeTests(unittest.TestCase):
    def test_parse_keeps_sorted_intervals(self):
        path, ranges = redact.parse_spec("fixture.jsonl:8000,1-5000,2-7")
        self.assertEqual(path, "fixture.jsonl")
        self.assertEqual(ranges, [range(1, 5001), range(2, 8), range(8000, 8001)])

    def test_range_selection_preserves_source_order_without_duplicates(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source, output = root / "source.jsonl", root / "output.jsonl"
            source.write_text("".join(json.dumps({"line": i}) + "\n" for i in range(1, 12)))
            selection = str(source) + ":7,4-5,2-4,3,9-6,10-200"
            run = subprocess.run([sys.executable, "-B", redact.__file__, "-o", str(output), selection],
                                 capture_output=True, text=True, timeout=10)
            self.assertEqual(run.returncode, 0, run.stderr)
            self.assertEqual([json.loads(line)["line"] for line in output.read_text().splitlines()],
                             [2, 3, 4, 5, 7, 10, 11])


class OutputTests(unittest.TestCase):
    def test_skips_non_record_lines_and_keeps_later_valid_entries(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source, output = root / "source.jsonl", root / "output.jsonl"
            rows = ['{"type":"user","line":1}', "null", "[]", "42", '"text"', "{",
                    '{"type":"api-request","line":7}', '{"type":"assistant","line":8}']
            original = "\n".join(rows) + "\n"
            source.write_text(original)
            run = subprocess.run([sys.executable, "-B", redact.__file__, "-o", str(output), str(source)],
                                 capture_output=True, text=True, timeout=10)
            self.assertEqual(run.returncode, 0, run.stderr)
            self.assertEqual([json.loads(line) for line in output.read_text().splitlines()],
                             [{"type": "user", "line": 1}, {"type": "assistant", "line": 8}])
            self.assertEqual(source.read_text(), original)

    def test_refuses_output_alias_of_an_input(self):
        transcript = b'{"type":"user","signature":"synthetic fixture"}\n'
        for alias in ("direct", "symlink", "hardlink"):
            with self.subTest(alias=alias), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                first, source = root / "first.jsonl", root / "source.jsonl"
                first.write_bytes(transcript)
                source.write_bytes(transcript)
                output = root / "output.jsonl"
                if alias == "direct":
                    output = source
                elif alias == "symlink":
                    output.symlink_to(source.name)
                else:
                    os.link(source, output)
                run = subprocess.run([sys.executable, "-B", redact.__file__, "-o", str(output),
                                      str(first), str(source) + ":1"], capture_output=True, text=True, timeout=10)
                self.assertEqual(source.read_bytes(), transcript)
                self.assertEqual(first.read_bytes(), transcript)
                self.assertTrue(output.samefile(source))
                self.assertEqual(run.returncode, 2, run.stderr)
                self.assertIn("output must not refer to an input transcript", run.stderr)

    def test_overwrites_existing_separate_output(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source, output = root / "source.jsonl", root / "output.jsonl"
            transcript = '{"type":"user","signature":"synthetic fixture"}\n'
            source.write_text(transcript)
            output.write_text("old output")
            run = subprocess.run([sys.executable, "-B", redact.__file__, "-o", str(output), str(source)],
                                 capture_output=True, text=True, timeout=10)
            self.assertEqual(run.returncode, 0, run.stderr)
            self.assertEqual(json.loads(output.read_text()), {"type": "user", "signature": ""})
            self.assertEqual(source.read_text(), transcript)


if __name__ == "__main__":
    unittest.main()

"""go_test_partitioned runs internal/node, internal/assistant and the other
partitioned packages in go_partitions' partitions (#5594, #7644, #12765)."""
import io
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

import go_partitions
import go_test_partitioned as gtp

MODULE = ["example/server/cmd/personad", "example/server/internal/assistant", "example/server/internal/assistantwire",
          "example/server/internal/node", "example/server/internal/node/sub"]
NODE, ASSISTANT = "./internal/node", "./internal/assistant"
# The fixture module has no internal/hub, internal/devices, internal/extmgr or internal/voice: they split the way
# internal/assistant does.
PATTERNS = {NODE: ["^(TestA|TestC)$", "^(TestB)$"], ASSISTANT: ["^(TestX|TestZ)$", "^(TestY)$"],
            "./internal/hub": ["^(TestH)$"], "./internal/devices": ["^(TestD)$"], "./internal/extmgr": ["^(TestM)$"],
            "./internal/voice": ["^(TestV)$"]}
PREFIX = "example/server/"
# MODULE with subpackages of internal/assistant, for the ./<dir>/... arguments check-fast passes (#12765).
TREE = MODULE + [PREFIX + "internal/assistant/inbox", PREFIX + "internal/assistant/inbox/item"]
ASSISTANT_TREE = "./internal/assistant/..."


def lister(module):
    """go list over a fixture module: ./... is all of it, ./<dir>/... the packages at and under <dir>."""
    def go_list(patterns):
        found = []
        for pattern in patterns:
            directory = "" if pattern == "./..." else PREFIX + pattern[len("./"):-len("/...")]
            found += [p for p in module if not directory or p == directory or p.startswith(directory + "/")]
        return list(dict.fromkeys(found))
    return go_list


def plan(argv, module=MODULE):
    return gtp.plan(argv, lister(module), lambda package: list(PATTERNS[package]))


def partitions(flags, package, trailing=()):
    return [["go", "test", *flags, "-run", pattern, package, *trailing] for pattern in PATTERNS[package]]


class PlanTests(unittest.TestCase):
    def test_the_whole_module_runs_the_partitioned_packages_in_partitions_beside_the_rest(self):
        flags = ["-count=1", "-p", "1"]
        self.assertEqual(plan(["go", "test", *flags, "./..."]), [
            ["go", "test", *flags, "example/server/cmd/personad", "example/server/internal/assistantwire",
             "example/server/internal/node/sub"],
            *partitions(flags, ASSISTANT), *partitions(flags, NODE),
        ])
        self.assertEqual(list(PATTERNS), list(go_partitions.PACKAGES))  # the fixture covers every partitioned package

    def test_every_command_keeps_the_callers_flags(self):
        # A deploy's skip list (scripts/deploy): every partition leaves the same tests out.
        commands = plan(["go", "test", "-count=1", "-p", "1", "-skip", "TestX|TestY", "-v", "./..."])
        for command in commands:
            self.assertEqual(command[2:8], ["-count=1", "-p", "1", "-skip", "TestX|TestY", "-v"])

    def test_a_partitioned_package_named_alone_runs_only_its_partitions(self):
        for package, named in ((NODE, "./internal/node"), (NODE, "./internal/node/"), (ASSISTANT, "./internal/assistant/")):
            self.assertEqual(plan(["go", "test", "-count=1", named]), partitions(["-count=1"], package))

    def test_partitioned_packages_beside_other_packages(self):
        self.assertEqual(plan(["go", "test", "./internal/kb/", "./internal/node/", "./internal/assistant"]),
                         [["go", "test", "./internal/kb/"], *partitions([], NODE), *partitions([], ASSISTANT)])

    def test_run_or_other_packages_or_another_command_run_as_given(self):
        for argv in (["go", "test", "-run", "TestX", "./..."], ["go", "test", "-run=TestX", "./internal/node/"],
                     ["go", "test", "-run", "TestX", "./internal/assistant"], ["go", "test", "-run", "TestX", ASSISTANT_TREE],
                     ["go", "test", "-count=1", "./internal/kb/"],
                     ["go", "test", "./internal/node/sub"], ["go", "test", "./internal/assistantwire"],
                     ["go", "vet", "./..."]):
            self.assertEqual(plan(argv), [argv], argv)

    def test_flag_values_are_not_packages(self):
        flags, packages = gtp.split(["-coverprofile", "/tmp/cover.out", "-skip", "A|B", "-timeout=20m", "./..."])
        self.assertEqual((flags, packages), (["-coverprofile", "/tmp/cover.out", "-skip", "A|B", "-timeout=20m"],
                                             ["./..."]))

    def test_coverprofile_runs_directly_before_package_discovery(self):
        for alias in ("-coverprofile", "--coverprofile", "-test.coverprofile", "--test.coverprofile"):
            for form in ((alias, "/tmp/c"), (alias + "=/tmp/c",)):
                for package in ("./...", NODE, ASSISTANT_TREE):
                    with self.subTest(form=form, package=package):
                        argv = ["go", "test", *form, package]
                        list_packages = mock.Mock(return_value=list(MODULE))
                        list_patterns = mock.Mock(side_effect=lambda name: list(PATTERNS[name]))
                        self.assertEqual(gtp.plan(argv, list_packages, list_patterns), [argv])
                        list_packages.assert_not_called()
                        list_patterns.assert_not_called()

    def test_coverprofile_tokens_in_values_and_double_dash_args_are_not_coverage(self):
        for flag in ("-skip", "--skip", "-run", "--run", "-exec", "--exec", "-outputdir", "-test.list"):
            for value in ("-coverprofile", "--test.coverprofile"):
                with self.subTest(flag=flag, value=value):
                    self.assertFalse(gtp.has_coverage_profile([flag, value, "./..."]))
        for args in (["./...", "--", "-coverprofile"], [NODE, "--", "--test.coverprofile=/tmp/c"]):
            with self.subTest(args=args):
                self.assertFalse(gtp.has_coverage_profile(args))

    def test_args_test_coverprofile_is_coverage(self):
        for terminator in ("-args", "--args"):
            for form in (("-test.coverprofile", "/tmp/c"), ("-test.coverprofile=/tmp/c",)):
                with self.subTest(terminator=terminator, form=form):
                    self.assertTrue(gtp.has_coverage_profile([NODE, terminator, *form]))

    def test_skip_and_exec_values_that_look_like_coverprofile_keep_the_partition_plan(self):
        for flag in ("-skip", "--skip", "-exec", "--exec", "-outputdir", "-test.list"):
            for value in ("-coverprofile", "--test.coverprofile"):
                flags = [flag, value]
                with self.subTest(flags=flags):
                    self.assertEqual(plan(["go", "test", *flags, NODE]), partitions(flags, NODE))

    def test_run_value_that_looks_like_coverprofile_still_narrows_without_coverage(self):
        argv = ["go", "test", "-run", "-coverprofile", "./..."]
        self.assertFalse(gtp.has_coverage_profile(argv[2:]))
        self.assertEqual(plan(argv), [argv])

    def test_double_dash_args_stay_after_generated_partition_flags_and_packages(self):
        trailing = ["--", "-coverprofile"]
        self.assertEqual(plan(["go", "test", NODE, *trailing]), partitions([], NODE, trailing))
        self.assertEqual(plan(["go", "test", "./...", *trailing]), [
            ["go", "test", "example/server/cmd/personad", "example/server/internal/assistantwire",
             "example/server/internal/node/sub", *trailing],
            *partitions([], ASSISTANT, trailing), *partitions([], NODE, trailing),
        ])

    def test_args_passthrough_runs_directly_before_package_discovery(self):
        trailing_forms = ((),
                          ("-test.coverprofile", "/tmp/c"),
                          ("-test.coverprofile=/tmp/c",),
                          ("-test.run", "TestAlpha"),
                          ("-test.run=TestAlpha",),
                          ("-test.v",))
        for terminator in ("-args", "--args"):
            for trailing in trailing_forms:
                for package in ("./...", NODE, ASSISTANT_TREE):
                    with self.subTest(terminator=terminator, trailing=trailing, package=package):
                        argv = ["go", "test", package, terminator, *trailing]
                        list_packages = mock.Mock(return_value=list(MODULE))
                        list_patterns = mock.Mock(side_effect=lambda name: list(PATTERNS[name]))
                        self.assertEqual(gtp.plan(argv, list_packages, list_patterns), [argv])
                        list_packages.assert_not_called()
                        list_patterns.assert_not_called()

    def test_empty_coverprofile_values_keep_the_partition_plan(self):
        for alias in ("-coverprofile", "--coverprofile", "-test.coverprofile", "--test.coverprofile"):
            for flags in ([alias + "="], [alias, ""]):
                with self.subTest(flags=flags):
                    self.assertEqual(plan(["go", "test", *flags, NODE]), partitions(flags, NODE))

    def test_last_coverprofile_value_wins_across_aliases(self):
        for empty, nonempty in (("-coverprofile=", "--test.coverprofile=/tmp/c"),
                                ("--test.coverprofile=", "-coverprofile=/tmp/c")):
            with self.subTest(empty=empty, nonempty=nonempty):
                self.assertEqual(plan(["go", "test", empty, NODE]), partitions([empty], NODE))
                argv = ["go", "test", nonempty, empty, NODE]
                self.assertEqual(plan(argv), partitions([nonempty, empty], NODE))
                argv = ["go", "test", empty, nonempty, NODE]
                self.assertEqual(plan(argv), [argv])

    def test_goflags_coverprofile_runs_directly_before_package_discovery(self):
        for alias in ("-coverprofile", "--coverprofile", "-test.coverprofile", "--test.coverprofile"):
            for goflags in (alias + "=/tmp/c", "'" + alias + "=/tmp/a b/c.out'",
                            '"' + alias + '=/tmp/a b/c.out"'):
                for package in ("./...", NODE, ASSISTANT_TREE):
                    with self.subTest(goflags=goflags, package=package):
                        argv = ["go", "test", package]
                        list_packages = mock.Mock(return_value=list(MODULE))
                        list_patterns = mock.Mock(side_effect=lambda name: list(PATTERNS[name]))
                        self.assertEqual(gtp.plan(argv, list_packages, list_patterns, goflags=goflags), [argv])
                        list_packages.assert_not_called()
                        list_patterns.assert_not_called()

    def test_goflags_empty_or_overridden_coverprofile_keeps_the_partition_plan(self):
        for alias in ("-coverprofile", "--coverprofile", "-test.coverprofile", "--test.coverprofile"):
            for goflags, flags in ((alias + "=", []),
                                   (alias + "=/tmp/from-env", [alias + "="]),
                                   (alias + "=/tmp/from-env", ["-coverprofile", ""])):
                with self.subTest(goflags=goflags, flags=flags):
                    argv = ["go", "test", *flags, NODE]
                    self.assertEqual(gtp.plan(argv, lister(MODULE), lambda package: list(PATTERNS[package]),
                                              goflags=goflags), partitions(flags, NODE))

    def test_goflags_explicit_nonempty_profile_override_runs_directly(self):
        argv = ["go", "test", "-test.coverprofile=/tmp/from-cli", NODE]
        list_packages = mock.Mock(return_value=list(MODULE))
        list_patterns = mock.Mock(return_value=list(PATTERNS[NODE]))
        self.assertEqual(gtp.plan(argv, list_packages, list_patterns, goflags="-coverprofile=/tmp/from-env"), [argv])
        list_packages.assert_not_called()
        list_patterns.assert_not_called()

    def test_goflags_values_and_ordinary_noncoverage_flags_do_not_false_positive(self):
        for goflags in ("-skip=-coverprofile", "-run=-test.coverprofile", "-exec=-coverprofile",
                        "-mod=readonly -count=1", "-ldflags=-w"):
            with self.subTest(goflags=goflags):
                argv = ["go", "test", NODE]
                commands = gtp.plan(argv, lister(MODULE), lambda package: list(PATTERNS[package]),
                                    goflags=goflags)
                if goflags.startswith("-run="):
                    self.assertEqual(commands, [argv])
                else:
                    self.assertEqual(commands, partitions([], NODE))

    def test_invalid_goflags_and_unknown_command_flags_run_directly(self):
        argv = ["go", "test", NODE]
        for goflags in ("-coverprofile /tmp/c", "'-coverprofile=/tmp/unterminated", "not-a-flag"):
            with self.subTest(goflags=goflags):
                list_packages = mock.Mock(return_value=list(MODULE))
                list_patterns = mock.Mock(return_value=list(PATTERNS[NODE]))
                self.assertEqual(gtp.plan(argv, list_packages, list_patterns, goflags=goflags), [argv])
                list_packages.assert_not_called()
                list_patterns.assert_not_called()
        for argv in (["go", "test", "-custom", "value", NODE], ["go", "test", NODE, "-custom=value"],
                     ["go", "test", "-coverprofile"]):
            with self.subTest(argv=argv):
                list_packages = mock.Mock(return_value=list(MODULE))
                list_patterns = mock.Mock(return_value=list(PATTERNS[NODE]))
                self.assertEqual(gtp.plan(argv, list_packages, list_patterns), [argv])
                list_packages.assert_not_called()
                list_patterns.assert_not_called()

    def test_covermode_without_coverprofile_keeps_the_partition_plan(self):
        flags = ["-covermode=atomic"]
        self.assertEqual(plan(["go", "test", *flags, "./..."]), [
            ["go", "test", *flags, "example/server/cmd/personad", "example/server/internal/assistantwire",
             "example/server/internal/node/sub"],
            *partitions(flags, ASSISTANT), *partitions(flags, NODE),
        ])

    def test_a_partition_is_described_by_its_package_and_size_not_its_names(self):
        self.assertEqual(gtp.describe(["go", "test", "-run", "^(TestA|TestB|TestC)$", NODE]),
                         "./internal/node partition (3 tests)")
        self.assertEqual(gtp.describe(["go", "test", "-run", "^(TestX)$", ASSISTANT]),
                         "./internal/assistant partition (1 tests)")
        self.assertEqual(gtp.describe(["go", "test", "-count=1", "./internal/hub", "./internal/kb"]), "2 package(s)")


class SubtreePlanTests(unittest.TestCase):
    """A ./<dir>/... argument that covers a partitioned package, as check-fast's Go checks pass it (#12765)."""

    def test_the_assistant_subtree_runs_the_assistant_in_partitions_beside_its_subpackages(self):
        flags = ["-count=1", "-timeout", "290s"]
        self.assertEqual(plan(["go", "test", *flags, ASSISTANT_TREE], TREE), [
            ["go", "test", *flags, PREFIX + "internal/assistant/inbox", PREFIX + "internal/assistant/inbox/item"],
            *partitions(flags, ASSISTANT),
        ])

    def test_a_subtree_with_no_other_package_runs_only_the_partitions(self):
        self.assertEqual(plan(["go", "test", ASSISTANT_TREE]), partitions([], ASSISTANT))

    def test_a_wider_subtree_leaves_a_package_that_splits_itself_to_its_own_split(self):
        # internal/node's TestMain splits a plain run of it into up to six processes; three -run partitions would be
        # slower on a machine with eight CPUs or more. It stays with the other packages, after them.
        self.assertEqual(go_partitions.SPLITS_ITSELF, {NODE})
        self.assertEqual(plan(["go", "test", "-count=1", "./internal/..."], TREE), [
            ["go", "test", "-count=1", PREFIX + "internal/assistantwire", PREFIX + "internal/node/sub",
             PREFIX + "internal/assistant/inbox", PREFIX + "internal/assistant/inbox/item", PREFIX + "internal/node"],
            *partitions(["-count=1"], ASSISTANT),
        ])

    def test_subtrees_beside_named_packages(self):
        self.assertEqual(plan(["go", "test", "./internal/kb/", ASSISTANT_TREE, "./internal/node"], TREE), [
            ["go", "test", PREFIX + "internal/assistant/inbox", PREFIX + "internal/assistant/inbox/item",
             "./internal/kb/"],
            *partitions([], ASSISTANT), *partitions([], NODE),
        ])
        # internal/node named on its own is partitioned as before, and does not run a second time from the subtree.
        self.assertEqual(plan(["go", "test", "./internal/...", "./internal/node/"], TREE), [
            ["go", "test", PREFIX + "internal/assistantwire", PREFIX + "internal/node/sub",
             PREFIX + "internal/assistant/inbox", PREFIX + "internal/assistant/inbox/item"],
            *partitions([], ASSISTANT), *partitions([], NODE),
        ])
        # Another subtree stays a pattern in the other packages' command; go test expands it.
        self.assertEqual(plan(["go", "test", "./internal/kb/...", ASSISTANT_TREE], TREE), [
            ["go", "test", PREFIX + "internal/assistant/inbox", PREFIX + "internal/assistant/inbox/item",
             "./internal/kb/..."],
            *partitions([], ASSISTANT),
        ])

    def test_a_subtree_beside_the_whole_module_does_not_run_twice(self):
        self.assertEqual(plan(["go", "test", "./...", ASSISTANT_TREE], TREE), plan(["go", "test", "./..."], TREE))

    def test_subtrees_that_cover_no_package_the_runner_splits_run_as_given_without_go_list(self):
        for package in ("./internal/kb/...", "./cmd/...", "./internal/assistantwire/...",
                        "./internal/assistant/inbox/...", "./internal/node/...", "./internal/node/sub/...",
                        "internal/assistant/...", "github.com/example/server/internal/assistant/...",
                        "./internal/.../assistant/...", "../server/internal/assistant/..."):
            with self.subTest(package=package):
                argv = ["go", "test", "-count=1", package]
                list_packages = mock.Mock(side_effect=lister(TREE))
                self.assertEqual(gtp.plan(argv, list_packages, lambda name: list(PATTERNS[name])), [argv])
                list_packages.assert_not_called()

    def test_a_subtree_where_go_list_finds_no_partitioned_package_runs_as_given(self):
        argv = ["go", "test", "./internal/..."]
        self.assertEqual(plan(argv, [PREFIX + "internal/kb", PREFIX + "internal/node"]), [argv])

    def test_explicit_packages_and_the_whole_module_list_what_they_did(self):
        # The plans above them are pinned by PlanTests; the go list each one makes is pinned here.
        for argv, listed in ((["go", "test", "./internal/kb/", "./internal/node/", "./internal/assistant"], None),
                             (["go", "test", "-count=1", "-p", "1", "./..."], ["./..."]),
                             (["go", "test", "./...", ASSISTANT_TREE], ["./..."]),
                             (["go", "test", ASSISTANT_TREE, "./internal/..."], [ASSISTANT_TREE, "./internal/..."])):
            with self.subTest(argv=argv):
                list_packages = mock.Mock(side_effect=lister(TREE))
                gtp.plan(argv, list_packages, lambda name: list(PATTERNS[name]))
                if listed is None:
                    list_packages.assert_not_called()
                else:
                    list_packages.assert_called_once_with(listed)

    def test_a_subtree_names_its_directory(self):
        for token, directory in (("./internal/assistant/...", "internal/assistant"), ("./internal/...", "internal"),
                                 ("./internal/assistant//...", "internal/assistant"), ("./...", None),
                                 ("./internal/assistant", None), ("internal/assistant/...", None),
                                 ("./internal/.../x/...", None), ("./../server/...", None)):
            with self.subTest(token=token):
                self.assertEqual(gtp.subtree(token), directory)


@unittest.skipIf(os.name == "nt", "the runner's exec and a shell go that kills itself need POSIX")
class ExecTests(unittest.TestCase):
    """A command the runner does not split replaces the runner (os.execvp), so check-fast's timeout and heavy slot wrap
    go itself: a go killed by SIGKILL (the OOM killer) still reads as killed, which check-fast's TIMEOUT rule for 137
    relies on. Run as a child instead, go's death would become python's exit status 247 and that rule a FAIL."""

    def test_a_command_run_as_given_is_the_callers_child_and_keeps_its_signal_status(self):
        temporary = tempfile.TemporaryDirectory(prefix="go-test-partitioned-exec-")
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        pid_file = root / "go.pid"
        go = root / "bin/go"
        go.parent.mkdir()
        go.write_text('#!/bin/sh\nprintf "%%s\\n" "$$" > "%s"\nkill -KILL "$$"\n' % pid_file)
        go.chmod(0o755)
        env = dict(os.environ, PATH="%s%s%s" % (go.parent, os.pathsep, os.environ.get("PATH", "")))
        env.pop("GOFLAGS", None)
        # ./internal/foo is no package the runner splits: plan() returns it as given without running go list.
        process = subprocess.Popen([sys.executable, "-I", str(Path(gtp.__file__).resolve()), "go", "test", "-count=1",
                                    "./internal/foo"], cwd=root, env=env, stdout=subprocess.PIPE,
                                   stderr=subprocess.STDOUT, text=True)
        out, _ = process.communicate(timeout=60)
        self.assertEqual(-signal.SIGKILL, process.returncode, out)
        self.assertEqual(str(process.pid), pid_file.read_text().strip(), out)  # go is the process the caller started


class RunTests(unittest.TestCase):
    def run_commands(self, commands):
        out = io.StringIO()
        with mock.patch.object(sys, "stdout", out):
            status = gtp.run(commands)
        return status, out.getvalue()

    def test_outputs_never_interleave_and_any_failure_fails(self):
        py = sys.executable
        status, text = self.run_commands([
            [py, "-c", "import time\nfor i in range(3): print('rest', i, flush=True); time.sleep(0.05)"],
            [py, "-c", "print('FAIL\\tpartition one'); raise SystemExit(1)"],
            [py, "-c", "print('ok\\tpartition two')"],
        ])
        self.assertEqual(status, 1)
        lines = text.splitlines()
        for line in ("rest 0", "rest 1", "rest 2", "FAIL\tpartition one", "ok\tpartition two"):
            self.assertIn(line, lines)
        self.assertEqual(lines.index("FAIL\tpartition one") - 1, next(
            i for i, line in enumerate(lines) if line.endswith("exit 1")))

    def test_all_passing_passes(self):
        py = sys.executable
        self.assertEqual(self.run_commands([[py, "-c", "pass"], [py, "-c", "pass"]])[0], 0)


if __name__ == "__main__":
    unittest.main()

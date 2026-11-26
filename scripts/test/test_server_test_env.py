"""Server-test launcher isolation with synthetic profiles, SSH, resolver and Go."""
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock
from urllib.parse import parse_qsl, urlencode, urlsplit, urlunsplit

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location("server_test_env", ROOT / "scripts/test/server_test_env.py")
ISOLATION = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(ISOLATION)


class ServerTestEnvironmentTests(unittest.TestCase):
    def test_parent_proxy_routes_are_removed_without_changing_the_parent_or_build_inputs(self):
        proxies = {key: "http://proxy.example.invalid:8080" for key in (
            "HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy",
            "NO_PROXY", "no_proxy")}
        parent = dict(proxies, PATH="/usr/bin", GOPROXY="https://modules.example.invalid",
                      PERSONA_HOME="/example/live", CODEX_HOME="/example/live/codex")
        original = dict(parent)
        child = ISOLATION.clean_environment(parent)
        self.assertEqual(parent, original)
        self.assertFalse(set(proxies) & set(child))
        self.assertEqual(child["PATH"], "/usr/bin")
        self.assertEqual(child["GOPROXY"], parent["GOPROXY"])
        self.assertNotIn("PERSONA_HOME", child)
        self.assertNotIn("CODEX_HOME", child)
        # A fixture's explicit routing is set after launch and must remain usable.
        child["HTTPS_PROXY"] = "http://fixture.example.invalid:8080"
        self.assertEqual(child["HTTPS_PROXY"], "http://fixture.example.invalid:8080")

    def test_database_url_targets_only_the_dedicated_test_database(self):
        with tempfile.TemporaryDirectory() as directory:
            config = Path(directory) / "hub.json"
            for source, expected in (
                ("postgres://user:synthetic@localhost:5432/persona?sslmode=disable", "postgres://user:synthetic@localhost:5432/persona_test?sslmode=disable"),
                ("postgresql://user:synthetic@localhost/persona", "postgresql://user:synthetic@localhost/persona_test"),
                ("postgres://user:p%40ss@[::1]:5432/persona?sslmode=require", "postgres://user:p%40ss@[::1]:5432/persona_test?sslmode=require"),
                # a hub set up before the rename keeps its jarvis database (#6149): the tests sit next to it
                ("postgres://user:synthetic@localhost:18661/jarvis?sslmode=disable", "postgres://user:synthetic@localhost:18661/jarvis_test?sslmode=disable"),
            ):
                with self.subTest(source=source):
                    config.write_text(json.dumps({"database_url": source}))
                    expected_parts = urlsplit(expected)
                    query = dict(parse_qsl(expected_parts.query, keep_blank_values=True))
                    for key, value in {"passfile": "", "sslcert": "", "sslkey": "", "sslrootcert": ""}.items():
                        query.setdefault(key, value)
                    self.assertEqual(ISOLATION.test_database_url(config),
                                     urlunsplit(expected_parts._replace(query=urlencode(query))))
                    self.assertNotEqual(source, expected)
            for source in (
                "postgres://user:synthetic@localhost/custom", "postgres://user:synthetic@localhost/persona_test",
                "postgres://user:synthetic@localhost/jarvis_test", "postgres://user:synthetic@localhost/jarvis/",
                "postgres://user:synthetic@localhost/%6aarvis", "postgres://user:synthetic@localhost/persona/",
                "postgres://user:synthetic@localhost/persona#fragment", "https://example.com/persona", "postgres:///persona", 42, None,
            ):
                with self.subTest(source=source):
                    config.write_text(json.dumps({"database_url": source}))
                    with mock.patch.object(ISOLATION.subprocess, "run") as run:
                        with self.assertRaises(ValueError):
                            ISOLATION.run_tests(config, ["go", "test"], {})
                        run.assert_not_called()

    def test_query_overrides_and_ambiguous_queries_stop_before_any_child_process(self):
        queries = (
            "dbname=persona", "database=persona", "db%6Eame=persona", "%64atabase=persona",
            "DBNAME=persona", "Database=persona", "dbname=", "dbname=persona_test",
            "dbname=persona&dbname=persona_test", "dbname=persona_test&db%6Eame=persona",
            "database=persona_test&dbname=persona", "%2564bname=persona",
            "service=fixture", "serv%69ce=fixture", "servicefile=/synthetic/config",
            "service=fixture&service=other", "passfile=/synthetic/passwords",
            "host=example.invalid", "port=5432", "user=other", "password=synthetic", "options=fixture",
            "sslmode=disable&sslmode=require", "sslmode=disable&ssl%6dode=require", "SSLMode=disable",
            "sslmode=%xx", "sslmode=%", "sslmode=%FF", "sslmode=disable;dbname=persona",
            "sslmode", "=fixture", "sslmode=disable&&connect_timeout=3",
        )
        with tempfile.TemporaryDirectory() as directory:
            config = Path(directory) / "hub.json"
            for query in queries:
                with self.subTest(query=query):
                    config.write_text(json.dumps({"database_url": "postgres://fixture:synthetic@127.0.0.1/persona?" + query}))
                    with mock.patch.object(ISOLATION.subprocess, "run") as run:
                        with self.assertRaises(ValueError):
                            ISOLATION.run_tests(config, ["go", "test"], {})
                        run.assert_not_called()

    def test_go_test_environment_has_no_runtime_profile_and_is_cleaned_up(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            profile = root / "live-profile"
            config = root / "hub.json"
            config.write_text(json.dumps({"database_url": "postgres://user:synthetic@localhost/persona"}))
            environment = {
                "HOME": str(root / "real-home"), "PATH": str(profile / "bin") + os.pathsep + "/usr/bin",
                "PERSONA_HOME": str(profile),
                "PERSONA_CONFIG": str(profile / "config.json"),
                "PERSONA_TOKEN": "synthetic-live-token", "PERSONA_DATABASE_URL": "postgres://example.invalid/production",
                "PERSONA_TEST_DATABASE_URL": "postgres://example.invalid/not_test",
                "PERSONA_OTHER_SETTING": "synthetic",
                "PGDATABASE": "persona", "PGUSER": "live-user", "PGPASSWORD": "synthetic-live-password",
                "PGHOST": "example.invalid", "PGPORT": "5432", "PGOPTIONS": "synthetic",
                "PGSERVICE": "live", "PGSERVICEFILE": str(profile / "service"),
                "PGPASSFILE": str(profile / "passwords"), "PGSSLCERT": str(profile / "certificate"),
                "PG_FUTURE_SETTING": "synthetic",
                "CLAUDE_CONFIG_DIR": str(profile / "claude"), "CODEX_HOME": str(profile / "codex"),
                "XDG_CONFIG_HOME": str(profile / "xdg"), "ZDOTDIR": str(profile / "shell"),
                "BASH_ENV": str(profile / "shellrc"), "ENV": str(profile / "envrc"), "GOTOOLCHAIN": "local",
                "HTTPS_PROXY": "http://proxy.example.invalid:8080", "https_proxy": "http://proxy.example.invalid:8080",
            }
            caches = {key: str(root / key.lower()) for key in ("GOCACHE", "GOMODCACHE", "GOPATH")}
            homes = []

            def run(command, **kwargs):
                child = kwargs["env"]
                if command[:2] == ["go", "env"]:
                    self.assertFalse(any(key.startswith(("PERSONA_", "PG")) for key in child))
                    self.assertEqual(child["HTTPS_PROXY"], environment["HTTPS_PROXY"])
                    self.assertEqual(child["https_proxy"], environment["https_proxy"])
                    return subprocess.CompletedProcess(command, 0, json.dumps(caches))
                homes.append(Path(child["HOME"]))
                self.assertTrue(homes[-1].is_dir())
                self.assertNotEqual(child["HOME"], environment["HOME"])
                self.assertTrue(Path(child["TMPDIR"]).is_dir())
                self.assertEqual(Path(child["HOME"]), Path(child["HOME"]).resolve())
                self.assertEqual({key for key in child if key.startswith(("PERSONA_"))},
                                 {"PERSONA_TEST_DATABASE_URL"})
                self.assertEqual(child['JARVIS_TEST_DATABASE_URL'], child['PERSONA_TEST_DATABASE_URL'])
                self.assertEqual(urlsplit(child["PERSONA_TEST_DATABASE_URL"]).path, "/persona_test")
                self.assertFalse(any(key.startswith("PG") for key in child))
                self.assertNotIn("HTTPS_PROXY", child)
                self.assertNotIn("https_proxy", child)
                self.assertFalse(any(str(profile) in value for value in child.values()))
                self.assertEqual(child["GOTOOLCHAIN"], "local")
                for key, value in caches.items():
                    self.assertEqual(child[key], value)
                return subprocess.CompletedProcess(command, 23)

            with mock.patch.object(ISOLATION.subprocess, "run", side_effect=run):
                self.assertEqual(ISOLATION.run_tests(config, ["go", "test", "./..."], environment), 23)
            self.assertEqual(len(homes), 1)
            self.assertFalse(homes[0].exists())

    def test_go_test_runs_without_group_write_under_a_permissive_umask(self):
        # A host whose umask is 002 (Ubuntu's default with a user's own group) must not hand it to go test: Go makes
        # t.TempDir()'s directories and the test binary with it, and the external adapter tests refuse them (#10280).
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            config = root / "hub.json"
            config.write_text(json.dumps({"database_url": "postgres://user:synthetic@localhost/persona"}))
            caches = {key: str(root / key.lower()) for key in ("GOCACHE", "GOMODCACHE", "GOPATH")}
            seen = {}

            def run(command, **kwargs):
                if command[:2] == ["go", "env"]:
                    return subprocess.CompletedProcess(command, 0, json.dumps(caches))
                current = os.umask(0)
                os.umask(current)
                seen["umask"] = current  # what the test command inherits
                for key in ("HOME", "TMPDIR", "GOTMPDIR"):
                    seen[key] = os.stat(kwargs["env"][key]).st_mode & 0o777
                return subprocess.CompletedProcess(command, 0)

            previous = os.umask(0o002)
            try:
                with mock.patch.object(ISOLATION.subprocess, "run", side_effect=run):
                    self.assertEqual(ISOLATION.run_tests(config, ["go", "test"], {"PATH": "/usr/bin"}), 0)
                after = os.umask(0o002)
            finally:
                os.umask(previous)
            self.assertEqual(seen["umask"], 0o022)
            for key in ("HOME", "TMPDIR", "GOTMPDIR"):
                self.assertEqual(seen[key] & 0o022, 0, f"{key} is {seen[key]:o}")
            self.assertEqual(after, 0o002, "the runner's own umask is restored")

    def test_go_flags_keep_the_effective_value_and_add_trimpath(self):
        # The temporary HOME hides a `go env -w GOFLAGS`: the launcher resolves it first and passes it on with
        # -trimpath, so checkouts share compiled packages (#10614).
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            config = root / "hub.json"
            config.write_text(json.dumps({"database_url": "postgres://user:synthetic@localhost/persona"}))
            caches = {key: str(root / key.lower()) for key in ("GOCACHE", "GOMODCACHE", "GOPATH")}
            for effective, expected in ((None, "-trimpath"), ("", "-trimpath"), ("-mod=mod", "-mod=mod -trimpath"),
                                        ("-trimpath=false", "-trimpath=false")):
                seen = {}

                def run(command, **kwargs):
                    if command[:2] == ["go", "env"]:
                        seen["query"] = command
                        values = dict(caches) if effective is None else dict(caches, GOFLAGS=effective)
                        return subprocess.CompletedProcess(command, 0, json.dumps(values))
                    seen["GOFLAGS"] = kwargs["env"].get("GOFLAGS")
                    return subprocess.CompletedProcess(command, 0)

                with self.subTest(effective=effective), mock.patch.object(ISOLATION.subprocess, "run", side_effect=run):
                    self.assertEqual(ISOLATION.run_tests(config, ["go", "test"], {"PATH": "/usr/bin"}), 0)
                    self.assertIn("GOFLAGS", seen["query"])
                    self.assertEqual(seen["GOFLAGS"], expected)
            result = subprocess.CompletedProcess(["go", "env"], 0, json.dumps(dict(caches, GOFLAGS=["-trimpath"])))
            with mock.patch.object(ISOLATION.subprocess, "run", return_value=result) as run:
                with self.assertRaises(ValueError):
                    ISOLATION.run_tests(config, ["go", "test"], {"PATH": "/usr/bin"})
                self.assertEqual(run.call_count, 1)

    def test_cache_failure_never_starts_the_test_command(self):
        with tempfile.TemporaryDirectory() as directory:
            config = Path(directory) / "hub.json"
            config.write_text(json.dumps({"database_url": "postgres://user:synthetic@localhost/persona"}))
            for result in (subprocess.CompletedProcess(["go", "env"], 1, "synthetic-private-output"),
                           subprocess.CompletedProcess(["go", "env"], 0, "not-json"),
                           subprocess.CompletedProcess(["go", "env"], 0, "{}")):
                with self.subTest(result=result.returncode, output=result.stdout):
                    with mock.patch.object(ISOLATION.subprocess, "run", return_value=result) as run:
                        with self.assertRaises(ValueError):
                            ISOLATION.run_tests(config, ["go", "test"], {"PERSONA_TOKEN": "synthetic-token"})
                        self.assertEqual(run.call_count, 1)
                        self.assertEqual(run.call_args.args[0][:2], ["go", "env"])
                        self.assertNotIn("PERSONA_TOKEN", run.call_args.kwargs["env"])


if __name__ == "__main__":
    unittest.main()

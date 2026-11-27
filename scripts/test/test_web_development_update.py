"""Isolated Git, process and history fixtures for Web development updates."""
import contextlib
import fcntl
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import time
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("webdev", ROOT / "server/internal/update/webdev.py")
webdev = importlib.util.module_from_spec(spec)
spec.loader.exec_module(webdev)


class DevelopmentFixtures(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="persona-web-dev-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.bin = self.root / "tools"
        self.bin.mkdir()
        self.env = patch.dict(os.environ, {"PATH": str(self.bin) + os.pathsep + os.environ["PATH"],
                                          "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.devnull})
        self.env.start()
        self.addCleanup(self.env.stop)

    def tool(self, name, source):
        path = self.bin / name
        path.write_text("#!" + sys.executable + "\n" + source)
        path.chmod(0o755)
        return str(path)

    def git(self, *args):
        return subprocess.check_output(["git", "-C", str(self.repo), *args], text=True, stderr=subprocess.DEVNULL).strip()


    def history_fixture(self):
        runtime = self.root / "runtime"
        (runtime / "data").mkdir(parents=True)
        (runtime / "hub.json").write_text(json.dumps({"database_url":"fixture-only"}))
        (runtime / "secret.key").write_bytes(b"fixture-key")
        (runtime / "data/history.txt").write_text("synthetic history")
        (runtime / "web").mkdir()
        (runtime / "web/index.html").write_bytes(b"saved web override")
        binary = self.root / "personad"
        binary.write_bytes(b"fixture installed program")
        database = self.root / "database.json"
        database.write_text('{"messages":["synthetic history"]}')
        self.env_db = patch.dict(os.environ, {"FIXTURE_DB":str(database)})
        self.env_db.start()
        self.addCleanup(self.env_db.stop)
        self.tool("pg_dump", "import os,pathlib,sys\np=pathlib.Path(sys.argv[sys.argv.index('--file')+1]);p.write_bytes(b'PGDMP'+pathlib.Path(os.environ['FIXTURE_DB']).read_bytes())\n")
        self.tool("pg_restore", "import pathlib,sys\nassert pathlib.Path(sys.argv[-1]).read_bytes().startswith(b'PGDMP')\nprint('fixture archive list')\n")
        events = self.root / "events"
        hook = self.tool("writer-hook", "import pathlib,sys\np=pathlib.Path(sys.argv[1]);p.write_text((p.read_text() if p.exists() else '')+sys.argv[2]+'\\n')\n")
        return {"runtime": str(runtime), "hub_database":"effective-fixture-database", "executables":{"personad":str(binary)}, "candidate":{"commit":"a"*40}, "config":{"backup":{
            "quiesce":[hook,str(events),"pause"], "resume":[hook,str(events),"resume"]}}}

    def wait_file(self, path, process):
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            if path.exists():
                return
            if process.poll() is not None:
                self.fail("helper exited before " + path.name)
            time.sleep(.01)
        self.fail("timed out waiting for " + path.name)


    def test_builtin_quiesce_custom_directory_and_retention(self):
        request = self.history_fixture()
        runtime = Path(request["runtime"])
        # The Hub paused the writers itself: no hooks, no coordination marker.
        directory = runtime / "backups/web-dev"
        request["config"]["backup"] = {"builtin": True, "directory": str(directory), "retain": 2}
        receipts = [webdev.snapshot(request) for _ in range(3)]
        snapshots = sorted(p for p in directory.iterdir() if webdev.NAME.fullmatch(p.name))
        self.assertEqual(len(snapshots), 2)
        self.assertEqual([p.name for p in snapshots], [r["id"] for r in receipts[-2:]])
        self.assertEqual(receipts[-1]["retained"], 2)
        self.assertFalse((runtime / "update/development-backups").exists())
        self.assertFalse((directory / "coordination-pending.json").exists())
        # A folder inside the runtime is never archived into the next snapshot.
        with tarfile.open(snapshots[-1] / "files.tar.gz") as archive:
            self.assertFalse(any("backups/web-dev" in member.name for member in archive))
        # The folder must be private.
        public = runtime / "public-snapshots"
        public.mkdir()
        public.chmod(0o755)
        request["config"]["backup"]["directory"] = str(public)
        with self.assertRaisesRegex(ValueError, "private"):
            webdev.snapshot(request)
        # Relative paths and an out-of-range retention are refused.
        for broken in ({"directory": "relative/dir"}, {"directory": str(directory), "retain": 51}):
            request["config"]["backup"] = dict(broken, builtin=True)
            with self.assertRaises(ValueError):
                webdev.snapshot(request)
        # Without the built-in pause, hooks are still required.
        request["config"]["backup"] = {}
        with self.assertRaisesRegex(ValueError, "quiesce"):
            webdev.snapshot(request)

    def test_existing_backslash_runtime_and_backup_directory(self):
        request = self.history_fixture()
        original_runtime = Path(request["runtime"])
        runtime = self.root / r"persona\prod"
        original_runtime.rename(runtime)
        request["runtime"] = str(runtime)
        recommended = runtime / "update" / "development-backups"
        request["config"]["backup"] = {"builtin": True, "directory": str(recommended)}
        receipt = webdev.snapshot(request)
        manifest = webdev.verify(recommended / receipt["id"])
        self.assertEqual(manifest["roots"], [str(runtime.resolve())])

        existing_store = self.root / r"existing\store"
        existing_store.mkdir(mode=0o700)
        request["config"]["backup"] = {"builtin": True, "directory": str(existing_store)}
        receipt2 = webdev.snapshot(request)
        webdev.verify(existing_store / receipt2["id"])

        for bad in (
            str(runtime / r"new\dir"),
            str(runtime / r"new\dir" / "snapshots"),
            str(runtime) + "//update/development-backups",
            str(runtime / "update" / ".." / "escape"),
            str(runtime / "bad\x00dir"),
        ):
            request["config"]["backup"] = {"builtin": True, "directory": bad}
            with self.assertRaisesRegex(ValueError, "invalid backup directory"):
                webdev.backup_store(request)

    def test_builtin_quiesce_refuses_extra_sources_and_other_local_managers(self):
        request = self.history_fixture()
        external = dict(request["config"]["backup"])
        runtime = Path(request["runtime"])
        extra_root = self.root / "extra-root"
        extra_root.mkdir()
        (extra_root / "note.txt").write_text("extra")

        # Built-in pause refuses extra_databases and extra_roots.
        for extra in ({"extra_databases": ["extra-db"]}, {"extra_roots": [str(extra_root)]}):
            request["config"]["backup"] = dict(extra, builtin=True)
            with self.assertRaisesRegex(ValueError, "built-in writer pause"):
                webdev.snapshot(request)

        # Built-in pause refuses another local manager even with no owned machines.
        other = runtime / "managers" / "m-other"
        other.mkdir(parents=True)
        (other / "manager.json").write_text(json.dumps({"database_url": "other-manager-db"}))
        request["config"]["backup"] = {"builtin": True}
        with self.assertRaisesRegex(ValueError, "other local managers"):
            webdev.snapshot(request)

        # External quiesce/resume hooks still work with extra sources and another local manager.
        request["config"]["backup"] = dict(external, extra_databases=["extra-db"], extra_roots=[str(extra_root)])
        receipt = webdev.snapshot(request)
        manifest = webdev.verify(runtime / "update/development-backups" / receipt["id"])
        self.assertEqual(manifest["databases"], 3)
        self.assertIn(str(extra_root.resolve()), manifest["roots"])

    def test_store_may_not_be_or_contain_a_collected_root(self):
        request = self.history_fixture()
        runtime = Path(request["runtime"])
        # The store and everything below it leave the archive: a store that is
        # the runtime, or holds it, would drop what the snapshot is for.
        for store in (runtime, runtime.parent):
            request["config"]["backup"] = {"builtin": True, "directory": str(store)}
            with self.assertRaisesRegex(ValueError, "may not be the runtime or contain"):
                webdev.snapshot(request)
        self.assertFalse((runtime / "update" / "development-backups").exists())


    def test_resume_failure_and_unknown_coordination_block_retry(self):
        request = self.history_fixture()
        request["config"]["backup"]["resume"] = [self.tool("resume-fails", "raise SystemExit(2)\n")]
        with self.assertRaises(subprocess.CalledProcessError):
            webdev.snapshot(request)
        with self.assertRaisesRegex(ValueError,"coordination needs recovery"):
            webdev.snapshot(request)

    def test_backup_and_resume_failures_both_reach_private_log(self):
        request = self.history_fixture()
        self.tool("pg_dump", "raise SystemExit(7)\n")
        request["config"]["backup"]["resume"] = [self.tool("resume-fails", "raise SystemExit(2)\n")]
        log = io.StringIO()
        with contextlib.redirect_stderr(log), self.assertRaises(subprocess.CalledProcessError) as caught:
            webdev.snapshot(request)
        self.assertEqual(caught.exception.returncode, 2)
        self.assertEqual(caught.exception.__cause__.returncode, 7)
        self.assertIn("snapshot failed: CalledProcessError (exit 7)", log.getvalue())
        self.assertIn("resume failed: CalledProcessError (exit 2)", log.getvalue())
        self.assertNotIn("effective-fixture-database", log.getvalue())
        store = Path(request["runtime"])/"update/development-backups"
        self.assertTrue((store/"coordination-pending.json").exists())
        self.assertEqual(list(store.glob("snapshot-*")), [])

    def test_snapshot_and_prepare_report_lock_contention(self):
        request = self.history_fixture()
        receipt = webdev.snapshot(request)
        store = Path(request["runtime"])/"update/development-backups"
        with (store/".lock").open("a") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            for operation, payload in [(webdev.snapshot, request), (webdev.prepare_backup, dict(request, backup_id=receipt["id"]))]:
                with self.subTest(operation=operation.__name__), self.assertRaisesRegex(ValueError, "another backup or prepare is in progress"):
                    operation(payload)

    def test_real_writer_is_quiesced_for_the_whole_snapshot(self):
        request = self.history_fixture()
        runtime = Path(request["runtime"])
        pause, paused, ready = self.root/"pause", self.root/"paused", self.root/"writer-ready"
        writer = self.root/"writer.py"
        writer.write_text('''import pathlib,sys,time
root,pause,paused,db,ready=map(pathlib.Path,sys.argv[1:])
counter=0
while True:
    if pause.exists():
        paused.touch()
        while pause.exists(): time.sleep(.002)
        paused.unlink(missing_ok=True)
    counter+=1
    (root/'data/history.txt').write_text(str(counter))
    db.write_text(str(counter))
    if counter == 1: ready.touch()
    time.sleep(.002)
''')
        process = subprocess.Popen([sys.executable,str(writer),str(runtime),str(pause),str(paused),str(self.root/"database.json"),str(ready)])
        self.addCleanup(lambda: (process.terminate(),process.wait(timeout=3)))
        # The byte comparison requires the writer's first completed paired write.
        self.wait_file(ready, process)
        hook = self.tool("coordinate", '''import pathlib,sys,time
pause,paused=map(pathlib.Path,sys.argv[1:3])
if sys.argv[3]=='pause': pause.touch()
else: pause.unlink(missing_ok=True)
for _ in range(1000):
    if paused.exists()==(sys.argv[3]=='pause'): break
    time.sleep(.002)
else: raise SystemExit(3)
''')
        request["config"]["backup"].update(quiesce=[hook,str(pause),str(paused),"pause"],resume=[hook,str(pause),str(paused),"resume"])
        receipt = webdev.snapshot(request)
        path = runtime / "update/development-backups" / receipt["id"]
        with tarfile.open(path/"files.tar.gz") as archive:
            history = archive.extractfile("root-0/data/history.txt").read()
        self.assertEqual((path/"database-0.dump").read_bytes()[5:], history)
        self.assertFalse(pause.exists())
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            if process.poll() is not None:
                self.fail("writer exited before resuming")
            live_history = (runtime/"data/history.txt").read_bytes()
            live_database = (self.root/"database.json").read_bytes()
            if live_history not in (b"", history) and live_history == live_database:
                break
            time.sleep(.01)
        else:
            self.fail("writer did not resume paired writes")


if __name__ == "__main__":
    unittest.main()

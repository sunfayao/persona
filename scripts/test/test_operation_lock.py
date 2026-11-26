"""Operation locks must serialize unrelated callers and allow nested deployment steps."""
import fcntl
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().parents[1] / "deploy/operation_lock.py"


class OperationLockTests(unittest.TestCase):
    @staticmethod
    def local_deploy_env(home):
        root = Path(home) / ".persona"
        cfg = Path(home) / "config.json"
        cfg.write_text(json.dumps({"hub": {"machine": "hub"}, "machines": [
            {"id": "hub", "hostnames": [socket.gethostname()]}]}))
        env = dict(os.environ, HOME=home, PERSONA_CONFIG=str(cfg), PERSONA_HOME=str(root))
        env.pop("PERSONA_OPERATION_DEPLOY_LOCKED", None)
        return env

    def test_nested_deploy_and_result(self):
        with tempfile.TemporaryDirectory() as home:
            env = self.local_deploy_env(home)
            command = [sys.executable, str(SCRIPT), "--", sys.executable,
                       str(SCRIPT), "--", sys.executable, "-c", "raise SystemExit(3)"]
            result = subprocess.run(command, env=env, capture_output=True, text=True)
            self.assertEqual(result.returncode, 3, result.stderr)
            self.assertEqual(result.stdout.count("acquired"), 1)

    def test_deploy_uses_existing_promotion_lock(self):
        with tempfile.TemporaryDirectory() as home:
            env = self.local_deploy_env(home)
            lock = Path(env["PERSONA_HOME"]) / "releases/hub/.publish.lock"
            lock.parent.mkdir(parents=True)
            with lock.open("a") as stream:
                fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
                result = subprocess.run([sys.executable, str(SCRIPT), "--", sys.executable,
                                         "-c", "raise Exception('must not run')"], env=env, capture_output=True, text=True)
                self.assertEqual(result.returncode, 75, result.stderr)


if __name__ == "__main__":
    unittest.main()

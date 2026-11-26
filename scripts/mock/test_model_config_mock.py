"""Exercise the unified model settings mock without a simulator or real providers."""
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import time
import unittest
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen


class MockHubTests(unittest.TestCase):
    mock_env = {}

    def setUp(self):
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        self.base = f"http://127.0.0.1:{port}"
        self.log = tempfile.TemporaryFile()
        env = {k: v for k, v in os.environ.items() if not k.startswith("MOCK_")}
        self.process = subprocess.Popen(
            ["python3", "-I", str(Path(__file__).with_name("mock_hub.py"))],
            env=dict(env, PORT=str(port), **self.mock_env), stdout=self.log, stderr=self.log)
        self.addCleanup(self.stop)
        for _ in range(60):
            if self.process.poll() is not None:
                raise RuntimeError("mock hub exited before startup")
            try:
                with urlopen(self.base + "/api/v1/health", timeout=1):
                    return
            except (URLError, TimeoutError):
                time.sleep(0.1)
        raise RuntimeError("mock hub startup timed out")

    def stop(self):
        self.process.terminate()
        try:
            self.process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            self.process.kill()
            self.process.wait(timeout=5)
        self.log.close()

    def request(self, path, body=None, method=None, language=None):
        headers = {"Content-Type": "application/json"}
        if language:
            headers["Accept-Language"] = language
        request = Request(self.base + path,
                          data=None if body is None else json.dumps(body).encode(),
                          method=method or ("GET" if body is None else "PATCH"), headers=headers)
        with urlopen(request, timeout=3) as response:
            return json.load(response)

    def assert_status(self, code, path, body=None, method=None, language=None):
        with self.assertRaises(HTTPError) as error:
            self.request(path, body, method, language)
        with error.exception as response:
            self.assertEqual(response.code, code)
            return json.load(response)


class ModelConfigMockTests(MockHubTests):

    def test_catalog(self):
        catalog = self.request("/api/v1/model-config")
        ids = [p["id"] for p in catalog["purposes"]]
        self.assertEqual(ids, ["assistant", "agent.new", "kb", "inbox.triage"])
        self.assertTrue(all(p["category_id"] == p["id"] and p["uses"] for p in catalog["purposes"]))

    def test_save_conflict_and_restore(self):
        path = "/api/v1/model-config/purposes/agent.new"
        original = self.request(path)["purpose"]
        saved = self.request(path, {"revision": original["revision"], "model": "model-mock-test"})["purpose"]
        self.assertEqual(saved["effective"]["model"], "model-mock-test")
        self.assertEqual(saved["revision"], original["revision"] + 1)
        with self.assertRaises(HTTPError) as error:
            self.request(path, {"revision": original["revision"], "model": "stale"})
        self.assertEqual(error.exception.code, 409)
        restored = self.request(path, {"revision": saved["revision"], "model": None})["purpose"]
        self.assertNotIn("model", restored["override"])
        self.assertEqual(restored["effective"]["model"], "model-chat")

    def test_uses_follow_category_and_cannot_be_edited(self):
        category_path = "/api/v1/model-config/purposes/kb"
        original = self.request(category_path)["purpose"]
        self.request(category_path, {"revision": original["revision"], "model": "processing-model"})
        for pid in ("translate", "inbox.draft", "kb.compress", "assistant.title"):
            path = "/api/v1/model-config/purposes/" + pid
            purpose = self.request(path)["purpose"]
            self.assertEqual(purpose["category_id"], "kb")
            self.assertEqual(purpose["effective"]["model"], "processing-model")
            self.assertEqual(purpose["override"], {})
            self.assertEqual(purpose["fields"], [])
            with self.assertRaises(HTTPError) as error:
                self.request(path, {"revision": purpose["revision"], "model": "individual"})
            self.assertEqual(error.exception.code, 400)
        self.assertEqual(len(self.request("/api/v1/model-config")["purposes"]), 4)

    def test_unknown_purpose_is_not_created(self):
        for body in (None, {"revision": 0, "model": "x"}):
            with self.assertRaises(HTTPError) as error:
                self.request("/api/v1/model-config/purposes/unknown", body)
            self.assertEqual(error.exception.code, 404)


if __name__ == "__main__":
    unittest.main()

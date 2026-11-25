"""Protected CI configuration is complete, private, and absent from public artifacts."""
import json
from pathlib import Path
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parent))
import ci_release_config as config


class CIReleaseConfigTests(unittest.TestCase):
    def test_public_configuration_names_only_the_public_update_source(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            path = config.prepare_public(root, {"GITHUB_REPOSITORY": "nwverse/persona"})
            data = json.loads(path.read_text())
            self.assertEqual(data["publish"]["update_url"], "https://github.com/nwverse/persona/releases/latest/download")
            self.assertEqual(data["publish"]["site_url"], "https://github.com/nwverse/persona")
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)

    def test_a_missing_repository_fails_before_building(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(config.ConfigurationError):
                config.prepare_public(Path(directory), {})


if __name__ == "__main__":
    unittest.main()

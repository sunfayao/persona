"""Keep extension UI-test environment variables and mock setup aligned."""
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


class ExtensionTestNames(unittest.TestCase):
    def test_mock_uses_the_same_extension_term(self):
        mock = (ROOT / "scripts/mock/mock_hub.py").read_text()
        self.assertIn('os.environ.get("MOCK_EXTENSIONS"', mock)
        docs = (ROOT / "docs/operations/testing.md").read_text()
        self.assertIn("MOCK_EXTENSIONS=1", docs)


if __name__ == "__main__":
    unittest.main()

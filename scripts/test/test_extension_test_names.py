"""Keep the mock hub's extension switch under its one name."""
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


class ExtensionTestNames(unittest.TestCase):
    def test_mock_uses_the_same_extension_term(self):
        mock = (ROOT / "scripts/mock/mock_hub.py").read_text()
        self.assertIn('os.environ.get("MOCK_EXTENSIONS"', mock)


if __name__ == "__main__":
    unittest.main()

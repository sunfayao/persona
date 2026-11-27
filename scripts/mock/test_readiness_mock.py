"""Every MOCK_READINESS scenario of mock_hub.py but the ones the hub cannot be in has the hub's own answers, which
server/internal/assistant/readiness_scenarios_test.go keeps in testdata/readiness_scenarios/.
"""
from pathlib import Path
import unittest

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent
GOLDEN = ROOT / "server" / "internal" / "assistant" / "testdata" / "readiness_scenarios"
# Scenarios with no golden: demo is full on the mock's usual data, assistant_offline is the hub's own 503.
NO_GOLDEN = {"demo", "assistant_offline"}


class ScenarioGoldenTests(unittest.TestCase):
    def test_every_scenario_has_its_golden(self):
        import sys
        sys.path.insert(0, str(HERE))
        import readiness_mock
        names = set(readiness_mock.SCENARIOS) - NO_GOLDEN
        self.assertEqual(names, {p.stem for p in GOLDEN.glob("*.json")})


if __name__ == "__main__":
    unittest.main()

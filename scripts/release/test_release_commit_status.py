"""Commit status opt-in never replaces either worker's local outcome."""
import contextlib
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import hub_release as hub

SHA = 'a' * 40
OMITTED = object()


def policy(config, value):
    if value is not OMITTED:
        config['report_commit_status'] = value
    return config


class CommitStatusTests(unittest.TestCase):
    def test_hub_success_and_all_failure_paths_retain_local_receipts(self):
        for value in (OMITTED, False, True):
            for failure in (None, 'validation', 'publication', 'status-api'):
                with self.subTest(policy=value, failure=failure), tempfile.TemporaryDirectory() as temporary:
                    root = Path(temporary)
                    config = policy(dict(checkout=str(root), state_dir=str(root / 'state'),
                        repository='example/project', update_url='file:///example', publish_command=['publisher']), value)
                    config_path = root / 'config.json'
                    config_path.write_text(json.dumps(config))
                    calls = []

                    def output(argv, cwd):
                        if argv[1] == 'remote':
                            return 'https://github.com/example/project.git'
                        return SHA if argv[1] == 'rev-parse' else ''

                    def run(argv, cwd, **kwargs):
                        if argv[:2] == ['gh', 'api']:
                            calls.append(argv)
                            if failure == 'status-api':
                                raise RuntimeError('status API denied')
                        if failure == 'validation' and 'scripts/release/build_hub_release.py' in argv:
                            raise RuntimeError('validation failed')
                        if failure == 'publication' and argv[0] == 'publisher':
                            raise RuntimeError('publication failed')

                    expected_failure = failure in ('validation', 'publication') or failure == 'status-api' and value is True
                    with patch('sys.argv', ['worker', '--config', str(config_path)]), \
                            patch.object(hub, 'output', side_effect=output), patch.object(hub, 'run', side_effect=run), \
                            patch.object(hub, 'cache_ready', return_value=False), contextlib.redirect_stdout(io.StringIO()):
                        if expected_failure:
                            with self.assertRaises(RuntimeError):
                                hub.main()
                        else:
                            hub.main()
                    result = json.loads((root / 'state/last.json').read_text())
                    receipt = json.loads((Path(result['evidence']) / 'worker-receipt.json').read_text())
                    self.assertEqual(result, receipt)
                    self.assertEqual(result['result'], 'failure' if expected_failure else 'success')
                    self.assertEqual(result['report_commit_status'], value is True)
                    self.assertTrue((Path(result['evidence']) / 'validation.log').exists() or failure == 'status-api' and value is True)
                    if value is True:
                        states = [next(arg for arg in call if arg.startswith('state=')) for call in calls]
                        self.assertEqual(states, ['state=pending', 'state=failure'] if expected_failure else ['state=pending', 'state=success'])
                    else:
                        self.assertEqual(calls, [])



if __name__ == '__main__':
    unittest.main()

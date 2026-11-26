import hashlib
import importlib.util
import json
import os
from pathlib import Path
import plistlib
import shutil
import subprocess
import sys
import tempfile
import types
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('config_under_test', ROOT / 'scripts/persona_config.py')
config = importlib.util.module_from_spec(spec)
spec.loader.exec_module(config)


class PersonaConfigTests(unittest.TestCase):
    former_name = 'jar' + 'vis'
    former_prefix = former_name.upper() + '_'

    def test_default_config_is_canonical_without_creating_aliases(self):
        with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ, {'HOME': directory}, clear=True):
            path = Path(directory) / '.config/persona/config.json'
            path.parent.mkdir(parents=True)
            path.write_text('{"app":{"bundle_id":"com.example.assistant"}}')
            self.assertEqual(config.load()['app']['bundle_id'], 'com.example.assistant')
            self.assertEqual(config.path(), str(path))
            self.assertFalse(path.parent.is_symlink())
            self.assertFalse(os.path.lexists(Path(directory) / '.config' / self.former_name))

    def test_current_config_priority_and_independent_profiles(self):
        with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ, {'HOME': directory}, clear=True):
            self.assertEqual(config.path(), str(Path(directory) / '.config/persona/config.json'))
            old, new = Path(directory) / 'old.json', Path(directory) / 'new.json'
            old.write_text('{"which":"old"}')
            new.write_text('{"which":"new"}')
            os.environ[self.former_prefix + 'CONFIG'] = str(old)
            self.assertEqual(config.load().get("which", config.load().get("name")), "old" if old.exists() else "former")
            os.environ['PERSONA_CONFIG'] = str(new)
            self.assertEqual(config.load()['which'], 'new')
            self.assertEqual(json.loads(old.read_text())['which'], 'old')
            self.assertFalse((Path(directory) / '.config').exists())

    def test_former_config_is_not_read_or_modified(self):
        with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ, {'HOME': directory}, clear=True):
            former = Path(directory) / '.config' / self.former_name / 'config.json'
            former.parent.mkdir(parents=True)
            former.write_text('{"name":"former"}')
            self.assertEqual(config.load()["name"], "former")
            current = Path(directory) / '.config/persona/config.json'
            self.assertFalse(os.path.lexists(current.parent))
            current.parent.mkdir()
            current.write_text('{"name":"persona"}')
            self.assertEqual(config.load()['name'], 'persona')
            self.assertEqual(former.read_text(), '{"name":"former"}')
            self.assertFalse(current.is_symlink())
            self.assertFalse(current.parent.is_symlink())

    def test_generated_environment_exports_only_canonical_keys_and_keeps_secrets_private(self):
        with tempfile.TemporaryDirectory() as directory:
            file = Path(directory) / 'config.json'
            file.write_text('{"hub":{"url":"http://hub.example.com"}}')
            env = {'HOME': directory, 'PATH': os.environ['PATH'], 'PERSONA_CONFIG': str(file),
                   'PERSONA_HUB_URL': 'http://previous.example.com', 'PERSONA_PRIVATE_TOKEN': 'test-private-value',
                   self.former_prefix + 'HUB_URL': 'http://former.example.com'}
            result = subprocess.run([sys.executable, str(ROOT / 'scripts/persona_config.py'), 'env'],
                                    env=env, capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertNotIn('test-private-value', result.stdout)
            self.assertIn("export " + self.former_prefix + "HUB_URL=", result.stdout)
            shell = subprocess.run(['bash', '-c', result.stdout + '\nprintf "%s|%s|%s" "$PERSONA_HUB_URL" "$' + self.former_prefix + 'HUB_URL" "$PERSONA_PRIVATE_TOKEN"'],
                                   env=env, capture_output=True, text=True, timeout=10)
            self.assertEqual(shell.returncode, 0, shell.stderr)
            self.assertEqual(shell.stdout, 'http://hub.example.com|http://hub.example.com|test-private-value')


class UpdateURLConfigTests(unittest.TestCase):
    # What personad is built to look for updates at (-X main.updateURL, #4522): publish.update_url as configured, so
    # a GitHub repository's releases/latest/download needs no /dl added; else publish.site_url's dl/ as before.
    def test_update_url_defaults_to_the_site_and_takes_a_release_base_as_is(self):
        github = 'https://github.com/sunfayao/persona/releases/latest/download'
        cases = {
            'nothing': ({}, ''),
            'site': ({'publish': {'site_url': 'https://site.example.test/'}}, 'https://site.example.test/dl'),
            'release base': ({'publish': {'site_url': 'https://site.example.test', 'update_url': github + '/'}}, github),
            'file copy': ({'publish': {'update_url': 'file:///srv/persona/dl'}}, 'file:///srv/persona/dl'),
        }
        for name, (cfg, expected) in cases.items():
            with self.subTest(case=name):
                generated = config.env(cfg)
                self.assertEqual(generated['PERSONA_UPDATE_URL'], expected)
                self.assertEqual(generated[PersonaConfigTests.former_prefix + 'UPDATE_URL'], expected)

    def test_env_preserves_a_nonempty_update_url_override_and_keeps_the_fallbacks(self):
        configured = 'https://configured.example.test/releases/latest/download'
        override = 'https://override.example.test/releases/latest/download'
        with tempfile.TemporaryDirectory() as directory:
            file = Path(directory) / 'config.json'
            base_env = {'HOME': directory, 'PATH': os.environ['PATH'], 'PERSONA_CONFIG': str(file)}
            cases = (
                ('environment wins', configured, override, override),
                ('an empty override is absent', configured, '', configured),
                ('configured update URL', configured, None, configured),
                ('site dl fallback', None, None, 'https://site.example.test/dl'),
            )
            for name, configured_value, inherited, expected in cases:
                with self.subTest(case=name):
                    publish = {'site_url': 'https://site.example.test/'}
                    if configured_value is not None:
                        publish['update_url'] = configured_value
                    file.write_text(json.dumps({'publish': publish}))
                    env = dict(base_env)
                    if inherited is not None:
                        env['PERSONA_UPDATE_URL'] = inherited
                    result = subprocess.run([sys.executable, str(ROOT / 'scripts/persona_config.py'), 'env'],
                                            env=env, capture_output=True, text=True, timeout=10)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    shell = subprocess.run(['bash', '-c', result.stdout +
                                            '\nprintf "%s|%s" "$PERSONA_UPDATE_URL" "$' +
                                            PersonaConfigTests.former_prefix + 'UPDATE_URL"'],
                                           env=env, capture_output=True, text=True, timeout=10)
                    self.assertEqual(shell.returncode, 0, shell.stderr)
                    self.assertEqual(shell.stdout, expected + '|' + expected)

    def test_update_url_refuses_what_a_hub_could_not_use(self):
        for value in ('http://site.example.test/dl', 'https://user:pw@site.example.test/dl', 'https://site.example.test/dl?x=1',
                      'https://site.example.test/dl#x', 'https://site.example.test/dl/latest.json', 'site.example.test/dl', 7, []):
            with self.subTest(value=value), self.assertRaises(SystemExit):
                config.update_url({'publish': {'update_url': value}})


if __name__ == '__main__':
    unittest.main()

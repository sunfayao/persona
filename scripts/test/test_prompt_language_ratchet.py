import importlib.util
from pathlib import Path
import tempfile
import unittest

SOURCE = Path(__file__).resolve().parents[1] / "ci" / "prompt_language_ratchet.py"


class PromptLanguageRatchetTests(unittest.TestCase):
    def setUp(self):
        spec = importlib.util.spec_from_file_location("prompt_language_ratchet", SOURCE)
        self.ratchet = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.ratchet)

    def scan(self, files):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name, text in files.items():
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(text)
            return self.ratchet.scan(root)

    def test_every_template_with_cjk_text_is_listed(self):
        got = self.scan({
            "agents/old.md": "用中文写",
            "agents/branch.md": '{{if .Language}}Write in {{.Language}}{{else}}用中文写{{end}}',
            "voice/full.md": '{{if eq .Lang "en"}}Hello{{else}}你好{{end}}',
            "agents/comment.md": '{{/* 说明 */}}\nEnglish instructions',
            "agents/data.md": 'The field is .Language. 中文',
            "agents/plain.md": "Write in {{lang .Language}}",
        })
        self.assertEqual(got, {"agents": ["agents/branch.md", "agents/data.md", "agents/old.md"], "voice": ["voice/full.md"]})

    def test_data_templates_keep_their_text(self):
        name = next(iter(self.ratchet.DATA))
        self.assertEqual(self.scan({name: "早报", "agents/other.md": "早报"}), {"agents": ["agents/other.md"]})
        self.assertTrue(all(reason.strip() for reason in self.ratchet.DATA.values()))

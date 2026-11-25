"""The bilingual README: reciprocal switches, the same shape in both languages, and its pictures."""
from pathlib import Path
import re
import unittest
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[2]
SVG = "{http://www.w3.org/2000/svg}"
README_ASSETS = ROOT / "docs/assets/readme"
READMES = {"en": ROOT / "README.md", "zh-CN": ROOT / "README.zh-CN.md"}
SUFFIX = {"en": "", "zh-CN": ".zh-CN"}
THEMES = ("light", "dark")
FEATURES = 6
BADGE = re.compile(r"\[!\[([^\]]+)\]\(docs/assets/readme/badge-([a-z-]+)\.svg\)\]\(([^)]+)\)")


def read(lang):
    return READMES[lang].read_text(encoding="utf-8")


def section(text, index):
    """The body of the index-th "## " section."""
    return re.split(r"^## .*$", text, flags=re.M)[index + 1]


class ReadmeLanguageTests(unittest.TestCase):
    def test_readmes_switch_languages_at_the_top(self):
        en, zh = read("en"), read("zh-CN")
        self.assertIn("[简体中文](README.zh-CN.md)", "\n".join(en.splitlines()[:12]))
        self.assertIn("[English](README.md)", "\n".join(zh.splitlines()[:12]))

    def test_both_languages_have_the_same_shape(self):
        en, zh = read("en"), read("zh-CN")
        heads = lambda s: re.findall(r"^(#{1,6}) ", s, re.M)  # noqa: E731
        self.assertEqual(heads(en), heads(zh))
        for lang in READMES:
            with self.subTest(lang=lang):
                features = re.findall(r"^- \*\*", section(read(lang), 0), re.M)
                self.assertEqual(FEATURES, len(features), "the first section lists the six features")

    def test_each_language_uses_its_own_banner(self):
        for lang, suffix in SUFFIX.items():
            pictures = re.findall(r'(?:src|srcset)="docs/assets/readme/(banner-[^"]+)"', read(lang))
            with self.subTest(lang=lang):
                self.assertEqual(sorted(f"banner-{theme}{suffix}.svg" for theme in THEMES), sorted(pictures))

    def test_badges_are_the_same_in_both_languages_and_link_to_maintained_docs(self):
        found = {lang: BADGE.findall(read(lang)) for lang in READMES}
        self.assertEqual(found["en"], found["zh-CN"])
        for label, name, target in found["en"]:
            kind = label.split(":")[0]
            self.assertIn(kind, ("agent", "engine"), name)
            path = target.split("#", 1)[0]
            self.assertTrue(path.startswith("docs/"), name)
            self.assertNotRegex(path, r"^docs/(plans|archive)/", name)
            self.assertTrue((ROOT / path).is_file(), target)
            self.assertTrue((README_ASSETS / f"badge-{name}.svg").is_file(), name)
        lines = [ln for ln in read("en").splitlines() if "docs/assets/readme/badge-" in ln]
        for line in lines[:-1]:
            self.assertTrue(line.endswith("<br>"), "end each row but the last with <br>")

    def test_badges_keep_the_pill_style(self):
        for path in sorted(README_ASSETS.glob("badge-*.svg")):
            with self.subTest(badge=path.name):
                root = ET.fromstring(path.read_text(encoding="utf-8"))
                self.assertEqual("24", root.attrib["height"])
                pill = root.find(SVG + "rect")
                self.assertEqual("6", pill.attrib["rx"])
                self.assertEqual("#26211e", pill.attrib["fill"])
                self.assertEqual("#4a413a", pill.attrib["stroke"])
                labels = root.findall(f"{SVG}g/{SVG}text")
                self.assertEqual(["#b3a79b", "#ffffff"], [node.attrib["fill"] for node in labels])

    def test_superagent_is_spelled_superagent(self):
        for lang in READMES:
            with self.subTest(lang=lang):
                spellings = set(re.findall(r"(?i)super[ -]?agent", read(lang)))
                self.assertEqual({"SuperAgent"}, spellings)

    def test_local_links_and_pictures_exist(self):
        for lang in READMES:
            text = read(lang)
            targets = re.findall(r"\]\(([^)#]+)(?:#[^)]*)?\)", text) + re.findall(r'(?:src|srcset)="([^"]+)"', text)
            for target in targets:
                if re.match(r"https?://", target):
                    continue
                with self.subTest(lang=lang, target=target):
                    self.assertTrue((ROOT / target).exists(), target)

    def test_every_readme_asset_is_used(self):
        used = read("en") + read("zh-CN")
        for path in README_ASSETS.iterdir():
            if path.is_file():
                with self.subTest(asset=path.name):
                    self.assertIn(f"docs/assets/readme/{path.name}", used, "delete assets the README no longer shows")

    def test_banners_are_in_their_language(self):
        for lang, suffix in SUFFIX.items():
            for theme in THEMES:
                with self.subTest(lang=lang, theme=theme):
                    root = ET.fromstring((README_ASSETS / f"banner-{theme}{suffix}.svg").read_text(encoding="utf-8"))
                    self.assertEqual(lang, root.attrib["lang"])
                    text = " ".join("".join(node.itertext()) for tag in ("title", "text")
                                    for node in root.iter(SVG + tag))
                    self.assertIn("SuperAgent", text)
                    if lang == "en":
                        self.assertNotRegex(text, r"[一-鿿]")
                    else:
                        self.assertRegex(text, r"[一-鿿]")


if __name__ == "__main__":
    unittest.main()

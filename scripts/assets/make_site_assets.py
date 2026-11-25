#!/usr/bin/env python3
"""The website's generated assets (site/public, docs/operations/site.md "Home page"):
  python3 scripts/assets/make_site_assets.py                 # the headline font, after editing headline text
  python3 scripts/assets/make_site_assets.py --serif-font /path/NotoSerifCJKsc-Bold.otf

- fonts/persona-serif.woff: Noto Serif CJK SC Bold, subset to the characters the pages set in it (h1, h2 and .hello
  of the home page, the extensions page and the releases page),
  so the headlines look the same on every system for about 20 KB. The source font is not in the repository: pass it
  with --serif-font (an .otf, or a .ttc with --font-number), or let the script find a system copy.
"""
import argparse
import os
import sys
from html.parser import HTMLParser

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
SITE = os.path.join(ROOT, "site", "public")
# Chinese pages whose headlines use the subset. English headings use a system serif stack instead, so translating
# them never grows this CJK font or requires another font download.
PAGES = [os.path.join(SITE, "index.html"), os.path.join(SITE, "extensions", "index.html"),
         os.path.join(SITE, "releases", "index.html")]
# (path, font number) of system copies of Noto Serif CJK SC Bold, tried in order
SERIF_CANDIDATES = [
    ("/usr/share/fonts/opentype/noto/NotoSerifCJK-Bold.ttc", 2),
    (os.path.expanduser("~/Library/Fonts/NotoSerifCJKsc-Bold.otf"), 0),
    ("/Library/Fonts/NotoSerifCJKsc-Bold.otf", 0),
]
VOID = {"br", "img", "meta", "link", "input", "hr", "source", "path", "rect", "circle"}


class SerifText(HTMLParser):
    """Collects the text inside h1, h2 and elements with class "hello": everything the page sets in the serif."""

    def __init__(self):
        super().__init__()
        self.depth, self.text = 0, []

    def handle_starttag(self, tag, attrs):
        if tag in VOID:
            return
        classes = (dict(attrs).get("class") or "").split()
        if self.depth or tag in ("h1", "h2") or "hello" in classes:
            self.depth += 1

    def handle_startendtag(self, tag, attrs):
        pass

    def handle_endtag(self, tag):
        if tag not in VOID and self.depth:
            self.depth -= 1

    def handle_data(self, data):
        if self.depth:
            self.text.append(data)


def serif(font, number):
    from fontTools import subset
    if not font:
        font, number = next(((p, n) for p, n in SERIF_CANDIDATES if os.path.exists(p)), (None, 0))
    if not font or not os.path.exists(font):
        sys.exit("serif: Noto Serif CJK SC Bold not found; pass --serif-font (and --font-number for a .ttc)")
    grab = SerifText()
    for page in PAGES:
        with open(page, encoding="utf-8") as f:
            grab.feed(f.read())
    chars = set("".join(grab.text)) | set(" 。，、？！：；「」…·")
    text = "".join(sorted(c for c in chars if c == " " or not c.isspace()))
    out = os.path.join(SITE, "fonts", "persona-serif.woff")
    subset.main([font, f"--font-number={number}", f"--text={text}", "--flavor=woff", "--no-hinting",
                 "--layout-features=kern,palt,vpal,liga", f"--output-file={out}"])
    print(f"serif: {len(text)} characters -> {out} ({os.path.getsize(out)} bytes)")


def main():
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--serif-font", help="Noto Serif CJK SC Bold (.otf, or .ttc with --font-number)")
    ap.add_argument("--font-number", type=int, default=0, help="font index inside a .ttc")
    args = ap.parse_args()
    serif(args.serif_font, args.font_number)


if __name__ == "__main__":
    main()

#!/usr/bin/env python3
"""integrations/antigravity/mcp-proxy.mjs, the Antigravity MCP proxy that runs and is embedded in the node, is what
esbuild makes of its readable source, integrations/antigravity/src/mcp-proxy.mjs: an edit to one alone is caught here.
It needs the esbuild that ESBUILD names (the web app's, web/node_modules/.bin/esbuild, is the one it is built with)
and is skipped without one."""

import os
import subprocess
import unittest

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
SOURCE = os.path.join(ROOT, "integrations", "antigravity", "src", "mcp-proxy.mjs")
BUILT = os.path.join(ROOT, "integrations", "antigravity", "mcp-proxy.mjs")
# The esbuild that minified it (#12233); another version may write the same code differently.
VERSION = "0.25.12"
FLAGS = ["--minify-whitespace", "--minify-syntax", "--format=esm", "--platform=node"]


class AntigravityMCPProxyBuild(unittest.TestCase):
    @unittest.skipUnless(os.environ.get("ESBUILD"), "ESBUILD names no esbuild to build the proxy with")
    def test_the_built_proxy_is_its_source_minified(self):
        esbuild = os.environ["ESBUILD"]
        version = subprocess.run([esbuild, "--version"], capture_output=True, text=True, timeout=30).stdout.strip()
        r = subprocess.run([esbuild, SOURCE, *FLAGS], capture_output=True, timeout=60)
        self.assertEqual(r.returncode, 0, r.stderr.decode(errors="replace"))
        with open(BUILT, "rb") as built:
            self.assertEqual(
                r.stdout, built.read(),
                f"{os.path.relpath(BUILT, ROOT)} is not {os.path.relpath(SOURCE, ROOT)} minified by esbuild {version} "
                f"(it is built with {VERSION}): regenerate it with the command at the top of the source")

    def test_the_source_says_how_the_proxy_is_built(self):
        with open(SOURCE) as source:
            head = source.read(2000)
        self.assertIn("esbuild " + VERSION, head)
        for flag in FLAGS:
            self.assertIn(flag, head)


if __name__ == "__main__":
    unittest.main()

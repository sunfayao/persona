#!/usr/bin/env python3
"""Guards the words a new user reads while getting started (docs/assistant/onboarding.md 8.1 "Copy guard").

    python3 -m unittest -v test_onboarding_copy      (from scripts/test; CI runs every scripts/**/test_*.py)

The setup card, the main conversation's coded errors, the login page, adding a computer, the first steps of the model
wizard and the empty states must not ask a beginner to know hub.json, a token ("pairing code" is fine), jq, personad, TLS, a
node, herdr, a launcher, localhost or a ~/. path. SCOPES lists where those words are read, in every language the
web app has: its i18n catalogs and the hub's catalogs (the words it sends for the codes and the readiness). Text in an
Advanced / Details section may name these concepts: ALLOW, with a reason. What is known to be wrong and
has an owner fixing it is PENDING: it must still be wrong (a fixed one has to leave the list), and nothing new may be.
Standard library only.
"""
import glob
import json
import os
import re
import unittest

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))


# The words, each a pattern that does not fire inside a longer ASCII word (Chinese next to it is fine: "token" right after a Chinese word is
# caught, "https" is not TLS). Case does not matter. The ja catalogs spell some of them in katakana.
BANNED = [
    ("hub.json", r"hub\.json"),
    ("token", r"(?<![A-Za-z])tokens?(?![A-Za-z])|トークン"),
    ("jq", r"(?<![A-Za-z])jq(?![A-Za-z])"),
    ("personad", r"personad"),
    ("TLS", r"(?<![A-Za-z])tls(?![A-Za-z])"),
    ("node", r"(?<![A-Za-z])nodes?(?![A-Za-z])|ノード"),
    ("herdr", r"herdr"),
    ("launcher", r"launchers?|ランチャー"),
    ("localhost", r"localhost"),
    ("~/.", r"~/\."),
]
_BANNED = [(word, re.compile(p, re.I)) for word, p in BANNED]
# Named placeholders are not read: {token} in a catalog, %s style formats.
_PLACEHOLDER = re.compile(r"\{[A-Za-z_][A-Za-z0-9_]*\}|%(\d+\$)?[sd]|\\\([^)]*\)")


def banned_words(text):
    """The banned words text shows a reader, in BANNED's order."""
    shown = _PLACEHOLDER.sub(" ", text)
    return [word for word, rx in _BANNED if rx.search(shown)]


class Scope:
    """Where beginner words live: kind json (a catalog per language, {lang} in the path).
    keys: a regex on the key; None is all of it."""

    def __init__(self, sid, kind, path, keys=None, langs=("zh-Hans", "en", "ja")):
        self.sid, self.kind, self.path, self.keys, self.langs = sid, kind, path, keys, langs


WEB = "web/src/i18n/locales/{lang}/"
SERVER = "server/internal/i18n/locales/{lang}/"
SCOPES = [
    # Web: the setup card and the coded errors (docs/assistant/onboarding.md 4.4, 4.6), the login page (#3356), adding a computer
    # (4.2), the provider wizard (4.3), the empty states (4.5).
    Scope("web/assistant-readiness", "json", WEB + "assistant-readiness.json"),
    Scope("web/lib-readiness", "json", WEB + "lib-readiness.json"),
    Scope("web/lib-api-errors", "json", WEB + "lib-api-errors.json"),
    Scope("web/login", "json", WEB + "login.json"),
    Scope("web/settings-loginsteps", "json", WEB + "settings-loginsteps.json"),
    Scope("web/settings-connectdevice", "json", WEB + "settings-connectdevice.json"),
    Scope("web/lib-add-computer", "json", WEB + "lib-add-computer.json"),
    Scope("web/machines-addcomputer", "json", WEB + "machines-addcomputer.json"),
    Scope("web/lib-computer", "json", WEB + "lib-computer.json"),
    Scope("web/settings-providerwizard", "json", WEB + "settings-providerwizard.json"),
    Scope("web/lib-providers", "json", WEB + "lib-providers.json"),
    Scope("web/machines", "json", WEB + "machines.json"),
    Scope("web/agentspage", "json", WEB + "agentspage.json"),
    Scope("web/agents-agentsview", "json", WEB + "agents-agentsview.json"),
    Scope("web/inbox-inbox", "json", WEB + "inbox-inbox.json"),
    Scope("web/inbox-inboxlist", "json", WEB + "inbox-inboxlist.json"),
    Scope("web/home", "json", WEB + "home.json"),
    Scope("web/assistant-persona", "json", WEB + "assistant-persona.json"),
    Scope("web/personaassistant", "json", WEB + "personaassistant.json"),
    # Zero computers / zero sources (#4485, #4480): "New agent"'s empty "Computer" field, the inbox's sources, the composer's
    # "Not sent yet" line, Settings › General's line under "Runs on".
    Scope("web/newagent-no-computer", "json", WEB + "newagent.json", r"^no_computer_"),
    Scope("web/lib-inbox-sources", "json", WEB + "lib-inbox-sources.json"),
    Scope("web/chat-composer-blocked", "json", WEB + "chat-composer.json", r"^blocked_hint$"),
    Scope("web/settings-assistant-footer", "json", WEB + "settings.json", r"^assistant_footer"),
    # The hub's words: the coded refusals, the readiness steps, confirming a computer, pairing, the built-in presets and
    # the provider test's errors.
    Scope("server/apierr", "json", SERVER + "apierr-apierr.json"),
    Scope("server/assistant-readiness", "json", SERVER + "assistant-readiness.json"),
    Scope("server/hub-machine_trust", "json", SERVER + "hub-machine_trust.json"),
    Scope("server/hub-pairing", "json", SERVER + "hub-pairing.json"),
    Scope("server/assistant-models_presets", "json", SERVER + "assistant-models_presets.json"),
    Scope("server/assistant-models_probe", "json", SERVER + "assistant-models_probe.json"),
    # The assistant_offline refusal's text.
    Scope("server/hub-assistant_link", "json", SERVER + "hub-assistant_link.json"),
]

# (scope, key) → why an "Advanced" / "Details" section may say it.
ALLOW = {
    ("web/login", "master_placeholder"): "登录页「高级」里用 hub 的总钥匙登录（Login.tsx 的 advanced Disclosure），只有自己装 hub 的人用",
    ("web/settings-providerwizard", "auth_bearer_hint"): "选了「自定义」才出现的网关表单（connect 步），说的是网关认的环境变量名；内置厂商只粘 Key，不经过这一步",
}
# (scope, key) → who is fixing it (the issue or the design's work item). Each must still be wrong somewhere.
PENDING = {
}

def _json_strings(path):
    with open(path, encoding="utf-8") as f:
        data = json.load(f)
    return [(k, v) for k, v in data.items() if isinstance(v, str)]


def scope_strings(scope):
    """(lang, file, key, text) of every string the scope covers."""
    out = []
    for lang in scope.langs:
        if scope.kind == "json":
            for path in sorted(glob.glob(os.path.join(ROOT, scope.path.format(lang=lang)))):
                out += [(lang, path, k, v) for k, v in _json_strings(path)
                        if scope.keys is None or re.search(scope.keys, k)]
    return out


def violations():
    """(scope id, key, lang, words, file) of every string that shows a banned word."""
    out = []
    for scope in SCOPES:
        for lang, path, key, text in scope_strings(scope):
            words = banned_words(text)
            if words:
                out.append((scope.sid, key, lang, words, os.path.relpath(path, ROOT)))
    return out


class BannedWordsTest(unittest.TestCase):
    def test_detector(self):
        self.assertEqual(banned_words("用配对码登录"), [])
        self.assertEqual(banned_words("输入{token}"), [])  # a placeholder is not read
        self.assertEqual(banned_words("在 %1$s 上"), [])
        self.assertEqual(banned_words("打开 https://example.com"), [])
        self.assertEqual(banned_words("用token登录"), ["token"])
        self.assertEqual(banned_words("Paste the Tokens"), ["token"])
        self.assertEqual(banned_words("运行 personad hub pair"), ["personad"])
        self.assertEqual(banned_words("改 ~/.persona/hub.json"), ["hub.json", "~/."])
        self.assertEqual(banned_words("jq -r .token"), ["token", "jq"])
        self.assertEqual(banned_words("TLS 端口"), ["TLS"])
        self.assertEqual(banned_words("等 node 重连"), ["node"])
        self.assertEqual(banned_words("the nodes"), ["node"])
        self.assertEqual(banned_words("在 herdr 里"), ["herdr"])
        self.assertEqual(banned_words("cc_launcher"), ["launcher"])
        self.assertEqual(banned_words("http://localhost:18660"), ["localhost"])
        self.assertEqual(banned_words("anode, nodejs, jquery, tokenize"), [])
        self.assertEqual(banned_words("トークンを貼り付け"), ["token"])
        self.assertEqual(banned_words("ノードを更新"), ["node"])
        self.assertEqual(banned_words("ランチャーを選ぶ"), ["launcher"])


class OnboardingCopyTest(unittest.TestCase):
    def test_every_scope_reads_something(self):
        # A file renamed or a key prefix gone would otherwise make its scope pass by reading nothing.
        for scope in SCOPES:
            got = scope_strings(scope)
            self.assertTrue(got, "%s: no strings at %s (keys %r): fix SCOPES" % (scope.sid, scope.path, scope.keys))
            langs = {lang for lang, _, _, _ in got}
            self.assertEqual(langs, set(scope.langs), "%s: missing languages %s" % (scope.sid, set(scope.langs) - langs))

    def test_no_banned_words(self):
        bad = [v for v in violations() if (v[0], v[1]) not in ALLOW and (v[0], v[1]) not in PENDING]
        self.assertFalse(bad, "beginner-facing words a new user cannot know (docs/assistant/onboarding.md 8.1); say it plainly, or "
                              "if it is in a 高级 / 详情 section add it to ALLOW with the reason:\n" +
                         "\n".join("  %s %s [%s] %s: %s" % (f, key, lang, sid, ", ".join(w)) for sid, key, lang, w, f in bad))

    def test_pending_are_still_wrong(self):
        wrong = {(sid, key) for sid, key, _, _, _ in violations()}
        fixed = [k for k in PENDING if k not in wrong]
        self.assertFalse(fixed, "fixed (or gone): take these out of PENDING in scripts/test/test_onboarding_copy.py:\n" +
                         "\n".join("  %s %s" % k for k in fixed))

    def test_allow_entries_exist(self):
        keys = {(scope.sid, key) for scope in SCOPES for _, _, key, _ in scope_strings(scope)}
        stale = [k for k in ALLOW if k not in keys]
        self.assertFalse(stale, "ALLOW entries with no such string any more:\n" + "\n".join("  %s %s" % k for k in stale))
        self.assertFalse(set(ALLOW) & set(PENDING), "an entry is both allowed and pending")
        self.assertTrue(all(reason.strip() for reason in list(ALLOW.values()) + list(PENDING.values())))


if __name__ == "__main__":
    unittest.main()

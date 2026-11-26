#!/usr/bin/env python3
"""Reads the personal build / deploy config (docs/operations/config.md) so no script hard-codes one person's setup.

The config lives outside the repository so clean worktrees (releases) see it too:
  $PERSONA_CONFIG, else ~/.config/persona/config.json   (template: config.example.json)

  python3 scripts/persona_config.py env              # shell exports: eval "$(python3 scripts/persona_config.py env)"
  python3 scripts/persona_config.py get <dotted.key>  # one value, e.g. hub.url
"""
import json
import os
import re
import shlex
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, os.path.join(ROOT, "scripts"))
from persona_identity import config_path, environment_aliases, LEGACY_PREFIX


def path() -> str:
    return config_path()


def load() -> dict:
    p = path()
    if not os.path.exists(p):
        sys.exit(f"missing {p}\nCopy config.example.json there and fill it in (docs/operations/config.md).")
    with open(p) as f:
        cfg = json.load(f)
    return cfg


def get(cfg: dict, key: str, default=None):
    cur = cfg
    for part in key.split("."):
        if not isinstance(cur, dict) or part not in cur:
            return default
        cur = cur[part]
    return cur


def machine(cfg: dict, mid: str) -> dict:
    for m in cfg.get("machines", []):
        if m.get("id") == mid:
            return m
    sys.exit(f"machine {mid!r} is not in {path()}")


def env(cfg: dict) -> dict:
    hub = cfg.get("hub", {})
    hub_ssh = machine(cfg, hub["machine"]).get("ssh", hub["machine"]) if hub.get("machine") else ""
    out = {
        "PERSONA_HUB_MACHINE": hub.get("machine", ""),
        "PERSONA_HUB_SSH": hub_ssh,
        "PERSONA_HUB_URL": hub.get("url", ""),
        "PERSONA_MACHINES": " ".join(m["id"] for m in cfg.get("machines", [])),
        # Legacy setting for a privately hosted update source.
        "PERSONA_PUBLISH_URL": get(cfg, "publish.site_url", "").rstrip("/"),
        # Where the personad built here looks for its updates (-X main.updateURL): an existing environment override,
        # then publish.update_url, else <site_url>/dl.
        "PERSONA_UPDATE_URL": update_url(cfg, os.environ.get("PERSONA_UPDATE_URL")),
    }
    for key, default in list(out.items()):
        if key.startswith("PERSONA_"):
            out[LEGACY_PREFIX + key[8:]] = default
    return out


def update_url(cfg: dict, override=None) -> str:
    """The update source built into personad (-X main.updateURL, docs/operations/hub-update.md "Update source"):
    a non-empty environment override, then publish.update_url as given (the directory latest.json is in, e.g. a GitHub
    repository's releases/latest/download), else publish.site_url's dl/, else none. hub.json update.url still replaces it."""
    value = override or get(cfg, "publish.update_url", "")
    if value in ("", None):
        site = get(cfg, "publish.site_url", "")
        return site.rstrip("/") + "/dl" if isinstance(site, str) and site.strip() else ""
    if (not isinstance(value, str) or not re.fullmatch(r"(https://[^/\s?#@]+|file://)(/[^\s?#]*)?", value.strip())
            or value.strip().rstrip("/").endswith("/latest.json")):
        sys.exit("publish.update_url must be an https:// (or file:///) directory without a user, query or fragment: "
                 "the one latest.json is in (docs/operations/config.md)")
    return value.strip().rstrip("/")


def main():
    args = sys.argv[1:]
    cfg = load()
    if args[:1] == ["env"]:
        generated = env(cfg)
        for k, v in generated.items():
            print(f"export {k}={shlex.quote(v)}")
        print(environment_aliases(exclude=generated))
    elif args[:1] == ["get"] and len(args) == 2:
        v = get(cfg, args[1])
        if v is None:
            sys.exit(f"{args[1]} is not set in {path()}")
        print(v if isinstance(v, str) else json.dumps(v))
    elif args[:1] == ["machine"] and len(args) == 2:
        print(json.dumps(machine(cfg, args[1])))
    else:
        sys.exit(__doc__)


if __name__ == "__main__":
    main()

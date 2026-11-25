#!/usr/bin/env python3
"""Prepare private CI release configuration from protected environment values.

Only an explicitly authorized release job calls this. Ordinary release-PR builds
use public configuration and never receive private key material. Files stay in a
private temporary directory and are not included in release artifacts or logs.
"""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import sys


class ConfigurationError(ValueError):
    pass


def required(name, env):
    value = env.get(name, "")
    if not isinstance(value, str) or not value.strip():
        raise ConfigurationError("protected release environment is missing " + name)
    return value


def write_private(path, data, mode=0o600):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    path.write_bytes(data)
    path.chmod(mode)


def prepare_public(directory, env):
    repository = required("GITHUB_REPOSITORY", env)
    config = {
        "publish": {
            "site_url": "https://github.com/" + repository,
            "update_url": "https://github.com/" + repository + "/releases/latest/download",
        },
    }
    path = directory / "public-config.json"
    write_private(path, (json.dumps(config, indent=2) + "\n").encode())
    return path


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("platform", choices=("public",))
    parser.add_argument("--directory", type=Path, required=True)
    args = parser.parse_args(argv)
    directory = args.directory.resolve()
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    directory.chmod(0o700)
    if directory.stat().st_mode & 0o077:
        raise ConfigurationError("private release configuration directory must not be accessible by others")
    if directory.is_symlink():
        raise ConfigurationError("private release configuration directory must not be a symlink")
    if args.platform == "public":
        print(prepare_public(directory, os.environ))
    else:
        raise ConfigurationError("unknown release configuration platform")


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (ConfigurationError, OSError, ValueError) as error:
        print("Protected release configuration failed: " + str(error), file=sys.stderr)
        sys.exit(1)

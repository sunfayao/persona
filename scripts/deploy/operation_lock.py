#!/usr/bin/env python3
"""Serialize deployments, including ones launched from another computer."""
import argparse
import fcntl
import os
from pathlib import Path
import select
import shlex
import socket
import signal
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "scripts"))
from persona_config import load  # noqa: E402
from persona_identity import data_dir, service_environment  # noqa: E402

# Keep the existing lock path so every checkout serializes against the same file.
REMOTE = r'''
import fcntl, os, pathlib, sys
root=pathlib.Path(os.environ.get('PERSONA_HOME',str(pathlib.Path.home()/'.persona'))).expanduser()
p=root/'releases/hub/.publish.lock'
p.parent.mkdir(parents=True, exist_ok=True)
f=open(p,'a')
try: fcntl.flock(f,fcntl.LOCK_EX|fcntl.LOCK_NB)
except BlockingIOError: print('BUSY',flush=True); sys.exit(75)
print('READY',flush=True)
sys.stdin.buffer.read()
'''


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    command = args.command
    if command[:1] == ["--"]:
        command = command[1:]
    if not command:
        parser.error("a command is required")
    if os.environ.get("PERSONA_OPERATION_DEPLOY_LOCKED") == "1":
        return subprocess.call(command)
    env = os.environ.copy()
    env["PERSONA_OPERATION_DEPLOY_LOCKED"] = "1"
    remote = None
    stream = None
    try:
        cfg = load()
        hub = cfg["hub"]["machine"]
        machine = next(m for m in cfg["machines"] if m["id"] == hub)
        names = set(machine.get("hostnames", []))
        hostname = socket.gethostname()
        if not names.intersection((hostname, hostname.split(".")[0])):
            host = machine.get("ssh", hub)
            prefix = subprocess.check_output(
                [sys.executable, str(ROOT / "scripts/persona_identity.py"),
                 "--service", "hub", "--prepare", "--prefix", "-"], text=True)
            remote = subprocess.Popen(
                ["ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=15", host,
                 prefix + "\npython3 -u -c " + shlex.quote(REMOTE)],
                stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True)
            if not select.select([remote.stdout], [], [], 30)[0]:
                raise RuntimeError("timed out acquiring the hub operation lock")
            answer = remote.stdout.readline().strip()
            if answer == "BUSY":
                print("operation lock busy: deploy", flush=True)
                return 75
            if answer != "READY":
                raise RuntimeError("could not acquire the hub operation lock")
        else:
            root = Path(data_dir(environment=service_environment(role="hub")))
            path = root / "releases/hub/.publish.lock"
            path.parent.mkdir(parents=True, exist_ok=True)
            stream = path.open("a")
            try:
                fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                print("operation lock busy: deploy", flush=True)
                return 75
        print("operation lock acquired: deploy", flush=True)
        child = subprocess.Popen(command, env=env, start_new_session=True)

        def stop_child(signum, frame):
            if child.poll() is None:
                os.killpg(child.pid, signal.SIGTERM)
                try:
                    child.wait(timeout=3)
                except subprocess.TimeoutExpired:
                    os.killpg(child.pid, signal.SIGKILL)
                    child.wait()
            raise SystemExit(128 + signum)

        signal.signal(signal.SIGTERM, stop_child)
        signal.signal(signal.SIGINT, stop_child)
        signal.signal(signal.SIGHUP, stop_child)
        while True:
            try:
                result = child.wait(timeout=1)
                break
            except subprocess.TimeoutExpired:
                if remote and remote.poll() is not None:
                    stop_child(signal.SIGTERM, None)
        print(f"operation lock released: deploy result={result}", flush=True)
        return result
    finally:
        if remote:
            remote.stdin.close()
            try:
                remote.wait(timeout=5)
            except subprocess.TimeoutExpired:
                remote.kill()
                remote.wait()
        if stream:
            stream.close()


if __name__ == "__main__":
    sys.exit(main())

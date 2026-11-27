"""Deployment compatibility; every process uses synthetic homes and stub services."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

from test_persona_script_identity import identity, executable, environment

ROOT = Path(__file__).resolve().parents[2]
FORMER = "jar" + "vis"
PREFIX = FORMER.upper() + "_"


class DeploymentRollingCompatibilityTests(unittest.TestCase):
    def test_environment_aliases_preserve_precedence_empty_values_and_do_not_print_secrets(self):
        values = {PREFIX + "HOME": "old", "PERSONA_HOME": "", PREFIX + "DB_PASSWORD": "private-fixture-value"}
        output = identity.environment_aliases(environment=values)
        self.assertNotIn("private-fixture-value", output)
        env = dict(values, PATH=os.environ["PATH"])
        result = subprocess.run(["sh", "-c", output + '\nprintf "%s|%s|%s" "$PERSONA_HOME" "$' + PREFIX + 'HOME" "$PERSONA_DB_PASSWORD"'],
                                env=env, capture_output=True, text=True, timeout=5)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "||private-fixture-value")


    def test_former_hub_socket_and_envfile_are_reused_not_recreated(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            state = home / "hub profile"
            (state / "bin").mkdir(parents=True)
            executable(state / "bin" / (FORMER + "d"), "#!/bin/sh\nexit 3\n")
            (state / "hub.json").write_text('{"listen":":18660"}')
            unit = home / ".config/systemd/user" / (FORMER + "d.service")
            unit.parent.mkdir(parents=True)
            file = home / "root.env"
            file.write_text(PREFIX + 'HOME="' + str(state) + '"\nSECRET=private-fixture-value\n')
            original = '[Service]\nEnvironmentFile=' + str(file) + '\nEnvironment="' + PREFIX + 'HUB_EDGE=0"\n'
            unit.write_text(original)
            unit.with_suffix('.socket').write_text('[Socket]\nListenStream=18660\nService=' + FORMER + 'd.service\n')
            commands = home / "commands"
            commands.mkdir()
            executable(commands / "systemctl", '#!/bin/sh\nprintf "%s\\n" "$*" >> "$HOME/calls"\nexit 0\n')
            result = subprocess.run(["bash", str(ROOT / "scripts/deploy/hub-systemd.sh")],
                                    env=environment(home, PATH=str(commands) + ":" + os.environ["PATH"]),
                                    capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(unit.read_text(), original)
            self.assertFalse(unit.with_name('personad.service').exists())
            self.assertFalse(unit.with_name('personad.socket').exists())
            self.assertIn('Service=' + FORMER + 'd.service', unit.with_suffix('.socket').read_text())
            self.assertNotIn('private-fixture-value', result.stdout + result.stderr)
            self.assertNotIn('personad.socket', (home / 'calls').read_text())

    def test_command_aliases_do_not_loop(self):
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            executable(directory / "personad", "#!/bin/sh\nexit 0\n")
            identity.command_aliases(directory)
            identity.command_aliases(directory)
            self.assertEqual(os.readlink(directory / (FORMER + "d")), "personad")
            self.assertTrue(os.path.samefile(directory / "personad", directory / (FORMER + "d")))
    def test_legacy_command_layout_upgrades_without_alias_loops(self):
        for name in ("personad", "persona-link", "personad-wechat", "personad-telegram"):
            with self.subTest(name=name), tempfile.TemporaryDirectory() as temporary:
                directory = Path(temporary)
                legacy = name.replace("personad", FORMER + "d").replace("persona-link", FORMER + "-link")
                old, current = directory / legacy, directory / name
                executable(old, "#!/bin/sh\nexit 0\n")
                current.symlink_to(legacy)
                identity.command_aliases(directory)
                self.assertFalse(old.is_symlink())
                self.assertTrue(current.samefile(old))
                new = directory / (name + ".new")
                executable(new, "#!/bin/sh\nprintf new\n")
                current.unlink()
                new.replace(current)
                identity.command_aliases(directory)
                self.assertFalse(current.is_symlink())
                self.assertEqual(os.readlink(old), name)
                self.assertTrue(old.samefile(current))
                inode = old.lstat().st_ino
                identity.command_aliases(directory)
                self.assertEqual(old.lstat().st_ino, inode)

    def test_install_program_replaces_old_dangling_and_looped_symlinks(self):
        source = (ROOT / "scripts/deploy/deploy.sh").read_text()
        function = source.split("install_program() {", 1)[1].split("\n}\n", 1)[0]
        function = "install_program() {" + function + "\n}\n"
        for mode in ("local", "remote"):
            for layout in ("old", "dangling", "loop"):
                with self.subTest(mode=mode, layout=layout), tempfile.TemporaryDirectory() as temporary:
                    home = Path(temporary)
                    work = home / "server"
                    work.mkdir()
                    (work / "bin").mkdir()
                    scripts = home / "scripts"
                    scripts.mkdir()
                    (scripts / "persona_identity.py").write_bytes((ROOT / "scripts/persona_identity.py").read_bytes())
                    state = home / "state with spaces"
                    binaries = state / "bin"
                    binaries.mkdir(parents=True)
                    current = binaries / "personad-examplechat"
                    old = binaries / (FORMER + "d-examplechat")
                    if layout == "old":
                        executable(old, "#!/bin/sh\nprintf old\n")
                    elif layout == "loop":
                        old.symlink_to(current.name)
                    current.symlink_to(old.name)
                    script = r'''set -eu
LDFLAGS=fixture
TAGS=fixture
PERSONA_HUB_SSH=unused
is_here() { [ "$MODE" = local ]; }
state_root() { printf '%s' "$PERSONA_HOME"; }
go() { printf '#!/bin/sh\nprintf new\n' > bin/personad-examplechat-linux-amd64; chmod 755 bin/personad-examplechat-linux-amd64; }
persona_alias_binaries() { python3 ../scripts/persona_identity.py --alias-binaries "$1"; }
remote_state() { eval "$2"; }
''' + function + 'install_program fixture linux-amd64 personad-examplechat\n'
                    env = environment(home)
                    env.update(MODE=mode, PERSONA_HOME=str(state))
                    result = subprocess.run(["bash", "--noprofile", "--norc"], input=script,
                                            cwd=work, env=env, text=True, capture_output=True, timeout=10)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertFalse(current.is_symlink())
                    self.assertIn("printf new", current.read_text())
                    self.assertEqual(os.readlink(old), current.name)
                    self.assertTrue(old.samefile(current))

if __name__ == "__main__":
    unittest.main()

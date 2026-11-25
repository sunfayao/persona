# Install Persona Link on Mac

Persona Link connects this Mac to your Persona hub so the hub can use Agents running here. It is a background connector and resident-node service, not a menu bar app. See [link.md](link.md) for connection modes and protocol.

## Install

On the Mac, run the hub's installer in node mode (see [link.md](link.md), "Install the node service"):

```bash
curl -fsSL <site>/install.sh | sh -s -- --node
```

It downloads persona-link and persona-node to `~/.persona/bin/` and runs `persona-link install`, which registers two independent launchd agents:

- `com.persona.link` runs the connector copied to `~/.persona/bin/persona-link`.
- `com.persona.node` runs the resident node. An existing node service is preserved, including its program and data directory.

Both start after installation and at login, and restart on exit. There is no UI to open. On first LAN use, macOS may ask for local-network permission; allow it if this Mac connects to the hub over the LAN.

## Pairing

1. Open **Machines › Add computer** in the signed-in web app, choose Mac, name the computer and copy its pairing link.
2. Pair in a terminal:

   ```bash
   ~/.persona/bin/persona-link pair - <<< '<pairing-link>'
   ```

3. Return to the web screen and confirm **This is my computer**. A link is valid for 10 minutes and can be used once; never post it publicly.
4. Verify with `~/.persona/bin/persona-link status`. The command reports the connector's mode, connection, resident-node state as seen by the hub, and the launchd agent. `status --json` is available for scripts.

For passive mode, use `persona-link pair --listen` and paste the printed link into hub admin **Computers › Add computer › Hub connects**. SSH tunnel setup and other advanced options remain in the CLI; see [link.md](link.md), "SSH tunnel".

## Everyday commands

Human-readable command output follows the terminal locale (`PERSONA_LANG`, then `LC_ALL`, `LC_MESSAGES` and `LANG`), with English as the fallback and `en`, `zh-Hans` and `ja` shipped together. JSON, saved status and peer messages keep their canonical form. The Electron setup window and `scripts/deploy/install.sh` do not currently forward their independently selected interface language as `PERSONA_LANG`; set it explicitly on a Link invocation when its output needs a particular language.

| Command | Purpose |
| --- | --- |
| `persona-link status [--json]` | Connector and service state; no credential contents |
| `persona-link reconnect` | Ask the running connector to reconnect immediately |
| `persona-link node status [--json]` | Resident-node service state without connecting to its socket |
| `persona-link logs -f` | Follow `~/.persona/link.log` |
| `persona-link pair --listen` | Generate a passive-mode pairing link |
| `persona-link name <new>` | Rename this computer on the hub while connected |
| `persona-link plugin status\|install\|uninstall\|on\|off` | The Persona plugin for Claude Code, which the resident node keeps installed for this Mac's Claude Code and Code sessions ([link.md](link.md) "Claude Code plugin") |

## Upgrade and uninstall

Upgrade by running the installer again. The connector executable is replaced at its stable path and its launchd agent is restarted. Existing pairing and node-service files stay. Reinstalling Link does **not** replace an already installed resident-node binary; see [node-update.md](node-update.md).

To remove the connector while keeping its pairing and resident node:

```bash
~/.persona/bin/persona-link uninstall
```

To take the Persona plugin out of Claude Code too, run `~/.persona/bin/persona-link plugin uninstall` while the node is still installed. To remove the resident node too, inspect its actual service and binary first, then run `persona-link node uninstall`. `--purge` is a separate, explicit destructive operation that removes connector credentials, status, logs and its installed executable; revoke this computer on the hub as well. Persona Screen can then be removed with `/Applications/Persona Link.app` if no longer needed. Do not run those removal commands merely to troubleshoot a connection.

## Troubleshooting

- **Connector not running**: `launchctl print gui/$(id -u)/com.persona.link` and `persona-link logs -f`.
- **Hub cannot reach the local node**: `persona-link node status`; logs are in `~/.persona/node-attach.log`.
- **LAN unreachable**: check macOS local-network permission for the connector. Other routes and TLS errors are explained by `persona-link status`.

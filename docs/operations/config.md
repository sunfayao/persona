# Configuration

The repository hard-codes no machine, account, domain or preference. Everything personal lives in the configuration files below, and code and scripts only read them:

| Config | Location | Read by | Covers |
| --- | --- | --- | --- |
| Build / deploy config | `~/.config/persona/config.json` on the dev machine (point elsewhere with `PERSONA_CONFIG`), template `config.example.json` at the repo root | `scripts/*` (via `scripts/persona_config.py`) | which machine / address the hub is on, machines to deploy to, publish settings |
| hub runtime config | `~/.persona/hub.json` on the hub machine (0600, contains the token and database password; written by `personad hub setup`, or generated automatically the first time the hub starts; `hub pair`, `hub peer`, `hub manager-code` and `hub assistant-token` only talk to a running hub: they need this file and never create it) | `personad hub` | Listen address, public address, which machines to connect, push timings, whether the hub starts the built-in manager |
| Link config | `~/.persona/link.json` on each computer with Persona Link (0600, contains its credentials; written by `persona-link pair`, `ssh`, `proxy` and the running link) | `persona-link` | Active and passive pairings, hub addresses, the optional SSH tunnel and outbound proxy (`proxy`: a URL or `"off"`) |
| Manager config | `~/.persona/managers/assistant/manager.json` for the built-in manager, `~/.persona/managers/<id>/manager.json` for one made in the app (0600) | the manager (`personad assistant`, `personad manager run`) | Whose assistant it is and what it should know, where it runs by default, the AI gateway and models, the Agents' triage, question delegation and launcher health, the Inbox and its AI twin, how long hook cards wait; connectors, extensions written by hand, bots, mail sign-ins and the Gmail proxy, Lark and lark-cli, usage sources ("manager.json" below) |

Keep machine configuration outside the checkout.
`CLAUDE.local.md` is in `.gitignore`.

## Who owns a setting

One setting has one writer. When two places seem to hold the same thing, this is who wins:

| Layer | Examples | Who writes it | Role |
| --- | --- | --- | --- |
| Build / deploy `config.json` | deploy machines, publish | you, once per environment | Scripts only. The hub, manager and node never read it |
| `hub.json` | listen, public/security, database, machines and their per-computer defaults | you / the installer | Hub runtime configuration and **defaults**. Read at hub start |
| `manager.json` | assistant identity, connectors, `agents`, `ai`, `inbox`, `usage` | you / the installer | The manager's own configuration and **defaults**. The hub reads none of these sections; a copy left in `hub.json` is ignored |
| Postgres settings (hub `hub_settings`, manager `settings`) | model configuration, launchers, inbox rules, extension settings, board and triage switches | the apps, the assistant, or a one-time migration | **User preferences. They win over the file defaults** |
| Per-device `devices.prefs` | quiet hours, translation scope, notification switches | that device | Per-device only; not a settings key and not synced as one |
| `PERSONA_*` environment | `DATABASE_URL`, `PUBLIC_URL`, `LISTEN`, `NODE_PACKAGES`, `ADMIN_ACCESS` | the deployment | Overrides the matching `hub.json` field, never written back. Each is also read under its former `JARVIS_` prefix (`JARVIS_DATABASE_URL`, …) so that a part of an older version keeps working during a rolling upgrade (`server/internal/compat`, `scripts/persona_identity.py`); `PERSONA_HOME` and `PERSONA_CONFIG` are the only brand-prefixed overrides of the state directory and the deploy config |
| Node-local third-party files | Claude / Codex / Antigravity / OMP `settings.json` | the user or those tools | Read by the node on that computer; the Agents settings page edits the allow-listed subset |
| Node-local `node-signing.json` | the certificate a Mac's resident node signs its builds with | you, on that Mac | Read by the resident node at start; never sent to the hub (`docs/operations/mac-node-signing.md`) |

Consequences worth remembering:

- **The database wins.** A value saved in an app is not written back to `hub.json` or `manager.json`, and editing the file afterwards does not override it. This covers model configuration, launcher and provider settings, inbox rules, per-machine switches such as `botmux` / `herdr_autostart`, and `auto_update_nodes`.
- **Files are defaults, not the current value.** `ai`, `inbox.triage_ai`, `inbox.judge_ai`, `assistant.launcher` and `machines[].launchers` are the baseline the model configuration inherits from, not a second place to change the model.
- **Scope is explicit, not inferred**: per-computer (a machine's own settings), per-manager (Model Settings, Assistant, Knowledge pick one), per-device (notification and translation preferences). A setting that looks the same in two places — the assistant time zone, a device's quiet-hours time zone, the browser's zone — is not one setting.

## Build / deploy config (config.json)

```json
{
  "hub": { "machine": "server", "url": "http://hub.example.com:18660" },
  "machines": [
    { "id": "server", "ssh": "server", "os": "linux" },
    { "id": "laptop", "ssh": "laptop", "os": "darwin", "hostnames": ["MyMacBook"] }
  ]
}
```

| Field | Purpose |
| --- | --- |
| `hub.machine` | Which machine the hub runs on (an id in `machines`): `deploy.sh` starts Postgres and the systemd service on it |
| `hub.url` | The hub's address (exported as `PERSONA_HUB_URL`); the HTTP in the example is only for a private network / VPN; configure the public entry separately in `public` of `hub.json` |
| `machines[]` | Machines where `deploy.sh` installs `personad`: `ssh` is the ssh host name; `os` (`linux` / `darwin`) and `arch` (`amd64` / `arm64`, optional) decide which binary to install, and whichever is missing is asked from the machine itself (`uname`); `hostnames` are the machine's own host names (when the deploy runs on it, it copies directly without ssh; the machine running the deploy must list them, and any machine without them goes through ssh); `via` is another machine's id: when the dev machine can't reach this one, it ssh-es once more through that machine to transfer the binary (leaving no files there), and `ssh` is then the host name on that machine |

`python3 scripts/persona_config.py env` prints shell variables (scripts `eval` it at the top); `get <dotted.key>` prints one value.

## Multiple Codex app-server instances

Each machine in `hub.json` can configure `codex_homes` (an array of directories), `codex_servers` (`id`, `name`, `home`, `endpoint`), and optionally `codex_discovery` (automatic discovery by default; false uses only registered sources). For example:

```json
{
  "codex_homes": ["~/.codex-profile"],
  "codex_servers": [
    {"id": "desktop", "name": "Desktop instance", "home": "~/.codex-profile", "endpoint": "unix://<absolute socket path>"}
  ]
}
```

No application name needs hard-coding: every 30 seconds the node discovers the current user's Codex/app-server processes, `CODEX_HOME`, and enabled listen endpoints; existing official control sockets under known homes can also be discovered. Discovered directories and named instances are remembered in the hub's Postgres settings; process/connection generations are not persisted. Private paths belong only in runtime config; do not commit actual config values.

The Unix socket and its immediate parent directory must belong to the current user and be inaccessible to groups/other users; WebSocket runs inside. TCP endpoints allow only literal loopback IPs, disallow external hosts, userinfo, and query credentials, and do not use the system HTTP proxy. Stdio without an endpoint is read-only; no daemon is started, no desktop app replaced, and no file permissions loosened.

## hub runtime config (hub.json)

The sections that are a manager's (whose assistant it is, `ai`, `agents`, `inbox`, `hook_timeout_minutes`, connectors, extensions written by hand, bots…) are in its `manager.json` (next section): the hub reads none of them, and says once at start which ones its `hub.json` still has.

```json
{
  "listen": ":18660",
  "public_url": "http://hub.example.com:18660",
  "public": {
    "tls_listen": ":18662",
    "advertise": ["https://hub.example.com:18662"],
    "advertise_ipv6": true,
    "advertise_lan": true
  },
  "security": { "allow_public_plaintext": false, "trusted_cidrs": [] },
  "name": "Home",
  "admin": { "access": "lan" },
  "token": "…",
  "database_url": "…",
  "machines": [
    { "id": "server", "name": "Server", "kind": "local", "icon": "server" },
    { "id": "laptop", "name": "Laptop", "kind": "ssh", "ssh_host": "laptop", "icon": "laptop" }
  ]
}
```

- First run writes only the local machine (`kind: local`, id is the host name); the generated `public_url` is the HTTP address for that host name (a private-network endpoint). TLS is off by default; the example explicitly enables `:18662` (an example port, changeable). Configure other machines and domains yourself.
- `listen` is the retained HTTP listener, default `:18660`; `public_url` goes into the QR code's `url` and the candidate list.
- For each `public` and `security` setting, its defaults, and how to allow traffic, see network configuration. Candidate addresses are sent only after authentication.
- `security.proxy_protocol` (optional, omitted by default): with same-machine port forwarding, a reverse tunnel, or nginx `stream` passthrough in front of the hub, public connections all look like loopback to the hub.
  Set `{"from": ["127.0.0.1/32"]}`: connections from these upstreams must carry a PROXY header (v1 / v2), and the hub checks the real source in the header; `listeners` defaults to only `tls`.
  Syntax, rules, and self-checks.
- `name` (optional): the hub's display name (e.g. "Home"), reported by persona-link and shown by clients.
  Defaults to the short host name of the hub machine; if the host name is a Docker container id or `PERSONA_LOCAL_NODE=0`, it uses `hub-<first 6 chars of hub_id>`. Manual file edits require a hub restart;
  edits in the admin UI's "Overview" take effect immediately.
- `initial_locale` (optional, `en` / `zh-Hans` / `ja`): the user language until a device reports one (`docs/protocol/i18n.md`, "User language").
  A new install's `personad hub setup` writes the language the installer ran in (`--lang`, else `PERSONA_INSTALL_LANG`);
  setup never changes it in an existing file. Once a device has reported a language (Postgres `user_locale`), this field no longer matters;
  omitted or unknown means `zh-Hans`. Edits take effect at the next hub start.
- `admin` (optional): hub admin UI (`<hub address>/admin`). `access` controls who can open it: `loopback` / `lan` (default) / `cidrs` / `any`
  (requires TLS); `cidrs` lists networks allowed with `access: cidrs`; `session_hours` is the login expiry (default 12); `first_setup: "code"` (written by a fresh `personad hub setup` install) requires a setup code even from LAN sources to set the first password; without it only loopback requires one. Access can only be broadened on the hub machine with
  `personad hub admin access <scope>` (or the deploy script); the web UI can only narrow it. Env vars `PERSONA_ADMIN_ACCESS` / `PERSONA_ADMIN_CIDRS` take precedence.
  The admin UI writes hub.json atomically and preserves unknown fields.
- `mesh` (optional, linked hubs): `discovery` (default true: advertise itself with mDNS on the LAN and list discovered hubs; unavailable inside a Docker bridge network),
  `path_switch_seconds` (default 60: when a computer has a better path, it must stay better this long before the primary switches; broken paths switch immediately),
  `plaintext` (off by default: `allow` lists `*`, networks / addresses, `192.0.2.*`, and host names / `*.example.com` used only for dialing that may pair and link without TLS; `advertise` lists `http://` addresses other hubs use to connect to this one in plaintext; no encryption or pinning). Peers, the group's primary, and its term live in
  Postgres, not here.
- `link` (optional, computer-link heartbeat): `ping_seconds` (ping interval, default 10), `ping_timeout_seconds` (time without pong before disconnecting, default 5).
  Omitted or 0 uses the default; setting only one leaves the other at its default. Range 1–600 seconds, capped at 600. Controls the hub's pings on Persona Link connections and streams, and the manager's pings on connector (kind service) streams.
  When set, the manager also tells Persona Link (in `endpoints` for active mode, in stream-opening request headers for passive mode), and the computer pings accordingly; hub-to-hub links are unaffected and remain 10 seconds / 5 seconds.
  On slow links (only tens of KB/s between hub and computer), pings queue behind data being written (a 1 MiB chunk at 30 KB/s takes over 30 seconds); a timeout shorter than writing one chunk always causes a false disconnect. Set
  `ping_timeout_seconds` longer (e.g. `{"ping_seconds": 30, "ping_timeout_seconds": 90}`); a dead link then takes roughly "interval + timeout" to detect. The network library's ping send and pong reply
  themselves wait at most 5 seconds, and forwarding a message writes for at most 1 minute; neither is configurable.
  Older Persona Link versions do not recognize these settings and still ping at their own 10 seconds / 5 seconds (active mode only pings the connection itself; passive mode also pings every stream); upgrade both together. Manual file edits require a hub restart.
- `machines`: these are defaults handed by the hub to the manager (the assistant process) at handshake, and **the manager connects them**. Where the text below says "the hub dials" or "the hub connects",
  the manager actually dials (on the hub machine it is the hub's child process using the same HOME: ssh config and its keys in `~/.persona/managers/assistant/` are all this machine's). Computers added in the admin UI at runtime are also handed to it and connected immediately.
- `machines[].kind`: `local` (the hub's own machine), `service` (the hub dials this connector's passive port, "Hub connects", see the next item), `link` (this machine's persona-link
  actively connects to the hub, "Computer connects"); `ssh` (the hub connects via `ssh_host`) and `tailcat` (tailcat only, see the item after next) exist for configs that already have them and are not added through the admin UI. One computer may have several entries (e.g. a service and a link); the hub recognizes
  the same computer by machine_uid, lists it once, and uses the best path.
- `machines[].trust` (optional): `"work"` prevents that computer from reading personal data through the manager. Omitted or `"personal"` keeps the existing access; other values are rejected when loading hub.json.
  Work computers cannot list or execute phone tools, connector tools (including mail), or assistant tools, and cannot read the assistant's live context or model configuration. `personad notify`, `personad ask`, and Agent question / approval cards still work; personal computers and the assistant can continue driving Agents on work computers.
  This policy applies to every path to the same physical computer: a `work` entry also restricts its aliases. The assistant's default placement skips work computers, and `manager.json` cannot set `assistant.machine` to one. A bad explicit placement rejects the manager's handshake; a bad edit while connected is logged and keeps the last valid settings. With no eligible computer, no assistant is placed. The app's assistant-machine setting has the same restriction.
  Tasks sent to a work computer: the Inbox AI twin cannot run there. Set the field in **hub.json**, then restart the hub to apply it; this field is separate from pairing approval (`machine_trust`).
- `machines[].address`, `machines[].proxy` (kind `service`): the connector's `host:port` (included in the link from `persona-link pair --listen`; paste it under **Add computer ›
  Hub connects** in the admin UI to fill it in); optional `proxy`, `socks5://[user:password@]host:port`, lets the hub connect through this SOCKS proxy (when the computer is only reachable through another machine).
  The connector's public-key fingerprint and the credentials the hub gives it are not in hub.json: pairing stores them in Postgres (credentials encrypted with `~/.persona/secret.key`).
- `machines[].tailcat_addr` (optional): the `tc…` address printed by `personad node tailcat init` on that computer; the hub connects through tailcat when ssh fails. The address is a secret (includes the WireGuard preshared key); keep it only in hub.json, like the token. `kind: "tailcat"` means
  tailcat only, no ssh attempt (no `ssh_host`); `kind: "ssh"` plus `tailcat_addr` means ssh first, tailcat on failure.
- Env vars `PERSONA_DATABASE_URL`, `PERSONA_PUBLIC_URL`, `PERSONA_LISTEN`, `PERSONA_NODE_PACKAGES`, `PERSONA_ADMIN_ACCESS` override the corresponding settings (new `PERSONA_*` names win, not written back to the file); Docker deployments use them. A nonempty `PERSONA_ADMIN_ACCESS` replaces the whole `admin` access scope together with `PERSONA_ADMIN_CIDRS`; setting only `PERSONA_ADMIN_CIDRS` has no effect; with `PERSONA_LOCAL_NODE=0`, first run does not write the local machine.
- `node_packages` (optional): where the manager fetches the node builds the hub has none of its own for (a computer of another OS or architecture, persona-node). `url` is a release's `dl/` (`https://<site>/dl`, or a mirror of it that serves `latest.json` and `<version>/manifest.json` with the archives beside them); empty uses the hub's update source (`update.url`, else this build's update URL: a release hub's; a Docker image built with one), through `update.proxy` unless `proxy` here names one; `"off"` never fetches. A mirror must answer 404 for a file it does not have (any other error counts as the site being away). `proxy` is an `http://[user:pass@]host:port` proxy for it (the environment's proxies are not used). The env var `PERSONA_NODE_PACKAGES` overrides `url` (a compose file's mirror, or `off`).
- `update` (optional): where the hub looks for its own releases, and how it reaches them. `url` is the directory `latest.json` is in (`https://github.com/<owner>/<repo>/releases/latest/download`, a publish site's `https://<site>/dl`, or a mirror), in place of the one the build was made with; empty keeps the build's. It must be `https://` (`http://` only to this machine: releases are not signed), or `file:///<directory>`, without a user, password, query or fragment; one that does not read leaves the hub with no source, and Settings › hub says why. `proxy` is an `http://[user:pass@]host:port` proxy for the update's checks and downloads alone (and for node builds fetched from the same source, below); everything else the hub talks to is unaffected. A `proxy` that does not read fails every check rather than going around it. Empty uses the environment's proxies for these requests. Its user and password never appear in the log or the app. Read at the hub's start.
- `update.development` (optional): snapshot configuration for Web development updates from `https://github.com/nwverse/persona.git`, fixed to `main`. The Hub pauses its own writers for the snapshot by default; `backup.quiesce` and `backup.resume` are an optional advanced override for writers outside the Hub's control, and must be set together. `backup.directory` and `backup.retain` are read when Settings › Hub › Update source › Web development does not set them (the Web UI accepts only a folder and a count, never a command or credential); optional `extra_roots` and `extra_databases` extend the snapshot. Build platforms are selected automatically from the Hub and registered computers' last-reported OS/architecture. See Web development updates for the recommended folder, path safety rules, snapshot boundaries and recovery.
- `tailcat.derp_map_url` (optional): the DERP map URL when using a self-hosted DERP relay (derper) instead of Tailscale's public relays; each computer's
  `init` must use the same `--derp-map-url`.
- `machines[].claude_config_dirs` (optional): Claude Code config directories on this machine besides `~/.claude` (`CLAUDE_CONFIG_DIR` set by launchers,
  e.g. `["~/.config/claude-code/profiles/work"]`; `~/` is that machine's home). The node finds directories used by running claude processes itself and remembers them in the database;
  only list directories "not usually running, but whose historical sessions you want in the list" here. Conversely, hide unwanted directories (automation scripts' own `CLAUDE_CONFIG_DIR`)
  in **Settings › Claude Code › Blocked Config Folders** in the App; stored in the database, not hub.json.
- `machines[].claude_start_dir` (optional): the `CLAUDE_CONFIG_DIR` passed when Persona starts an Agent with the `claude` launcher on this machine
  (new Agent, to-do dispatch, assistant's agent_start, assistant chats), e.g. `"~/.config/claude-code/profiles/work"` (`~/` is that machine's home). For computers where "claude is signed into a profile directory, not `~/.claude`":
  without it, Persona's claude uses the default directory, which is not logged in and immediately replies `Not logged in · Please run /login`. Only controls the `claude` launcher;
  aliases / functions in `~/.zshrc` and Persona-managed launchers keep their own settings. This directory is also added automatically to the session-search directories above. The node must also be this version; manual hub.json edits require a hub restart and take effect on reconnect.
- `machines[].inbox_dir` (optional): this computer's **receive folder**, where files sent to it to keep land (from the phone with `dest=inbox`, and from another computer through `POST /api/v1/files/transfer`), e.g. `"~/Inbox"` (`~/` is that machine's home) or an absolute folder in the home.
  Omitted means `~/Downloads/Persona`. The node makes it when missing and refuses a folder the phone may not write to (outside the home, `~/.persona`, `~/Library`, key folders, links out of the home: 409 `read_only`), so a typo never creates a folder elsewhere. Manual hub.json edits require a hub restart.
- `machines[].extra_path` (optional): directories this computer's node puts in front of its PATH, for tools a service's PATH (launchd, systemd, an ssh session) does not have,
  e.g. `["~/.cargo/bin", "/opt/tools/bin"]` (`~/` is that machine's home; other relative entries and `$VARIABLES` are ignored). The node also adds the shims directories of mise
  (`$MISE_DATA_DIR`, else `$XDG_DATA_HOME/mise`, else `~/.local/share/mise`) and asdf (`$ASDF_DATA_DIR`, else `~/.asdf`) that exist, after the rest of its PATH, by itself: a repository whose `git-lfs` comes from mise
  gets its worktrees without any setting. Both reach the herdr server the node starts too, after the system directories its login shell starts from; a server already running keeps its PATH until it restarts. The node reads them itself, so no service file holds them
  and rerunning `personad hub setup` or `node-launchd.sh install` keeps them. The node must also be this version; manual hub.json edits require a hub restart and take effect on reconnect.
- `machines[].botmux` (optional): false means this computer's node neither lists botmux sessions nor runs botmux (no `botmux observe`, `botmux dashboard current`);
  omitted lists sessions if botmux is installed. The node must also be this version; manual hub.json edits require a hub restart and take effect on reconnect.
  It is now only a default: "Also Show botmux Sessions" in machine details (the web in the page header) is stored per machine in settings (`botmux/<machine id>`) and wins once set.
- `machines[].herdr_autostart` (optional): false means this computer's node does not start herdr's server itself when herdr is not running; omitted means on. **Only a default**: once the user chooses per machine under **Settings › Machines › Engine**, that choice wins
  (stored in the hub's settings, `herdr_autostart/<machine id>`); later edits here do not affect machines already configured.
- `machines[].herdr_autoinstall` (optional, off by default): true means when herdr is missing on this computer, the node installs a pinned version before starting herdr itself
  (at startup, before creating / continuing sessions). Without it, installation happens only when the user taps "Start" in the Agents list. The node must also be this version; manual hub.json edits require a hub restart and take effect on reconnect.
- `machines[].claude_global_hooks` (optional, off by default): true means this computer's node adds **report-only** hooks to `settings.json` in the default Claude config directory (asynchronous, short timeout, never answer approvals),
  so sessions not started by Persona also report turns and question events to Persona; false or removing the key removes Persona's entries on the next connection (if none exist, nothing is written).
  No blocking hooks are added: users' global configs often have other tools' synchronous PermissionRequest hooks; stacking them delays terminal dialogs.
  The node must also be this version; manual hub.json edits require a hub restart and take effect on reconnect.
- `machines[].usage_windows` (optional, on by default): false stops this computer's node from collecting Codex and Claude subscription usage. Omitted means refresh every
  5 minutes via Codex app-server or local files, without giving Persona access to login credentials.
  The node must also be this version; manual hub.json edits require a hub restart and take effect on reconnect.
- `machines[].launchers` (optional): add launchers the node cannot list on this computer (e.g. shell wrapper functions), or override `agent_type`, `base_url`, `model` for same-name launchers the node reports (omitted fields stay unchanged),
  e.g. `[{"name":"example_wrapper","agent_type":"codex"}]`: the wrapper actually runs Codex (`agent_type` is `claude` / `codex` / `traex` or a runtime's agent).
- `machines[].launcher_recovery` (optional, advanced): controls how an Assistant conversation recovers when its resolved launcher is missing from one discovery snapshot, for example
  `{"wait_seconds":30,"fallbacks":{"custom-launcher-a":["custom-launcher-b","claude"]}}`. The manager asks for fresh discovery immediately and then retries for up to `wait_seconds` (default 30, capped at 120; omitted or non-positive uses the default), including time spent in discovery, and honors request cancellation. A failed refresh or a forced response containing only managed/built-in entries is not evidence that the requested shell launcher recovered. The last successful in-budget catalog is reused at the deadline; no extra discovery runs after it. Unframed node output does not replace the previous cache: an ordinary picker read may retain that last good list and independently usable runtime or app overlays, while a forced recovery read returns the error and its overlays preserve it. If the launcher returns, it runs with its original model.
  `fallbacks` maps a resolved launcher to its only permitted ordered substitutes. A candidate must currently run Assistant on this computer, have the same Agent kind, and pass the existing health check; unavailable candidates are skipped in list order. If a disappeared launcher's kind is absent from both its managed configuration and its last successful shell snapshot, no candidate is eligible because the manager will not guess its kind. Built-in `claude` has no implicit priority or eligibility: list it explicitly to permit it when the original kind is known to be Claude. With no configured usable substitute the send fails with the launcher's own error. A present but Assistant-incompatible launcher does not wait and follows the same explicit-substitute rule. An inherited task baseline on another computer also waits for fresh recovery before using this allowlist. This is machine-local Agent/launcher administration, not an everyday model setting; primary screens are unchanged. Edit `hub.json` and restart the hub. To roll back the change, remove this section and restore the previous hub, manager and node binaries together.
- `personas.github_login` (optional): whose level on the NWVERSE board determines the tiered avatar (Milk Frog). Defaults to the GitHub connector's logged-in account;
  with neither, shows the default "Unhatched" tier.
- `assistant.mode` (default `child`): `child`, the hub starts the built-in manager as its child (below); `service`, the built-in manager is a service of its own, bound once with a one-time code, and the hub neither starts it nor gives it a token (`persona-hub manager-mode`). `hub setup` writes `service` into a new install's `hub.json` when it installs services, and installs and binds that service; `hub setup --manager-service` switches an existing hub. A value this version does not know (a later version's, a typo) starts no built-in manager, and the hub says so in its log. Taken at the hub's start.
- `assistant.external` (default `false`): the assistant process (`personad assistant`) is started externally. By default the hub starts it as a child process and restarts it on exit,
  giving it a fresh bus token at every start (via stdin); with `true` the hub does not start it. On the machine that will run it, first generate a token with `personad hub assistant-token` (printed once; generating again replaces the old one), then
  `PERSONA_ASSISTANT_TOKEN=<token> PERSONA_ASSISTANT_HUB=<hub address> personad assistant`. It reads hub.json and env vars just like the hub: database from `database_url` (`PERSONA_DATABASE_URL` takes priority);
  its own sections come from the `manager.json` on that machine (`~/.persona/managers/assistant/manager.json`, "manager.json" below); without `PERSONA_ASSISTANT_HUB`, it connects to the local `listen` port. While the assistant is disconnected, its APIs return `503 assistant_offline`.
  An `https` hub with its own self-signed certificate (the public TLS port) also needs `PERSONA_ASSISTANT_FINGERPRINT=<fp>`: the public-key fingerprint `personad hub pair` prints, which it pins as a manager on another computer does.
  Without it the system's CAs are checked (a hub behind a reverse proxy with a public certificate); a value that is not a fingerprint, or one set for an `http` hub, stops it at start.
  The assistant also carries all computers: when running elsewhere, that machine connects the computers in `machines` (ssh host names, tailcat key,
  and even the hub machine's own `node --stdio` are on that machine); while disconnected, machines show offline and machine APIs return `503 assistant_offline`.
  Other managers created in the App do not read this hub.json section: their configuration is `manager.json` in their own directory; their database is a Postgres of their own (a Docker container `personad manager install` makes, or `--database-url`), never the hub's.
  This key stays in hub.json: the hub decides whether to start the built-in manager, before any manager is connected.
- `notify`: timing for "Done" pushes (`hub/notify.go`, omitted uses defaults): `done_delay_seconds` (how long after completion to wait for someone to look before pushing, default 60),
  `long_turn_seconds` (how long a round must be to count as a "long task", default 60), `background_hold_minutes` (maximum wait for background tasks to finish, default 120),
  `live_metrics_seconds` (minimum interval between pushes to one device when only Live Activity numbers change, default 60).
  Each device's push preferences are set separately in the App (**Settings › Notifications**, stored in hub `devices.prefs`), not here.
  This section stays in hub.json: push delivery is the hub's, and it hands the timings to the managers in its hello.
- `screen_link_off` (default `false`): the hub never selects the live screen's session subprotocol, so every live screen stays on the
  plain relay through the hub, whatever the apps and nodes support.
  The switch for when direct routes misbehave; it takes effect when the hub restarts.
- `intercept_readers` (default empty): extension IDs whose tokens may read `GET /api/v1/push/intercepts`, on top of the
  `notify` scope. Full credentials always may; an extension token whose ID is not listed gets 403 even with `notify`.
  A listed extension still reads only its own manager's entries, and held-push bodies are stored and served as a
  summary (title plus at most 140 runes), never as full text. Add the extension's ID (its manifest `name`) here when
  its control panel needs the held-push list; it takes effect when the hub restarts.
- `shutdown_timeout_seconds` (default 8): total hub shutdown budget, including closing the database connection pool; external `docker stop -t` must be longer.
- `auto_update_nodes` (default `false`): lets the managers update the computers' nodes by themselves, after a confirmed hub update and when a node on another build reconnects (the hub hands it over in `devices.config`).
  Off, a node changes only when someone picks **Redeploy** for its computer.

## manager.json

A manager's own sections of the configuration are in its `manager.json`, not in `hub.json`: the hub reads none of them (`server/internal/managerconf`). When its `hub.json` still has some, the hub says once at start which: an edit there does nothing.

- The built-in manager (the assistant process the hub starts, `personad assistant`): `~/.persona/managers/assistant/manager.json` (0600, its directory 0700), where its own directory goes once it is a service of its own. It holds no identity: the hub hands this manager its token.
- A manager made in the app: the `manager.json` of its own directory (`~/.persona/managers/<id>/manager.json`), next to who it is (`id`, `hub`, `token`…).
- **Its computers** (`machines`, `machines_from_hub`): the entries the manager connects, as the hub last handed them over, kept so that a manager whose hub hasn't answered within 20 seconds of its start connects them. The hub still edits the lists for now (adding, removing and moving a computer go through `hub.json` and the admin page), and each hand-over is written over `machines`: an edit there by hand lasts only until the hub's next one.
- Edits: the admin UI's **Usage sources** writes `manager.json`'s `usage`; hand edits of `manager.json` take effect when the hub restarts (which restarts the built-in manager), or the manager for one made in the app. The sections the hub's hello can carry (`assistant`'s four keys, `agents`, `ai`, `inbox`'s models and `inbox.agent.machine`, `hook_timeout_minutes`) are also read again whenever the hello arrives (a reconnect, a `manager.updated`). What is changed in the app (About me, the assistant's machine and launcher, the triage switches, model configuration) is stored in the database and wins over these defaults.
- **A `manager.json` that does not read stops the manager, not the hub**: when it is not JSON, is empty, has a section of the wrong shape, has a `connector_gate` rule the gate cannot use, or is a link to nothing (a volume not mounted yet), the manager does not start and says why, naming the file. Started without its sections it would drop the user's gate rules (`lark_gate`, `connector_gate`). The hub keeps running and starts the built-in manager again with backoff (1 s doubling to a minute), logging the reason each time; the app shows that manager as offline meanwhile. A manager made in the app exits with status 2 on it, which its systemd service does not restart on (launchd and Docker do), so on Linux start it again once the file is fixed. While a manager runs, a `manager.json` that stops reading keeps the sections it last read and says so in its log.
  While a manager runs, a `manager.json` that stops reading (a hand edit gone wrong) doesn't take effect: it keeps the sections it last read and says so in its log, rather than falling back to defaults, until the file is fixed (its next start fails as above).

```json
{
  "assistant": {
    "owner": "Your name",
    "notes": "- Code is in `~/code`.\n- Use `xxx-*` skills for work data.",
    "machine": "server",
    "launcher": "claude"
  },
  "google": { "client_id": "…", "proxy": "" },
  "lark": { "account": 2 },
  "extensions": [{ "id": "voice", "command": "personad", "args": ["voice"] }]
}
```

- `assistant.owner`: how the assistant in the "Chat" tab addresses you; `assistant.notes`: Markdown appended to the assistant instructions,
  describing things specific to your environment: code locations, skills / CLIs for work data, directories it must not touch.
- `assistant.machine` / `assistant.launcher`: default machine and launcher for the assistant (unset, or a machine the hub doesn't have: the hub's own machine, else the first one; `claude`); App settings take priority once changed.
  `owner`, `notes`, `machine`, `launcher` are all defaults, laid over the hub's hello: changed settings belong to the assistant itself (settings `assistant`, only it reads/writes them).
- `assistant.memory_dreaming`: set to `true` in `manager.json` to enable background consolidation of the main chat's memory.
  It is off by default and uses the Data processing model. See Assistant memory.
- `assistant.complete_agentless_todos`: advanced, off by default. Set to `true` to let the conversation supervising an agentless memo complete it without a second confirmation card. A memo from the same conversation with no supervision ledger also qualifies. The fast path never applies to another conversation or a planned task (with or without an Agent attached); reopening and track outbound confirmation are unchanged (To-dos, "The assistant's todo tools").
- `assistant.supervision_batch_minutes`: advanced, unset by default. An administrator can pin a whole number from 0 to 1440 here; a present value, including explicit `0`, takes precedence over the assistant's saved `assistant.supervision_batch_minutes` setting. When omitted, the saved setting and its default `0` are unchanged. Removing the pin restores the saved value. An out-of-range value is a configuration error: a running manager keeps the configuration it last read (supervision batching).
- `assistant.task_execution_wait_seconds`: advanced, default `600`, from 0 to 3600. A scheduled task whose strict execution options need a node capability waits this long for an update before recording `task_execution_wait_timeout`; nothing is sent during the wait. `0` records the refusal immediately. Removing the setting restores 600 seconds.
- `assistant.task_execution_legacy_fallback`: advanced, default `true`. It lets only an attested inherited ordinary task omit `task_execution` on a node that predates it, and records actual use as `compatibility_fallback: "legacy_task_execution"` in run history. Set it to `false` to make every such task wait for the capability instead. Explicit execution choices and Health tasks never use this omission.
- `assistant.node_update_handoff_grace_seconds`: advanced, default `60`, from 0 to 86400. After this much continuous headless activity, an update stops admitting new headless turns while admitted RPCs and current turns finish. `0` preserves the indefinite natural-idle wait. Removing the setting restores 60 seconds. The value is also carried by `node.update.commit` for the resident drain.
  `assistant.external` stays in hub.json ("hub runtime config" above).
- The everyday entry for model settings is **Settings › Model Providers / Model Configuration**: the former manages connections and credentials; the latter manages uses such as chat, classification, translation, knowledge processing, `/agent`, and scheduled tasks. `ai`, `inbox.*_ai` and the launcher defaults (`assistant.launcher` here, `machines[].launchers` in hub.json) remain as a baseline; unified-use overrides live in Postgres, not written back to either file. Without an override the original model is kept; opening settings does not migrate it to another provider.
- `ai` (no default): model for small App features calling model APIs directly (not through Claude Code): Inbox triage, drafting replies, recognizing email config from pasted text.
  When omitted, these uses automatically select a directly callable connection on the hub (with a Key in **Settings › Model Providers**); only if none exist do they report "No directly callable model yet".
  `api` is `messages` (Anthropic Messages, default) or `responses` (OpenAI Responses, always streamed, compatible with relays accepting only `stream: true`);
  `base_url` gets `/v1/messages` or `/v1/responses` appended; `api_key_file` is the key file on the manager's machine (the hub machine for the built-in manager; 0600, read only at call time); `model`;
  `reasoning_effort` applies to `responses` and `messages` (e.g. `low`): `off` maps to Responses `none` and the model's supported Messages no-thinking mode, or low effort on Messages models that cannot disable thinking; `bearer` (default `false`) true sends the Messages API key as `Authorization: Bearer` instead of `x-api-key`, for gateways accepting only Bearer (`responses` always uses Bearer).
- `inbox.triage_ai` / `inbox.judge_ai` (optional, same fields as `ai`, omitted fields inherit from `ai`): AI twin model 1 (triage) and model 2 (linking a Needs you item to one of the user's tasks);
  omitted means both use `ai`. `inbox.agent`: model 3, `enabled`, `launcher` (Claude Code launcher, default the assistant's), `machine` (default the assistant's machine),
  `concurrency` (1), `timeout_minutes` (15), `max_age_hours` (24), `rules` (not read; what the twin takes on is set by inbox policies).

  ```json
  "inbox": {
    "triage_ai": { "base_url": "<gateway address>", "api_key_file": "~/.persona/inbox-ai.key", "model": "<model>" },
    "judge_ai":  { "base_url": "<gateway address>", "api_key_file": "~/.persona/inbox-ai.key", "model": "<model>" },
    "agent": { "enabled": true, "launcher": "<launcher>" }
  }
  ```
- `inbox.rules`: personal rules for Inbox AI triage (natural language, e.g. "anything from this person needs me", "this alert group is only noise"), appended to the triage prompt and preferred over default judgments.
  When a connector item carries its own rules (`rules`, e.g. an extension's `triage_rules` setting), those are used instead of this setting.
- `agents.triage`: triage after a round stops. Switches are under **Settings › Session Triage** in the App (stored in the manager's settings, all off by default but `transient_retry`),
  and only limits live here: `max_retries` (unused for temporary model failures), `retry_base_seconds` (the first continuation's backoff, doubled for each next one, default 15), `account_exhaustion_max_retries` (default 0), `max_wakes` (maximum wakes, default 6), `settle_seconds` (delay after stopping before triage, default 15), `just_finished_hours` (how long a done turn nobody has seen stays Just finished in the Agents lists, default 3; `docs/protocol/agent-states.md` ("Just finished")).
  `account_exhaustion_max_retries` applies only to coding Agent sessions after the explicit upstream account-pool phrases documented. A task leader's Agent (`role=leader`) is a coding Agent and remains subject to the budget. All Assistant conversations keep their existing continuation: main chat, leader conversations, background supervision, side conversations, scheduled-task and track conversations, and meeting-room conversations including guests; only main-chat, leader-conversation and background-supervision protected continuations re-resolve the configured launcher and model. The default 0 sends no automatic continuation for that class. A positive value compares the existing consecutive transient retry count (continuations already sent, including ordinary transient sends in the same failed sequence) with the bound. Any turn that does not end in a transient error resets it; a turn that works and then ends in a transient error, or a manual Continue, does not. Manual Retry and explicit Continue remain available regardless of the budget.
  `enabled` / `auto_continue` / `wake` / `decide` / `tool_loop` / `closeout` / `closeout_ask` / `transient_retry` are merely initial values if never saved in the App (omitted means off, but `transient_retry` on). Triage is done by the manager (assistant process), which reads this section here. Hand edits take effect on manager restart (a hub restart for the built-in manager) or the next hello (a reconnect or `manager.updated`), as described in **Edits** above.
- `agents.launcher_health`: launcher health checks and automatic repair: `enabled` (off by default; true enables periodic checks and automatic repair), `interval_minutes` (default 30),
  `repair` (repair Agent: no `skill` means no repair Agent, only CLI upgrades; `machine` defaults to the assistant's computer, `cwd` to home, `launcher` to an available one, `permission` is the permission mode, default `bypassPermissions`).
- `agents.delegate`: the assistant answers Agent questions first. **Off by default**, enabled only with `enabled: true`;
  `timeout_seconds` is how long the assistant may take (default 180); if no result arrives, the question is pushed to the user unchanged. `rules` are the user's own delegation rules (prose shown to the model with built-in rules; they cannot relax red lines).
  For hand edits see **Edits** above. Example enabling it (alongside the other sections of `manager.json`):

  ```json
  "agents": {
    "delegate": {
      "enabled": true,
      "timeout_seconds": 180,
      "rules": "If tests are missing, add them without asking me"
    }
  }
  ```

  Model selection is elsewhere: **Settings › Model Configuration › Question Delegation** (purpose `agent.delegate`). If never selected, it uses the "Information Filtering" connection with Opus 5.5 (`claude-opus-5-5`);
  change provider or model there, or use `PATCH /api/v1/model-config/purposes/agent.delegate` (`{"revision": <current>, "model": "claude-sonnet-5-5"}`); "Restore Defaults" returns to Opus.
- `agents.dispatch` allows an Agent to start a new Agent on another connected computer. New dispatches use A2A for questions and results. Dispatch is off by default.
  `enabled` (true lists the Agents' `dispatch_start` / `dispatch_read` tools; a dispatched task runs on the target computer's default launcher and model for new Agents, started by an Agent and not by you),
  `max_active` (how many of one Agent's dispatched tasks may be working at a time, default 3). Work computers and guest conversations never get the tools. Restart the hub after changing it. Example:

  ```json
  "agents": {
    "dispatch": { "enabled": true }
  }
  ```

- `agents.a2a` defaults to the manager's confirmed personal computers. `machines` limits participation in A2A and new dispatches. An empty list disables both.
  Set `public_url` to expose Agent Cards and A2A JSON-RPC endpoints with Persona credentials. Agent-to-Agent communication

- `agents.review`: the independent review of a supervised task's finished change. **Off by default**, on once `launcher` names a launcher on the working agent's computer (pick one on another model than your agents'; there is no default reviewer).
  `model` and `effort` (optional) override the launcher's own; `permission` is the reviewer's permission mode (default `plan`); `timeout_minutes` bounds one review (default 30). The reviewer works in a disposable copy of the change, so its permission mode is a second guard, not the only one. Example:

  ```json
  "agents": {
    "review": { "launcher": "<launcher name>", "model": "<model id>" }
  }
  ```

- `hook_timeout_minutes` (default 30, 0 or negative also means 30): maximum time Claude Code hooks wait for phone answers (questions, approvals); timeout hands control back to the terminal.
- `usage` (optional): subscription usage sources from an account-pool gateway (such as a relay), merged into the sidebar's usage bar.
  `usage.sources[]` (at most 8): `id`, `name` (source name in the sidebar), `type` (`relay` / `relay_key` / `generic`, or `extension` with `extension` and `launcher` instead of `url` and `key`), `url`, `key` (the gateway's admin Key, or for `relay_key` an ordinary API key of the relay, **a secret: only in `manager.json` (0600)**, unreadable from the admin UI and APIs, absent from logs), `enabled` (false disables this source).
  `usage.enabled` (false is the master switch), `usage.poll_minutes` (poll interval per source, default 5, 1 to 1440), `usage.hidden_providers` (providers such as `codex` that the usage bars leave out; display only). This section belongs to **the manager** (`personad assistant`, which reads and writes it in its `manager.json`; the hub does not read it): it only reads the gateway's cached numbers (`refresh=false`), requests come from the manager, and nodes never touch the gateway.
  Edit in the admin UI at `/admin/usage` (the hub forwards to the manager) for immediate effect; manual edits require a hub restart (which restarts the manager too), or opening this admin page (the manager adopts the version on disk).
- `microsoft` (optional): Outlook's "Sign In with Microsoft". `client_id` is the default client ID
  (usually unnecessary: the user enters their own Microsoft Entra-registered one in the App, encrypted in the hub and preferred over this); `redirect_uri` defaults to
  `persona-mail-oauth://outlook`, and changes must match the registration in Entra.
- `lark.account` (optional integer, still effective): the default Lark gateway account number, `1` for the main account, `2` for the second; omitted or `0` uses the main account, valid range 1–999999999,
  e.g. `"lark": {"account": 2}`. The number comes from the `id` in the Lark gateway's `/accounts`, not the Lark user ID. Operations on Inbox Lark items from the default account omit `ol_account`,
  while other accounts pass their own number; the Inbox Agent also uses this default when handling Lark items. The Lark extension's default account is authoritative (`default` in its messaging source's `GET accounts`, #5098):
  if the extension cannot be queried, use the last retrieved value; only if none has been retrieved since the manager started is this field used. Restart the hub after changing (`systemctl --user restart personad`).
  Server-side Inbox records are isolated and retained per account, available again when switching back.
- `lark.cli` (optional): Lark's official open-platform CLI, lark-cli, as a second Lark source besides the Lark extension (gateway):
  `"lark": {"cli": {"bin": "lark-cli", "profile": ""}}`. `bin` is a command name or absolute path, default `lark-cli`; non-absolute paths are found in a login shell
  (global npm installs often live under nvm-like directories absent from systemd's PATH); `profile` is optional lark-cli's `--profile`.
  When it is empty and the built-in Lark bot has credentials, the manager binds managed profile `persona` to the bot's app (using `config show` and `config init`, with the secret on stdin); otherwise lark-cli's default profile is used. An explicit profile always wins.
  Optional `inbox_poll_seconds` (default 120, at least 60) and `inbox_page_limit` (search pages per Inbox round, default 5; the rest continue next round) tune the lark-cli Inbox.
  Application and user logins stay with lark-cli (`~/.lark-cli`), never in `manager.json`: sign in from Settings › Connectors › Lark via the device link, or run
  `lark-cli auth login --domain all` yourself on the assistant's machine (or `lark-cli --profile <profile> auth login --domain all` when `lark.cli.profile` is set). The App's login starts the blocking command without `--no-wait` and polls it; after a week unused, sign in again.
  Its PATH contains `lark-cli` (with the configured `profile`), write operations go through the gate, and the built-in `larkcli` connector's status, read tools, and watched-document comment Inbox are available when this section is present. Choose document links and wiki spaces in Settings › Connectors; nothing is watched by default. The app's `larkcli_profile` setting is stored with `bots.feishu` in manager settings: an empty value follows the automatic rule, while a profile name selects an existing `lark-cli profile list` entry. A non-empty `lark.cli.profile` in `manager.json` wins and makes that app choice read-only.
  The app's sign-in asks only for the connector's read-as-me scopes (the commands it uses). `lark-cli auth login --domain all` remains an optional manual step when someone wants the assistant wrapper to reach every Lark domain.
  Another personal computer where the manager starts assistant conversations (a Lark `/agent` placed there) gets lark-cli's name and the gate rules, not the profile (an absolute `bin` goes by its base name): its own lark-cli, with its default profile, tells its gate which calls only read. A work computer gets nothing.
  `extensions: true` additionally permits extensions that declare the `lark-cli` host integration to use its typed, read-only RPC for auxiliary context and auth status. It does not permit arbitrary commands, writes or credential export; the manager retains the binary, profile and timeouts. Omitted or false keeps this integration off.
- `lark.prefer` (optional): which source is primary when both the Lark extension (gateway) and lark-cli are available: omitted (default, extension path preferred) or `cli`; other values are the Lark extension's own source names, passed through and validated by it. Currently only affects how the assistant prompt divides the work.
- `lark_gate` (optional; new rules go in `connector_gate` below with `connector: lark`): gate rules for the assistant's Lark writes (the Lark extension's command gate, and lark-cli), after `connector_gate`'s `lark` rules and before built-in rules; first match wins:
  `[{"method":"POST","path":"/conversations/*/messages","action":"allow"}]` (`action` is `allow` / `confirm` / `deny`, optional `body` matches top-level fields).
  lark-cli commands use `{"method":"CLI","path":"/im/+messages-send","action":"allow"}`
  (`path` joins command words with `/`, `*` / `**` wildcards as above); a match skips lark-cli's self-reported risk level.
- `connector_gate` (optional): gate rules for connector writes (such as `github_comment`); first match wins, no match means confirm each operation on the phone:
  `[{"connector":"github","action":"github_comment","match":{"repo":"example-org/*"},"decision":"allow"}]`
  (`connector`, `action` allow `*` wildcards; `match` matches top-level tool arguments, strings may also use wildcards; `decision` is `allow` / `confirm` / `deny`,
  an invalid list is reported in the manager's log at start; `server/internal/connector/gate.go`). Writes matching `confirm_always` in an extension manifest clamp even an `allow` rule to `confirm`. A call whose arguments give a field more than once is refused before any rule is read, and a `deny` or `confirm` rule's `match` reads field names without regard to capitals.
  The assistant's Lark write rules also go here: `connector` is `lark`, `action` is "method path", e.g. `{"connector":"lark","action":"POST /conversations/*/messages","decision":"allow"}`,
  or lark-cli's `{"connector":"lark","action":"CLI /im/+messages-send","decision":"allow"}` (`*` matches one path segment, `**` the rest, `"*"` any call; `match` matches top-level body fields).
  These precede `lark_gate`, followed by built-in rules.
- `sent_ledger_days` (optional): how many days the ledger of what was written as the user is kept; omitted or 0 means 90, below 0 keeps it for good.
- `sent_recall_hours` (optional): how many hours after a send its message can still be taken back; omitted or 0 means 24, below 0 no limit. A connector may allow less (GitHub's comments have no limit of their own).
- `github` (optional): GitHub connector. `{"auth": {"from_machine": "<machine id>"}}`: use `gh`'s login on that machine (the hub runs `gh auth token` via the node,
  keeping the token only in hub memory, never in the database or logs, or exposing it to the App or assistant). Keep `gh` signed in there, with the login in `~/.config/gh/hosts.yml`
  (keychain credentials cannot be read via ssh: `gh auth login --insecure-storage`). `auth.host` defaults to `github.com`; put a GitHub Enterprise host name here (not implemented yet).
  Check status under **Settings › Connectors › GitHub**. `from_machine` is only a default: every hub has GitHub, and under "Sign In From" in **Settings › Connectors › GitHub**
  you can switch to another computer's gh login, or use device-code login (the built-in GitHub CLI Client ID, no OAuth App registration needed; the authorization page shows "GitHub CLI");
  once chosen in the App, that wins (stored in hub settings). The connector needs `repo` scope: gh's default login has it; otherwise the page suggests `gh auth refresh -s repo`.
  `github.proxy` (optional): HTTP proxy for all requests to GitHub (device-code creation and polling at `github.com`, API at `api.github.com`, plus CI logs),
  `http://[user:password@]host:port`, same format as `google.proxy`; omitted means direct (does not read process `HTTPS_PROXY`). Some networks reach only `api.github.com`, not `github.com`,
  so device-code login cannot obtain a code and needs this proxy. An invalid value does not fall back to direct: every GitHub request fails, and the settings page states that `github.proxy` in manager.json is invalid. Actual addresses belong only in manager.json.
  When `extension_market.proxy` is not set, the extension market also uses it to read GitHub markets and download extension packages (`github.com`, `*.github.com`, `*.githubusercontent.com`; when both are unset, uses process `HTTPS_PROXY`, #3277).
  Repository binding checks of public GitHub repositories use `github.proxy` too; when unset they use the manager process's `HTTPS_PROXY` and `NO_PROXY`.
  GitLab and self-hosted repository checks use the process's proxy environment. Checks validate destination DNS locally and tunnel to a pinned IP, preserving TLS hostname checks;
  they do not pass connector tokens to the repository or fall back to direct when a selected proxy fails..
  `github.inbox` (optional) supplies default Inbox rules (App changes take priority): `ci` (`off` / `default_branch` / `mine` (default) / `all`),
  `muted_repos` (`["owner/name"]`), `ignore_branches` (branches not to notify for, `path.Match` patterns, default `["train/*"]`).
- `bots.feishu` (optional): application credentials for the bot used to chat with Persona in Lark: `app_id`, `app_secret`, `domain` (`feishu` / `lark` / open-platform address),
  for people who do not want to enter them in the App. Values saved in the App (**Settings › Connectors › Lark Bot**) win; switches, pairing, and all features are in the App.
- `extensions`: extensions hand-written in manager.json, started by the manager and forwarded to the App (voice chat, etc.): `id`, `name`, `kind`, `command` + `args` (or only `socket` for an extension running elsewhere),
  `dir` (working directory), `env` (extra environment variables), `disabled` (`true` disables while keeping the config), `config` (the extension's own settings);
  extensions installed with `personad extension install` are registered in the database, not here. For personad's built-in voice extension (GPT-Live), use `"command": "personad", "args": ["voice"]`,
  with upstreams in `config.providers`. Restart the hub after changes.
- `extension_market.proxy` (optional): an HTTP or SOCKS5 proxy for extension marketplace catalog reads, package downloads and HTTPS git clones, independent of the GitHub connector. Write `http://[user:password@]host:port` or `socks5://[user:password@]host:port` (default ports 80 and 1080). It covers GitHub, ordinary HTTPS markets, OCI registries and mirrors. An explicit value overrides `github.proxy`, the process proxy environment and `NO_PROXY`; an invalid value fails rather than falling back to a direct connection. SSH git clones do not use it. When omitted, GitHub keeps using `github.proxy` and other market traffic follows the manager process environment, as before. Proxy credentials belong only in this 0600 manager.json. Restart the manager after changing it.
- `extension_market.ca_file` (optional): a path to a PEM file containing one or more CA certificates for a private HTTPS extension marketplace. Use an absolute path so lookup does not depend on the manager's working directory, for example `"extension_market":{"ca_file":"/etc/persona/market-ca.pem"}`. The certificates are appended to the system root pool only for the extension marketplace HTTP client used for catalog reads and package downloads; other manager HTTP clients, process-wide trust and git clones are unchanged. HTTPS git clones that need the same CA must configure Git separately (for example `http.sslCAInfo`). The added roots are trusted for every host this client contacts, including GitHub, registries, mirrors and redirect targets, so configure only CAs you control and use a name-constrained CA when the trust must be narrower. Certificate and hostname verification stays enabled, and marketplaces must still use HTTPS or GitHub. The file must be a regular file no larger than 1 MiB. An unreadable, non-regular or oversized file, or one with no parseable PEM certificates, fails all marketplace operations that use this client, including the official marketplace, with a clear error instead of silently using the system-only pool. When omitted, TLS behavior is unchanged. Remove the setting and restart the manager to return to system roots only; restart after every change to the file or setting.

## A dedicated assistant node on an existing computer

Register a separate `kind: local` machine with its manager through the atomic admin endpoint described by the manager's admin API. Keep the existing computer's ownership and
runtime unchanged. Do not create a carrier-owned local entry and move it afterwards.

A second node opts into session discovery isolation with four absolute paths:

```sh
PERSONA_HOME=/absolute/manager-state
PERSONA_NODE_SESSION_DIR=/absolute/manager-state/assistant-m-abcdef
CLAUDE_CONFIG_DIR=/absolute/manager-state/claude
CODEX_HOME=/absolute/manager-state/codex
```

Set these on the dedicated manager service so its local stdio node and agent subprocesses inherit them. The assistant,
Claude and Codex directories must be distinct, non-overlapping descendants of the independent state root. Missing paths,
relative paths and symlink escapes are rejected. With this scope, the node discovers only the private profiles and
sessions whose actual working directory is inside the assistant directory; process, remote and terminal-host discovery
cannot add other profiles. Global hook configuration does not install or remove hooks. Built-in Persona MCP servers
explicitly carry the node's state root, including when the agent runtime filters inherited environment variables.

`manager.json` has no `assistant.dir` setting. The manager derives its assistant directory from the node's reported
`data_dir` and `identity.AssistantDirName(managerID)`. Verify that reported `cwd` and each new session's actual `cwd`
resolve to `PERSONA_NODE_SESSION_DIR`; do not rely on an ignored JSON field. An internal symlink to
`<state root>/.persona/assistant-m-abcdef` can retain assistant-directory recognition by older nodes on the same
computer. Both the link and target must stay inside the independent root. Verify the existing node does not list the
new session after the first turn, including aliases under another agent profile.

This scope isolates discovery and Persona state, not operating-system permissions. Prepare only the required provider
configuration and credentials in the private profiles; do not copy existing histories or unrelated credentials. Before
activation, verify the installed binary's version, checksum and `personad manager capabilities --json`, the four service
environment values, durable ownership and the actual derived directory. After activation, verify a reply and a harmless
Persona tool call through the dedicated node, then switch between both assistants and inspect both histories.

## A local external model adapter

An advanced, optional node integration can reuse connections managed by a local program. Set
`PERSONA_LAUNCHER_ADAPTER_CONFIG` only in that node service's startup environment to the absolute path of a private
JSON configuration file. With no value, no adapter file or executable is searched for. This is local configuration,
not hub state: the hub, manager configuration messages and clients cannot register executable commands.

```json
{
  "version": 1,
  "id": "example",
  "command": ["/absolute/path/to/interpreter", "/absolute/path/to/adapter-entry", "integration"],
  "native_program": "/absolute/path/to/claude"
}
```

Use a stable `id`. `command` is an argument array, not a shell command; the first entry and `native_program` must be
trusted absolute executable paths. A compiled adapter can use a one-element command array. Interpreter-based tools
must name their interpreter explicitly rather than depend on a service finding it through an `env` shebang.
Keep the configuration 0600 in a directory other users cannot modify; symlink configuration files and unsafe owners
or write permissions are rejected. The command and native executable are validated on this computer, never supplied
by a model inventory or request. Restart that node to load a changed registry; no installer or production service
configuration is changed automatically.

The first supported use is an explicit main-chat model pick on POSIX using the Claude Code CLI without a required
agent sandbox. Unsupported execution methods and sandbox combinations fail before the agent starts, without
changing the policy. This source cannot become a global default, profile/tier, background-task model or coding Agent
launcher. Existing connections and their defaults are unchanged. If a saved pick names a disabled adapter, restore
that local configuration or reset the main chat's pick; Persona does not choose another connection silently.

The inventory is sanitized metadata. Connection credentials stay on the executing computer; a bootstrap may use a
0600 temporary settings file in a 0700 directory, removed at process exit or forced stop.

## For developers

- When a new feature needs a value "specific to one person" (machine name, domain, account, email, path, launcher, internal company tool), add it to one of the two configs above, never to code,
  UI text, or tests. UI text says "the hub machine" and "your computers", not specific machines.
- Details of this dev machine (available machines, hub address, where accounts live) belong in `CLAUDE.local.md` at the repo root (uncommitted), not `CLAUDE.md`.

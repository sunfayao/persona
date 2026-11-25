# Persona naming cutover

代码与界面使用 **Persona**。**合并代码不授权部署或发布**；历史[切换操作](persona-cutover.md)也不能因此直接执行。

<a id="源码位置构建入口与兼容名字"></a>

## Canonical source and build entry points

| Category | Canonical name |
| --- | --- |
| Go module | `github.com/nwverse/persona/server` |
| Hub / node / hook / MCP binary | `personad`, source `server/cmd/personad` |
| Connector program | `persona-link` |
| Hub / manager programs | `persona-hub`, `persona-manager` |
| Linux services | `personad.service`, `personad.socket`, `personad-node.service`, `persona-link.service` |
| macOS LaunchAgents | `com.persona.hub`, `com.persona.node` (custom connector jobs are inventoried separately) |
| Runtime root / build config | `~/.persona`, `~/.config/persona/config.json` |
| Config override / runtime variables | `PERSONA_CONFIG`, `PERSONA_*` |
| Config command tool | `scripts/persona_config.py` |
| MCP server / tool prefix | `persona`, `mcp__persona__*`, `persona_settings`, `ask_persona` |
| URL scheme / assistant route | `persona://`, `/persona`, `/persona/<id>`, `/persona/new` |
| Message context / file tags | `persona-context`, `persona-file` |
| Header / runtime subprotocol | `X-Persona-*`, `persona-runtime.v1` |
| Default database / container names for new installs | `persona`, `persona_test`, `persona-postgres` |

The `/` front door still opens the main conversation. Previous short assistant URLs are not aliases; previously sent links stop working. The existing avatar data model and `personas/` directory remain. Mochi's built-in ID is `persona` and the bundled frog pack is `nwverse`. The one-time naming cutover preserves old appearance IDs as data, but the five retired bundled choices now display Mochi through the look API's effective-ID fallback; they are not runtime brand aliases. See [Avatars](../apple/persona.md).

## Runtime root: one directory, no automatic migration

The runtime reads only `PERSONA_HOME`, lexically resolves an absolute path, `~/…`, or a path relative to HOME, and otherwise uses `~/.persona`. A query does not create files. Initialization creates that one directory and never creates a compatibility symlink or merges another store. An existing valid custom symlink may be used; a dangling or cyclic link is refused rather than repaired into an empty store. A custom root does not create default-directory aliases.

**Two independent stores** (#744). A machine that has not been through the cutover can still have its older default directory (`~/.<previous-name>`) next to `~/.persona` as a store of its own, not an alias. In that case:

- With `PERSONA_HOME` naming one of them, as a node's or Persona Link's service definition does, initialization uses that one. It never aliases the other over it or merges the two.
- Without `PERSONA_HOME`, initialization (hub, node, Persona Link) refuses: "independent state directories exist". It changes neither directory.
- A client run without that variable (`personad mcp`, `hook`, `launch`, `notify`) resolves the older directory and dials a socket no node listens on.

Agents never run with the node's environment, so the node writes its root into everything it hands them: `PERSONA_HOME` in the `env` of `mcp.json`'s server, in front of the hook command, and in front of the commands it types into a pane (`docs/integrations/engines.md`, "What a start types").

`PERSONA_CONFIG` selects the build/deploy config; otherwise only `~/.config/persona/config.json` is read. The config script emits only `PERSONA_*` build variables and uses the existing configured registration values unchanged. It does not export tokens or bridge environment names.

<a id="部署脚本按服务恢复运行目录"></a>

## Recover the runtime directory per service

`scripts/persona_identity.py --service hub|node|link` reads only the matching canonical service definition. It never borrows a colocated service's root when the selected role has a unit of its own. A `PERSONA_HOME` already present in the environment, including an empty value, takes precedence.

The resolver supports systemd unit resets, sorted drop-ins, `XDG_CONFIG_HOME`, `%h` / `%%`, external `EnvironmentFile` overrides and `UnsetEnvironment`. It extracts only `PERSONA_HOME`, never executes shell text and never returns other credentials. Missing required environment files fail closed. Auto mode rejects different independent roots; explicit role selection avoids ambiguity. `--prepare` creates only the selected directory. `--prefix` resolves the remote root remotely and exports only `PERSONA_HOME`, not the caller's local root.

## Verification boundaries

- Scan tracked file contents and names, including generated schema bundles.
- Run Go build and the full suite against a dedicated real Postgres test database using `PERSONA_TEST_DATABASE_URL`.
- Run the web typecheck, unit tests, production build and available source checks; explicitly report if no lint command is configured.
- Generate extension wire code centrally with `python3 scripts/gen/extension_schema.py`; `--check` must show no drift. Do not hand-edit generated Go/TypeScript.
- Rebase onto the latest `origin/main` before the single PR is pushed, rename any newly merged previous-brand additions, rerun affected verification, and scan the merged `origin/main` again.

Historical [acceptance](rename.md) and [audit](rename.md) records describe earlier stages, not authorization to run this incompatible cutover. Previous compatibility guarantees no longer apply to this version. Do not deploy, publish, restart live services, move live data or change live MCP configuration until the user approves the operational plan.

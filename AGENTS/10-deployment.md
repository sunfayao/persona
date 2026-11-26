# Deployment

> How a build reaches the machines that run Persona, and what you may and may not do there. Read this before you run
> `scripts/deploy`, change it, or change anything about how the programs start, update or restart. You usually arrive
> from core constraint 11 of `AGENTS.md`.

---

## The rule first

Deploying replaces the programs the owner is using right now, on the hub's machine and on their computers. You deploy
only when the owner asked for it in this conversation. Finishing a change, passing tests or merging a branch is not
a request to deploy.

## What runs where

| Machine | Runs | Installed as |
| --- | --- | --- |
| The Linux server | hub, manager, its own node | systemd user services (`scripts/deploy/hub-systemd.sh`) behind the hub's edge, which holds the listening sockets while the binary is switched |
| Each computer of the owner | node and Persona Link | a launchd agent on macOS (`node-launchd.sh`), a systemd user service on Linux (`link-systemd.sh`) |

Two kinds of configuration exist, and neither is in the repository:

| File | Read by | Holds |
| --- | --- | --- |
| `~/.config/persona/config.json` (`PERSONA_CONFIG`) | only `scripts/` (through `scripts/persona_config.py`) | Machines, addresses and accounts to build for and deploy to. `config.example.json` shows its shape |
| `~/.persona/` on each machine (`PERSONA_HOME`) | the programs themselves | Run-time state: `hub.json`, `managers/assistant/manager.json`, `link.json`, keys |

`docs/operations/config.md` is the field reference for both. A `PERSONA_*` environment variable overrides the
matching `hub.json` field without being written back.

## The command

```bash
scripts/deploy/deploy.sh                 # every machine of the configuration
scripts/deploy/deploy.sh <machine id>    # one machine
scripts/deploy/deploy.sh web             # only the web app: no restart
```

| What it does | Consequence for you |
| --- | --- |
| Refuses a checkout that lacks commits of `origin/main` | Deploy from the trunk or a branch that contains it. `PERSONA_DEPLOY_ANY_COMMIT=1` exists for a deliberate rollback only |
| Takes the operation lock (`scripts/deploy/operation_lock.py`) | Two deploys never overlap; do not work around a held lock |
| Builds the web app first (`npm ci` when the lock file changed, `npm run build`) | A failing web build, including the privacy scan and the first-screen budget, stops the deploy before any machine is touched |
| Builds the server with `-tags "$(cat build-tags.txt)"` and a version from the nearest `v*` tag plus the build time | One build for all programs of a deploy; their versions must match |
| Puts this build's node programs for every platform into the hub's `bin/nodes/` before it switches the hub | Nodes update themselves from the hub. Never update a hub by copying a binary and switching by hand: its nodes would not match |
| Switches the hub to the new binary behind its edge (`scripts/deploy/hub-systemd.sh`); when the switch fails the old hub keeps serving and the script restores the previous binary | A failed deploy leaves the owner on the old version, not on nothing |
| Restarts a resident node only once it is idle (`node-launchd.sh restart-idle`) | Agents in the middle of a turn are not cut off |
| Makes the database container on a new hub (`scripts/deploy/db.sh`: Postgres 18 on the loopback interface, a named volume) | The script is idempotent; an existing database is never recreated |

## Changes that affect a running system

| If you change | Then remember |
| --- | --- |
| How a program starts, its flags, its service unit | Services installed earlier still start `personad` with the old arguments: keep them working |
| A migration | It runs when the new hub or manager first opens the database, under a lock; an old build may run against the new schema during a rollback (`04`) |
| Anything a node or the web app reads from the hub | Nodes and open pages are older than the hub for a while (`06`) |
| What starts child processes or listens on a port | The reaping and listener rules of `02`: a switched hub must not inherit or orphan children and sockets |
| The updater (`internal/update`, `nodeupdate`, `managerupdate`) | A broken updater cannot fix itself. Test it against an installed older build before it ships |

## On the hub's machine

| Fact | Consequence |
| --- | --- |
| The hub is socket-activated | `systemctl --user stop personad` alone does not keep it stopped; stop `personad.socket` too |
| `PERSONA_HUB_EDGE=0 scripts/deploy/hub-systemd.sh` turns the edge off, `=1` on | That is the rollback of the switching mechanism itself |
| A slot that does not stop is killed after its stop timeout; extension programs run in their own process groups | Check for survivors before the next start |
| A macOS node keeps its privacy permissions across updates only when it is signed with a stable identity | `docs/operations/mac-node-signing.md` |

## After a deploy

Check what the owner would notice, not what the script printed: the hub answers, the manager is joined, each
computer shows as connected, a message to an agent goes through and comes back. `personad node doctor` on a computer
reports what that node needs. If something is wrong, say so first and roll back with the previous commit rather than
patching forward on a live system.

## What you never do

- Read or edit the owner's live files (`~/.persona/hub.json`, `manager.json`, `secret.key`, service files) to "check
  something". Use the test profile and a throw-away home.
- Restart, stop or signal a running service outside a deploy the owner asked for.
- Put a machine name, address, token or account into the repository, a log or a commit message. The privacy scan
  blocks the commit, and the push.

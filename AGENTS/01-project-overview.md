# Project overview

> What Persona is, how its processes fit together and where things live. Read this when you are new to the
> repository or cannot tell which part a change belongs to. You usually arrive from the top of `AGENTS.md`.

---

## What it is

Persona is a self-hosted agent product for one owner. A **hub** on a server keeps state in Postgres and serves the web
app. Each of the owner's **computers** runs a **node** that starts and drives coding agents (Claude Code, Codex,
Antigravity, Trae and others) in terminals or headless runtimes. A **manager** process next to the hub holds the
product logic: the assistant (the owner's main conversation, tasks, to-dos, inbox, schedules), the device manager and
the extension manager. The **web app** is the only client.

This repository is the owner's own project. It began as a fork of an open-source project and no longer follows it
or takes anything from it. One person works on it with coding agents. There are no issues, pull requests, hosted
CI, public releases or native clients.

## Processes and how they talk

```
web app ──HTTPS / WebSocket──▶ hub ◀──bus (WebSocket, JSON-lines RPC)──▶ manager process
                                │                                         ├─ assistant
                                │                                         ├─ devices ──RPC──▶ node (one per computer)
                                │                                         └─ extmgr ──▶ extension programs
                                └── Persona Link (persona-link) keeps a computer reachable
node ◀── unix sockets ──▶ local agents (hooks, MCP server, runtimes)
```

| Program (`server/cmd/`) | Role |
| --- | --- |
| `persona-hub` | The hub: authentication, routing, relay to managers, the web files. No product logic. |
| `persona-manager` | The managers in one process: `assistant`, `devices`, `extmgr`. |
| `persona-node` | The device program on each computer. Links no hub, manager or database code. |
| `persona-link` | Connector that keeps a computer reachable from the hub. |
| `personad` | All of the above in one binary; the entry point services installed earlier still run. |
| `persona-projects` | A separate team-documents service with its own database. |
| `password-wasm` | The browser's password client, built to WebAssembly for the web app. |

Hub, manager and node are deployed at different moments, so two parts of different versions talk to each other all
the time. That is why `06-wire-contracts-and-extensions.md` matters for almost every change that crosses a process.

## Technology

| Layer | Choice |
| --- | --- |
| Server | Go 1.27, one module `server/` (`github.com/sunfayao/persona/server`), 140 packages under `server/internal/` |
| Storage | Postgres through pgx v5 with hand-written SQL; embedded migrations in two sets (hub, manager) |
| Web | React 19, TypeScript 5.9 (strict), Vite 7, Tailwind 4, vitest 4; hand-written router and store |
| Agent runtimes | TypeScript packages under `integrations/`, embedded into the node by `server/internal/runtimepkg` |
| Extensions | `sdk/go` (a separate Go module), `sdk/ts`, JSON Schemas under `schema/extension/` |
| Tooling | Python 3 scripts under `scripts/` (test runner, deploy, generators, mock hub, privacy scan) |

## Where things live

| Path | Contents | Rules |
| --- | --- | --- |
| `server/cmd/` | `main` packages: flags, wiring, exit codes | `02` |
| `server/internal/hub/` | The communication center | `02` |
| `server/internal/assistant/`, `devices/`, `extmgr/` | The managers and their domain packages | `02`, `03` |
| `server/internal/node/` | The node and its domain packages (`doctor/`, `files/`, ...) | `02`, `03` |
| `server/internal/db/` | Stores and migrations | `04` |
| `server/internal/prompts/` | Every prompt template and agent tool description | `05` |
| `server/internal/proto`, `*wire`, `nodeproto`, `rpc` | Contract packages shared between processes | `06` |
| `server/internal/i18n/` | Server text shown to people, in three languages | `08` |
| `web/` | The web app, the admin pages, the public share viewer, browser check scripts | `07` |
| `integrations/` | Agent runtime packages and the runtime SDK | `06` |
| `sdk/`, `schema/`, `spec/` | Extension SDKs, schemas that generate code, fixtures shared by server and web tests | `06` |
| `scripts/` | Test runner, deploy, generators, mock hub, privacy scan, commit hooks | `09`, `10` |
| `docs/` | Reference material that the rule files link to | `12` |

## Data flow of one user message

1. The web app posts to the hub (`/api/v1/...`) or sends on the WebSocket.
2. The hub authenticates, finds the owning manager and forwards the request over the bus unchanged.
3. The assistant decides (store, model call, or a node RPC through the device manager).
4. The node types into the agent's terminal or calls its runtime, then reports events back.
5. The assistant publishes a snapshot or an event; the hub relays it to every open web socket.

Keep three states apart in anything you build on this path: the hub **received** it, the agent **got** it, the agent
**did** it. The web app shows them differently and never retries an uncertain send as a new message.

# Persona Runtime SDK

The SDK that connects an Agent runtime with no terminal of its own, one that manages its own sessions, to Persona: the runtime side of the `runtime.sock` protocol. The private npm package is `persona-runtime-sdk` (`package.json`). The update package ids `persona-dsh-runtime` and `persona-pi-runtime` and the wire subprotocol `persona-runtime.v1` are identities of their own.

A long-running service that cannot reach the local socket runs in listening mode as a daemon (`createRuntime(driver, {listen: {port, token}})` or `rt.accept(req, socket, head)`), and the node connects to it.
The specification (what to implement, the conventions for content and interactions, the acceptance checklist) is in [`docs/integrations/runtimes.md`](../../docs/integrations/runtimes.md); the wire protocol is in `docs/protocol/protocol.md`, "Runtime ↔ node".

| File | Contents |
| --- | --- |
| `index.js` | Entry point: `createRuntime` (driver and options; the returned `rt` also has `recent()` and `touched()` to pick the sessions to list) and the item helpers (`page` for paging, `appToolName` for tool names, `light`, `clipInput`, `snippet`, `summaryOf`, `interactionId`) |
| `lib/runtime.js` | Connect and reconnect, `runtime.hello`, list pushes, watch backfill and continuity, byte-by-byte streaming, asking the phone, "who answers first", per-request token usage and optional subscription-window reports |
| `lib/history.js` | Listing sessions by time window (`listHistory`): the window (`active_at`, then `updated_at`, then `started_at`), order, paging and cursor; used by `rt.history()` / `historyPage` (`docs/integrations/runtimes.md`, "History") |
| `lib/items.js` | Length limits in a page, the truncation note, stable interaction ids |
| `lib/wire.js` | The same JSON-line RPC as `server/internal/rpc` |
| `lib/loader.js` | The loader (`createLoader`) of a runtime installed inside another program: loads the implementation from a hash-named version directory; when the node brings a new version it fetches the package, verifies it, waits for the phone's cards, swaps it in, and goes back to the old one on failure (`docs/integrations/runtimes.md`, "Auto-update") |
| `lib/package.js` | Packages: the file list (the same as `runtimepkg.Packages` in personad), hashes, verification, atomic writes to the store; `installCurrent` (atomic switch of the fixed `current` path, for hosts that reload themselves, such as pi) and `selfPackage` (which package a module runs from and whether it is in the store) |
| `lib/listen.js` | The daemon endpoint: the handshake (token, subprotocol `persona-runtime.v1`) and the server's WebSocket frames, used by the listening mode of `runtime.js` |
| `testing/fake-node.js` | A fake node for unit tests (a temporary socket that records what the runtime said and calls it as a node does) |
| `testing/node-harness.mjs` | For end-to-end tests: starts a real `personad node --stdio` (isolated HOME) with the script acting as the hub |
| `testing/model.mjs` | A scriptable fake model for end-to-end tests (`scriptedModel(DSH_CHECKOUT)`: DSH's llm-mock-server, switching scripts by phase) |
| `testing/conformance.mjs` | Generic end-to-end acceptance (`conformance({...})`: text turns, approval, denial with a reason, questions, streaming, interrupt, push continuity) |
| `examples/echo-runtime.mjs` | The smallest complete runtime and the skeleton for a new one (`--listen <port>` runs it as a daemon) |
| `e2e/echo.mjs` | Acceptance template: `PERSONAD=<personad> node e2e/echo.mjs` |
| `e2e/daemon.mjs` | Daemon acceptance template: the echo example as a daemon that the node connects to from `daemons.json`: `PERSONAD=<personad> node e2e/daemon.mjs` |

`npm test` runs the unit tests. There are no dependencies and nothing to build: Node 22.19+.

In listening mode the echo example reads the daemon token from `PERSONA_DAEMON_TOKEN`.

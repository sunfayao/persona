# Wire contracts, compatibility and extensions

> What crosses a process boundary, how it stays compatible while parts are upgraded one after another, and what the
> schema, fixture and SDK directories are for. Read this before you change a type in a contract package, a JSON
> shape the web app or a node reads, anything under `schema/`, `spec/`, `sdk/` or `integrations/`. You usually arrive
> from core constraint 4 of `AGENTS.md`.

---

## Contract packages

| Package | What crosses | Between |
| --- | --- | --- |
| `internal/proto` | Client API and node protocol types, client feature flags | hub, node, web app |
| `internal/rpc` | JSON-lines RPC framing, lanes, resume | hub, managers, nodes |
| `internal/nodeproto` | Node handshake: version, announced capabilities, event and error codes | manager, node |
| `internal/assistantwire` | Bus methods, events, route list, snapshot, node method names | hub, assistant |
| `internal/devicewire`, `extwire` | The same for the device and extension managers; route scopes | hub, devices, extmgr |
| `internal/connectorwire`, `aiwire`, `dialogwire`, `repositorywire` | Inbox things, model configuration, terminal dialogs, repository identity | as named |
| `sdk/go/wire` | The extension protocol, generated from `schema/extension/` | extensions, extmgr |

A contract package holds types, constants and trivial pure helpers. It imports only the standard library and other
contract packages, and carries no business methods. A new `*wire` package is listed in
`archguard/testdata/contracts.txt`.

## Compatibility: parts of different versions always meet

The hub, the manager and each computer's node are replaced at different moments, and a web page loaded yesterday
talks to today's hub. So every contract change is **additive and gated**:

| Rule | In practice |
| --- | --- |
| No capability means an old peer | Ask `NodeAnnounced` or read the `features` of the hello. Never guess from a version or build string |
| Add, do not change | A new field, method, event or route. An old reader must still work without it; a new reader must still work when it is missing |
| Unknown is tolerated | Decoders skip unknown fields, flags and enum values. Generated TypeScript enums stay open |
| A new value of an existing parameter is ignored by an old peer | Read back what was applied before you report success |
| Names are data | Route patterns, RPC and tool names, JSON fields, error `code` strings and stored identifiers are never renamed. The old `jarvis` spellings (headers, tags, tool aliases, storage keys) are still read and, to a peer without `persona_contracts_v1`, still sent |
| Security is not relaxed for compatibility | An old peer that cannot prove something is refused |

Compatibility readers for a client that can no longer connect (the removed native apps) may be deleted. Readers for
data that can still be on the owner's machines may not (`04`).

## Samples and fixtures

| Directory | What it pins | How to update |
| --- | --- | --- |
| `spec/fixtures/wire/` | A JSON sample of every message the hub sends a client | `go test -count=1 ./internal/hub -run '^TestWireSamples$' -update`, then read the diff as the review of your contract change |
| `spec/fixtures/<topic>/` | Behaviour cases read by **both** Go tests and web tests (injected messages, deep links, permission modes, to-do views) | Edit the case file; both sides must pass |
| `server/internal/hub/testdata/route_scopes.txt` | Every route and the scope an extension token needs | `PERSONA_UPDATE_ROUTE_SCOPES=1 go test ./internal/hub -run TestRouteScopes` |

`web/src/lib/types.ts` is written by hand and holds only the fields the web app reads; nothing checks it against the
Go types. When you change a shape the web app reads, you change both, and the sample.

## Schemas that generate code

| Source of truth | Generates | Command |
| --- | --- | --- |
| `schema/wire/*.schema.json` | `web/src/lib/todo-view-wire.ts`, `recents-wire.ts`, `server/internal/assistantwire/todo_view_gen.go` | `python3 scripts/gen/extension_schema.py` (`--check` compares) |
| `schema/extension/*.json` | `sdk/go/wire/wire_gen.go`, `sdk/ts/src/wire.ts`, `web/src/lib/extension-wire.ts` | the same |
| `schema/configuration/` | Validation of the configuration files | read by tests |

You never edit a generated file; you edit the schema and run the generator. A fixture sample names exactly the fields
its schema has.

## Extensions

An extension is a package with a manifest (`persona-extension.json`): what it contributes, which permissions it asks
for, and optionally a program that speaks JSON frames or HTTP on a unix socket. Everything about extensions belongs
to the manager (`internal/extmgr`, `internal/extension`); the hub only relays their routes.

What the server guarantees to the owner, and what your change must keep true:

- An extension's token opens only the routes whose scope it was granted.
- Its secrets are decrypted only in the manager; its program is a sandboxed child of the manager with an egress
  proxy and no ambient authority.
- Its views are validated against the schema before the web app draws them.
- A crash or removal of an extension changes nothing a client sees outside that extension.

`sdk/go` is a **separate Go module** that imports nothing from the server; the server depends on it through a
`replace`. Code under `server/plugins` and built-in extensions use only the SDK and the standard library. `sdk/ts`
is the same contract for TypeScript; the trusted Deno adapter (`server/internal/extension/denohost/adapter.mjs`) is
built from it with `scripts/gen/deno_adapter.sh` and must be regenerated when `sdk/ts/src` changes.

## Reference documents

| Document | Use it for |
| --- | --- |
| `docs/protocol/protocol.md` | The routes and WebSocket frames the web app and nodes speak |
| `docs/protocol/agent-states.md` | What each session state means and when it changes |
| `docs/protocol/connector-protocol.md` | The protocol between the manager and a connector program |
| `docs/protocol/deep-links.md` | The link forms, pinned by `spec/fixtures/deep-links/` |
| `docs/integrations/runtimes.md` | The socket protocol and SDK of headless agent runtimes |
| `schema/extension/README.md`, `schema/wire/README.md`, `schema/configuration/README.md` | The map of each schema family |
| `spec/README.md` | What each fixture directory pins and who reads it |

Behaviour is what the server and its tests do. Where a document differs, the code is right: correct the document in
the same change, or delete the passage.

## Agent runtimes

`integrations/` holds the packages the node installs next to coding agents: the Claude Code plugin, the runtime SDK
and one package per headless runtime. `go generate ./internal/runtimepkg` packs them into the node; a size budget
test watches the result. A runtime package talks to the node only through the runtime SDK and the node's sockets;
it is versioned with the node that embeds it, so it needs no compatibility with older nodes, but the node must cope
with a runtime that has not been updated yet on a computer.

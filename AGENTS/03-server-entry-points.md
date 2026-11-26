# Server entry points: routes, node calls, events

> How a manager exposes an HTTP route to the web app, calls a node, and pushes an update. Read this before you add or
> change a route, an RPC method, an event or an error a client can see. You usually arrive from core constraint 3 of
> `AGENTS.md`.

---

## The idea: a domain registers its own entries

A route, a node method, a capability, a scope or a feature flag is declared **in the domain's own file** and appended
to a shared list from `init()` or from the root's constructor. You do not edit a central `switch`, and you do not
edit a hub allowlist to make a test pass.

| Entry | Declared in | Appended to | Served by |
| --- | --- | --- | --- |
| Assistant route | `assistantwire/<domain>.go`: `var xRoutes = []string{"METHOD /api/v1/..."}` | `assistantwire.Routes` in `init()` | `s.handle(pattern, fn)` from an `xRoutes()` method the constructor calls |
| Device or extension route | `devicewire`, `extwire` | their `Routes` lists | the manager's own mux |
| Node method | `assistantwire` (`Device...`) or `proto` (`Method...`) constant | `nodeRPC(name, (*Node).method)` in `init()` of the node's domain file | the node's method table |
| Node capability | `nodeproto/<domain>.go` constant | `nodeproto.Announced` | read by the manager with `NodeAnnounced(machine, capability)` |
| Extension-token scope of a route | `extwire/<domain>_scopes.go` | `extwire.RouteScopes` | checked by the hub before it relays |
| Client feature flag | `proto/features_<domain>.go` | `proto.Features` in `init()` | sent in the hub's `hello` |

`TestRoutesAreTheWireOnes` requires the assistant to serve exactly the wire lists; the hub relays those lists without
a per-route line.

## A route, end to end

1. Add the pattern to the domain's route list in the wire package. The hub now relays it and picks the owner from
   the path (`/machines/{mid}/`, a session, a conversation, a to-do).
2. Decide what an extension token may do with it: add a scope row, or leave it reachable with full credentials only.
   Then regenerate the record and read the diff:
   `PERSONA_UPDATE_ROUTE_SCOPES=1 go test ./internal/hub -run TestRouteScopes`.
3. Register the handler from the domain and write it in this shape:

```go
func (d *Domain) create(w http.ResponseWriter, r *http.Request) {
	if !userOnly(w, r) { // full-access device, not an extension token
		return
	}
	var in wire.Create
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, bodyMax)).Decode(&in); err != nil {
		apierr.Write(w, http.StatusBadRequest, err)
		return
	}
	out, err := d.store().Insert(r.Context(), in)
	if err != nil {
		apierr.WriteCode(w, http.StatusConflict, "already_exists", i18n.Err("assistant-widgets.exists"))
		return
	}
	apierr.WriteJSON(w, out)
}
```

| Rule | Detail |
| --- | --- |
| Bodies are bounded | Always `http.MaxBytesReader`; the hub and `extmgr` have `decodeJSONBody` helpers |
| Replies | `apierr.WriteJSON`; errors `apierr.Write(w, status, err)` or `apierr.WriteCode(w, status, "stable_code", err)` |
| Error codes are API | The web app branches on `code`. You never rename one; you add a new one |
| Text a person reads | `i18n.Err("namespace.key")`: the edge localizes it to the request's language (`08`). Validation messages for developers stay plain English |
| Who may call | Check the caller's role in the handler (`userOnly`, the to-do scope wrapper); the hub passes it in a header |
| The store | A narrow interface declared beside the handler and satisfied by `*db.DB` (`04`) |

## Calling a node

```go
// manager side
if !nodes.NodeAnnounced(machine, nodeproto.Widgets) { // an older node is simply not asked
	return errOlderNode
}
err := s.call(ctx, machine, wire.DeviceWidgetWrite, params, &out)

// node side, in the domain's file
func init() { nodeRPC(wire.DeviceWidgetWrite, (*Node).widgetWrite) }
```

- The parameter struct is typed and ignores unknown JSON fields, so a newer manager can add a field.
- A new method or a new meaning of a parameter is **gated by a capability** the node announces. A new value for an
  existing parameter is silently ignored by an older node: check what came back instead of assuming it was applied.
- `nodeproto.Version` is raised only for a change older peers cannot follow. Additions are capabilities.
- The node trusts nothing it is sent: it validates paths (`os.OpenRoot`), names and sizes itself, because the
  manager may be older, newer or wrong.
- You do not add a `case` to `Node.handleMethod`; the method table is the only registry.

## Pushing to the web app

| Kind | Call | Rule |
| --- | --- | --- |
| A live event | `s.event(ctx, map[string]any{"type": "...", ...})` | The type must be declared in the hub's list of manager message types; ids in it must belong to the caller |
| The whole state | `s.publish(ctx)` builds a snapshot with a sequence number | The hub keeps the newest; never send a partial snapshot |
| Hub to manager | Methods of the manager's `Handler` interface (`UserSpoke`, `TurnEnded`, `SessionsChanged`) | Queued; only deltas may be coalesced |

A new event type the web app must understand is a contract change: add its sample (`06`) and its handling in
`web/src/lib/store.ts`.

## Background loops and time

A loop starts in the root's `Run(ctx)`, wakes on a channel or a ticker, and returns on `ctx.Done()`. In a domain
package the ticker and `Now` come from the injected clock. In a handler you use the request's context; you never
store it.

## Traps

- To tell a client that left a field out from one that never knew the field, look at the keys of the request (a
  key is present even when its value is `false` or `null`), not at the decoded value.
- A number can be a credential. When you redact, exempt only keys that are plainly counts or flags.
- `max_tokens` means the output limit in some model lists and the whole context in others. Read only names that
  mean one thing (`max_output_tokens`, `max_completion_tokens`).
- A pooled connection over a long link can die silently. Ping an idle connection before you reuse it.

## What does not belong here

No business rule in a handler beyond decoding, authorization and calling the domain. No direct SQL outside
`internal/db`. No model call from the hub. No new route in `internal/hub` for a product feature.

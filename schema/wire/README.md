# Hub wire schemas

JSON Schemas for the wire shapes of domains the shared fixtures (`spec/fixtures/`) already pin (#7891 stage 5, #9117).
Each `<domain>.schema.json` generates the types with `scripts/gen/extension_schema.py`, the same generator and
subset as `schema/extension/README.md`; the file's `x-out` names the generated TypeScript (`ts`) and Go (`go`) files it
wants. A Go file takes the package of the directory it is written to (the generator checks that the files already there
say the same), gets no package doc and names its registry of definitions after the schema (`todoViewDefs`), since it
shares its package with hand-written code. Go is generated only for a schema whose `x-out` names it.

```sh
python3 scripts/gen/extension_schema.py           # after changing a schema
python3 scripts/gen/extension_schema.py --check   # compare only (also part of scripts/ci/guards.sh)
python3 scripts/test/test_wire_schema.py          # --check, fixture samples hold to the schema, no hand-written copy
```

| File | Domain | Generated |
| --- | --- | --- |
| `recents.schema.json` | `RecentSetup`, New Agent's recent setups (`spec/fixtures/recents`) | `web/src/lib/recents-wire.ts` |
| `todo-view.schema.json` | To-dos (#10226): a row's view (`TodoSummary`) and its asks (`OwnerAsk`), the list's counts (`TodoCounts`) and places (`TodoPlaces`) (`spec/fixtures/todo-view`, `spec/fixtures/todo-counts`); a to-do's conversation in rounds (`TodoTimeline`), its results (`TodoResult`) and what changed since the owner looked (`TodoRecap`) (#10465, `spec/fixtures/todo-rounds`), and what `GET /api/v1/todos/{id}` carries of them beside the row (`TodoItemDetail`); a decision push's `todo` (`TodoDecidePush`, #10465, `spec/fixtures/todo-push`) | `web/src/lib/todo-view-wire.ts`, `server/internal/assistantwire/todo_view_gen.go` |

Every sample a fixture holds for a definition must name exactly the schema's fields, at any depth, and every definition
must be reached by some sample (`test_wire_schema.py`). The Go test (`server/internal/assistantwire/todo_view_gen_test.go`)
writes each sample back through the generated type and compares the JSON values; Go also holds what it writes to the
schema with `server/internal/jsonsubset`, as the tests of the code that builds these values should, since a nil slice or
map writes `null`. A sample writes an optional boolean only when it is true and leaves an empty list or map out, as Go's
`omitempty` does, and a time as Go writes it.

Hand-written copies of a generated type may not return: the test fails when a tracked file declares one of a schema's
names outside its generated files, in every language its `x-out` names. `x-reserved` lists names a later stage will
define, guarded from now on.

# Extension SDK

The official SDK for writing Persona extensions. It has no dependencies and is self-contained: an extension repository copies a version of it (`scripts/gen/sync-sdk.sh`) and does not need this repository.

| Directory | Contents |
| --- | --- |
| `go/` | Go module `github.com/nwverse/persona/sdk/go`, standard library only. `wire/` holds the generated types, `ext/` the runtime (process protocol, UI routing, HTTP, environment variables) and `exttest/` the conformance checks an extension runs in its own tests. The hub references the same copy with `replace`. The root package `sdk` compiles the SDK and `bundle/sdk.tar.gz` into personad for `personad extension new` |
| `ts/` | TypeScript (Bun): `src/index.ts` is the runtime, `src/wire.ts` the generated types; an extension is built into one executable with `bun build --compile` |
| `templates/` | The templates of `personad extension new`: `go/` and `ts/`. `{{id}}` and `{{name}}` are replaced and the `.tmpl` suffix is removed |

Do not edit generated files. For `wire`, change `schema/extension/` and run `python3 scripts/gen/extension_schema.py` (see `schema/extension/README.md`). After changing `ts/` or `templates/`, run `python3 scripts/gen/sdk_bundle.py` to update `go/bundle/sdk.tar.gz`; `scripts/test/test_sdk_bundle.py` checks it.

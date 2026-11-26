# Persona

Persona is a self-hosted Agent product. The Go hub holds persistent state in Postgres and serves the web app. Computers connect through Persona Link and run their own Agents. The web app talks to the hub.

Assistant serves everyday users. Use plain language, simple defaults and immediate local feedback. Show Processing when the hub receives a message. Keep receipt, delivery and execution distinct, preserve recoverable drafts, and show waiting and failure. Agents is for people working directly with coding tools and is the home for specialist controls.

## Development

- `deploy` is the trunk. Start from it in an isolated `task/*` branch or worktree. Verify the change on the Linux devbox, then integrate it into `deploy`.
- Work directly in this fork. The workflow uses local builds and tests, with no issues, claims, pull requests, hosted CI or public releases.
- Write Conventional Commits in English. Omit attribution trailers.
- Deploy with `scripts/deploy/deploy.sh` to the Linux devbox and the Mac. Keep machine and account settings outside the repository. Use `config.example.json` as the deployment template.
- Keep credentials out of repository files, logs and commit messages. Configure the privacy hooks with `git config core.hooksPath scripts/hooks`.

## Build and verify

Run server verification on the Linux devbox with the test Postgres profile. Set `PERSONA_TEST_PROFILE` to its JSON path. Use the partitioned runner with `-p 12` for large packages and for full runs.

```bash
export GOPROXY='https://goproxy.byted.org|direct' GOFLAGS=-trimpath
cd server
go generate ./internal/runtimepkg
go build -tags "$(cat build-tags.txt)" ./...
go vet -tags "$(cat build-tags.txt)" ./...
GOMAXPROCS=8 python3 -I ../scripts/test/server_test_env.py --database-file "$PERSONA_TEST_PROFILE" -- \
  python3 -I ../scripts/test/go_test_partitioned.py go test -count=1 -p 12 -timeout 30m ./...
```

From the root, run `scripts/test/check-fast.sh` for formatting, the migration freeze, English-only prompts and privacy. For web changes, run `npm ci`, `npx tsc -b` and `npx vitest run` in `web/`. Inspect changed screens in light and dark using the mock hub and the browser checks in `web/e2e/`. Use an unused mock port and keep captures outside the checkout.

## Code conventions

- Write code comments, docs and prompts in English. Use the web and server locale resources for user-facing strings in English, Simplified Chinese and Japanese.
- Keep templates in `server/internal/prompts/`, one prompt per file. Use `{{lang .Language}}` or `{{lang .Lang}}` when a model writes for the owner. Keep prompt rendering and its goldens in sync.
- The hub is a communication center. Business logic belongs in the managers (`assistant`, `devices`, `extmgr`). Follow the package boundaries in [the server package plan](docs/plans/plan-server-packages.md) and the `archguard` tests.
- Keep server state in Postgres. Name new migrations by their UTC second, `$(date -u +%Y%m%d%H%M%S)_<name>.sql`. Keep applied migrations intact.
- Assistant and Agents are the primary destinations. Open Settings from the sidebar. Collapse tool execution by default while a turn runs and after it ends. Keep the latest reply segment visible during the turn, the final answer visible when it completes, and full details available on demand.
- Read the design principles and the relevant design-system section before changing a screen. Use the existing components and tokens, keep button labels on one line, and keep requests that need attention visible. Record any deviations with the verification results.

## Layout

| Path | Contents |
| --- | --- |
| `server/` | Go hub, manager, node and Persona Link |
| `web/` | Web client, admin UI, browser tests |
| `integrations/` | Agent runtimes and the runtime SDK |
| `sdk/`, `schema/`, `spec/` | Extension SDKs, schemas and shared contracts |
| `scripts/` | Local deployment, verification, generators and mock hub |
| `docs/` | Architecture, product behavior and operating instructions |

## Docs index

| Area | Read |
| --- | --- |
| Operations | [Configuration](docs/operations/config.md), [local testing](docs/operations/testing.md), [Persona Link](docs/operations/link.md), [hub restart](docs/operations/hub-restart.md) |
| Server | [Architecture](docs/server/architecture.md), [server operations](docs/server/server.md), [package plan](docs/plans/plan-server-packages.md) |
| Protocol | [HTTP and WebSocket](docs/protocol/protocol.md), [agent states](docs/protocol/agent-states.md), [connector](docs/protocol/connector-protocol.md), [compatibility](docs/protocol/rolling-upgrade-compatibility.md) |
| Product | [Assistant](docs/assistant/assistant.md), [prompts](docs/assistant/prompts.md), [Inbox](docs/assistant/inbox.md), [To-dos](docs/assistant/todos.md), [web](docs/web/web.md) |
| Design | [Principles](docs/design/PRINCIPLES.md), [design system](docs/design/DESIGN_SYSTEM.md) |
| Integrations | [Engines](docs/integrations/engines.md), [runtimes](docs/integrations/runtimes.md), [extensions](docs/extensions/extensions.md) |
| Lessons | [Lessons index](docs/lessons/README.md) |

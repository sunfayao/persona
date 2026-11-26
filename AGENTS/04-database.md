# Database and stored state

> How Postgres, migrations and files on disk are used. Read this before you add a table, a column, a query, a
> setting or a file under the data directory. You usually arrive from core constraint 5 of `AGENTS.md`.

---

## Two owners, two migration sets

The hub and the manager each own tables. Migrations are embedded SQL files in two directories:

| Set | Directory | Owner |
| --- | --- | --- |
| Hub | `server/internal/db/migrations/hub/` | sessions, devices, tokens, hub settings: what routing and authentication need |
| Manager | `server/internal/db/migrations/manager/` | everything the assistant, devices and extension managers store |

A table belongs to the side whose store reads and writes it. `TestStoresTouchTheirOwnTables` fails when a store
touches the other side's table. A table both sides seem to need is a design smell: one side asks the other over the
bus.

## Adding a migration

```bash
cd server/internal/db/migrations/manager   # or hub
touch "$(date -u +%Y%m%d%H%M%S)_widgets.sql"
```

| Rule | Why |
| --- | --- |
| The name is the UTC second it was written, lower case, `YYYYMMDDHHMMSS_<snake_case>.sql`, in exactly one set | Order across branches; checked by `TestMigrationSets` and `scripts/ci/migration_freeze.py` |
| An applied migration is never edited, renamed or deleted (comments and whitespace aside) | Databases in use have already run it |
| DDL is idempotent (`CREATE TABLE IF NOT EXISTS`, `ADD COLUMN IF NOT EXISTS`) | A restart in the middle must be safe |
| A new column has a default or is nullable; you do not drop or rename a column an older build reads | Hub and manager are replaced at different moments, and a rollback runs the old build on the new schema |
| Removing a feature does not remove its tables in the same change | Code goes first; a later migration may drop data once nothing reads it |

## Stores

- All SQL lives in `server/internal/db`, as methods on `*db.DB`, hand-written for pgx v5. There is no ORM and no
  generator.
- One file per subject (`db/widgets.go`): a column-list constant, a `scanWidget(row pgx.Row)` helper, queries with
  `$1` parameters, `INSERT ... ON CONFLICT ... RETURNING` for upserts, `= ANY($1)` for lists.
- A transaction is `pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error { ... })`.
- The package that uses a store declares the **narrow interface** it needs beside its code and asserts it:

```go
type widgetStore interface {
	InsertWidget(ctx context.Context, w db.Widget) (db.Widget, error)
}

var _ widgetStore = (*db.DB)(nil)
```

- `internal/db` is over the file limit of `archguard` by one file: a new subject that grows it needs an existing
  file to merge into, or a sub-package.
- The hub's store interface is frozen (`hub/testdata/hub_store_methods.txt`): the hub does not gain store methods
  for product features.

## Settings and secrets

| Kind | Where | Rule |
| --- | --- | --- |
| Manager settings | the `settings` table, described by the registry in `assistant/settings_registry.go` | A new plain setting is one registry entry plus its title key in the three locale files; the registry is also what the model's settings tool reads |
| Hub settings | the `hub_settings` table | Only what the hub itself needs |
| Secrets stored in settings | sealed with the owner's key | List the key in `db.SealedSettings`, or key rotation skips it (a guard test checks) |

## Files on disk

The data directory is `~/.persona` (`PERSONA_HOME`). `hub.json` belongs to the hub, `managers/assistant/manager.json`
to the manager, `link.json` to the connector; each side reads only its own. Code never reads another part's file,
and you never read the owner's live files while developing: tests run in a throw-away home (`09`).

Old JSON stores were imported into Postgres once and renamed `*.migrated`. Code that still reads an old stored shape
exists so that data written earlier keeps working; do not delete such a reader unless the data can no longer exist.

## Tests that need the database

A package that touches Postgres calls `dbtest.Main(m)` from its `TestMain`: every test process gets a database of its
own beside the one named by `PERSONA_TEST_DATABASE_URL` and drops it afterwards. Without that variable those tests
**skip silently**, so a green run where no test database is configured proves nothing (`09`).

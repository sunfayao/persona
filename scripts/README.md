# Local tools

Run these from the repository root unless a command says otherwise.

| Tool | Purpose |
| --- | --- |
| `scripts/deploy/deploy.sh [machine id or web]` | Build and deploy to the fork's configured Linux and macOS machines |
| `scripts/test/check-fast.sh [migration base]` | Check Go formatting, migrations, English prompt templates and privacy |
| `scripts/test/server_test_env.py` | Isolate server tests from the live profile and select the test database |
| `scripts/test/go_test_partitioned.py` | Run Go packages in bounded partitions with `-p 12` |
| `scripts/mock/mock_hub.py` | Serve synthetic hub data for the web app and browser tests |
| `scripts/gen/` | Regenerate SDKs, wire types and the Deno adapter from their sources |
| `scripts/privacy_check.py` and `scripts/hooks/` | Scan the worktree, staged changes, commits and push ranges for private values |
| `scripts/test/backup_restore_drill.py` | Verify a backup on an isolated test target |

Testing is described in `AGENTS/09-testing.md` and deployment in `AGENTS/10-deployment.md`; configuration in `docs/operations/config.md`.

# Persona

Persona is a self-hosted personal assistant and Agent workspace. The Go hub serves the web app and stores state in Postgres. Persona Link connects computers that run Agents.

`deploy` is the trunk. Make changes on `task/*` branches and build and test on the Linux devbox. See [local testing](docs/operations/testing.md) for the build, vet, partitioned Go suite and web checks.

Set up the Linux hub and Mac using [the deployment config](docs/operations/config.md) and [Persona Link](docs/operations/link.md). To deploy a verified build, run `scripts/deploy/deploy.sh` from the repository root.

[Agent guide](CLAUDE.md) · [Architecture](docs/server/architecture.md) · [Web app](docs/web/web.md) · [License](LICENSE)

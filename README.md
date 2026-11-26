# Persona

Persona is a self-hosted personal assistant and Agent workspace. A Go hub serves the web app and stores state in
Postgres; a node on each computer runs the coding agents; Persona Link keeps computers reachable.

`deploy` is the trunk. Changes are made on `task/*` branches and built and tested on the Linux build machine.

- [AGENTS.md](AGENTS.md): the rules for working in this repository, with the detailed files under [AGENTS/](AGENTS/).
- [docs/operations/config.md](docs/operations/config.md): the deployment and run-time configuration.
- [docs/protocol/protocol.md](docs/protocol/protocol.md): the contract between the hub, the web app and nodes.
- [LICENSE](LICENSE) · [Third-party notices](THIRD_PARTY_NOTICES.md)

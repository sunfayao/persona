<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/readme/banner-dark.svg">
  <img src="docs/assets/readme/banner-light.svg" alt="Persona: your SuperAgent. One place for every task." width="100%">
</picture>

English · [简体中文](README.zh-CN.md)

### One SuperAgent for everything you do.<br>It knows your world, runs your Agents, and goes wherever you go.

**Extension market: [Persona Extensions](https://github.com/nwverse/persona-extensions).** Connect data sources, add tools and skills to the assistant, and add views to the web app. [Add it in one line](#extensions)

[![agent: Claude Code](docs/assets/readme/badge-claude-code.svg)](docs/integrations/claude-integration.md) [![agent: Codex](docs/assets/readme/badge-codex.svg)](docs/integrations/codex.md) [![agent: opencode](docs/assets/readme/badge-opencode.svg)](docs/integrations/opencode.md) [![agent: pi](docs/assets/readme/badge-pi.svg)](docs/integrations/pi.md) [![agent: DSH](docs/assets/readme/badge-dsh.svg)](docs/integrations/dsh.md) [![agent: Antigravity CLI](docs/assets/readme/badge-antigravity.svg)](docs/integrations/runtimes.md#antigravity-cli)<br>
[![engine: herdr](docs/assets/readme/badge-herdr.svg)](docs/integrations/engines.md) [![engine: Orca](docs/assets/readme/badge-orca.svg)](docs/integrations/engines.md) [![engine: Herdr App](docs/assets/readme/badge-herdr-app.svg)](docs/integrations/engines.md) [![engine: Paseo](docs/assets/readme/badge-paseo.svg)](docs/integrations/paseo.md) [![engine: Cindy](docs/assets/readme/badge-cindy.svg)](docs/integrations/engines.md)

[Get started](#get-started) · [Features](#features) · [Extensions](#extensions) · [How it works](#how-it-works) · [Docs](#docs)

</div>

<br>

## Features

- **A SuperAgent, one entry for everything.** Ask in one chat. Persona pulls in your context, breaks the work down and hands it to Agents. [Assistant](docs/assistant/assistant.md)
- **Browser and computer, one session.** Claude Code, Codex, opencode, pi, DSH and Antigravity CLI keep running on your computers. Watch them live, answer and approve from the web app, wherever you are. Capabilities vary by runtime and engine. [Runtimes](docs/integrations/runtimes.md) · [Engines](docs/integrations/engines.md)
- **Extensions for everything.** Tools, Agent runtimes, data sources, channels and voice plug in, with an SDK and a market. [Extensions](docs/extensions/extensions.md) · [Extension market](https://github.com/nwverse/persona-extensions)
- **Inbox.** Lark, mail and GitHub land in one AI-sorted Inbox. [Inbox](docs/assistant/inbox.md)
- **Connectors and channels.** Bring in Lark, mail, GitHub, iMessage, Telegram and WeChat; talk to Persona from the web app, Lark, WeChat or by voice. [Connectors](docs/extensions/connectors.md) · [Channels](docs/extensions/channels.md)
- **A web app in three languages.** The web app is served by your hub and speaks English, Chinese and Japanese. [Web app](docs/web/web.md)

## Extensions

<a href="https://github.com/nwverse/persona-extensions#video-tour"><img src="https://raw.githubusercontent.com/nwverse/persona-extensions/main/docs/assets/promo-preview.webp" alt="Persona Extensions: add the market with one line, then pick the extensions you need" width="100%"></a>

[Persona Extensions](https://github.com/nwverse/persona-extensions) is the public extension market for Persona: connect data sources, add tools and skills to the assistant, and add views to the web app.

In the web app, open **Settings → Extensions → Extension Market**. Recent hubs already list it as the official market; if yours doesn't, add a market with this address (no GitHub token needed):

```text
github:nwverse/persona-extensions
```

Then install what you need, for example:

- **GitHub, Telegram and X**: what needs you lands in the Inbox, and the assistant can search and read them.
- **Home Assistant and Markets**: their own tabs in the web app, for your home's devices and automations and for a watchlist with price alerts.
- **Hacker News, LINUX DO and V2EX**: browse and read posts in the web app, and let the assistant look things up.
- **WeChat**: talk to Persona from WeChat.

<sub>[All extensions](https://github.com/nwverse/persona-extensions/tree/main/extensions) · [Video tour](https://github.com/nwverse/persona-extensions#video-tour) · [Write your own](docs/extensions/extensions.md)</sub>

## How it works

You run a small hub on an always-on machine; the web app talks only to it, and it reaches each of your computers. Agents run in herdr, Orca, Herdr App, Paseo, Cindy or T3 Code, or through a runtime integration. Engine and runtime capabilities vary; Cindy's read-only database fallback needs the host API for sending and interruption. Your data stays on your own hardware.
[Architecture](docs/server/architecture.md) · [Network and security](docs/operations/network.md)

## Get started

- **Just looking?** Run the synthetic mock hub against the web app: no server or account needed ([testing](docs/operations/testing.md)).
- **Your own setup:** one command from the [website](https://personaside.com/#start) installs the hub, then open the web app. See [Getting started](docs/operations/getting-started.md).
- **From source:** [config](docs/operations/config.md) · [deploy](docs/server/server.md) · [contributing](CONTRIBUTING.md) · [Agent guide](CLAUDE.md)

## Docs

[Getting started](docs/operations/getting-started.md) · [Architecture](docs/server/architecture.md) · [Protocol](docs/protocol/protocol.md) · [Docker](docs/operations/docker.md) · [Web](docs/web/web.md) · [Engines](docs/integrations/engines.md) · [Agent runtimes](docs/integrations/runtimes.md) · [All docs](CLAUDE.md#docs-index)

## Star History

<a href="https://www.star-history.com/#nwverse/persona&Date">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=nwverse/persona&type=Date&theme=dark" />
    <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=nwverse/persona&type=Date" />
    <img alt="Star History Chart" src="https://api.star-history.com/svg?repos=nwverse/persona&type=Date" />
  </picture>
</a>

## License

Persona is licensed under the [MIT License](LICENSE).

<sub>Third-party licenses: [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).</sub>

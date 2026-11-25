<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/readme/banner-dark.zh-CN.svg">
  <img src="docs/assets/readme/banner-light.zh-CN.svg" alt="Persona：你的 SuperAgent，一个入口解决所有问题。" width="100%">
</picture>

[English](README.md) · 简体中文

### 一个 SuperAgent，搞定你的所有事。<br>懂你的上下文，指挥你的 Agent，走到哪儿跟到哪儿。

**扩展市场：[Persona Extensions](https://github.com/nwverse/persona-extensions)。** 接入数据源，给助手加工具和技能，给网页应用加界面。[一行就能加上](#扩展)

[![agent: Claude Code](docs/assets/readme/badge-claude-code.svg)](docs/integrations/claude-integration.md) [![agent: Codex](docs/assets/readme/badge-codex.svg)](docs/integrations/codex.md) [![agent: opencode](docs/assets/readme/badge-opencode.svg)](docs/integrations/opencode.md) [![agent: pi](docs/assets/readme/badge-pi.svg)](docs/integrations/pi.md) [![agent: DSH](docs/assets/readme/badge-dsh.svg)](docs/integrations/dsh.md) [![agent: Antigravity CLI](docs/assets/readme/badge-antigravity.svg)](docs/integrations/runtimes.md#antigravity-cli)<br>
[![engine: herdr](docs/assets/readme/badge-herdr.svg)](docs/integrations/engines.md) [![engine: Orca](docs/assets/readme/badge-orca.svg)](docs/integrations/engines.md) [![engine: Herdr App](docs/assets/readme/badge-herdr-app.svg)](docs/integrations/engines.md) [![engine: Paseo](docs/assets/readme/badge-paseo.svg)](docs/integrations/paseo.md) [![engine: Cindy](docs/assets/readme/badge-cindy.svg)](docs/integrations/engines.md)

[开始使用](#开始使用) · [主要功能](#主要功能) · [扩展](#扩展) · [怎么工作](#怎么工作) · [文档](#文档)

</div>

<br>

## 主要功能

- **SuperAgent，一个入口解决所有问题**。在一个对话里说一句，Persona 带上你的上下文，拆好活，交给 Agent 去做。[助手](docs/assistant/assistant.md)
- **浏览器和电脑无缝衔接**。Claude Code、Codex、opencode、pi、DSH 和 Antigravity CLI 一直跑在你自己的电脑上。在网页应用里随时随地实时看、回答、审批；具体能力因运行时和引擎而异。[运行时](docs/integrations/runtimes.md) · [引擎](docs/integrations/engines.md)
- **完整的扩展系统**。工具、Agent 运行时、数据源、渠道、语音都能以扩展接入，有 SDK，也有扩展市场。[扩展](docs/extensions/extensions.md) · [扩展市场](https://github.com/nwverse/persona-extensions)
- **知识库和收件箱**。聊天、人、项目沉淀成共享记忆；飞书、邮件、GitHub 汇进一个由 AI 分拣的收件箱。[知识库](docs/assistant/knowledge.md) · [收件箱](docs/assistant/inbox.md)
- **连接器和渠道**。接入飞书、邮件、GitHub、iMessage、Telegram、微信；在网页应用、飞书、微信里或用语音都能找 Persona。[连接器](docs/extensions/connectors.md) · [渠道](docs/extensions/channels.md)
- **三种语言的网页应用**。网页应用由你的 hub 提供，支持中文、英文、日文。[网页应用](docs/web/web.md)

## 扩展

<a href="https://github.com/nwverse/persona-extensions/blob/main/README.zh-CN.md#视频介绍"><img src="https://raw.githubusercontent.com/nwverse/persona-extensions/main/docs/assets/promo-preview-zh.webp" alt="Persona Extensions 宣传片：加一行地址，再挑需要的扩展" width="100%"></a>

[Persona Extensions](https://github.com/nwverse/persona-extensions) 是 Persona 的公开扩展市场：接入数据源，给助手加工具和技能，给 App 加界面。

在网页应用里打开 **设置 → 扩展 → 扩展市场**。较新的 hub 已经把它列为官方市场；没有的话，用这个地址添加一个市场（不用 GitHub token）：

```text
github:nwverse/persona-extensions
```

然后挑需要的装上，比如：

- **GitHub、Telegram 和 X**：需要你处理的进收件箱，助手也能搜、能读。
- **Home Assistant 和行情**：网页应用里各多一个页签，管家里的设备和自动化，看自选和价格提醒。
- **Hacker News、LINUX DO 和 V2EX**：在网页应用里刷帖、读帖，助手也能帮你查。
- **微信**：在微信里和 Persona 对话。

<sub>[全部扩展](https://github.com/nwverse/persona-extensions/tree/main/extensions) · [视频介绍](https://github.com/nwverse/persona-extensions/blob/main/README.zh-CN.md#视频介绍) · [自己写扩展](docs/extensions/extensions.md)</sub>

## 怎么工作

在一台常开的机器上跑一个小小的 hub，网页应用只连它，它再连到你的每台电脑。Agent 跑在那里的 herdr、Orca、Herdr App、Paseo、Cindy 或 T3 Code 里，也可以通过运行时集成接入。各引擎和运行时的能力不同；Cindy 的数据库回退模式只读，发送和中断需要 host API。数据留在你自己的机器上。
[架构](docs/server/architecture.md) · [网络与安全](docs/operations/network.md)

## 开始使用

- **先看看**：把合成的 mock hub 和网页应用跑起来，不用服务器也不用账号（[测试](docs/operations/testing.md)）。
- **自己部署**：用[官网](https://personaside.com/#start)上的一行命令装好 hub，再打开网页应用。看 [开始使用](docs/operations/getting-started.md)。
- **从源码构建**：[配置](docs/operations/config.md) · [部署](docs/server/server.md) · [参与开发](CONTRIBUTING.md) · [Agent 指南](CLAUDE.md)

## 文档

[开始使用](docs/operations/getting-started.md) · [架构](docs/server/architecture.md) · [协议](docs/protocol/protocol.md) · [Docker](docs/operations/docker.md) · [网页](docs/web/web.md) · [引擎](docs/integrations/engines.md) · [Agent 运行时](docs/integrations/runtimes.md) · [全部文档](CLAUDE.md#docs-index)

## Star History

<a href="https://www.star-history.com/#nwverse/persona&Date">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=nwverse/persona&type=Date&theme=dark" />
    <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=nwverse/persona&type=Date" />
    <img alt="Star History Chart" src="https://api.star-history.com/svg?repos=nwverse/persona&type=Date" />
  </picture>
</a>

## License

Persona 采用 [MIT 许可证](LICENSE)。

<sub>第三方许可：[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。</sub>

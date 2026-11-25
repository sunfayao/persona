# Third-party notices

## Container security profiles

`deploy/docker/extension-seccomp.json` and `deploy/docker/persona-hub.apparmor` derive from `moby/profiles` at
`9ee3204f0d0cfa19ca2c6a7b0fcf5a2b353954ae` (Apache-2.0), with additional
namespace and mount setup rules for non-root bubblewrap. Its license is in
`deploy/docker/MOBY-LICENSE`.

## Password login WebAssembly client

`web/public/pake/client.wasm` uses `github.com/bytemare/opaque` v0.18.0 (MIT),
its transitive cryptographic dependencies (MIT and BSD), and the Go runtime (BSD).
The build collects their full license texts in a generated
`THIRD_PARTY_NOTICES.txt` under the web build's `pake` output, deployed beside the WASM client at
`/pake/THIRD_PARTY_NOTICES.txt`. The generated notices must accompany binary distributions.

## Brand marks

`web/src/ui/brands/` holds other products' marks (`web/src/views/inbox/lark.svg` is the Lark mark for the web inbox),
shown next to the service Persona connects to. The marks are trademarks of their owners; using them here does not imply
endorsement. They are drawn as published: not recolored (the one-color marks take the color of the words beside them), not
redrawn.

| Asset | Mark | Source | Terms |
| --- | --- | --- | --- |
| `web/src/ui/brands/claude.svg` | Claude (Anthropic), agent mark in the web client | Simple Icons `claude.svg` (CC0 for the file) | Anthropic's brand guidelines |
| `web/src/ui/brands/codex.svg` | OpenAI Blossom, the Codex agent mark in the web client | github.com/openai/openai-cookbook, `chatgpt-blossom-black.svg` (viewBox cropped to the mark) | openai.com/brand |
| `web/src/ui/brands/trae.svg` | TRAE, agent mark in the web client | Simple Icons `trae.svg` (CC0 for the file; viewBox cropped to the mark); upstream trae.ai | TRAE's trademark |
| `web/src/views/inbox/lark.svg` | 飞书 / Lark | github.com/larksuite/lark-samples, `public/svg/icon.svg` (viewBox cropped to the mark) | Lark user agreement; no public logo guidelines found |
| `web/src/ui/brands/github.svg` | GitHub mark in the web client | github.com/primer/octicons `icons/mark-github-24.svg` as published, a one-color template that takes the color of the words beside it (MIT for the file, license under "Primer Octicons" below) | GitHub's logo usage terms |
| `web/src/ui/brands/telegram.svg` | Telegram in the web client | telegram.org/img/t_logo.svg | Telegram's trademark |
| `web/src/ui/brands/opencode.svg` | OpenCode, agent mark | Simple Icons 16.36.0 `opencode.svg` (CC0 for the file; viewBox cropped to the mark); upstream github.com/anomalyco/opencode `packages/identity/mark.svg` (the project is MIT) | OpenCode's trademark |
| `web/src/ui/brands/cursor.svg` | Cursor, agent mark | Simple Icons 16.36.0 `cursor.svg` (CC0 for the file; viewBox cropped to the mark); upstream cursor.com/brand | Cursor's brand page |
| `web/src/ui/brands/gemini.svg` | Google Gemini, agent mark | Simple Icons 16.36.0 `googlegemini.svg` (CC0 for the file); upstream gemini.google.com, no brand kit found | Google brand terms |
| `web/src/ui/brands/pi.svg` | Pi, agent mark | Simple Icons 16.36.0 `pi.svg` (CC0 for the file); upstream pi.dev/favicon.svg, no licence text found | Pi's trademark |
| `web/src/ui/brands/grok.svg` | Grok, agent mark | Lobe Icons 1.104.0 `grok.svg` (MIT license below) | xAI's trademark |
| `web/src/ui/brands/agy.svg` | Antigravity CLI, agent mark | Lobe Icons 1.104.0 `antigravity.svg` (MIT license below) | Google brand terms |
| `web/src/ui/brands/dsh.svg` | DeepSeek Harness, agent mark | Simple Icons 16.36.0 `deepseek.svg` (CC0 for the file) | DeepSeek's trademark |
| `web/src/ui/brands/kimi.svg` | Kimi CLI, agent mark | Simple Icons 16.36.0 `kimi.svg` (CC0 for the file) | Moonshot AI's trademark |
| `web/src/ui/brands/copilot.svg` | GitHub Copilot, agent mark | Simple Icons 16.36.0 `githubcopilot.svg` (CC0 for the file) | GitHub's logo usage terms |

## Lobe Icons

https://github.com/lobehub/lobe-icons

MIT License

Copyright (c) 2023 LobeHub

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

## Web client

The web client's build writes `third-party-licenses.txt` next to its `index.html` (the hub serves it at
`/third-party-licenses.txt`). It lists every npm package `web/package-lock.json` installs for the client, which is every
package the build can include (xterm, pdf.js, noVNC, KaTeX, Mermaid and its dependencies, SheetJS and the rest), with its
version, the license it is used under, its source and the license text it is published with. `web/third-party-licenses.ts`
writes it, and the build fails (as does `web/third-party-licenses.test.ts` in CI's web tests) when a dependency is under a
license not accepted yet, offers a choice of licenses not made yet, no longer offers the license chosen, or comes without
a license file.

- Choices among offered licenses: DOMPurify (MPL-2.0 or Apache-2.0) is used under Apache-2.0, elkjs (EPL-2.0 or
  GPL-3.0-or-later) under EPL-2.0, JSZip (MIT or GPL-3.0-or-later) under MIT.
- Source of the copyleft code, bundled from the npm releases without changes to it: noVNC (MPL-2.0) at
  https://github.com/novnc/noVNC, elkjs (EPL-2.0) at https://github.com/kieler/elkjs, at the versions
  `third-party-licenses.txt` names.
- khroma's package.json names no license; its LICENSE file grants MIT. isarray, rehype-katex and remark-math (MIT) are
  published without a license file; their entries point at their source.
- Authors' email addresses are left out of the license texts: the published hub binary embeds the web client, and the
  release scan (`scripts/release/check_bundle_privacy.py`) refuses email addresses in it.

## Ovis

Parts of `server/internal/screen` (Claude Code input-box rules) are ported from Ovis, and
`server/internal/screen/testdata/claude-rewind-2.1.304-*` is taken from its screen fixtures.

https://github.com/devswha/ovis

MIT License

Copyright (c) 2026 devswha

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated
documentation files (the "Software"), to deal in the Software without restriction, including without limitation the
rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit
persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the
Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE
WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR
OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

## JetBrains Mono

The website carries the Latin subset of JetBrains Mono (v2.304) as WOFF2
(`site/public/fonts/jetbrains-mono-latin-400-normal.woff2`, `site/public/fonts/jetbrains-mono-latin-600-normal.woff2`),
with its license beside it (`site/public/fonts/OFL-JetBrainsMono.txt`).

https://github.com/JetBrains/JetBrainsMono

Copyright 2020 The JetBrains Mono Project Authors (https://github.com/JetBrains/JetBrainsMono)

This Font Software is licensed under the SIL Open Font License, Version 1.1.
This license is copied below, and is also available with a FAQ at:
https://scripts.sil.org/OFL


-----------------------------------------------------------
SIL OPEN FONT LICENSE Version 1.1 - 26 February 2007
-----------------------------------------------------------

PREAMBLE
The goals of the Open Font License (OFL) are to stimulate worldwide
development of collaborative font projects, to support the font creation
efforts of academic and linguistic communities, and to provide a free and
open framework in which fonts may be shared and improved in partnership
with others.

The OFL allows the licensed fonts to be used, studied, modified and
redistributed freely as long as they are not sold by themselves. The
fonts, including any derivative works, can be bundled, embedded, 
redistributed and/or sold with any software provided that any reserved
names are not used by derivative works. The fonts and derivatives,
however, cannot be released under any other type of license. The
requirement for fonts to remain under this license does not apply
to any document created using the fonts or their derivatives.

DEFINITIONS
"Font Software" refers to the set of files released by the Copyright
Holder(s) under this license and clearly marked as such. This may
include source files, build scripts and documentation.

"Reserved Font Name" refers to any names specified as such after the
copyright statement(s).

"Original Version" refers to the collection of Font Software components as
distributed by the Copyright Holder(s).

"Modified Version" refers to any derivative made by adding to, deleting,
or substituting -- in part or in whole -- any of the components of the
Original Version, by changing formats or by porting the Font Software to a
new environment.

"Author" refers to any designer, engineer, programmer, technical
writer or other person who contributed to the Font Software.

PERMISSION & CONDITIONS
Permission is hereby granted, free of charge, to any person obtaining
a copy of the Font Software, to use, study, copy, merge, embed, modify,
redistribute, and sell modified and unmodified copies of the Font
Software, subject to the following conditions:

1) Neither the Font Software nor any of its individual components,
in Original or Modified Versions, may be sold by itself.

2) Original or Modified Versions of the Font Software may be bundled,
redistributed and/or sold with any software, provided that each copy
contains the above copyright notice and this license. These can be
included either as stand-alone text files, human-readable headers or
in the appropriate machine-readable metadata fields within text or
binary files as long as those fields can be easily viewed by the user.

3) No Modified Version of the Font Software may use the Reserved Font
Name(s) unless explicit written permission is granted by the corresponding
Copyright Holder. This restriction only applies to the primary font name as
presented to the users.

4) The name(s) of the Copyright Holder(s) or the Author(s) of the Font
Software shall not be used to promote, endorse or advertise any
Modified Version, except to acknowledge the contribution(s) of the
Copyright Holder(s) and the Author(s) or with their explicit written
permission.

5) The Font Software, modified or unmodified, in part or in whole,
must be distributed entirely under this license, and must not be
distributed under any other license. The requirement for fonts to
remain under this license does not apply to any document created
using the Font Software.

TERMINATION
This license becomes null and void if any of the above conditions are
not met.

DISCLAIMER
THE FONT SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO ANY WARRANTIES OF
MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT
OF COPYRIGHT, PATENT, TRADEMARK, OR OTHER RIGHT. IN NO EVENT SHALL THE
COPYRIGHT HOLDER BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
INCLUDING ANY GENERAL, SPECIAL, INDIRECT, INCIDENTAL, OR CONSEQUENTIAL
DAMAGES, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
FROM, OUT OF THE USE OR INABILITY TO USE THE FONT SOFTWARE OR FROM
OTHER DEALINGS IN THE FONT SOFTWARE.

## Unbounded and Noto Serif CJK

Fonts under the same SIL Open Font License 1.1 as JetBrains Mono (its full text is above). Each keeps its license file
beside it.

| Font | Files | Copyright | License file |
| --- | --- | --- | --- |
| Unbounded (the website) | `site/public/fonts/unbounded-latin-800-normal.woff2` | Copyright 2022 The Unbounded Project Authors (https://github.com/googlefonts/unbounded) | `site/public/fonts/OFL-Unbounded.txt` |
| Noto Serif CJK SC Bold (the website's headlines, a subset made by `scripts/assets/make_site_assets.py`) | `site/public/fonts/persona-serif.woff` | Copyright 2017-2022 Adobe (http://www.adobe.com/), with Reserved Font Name 'Source' (https://github.com/notofonts/noto-cjk) | `site/public/fonts/OFL-NotoSerifCJK.txt` |

## KaTeX and Mermaid

The web client takes KaTeX and Mermaid as npm packages (see "Web client").

`mermaid.min.js` is Mermaid's own bundle and carries its dependencies, among them DOMPurify, used under Apache-2.0, and
elkjs, used under EPL-2.0 (source at https://github.com/kieler/elkjs). The web client's generated notices list the same
packages with their license texts.

| Component | Source | Copyright |
| --- | --- | --- |
| KaTeX | https://github.com/KaTeX/KaTeX | Copyright (c) 2013-2020 Khan Academy and other contributors |
| Mermaid | https://github.com/mermaid-js/mermaid | Copyright (c) 2014 - 2022 Knut Sveidqvist |

Both under the MIT License:

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated
documentation files (the "Software"), to deal in the Software without restriction, including without limitation the
rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit
persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the
Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE
WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR
OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

## BeautifulUI

The web client (`web/`) uses Beautiful UI (https://www.beautifului.dev/) design tokens, keyframes and component grammar directly
(`web/src/styles.css`, the views under `web/src/`), reimplemented in React against Persona's data (MIT, Copyright (c) 2026 Shane Levine).

BeautifulUI — MIT License, Copyright (c) 2026 ChrisPan

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated
documentation files (the "Software"), to deal in the Software without restriction, including without limitation the
rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit
persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the
Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE
WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR
OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

Beautiful UI — MIT License, Copyright (c) 2026 Shane Levine

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated
documentation files (the "Software"), to deal in the Software without restriction, including without limitation the
rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit
persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the
Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE
WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR
OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

## SheetJS

The web client opens spreadsheets with SheetJS Community Edition 0.20.3, kept as the tarball SheetJS publishes on
cdn.sheetjs.com (`web/vendor/xlsx-0.20.3.tgz`, docs/web/web.md) and listed in the web client's generated notices with its
license text.

https://sheetjs.com/

Copyright (C) 2012-present SheetJS LLC

Licensed under the Apache License, Version 2.0 (the "License"); you may not use these files except in compliance with
the License. You may obtain a copy of the License at http://www.apache.org/licenses/LICENSE-2.0. Unless required by
applicable law or agreed to in writing, software distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific language
governing permissions and limitations under the License.

## Lucide

The plan card's step marks (`web/src/views/chat/PlanCard.tsx`: circle, circle-check and the in-progress arc) and several
web icons drawn in `web/src/ui/icons.tsx` (sparkles, corner-down-right, workflow and others its comments name) take their
path data from Lucide, the icon set cindy uses.

https://github.com/lucide-icons/lucide

ISC License

Copyright (c) for portions of Lucide are held by Cole Bemis 2013-2022 as part of Feather (MIT). All other copyright (c) for Lucide are held by Lucide Contributors 2022.

Permission to use, copy, modify, and/or distribute this software for any
purpose with or without fee is hereby granted, provided that the above
copyright notice and this permission notice appear in all copies.

THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.

## Primer Octicons

GitHub's mark (`web/src/ui/brands/github.svg`) is the `mark-github` icon of Primer Octicons,
drawn as published.

https://github.com/primer/octicons

MIT License

Copyright (c) 2026 GitHub Inc.

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

## cindy

`server/internal/assistant/models_catalog.json` (the built-in model catalog that fills in what a gateway lists) is taken from
cindy's model registry (`packages/model-providers/catalog/model-registry.json`): for the Claude series and the GLM,
Kimi, DeepSeek V4, Qwen3, MiniMax, Seed and MiMo models cindy runs on Claude Code, only the id, name, context window,
thinking levels (low, medium, high, xhigh, max) and default level, as the registry states them, reformatted to a flat
list. The provider settings (the add-a-provider steps, the connection test and the model list it fetches) and the web
chat's subagent and workflow cards (`web/src/views/chat/TaskCard.tsx`, after cindy's `AgentTaskCard`: layout, states and
wording) and the main chat's hand-off cards with the way-back strip on an Agent's page (`web/src/views/assistant/Handoff.tsx`,
`web/src/views/chat/HandoffBack.tsx`, after cindy's `SessionHandoffCard` and its return pill: layout, states and wording)
and a lead's Team side panel (`web/src/views/chat/TeamPanel.tsx`, after cindy's Team tab, `OrcaWorkerPanel` and its worker
strip: layout, states, wording and when it opens) follow cindy's design. No code is copied for those.

The web chat's work summary follows cindy's chat at the owner's request (2026-11-12): the timeline inside an opened
summary (`web/src/lib/work-groups.ts`, after `createCompletedWorkGroup` in `messageWorkGroups.ts`), the thought rows
(`web/src/views/chat/ThinkingRow.tsx`, after `ThinkingActivityRow` in `WorkGroupBlock.tsx`), the tool rows
(`web/src/views/chat/ActionRow.tsx`, after `AgentActionRow` and `activityRowChrome.ts`) and the durations (`workDuration`,
after `formatDuration`). Two files are ported from cindy's code with changes: `web/src/lib/thinking-text.ts` (from
`packages/maker-shared/src/thinkingText.ts`) and `web/src/views/chat/ThinkingText.tsx` (from
`apps/desktop/src/renderer/components/chat/ThinkingText.tsx`), with tests from cindy's.

The web chat's plan (`web/src/views/chat/PlanCard.tsx`, #5156) is ported from cindy's `TodoListCard.tsx` with changes:
the inline card that opens in place (`InlinePlanCard`), the pill above the composer with its hover / pinned flyout and
Close Plan (`TodoListCard`), the progress ring's arithmetic and the in-progress arc's path and sizes.

The changed-files card (`web/src/views/chat/TurnChanges.tsx`, #5157) and the error notice (`web/src/ui/ErrorNotice.tsx`)
are ported from cindy's `TurnChangesCard.tsx` and `components/ui/error-notice.tsx` with changes: their layout, sizes and
states; the counting, review targets and fold data are Persona's own.

The web chat's bar under a turn's last reply (`web/src/views/chat/ReplyBar.tsx`, #5314) is ported from cindy's
`MessageActionBar.tsx` with changes: which reply has it, the order of its actions, its sizes, the linger before it fades
and when its copy check resets. `web/src/lib/relative-time.ts` is ported from cindy's `hooks/useRelativeTime.ts` with
changes (the wording of the time and its exact form); the actions behind the buttons are Persona's own.

The web composer follows cindy's design at the owner's request (2026-11-12): the toolbar, the + / @ panel and the
permission picker (`web/src/views/chat/Composer.tsx`, `ComposerPanel.tsx`, `PermissionPicker.tsx`, after cindy's
`ChatInput`, `ExtraDirsButton`, `AtMentionPanel`, `PermissionSelector` and `SendButton`), the `/` palette
(`SlashPalette.tsx`, after `SlashCommandPalette`) and the queued messages (`QueuePanel.tsx`, after `PendingQueuePanel`):
layout, states and wording, with the permission mode names and the placeholders' tone taken from cindy's English
strings. No code is copied for those.

The web chat's run status bar (`web/src/views/chat/RunningStatusBar.tsx` and its speed card
`web/src/views/chat/RunningSpeedCard.tsx`, #5312) is ported from cindy's `RunningStatusBar` (in `CCAgentSessionView.tsx`)
and `RunningTokenRatePopover.tsx` with changes: their layout, state words, the Done pop and the card's sparkline. The
speed history (`web/src/lib/token-rate.ts`) is ported from `packages/maker-shared/src/runningTokenRateHistory.ts` and
`formatRecentOutputTokenRate`, with tests from cindy's, changed to measure generation from the status line's token count
over time; the state words follow `localizeAgentStatus.ts` and `workingStatus.ts`, and are worked out from Persona's own
transcript items (`web/src/lib/run-step.ts`).

The web chat's top bar and its background work follow cindy's design at the owner's request (2026-11-12, #5421): the
top bar (`Header` in `web/src/views/chat/ChatView.tsx`, after cindy's `SessionContentHeader`, `AgentDeviceIndicator`,
`SessionRenameInput` and `GitContextBadge`), the goal bar over the composer (`web/src/views/chat/GoalBar.tsx`, after
`GoalIndicator`) and the Background tasks side panel (`web/src/views/chat/BackgroundPanel.tsx`, after the right sidebar's
`background-tasks` plugin, `BackgroundTasksBody` and `openBackgroundTasksTab`): layout, states, wording and how they
open. No code is copied for those. The "on another computer" arc on an Agent's mark (`RemoteSignal` in
`web/src/ui/AgentMark.tsx`) takes its path and placement from cindy's `components/icons/RemoteSourceMark.tsx`.

The handoff note an Agent session's conversation carries when it switches between Claude Code and Codex
(`server/internal/node/engine_handoff_build.go`, read by `server/internal/node/engine_handoff.go`, with its words in
`server/internal/prompts/agents/engine_handoff.md`, `engine_handoff_command.md` and `engine_handoff_speaker.md`) is
ported from cindy's `apps/desktop/src/main/maker-ipc/agentHandoff.ts` with changes: the same sections, framing and
bounds (the work ledger, the index of earlier turns, the latest turns word for word, the 16000-character cap), read
from Persona's transcript items; the transcript files on the computer take the place of cindy's history tools, and the
cap squeezes the data instead of cutting the text.

Switching an Agent between Claude Code and Codex in the web chat follows cindy's design at the owner's request
(2026-11-12): the "Switch Agent?" confirmation (`web/src/views/chat/EngineSwitchDialog.tsx`, after cindy's
`ConfirmDialog` and `agentSwitchConfirmation.ts`: layout, order, wording and when it asks) and the switch divider with
its handoff panel (`web/src/views/chat/EngineSwitchDivider.tsx`, after the agent switch card in cindy's `SystemCard.tsx`:
layout, states and wording). No code is copied for those.

What a turn cost on the web chat's reply bar (`web/src/views/chat/TurnUsage.tsx` and `web/src/lib/turn-cost.ts`, #5426)
is ported from cindy's `MessageActionBar.tsx` usage cell, `UsageCostPopover.tsx`, `lib/turnUsageTooltip.ts`
(`buildTurnUsageTooltipLines`, `formatPercent`, `getTurnUsageSuggestion`), `lib/usageFormat.ts` (`formatTurnCostMoney`)
and the per-model pricing of `main/usage/turnCostCalculator.ts`, with changes: the usage is summed in the browser from the
transcript items (`web/src/lib/turn-usage.ts`) and priced at the hub catalog's reference prices, the duration line uses
the work summary's time, and the Persona conversation's plain variant is Persona's own. The card's English, Chinese and
Japanese lines take cindy's `chat.messageActionBar` and `usageDetails` strings.

https://github.com/makecindy/cindy

Licensed under the Apache License, Version 2.0 (the "License"); you may not use these files except in compliance with
the License. You may obtain a copy of the License at http://www.apache.org/licenses/LICENSE-2.0. Unless required by
applicable law or agreed to in writing, software distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific language
governing permissions and limitations under the License.

# 交互作答夹具

这组夹具把界面操作转换为发给 hub 的 `Resolution`，由 Go 测试读取并按服务端协议类型核对；设计新增行为与现状明确分开。

## 文件与适用范围

| 文件 | 内容 |
| --- | --- |
| `questions.json` | 单选、多选、自填、多题翻页与提交门禁 |
| `recommended.json` | 推荐标记的显示和原文发送 |
| [`../question-review/review.json`](../question-review/review.json) | The review of a card of several questions: the click that opens it, its Submit and Previous, and the cards that skip it |
| `decisions.json` | 计划、授权、通知作答和交回终端 |
| `screen.json` | 新旧 hub 的终端选项、自由输入、步骤栏 |
| `transport.json` | 断网、成功、410、提示变化和失败后的卡片处理 |
| `unified.json` | 设计新增的意见互斥、全文阅读门禁、状态门禁、原因长度和 MCP 协议缺口；「总是允许」只给 Claude 的卡或带 `always_label` 的卡 |

`ios_current` 是原有客户端的现状断言（用例沿用此名）；`unified` 是待落地的设计断言。实现统一版时，沿用现状用例中没有被明确替代的规则，并应用 `unified.json` 与 [审批策略表](../approval/README.md) 的覆盖规则。不要要求两个 profile 对冲突场景同时成立。

## JSON 结构

顶层是 `schema_version: 1` 和 `cases` 数组；每条包含唯一的英文 snake_case `name`、中文 `description`、出处数组 `source`、`input`、`expected`。

- `from_test`：直接采用现有单测断言，注明测试名；合成的协议包装不属于原单测断言。
- `derived: true`：从实现补充的边界。
- `design_change: true`：设计要求，尚无实现可作运行对照；不能把它当成已有行为。
- `input.profile`：`ios_current` 或 `unified`。
- `input.interaction`：现有协议的 Interaction；标识均为合成占位值，终端 `dialog` 的实际结构以 Go 类型为准。
- `input.operation`：测试驱动的界面操作，不是网络字段；除明确列出的上下文外，默认请求待处理、连接可用、尚未发送。
- `expected.allowed`：此刻能否触发该操作；允许翻页或更新选择不代表已经发送。
- `expected.resolution`：POST `/api/v1/interactions/{id}/resolve` 的完整 JSON 请求体；`null` 表示不发请求，不能发送 JSON null 代替。
- `expected.next_page`、`picks`、`display`、`dialog`、`card_present`：仅在相关用例出现的补充断言。

`operation.picks` 是按问题原文索引的字符串数组，语义是集合；不能按输入数组次序拼答案。`other` 同样按问题原文索引。`page` 和 `option_index` 从零开始。`pick_option` 表示点击，`advance` 表示下一题／提交，`submit` 表示点击可用的提交入口；状态是点击前的快照。末题已答、前题漏答时，操作允许执行，但只跳回漏答题，`resolution` 仍为 null。

**The review step (#2970, #8483).** A card of several questions shows a review before its answers go: with every question answered, Submit (or the button on an earlier question) only opens the review, and the review's own Submit sends the answers. `operation.reviewing` is whether the review shows before the click (default false), and `expected.reviewing` whether it shows after. `confirm_review` is the review's Submit and `review_back` its Previous; both are disabled while the answer is on its way, and the options and the form's footer are not reachable while the review shows. `../question-review/review.json` asserts these transitions, starting with the first click that sends nothing. It lives outside this folder because the answer replay reads every file here and does not model the review. `questions.json` stays a synchronous answer-construction contract: two of its cases (`multiple_questions_send_original_keys`, `all_answered_can_submit_from_earlier_page`) expect what the final confirmation sends. Every other question case must leave the review closed.

`screen_option` 的 `option` 是终端给的字符串键，不是数组下标。带 hub `dialog` 时发送 `option`，不把 `rev` 塞进 Resolution；hub 从待处理 Interaction 取版本并在 node 按键前校验。旧 hub 没有 `dialog` 时使用 `keys`；自填按 `pre_keys → text → keys` 执行。原始终端输入只禁用空字符串，空白字符照发；自填选项会去掉首尾空白。

The `resolve` operation carries `api_available`, `network_up` and `outcome`; `resolution` is the request actually attempted, and an HTTP failure does not turn an attempted request into null. A 500 is not a gap and is not retried. As on the hub (`server/internal/assistant/fleet_ix_screen.go`, `server/internal/assistant/fleet_ix_codex.go`), `prompt_changed` ends the card before its 409 comes back, which is why the old card is not put back (#8915).

所有可选协议字段按实际编码保留或省略：例如拒绝必须是 `allow: false`，不能省略；对话页空计划意见编码为 `feedback: ""`。Go 的 `omitempty` 会省略空字符串，对照测试只允许这些已知的空字符串差异，不能据此删掉 false。

## 已确认的差异

`unified.json` 的设计新增规则：

| 场景 | 统一版 |
| --- | --- |
| 拒绝原因 | 原因通过 `message` 发送，非空时禁用允许和总是允许，最多 4000 字 |
| 计划意见 | 有意见就禁用两个批准入口，清空后恢复 |
| 全部放行 | 对话页读过全文后才允许 `bypassPermissions` |
| 状态门禁 | 只有 `pending` 可开始新作答；`unknown` 先查请求，不重发答案 |
| MCP 表单 | P1 需要协议和能力协商；当前只记录提交不可用，交回终端使用已有 `dismiss` |

原因长度用例使用合成 ASCII，避免擅自规定“字”按 UTF-16、码点还是字素计数；Unicode 计数和原因／意见的空白归一化仍需产品规格补充。夹具中的非空原因按原文发送。空意见打回沿用现状。`default` 虽被协议列为计划模式，计划表单没有对应按钮，不能凭协议增加界面入口。

`transport.json` 保留现状：网络不可用不移走卡片；正常提交先移走；普通失败恢复；410 和 `prompt_changed` 不恢复旧卡片。`../../../docs/protocol/protocol.md` 的 `answering → answered` 和“已回传你的选择”属于后续状态机，不应拿当前乐观移除行为冒充已实现。

## 对照方法

从仓库根目录执行：

```sh
cd server
go test -count=1 -p 1 -run 'TestResolutionFixture' -v ./internal/proto
```

`TestResolutionFixtures`（`server/internal/proto/resolution_fixtures_test.go`）读取两个领域的全部 JSON，检查来源标记、唯一名称和格式，用真实 `proto.Interaction`、`proto.Resolution` 解码并核对重编码字段；额外禁止未知 Resolution 字段，检查问题原文键、明确的计划模式和终端选项。带 hub dialog 的合成屏幕还要由真实 `screen.ParseDialog` 解析出同一份结构及版本。

验证范围限于协议解码和格式检查；设计新增用例只做协议和规格一致性校验。

## 规则与用例

下面每行是一条行为规则、来源和覆盖它的用例；源文件函数名是定位依据，行号不作为稳定契约。

### decisions.json

| 规则 | 用例 |
| --- | --- |
| 对话页全部放行使用 bypassPermissions。 | `plan_approve_all` |
| 对话页只接受编辑使用 acceptEdits。 | `plan_approve_edits` |
| 对话页打回保留意见的首尾空白。 | `plan_reject_preserves_whitespace` |
| 对话页允许不填意见直接打回。 | `plan_reject_empty` |
| 允许一次只发送 allow。 | `permission_allow` |
| 当前授权面板拒绝不附带原因。 | `permission_deny` |
| 本会话总是允许附带 always。 | `permission_always` |
| 没有会话标识时不提供总是允许。 | `permission_always_without_session` |
| 管理器说不会记住（`always_honored: false`）时不提供总是允许。 | `permission_always_not_honored` |
| 管理器说会记住（`always_honored: true`）时，别的 Agent 的卡没有 `always_label` 也提供总是允许。 | `permission_always_honored_agent` |
| 打回编辑状态下批准按钮不显示。 | `plan_rejecting_hides_approve` |
| 取消打回后旧意见仍保留且现有批准按钮可用。 | `plan_cancel_retains_feedback_allows_approve` |
| 通知动作映射为现有 Resolution。 | `notification_ix_allow` |
| 通知动作映射为现有 Resolution。 | `notification_ix_deny` |
| 通知动作映射为现有 Resolution。 | `notification_ix_approve_plan` |
| 通知动作映射为现有 Resolution。 | `notification_ix_reject_plan` |
| 通知打回缺少文字时发送空意见。 | `notification_reject_without_text` |
| 交给电脑处理发送 dismiss 而不表示拒绝。 | `dismiss_question` |
| 交给电脑处理发送 dismiss 而不表示拒绝。 | `dismiss_plan` |
| 交给电脑处理发送 dismiss 而不表示拒绝。 | `dismiss_permission` |
| 交给电脑处理发送 dismiss 而不表示拒绝。 | `dismiss_screen` |

### questions.json

| 规则 | 用例 |
| --- | --- |
| 单题单选点击后立即发送带推荐后缀的原文。 | `single_tap_sends_original_label` |
| 点选单选项会清空自填答案。 | `single_pick_clears_other` |
| 其他回答去掉首尾空白后发送。 | `other_answer_trimmed` |
| 没有选择也没有自填答案时不能提交。 | `empty_answer_cannot_submit` |
| 只有空白的其他回答不能提交。 | `blank_other_cannot_submit` |
| 多选按选项原顺序拼接而非点击顺序。 | `multiple_picks_follow_option_order` |
| 多选的自填答案覆盖已选项而不与选项合并。 | `multiple_other_overrides_picks` |
| 空白自填内容不会覆盖已选项。 | `blank_other_uses_picks` |
| 多选点击选项只更新选择而不发送。 | `multiple_pick_waits_for_submit` |
| 再次点击多选项会取消选择。 | `multiple_pick_toggles_off` |
| 多题提交以问题原文为键并包含全部答案。 | `multiple_questions_send_original_keys` |
| 末题已答但前题漏答时跳回漏答题且不发送。 | `last_page_missing_answer_returns_to_gap` |
| 多题当前题已答时下一题只翻页。 | `next_page_does_not_send` |
| 全部答完后可从前一题直接提交。 | `all_answered_can_submit_from_earlier_page` |
| 正在发送时选项按钮不可再点。 | `sending_disables_option` |

### question-review/review.json

| Rule | Case |
| --- | --- |
| On a card of several questions, Submit on the last one opens the review and sends nothing yet. | `stepped_submit_opens_review` |
| With every question answered, the button on an earlier question also opens the review. | `stepped_advance_from_earlier_page_opens_review` |
| A question left unanswered goes back to it; the review does not open. | `stepped_missing_answer_skips_review` |
| The review's Submit sends every answer once, keyed by the original question. | `review_confirm_sends_all_answers` |
| While the answer is on its way the review's buttons are disabled. | `review_confirm_while_sending_sends_nothing` |
| Previous on the review closes it without sending. | `review_back_returns_to_questions` |
| The review replaces the options. | `review_hides_options` |
| A card of one question has no review. | `single_question_sends_without_review` |

### recommended.json

| 规则 | 用例 |
| --- | --- |
| 推荐标记仅按已支持的结尾格式显示，发送保留原文。 | `recommended_spaced` |
| 推荐标记仅按已支持的结尾格式显示，发送保留原文。 | `recommended_unspaced` |
| 推荐标记仅按已支持的结尾格式显示，发送保留原文。 | `recommended_chinese_fullwidth` |
| 推荐标记仅按已支持的结尾格式显示，发送保留原文。 | `recommended_chinese_ascii` |
| 推荐标记仅按已支持的结尾格式显示，发送保留原文。 | `recommended_trims_prefix_space` |
| 推荐标记仅按已支持的结尾格式显示，发送保留原文。 | `recommended_case_sensitive` |
| 推荐标记仅按已支持的结尾格式显示，发送保留原文。 | `recommended_suffix_only` |
| 推荐标记仅按已支持的结尾格式显示，发送保留原文。 | `recommended_trailing_space` |

### screen.json

| 规则 | 用例 |
| --- | --- |
| 终端普通选项按 hub 能力构造回答。 | `hub_screen_choice` |
| 终端自填选项按 hub 能力构造回答。 | `hub_screen_custom` |
| 终端转回对话选项按 hub 能力构造回答。 | `hub_screen_chat` |
| 终端普通选项按 hub 能力构造回答。 | `legacy_screen_choice` |
| 终端自填选项按 hub 能力构造回答。 | `legacy_screen_custom` |
| 终端转回对话选项按 hub 能力构造回答。 | `legacy_screen_chat` |
| 自填选项只有空白时不可发送。 | `screen_custom_blank_blocked` |
| 原始终端输入按原文发送，仅空字符串禁用。 | `raw_terminal_text` |
| 原始终端输入按原文发送，仅空字符串禁用。 | `raw_terminal_empty` |
| 原始终端输入按原文发送，仅空字符串禁用。 | `raw_terminal_whitespace` |
| 原始终端按键按给定顺序发送。 | `raw_terminal_keys` |
| 终端多题标签栏解析为步骤并保留选项原文。 | `screen_question_steps` |
| 没有编号选项时不解析成对话框。 | `screen_without_numbered_options` |

### transport.json

| 规则 | 来源 | 用例 |
| --- | --- | --- |
| 作答回传根据连接条件和 hub 结果保留或移除交互卡片。 | `docs/protocol/protocol.md §3.4、§5.3` | `resolve_no_client` |
| 作答回传根据连接条件和 hub 结果保留或移除交互卡片。 | `docs/protocol/protocol.md §3.4、§5.3` | `resolve_network_down` |
| 作答回传根据连接条件和 hub 结果保留或移除交互卡片。 | `docs/protocol/protocol.md §3.4、§5.3` | `resolve_success` |
| 作答回传根据连接条件和 hub 结果保留或移除交互卡片。 | `docs/protocol/protocol.md §3.4、§5.3` | `resolve_gone` |
| 作答回传根据连接条件和 hub 结果保留或移除交互卡片。 | `docs/protocol/protocol.md §3.4、§5.3` | `resolve_prompt_changed` |
| 作答回传根据连接条件和 hub 结果保留或移除交互卡片。 | `docs/protocol/protocol.md §3.4、§5.3` | `resolve_error_restores_card` |

### unified.json

| 规则 | 来源 | 用例 |
| --- | --- | --- |
| 填写拒绝原因后不能允许一次。 | `docs/protocol/protocol.md §3.4、§5.3`；`docs/protocol/protocol.md：Resolution` | `reason_disables_allow` |
| 填写拒绝原因后不能本会话总是允许。 | `docs/protocol/protocol.md §3.4、§5.3`；`docs/protocol/protocol.md：Resolution` | `reason_disables_always` |
| 拒绝原因通过 message 原样传给 hub。 | `docs/protocol/protocol.md §3.4、§5.3`；`docs/protocol/protocol.md：Resolution` | `deny_with_reason` |
| 清空拒绝原因后恢复允许。 | `docs/protocol/protocol.md §3.4、§5.3`；`docs/protocol/protocol.md：Resolution` | `clear_reason_enables_allow` |
| 填写计划意见后批准按钮不可用。 | `docs/protocol/protocol.md §3.4、§5.3`；`docs/protocol/protocol.md：Resolution` | `feedback_disables_approve_all` |
| 填写计划意见后批准按钮不可用。 | `docs/protocol/protocol.md §3.4、§5.3`；`docs/protocol/protocol.md：Resolution` | `feedback_disables_approve_edits` |
| 填写计划意见后通过打回发送。 | `docs/protocol/protocol.md §3.4、§5.3`；`docs/protocol/protocol.md：Resolution` | `feedback_rejects_plan` |
| 没有读过全文时不能全部放行。 | `docs/protocol/protocol.md §3.4、§5.3`；`docs/protocol/protocol.md：Resolution` | `unread_plan_blocks_bypass` |
| 对话页读过全文且无意见时可全部放行。 | `docs/protocol/protocol.md §3.4、§5.3`；`docs/protocol/protocol.md：Resolution` | `read_plan_allows_bypass` |
| 无意见时可以只自动接受编辑。 | `docs/protocol/protocol.md §3.4、§5.3`；`docs/protocol/protocol.md：Resolution` | `unified_accept_edits` |
| 非待处理状态不能再次提交作答。 | `docs/protocol/protocol.md §3.4、§5.3`；`docs/protocol/protocol.md：Resolution` | `state_answering_blocks_submit` |
| 非待处理状态不能再次提交作答。 | `docs/protocol/protocol.md §3.4、§5.3`；`docs/protocol/protocol.md：Resolution` | `state_answered_blocks_submit` |
| 非待处理状态不能再次提交作答。 | `docs/protocol/protocol.md §3.4、§5.3`；`docs/protocol/protocol.md：Resolution` | `state_expired_blocks_submit` |
| 非待处理状态不能再次提交作答。 | `docs/protocol/protocol.md §3.4、§5.3`；`docs/protocol/protocol.md：Resolution` | `state_resolvedelsewhere_blocks_submit` |
| 非待处理状态不能再次提交作答。 | `docs/protocol/protocol.md §3.4、§5.3`；`docs/protocol/protocol.md：Resolution` | `state_handedtoterminal_blocks_submit` |
| 非待处理状态不能再次提交作答。 | `docs/protocol/protocol.md §3.4、§5.3`；`docs/protocol/protocol.md：Resolution` | `state_unknown_blocks_submit` |
| 待处理状态允许作答。 | `docs/protocol/protocol.md §3.4、§5.3`；`docs/protocol/protocol.md：Resolution` | `state_pending_allows_submit` |
| 当前协议尚不能编码 MCP 表单作答，应走终端降级。 | `docs/protocol/protocol.md §3.4、§5.3`；`docs/protocol/protocol.md：Resolution` | `mcp_form_requires_protocol_extension` |
| 拒绝原因按设计限制最多四千字。 | `docs/protocol/protocol.md §3.4、§5.3` | `reason_length_at_limit` |
| 拒绝原因按设计限制最多四千字。 | `docs/protocol/protocol.md §3.4、§5.3` | `reason_length_above_limit` |

## 新增用例

先定位对应的协议类型或设计文档章节，确认属于现状还是设计新增；在对应主题文件追加唯一用例，并同步本页规则表。协议字段按 Go 定义命名，不把页面状态写进 Resolution。直接搬断言填写 `from_test`；实现边界填写 `derived`；新设计填写 `design_change`，注明缺少现有实现的部分。

执行上面的对照命令，再跑服务端要求的检查。不要用临时驱动重写另一套答案算法作为期望值来源。提交前运行隐私扫描；数据只用编造内容和占位标识。

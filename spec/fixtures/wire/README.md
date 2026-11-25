# 线上 JSON 样本

这里的“线上”指 hub 实际发送的 JSON 结构，数据全部编造，没有连接线上 hub 或读取真实会话。样本由 [wire_samples_test.go](../../../server/internal/hub/wire_samples_test.go) 导出，作为 hub 线上格式的 golden 样本。

依据是 [hub-changes.md 4.13](../../../docs/archive/redesign/hub-changes.md)、[设计总纲 D1、D4、D5](../../../docs/archive/redesign/README.md) 和 [协议文档](../../../docs/protocol/protocol.md)。设计中的目标行为与现状有差异时，见下文。

## 文件与接口

| 文件 | 接口或事件 | 导出方式 |
| --- | --- | --- |
| [connection.json](connection.json) | WS `hello`、`revs`；GET `/api/v1/health` | Calls `Hub.hello` and `Hub.health`, then puts the fixed `wireFeatures` in place of the hub's feature list; revs uses the real envelope and `client.sendJSON` |
| [sessions.json](sessions.json) | GET `/api/v1/sessions`；WS `sessions` | `proto.Session` 经 `agentState`、`fillStateMarks` 后，用列表及事件的 map 结构发送 |
| [machines.json](machines.json) | GET `/api/v1/machines`；WS `machines` | 调用 `Machine.snapshot`，再经 `writeJSON` 或 `sendJSON` |
| [machines-concern.json](machines-concern.json) | Anonymous machines for concern-grid tests | `writeJSON` encodes the machine input |
| [items.json](items.json) | GET `/api/v1/sessions/{mid}/{sid}/items` | `proto.Item`、`WithoutImageData`、`FoldWork`；增量和展开页调用 `Hub.sessionItems` |
| [item_events.json](item_events.json) | WS `items`，含订阅后的补发 | 调用 `Hub.itemsToSubscribers`、`Hub.sendBacklog`，读取客户端发送队列 |
| [interactions.json](interactions.json) | GET `/api/v1/interactions`；WS `interaction`、`interaction_resolved` | `proto.Interaction` 使用实际列表或信封结构；屏幕经 `ParseDialog`、`SecretPrompt` 处理 |
| [fleet.json](fleet.json) | GET `/api/v1/fleet`；WS `fleet` | 调用 `buildFleet`，再使用实际快照或信封结构 |
| [inbox.json](inbox.json) | GET `/api/v1/inbox` | `InboxItem.listed` 清理列表中的内部字段，再经 `writeJSON` |
| [feed.json](feed.json) | GET `/api/v1/assistant/conversations/{id}/feed` | `answeredBody` 由 `answeredItems` 等生成，再经 `writeJSON` |
| [queue.json](queue.json) | POST `/api/v1/sessions/{mid}/{sid}/queue` 的应答（Persona 对话取回、删除、移动排队消息） | `proto.QueueEdit` 按 node 的编码生成，再经 `writeJSON` 写出；hub 对此应答原样转发 |
| [errors.json](errors.json) | 未鉴权请求的 401；发送及屏幕操作拒绝的 409；a message with too many pictures, 413 `images_over_limit` | 调用 `Hub.auth`、`callErr`、`apierr.ImagesOverLimit`，同时断言 HTTP 状态 |
| [compatibility.json](compatibility.json) | 上述类型的旧格式、未知字段及异常类型 | 用 Go 编码器构造兼容性输入，异常输入不代表 hub 正常输出 |

`wireNode` 通过内存管道提供固定的 `session.items` RPC 响应；hub 仍执行真实的分页包装、图片处理和补发逻辑。所有样本都没有启动完整 hub、node、herdr，不监听端口，也不访问数据库。

时间固定为 `2026-01-02T03:04:05Z`，零时间用 Go 原值。标识、域名、路径、消息正文均为占位数据；示例核数、内存容量等也是编造输入，不代表实测结果。

The export replaces two values after the production builders return: `hello.client_id`, which is random, and the feature list of `hello` and `/health`, which becomes the fixed `wireFeatures` (rules below).

## 规则与用例

表中路径均相对仓库根目录，函数和类型名可直接定位；每个 JSON 用例的 `source` 另有具体出处。

| 规则 | 来源 | 覆盖用例 |
| --- | --- | --- |
| hello and a trusted health carry the hub's feature list under `features`. The samples carry a fixed list instead (`wireFeatures` in `wire_samples_test.go`): a few long-lived flags the clients read and one no client knows, `example_unknown_feature`, which decoders keep reading past. Adding or removing a feature flag changes no sample; `TestHelloAndHealthCarryTheHubsFeatures` checks that both carry exactly the hub's `features()`, `proto.Features` first and in order (#11022). | `server/internal/hub/hello.go`：`hello`、`health`；`server/internal/hub/hello_features_test.go` | `hello_features`、`health_features` |
| revs 是会话标识到整数版本号的映射。 | 见用例的 `source` | `revs_changed_sessions` |
| 会话列表保留模式、队列、后台任务、子任务和循环信息，事件用信封中的机器标识设置会话归属。 | `server/internal/proto/session.go`：`Session` | `sessions_list`、`sessions_event` |
| 完整会话行携带等待下一条消息的服务商切换（`pending_switch`）和 Persona 对话自己选的模型（`model_pick`）；摘要行保留 `model_pick`，省略启动器和 `pending_switch`，客户端用此前完整行里的值。 | `server/internal/proto/session.go`：`PendingSwitch`、`ModelPick`；`server/internal/hub/sessions_list.go`：`runtimeSummary` | `sessions_model_switch`、`sessions_summary_model_pick` |
| Persona 对话的 `queued` 先列用户自己的消息，最后 `queued_relayed` 条来自其他对话或系统，客户端只展示和拷贝这几条。 | `server/internal/proto/session.go`：`Session`；`server/internal/assistant/fleet_sessions.go`：`Service.decorateInventory` | `sessions_assistant_queue` |
| Persona 对话编辑队列的应答是 `{queue, images}`：`queue` 是编辑前排队的消息，`images[i]` 是第 i 条的照片（没有照片的为 null，一条都没有时整个键省略），客户端取回时放回输入框。 | `server/internal/proto/session.go`：`QueueEdit`；`server/internal/node/assistant_queue.go`：`assistantEditQueue`；`docs/protocol/protocol.md`：`POST /api/v1/sessions/{mid}/{sid}/queue` | `queue_edit_images`、`queue_edit_words` |
| 在线与离线机器使用同一类型，系统信息和版本字段可选。 | `server/internal/hub/machine.go`：`Machine.snapshot` | `machines_list`、`machines_event` |
| items 用 kind 区分内容块，工具输入中的自定义键名原样保留。 | `server/internal/proto/item.go`：`Item` | `items_all_kinds` |
| 助手条目携带写出它的模型（`model`），用户条目没有；客户端据此标出回复模型变化的位置。 | `server/internal/proto/item.go`：`Item.Model` | `items_reply_models` |
| 工具调用与结果共用 `tool_use_id`，失败结果携带 `is_error`。 | `proto.Item` | `items_all_kinds` |
| 懒加载图片的 `data` 仍是空字符串，并携带尺寸和 lazy 标记，普通图片保留 base64。 | `proto.Image.Placeholder`、`WithoutImageData` | `items_all_kinds`、`items_inline_image`、`items_live_lazy`、`items_http_after` |
| 后台任务通知仍是 notice，task 区分 shell、monitor 和 agent，退出码零值也可显式出现。 | `proto.TaskNote` | `items_all_kinds`、`items_task_notifications` |
| fold 保存被折叠条目的计数和终点，展开范围分页使用 more。 | `server/internal/transcript/fold.go`：`FoldWork` | `items_folded_work`、`items_fold_range` |
| fold 的 tasks 保存折叠掉的任务快照及其原调用位置（source_item_id、source_tool_use_id、source_seq），旧客户端忽略这些新增字段。 | `server/internal/transcript/fold.go`：`FoldWork`；`proto.FoldTaskSnapshot`；`docs/protocol/protocol.md`「Folded work」 | `items_folded_tasks` |
| 轮次耗时是 `turn_duration` 条目，保留耗时来源和原生关联，实时下发沿用订阅 items 事件，不新增事件或顶层字段；旧客户端把它当未知 kind。 | `server/internal/transcript/transcript.go`：`ParseLine`；`proto.TurnDuration`；`Hub.itemsToSubscribers` | `items_recorded_turn_duration`、`items_live_recorded_turn_duration` |
| 实时事件的 from_seq 可以是零，空 items 也能推进 to_seq。 | `Hub.itemsToSubscribers`；`docs/protocol/protocol.md`「Continuity of items」 | `items_live_lazy`、`items_empty_advance` |
| 最新页补发携带 has_more，接续补发不携带它，达到补发上限时最后一批带 more_after。 | `Hub.sendBacklog`；`docs/protocol/protocol.md`「WebSocket」 | `items_backlog_latest`、`items_backlog_catchup`、`items_backlog_capped` |
| HTTP 增量页携带 history、from_seq、to_seq 和 more_after。 | `Hub.sessionItems` | `items_http_after` |
| question、plan、permission、screen 分别携带相应载荷，question 支持单选和多选。 | `proto.Interaction` | `interaction_question`、`interaction_plan`、`interaction_permission`、`interaction_screen`、`interactions_list` |
| 屏幕对话框携带选项和 rev，密码提示使用 secret 且不记录密码输入。 | `server/internal/screen/dialog.go`：`ParseDialog`；`secret.go`：`SecretPrompt` | `interaction_screen`、`interaction_secret` |
| elicitation 交互携带 MCP 服务名和 form（requested_schema）或 url 载荷，只发给声明 elicitation 能力的连接，旧客户端收不到。 | `server/internal/hub/http.go`：`Hub.broadcast`、`broadcastTo`；`proto.Elicitation` | `interaction_elicitation_form`、`interaction_elicitation_url` |
| interaction_resolved 按 id 撤回交互。 | `server/internal/hub/hub.go`：`resolve` | `interaction_resolved` |
| 助手 ask_user 的提问卡另带 deadline、reversible（没说就没有）和每题的 recommend；客户端不建模这几个键，照常读卡片的其余字段。 | `server/internal/proto/interaction.go`：`Interaction.Deadline`、`Interaction.Reversible`、`Question.Recommend`；`server/internal/assistant/decision_deadline.go`：`keepAskDecision` | `interaction_question_decision` |
| 助手代答的问题撤回时另带 answered_by、reason 和按问题文字的 answers；改答事件 interaction_reanswered 只带改了的问题，放在 resolution.answers。 | `server/internal/hub/fleet_link.go`：`EventFleetResolved`；`server/internal/assistant/fleet_delegate.go`：`reanswer`；`Hub.relayBroadcastFrom`；`docs/protocol/protocol.md`「WebSocket `/api/v1/ws`」 | `interaction_resolved_assistant`、`interaction_reanswered` |
| Fleet 保留驼峰键名，时间使用 Unix 秒数字。 | `server/internal/hub/liveactivity.go`：`FleetState`、`buildFleet` | `fleet_snapshot`、`fleet_event` |
| 收件箱列表统一承载飞书和邮件，移除 context 等内部字段并保留草拟回复。 | `server/internal/assistant/inbox.go`：`InboxItem.listed` | `inbox_list` |
| One made-up item carries every block an Inbox item has, as the list shows it: the item's bookkeeping and each block's internal fields are left out, and `TestListedKeys` pins which keys those are. A move of the item type keeps this case byte for byte (`docs/plans/plan-inbox-vertical-slice.md`, S8). | `server/internal/assistant/inbox.go`：`InboxItem.listed`；`server/internal/assistant/inbox_listed_keys_test.go`：`TestListedKeys` | `inbox_every_block` |
| 回答记录保留一行摘要，`items` 成对保存完整问答并保留换行；旧记录没有 `items`。 | `server/internal/assistant/answered.go`：`answeredBody`、`answeredItems` | `feed_answered_items` |
| 401 保留字符串 error，409 的 code、delivered、draft、clearable 与 error 平铺。 | `Hub.auth`、`callErr`；`docs/protocol/protocol.md`「Sending messages」 | `unauthorized`、`conflict_input_draft`、`conflict_dialog_open`、`conflict_prompt_changed` |

## JSON 结构

每个文件是用例数组，UTF-8，2 空格缩进，末尾换行。

- `name`：跨本领域所有文件唯一的英文 snake_case 名称。
- `description`：中文一句话说明。
- `source`：规则出处数组，包含文件和函数、类型、单测或文档章节。
- `from_test`：直接搬用既有单测断言时填写测试名；其余用例标 `derived: true`，两者互斥。
- `decoder`：样本的类别（`event`、`compat_event`、`compat_session`、`sessions`、`machines`、`interactions`、`inbox`、`items`、`feed`、`items_after`、`items_range`、`fleet`、`error`、`health`）。
- `http_status`：错误样本的 HTTP 状态，Go 捕获响应时已核对。
- `input`：实际 JSON 消息或响应体；无需把整个夹具外壳送入解码器。
- `expected.accepted`：解码方是否接受这条输入；false 表示整条事件被忽略或解码失败。
- `expected.fields`：解码结果的局部断言，键使用 JSON Pointer，数组下标从零开始；未列出的字段也属于 golden 输入，但没有额外的语义断言。

Go 测试逐字节比较整份 golden，并重新读取仓库文件，用真实 Go 类型解码正常样本。`compat_*` 样本是用 Go 编码器构造的兼容性输入，不代表 hub 的正常输出。

## 生成与校验

在仓库根目录运行：

```sh
cd server
go test -count=1 -p 1 ./internal/hub -run '^TestWireSamples$'
go test -count=1 -p 1 ./internal/hub -run '^TestWireSamples$' -update
```

第一条只比较，缺文件、内容漂移或多出未登记的 JSON 都会失败；第二条重写本领域全部 JSON。**hub 改了协议就跑 `-update`，检查 diff，并在提交里说明字段和行为变化。** 不要直接编辑生成的 JSON。

新增用例时，在 `wire_samples_test.go` 的相应主题中构造编造输入，优先调用实际处理函数；只有没有可独立调用的路径时，才使用相同的 map 结构和生产编码器。填写出处和期望，更新本页规则表，再执行 Go golden 校验。若更改兼容性规则，先核对真实行为，不能只修改 expected 让测试通过。

本分支导出的是当前代码。其他车道合入 hello、health 或错误体字段后，协调者需要再次 `-update` ；新增字段若与 `Envelope` 的已知键同名，必须保持原有类型。

## 与设计文档的差异

- 协议文档的“所有 JSON 字段 snake_case”有现存例外：Fleet 使用驼峰字段，飞书 parts 保留上游 refId 和 metadata 的键名。

这些用例记录当前行为，避免把尚未落地的设计当成现有契约；本车道没有修改协议。

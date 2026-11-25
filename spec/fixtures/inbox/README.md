# 收件箱夹具

本目录固定收件箱的列表行为，由 web 应用读取，hub 排序部分由 Go 实现对照。来源为 `docs/assistant/inbox.md`「AI triage」「App」。分拣模型、采集、发送、数据保留期限不属于本目录范围，输入视为列表已收到的数据。

## 规则与用例

每个用例有 source 数组。

| 规则 | 覆盖用例 |
| --- | --- |
| tier 缺失、空值或未知时按 action 显示，但任何非空 tier 都算已分拣。 | `inbox_decode_triage_assertions`、`unknown_tier_is_triaged_action` |
| 只有 done_at 非空才是已处理，read、done_by 或 AI 处理状态都不能替代它。 | `read_and_agent_do_not_change_tier`、`untriaged_and_done_test_assertions` |
| 非空分段固定按需要你、知悉即可、已折叠、已处理排列。 | `tiers_and_done_sections`、`empty_inbox` |
| 默认展开 action 和 fyi，折叠后仍保留成员及标题计数。 | `tiers_and_done_sections`、`expanded_noise_and_done` |
| hub 输出先按 updated_at 倒序，分拣档位不改变它的排序。 | `hub_orders_by_update`、`hub_order_reversed_input`、`hub_order_empty` |
| 未处理事项在段内保持输入顺序，不自行重排 updated_at。 | `input_order_is_authoritative` |
| 已处理事项按 done_at 倒序展示。 | `tiers_and_done_sections`、`expanded_noise_and_done` |
| 来源筛选比较 account，飞书的筛选值为 feishu，邮箱的筛选值为账号 id。 | `inbox_filter_feishu`、`inbox_filter_mail_account`、`source_filter_compares_account` |
| 来源与类型条件取交集，group 包含 bot，dm 不包含 bot。 | `inbox_filter_group_includes_bot`、`inbox_filter_dm_excludes_bot`、`inbox_filter_mail_kind`、`inbox_filter_mention`、`inbox_filter_incompatible_source_kind` |
| 搜索不区分大小写，匹配 title、sender、preview、summary，不匹配 context 或 reason。 | `search_four_fields_only` |
| 未屏蔽的待处理交互按 created_at 升序放在 action 事项之前，助手交互也参与。 | `waiting_agents_before_items` |
| 交互只在来源不限且类型为全部或 agent 时显示，搜索匹配 sessionTitle、summary 或计算后的 title。 | `agent_only`、`source_hides_agents`、`kind_hides_agents`、`search_agents` |
| 空间屏蔽可由单会话 allowed 覆盖。 | `agent_mute_allow_override` |
| 段标题件数包括交互和事项，actionCount 只数所有未处理 action 事项，不受页面筛选影响。 | `waiting_agents_before_items`、`source_hides_agents`、`inbox_filter_mail_account` |
| 筛选标签当前没有数字，因此 filter_counts 为空对象。 | `inbox_filter_feishu`、`inbox_filter_mail_kind` |
| 有 status（第一期）时按它分段：进行中排在需要你和知悉即可之间、永远展开；进行中的事项不算需要你（actionCount 不数它）；缺 status 或值不认识时按 tier。 | `status_sections_with_working`、`working_never_folds`、`old_hub_without_status` |
| 本任务补充同时间按 id 升序的规则。 | `equal_times_use_id_extension`、`equal_times_reversed_input` |

## JSON 结构

文件顶层为 `{schema_version: 1, operation, cases: [...]}`。用例都有英文 snake_case 的 name、中文 description、出处 source、input、expected，并标 `from_test` 或 `derived: true`。所有业务内容与标识均为编造，时间使用 UTC RFC 3339，缺失或 null 的可选值按缺省处理。

| operation | 文件 | 输入与输出 |
| --- | --- | --- |
| `inbox_item_state` | `item_state.json` | 输入 items，输出 id、triage、is_triaged、is_done；sessions 为空。 |
| `hub_inbox_order` | `hub_order.json` | 输入无序 items，输出 hub 列表中的 ids，直接以 Go 实现对照。 |
| `inbox_list` | `sections.json`、`filters.json`、`agents.json`、`ties.json`、`status.json` | 输入已加载列表、待处理交互和设置，输出分段、可见行和计数。 |

`inbox_list.input`：

- `items` 是 InboxItem 的 wire JSON，数组顺序即列表顺序；正常联网列表已由 hub 排序，本地缓存或乐观更新仍以数组为准。
- `sessions` 只用于判断交互所属空间的屏蔽状态。
- `interactions` 是 pending 交互的投影，含 id、machine_id、session_id、kind、created_at、session_title、summary、title；title 是 UI 已计算的搜索标题，不是新增 wire 字段；此处不测试交互文案生成。
- `mutes` 使用 spaces、sessions、allowed。
- `settings.source` 为 `""`、`feishu` 或邮箱账号 id；`kind` 为 `""`、mention、dm、group、mail 或 agent；query 为空表示不搜索。
- `settings.expanded` 明确列出展开的 action、fyi、noise、done，避免测试依赖 UI 状态初始化。

`inbox_list.expected`：

- `groups` 按实际分段顺序排列，只有非空段；每段有 id、title、count、rows、visible_rows、folded。
- rows 保存该段展开时应有的完整顺序；visible_rows 在折叠时为空，否则与 rows 相同；done 即使折叠也投影其展开后的顺序。
- 行引用为 `{type: "item" | "interaction", id}`，类型分开，不能让事项 id 与交互 id 相互覆盖。
- count 数的是行，不是消息总数，事项自身的 count 不会放大段标题计数。
- `action_count` 是所有未处理 action 事项的数量，不能把它误当作筛选后的段标题数量或包含所有来源的 App 总角标。
- `filter_counts: {}` 表明当前 UI 没有筛选数字，消费端不能擅自从段计数补出标签计数。

## 同时间排序的兼容边界

`ties.json` 带 `compatibility: "id_tiebreak_extension"`。它的 expected 是本任务明确要求的确定性规则：事项的 updated_at 相同、已处理事项的 done_at 相同、交互的 created_at 相同时，分别按自身 id 升序。其余比较键和段顺序维持当前行为。没有时间戳的普通缓存事项仍保留输入顺序，不把缺失时间伪装成同一真实时间。

已知差异：

- hub 的 `inboxList` 只比较 updated_at，输入来自 map，相等时间的返回次序没有保证。
- 两个同时间用例的 expected 完全一致；用例里的 `ios_expected` 记录已移除客户端以输入顺序遍历时的结果，现在没有读取方。确定性规则检查 expected 的 id 次序与反向输入不变性。

`docs/assistant/inbox.md` 的「新的在前」由 hub 输出保证，不能据此给缓存列表额外加时间重排。其余用例均以现有实现的输出为 expected。

## 对照与复跑

`hub_order.json` 由 Go 对照：创建 Service 并调用 inbox.init，把 input.items 放入 inbox.items，直接调用 inboxList，将返回 id 数组与 expected.ids 比较；无数据库或网络请求。收件箱从 2026-11-04 起是管理者的。第一期的 status 分段（`status.json`）按契约写的 expected。

## 新增用例

按主题追加用例并更新规则表，保证两个领域的 name 唯一。先编造输入并独立写 expected，再用实现对照；未知档位、空列表、交叉筛选、屏蔽例外和时间相等应分别覆盖。发现当前实现与新规则冲突时，明确标 compatibility，并写明谁需要跟进，不能悄悄改 expected。提交前检查 UTF-8、2 空格缩进和隐私扫描。

## Hand to AI memory (`handoff_memory.json`)

Hand to AI remembers, per source, the target an item was last handed to (#2121; `docs/assistant/inbox.md` "Item actions"). Every app follows the same rules:

- `keys`: the source key is the item's `source`, `|`, and the most specific non-empty mute option, in the order `repo`, `chat`, `sender` (`tag:value`). An item without one has no key and no memory.
- `resolve`: a remembered target is preselected and marked Last time.
  - A remembered Agent that is no longer listed resolves to the main chat, with `gone` set so the app says so once.
  - Nothing remembered, or an unreadable value, gives `null`: the platform's own default.
  - Cases with `needs_new_chat` apply only where New Chat is a target.
- `recency`: one entry per source, latest first, at most `limit` sources.

## Following up: state words, row line, timeline, showcase (#6611)

These five files are the contract of `docs/assistant/inbox.md` ("Following up") for the manager and the web app. The data is made up; `chat` is a placeholder connector id that the web app draws with their generic source icon. The showcase is in Chinese because its screenshots are read in zh-Hans.

| operation | File | Checked by | Input and output |
| --- | --- | --- | --- |
| `inbox_now` | `now.json` | Go (`expected.now`, `expected.section` from the item; the test comes with the manager's state-key change), web (`expected.text` in `en`, `zh-Hans`, `ja`) | `texts` is the one table of words; `compose` says how `waiting`, `check_due` and `failed` are put together; a case with `input.follow_by_lang` gives the model's `who` and `next` per language; `{time}` in the text is `check_at` in the app's own short format for a future moment, put in before comparing |
| `inbox_row_meta` | `row.json` | web | `expected.meta` per language is the row's third line and the detail's small line; `null` means no `where`, so the app keeps today's line |
| `inbox_timeline` | `timeline.json` | web | `folded` and `expanded` are the entry ids shown, in order; `live_state` lists the steps that show an Agent session's live state (only the newest step linking each session, from `input.sessions`); its `session_state` is the session's `state` key, which the web app puts in its Agents list's words; `steps[].icon` is `source` (the item's source mark), `reported` for every reported step, or an icon name for the kind, which the web app maps to the icon it draws for that kind today; `reported` steps are drawn apart: their `text` is the label and the words for apps from before, their `detail` the words alone, which an app that knows the kind shows under its own label (`text` only when `detail` is empty) |
| `inbox_follow_time` | `time.json` | web | `{time}` in a state line: `input.at` against `input.now` in the fixture's `time_zone`, `expected` per language |
| `inbox_showcase` | `showcase.json` | the web app's screenshot runs | the list, then the `detail` item with its timeline folded and expanded, at `now_clock`, in zh-Hans and en, light and dark; `sessions` are the linked conversations as the app's session list has them (wire sessions) |

Who may change what: the keys, sections and words are fixed with the contract; a lane that needs a different input (how the manager marks a muted item, say) changes the case's input and says so in its PR.

## Section bar: choices, counts, landing and search (#9053)

These three files are the contract of `docs/assistant/inbox.md` ("Section bar") for the web app, which replays all three. The data is made up and the expected values come from the rules below, not from the web app.

| operation | File | Input | Expected |
| --- | --- | --- | --- |
| `inbox_bar` | `bar.json` | `inbox_list`'s input (items, interactions, sessions, mutes) with `settings.chosen` (`action`, `working`, `fyi`, `done` or `all`) in place of `settings.expanded`, and an optional `done_days_total` (the hub's day counts added up; absent on a hub without them) | `bar`: the choices shown, in order, each `{id, count, tone}`; `shown_sections`: the sections drawn (`inbox_list` group ids); `empty`: `null`, `caught_up`, `section`, `filtered` or `no_match` |
| `inbox_bar_landing` | `bar_landing.json` | one item opened from outside the list | `{choice, open_collapsed}` |
| `inbox_bar_search` | `bar_search.json` | `start` (`chosen`, `saved`, `query`) and `steps`: `{query}` (the search text becomes this), `{pick}` (a choice tapped) or `{enter}` (the Inbox entered with this search text) | `after`: `{chosen, saved}` after each step |

Rules the expected values follow:

- **Rows** are `inbox_list`'s: an item's section is its known `status`, else `tier` (fyi, noise), else Needs you; `done_at` wins. Waiting Agents count in Needs you when no source is chosen and the kind is all or Agent, muted ones never. Source, kind and search narrow every count.
- **Choices**: `action` and `all` always; `working` and `fyi` while they have a row or are chosen; `done` while it has a row, while the hub's day counts are above zero with no filter or search, or while chosen. Collapsed has no choice; it shows under `all`.
- **Counts**: `action` its rows with tone `needs`; `working` and `fyi` their rows with tone `neutral`; `done` and `all` `null`. A count of 0 stays in the data; the badge hides it.
- **Shown sections**: the chosen one when it has rows (Done also when the day counts name groups); under `all`, every section with rows in `inbox_list`'s order.
- **Empty**: Done chosen and empty is `filtered` with a source or kind, `no_match` with a search, else `section`. Otherwise nothing shown with rows somewhere else open is `section`; nothing open anywhere is `filtered`, `no_match` or `caught_up`, in that order.
- **Landing**: the item's section, Done for a done item, and `all` with `open_collapsed` for a collapsed one. When a list applies it (an outside opening, or a list shown beside the item) is the module doc's rule, not part of these cases.
- **Search**: text appearing saves the choice and shows `all`; a pick narrows; text cleared restores the saved choice (`action` when none). Entering the Inbox shows `action`, or `all` with `saved: action` when a kept search text comes with it.

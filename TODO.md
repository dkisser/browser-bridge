# TODO

记录在 2026-09-15 用 `skills/browser-bridge-user/evals/evals.json` 对 MCP 工具做真实链路评测时发现的问题。当时的背景：分支 `fix/mcp-tool-contracts` 刚修复了 `get_text`/`get_html` 返回 `[object Object]`、`screenshot` 返回 0 字节图片两个契约 bug，评测目的是验证修复并跑通 eval 0/1/2（Gmail 提取邮件主题、GitHub 通知截图、gettext 条件回退截图）。以下问题均在这轮实测中观察到。#1 已于 2026-09-15 修复（snapshot 的 filter/tier 改动），#2、#3 尚未修复。

## 1. snapshot 截断 tier 会误导：文本 run 被静默丢弃

**状态：已修复（2026-09-15）**。三处改动：tier 2 丢弃文本时在原位置输出 `text [text suppressed]` 占位符；统计行标注生效 tier（`[nodes: 40/484 | tier: 2 | truncated: true]`）；snapshot 默认改为 interactive filter（操作面：可交互元素 + heading），典型应用页默认输出 ~1.5K tokens，不再轻易触发 tier 降级。

**现象**

对 Gmail 收件箱（811 节点的大页面）做 `snapshot(max_chars=15000)`，输出里邮件行的发件人/主题单元格全部显示为空 generic（只有日期），看起来像"选择器找不到内容"。但用 `get_text('.bog')` 却能拿到第一封邮件的真实主题——证明文本一直在 DOM 里，只是 snapshot 没输出。

**上下文 / 根因**

`snapshot` 的预算机制在超预算时逐级降级（`packages/shared/src/snapshot.ts` 的 `renderSnapshotTree`）：tier 1 截断每条文本到 40 字符，**tier 2 直接丢弃所有 `role: 'text'` 节点**。Gmail 页面头部 chrome 吃掉大量预算，等 walker 走到列表区时已经降到 tier 2，主题/发件人的文本 run 被整体丢掉。输出里没有任何标志告诉读者"这些单元格不是空的，是文本被 tier 丢弃了"，仅在末尾有 `[309/784 nodes | truncated]` 这类统计，极易误判为选择器或页面问题。实测中靠 `max_chars=100000`（tier 0，624/811 节点无截断）才拿到完整内容——与用户此前"被逼用 100K snapshot"的教训一致。

**可选方向**

- 文本 run 被 tier 2 丢弃时，在对应位置输出占位（如 `…` 或 `[text suppressed by tier]`），让"空"和"被截断"可区分；
- 或在统计行里明确标注当前生效的 tier；
- 工具 description 中补充"truncated 时文本可能被整体省略，需要更精确时用 selector 缩小范围或提高 max_chars"。

**相关文件**：`packages/shared/src/snapshot.ts`（`renderAtTier`/`renderSnapshotTree`）、`apps/extension/src/content.ts`（snapshot case）、`apps/websocket/src/mcp/tools/snapshot.ts`

## 2. `wait_navigation` 竞态：导航已完成时调用会超时

**现象**

`navigate` 超时后（如目标地址发生重定向链时，实测 `mail.google.com/mail/u/0/h/` 那次），再补一个 `wait_navigation`，即使页面早已加载完成，也会一直等到超时。实测等了 20s 超时，而此时 `pageinfo` 显示页面早已就绪。

**上下文 / 根因**

extension 的 `wait:navigation` 实现（`apps/extension/src/background.ts` 的 `wait:navigation` case）是：注册 `chrome.tabs.onUpdated` 监听器，等 `status === 'complete'` 才 resolve，纯"等未来事件"。如果命令到达时导航已经完成（`navigate` 自己的监听器已经消费过 complete 事件、或加载发生在命令下发前），就永远等不到下一次 complete，只能超时。`navigate` 本身没有这个问题（它先 `tabs.update` 再监听，事件一定在未来），问题只出现在事后补救式调用 `wait_navigation` 的场景。

**可选方向**

- 等事件之前先查一次当前状态：`chrome.tabs.get(tabId)` 若 `status === 'complete'` 立即返回；
- 或短时间轮询 `status`（如每 500ms 查一次，直到 complete 或超时），替代单次事件监听。

**相关文件**：`apps/extension/src/background.ts`（`case 'wait:navigation'`）、`apps/websocket/src/mcp/tools/wait-navigation.ts`

## 3. 虚拟化列表（如 Gmail）的 DOM 在调用间挂载/卸载，选择器查询结果不稳定

**现象**

同一轮评测中，对 Gmail 收件箱的查询结果前后矛盾：

- `snapshot`（无 selector）两次都完整列出了 12 行邮件（`table`/`row` 标签）；
- 但 `get_text('table tr')`、`get_text('table tbody tr')`、`snapshot('table')`、`snapshot('[role="row"]')` 在多个时刻只命中搜索框那一张表/一行，返回 `"Tab"` 或只有搜索框结构；
- 稍后又出现过 `get_text('.bog, .y6')` 只返回 1 封邮件（展开的那封未读），而其他 11 行走完全不同的标记。

同一页面、不同调用之间 DOM 结构对选择器呈现不一致。

**上下文 / 分析**

Gmail 新版收件箱是虚拟化渲染 + 分区挂载的：未读区/其他邮件区的列表节点只在特定时刻才挂进 DOM（实测中列表行数、tbody 结构在两次 snapshot 之间也发生过变化），且只有展开的那封未读邮件带 `.bog`/`.y6` 旧版 class，其余邮件的 class 不同。这导致：按 class 选择器做 `get_text` 时灵时不灵；`snapshot` 全页走查（含隐藏元素判断，见 `content.ts` 的 `isHidden`）反而稳定。另外注意 `resolveSelector` 在选择器无匹配时会降级按**文本**查找并报 "Element with text not found"，这个报错文案也让排查方向更容易跑偏（实测中一度以为选择器写错）。

这不算 Browser Bridge 的 bug（是目标站点的渲染策略），但值得：

- 在 `get_text`/`snapshot` 的 description 或 skill 文档中提示：对虚拟化列表（Gmail、大表格）优先用全页 snapshot 或大预算 snapshot，class 选择器可能只命中部分行；**（2026-09-15 已做：SKILL.md 新增 "Snapshot vs. reading" 与 "Virtualized lists" 小节，snapshot description 重写并注明 truncated 时文本可能被占位/丢弃）**
- 考虑给文本提取类工具加"命中 0 个元素 vs 元素存在但文本为空"的区分报错，减少误判。（仍未做，另立任务）

**相关文件**：`apps/extension/src/content.ts`（`resolveSelector`、`walkElement`/`isHidden`）、`apps/websocket/src/mcp/tools/get-text.ts`、`skills/browser-bridge-user/SKILL.md`

## 4. full filter 的格式收紧（2026-09-15 评审确定为缓办项）

**背景**

interactive 成为 snapshot 默认（见 ADR-0002）后，full 模式变为显式 opt-in 的非热路径。实测 full 模式输出有四处膨胀：缩进开销（depth 10 的行光前导空格 20 字符）、`generic` 包装行、文本双写（带 name 的 link/button 下再挂一层相同的 text run）、img 的 src 长 URL（10 个头像图 ≈ 1K 字符）。这些在 interactive 模式下已天然消失，是否还值得收紧 full 模式，等 full 的真实使用数据再定。

**可选方向（按难度排序，均已勘明）**

- **img src**：`content.ts` 的 `collectAttrs` 删 img 分支（2 行）。风险极低：alt 保留在 name，只损失 full 模式看图片 URL 的能力。
- **文本双写**：walker 已有 `suppressTextRuns`（content.ts）只挡直接文本节点，`<span>` 包装就漏。修复 = walk 时向下传递最近命名祖先的 name，text run 是其子串则跳过（约 15 行）。风险低-中：子串匹配有边界，但操作面下被丢的内容本已在 name 里。
- **缩进**：renderer 改 `INDENT` 或 depth cap（2-5 行），但输出形状全变，`packages/shared/tests/snapshot.test.ts` 的 33 个断言大部分要跟着改——机械但繁琐。
- **generic 包装合并**：renderer 合并单子链 generic 且保留带 ref 的行（30-50 行 + 边界规则）。唯一算"有点大"的项。

**相关文件**：`packages/shared/src/snapshot.ts`、`apps/extension/src/content.ts`、`packages/shared/tests/snapshot.test.ts`

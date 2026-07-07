# Browser Bridge 浏览器工具并发与隔离设计

## 1. 背景与问题

当前 Browser-Bridge 的浏览器工具存在以下痛点：

1. **总在同一个 tab 中操作**：`navigate`、`click`、`gettext` 等工具默认操作当前窗口的 active tab，导致页面不断跳转。
2. **无法并发取数据**：底层虽然能接收 `tabId`，但 MCP tool schema 基本未暴露该参数，AI 无法在同一浏览器内并行操作多个 tab。
3. **影响用户当前 tab**：自动化流程会抢占用户正在使用的 active tab，打断用户工作。
4. **工具调用不自然**：想查两个页面需要先 `tab_new`、再 `tab_switch`、再操作，流程割裂。

本设计通过“**完全显式的 tab 句柄**”解决上述问题：所有页面操作工具必须显式传入 `tab_id`，系统不再隐式使用 active tab。

## 2. 设计目标

- **无感隔离**：AI 的自动化操作默认不影响用户当前 active tab。
- **同浏览器多 tab 并发**：在单个已连接浏览器内，AI 可以同时打开并操作多个工作 tab。
- **原子工具 + 显式上下文**：保留现有原子工具，但每个工具调用都必须显式指定 `tab_id`。
- **可配置生命周期**：工作 tab 默认保留，由 AI 显式 `tab_close` 关闭；`tab_new` 保留 `auto_close` schema 参数供未来扩展。

## 3. 架构总览

```
┌─────────────────────────────────────┐
│  MCP Client (Claude / other LLM)    │
│  调用工具时显式传入 tab_id           │
└─────────────┬───────────────────────┘
              │ HTTP Stream (MCP)
              ▼
┌─────────────────────────────────────┐
│  WebSocket MCP Server               │
│  - schema 校验 tab_id               │
│  - 通过 sendCommand 下发命令         │
└─────────────┬───────────────────────┘
              │ WebSocket
              ▼
┌─────────────────────────────────────┐
│  Local Proxy / WebSocket Server     │
│  - 路由到对应 browserId 的 extension │
└─────────────┬───────────────────────┘
              │ WebSocket
              ▼
┌─────────────────────────────────────┐
│  Chrome Extension (offscreen doc)   │
│  - background.ts 按 tabId 执行命令   │
│  - content.ts 在指定 tab 执行 DOM 操作│
└─────────────────────────────────────┘
```

核心原则：**当前 active tab 从所有工具中彻底移除**。涉及页面的工具调用必须显式指定 `tab_id`；`tab_id` 由 `tab_new` 创建并返回，`navigate` 接收 `tab_id` 并在结果中回显，方便 AI 链式使用。

## 4. 组件与职责

### 4.1 Extension：`apps/extension/src/background.ts`

- 所有命令统一要求 `payload.tabId`。
- 删除 `getActiveTabId()` 函数及其所有 fallback 调用。
- `navigate`、`goBack`、`goForward`、`refresh`、`screenshot`、`pageinfo`、`wait:navigation` 改为必须接收 `tabId`。
- `sendToContentScript` 必须接收 `tabId`，不再自己查询 active tab。
- `tab:new` 支持 `active: false`，让新 tab 在后台打开，不抢夺用户焦点。

### 4.2 Shared Protocol：`packages/shared/src/types.ts`

- `CommandPayload` 类型改为必须包含 `tabId`。
- 新增 `TabNewParams`、`TabCloseParams` 等显式类型。

### 4.3 WebSocket MCP Tools：`apps/websocket/src/mcp/tools/*.ts`

- 每个页面操作工具的 schema 增加必填 `tab_id: z.number().int().min(0)`。
- `execute*` 函数把 `tab_id` 通过 `sendCommand` 透传给 extension。
- `tab_new` 增加可选参数：
  - `active: boolean`（默认 `false`）：创建后是否将该 tab 设为 active；默认后台打开，不抢夺用户焦点。
  - `auto_close: boolean`（默认 `false`）：**第一期仅作为 schema 占位，不实现自动关闭逻辑**，保持完全显式精神；AI 仍需调用 `tab_close` 关闭。后续如需自动清理，再实现该参数。
- `navigate` 必须带 `tab_id`，不再创建新 tab；想在新 tab 打开需先 `tab_new` 再 `navigate`。`navigate` 返回的 `tab_id` 为传入值的回显。

### 4.4 Command Client：`apps/websocket/src/mcp/command-client.ts`

- 基本不变，继续负责把命令发送到 WebSocket server。

### 4.5 测试

- 更新所有现有 tool 测试，每个调用补充 `tab_id`。
- 新增 `tab-new` 对 `active: false` 的覆盖。
- 新增“缺少 `tab_id` 时 schema 报错”的测试。
- 新增“操作已关闭 tab 返回错误”的测试。
- extension 侧如已有测试，更新为所有命令必须带 `tabId`。

## 5. 数据流与工具调用流程

以 AI 并发查两个页面为例：

```text
1. set_browser({ browser_id: "..." })           -> 选定浏览器
2. tab_new({ url: "https://a.com" })            -> 返回 { tab_id: 101 }
3. tab_new({ url: "https://b.com" })            -> 返回 { tab_id: 102 }
4. gettext({ tab_id: 101, selector: "h1" })
5. gettext({ tab_id: 102, selector: "h1" })
6. tab_close({ tab_id: 101 })
7. tab_close({ tab_id: 102 })
```

第 4、5 步可以由 AI 并行发起（MCP 支持同时多个 tool call），每个请求都带自己的 `tab_id`，extension 侧分别操作不同 tab，互不阻塞。

### 5.1 关键数据变化

- **MCP → WebSocket server**：`sendCommand` 的 `params` 里必须带 `tab_id`。
- **WebSocket server → Extension**：`CommandMessage.payload.params` 带 `tab_id`。
- **Extension → Content script**：DOM 命令用 `chrome.tabs.sendMessage(tabId, ...)`，不再查询 active tab。
- **返回值**：
  - `navigate` 返回 `{ tab_id, url, title }`，其中 `tab_id` 为传入值的回显
  - `tab_new` 返回 `{ tab_id, url }`
  - 其他工具返回各自结果

### 5.2 默认行为

- `tab_new` 默认 `active: false`，新 tab 在后台打开。
- `navigate` 必须带 `tab_id`，不再创建新 tab。
- 所有页面操作工具不再回退到 active tab。

## 6. Tab 生命周期与隔离策略

### 6.1 生命周期

- **创建**：`tab_new` 创建，默认后台、不激活。
- **使用**：所有工具通过 `tab_id` 操作。
- **结束**：由 AI 显式调用 `tab_close({ tab_id })` 关闭。
- **可配置参数**：`tab_new` 的 `auto_close` 第一期仅作为 schema 占位，不启用自动关闭逻辑，保持完全显式精神。后续如需自动清理，可扩展为 session 结束时批量关闭或定时器。

### 6.2 用户当前 tab 保护

- Extension 删除 `getActiveTabId()` 后，没有任何工具能误操作用户当前正在看的 tab。
- 即使 AI 传了错误的 `tab_id`，最多也是操作到某个它自己创建的工作 tab，不会跳到用户的 active tab。
- `screenshot` 工具现在需要 `tab_id`，不会截取用户当前屏幕。

### 6.3 并发安全

- 不同 `tab_id` 的请求在 extension 和 content script 层完全独立。
- 同一 tab 上的连续操作仍然是串行的（由 MCP/WS 请求顺序保证），这是符合预期的。

## 7. 错误处理

- **缺少 `tab_id`**：MCP schema 层直接校验失败，返回清晰错误，不会落到 extension。
- **`tab_id` 不存在或已关闭**：extension 调用 `chrome.tabs.get(tabId)` 会抛错，返回 `status: 'error'` + 错误信息，例如 `"Tab 101 not found or already closed"`。
- **Content script 未注入**：`sendToContentScript` 保持现有逻辑：先 ping，失败则 `executeScript` 注入 `content.js`，再发命令。
- **导航超时**：`wait:navigation` 和 `navigate` 继续用 `tabs.onUpdated` + timeout；超时时返回错误并清理监听器。
- **并发错误隔离**：一个 tab 上的失败不会影响其他 tab 的请求。

## 8. 测试策略

### 8.1 Unit Tests（Bun）

- 更新所有现有 tool 测试，每个调用都补 `tab_id`。
- 新增 `tab-new` 对 `active: false` 的测试。
- 新增“缺少 `tab_id` 时 schema 报错”的测试。
- 新增“操作已关闭 tab 返回错误”的测试。

### 8.2 Extension 侧测试

- 如已有测试，更新为所有命令必须带 `tabId`。
- 验证删除 `getActiveTabId()` 后没有任何 fallback 路径。

### 8.3 E2E / 手动验证

- 启动 extension + WebSocket server。
- 让 AI 并发打开两个 tab 分别取数据。
- 确认用户当前 tab 不被影响。

## 9. 迁移与兼容性

这是**破坏性变更**：所有现有工具调用都需要补充 `tab_id`。

- 更新 `skills/browser-bridge-user/SKILL.md` 中的示例和提示词。
- 更新 README 中相关示例。
- 更新所有 tool 测试。
- 由于项目目前是个人/demo 项目，没有外部消费者，本次变更直接落地，不提供旧版兼容层。

## 10. 后续可扩展点

- **`auto_close` 自动关闭**：在 `tab_new` 中实现 `auto_close: true`，在指定操作完成后自动关闭 tab。
- **Tab 池/复用**：当 AI 频繁创建 tab 时，可考虑复用空闲工作 tab 而非每次都新建。
- **后台 Window**：为 AI 工作 tab 创建独立的后台窗口，进一步避免干扰用户。
- **多浏览器并发**：在方案 3 基础上，未来可扩展为同 session 内同时连接多个 browserId。

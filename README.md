<p align="center">
  <img src="./docs/assets/logo.png" alt="Browser Bridge Logo" width="200" />
</p>

<p align="center">
  <a href="./README.en.md">English</a> | 中文
</p>

# Browser Bridge

**从云端安全地握住你的浏览器。**

Browser Bridge 让开发者和 Agent 通过同一个命令行接口远程操控浏览器：搜索、填表、抓取页面、管理标签。对人来说它是 CLI 工具；对 Agent 来说，它是一个标准、可编排的浏览器控制入口。

---

## 一句话描述

一个两部分的桥接系统：云端 CLI 作为统一的控制接口，既接收用户输入，也供任意 Agent 调用；命令通过 WebSocket 下发到用户本地的 Chrome Extension，由 Extension 在浏览器里完成具体动作。

---

## 为什么需要它

- **Agent 也能操控浏览器**：任何 LLM、脚本或自动化工具只需调用 CLI，就能像人类一样操作真实浏览器，无需学习 Chrome Extension API。
- **自动化重复操作**：批量填表、定时抓取、跨站点数据收集。
- **远程办公**：让云端脚本、团队共享的 CLI 工具或 Agent 操作你本地已登录的浏览器。
- **保留浏览器状态**：不需要 headless 浏览器或额外 Cookie 管理，直接复用真实用户会话。

---

## 架构：两部分，一座桥

```
                        ┌─────────────────┐
                        │  Agent          │
                        │ （本地或云端）   │
                        └────────┬────────┘
                                 │ 调用 CLI
                                 ▼
┌─────────────────────────────────────────────────┐
│                      CLOUD                      │
│  ┌─────────┐      ┌─────────────────────────┐   │
│  │   CLI   │──────▶│      WebSocket Server   │   │
│  │         │      │                         │   │
│  └─────────┘      └───────────┬─────────────┘   │
└───────────────────────────────┼─────────────────┘
                                │ WebSocket
                                │ 安全长连接
┌───────────────────────────────┼─────────────────┐
│              LOCAL             ▼                │
│  ┌─────────────┐    ┌─────────────────┐         │
│  │   Chrome    │◀───│  WebSocket Local│         │
│  │  Extension  │    │    （本地代理）  │         │
│  │             │    └─────────────────┘         │
│  └──────┬──────┘                                │
│         │ Chrome Extension APIs                 │
│         ▼                                       │
│  ┌─────────────┐                                │
│  │   Chrome    │                                │
│  │  （浏览器）  │                                │
│  └─────────────┘                                │
└─────────────────────────────────────────────────┘
```

| 部分 | 组件 | 职责 |
|------|------|------|
| **Cloud** | CLI | 统一的浏览器控制接口；可被本地或云端的用户、Agent 调用。 |
| | WebSocket Server | 接收 CLI 指令，转发给对应的本地客户端。 |
| **Local** | WebSocket Local | 本地代理，维持与服务端的长连接。 |
| | Chrome Extension | 通过 WebSocket 与本地代理通信，再通过 Chrome Extension API 执行浏览器操作。 |
| | Chrome | 实际运行页面、管理标签、执行 DOM 操作。 |

---

## 数据流：一条命令如何抵达浏览器

```
用户或 Agent 调用 CLI 输入命令
    │
    ▼
WebSocket Server 鉴权并路由
    │
    ▼
WebSocket Local（用户电脑上的代理）
    │
    ▼
WebSocket（本地代理 ↔ Extension）
    │
    ▼
Chrome Extension
    │
    ▼
Chrome Extension APIs
    │
    ▼
浏览器执行：打开标签 / 填表 / 抓取 / 点击……
```

---

## 谁在使用

Browser Bridge 同时服务于两种调用方：

- **终端用户**：直接在命令行输入指令，远程操控自己的浏览器。
- **Agent / 自动化系统**：把 CLI 当作标准入口，让本地或云端的 LLM、脚本、调度任务或其他 Agent 调用，完成需要浏览器的复杂工作流。

---

## 用户旅程：三步连接

```
安装 Extension  ──▶  完成认证  ──▶  人或 Agent 通过 CLI 操作浏览器
```

1. **安装**：用户从 Chrome 应用商店或本地加载 Browser Bridge Extension。
2. **认证**：在 Extension 中完成身份校验（扫码、账号密码、或后续接入的其它方式）。认证模块被抽象为可插拔接口，支持多种 Provider。
3. **操控**：认证通过后，用户或 Agent 即可通过 CLI 向浏览器发送指令。Agent 可以运行在本地，也可以运行在云端。

---

## 安全边界

- 只有经过认证的本地 Extension 才能注册到 WebSocket Server。
- CLI / Agent 发送的每条命令都经过服务端路由，不会直接暴露本地网络。
- 本地代理与 Extension 之间通过本地 WebSocket 通信，不监听外部端口。

---

## 一行安装

```bash
curl -fsSL https://raw.githubusercontent.com/dkisser/browser-bridge/main/install/install.sh | bash
```

按提示在 Chrome 里加载 `~/.browser-bridge/extension/`，然后：

```bash
bridge up
```

详细说明、错误码、卸载方式见 [`install/README.md`](./install/README.md)。

## 快速开始（开发模式）

```bash
# 1. 安装依赖
bun install

# 2. 启动 WebSocket 服务
bun run dev:websocket

# 3. 启动本地代理（另一个终端）
bun run dev:local-proxy

# 4. 构建 Extension（再开一个终端）
bun run dev:extension

# 5. 在 Chrome 中加载 apps/extension/dist/ 目录

# 6. 运行 CLI
bun run cli
```

---

## 项目结构

```
Browser-Bridge/
├── apps/
│   ├── cli/            # 云端命令行工具
│   ├── extension/      # Chrome Extension（Manifest V3，Vite 构建）
│   └── websocket/      # WebSocket Server / Client / Protocol
├── packages/
│   └── shared/         # 共享常量与工具
├── README.md
└── biome.json
```

---

## 技术栈

- **Runtime / 包管理**：Bun
- **Extension 构建**：Vite + Manifest V3
- **通信协议**：WebSocket（云端 + 本地）+ Chrome Extension API
- **类型检查**：TypeScript（strict）
- **代码风格**：Biome

---

> 更直观的完整架构图见 [`docs/architecture-diagram.html`](./docs/architecture-diagram.html)。

<p align="center">
  <img src="./docs/assets/logo.png" alt="Browser Bridge Logo" width="160" />
</p>

<h1 align="center">Browser Bridge</h1>

<p align="center">
  <a href="#-快速开始">快速开始</a> •
  <a href="#-功能特性">功能特性</a> •
  <a href="#-架构">架构</a> •
  <a href="#-安装">安装</a> •
  <a href="./README.md">English</a>
</p>

<p>
  <strong>让浏览器成为任意 Agent 的工具：</strong>让任何 AI Agent、LLM 或脚本控制你的本地浏览器。
  可以使用内置 CLI、Claude Code skill，或任何能使用 bridge 协议的集成。
  会话、Cookie 和凭证始终保留在本地。
</p>

<p align="center">
  <img src="./docs/assets/news-demo.gif" alt="查新闻" width="720" />
  <br />
  <em>查新闻</em>
</p>

<p align="center">
  <img src="./docs/assets/gmail-demo.gif" alt="管理邮件" width="720" />
  <br />
  <em>管理邮件</em>
</p>

<p align="center">
  <strong>一句话介绍：</strong>Browser Bridge 让你的本地 Chrome 成为任何 Agent 的可复用工具。
  一个浏览器，任意 LLM、脚本或终端命令——同时把你的数据保留在本地。
</p>

---

## ✨ 功能特性

- 🤖 **Agent 就绪的接口** —— 一个 bridge 协议，可通过 CLI、Claude Code skill 或自定义集成来消费。
- 🔒 **本地会话，云端控制** —— 复用你已登录的浏览器，无需云端浏览器或同步 Cookie。
- 🌉 **WebSocket 桥接** —— Agent 与服务端通信，服务端路由到本地代理，再连接到 Chrome。
- 🧩 **Chrome 扩展（MV3）** —— 基于 Vite 构建，以解压扩展形式加载。
- ⚡ **Bun + TypeScript** —— 启动快、类型严格、整个 monorepo 一个包管理器。
- 🧪 **开发友好** —— 服务端、代理、扩展均支持热重载。
- 🤖 **MCP server** —— Streamable HTTP MCP server，向 Claude Desktop、Cursor 等 MCP 客户端暴露浏览器控制工具。

---

## 🚀 快速开始

### 1. 安装 bridge 与扩展

```bash
curl -fsSL https://github.com/dkisser/browser-bridge/releases/latest/download/install.sh | bash
```

在 Chrome 中加载 `~/Browser-Bridge/extension/` 作为解压扩展。bridge 服务会自动启动。

### 2. 发送第一条命令

```bash
bridge navigate https://github.com --browser <browser-id>
```

命令会经过 CLI → WebSocket 服务端 → 本地代理 → Chrome 扩展 → 浏览器。

> 使用 `bridge browser:list` 查看已连接 Chrome 实例的 `<browser-id>`。

### 3. 从任意 Agent 使用

`bridge` CLI 只是 bridge 协议的一种消费者。Browser Bridge 在 [`./skills`](./skills/browser-bridge-user/SKILL.md) 中内置了开箱即用的 Claude Code skill；任何能打开 WebSocket 的客户端——例如你自己构建的 MCP server、自定义 SDK 或其他 Agent 框架——都可以用同样的方式发送命令。

---

## 🤖 使用 MCP

Browser Bridge 在 WebSocket 服务端之外，还同时暴露了一个 [Streamable HTTP MCP server](docs/mcp-setup.md)。启动 `bridge up`（或 `bun run dev:websocket`）后，在你的 MCP 客户端（支持 Streamable HTTP 的 Claude Desktop、Cursor 等）中添加 `http://localhost:3003/mcp` 即可。

MCP server 提供了一组浏览器控制工具，例如 `navigate`、`click`、`type`、`screenshot`、`get_text` 等，可直接操作已连接的 Chrome 浏览器，无需经过 CLI。

完整客户端配置、环境变量和工具列表请参考 [docs/mcp-setup.md](docs/mcp-setup.md)。

---

## 🏗️ 架构

```
┌─────────────┐      ┌─────────────────┐      ┌─────────────────┐      ┌─────────────────┐
│  CLI / Agent │ ───▶ │  WebSocket      │ ───▶ │  Local Proxy    │ ───▶ │  Chrome         │
│             │      │  Server         │      │  (本机)         │      │  Extension      │
└─────────────┘      └─────────────────┘      └─────────────────┘      └─────────────────┘
                                                                              │
                                                                              ▼
                                                                       ┌─────────────┐
                                                                       │   Chrome    │
                                                                       │  (浏览器)   │
                                                                       └─────────────┘
```

| 层级 | 组件 | 职责 |
|------|------|------|
| 云端 / 共享 | 接口层 | 面向 Agent 的入口：CLI、Claude Code skill 或任何自定义集成。 |
| 云端 / 共享 | WebSocket Server | 将命令路由到对应的本地代理。 |
| 本地 | Local Proxy | 从本机维持与服务端的长连接。 |
| 本地 | Chrome Extension | 接收消息并执行浏览器操作。 |

完整架构图见 [`docs/architecture-diagram.html`](./docs/architecture-diagram.html)。

---

## 📦 安装

### 方案 A：一行命令安装（推荐）

```bash
curl -fsSL https://github.com/dkisser/browser-bridge/releases/latest/download/install.sh | bash
```

安装脚本会下载运行时，在 `~/Browser-Bridge/extension/` 创建扩展的软连接，并自动启动 bridge 服务。你只需在 Chrome 中加载该解压扩展即可。

在 macOS 上，安装脚本还会默认开启登录自启动，这样你每次登录后 bridge 服务会自动运行。如需关闭，可在安装时传入 `--no-autostart`，或之后运行 `bridge autostart off`。

如需强制重装同一版本，可传入 `--force`；如需安装指定版本，可设置 `BB_VERSION=vX.Y.Z`。

### 方案 B：一行命令安装并附带 Claude Code skill

如果你已经在使用 [Claude Code](https://claude.ai/code)，先克隆仓库，然后在项目根目录运行安装脚本并传入 `--with-skills`，即可同时安装 Browser Bridge 和 `./skills` 目录下的 skill：

```bash
git clone https://github.com/dkisser/browser-bridge.git
cd browser-bridge
./install/install.sh --with-skills
```

如果你想把 skill 安装到 `~/.claude/skills/` 以外的目录，请使用 `--skills-dir <路径>` 指定。使用 `--no-skills` 可显式跳过 skill 安装。

curl 一键安装默认**不会**安装 skill；如需安装，请使用 `--with-skills`。

### 方案 C：从源码构建（仅贡献者）

普通用户无需执行。开发者请参考下方的 [🛠️ 开发](#-开发) 章节。

---

## 🛠️ 开发

> 以下步骤仅适用于贡献者/开发者，终端用户无需安装 `bun` 或 `git`。

```bash
# 1. 安装依赖
bun install

# 2. 启动 WebSocket 服务端
bun run dev:websocket

# 3. 另一个终端启动本地代理
bun run dev:local-proxy

# 4. 第三个终端构建扩展
bun run dev:extension

# 5. 在 Chrome 中加载 apps/extension/dist/ 作为解压扩展

# 6. 运行 CLI
bun run cli
```

---

## 📂 项目结构

```
Browser-Bridge/
├── apps/
│   ├── cli/            # CLI 入口（bridge 协议消费者之一）
│   ├── extension/      # Chrome 扩展（Manifest V3，Vite）
│   ├── local-proxy/    # 本地 WebSocket 代理
│   └── websocket/      # WebSocket 服务端、客户端和协议
├── packages/
│   └── shared/         # 共享常量与工具
├── install/            # 一键安装脚本
└── docs/               # 架构图与指南
```

---

## 🧰 技术栈

- **运行时与包管理**：<a href="https://bun.sh" target="_blank">Bun</a>
- **扩展构建**：Vite + Manifest V3
- **通信协议**：WebSocket
- **类型检查**：TypeScript（strict）
- **代码规范**：Biome
- **测试**：Bun test runner + Bats（安装脚本）

---

## 🛡️ 安全

- 只有经过认证的扩展才能注册到 WebSocket 服务端。
- 命令通过服务端路由，本地网络不会直接暴露。
- 本地代理以出向连接方式连接服务端与扩展，尽量减少开放端口。

---

## 🤝 贡献

欢迎贡献。请先提交 issue 讨论重大变更。

---

## 📄 许可证

[MIT](./LICENSE)

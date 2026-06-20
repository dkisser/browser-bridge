<p align="center">
  <img src="./docs/assets/logo.png" alt="Browser Bridge Logo" width="160" />
</p>

<h1 align="center">Browser Bridge</h1>

<p align="center">
  <strong>让 AI Agent 和脚本通过简单 CLI 控制你的本地浏览器。</strong>
</p>

<p align="center">
  <a href="#-快速开始">快速开始</a> •
  <a href="#-功能特性">功能特性</a> •
  <a href="#-架构">架构</a> •
  <a href="#-安装">安装</a> •
  <a href="./README.md">English</a>
</p>

<p align="center">
  <img src="./docs/assets/demo.gif" alt="Browser Bridge Demo" width="720" />
</p>

> **一句话介绍：** Browser Bridge 把 Chrome 扩展与 WebSocket 中继连接起来，让任何 LLM、脚本或终端命令都能像人一样操作真实浏览器——同时把你的登录态和凭证保留在本地。

---

## ✨ 功能特性

- 🤖 **Agent 就绪的 CLI** —— LLM 和脚本只需调用一个命令即可驱动浏览器。
- 🔒 **本地会话，云端控制** —— 复用你已登录的浏览器，无需云端浏览器或同步 Cookie。
- 🌉 **WebSocket 桥接** —— CLI → 服务端 → 本地代理 → Chrome。
- 🧩 **Chrome 扩展（MV3）** —— 基于 Vite 构建，以解压扩展形式加载。
- ⚡ **Bun + TypeScript** —— 启动快、类型严格、整个 monorepo 一个包管理器。
- 🧪 **开发友好** —— 服务端、代理、扩展均支持热重载。

---

## 🚀 快速开始

### 1. 安装 CLI 和扩展

```bash
curl -fsSL https://github.com/dkisser/browser-bridge/releases/latest/download/install.sh | bash
```

在 Chrome 中加载 `~/.browser-bridge/extension/` 作为解压扩展，然后运行：

```bash
bridge up
```

### 2. 发送第一条命令

```bash
bridge navigate https://github.com --browser <browser-id>
```

命令会经过 CLI → WebSocket 服务端 → 本地代理 → Chrome 扩展 → 浏览器。

> 使用 `bridge browser:list` 查看已连接 Chrome 实例的 `<browser-id>`。

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
| 云端 / 共享 | CLI | 面向人或 Agent 的命令接口。 |
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

### 方案 B：从源码构建（仅贡献者）

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
│   ├── cli/            # CLI 入口
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

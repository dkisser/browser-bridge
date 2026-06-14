# Browser Bridge

**从云端安全地握住你的浏览器。**

Browser Bridge 让开发者通过命令行远程操控自己的浏览器：搜索、填表、抓取页面、管理标签——就像坐在电脑前一样，只是命令来自云端。

---

## 一句话描述

一个两部分的桥接系统：云端 CLI 通过 WebSocket 向用户本地的 Chrome Extension 下发指令，Extension 在浏览器里完成具体动作。

---

## 为什么需要它

- **自动化重复操作**：批量填表、定时抓取、跨站点数据收集。
- **远程办公**：让云端脚本或团队共享的 CLI 工具操作你本地已登录的浏览器。
- **保留浏览器状态**：不需要 headless 浏览器或额外 Cookie 管理，直接复用真实用户会话。

---

## 架构：两部分，一座桥

```
┌─────────────────────────────────────┐
│               CLOUD                 │
│  ┌─────────┐      ┌─────────────┐   │
│  │   CLI   │──────▶│ WebSocket   │   │
│  │ (你输入 │      │   Server    │   │
│  │  命令)  │      │             │   │
│  └─────────┘      └──────┬──────┘   │
└──────────────────────────┼──────────┘
                           │ WebSocket
                           │ 安全长连接
┌──────────────────────────┼──────────┐
│              LOCAL        ▼         │
│  ┌─────────────┐    ┌───────────┐   │
│  │   Chrome    │◀───│ WebSocket │   │
│  │  Extension  │    │   Local   │   │
│  │             │    │  (本地代理)  │   │
│  └──────┬──────┘    └───────────┘   │
│         │ Native Messaging           │
│         ▼                            │
│  ┌─────────────┐                     │
│  │   Chrome    │                     │
│  │  (浏览器)    │                     │
│  └─────────────┘                     │
└─────────────────────────────────────┘
```

| 部分 | 组件 | 职责 |
|------|------|------|
| **Cloud** | CLI | 用户输入命令，将指令发送到服务端。 |
| | WebSocket Server | 接收 CLI 指令，转发给对应的本地客户端。 |
| **Local** | WebSocket Local | 本地代理，维持与服务端的长连接。 |
| | Chrome Extension | 通过 Native Messaging 与本地代理通信，执行浏览器操作。 |
| | Chrome | 实际运行页面、管理标签、执行 DOM 操作。 |

---

## 数据流：一条命令如何抵达浏览器

```
CLI 输入命令
    │
    ▼
WebSocket Server 鉴权并路由
    │
    ▼
WebSocket Local（用户电脑上的代理）
    │
    ▼
Chrome Native Messaging
    │
    ▼
Chrome Extension
    │
    ▼
浏览器执行：打开标签 / 填表 / 抓取 / 点击……
```

---

## 用户旅程：三步连接

```
安装 Extension  ──▶  完成认证  ──▶  在云端操作浏览器
```

1. **安装**：用户从 Chrome 应用商店或本地加载 Browser Bridge Extension。
2. **认证**：在 Extension 中完成身份校验（扫码、账号密码、或后续接入的其它方式）。认证模块被抽象为可插拔接口，支持多种 Provider。
3. **操控**：认证通过后，用户即可在云端通过 CLI 向自己的浏览器发送指令。

---

## 安全边界

- 只有经过认证的本地 Extension 才能注册到 WebSocket Server。
- CLI 发送的每条命令都经过服务端路由，不会直接暴露本地网络。
- 本地代理与 Extension 之间使用 Chrome Native Messaging，不监听外部端口。

---

## 快速开始

```bash
# 1. 安装依赖
bun install

# 2. 启动 WebSocket 服务
bun run dev:websocket

# 3. 构建 Extension（另一个终端）
bun run dev:extension

# 4. 在 Chrome 中加载 apps/extension/dist/ 目录

# 5. 运行 CLI
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
- **通信协议**：WebSocket + Chrome Native Messaging
- **类型检查**：TypeScript（strict）
- **代码风格**：Biome

---

> 更直观的完整架构图见 [`docs/architecture-diagram.html`](./docs/architecture-diagram.html)。

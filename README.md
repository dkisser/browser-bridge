<p align="center">
  <img src="./docs/assets/logo.png" alt="Browser Bridge Logo" width="160" />
</p>

<h1 align="center">Browser Bridge</h1>

<h3 align="center">Browser as a Tool for Any Agent</h3>

<p align="center">
  Let any AI agent, LLM, or script control your local browser.<br />
  Use the included CLI, the Claude Code skill, or any integration that speaks the bridge protocol.<br />
  Your sessions, cookies, and credentials stay local.
</p>

<p align="center">
  <a href="#-quick-start">Quick Start</a> •
  <a href="#-features">Features</a> •
  <a href="#-architecture">Architecture</a> •
  <a href="#-install">Install</a> •
  <a href="./README_CN.md">中文</a>
</p>

<p align="center">
  <img src="./docs/assets/news-demo.gif" alt="Browsing news with an agent" width="720" />
  <br />
  <em>Browsing news with an agent</em>
</p>

<p align="center">
  <img src="./docs/assets/gmail-demo.gif" alt="Managing Gmail with an agent" width="720" />
  <br />
  <em>Managing Gmail with an agent</em>
</p>

<p align="center">
  <strong>One-line pitch:</strong> Browser Bridge turns your local Chrome into a reusable tool for any agent.<br />
  One browser, any LLM, script, or terminal command — while keeping your data local.
</p>

---

## ✨ Features

- 🤖 **Agent-ready interface** — one bridge protocol, consumed via CLI, Claude Code skill, or custom integration.
- 🔒 **Local session, cloud control** — reuse your logged-in browser; no cloud browser or cookie sync needed.
- 🌉 **WebSocket bridge** — agents talk to a server, server talks to a local proxy, proxy talks to Chrome.
- 🧩 **Chrome Extension (MV3)** — built with Vite, loads as an unpacked extension.
- ⚡ **Bun + TypeScript** — fast startup, strict types, one package manager for the whole monorepo.
- 🧪 **Dev-friendly** — hot reload for server, proxy, and extension.

---

## 🚀 Quick Start

### 1. Install the bridge and extension

```bash
curl -fsSL https://github.com/dkisser/browser-bridge/releases/latest/download/install.sh | bash
```

Load `~/.browser-bridge/extension/` as an unpacked extension in Chrome, then run:

```bash
bridge up
```

### 2. Send your first command

```bash
bridge navigate https://github.com --browser <browser-id>
```

That’s it. The command travels from CLI → WebSocket server → local proxy → Chrome extension → browser.

> Use `bridge browser:list` to see the `<browser-id>` of your connected Chrome instance.

### 3. Use it from any agent

The `bridge` CLI is just one consumer of the bridge protocol. Browser Bridge ships with a ready-to-use Claude Code skill in [`./skills`](./skills/browser-bridge-user/SKILL.md), and anything that can open a WebSocket — for example, an MCP server you build, a custom SDK, or another agent framework — can send commands the same way.

---

## 🏗️ Architecture

```
┌─────────────┐      ┌─────────────────┐      ┌─────────────────┐      ┌─────────────────┐
│  CLI / Agent │ ───▶ │  WebSocket      │ ───▶ │  Local Proxy    │ ───▶ │  Chrome         │
│             │      │  Server         │      │  (your machine) │      │  Extension      │
└─────────────┘      └─────────────────┘      └─────────────────┘      └─────────────────┘
                                                                              │
                                                                              ▼
                                                                       ┌─────────────┐
                                                                       │   Chrome    │
                                                                       │  (browser)  │
                                                                       └─────────────┘
```

| Layer | Component | Role |
|-------|-----------|------|
| Cloud / shared | Interfaces | Agent-facing entry points: CLI, Claude Code skill, or any custom integration. |
| Cloud / shared | WebSocket Server | Routes commands to the right local proxy. |
| Local | Local Proxy | Maintains the outbound connection from your machine. |
| Local | Chrome Extension | Receives messages and executes browser actions. |

See [`docs/architecture-diagram.html`](./docs/architecture-diagram.html) for the full diagram.

---

## 📦 Install

### Option A: One-line installer (recommended)

```bash
curl -fsSL https://github.com/dkisser/browser-bridge/releases/latest/download/install.sh | bash
```

### Option B: One-line installer with Claude Code skill

If you already have [Claude Code](https://claude.ai/code), clone the repo and run the installer from the project root. It will install Browser Bridge plus the ready-to-use skill in `./skills`:

```bash
git clone https://github.com/dkisser/browser-bridge.git
cd browser-bridge
./install/install.sh
```

Use `--skills-dir <path>` if you want to install skills somewhere other than `~/.claude/skills/`. Use `--no-skills` to skip the skill installation.

### Option C: Build from source (contributors only)

See the [Development](#-development) section below. You only need this if you are contributing to Browser Bridge.

---

## 🛠️ Development

> The steps below are for contributors/developers only. End users do not need to install `bun` or `git`.

```bash
# 1. Install dependencies
bun install

# 2. Start the WebSocket server
bun run dev:websocket

# 3. In another terminal, start the local proxy
bun run dev:local-proxy

# 4. In a third terminal, build the extension
bun run dev:extension

# 5. Load apps/extension/dist/ as an unpacked extension in Chrome

# 6. Run the CLI
bun run cli
```

---

## 📂 Project Structure

```
Browser-Bridge/
├── apps/
│   ├── cli/            # CLI entrypoint (one bridge protocol consumer)
│   ├── extension/      # Chrome Extension (Manifest V3, Vite)
│   ├── local-proxy/    # Local WebSocket proxy
│   └── websocket/      # WebSocket server, client, and protocol
├── packages/
│   └── shared/         # Shared constants and utilities
├── install/            # One-line installer scripts
└── docs/               # Architecture diagrams and guides
```

---

## 🧰 Tech Stack

- **Runtime & package manager**: [Bun](https://bun.sh)
- **Extension build**: Vite + Manifest V3
- **Transport**: WebSocket
- **Type checking**: TypeScript (strict)
- **Linting & formatting**: Biome
- **Testing**: Bun test runner + Bats for install scripts

---

## 🛡️ Security

- Only authenticated extensions can register with the WebSocket server.
- Commands are routed through the server; the local network is not exposed directly.
- The local proxy connects outbound to the server and extension, minimizing open ports.

---

## 🤝 Contributing

Contributions are welcome. Please open an issue first to discuss significant changes.

---

## 📄 License

[MIT](./LICENSE)

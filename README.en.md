<p align="center">
  <img src="./docs/assets/logo.png" alt="Browser Bridge Logo" width="200" />
</p>

<p align="center">
  English | <a href="./README.md">中文</a>
</p>

# Browser Bridge

**Hold your browser securely from the cloud.**

Browser Bridge lets developers and agents remote-control a browser through the same command-line interface: search, fill forms, scrape pages, and manage tabs. For humans it is a CLI tool; for agents, it is a standard, orchestratable browser control entrypoint.

---

## In One Sentence

A two-part bridge system: a cloud-side CLI serves as the unified control interface, accepting input from users and any agent alike. Commands are sent over WebSocket to the user's local Chrome Extension, which performs the actions inside the browser.

---

## Why You Need It

- **Agents can control the browser too**: any LLM, script, or automation tool only needs to call the CLI to operate a real browser like a human, without learning the Chrome Extension API.
- **Automate repetitive work**: batch form filling, scheduled scraping, cross-site data collection.
- **Remote work**: let cloud scripts, shared team CLI tools, or agents operate your locally-logged-in browser.
- **Preserve browser state**: no headless browser or extra cookie management — directly reuse the real user session.

---

## Architecture: Two Parts, One Bridge

```
                        ┌─────────────────┐
                        │     Agent       │
                        │ (local or cloud)│
                        └────────┬────────┘
                                 │ calls CLI
                                 ▼
┌─────────────────────────────────────────────────┐
│                      CLOUD                      │
│  ┌─────────┐      ┌─────────────────────────┐   │
│  │   CLI   │──────▶│      WebSocket Server   │   │
│  │         │      │                         │   │
│  └─────────┘      └───────────┬─────────────┘   │
└───────────────────────────────┼─────────────────┘
                                │ WebSocket
                                │ Secure long-lived connection
┌───────────────────────────────┼─────────────────┐
│              LOCAL             ▼                │
│  ┌─────────────┐    ┌─────────────────┐         │
│  │   Chrome    │◀───│  WebSocket Local│         │
│  │  Extension  │    │    (proxy)      │         │
│  │             │    └─────────────────┘         │
│  └──────┬──────┘                                │
│         │ Native Messaging                      │
│         ▼                                       │
│  ┌─────────────┐                                │
│  │   Chrome    │                                │
│  │  (browser)  │                                │
│  └─────────────┘                                │
└─────────────────────────────────────────────────┘
```

| Part | Component | Responsibility |
|------|-----------|----------------|
| **Cloud** | CLI | Unified browser control interface; can be invoked by users and agents from local or cloud environments. |
|  | WebSocket Server | Receives CLI commands and forwards them to the matching local client. |
| **Local** | WebSocket Local | Local proxy that maintains the long-lived connection with the server. |
|  | Chrome Extension | Talks to the local proxy via Native Messaging and performs browser actions. |
|  | Chrome | Runs pages, manages tabs, executes DOM operations. |

---

## Data Flow: How a Command Reaches the Browser

```
User or agent invokes the CLI to enter a command
    │
    ▼
WebSocket Server authenticates and routes
    │
    ▼
WebSocket Local (proxy on the user's machine)
    │
    ▼
Chrome Native Messaging
    │
    ▼
Chrome Extension
    │
    ▼
Browser executes: open tab / fill form / scrape / click ...
```

---

## Who Uses It

Browser Bridge serves two kinds of callers:

- **End users**: type commands directly in the terminal to remote-control their own browser.
- **Agents / automation systems**: use the CLI as a standard entrypoint, letting LLMs, scripts, scheduled jobs, or other agents invoke it from local or cloud environments for complex browser-based workflows.

---

## User Journey: Three Steps to Connect

```
Install Extension  ──▶  Authenticate  ──▶  Human or agent controls the browser via the CLI
```

1. **Install**: load the Browser Bridge Extension from the Chrome Web Store or locally.
2. **Authenticate**: complete identity verification in the Extension (QR code, account/password, or future providers). The auth module is abstracted as a pluggable interface that supports multiple providers.
3. **Control**: once authenticated, a user or agent can send commands to the browser via the CLI. The agent may run locally or in the cloud.

---

## Security Boundaries

- Only authenticated local Extensions can register with the WebSocket Server.
- Every command from the CLI or an agent is routed through the server — the local network is never exposed directly.
- The local proxy talks to the Extension via Chrome Native Messaging and does not listen on external ports.

---

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/<org>/browser-bridge/main/install/install.sh | bash
```

Follow the printed instructions to load `~/.browser-bridge/extension/` in Chrome, then:

```bash
bridge up
```

See [`install/README.md`](./install/README.md) for details, error codes, and uninstall steps.

## Quick Start (Development)

```bash
# 1. Install dependencies
bun install

# 2. Start the WebSocket server
bun run dev:websocket

# 3. Start the local proxy (in another terminal)
bun run dev:local-proxy

# 4. Build the Extension (in another terminal)
bun run dev:extension

# 5. Load apps/extension/dist/ as an unpacked extension in Chrome

# 6. Run the CLI
bun run cli
```

---

## Project Structure

```
Browser-Bridge/
├── apps/
│   ├── cli/            # Cloud-side command-line tool
│   ├── extension/      # Chrome Extension (Manifest V3, Vite build)
│   └── websocket/      # WebSocket Server / Client / Protocol
├── packages/
│   └── shared/         # Shared constants and utilities
├── README.md
└── biome.json
```

---

## Tech Stack

- **Runtime / package manager**: Bun
- **Extension build**: Vite + Manifest V3
- **Transport**: WebSocket + Chrome Native Messaging
- **Type checking**: TypeScript (strict)
- **Code style**: Biome

---

> See [`docs/architecture-diagram.html`](./docs/architecture-diagram.html) for a more visual full-architecture diagram.
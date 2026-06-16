<p align="center">
  <img src="./docs/assets/logo.png" alt="Browser Bridge Logo" width="200" />
</p>

<p align="center">
  English | <a href="./README.md">中文</a>
</p>

# Browser Bridge

**Hold your browser securely from the cloud.**

Browser Bridge lets developers remote-control their own browser from the command line: search, fill forms, scrape pages, manage tabs — as if you were sitting in front of the computer, except the commands come from the cloud.

---

## In One Sentence

A two-part bridge system: a cloud-side CLI sends commands to the user's local Chrome Extension over WebSocket, and the Extension performs the actions inside the browser.

---

## Why You Need It

- **Automate repetitive work**: batch form filling, scheduled scraping, cross-site data collection.
- **Remote work**: let cloud scripts or shared team CLI tools operate your locally-logged-in browser.
- **Preserve browser state**: no headless browser or extra cookie management — directly reuse the real user session.

---

## Architecture: Two Parts, One Bridge

```
┌─────────────────────────────────────┐
│               CLOUD                 │
│  ┌─────────┐      ┌─────────────┐   │
│  │   CLI   │──────▶│ WebSocket   │   │
│  │ (where  │      │   Server    │   │
│  │ you type│      │             │   │
│  │commands)│      │             │   │
│  └─────────┘      └──────┬──────┘   │
└──────────────────────────┼──────────┘
                           │ WebSocket
                           │ Secure long-lived connection
┌──────────────────────────┼──────────┐
│              LOCAL        ▼         │
│  ┌─────────────┐    ┌───────────┐   │
│  │   Chrome    │◀───│ WebSocket │   │
│  │  Extension  │    │   Local   │   │
│  │             │    │  (proxy)  │   │
│  └──────┬──────┘    └───────────┘   │
│         │ Native Messaging           │
│         ▼                            │
│  ┌─────────────┐                     │
│  │   Chrome    │                     │
│  │  (browser)  │                     │
│  └─────────────┘                     │
└─────────────────────────────────────┘
```

| Part | Component | Responsibility |
|------|-----------|----------------|
| **Cloud** | CLI | User enters commands and sends them to the server. |
|  | WebSocket Server | Receives CLI commands and forwards them to the matching local client. |
| **Local** | WebSocket Local | Local proxy that maintains the long-lived connection with the server. |
|  | Chrome Extension | Talks to the local proxy via Native Messaging and performs browser actions. |
|  | Chrome | Runs pages, manages tabs, executes DOM operations. |

---

## Data Flow: How a Command Reaches the Browser

```
CLI command entered
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

## User Journey: Three Steps to Connect

```
Install Extension  ──▶  Authenticate  ──▶  Control browser from the cloud
```

1. **Install**: load the Browser Bridge Extension from the Chrome Web Store or locally.
2. **Authenticate**: complete identity verification in the Extension (QR code, account/password, or future providers). The auth module is abstracted as a pluggable interface that supports multiple providers.
3. **Control**: once authenticated, you can send commands from the cloud to your browser via the CLI.

---

## Security Boundaries

- Only authenticated local Extensions can register with the WebSocket Server.
- Every CLI command is routed through the server — the local network is never exposed directly.
- The local proxy talks to the Extension via Chrome Native Messaging and does not listen on external ports.

---

## Quick Start

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
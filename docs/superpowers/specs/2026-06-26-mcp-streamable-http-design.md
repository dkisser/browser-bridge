# Browser Bridge MCP Server Design

**Date:** 2026-06-26  
**Topic:** Add a Streamable HTTP MCP server so agents can control browsers through Browser Bridge  
**Status:** Design approved

## 1. Goal

Browser Bridge already exposes a WebSocket protocol and a CLI. This design adds a **Model Context Protocol (MCP) server** using **Streamable HTTP** so any MCP-compatible agent (Claude Desktop, Cursor, etc.) can discover connected browsers and send control commands without using the CLI.

## 2. Non-Goals

- Replace the CLI or WebSocket protocol. The MCP layer is an additional consumer.
- Add remote/cloud MCP access. The first version is localhost-only.
- Expose every possible CLI command from day one. We start with a curated subset.

## 3. Architecture

### 3.1 Deployment Model

The MCP server runs **inside the existing `apps/websocket` Bun process**, co-located with the WebSocket server. It uses FastMCP's `httpStream` transport and binds to a dedicated port.

```
┌─────────────────────────────────────┐
│   Browser Bridge Server Process     │
│   (Bun)                             │
│                                     │
│  ┌──────────────┐  ┌─────────────┐  │
│  │ WebSocket    │  │ FastMCP     │  │
│  │ port 3001    │  │ Streamable  │  │
│  │              │  │ HTTP port   │  │
│  │              │  │ 3003        │  │
│  └──────┬───────┘  └──────┬──────┘  │
└─────────┼─────────────────┼─────────┘
          │                 │
          │ WebSocket       │ Streamable HTTP
          │                 │
    ┌─────▼─────┐      ┌────▼────┐
    │ Browser   │      │ Agent   │
    │ Extension │      │ Client  │
    │ / Proxy   │      │         │
    └───────────┘      └─────────┘
```

### 3.2 Why a Dedicated Port

FastMCP's `httpStream` transport expects to own its own HTTP server. Rather than fighting its internals or hand-rolling a Bun.serve adapter, we let it bind to its own port (`BRIDGE_MCP_PORT`, default `3003`). The WebSocket server continues on `3001`. This keeps both transports simple and independent.

### 3.3 Component Breakdown

| Component | Location | Responsibility |
|-----------|----------|----------------|
| WebSocket + MCP bootstrap | `apps/websocket/src/index.ts` | Starts Bun.serve and FastMCP side-by-side, reads env vars |
| FastMCP server | `apps/websocket/src/mcp/server.ts` | FastMCP instance, tool registration, lifecycle |
| Browser session state | `apps/websocket/src/mcp/browser-session.ts` | Per-MCP-connection selected browser and timeout. Persisted for the lifetime of the HTTP stream connection. |
| Tool handlers | `apps/websocket/src/mcp/tools/*.ts` | One file per MCP tool |
| Shared client | `@browser-bridge/websocket/client` | Re-used to send commands to the WebSocket server |
| Shared types | `@browser-bridge/shared` | `CommandType`, `Envelope`, `ResponsePayload`, etc. |

## 4. MCP Tools (Curated Subset)

All browser-control tools accept an optional `timeout_ms` parameter that overrides the default command timeout.

| Tool | Description | Key Parameters |
|------|-------------|----------------|
| `list_browsers` | List connected browsers and their status | — |
| `set_browser` | Explicitly pin a browser for this MCP session | `browserId: string` |
| `navigate` | Navigate the active tab to a URL | `url: string`, `timeout_ms?: number` |
| `click` | Click an element by CSS selector | `selector: string`, `timeout_ms?: number` |
| `type` | Type text into an input | `selector: string`, `text: string`, `submit?: boolean`, `timeout_ms?: number` |
| `screenshot` | Capture the page as base64 PNG | `fullPage?: boolean`, `timeout_ms?: number` |
| `pageinfo` | Return title, URL, and tab list | `timeout_ms?: number` |

### 4.1 Browser Resolution Rules

When a browser-control tool is called:

1. Use the browserId explicitly set via `set_browser` in this session.
2. If none is set, call `list_browsers` internally.
3. If exactly one browser is online, use it automatically.
4. If zero or multiple browsers are online, return an error that includes the browser list.

## 5. Data Flow

1. Agent POSTs a JSON-RPC request to `http://localhost:3003/mcp` (exact path follows FastMCP's `httpStream` default; documented and validated during implementation).
2. FastMCP parses the tool call.
3. Tool handler resolves the target browserId.
4. Tool handler creates a short-lived `ManagedClient` to `ws://localhost:3001`.
5. Tool handler sends an `Envelope { type: 'command', browserId, payload }`.
6. WebSocket server forwards the command to the browser extension/proxy.
7. Browser returns a response envelope.
8. Tool handler maps `ResponsePayload` to an MCP tool result.
9. FastMCP streams the JSON-RPC response back to the agent.

## 6. Error Handling

| Situation | MCP Result |
|-----------|------------|
| No browser online | Error: "No browser connected. Start the extension/local-proxy first." |
| Multiple browsers online | Error with browser list; suggest calling `set_browser` |
| Browser command timeout | Error: "Browser did not respond within timeout" |
| Browser command returns error | Error containing the browser's error message |
| WebSocket server unreachable | Error: "Cannot reach Browser Bridge WebSocket server on localhost:3001" |
| Invalid tool arguments | Standard MCP invalid-params error |

## 7. Authentication

For the first version, the MCP endpoint binds to `127.0.0.1` by default and accepts connections without authentication. The bind address is controlled by `BRIDGE_MCP_HOSTNAME`; changing it to a non-loopback address is considered a future-auth scenario. The existing WebSocket server continues to use API-key auth when `BRIDGE_API_KEYS` is configured. This keeps local setup friction minimal while preserving the production auth path for the WebSocket protocol.

If the MCP hostname is changed to a non-loopback address, a future version can add a bearer token requirement.

## 8. Configuration

| Environment Variable | Default | Purpose |
|----------------------|---------|---------|
| `BRIDGE_MCP_PORT` | `3003` | Port for the Streamable HTTP MCP endpoint |
| `BRIDGE_MCP_HOSTNAME` | `127.0.0.1` | Bind address (localhost only by default) |
| `BRIDGE_MCP_TIMEOUT_MS` | `10000` | Default per-command timeout |

## 9. Testing Strategy

- **Unit tests:** Each tool handler tested with a mocked WebSocket client and registry.
- **Browser resolution tests:** zero, one, multiple browsers; explicit `set_browser`; missing browser.
- **Integration tests:** Start real Bun WebSocket server + FastMCP, connect via an MCP client, and exercise end-to-end tool calls.
- **Error tests:** timeout, unreachable ws-server, browser command error, malformed tool args.
- **Coverage target:** 80%+ for new MCP code.

## 10. Installation Guide

Users do not install a separate package. The MCP endpoint becomes available automatically when the Browser Bridge WebSocket server is running.

### 10.1 Claude Desktop

Add to `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "browser-bridge": {
      "transport": "streamableHttp",
      "url": "http://localhost:3003/mcp"
    }
  }
}
```

### 10.2 Cursor

Add to Cursor MCP settings:

```json
{
  "mcpServers": {
    "browser-bridge": {
      "transport": "streamableHttp",
      "url": "http://localhost:3003/mcp"
    }
  }
}
```

### 10.3 Documentation

- Update `README.md` with an MCP section and copy-paste config snippets.
- Create `docs/mcp-setup.md` with a detailed guide, troubleshooting, and tool reference.
- Update `skills/browser-bridge-user/` to mention MCP as an alternative to the CLI.

## 11. Dependencies

Add to `apps/websocket/package.json`:

- `fastmcp` — FastMCP TypeScript framework for MCP servers
- `zod` — Runtime schema validation for tool parameters

## 12. Future Considerations

- Add more tools (tab management, waiting, extraction) as usage grows.
- Evaluate re-using the same Bun.serve port with a custom FastMCP transport adapter.
- Add optional bearer-token auth when binding to non-loopback addresses.
- Consider caching the browser list for a short window to avoid repeated `list_browsers` calls.

## 13. Decisions Log

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Deployment | Same process as ws-server | Single binary to run, simplest install |
| Transport | Streamable HTTP | Required by user; modern MCP transport |
| Port | Dedicated port 3003 | FastMCP httpStream transport owns its own HTTP server |
| Initial tools | Curated subset | Covers 80% agent use case with smaller surface |
| Browser selection | Auto-detect single browser | Low friction; `set_browser` available for multiple browsers |
| Auth | None on localhost | Minimal setup; WebSocket auth remains separate |
| Timeout | Per-tool `timeout_ms` override | User requested ability to set timeout per MCP call |
| Installation docs | README + dedicated doc | Clear, copy-paste friendly guidance |
| Implementation library | FastMCP TypeScript (`fastmcp`) | User preference; native Bun support; ergonomic API |

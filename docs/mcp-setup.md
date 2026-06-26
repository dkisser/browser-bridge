# Browser Bridge MCP Setup

Browser Bridge exposes an MCP server over Streamable HTTP so agents can control browsers directly.

## Start the server

```bash
bun run dev:websocket
```

The MCP endpoint is available at `http://localhost:3003/mcp`.

## Claude Desktop

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

## Cursor

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

## Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `BRIDGE_MCP_PORT` | `3003` | MCP HTTP port |
| `BRIDGE_MCP_HOSTNAME` | `127.0.0.1` | Bind address |
| `BRIDGE_MCP_TIMEOUT_MS` | `10000` | Default command timeout |

## Available tools

- `list_browsers` — list connected browsers
- `set_browser` — choose a browser for this session
- `navigate` — open a URL
- `click` — click an element by selector
- `type` — type text into an input
- `screenshot` — capture the page
- `pageinfo` — get title, URL, tabs

Each browser-control tool accepts an optional `timeout_ms` argument.

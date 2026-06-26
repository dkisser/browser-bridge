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

| Tool | Description | Parameters |
|---|---|---|
| `list_browsers` | List connected browsers and their status | — |
| `set_browser` | Pin a browser for this MCP session | `browserId: string` |
| `navigate` | Navigate the active tab to a URL | `url: string`, `timeout_ms?: number` |
| `go_back` | Go back one page in browser history | `timeout_ms?: number` |
| `go_forward` | Go forward one page in browser history | `timeout_ms?: number` |
| `refresh` | Refresh the current page | `timeout_ms?: number` |
| `tab_list` | List all open tabs | `timeout_ms?: number` |
| `tab_new` | Open a new tab | `url?: string`, `timeout_ms?: number` |
| `tab_close` | Close a tab by ID | `tabId: number`, `timeout_ms?: number` |
| `tab_switch` | Switch to a tab by ID | `tabId: number`, `timeout_ms?: number` |
| `click` | Click an element by CSS selector | `selector: string`, `timeout_ms?: number` |
| `type` | Type text into an input | `selector: string`, `text: string`, `submit?: boolean`, `timeout_ms?: number` |
| `select` | Select a dropdown option by value | `selector: string`, `value: string`, `timeout_ms?: number` |
| `scroll` | Scroll the page or an element | `x: number`, `y: number`, `selector?: string`, `timeout_ms?: number` |
| `hover` | Hover over an element | `selector: string`, `timeout_ms?: number` |
| `get_text` | Get the text content of an element | `selector: string`, `timeout_ms?: number` |
| `get_html` | Get the inner HTML of an element | `selector: string`, `timeout_ms?: number` |
| `screenshot` | Capture a base64 PNG screenshot | `fullPage?: boolean`, `timeout_ms?: number` |
| `pageinfo` | Get title, URL, and tab list | `timeout_ms?: number` |
| `wait_element` | Wait for an element to appear | `selector: string`, `timeout_ms?: number` |
| `wait_navigation` | Wait for page navigation to complete | `timeout_ms?: number` |

Each browser-control tool accepts an optional `timeout_ms` argument.

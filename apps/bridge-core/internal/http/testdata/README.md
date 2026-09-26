# Golden fixture: TS tools/list

`tools_list_ts.json` is the `result.tools` array exported from the TS
(FastMCP) bridge-core MCP server — the wire contract the Go port must match.

Regeneration:

1. `git worktree add .worktrees/main-ref main` (repo root) and `bun install`
   inside the worktree.
2. Start only the MCP server on a scratch port with a small bun script that
   calls `startMcpServer({ websocketUrl: 'ws://127.0.0.1:3299', port: 3203,
   hostname: '127.0.0.1', defaultTimeoutMs: 10000, version: '0.3.2' })` — the
   websocketUrl is never dialed for tools/list.
3. Drive it over StreamableHTTP: POST `initialize` (capture the
   `mcp-session-id` response header), then POST `tools/list` with that header;
   parse the SSE `data:` line. Take `result.tools`.
4. Pretty-print with two-space indent into this file.

Never run the TS server on ports 3001-3003 (production install). 3203 is the
scratch port used originally.

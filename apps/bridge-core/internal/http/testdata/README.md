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

## This file now diverges from the TS server on purpose

Two properties were removed from this fixture after the snapshot was taken,
and regenerating from a `main` worktree will bring them back:

- `tab_new.auto_close`
- `screenshot.fullPage`

Both were declared all the way down to the wire and implemented nowhere. The
extension read neither, so an agent asking for `fullPage: true` got a
viewport screenshot, a success response, and no way to tell the difference —
the worst kind of wrong answer, because the caller has nothing to check it
against. `auto_close` had no defined moment to close at, and the one reading
that would have worked, closing the tab when the call returned, contradicts
`screenshot`, which needs the tab to stay open and visible.

Neither is a small patch. `fullPage` needs either `chrome.debugger` (a
permission that puts a banner on the user's toolbar) or scroll-and-stitch
(which duplicates fixed headers and misses content that lazy-loads below the
fold), and `auto_close` needs a session concept this architecture does not
have. Both are features with failure modes worth designing deliberately.

So they were removed instead of implemented, and because the tool schemas are
`additionalProperties: false`, a client that still passes one now gets a
validation error rather than a silently wrong image. The Go server is the
contract on this branch — the TS control plane is deleted — so the snapshot's
original job, "match what TS served", has been overtaken by "record what we
deliberately serve". If you regenerate, re-apply this section.

# Security enforcement lives in the extension

The control panel, the policy store, and the enforcement of every gate live in the browser extension; local-proxy gains only a pairing-token check on its WebSocket upgrade and otherwise stays a dumb pipe. Decided when designing the safety layer: the product drives the user's everyday logged-in browser, so the primary threats are agent misoperation and prompt injection, not remote network attackers (everything already binds to 127.0.0.1). The extension is the execution point — every command already runs through its service worker and content scripts — so that is where a command can be stopped with full page context, and where the human already is.

## Considered Options

- **Local-proxy-hosted control panel**: rejected. The proxy had no auth story (CORS was `*`) — a panel there means building authentication and a brand-new web attack surface, and approval requests would still have to travel back into the browser where the human and the page live.
- **MCP-server-side gating**: rejected. It blocks tools before any page context exists, and asking the human mid-session depends on each MCP client's capabilities. The browser is the only surface where the human can see exactly which element is about to be clicked.
- **Extension (chosen)**: zero new attack surface beyond what `<all_urls>` already grants, popup UI already exists, and enforcement sits at the last possible point before an action executes.

## Consequences

- Policy state lives in extension storage, not `~/.browser-bridge/config.json`; the CLI cannot read or edit policy directly (single-profile product, no multi-browser story).
- The local-proxy "bloat" concern is moot: it keeps only token verification plus the existing HTTP status API.
- Authorization for non-local deployments (cloud ws-server) is an explicitly deferred branch, not covered by this decision.

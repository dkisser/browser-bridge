# Popup Cloud Connection Switch Design

## Date

2026-06-19

## Goal

Add a switch in the Chrome extension popup to control the connection between `local-proxy` and `ws-server` (the cloud WebSocket server). The existing "Connect to Local Proxy" button is removed because the new switch covers the primary user-facing connection control.

> Scope note: this switch controls **local-proxy ↔ ws-server** only. The **extension ↔ local-proxy** WebSocket connection remains managed automatically by the offscreen document so that cloud commands can still be relayed to the browser.

## Architecture

### Components

| Component | Change | Responsibility |
|---|---|---|
| `local-proxy` HTTP layer (`LocalServer`) | Extend `Bun.serve` `fetch` handler | Expose `/api/status`, `/api/connect`, `/api/disconnect` |
| `CloudClient` (`apps/local-proxy/src/cloud-client.ts`) | Add `manualDisconnect` flag | Distinguish user-initiated disconnect from accidental disconnect; suppress auto-reconnect when user explicitly turns off |
| Extension popup (`popup.html`, `popup.ts`) | Replace "Connect to Local Proxy" button with a toggle switch | Display cloud connection state and send connect/disconnect commands |
| Shared constants (`packages/shared/src/constants.ts`) | Ensure `LOCAL_WS_PORT` is the single source of truth | Both local-proxy and popup use the same port for HTTP/WebSocket |

### Data Flow

```
┌─────────────┐  fetch: /api/*  ┌──────────────┐  WebSocket  ┌──────────┐
│ popup (MV3) │ ◄──────────────►│ local-proxy  │ ◄──────────►│ ws-server│
└─────────────┘                 │  :3002       │             │  :3001   │
                                └──────────────┘             └──────────┘
                                       ▲
                                       │ WebSocket
                                ┌──────┴──────┐
                                │  offscreen  │
                                │  document   │
                                └─────────────┘
```

The popup talks directly to the local-proxy HTTP surface instead of going through the background service worker / offscreen document. This keeps the control plane simple and stateless from the popup's perspective.

## State Machine

```
                    ┌─────────────┐
           ┌───────►│  connected  │◄────────┐
           │        └─────────────┘         │
           │              │                 │
           │    network   │    user         │   user
           │   disconnect │  disconnect     │  connect
           │              ▼                 │
           │        ┌─────────────┐         │
           └────────┤  offline    │─────────┘
                    └─────────────┘
                           │
                           │ auto-reconnect
                           │ (only if manualDisconnect === false)
                           ▼
                    ┌─────────────┐
                    │ connecting  │
                    └─────────────┘
```

### `CloudClient` Behavior

- `manualDisconnect` defaults to `false` on process start.
- `POST /api/connect` sets `manualDisconnect = false` and calls `cloud.connect()`.
- `POST /api/disconnect` sets `manualDisconnect = true`, clears any pending reconnect timer, and calls `cloud.close()`.
- On `onClose`, `scheduleReconnect()` is only invoked when `manualDisconnect === false`.
- The flag is runtime-only; a process restart resumes the default auto-connect behavior.
- While `cloud.connect()` is in progress, `/api/status` reports `connected: false` because the socket is not yet `OPEN`.

## API Design

All endpoints listen on the same port as the WebSocket server (`LOCAL_WS_PORT`, default `3002`).

### `GET /api/status`

Returns the current cloud connection state.

**Response 200:**

```json
{
  "success": true,
  "data": {
    "connected": true,
    "browserId": "b-a1b2c3d4",
    "serverUrl": "ws://localhost:3001",
    "manualDisconnect": false
  }
}
```

### `POST /api/connect`

Initiates a connection to the configured ws-server.

**Response 200 (already connected or newly connected):**

```json
{
  "success": true,
  "data": { "connected": true }
}
```

**Response 200 (failed):**

```json
{
  "success": false,
  "error": "Connection timeout"
}
```

### `POST /api/disconnect`

Closes the cloud connection and suppresses auto-reconnect.

**Response 200:**

```json
{
  "success": true,
  "data": { "connected": false }
}
```

### CORS

Because the popup runs under `chrome-extension://<id>`, responses must include:

```
Access-Control-Allow-Origin: *
Access-Control-Allow-Methods: GET, POST, OPTIONS
Access-Control-Allow-Headers: Content-Type
```

This is acceptable for a localhost-only development tool. If authentication or remote management is added later, CORS must be tightened.

## Popup Behavior

1. **On open:** immediately `GET /api/status` and render the toggle.
2. **While open:** poll `/api/status` every **5 seconds** and update the toggle if the state changed in the background.
3. **On toggle:**
   - Call `POST /api/connect` or `POST /api/disconnect`.
   - Show a transient "Connecting…" / "Disconnecting…" label.
   - On completion, immediately refresh status via `GET /api/status`.
4. **On error:** if local-proxy is unreachable, disable the switch and show "Local proxy unreachable".

## Error Handling

| Scenario | local-proxy behavior | popup behavior |
|---|---|---|
| local-proxy not running | Connection refused | Disable switch, show "Local proxy unreachable" |
| Connect already in progress | Return `{ success: false, error: "Connection in progress" }` | Show "Connecting…" and wait for next poll |
| Connect timeout | `cloud.connect()` rejects | Show "Connection failed" |
| Already connected | Idempotent success | No UI change |
| Already disconnected | Idempotent success | No UI change |
| User disconnect then network drops | No auto-reconnect because `manualDisconnect = true` | Switch remains off |

## Testing Strategy

### Unit Tests (`apps/local-proxy`)

- `CloudClient` sets `manualDisconnect = true` on explicit disconnect.
- `CloudClient` does not schedule reconnect when `manualDisconnect = true`.
- `CloudClient` still auto-reconnects when `manualDisconnect = false`.
- `GET /api/status` returns correct `connected` / `manualDisconnect` values.
- `POST /api/connect` invokes `cloud.connect()`.
- `POST /api/disconnect` invokes `cloud.close()`.
- CORS headers are present on all HTTP responses.

### Integration Tests (`apps/extension` + `apps/local-proxy`)

- Popup renders switch state from `/api/status` on open.
- Toggling the switch updates the cloud connection state.
- 5-second polling updates the UI when the cloud state changes externally.
- Popup disables the switch when local-proxy is unreachable.

### E2E Tests

- Start ws-server and local-proxy.
- Open the extension popup; verify switch is on and cloud reports browser online.
- Turn switch off; verify ws-server reports browser offline.
- Turn switch on; verify ws-server reports browser online again.

## Out of Scope

- Controlling the extension ↔ local-proxy WebSocket connection from the popup.
- Persisting `manualDisconnect` across local-proxy restarts.
- Adding authentication to the HTTP API.
- Remote configuration of `serverUrl` from the popup.

## Decision Log

| Decision | Rationale |
|---|---|
| HTTP API on local-proxy instead of message passing through background SW | Simpler control plane; popup can read state without waking the service worker |
| Direct `fetch` from popup to `localhost:3002` | Extension already has `<all_urls>` host permission; avoids adding a new relay layer |
| `manualDisconnect` runtime flag only | Keeps process startup behavior unchanged (auto-connect) and avoids config file churn |
| 5-second polling while popup is open | Balances freshness with minimal localhost traffic |
| Remove "Connect to Local Proxy" button | Redundant once the cloud switch is the primary control |

## Related Files

- `apps/local-proxy/src/local-server.ts`
- `apps/local-proxy/src/cloud-client.ts`
- `apps/local-proxy/src/config.ts`
- `apps/extension/src/popup.html`
- `apps/extension/src/popup.ts`
- `packages/shared/src/constants.ts`
- `packages/shared/src/types.ts`

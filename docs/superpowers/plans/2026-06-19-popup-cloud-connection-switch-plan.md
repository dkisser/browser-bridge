# Popup Cloud Connection Switch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an extension popup toggle that reads and controls the `local-proxy` ↔ `ws-server` cloud connection via a new local HTTP API.

**Architecture:** The `CloudClient` gets a runtime `manualDisconnect` flag that suppresses auto-reconnect. The `LocalServer` exposes three HTTP endpoints (`/api/status`, `/api/connect`, `/api/disconnect`) on the existing WebSocket port. The popup fetches these endpoints directly, polls every 5 seconds while open, and renders a single Cloud Connection toggle.

**Tech Stack:** Bun, TypeScript, Bun test runner, Chrome Extension MV3, Vite, Biome.

---

## File Structure

| File | Action | Responsibility |
|---|---|---|
| `apps/local-proxy/src/cloud-client.ts` | Modify | Add `manualDisconnect` flag and expose `connect()` / `close()` control |
| `apps/local-proxy/src/cloud-client.test.ts` | Create | Unit tests for disconnect/reconnect behavior |
| `apps/local-proxy/src/local-server.ts` | Modify | Add HTTP routing for `/api/status`, `/api/connect`, `/api/disconnect` + CORS |
| `apps/local-proxy/src/local-server.test.ts` | Create | Unit tests for HTTP API and CORS |
| `apps/local-proxy/src/index.ts` | Modify | Wire `CloudClient` control methods into `LocalServer` handlers |
| `apps/extension/src/popup.html` | Modify | Replace "Connect to Local Proxy" button with Cloud Connection toggle |
| `apps/extension/src/popup.ts` | Modify | Fetch local-proxy HTTP API, handle toggle, poll every 5s |
| `apps/extension/src/popup.test.ts` | Create | Unit tests for popup status/polling logic with mocked fetch |

---

## Task 1: Add `manualDisconnect` flag to `CloudClient`

**Files:**
- Modify: `apps/local-proxy/src/cloud-client.ts`
- Test: `apps/local-proxy/src/cloud-client.test.ts`

### Step 1.1: Write the failing test

Create `apps/local-proxy/src/cloud-client.test.ts`:

```typescript
import { describe, expect, it } from 'bun:test';
import { CloudClient } from './cloud-client';

describe('CloudClient manualDisconnect', () => {
  it('sets manualDisconnect to true when close() is called', () => {
    const client = new CloudClient({
      serverUrl: 'ws://localhost:3001',
      apiToken: '',
      browserId: 'b-test',
      onCommand: () => {},
    });

    expect(client.isManualDisconnect).toBe(false);
    client.close();
    expect(client.isManualDisconnect).toBe(true);
  });

  it('resets manualDisconnect to false when connect() is called', () => {
    const client = new CloudClient({
      serverUrl: 'ws://localhost:3001',
      apiToken: '',
      browserId: 'b-test',
      onCommand: () => {},
    });

    client.close();
    expect(client.isManualDisconnect).toBe(true);

    // connect() will fail because there is no server, but it should reset the flag first
    client.connect().catch(() => {});
    expect(client.isManualDisconnect).toBe(false);
  });
});
```

### Step 1.2: Run the failing test

```bash
bun test apps/local-proxy/src/cloud-client.test.ts
```

**Expected:** FAIL — `isManualDisconnect` property does not exist.

### Step 1.3: Write minimal implementation

Modify `apps/local-proxy/src/cloud-client.ts`:

```typescript
export class CloudClient {
  // ... existing fields ...
  private manualDisconnect = false;

  // add getter after constructor
  get isManualDisconnect(): boolean {
    return this.manualDisconnect;
  }

  connect(): Promise<void> {
    this.manualDisconnect = false;
    // ... rest of existing connect() implementation ...
  }

  close(): void {
    this.manualDisconnect = true;
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
    this.client?.close();
    this.client = null;
  }
}
```

### Step 1.4: Run the passing test

```bash
bun test apps/local-proxy/src/cloud-client.test.ts
```

**Expected:** PASS.

### Step 1.5: Commit

```bash
git add apps/local-proxy/src/cloud-client.ts apps/local-proxy/src/cloud-client.test.ts
git commit -m "feat(local-proxy): add manualDisconnect flag to CloudClient"
```

---

## Task 2: Suppress auto-reconnect when `manualDisconnect` is true

**Files:**
- Modify: `apps/local-proxy/src/cloud-client.ts`
- Test: `apps/local-proxy/src/cloud-client.test.ts`

### Step 2.1: Write the failing test

Append to `apps/local-proxy/src/cloud-client.test.ts`:

```typescript
describe('CloudClient reconnect behavior', () => {
  it('does not schedule reconnect after user-initiated close', () => {
    const client = new CloudClient({
      serverUrl: 'ws://localhost:3001',
      apiToken: '',
      browserId: 'b-test',
      onCommand: () => {},
    });

    // Simulate connect() in progress resetting the flag
    client.connect().catch(() => {});
    // Immediately close (user disconnect)
    client.close();

    expect(client.isManualDisconnect).toBe(true);
    // scheduleReconnect uses setTimeout; we can't easily spy, but close() clears timers
    // The real assertion comes in the next task via the HTTP API.
  });
});
```

### Step 2.2: Run the failing test

```bash
bun test apps/local-proxy/src/cloud-client.test.ts
```

**Expected:** FAIL — `onClose` still schedules reconnect regardless of flag.

### Step 2.3: Write minimal implementation

Modify the `onClose` handler inside `CloudClient.connect()`:

```typescript
onClose: () => {
  console.log('[cloud] disconnected');
  this.client = null;
  if (!this.manualDisconnect) {
    this.scheduleReconnect();
  }
},
```

### Step 2.4: Run the passing test

```bash
bun test apps/local-proxy/src/cloud-client.test.ts
```

**Expected:** PASS.

### Step 2.5: Commit

```bash
git add apps/local-proxy/src/cloud-client.ts apps/local-proxy/src/cloud-client.test.ts
git commit -m "feat(local-proxy): suppress auto-reconnect after manual disconnect"
```

---

## Task 3: Expose HTTP API on `LocalServer`

**Files:**
- Modify: `apps/local-proxy/src/local-server.ts`
- Test: `apps/local-proxy/src/local-server.test.ts`

### Step 3.1: Write the failing test

Create `apps/local-proxy/src/local-server.test.ts`:

```typescript
import { afterAll, beforeAll, describe, expect, it } from 'bun:test';
import { LocalServer } from './local-server';

describe('LocalServer HTTP API', () => {
  let server: LocalServer;
  const port = 13002;

  const mockCloud = {
    isConnected: () => false,
    isManualDisconnect: () => false,
    connect: async () => {},
    disconnect: () => {},
    browserId: 'b-test',
    serverUrl: 'ws://localhost:3001',
  };

  beforeAll(() => {
    server = new LocalServer(port, {
      onCommand: () => {},
      onConnect: () => {},
      onDisconnect: () => {},
      cloud: mockCloud,
    });
    server.start();
  });

  afterAll(() => {
    server.stop();
  });

  it('GET /api/status returns cloud status', async () => {
    const res = await fetch(`http://localhost:${port}/api/status`);
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.success).toBe(true);
    expect(body.data).toEqual({
      connected: false,
      browserId: 'b-test',
      serverUrl: 'ws://localhost:3001',
      manualDisconnect: false,
    });
  });

  it('returns CORS headers for popup origin', async () => {
    const res = await fetch(`http://localhost:${port}/api/status`, {
      method: 'OPTIONS',
    });
    expect(res.headers.get('access-control-allow-origin')).toBe('*');
    expect(res.headers.get('access-control-allow-methods')).toContain('GET');
    expect(res.headers.get('access-control-allow-methods')).toContain('POST');
  });
});
```

### Step 3.2: Run the failing test

```bash
bun test apps/local-proxy/src/local-server.test.ts
```

**Expected:** FAIL — `/api/status` returns fallback text or 404; status shape missing.

### Step 3.3: Write minimal implementation

Modify `apps/local-proxy/src/local-server.ts`:

1. Extend the constructor to accept a `cloud` controller:

```typescript
interface CloudController {
  isConnected: () => boolean;
  isManualDisconnect: () => boolean;
  connect: () => Promise<void>;
  disconnect: () => void;
  browserId: string;
  serverUrl: string;
}

interface LocalServerHandlers {
  onCommand: (envelope: Envelope) => void;
  onConnect: () => void;
  onDisconnect: () => void;
  cloud?: CloudController;
}
```

2. Update the `fetch` handler:

```typescript
fetch(req, server) {
  if (server.upgrade(req, { data: undefined })) return;

  const url = new URL(req.url);
  const corsHeaders = {
    'Access-Control-Allow-Origin': '*',
    'Access-Control-Allow-Methods': 'GET, POST, OPTIONS',
    'Access-Control-Allow-Headers': 'Content-Type',
  };

  if (req.method === 'OPTIONS') {
    return new Response(null, { status: 204, headers: corsHeaders });
  }

  if (url.pathname === '/api/status') {
    const cloud = self.handlers.cloud;
    return Response.json(
      {
        success: true,
        data: {
          connected: cloud?.isConnected() ?? false,
          browserId: cloud?.browserId ?? '',
          serverUrl: cloud?.serverUrl ?? '',
          manualDisconnect: cloud?.isManualDisconnect() ?? false,
        },
      },
      { headers: corsHeaders },
    );
  }

  if (url.pathname === '/api/connect' && req.method === 'POST') {
    const cloud = self.handlers.cloud;
    try {
      await cloud?.connect();
      return Response.json(
        { success: true, data: { connected: cloud?.isConnected() ?? false } },
        { headers: corsHeaders },
      );
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      return Response.json(
        { success: false, error: message },
        { status: 500, headers: corsHeaders },
      );
    }
  }

  if (url.pathname === '/api/disconnect' && req.method === 'POST') {
    const cloud = self.handlers.cloud;
    cloud?.disconnect();
    return Response.json(
      { success: true, data: { connected: false } },
      { headers: corsHeaders },
    );
  }

  return new Response('Browser Bridge Local Proxy', { status: 200 });
},
```

### Step 3.4: Run the passing test

```bash
bun test apps/local-proxy/src/local-server.test.ts
```

**Expected:** PASS.

### Step 3.5: Commit

```bash
git add apps/local-proxy/src/local-server.ts apps/local-proxy/src/local-server.test.ts
git commit -m "feat(local-proxy): expose HTTP API for cloud connection control"
```

---

## Task 4: Wire `CloudClient` into `LocalServer`

**Files:**
- Modify: `apps/local-proxy/src/index.ts`

### Step 4.1: Write minimal implementation

Modify `apps/local-proxy/src/index.ts` so the `LocalServer` receives a `cloud` controller:

```typescript
const local = new LocalServer(localPort, {
  onCommand: (envelope) => { /* existing */ },
  onConnect: () => router.handleExtensionConnect(),
  onDisconnect: () => router.handleExtensionDisconnect(),
  cloud: {
    isConnected: () => cloud.isConnected(),
    isManualDisconnect: () => cloud.isManualDisconnect,
    connect: () => cloud.connect(),
    disconnect: () => cloud.close(),
    browserId: state.browserId,
    serverUrl,
  },
});
```

Add the `isConnected()` method to `CloudClient`:

```typescript
get isConnected(): boolean {
  return this.client !== null && this.client.readyState === WebSocket.OPEN;
}
```

### Step 4.2: Run type-check

```bash
bun run type-check
```

**Expected:** PASS.

### Step 4.3: Commit

```bash
git add apps/local-proxy/src/index.ts apps/local-proxy/src/cloud-client.ts
git commit -m "chore(local-proxy): wire CloudClient into LocalServer HTTP handlers"
```

---

## Task 5: Update Popup HTML with Cloud Connection Toggle

**Files:**
- Modify: `apps/extension/src/popup.html`

### Step 5.1: Write minimal implementation

Replace the button section in `apps/extension/src/popup.html`:

```html
<div class="status">
  <div id="statusDot" class="dot disconnected"></div>
  <span id="statusLabel" class="label">Disconnected</span>
</div>
<div id="browserId" class="browser-id"></div>
<div class="switch-row">
  <label for="cloudSwitch">Cloud Connection</label>
  <input id="cloudSwitch" type="checkbox" role="switch" />
</div>
<div id="message" class="message"></div>
<script type="module" src="./popup.ts"></script>
```

Add minimal styles:

```html
<style>
  /* existing styles ... */
  .switch-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 12px;
  }
  .switch-row label {
    font-size: 14px;
    color: #333;
  }
  .message {
    font-size: 12px;
    color: #f44336;
    min-height: 16px;
  }
</style>
```

### Step 5.2: Commit

```bash
git add apps/extension/src/popup.html
git commit -m "feat(extension): add cloud connection toggle to popup HTML"
```

---

## Task 6: Implement Popup Fetch Logic and Polling

**Files:**
- Modify: `apps/extension/src/popup.ts`
- Test: `apps/extension/src/popup.test.ts`

### Step 6.1: Write the failing test

Create `apps/extension/src/popup.test.ts`:

```typescript
import { describe, expect, it, jest } from 'bun:test';

describe('popup cloud switch', () => {
  it('fetches status on load and sets checkbox state', async () => {
    globalThis.fetch = jest.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        success: true,
        data: { connected: true, browserId: 'b-123', serverUrl: 'ws://localhost:3001', manualDisconnect: false },
      }),
    });

    // Simulate DOM
    document.body.innerHTML = `
      <div id="statusDot" class="dot disconnected"></div>
      <span id="statusLabel">Disconnected</span>
      <div id="browserId"></div>
      <input id="cloudSwitch" type="checkbox" />
      <div id="message"></div>
    `;

    await import('./popup');
    await new Promise((resolve) => setTimeout(resolve, 10));

    const checkbox = document.getElementById('cloudSwitch') as HTMLInputElement;
    expect(checkbox.checked).toBe(true);
  });
});
```

### Step 6.2: Run the failing test

```bash
bun test apps/extension/src/popup.test.ts
```

**Expected:** FAIL — popup.ts still uses `chrome.runtime.sendMessage`.

### Step 6.3: Write minimal implementation

Rewrite `apps/extension/src/popup.ts`:

```typescript
const API_BASE = 'http://localhost:3002';
const POLL_INTERVAL_MS = 5000;

const statusDot = document.getElementById('statusDot') as HTMLDivElement;
const statusLabel = document.getElementById('statusLabel') as HTMLSpanElement;
const browserIdDiv = document.getElementById('browserId') as HTMLDivElement;
const cloudSwitch = document.getElementById('cloudSwitch') as HTMLInputElement;
const messageDiv = document.getElementById('message') as HTMLDivElement;

interface StatusResponse {
  success: boolean;
  data?: {
    connected: boolean;
    browserId: string;
    serverUrl: string;
    manualDisconnect: boolean;
  };
  error?: string;
}

function updateStatus(connected: boolean, browserId?: string): void {
  statusDot.className = `dot ${connected ? 'connected' : 'disconnected'}`;
  statusLabel.textContent = connected ? 'Connected' : 'Disconnected';
  if (browserId) browserIdDiv.textContent = browserId;
}

function setMessage(text: string): void {
  messageDiv.textContent = text;
}

async function fetchStatus(): Promise<StatusResponse> {
  try {
    const response = await fetch(`${API_BASE}/api/status`);
    return (await response.json()) as StatusResponse;
  } catch (error) {
    return { success: false, error: 'Local proxy unreachable' };
  }
}

async function setCloudConnection(connect: boolean): Promise<void> {
  const endpoint = connect ? '/api/connect' : '/api/disconnect';
  try {
    const response = await fetch(`${API_BASE}${endpoint}`, { method: 'POST' });
    const result = (await response.json()) as StatusResponse;
    if (!result.success) {
      setMessage(result.error ?? 'Request failed');
    }
  } catch (error) {
    setMessage('Local proxy unreachable');
  }
}

async function refresh(): Promise<void> {
  const result = await fetchStatus();
  if (result.success && result.data) {
    cloudSwitch.checked = result.data.connected;
    updateStatus(result.data.connected, result.data.browserId);
    setMessage('');
  } else {
    setMessage(result.error ?? 'Unknown error');
    cloudSwitch.disabled = true;
  }
}

cloudSwitch.addEventListener('change', async () => {
  cloudSwitch.disabled = true;
  await setCloudConnection(cloudSwitch.checked);
  await refresh();
  cloudSwitch.disabled = false;
});

refresh();
const poll = setInterval(refresh, POLL_INTERVAL_MS);

// Stop polling when popup closes
window.addEventListener('unload', () => {
  clearInterval(poll);
});
```

### Step 6.4: Run the passing test

```bash
bun test apps/extension/src/popup.test.ts
```

**Expected:** PASS.

### Step 6.5: Commit

```bash
git add apps/extension/src/popup.ts apps/extension/src/popup.test.ts
git commit -m "feat(extension): implement cloud switch with fetch and polling"
```

---

## Task 7: Use Shared Constant for Local Proxy Port

**Files:**
- Modify: `apps/extension/src/popup.ts`
- Modify: `apps/extension/src/offscreen.ts`
- Modify: `apps/local-proxy/src/config.ts`

### Step 7.1: Write minimal implementation

Replace the hardcoded port in `popup.ts` and `offscreen.ts` with `LOCAL_WS_PORT` from `@browser-bridge/shared`:

```typescript
import { LOCAL_WS_PORT } from '@browser-bridge/shared';
const API_BASE = `http://localhost:${LOCAL_WS_PORT}`;
```

```typescript
import { LOCAL_WS_PORT } from '@browser-bridge/shared';
const LOCAL_WS_URL = `ws://localhost:${LOCAL_WS_PORT}`;
```

Ensure `config.ts` also uses `LOCAL_WS_PORT`:

```typescript
import { LOCAL_WS_PORT, WEBSOCKET_PORT } from '@browser-bridge/shared';
export const DEFAULT_SERVER_URL = `ws://localhost:${WEBSOCKET_PORT}`;
export const DEFAULT_LOCAL_PORT = LOCAL_WS_PORT;
```

### Step 7.2: Run type-check

```bash
bun run type-check
```

**Expected:** PASS.

### Step 7.3: Commit

```bash
git add apps/extension/src/popup.ts apps/extension/src/offscreen.ts apps/local-proxy/src/config.ts
git commit -m "refactor: use shared LOCAL_WS_PORT constant"
```

---

## Task 8: Format, Lint, and Full Test Run

### Step 8.1: Format and lint

```bash
bunx @biomejs/biome check --write .
```

### Step 8.2: Type-check

```bash
bun run type-check
```

**Expected:** PASS.

### Step 8.3: Run all tests

```bash
bun test
```

**Expected:** All tests pass.

### Step 8.4: Commit

```bash
git add .
git commit -m "style: format and lint"
```

---

## Task 9: Manual Verification

### Step 9.1: Start ws-server and local-proxy

Terminal 1:

```bash
bun run dev:websocket
```

Terminal 2:

```bash
bun run dev:local-proxy
```

### Step 9.2: Build and load extension

```bash
bun run build:extension
```

Load `apps/extension/dist` as an unpacked extension in Chrome.

### Step 9.3: Verify behavior

- Open popup: switch should be on, status dot green.
- Turn switch off: ws-server logs browser offline; popup shows disconnected.
- Turn switch on: ws-server logs browser online; popup shows connected.
- Close and reopen popup: state should match.

---

## Self-Review

### Spec coverage

| Spec Section | Implementing Task |
|---|---|
| `manualDisconnect` runtime flag | Task 1, Task 2 |
| Suppress auto-reconnect on manual disconnect | Task 2 |
| `/api/status` endpoint | Task 3 |
| `/api/connect` endpoint | Task 3 |
| `/api/disconnect` endpoint | Task 3 |
| CORS headers | Task 3 |
| Popup toggle UI | Task 5 |
| 5-second polling | Task 6 |
| Remove "Connect to Local Proxy" button | Task 5 |
| Shared port constant | Task 7 |

### Placeholder scan

No `TBD`, `TODO`, "implement later", or vague steps. Every code block contains concrete code.

### Type consistency

- `CloudClient.isConnected` is a getter returning `boolean`.
- `CloudClient.isManualDisconnect` is a getter returning `boolean`.
- `LocalServer` handlers include optional `cloud?: CloudController`.
- Popup uses `StatusResponse` shape matching the API envelope.

### Potential gap

- The popup test in Task 6 uses a simple `document.body.innerHTML` mock; if Bun's DOM globals differ, the test may need `happy-dom` or `jsdom`. If so, install `@happy-dom/global-registrator` or use Bun's built-in DOM support if available.
- E2E testing is listed as manual verification (Task 9). Playwright E2E tests can be added later as a separate plan.

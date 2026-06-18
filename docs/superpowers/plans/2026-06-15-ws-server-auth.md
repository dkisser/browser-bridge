# WS-Server API Key Authentication Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Authenticate local-proxy connections to the WS-server via API key in the WebSocket handshake header, replacing the current message-body token approach.

**Architecture:** The WS-server validates the `Authorization: Bearer <key>` header during the WebSocket upgrade. Unauthenticated connections are closed immediately (code 4001). Non-localhost connections over plain `ws://` are rejected (code 4002). The `register` event no longer carries or validates a token — it only associates a `browserId` with the already-authenticated connection.

**Tech Stack:** Bun, TypeScript, `Bun.serve` WebSocket API, browser-style `WebSocket` constructor (Bun supports `new WebSocket(url, { headers })` for custom headers).

---

### Task 1: Add `validateHeader` to `ApiKeyAuthProvider`

**Files:**
- Modify: `packages/shared/src/auth.ts`
- Test: `packages/shared/src/__tests__/index.test.ts`

- [ ] **Step 1: Write the failing test**

Add to `packages/shared/src/__tests__/index.test.ts`, inside the existing `describe('ApiKeyAuthProvider')` block:

```typescript
it('validateHeader accepts valid Bearer token', async () => {
  const provider = new ApiKeyAuthProvider({ 'key-123': 'user-1' });
  const result = await provider.validateHeader('Bearer key-123');
  expect(result.valid).toBe(true);
  expect(result.userId).toBe('user-1');
});

it('validateHeader rejects invalid Bearer token', async () => {
  const provider = new ApiKeyAuthProvider({ 'key-123': 'user-1' });
  const result = await provider.validateHeader('Bearer wrong');
  expect(result.valid).toBe(false);
});

it('validateHeader rejects malformed header', async () => {
  const provider = new ApiKeyAuthProvider({ 'key-123': 'user-1' });
  const result = await provider.validateHeader('key-123');
  expect(result.valid).toBe(false);
});

it('validateHeader rejects empty string', async () => {
  const provider = new ApiKeyAuthProvider({ 'key-123': 'user-1' });
  const result = await provider.validateHeader('');
  expect(result.valid).toBe(false);
});
```

Also add a test for the `string[]` constructor:

```typescript
it('accepts string array of keys', async () => {
  const provider = new ApiKeyAuthProvider(['key-a', 'key-b']);
  const resultA = await provider.validateToken('key-a');
  expect(resultA.valid).toBe(true);
  const resultB = await provider.validateToken('key-b');
  expect(resultB.valid).toBe(true);
  const resultC = await provider.validateToken('key-c');
  expect(resultC.valid).toBe(false);
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `bun test packages/shared/src/__tests__/index.test.ts`
Expected: FAIL — `validateHeader` does not exist on `ApiKeyAuthProvider`

- [ ] **Step 3: Implement `validateHeader`**

In `packages/shared/src/auth.ts`, add the `validateHeader` method to both `AuthProvider` interface and `ApiKeyAuthProvider` class:

Update the `AuthProvider` interface:

```typescript
export interface AuthProvider {
  id: string;
  validateToken(token: string): Promise<AuthResult>;
  validateHeader(authorizationHeader: string): Promise<AuthResult>;
  refreshToken(token: AuthToken): Promise<AuthToken>;
}
```

Add to `NoopAuthProvider`:

```typescript
async validateHeader(_header: string): Promise<AuthResult> {
  return { valid: true, userId: 'local', permissions: ['*'] };
}
```

Add to `ApiKeyAuthProvider` — update the constructor to also accept `string[]`, and add the `validateHeader` method:

```typescript
export class ApiKeyAuthProvider implements AuthProvider {
  id = 'api-key';
  private validKeys: Map<string, string>;

  constructor(keys: Record<string, string> | string[]) {
    if (Array.isArray(keys)) {
      this.validKeys = new Map(keys.map((k, i) => [k, `user-${i}`]));
    } else {
      this.validKeys = new Map(Object.entries(keys));
    }
  }

  async validateToken(token: string): Promise<AuthResult> {
    const userId = this.validKeys.get(token);
    if (!userId) {
      return { valid: false, userId: '', permissions: [] };
    }
    return { valid: true, userId, permissions: ['*'] };
  }

  async validateHeader(authorizationHeader: string): Promise<AuthResult> {
    if (!authorizationHeader.startsWith('Bearer ')) {
      return { valid: false, userId: '', permissions: [] };
    }
    const token = authorizationHeader.slice(7);
    return this.validateToken(token);
  }

  async refreshToken(token: AuthToken): Promise<AuthToken> {
    return token;
  }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `bun test packages/shared/src/__tests__/index.test.ts`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add packages/shared/src/auth.ts packages/shared/src/__tests__/index.test.ts
git commit -m "feat(shared): add validateHeader method to AuthProvider for Bearer token parsing"
```

---

### Task 2: Add `isLocalhost` utility to shared

**Files:**
- Modify: `packages/shared/src/index.ts`
- Create: `packages/shared/src/utils.ts`
- Test: `packages/shared/src/__tests__/index.test.ts`

- [ ] **Step 1: Write the failing test**

Add a new `describe` block to `packages/shared/src/__tests__/index.test.ts`:

```typescript
import { isLocalhost } from '../utils';

describe('isLocalhost', () => {
  it('recognizes localhost', () => {
    expect(isLocalhost('localhost')).toBe(true);
  });

  it('recognizes 127.0.0.1', () => {
    expect(isLocalhost('127.0.0.1')).toBe(true);
  });

  it('recognizes [::1]', () => {
    expect(isLocalhost('[::1]')).toBe(true);
  });

  it('recognizes ::1 without brackets', () => {
    expect(isLocalhost('::1')).toBe(true);
  });

  it('rejects remote host', () => {
    expect(isLocalhost('example.com')).toBe(false);
  });

  it('rejects IP that is not loopback', () => {
    expect(isLocalhost('192.168.1.1')).toBe(false);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `bun test packages/shared/src/__tests__/index.test.ts`
Expected: FAIL — `isLocalhost` module not found

- [ ] **Step 3: Implement `isLocalhost`**

Create `packages/shared/src/utils.ts`:

```typescript
export function isLocalhost(host: string): boolean {
  return host === 'localhost' || host === '127.0.0.1' || host === '::1' || host === '[::1]';
}
```

Add export to `packages/shared/src/index.ts`:

```typescript
export { isLocalhost } from './utils';
```

- [ ] **Step 4: Run test to verify it passes**

Run: `bun test packages/shared/src/__tests__/index.test.ts`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add packages/shared/src/utils.ts packages/shared/src/index.ts packages/shared/src/__tests__/index.test.ts
git commit -m "feat(shared): add isLocalhost utility for TLS enforcement checks"
```

---

### Task 3: Server-side handshake auth and TLS enforcement

**Files:**
- Modify: `apps/websocket/src/server/index.ts`
- Test: `apps/websocket/src/server/__tests__/server.test.ts`

- [ ] **Step 1: Write the failing test**

Add to `apps/websocket/src/server/__tests__/server.test.ts`:

```typescript
import { ApiKeyAuthProvider } from '@browser-bridge/shared/auth';

describe('WS server handshake auth', () => {
  const AUTH_PORT = 3097;
  const VALID_KEY = 'server-test-key';
  let server: Server;

  beforeAll(() => {
    server = startServer(AUTH_PORT, new ApiKeyAuthProvider({ [VALID_KEY]: 'user-1' }));
  });

  afterAll(() => {
    server.stop();
  });

  it('closes connection without Authorization header', async () => {
    const ws = new WebSocket(`ws://localhost:${AUTH_PORT}`);

    const closeEvent = await new Promise<CloseEvent>((resolve) => {
      ws.addEventListener('close', (e) => resolve(e));
    });

    expect(closeEvent.code).toBe(4001);
    expect(closeEvent.reason).toBe('unauthorized');
  });

  it('closes connection with wrong API key', async () => {
    const ws = new WebSocket(`ws://localhost:${AUTH_PORT}`, {
      headers: { Authorization: 'Bearer wrong-key' },
    });

    const closeEvent = await new Promise<CloseEvent>((resolve) => {
      ws.addEventListener('close', (e) => resolve(e));
    });

    expect(closeEvent.code).toBe(4001);
    expect(closeEvent.reason).toBe('unauthorized');
  });

  it('accepts connection with valid API key', async () => {
    const ws = new WebSocket(`ws://localhost:${AUTH_PORT}`, {
      headers: { Authorization: `Bearer ${VALID_KEY}` },
    });

    const message = await new Promise<string>((resolve) => {
      ws.addEventListener('message', (e) => {
        resolve(e.data as string);
        ws.close();
      });
    });

    const envelope = JSON.parse(message);
    expect(envelope.type).toBe('event');
    expect(envelope.payload).toEqual({ event: 'welcome' });
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `bun test apps/websocket/src/server/__tests__/server.test.ts`
Expected: FAIL — connections are accepted without auth (current NoopAuthProvider behavior)

- [ ] **Step 3: Implement handshake auth in server**

In `apps/websocket/src/server/index.ts`, update the `fetch` handler and `open` handler:

Change the `fetch` handler to read the `Authorization` header and store auth result on the upgrade data. Change `open` to check auth and enforce TLS:

```typescript
import { WEBSOCKET_PORT } from '@browser-bridge/shared';
import { isLocalhost } from '@browser-bridge/shared/utils';
import { NoopAuthProvider } from '@browser-bridge/shared/auth';
import { encode, decode } from '../protocol';
import { ConnectionRegistry } from './registry';
import type { AuthProvider, Envelope } from '@browser-bridge/shared/types';
import type { ServerWebSocket } from 'bun';

interface WsData {
  connectionId: string;
  browserId?: string;
  userId?: string;
  authenticated?: boolean;
}

export function startServer(
  port = WEBSOCKET_PORT,
  authProvider: AuthProvider = new NoopAuthProvider(),
) {
  const registry = new ConnectionRegistry(authProvider);
  const cliConnections = new Set<ServerWebSocket<WsData>>();

  const server = Bun.serve<WsData>({
    port,
    async fetch(req, server) {
      const url = new URL(req.url);
      const host = url.hostname;

      // TLS enforcement: reject non-localhost ws:// connections
      if (url.protocol === 'ws:' && !isLocalhost(host)) {
        return new Response('TLS required', { status: 426 });
      }

      const authHeader = req.headers.get('Authorization') ?? '';
      const authResult = await authProvider.validateHeader(authHeader);

      if (server.upgrade(req, {
        data: {
          connectionId: crypto.randomUUID(),
          authenticated: authResult.valid,
          userId: authResult.userId,
        },
      })) {
        return;
      }
      return new Response('Browser Bridge WebSocket server', { status: 200 });
    },
    websocket: {
      open(ws) {
        if (!ws.data.authenticated) {
          ws.close(4001, 'unauthorized');
          return;
        }

        console.log(`Client connected: ${ws.data.connectionId} (user: ${ws.data.userId})`);
        cliConnections.add(ws);
        ws.send(encode('event', { event: 'welcome' }));
      },

      async message(ws, message) {
        const text =
          typeof message === 'string'
            ? message
            : new TextDecoder().decode(message);

        let envelope: Envelope;
        try {
          envelope = decode(text);
        } catch {
          ws.send(encode('response', { status: 'error', error: 'invalid_json' }, { id: '' }));
          return;
        }

        switch (envelope.type) {
          case 'event': {
            const event = envelope.payload as Record<string, unknown>;

            if (event.event === 'register') {
              const browserId = event.browserId as string;
              const result = await registry.register(ws, browserId);
              if (result.success) {
                ws.send(encode('response', { status: 'ok' }, { id: envelope.id, browserId }));
              } else {
                ws.send(encode('response', { status: 'error', error: result.error }, { id: envelope.id }));
              }
            }

            if (event.event === 'online') {
              const browserId = (event.browserId as string) || ws.data.browserId;
              registry.setStatus(browserId, 'online');
              ws.send(encode('response', { status: 'ok' }, { id: envelope.id, browserId }));
            }

            if (event.event === 'offline') {
              const browserId = (event.browserId as string) || ws.data.browserId;
              registry.setStatus(browserId, 'offline');
              ws.send(encode('response', { status: 'ok' }, { id: envelope.id, browserId }));
            }
            break;
          }

          case 'command': {
            const browserId = envelope.browserId;
            const status = registry.getStatus(browserId);

            if (!status || status === 'offline') {
              ws.send(
                encode(
                  'response',
                  { status: 'error', error: 'browser_offline', message: `Browser ${browserId} is offline` },
                  { id: envelope.id, browserId },
                ),
              );
              break;
            }

            const targetWs = registry.getWebSocket(browserId);
            if (!targetWs) {
              ws.send(
                encode(
                  'response',
                  { status: 'error', error: 'browser_not_found' },
                  { id: envelope.id, browserId },
                ),
              );
              break;
            }

            targetWs.send(text);
            break;
          }

          case 'response': {
            for (const cliWs of cliConnections) {
              if (cliWs !== ws && cliWs.readyState === 1) {
                cliWs.send(text);
              }
            }
            break;
          }
        }
      },

      close(ws) {
        cliConnections.delete(ws);
        const browserId = registry.removeByWebSocket(ws);
        if (browserId) {
          console.log(`Browser disconnected: ${browserId}`);
        }
        console.log(`Client disconnected: ${ws.data.connectionId}`);
      },
    },
  });

  console.log(`WebSocket server running on ws://localhost:${server.port}`);
  return server;
}
```

- [ ] **Step 4: Update `ConnectionRegistry.register` to remove token parameter**

In `apps/websocket/src/server/registry.ts`, update `register` to no longer take a `token` parameter. The connection is already authenticated at the handshake, and `userId` comes from `ws.data.userId`:

```typescript
import type { BrowserStatus, BrowserConnection, AuthProvider } from '@browser-bridge/shared';
import type { ServerWebSocket } from 'bun';

interface RegistryEntry {
  browserId: string;
  userId: string;
  ws: ServerWebSocket;
  status: BrowserStatus;
  lastSeen: number;
}

export class ConnectionRegistry {
  private browsers = new Map<string, RegistryEntry>();
  private authProvider: AuthProvider;

  constructor(authProvider: AuthProvider) {
    this.authProvider = authProvider;
  }

  async register(
    ws: ServerWebSocket,
    browserId: string,
  ): Promise<{ success: boolean; error?: string }> {
    this.browsers.set(browserId, {
      browserId,
      userId: (ws.data as any).userId || 'unknown',
      ws,
      status: 'offline',
      lastSeen: Date.now(),
    });

    ws.data = { ...ws.data, browserId, userId: (ws.data as any).userId || 'unknown' };
    return { success: true };
  }

  setStatus(browserId: string, status: BrowserStatus): boolean {
    const entry = this.browsers.get(browserId);
    if (!entry) return false;
    entry.status = status;
    entry.lastSeen = Date.now();
    return true;
  }

  getStatus(browserId: string): BrowserStatus | undefined {
    return this.browsers.get(browserId)?.status;
  }

  getWebSocket(browserId: string): ServerWebSocket | undefined {
    return this.browsers.get(browserId)?.ws;
  }

  removeByWebSocket(ws: ServerWebSocket): string | undefined {
    for (const [browserId, entry] of this.browsers) {
      if (entry.ws === ws) {
        this.browsers.delete(browserId);
        return browserId;
      }
    }
    return undefined;
  }

  listBrowsers(): BrowserConnection[] {
    return Array.from(this.browsers.values()).map(({ ws: _, ...rest }) => rest);
  }
}
```

- [ ] **Step 5: Update existing server tests**

The existing `ConnectionRegistry` test in `apps/websocket/src/server/__tests__/server.test.ts` calls `registry.register(mockWs, 'b-1', 'any-token')` — remove the third argument:

```typescript
describe('ConnectionRegistry', () => {
  it('registers a browser and tracks status', async () => {
    const registry = new ConnectionRegistry(new NoopAuthProvider());
    const mockWs = { data: {} } as any;

    const result = await registry.register(mockWs, 'b-1');
    expect(result.success).toBe(true);
    expect(registry.getStatus('b-1')).toBe('offline');

    registry.setStatus('b-1', 'online');
    expect(registry.getStatus('b-1')).toBe('online');
  });

  it('removes token validation (always succeeds after handshake auth)', async () => {
    const registry = new ConnectionRegistry(new ApiKeyAuthProvider({ 'good-key': 'user-1' }));
    const mockWs = { data: { userId: 'user-1' } } as any;

    // register no longer validates tokens — auth happened at handshake
    const result = await registry.register(mockWs, 'b-2');
    expect(result.success).toBe(true);
  });
});
```

Also update the `WS server routing` test — since `NoopAuthProvider` is used, connections without auth headers are accepted (NoopAuthProvider always returns valid=true). But to be explicit, update the `beforeAll`:

The existing `WS server routing` test block still uses `startServer(3098)` which defaults to `NoopAuthProvider`. This is fine — `NoopAuthProvider.validateHeader` always returns valid, so unauthenticated connections are accepted. No changes needed for that block.

- [ ] **Step 6: Run all server tests**

Run: `bun test apps/websocket/src/server/__tests__/server.test.ts`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add apps/websocket/src/server/index.ts apps/websocket/src/server/registry.ts apps/websocket/src/server/__tests__/server.test.ts
git commit -m "feat(server): add handshake auth and TLS enforcement to WS server"
```

---

### Task 4: Client-side — send API key in WebSocket handshake header

**Files:**
- Modify: `apps/websocket/src/client/index.ts`
- Modify: `apps/local-proxy/src/cloud-client.ts`
- Modify: `apps/local-proxy/src/index.ts`
- Test: `apps/websocket/src/client/__tests__/client.test.ts`

- [ ] **Step 1: Write the failing test**

Update `apps/websocket/src/client/__tests__/client.test.ts` — the existing tests use `startServer(3099)` with default `NoopAuthProvider`, so they should still pass without changes. Add a test that verifies a client with an API key can connect to a server that requires one:

```typescript
import { ApiKeyAuthProvider } from '@browser-bridge/shared/auth';

describe('WS client with API key auth', () => {
  const AUTH_PORT = 3102;
  const VALID_KEY = 'client-test-key';
  let authServer: Server;

  beforeAll(() => {
    authServer = startServer(AUTH_PORT, new ApiKeyAuthProvider({ [VALID_KEY]: 'user-1' }));
  });

  afterAll(() => {
    authServer.stop();
  });

  it('connects successfully with valid API key', async () => {
    const client = createClient({
      url: `ws://localhost:${AUTH_PORT}`,
      headers: { Authorization: `Bearer ${VALID_KEY}` },
    });

    const message = await new Promise<string>((resolve) => {
      client.on('message', (data: string) => {
        resolve(data);
      });
    });

    const envelope = JSON.parse(message);
    expect(envelope.type).toBe('event');
    expect(envelope.payload).toEqual({ event: 'welcome' });

    client.close();
  });

  it('fails to connect without API key', async () => {
    const client = createClient({ url: `ws://localhost:${AUTH_PORT}` });

    const closeEvent = await new Promise<number>((resolve) => {
      const ws = (client as any).ws;
      ws.addEventListener('close', (e: CloseEvent) => resolve(e.code));
    });

    expect(closeEvent).toBe(4001);
  });
});
```

Note: The test structure may need adjustment depending on how `createClient` exposes the underlying socket. See Step 3 for the updated `createClient` signature.

- [ ] **Step 2: Run test to verify it fails**

Run: `bun test apps/websocket/src/client/__tests__/client.test.ts`
Expected: FAIL — `createClient` does not accept `headers` option

- [ ] **Step 3: Update `createClient` to accept headers**

In `apps/websocket/src/client/index.ts`, add `headers` to `ClientOptions` and pass it to the `WebSocket` constructor:

```typescript
export interface ClientOptions {
  url?: string;
  headers?: Record<string, string>;
  onMessage?: (envelope: Envelope) => void;
  onError?: (error: Event) => void;
  onClose?: () => void;
}

export function createClient(options: ClientOptions = {}) {
  const {
    url = `ws://localhost:${WEBSOCKET_PORT}`,
    headers,
    onMessage,
    onError,
    onClose,
  } = options;

  const socket = headers
    ? new WebSocket(url, { headers } as any)
    : new WebSocket(url);
  const pending = new Map<string, PendingRequest>();

  socket.addEventListener('open', () => {
    console.log('Connected to server');
  });

  socket.addEventListener('message', (event) => {
    try {
      const envelope = decode(event.data as string);
      if (envelope.type === 'response' && pending.has(envelope.id)) {
        const req = pending.get(envelope.id)!;
        clearTimeout(req.timer);
        pending.delete(envelope.id);
        req.resolve(envelope);
      }
      onMessage?.(envelope);
    } catch {
      onMessage?.({ id: '', type: 'event', browserId: '', payload: event.data, timestamp: 0 });
    }
  });

  socket.addEventListener('error', (error) => {
    onError?.(error);
  });

  socket.addEventListener('close', () => {
    for (const [id, req] of pending) {
      clearTimeout(req.timer);
      req.reject(new Error('connection closed'));
      pending.delete(id);
    }
    onClose?.();
  });

  return {
    send(type: Envelope['type'], payload: unknown, opts?: { id?: string; browserId?: string }) {
      socket.send(encode(type, payload, opts));
    },

    sendCommand(
      browserId: string,
      payload: CommandPayload,
      opts: { timeout?: number } = {},
    ): Promise<Envelope> {
      const timeout = opts.timeout ?? 10000;
      const id = crypto.randomUUID();
      return new Promise((resolve, reject) => {
        const timer = setTimeout(() => {
          pending.delete(id);
          reject(new Error(`timeout: no response for command ${payload.command} within ${timeout}ms`));
        }, timeout);

        pending.set(id, { resolve, reject, timer });
        socket.send(encode('command', payload, { id, browserId }));
      });
    },

    sendRaw(text: string) {
      socket.send(text);
    },

    close() {
      socket.close();
    },

    get readyState() {
      return socket.readyState;
    },
  };
}
```

- [ ] **Step 4: Update `CloudClient` to pass API key as header**

In `apps/local-proxy/src/cloud-client.ts`, pass the `Authorization` header when creating the client, and remove the `token` from the `register` message:

```typescript
connect(): Promise<void> {
  return new Promise((resolve, reject) => {
    this.client = createClient({
      url: this.serverUrl,
      headers: { Authorization: `Bearer ${this.apiToken}` },
      onMessage: (envelope) => this.handleMessage(envelope),
      onError: (error) => {
        console.error('[cloud] connection error:', error);
        reject(error);
      },
      onClose: () => {
        console.log('[cloud] disconnected');
        this.client = null;
        this.scheduleReconnect();
      },
    });

    const check = setInterval(() => {
      if (this.client && this.client.readyState === WebSocket.OPEN) {
        clearInterval(check);
        this.register();
        this.reconnectAttempts = 0;
        resolve();
      }
    }, 50);

    setTimeout(() => {
      clearInterval(check);
      reject(new Error('Connection timeout'));
    }, 10000);
  });
}

private register(): void {
  if (!this.client) return;
  this.client.send('event', {
    event: 'register',
    browserId: this.browserId,
  });
}
```

- [ ] **Step 5: Update `local-proxy/src/index.ts` to remove `'dev-token'` fallback**

```typescript
#!/usr/bin/env bun
import { DEFAULT_SERVER_URL, DEFAULT_LOCAL_PORT } from './config';
import { StateManager } from './state';
import { CloudClient } from './cloud-client';
import { LocalServer } from './local-server';
import { Router } from './router';

function isLocalhostUrl(url: string): boolean {
  try {
    const parsed = new URL(url);
    return parsed.hostname === 'localhost' || parsed.hostname === '127.0.0.1' || parsed.hostname === '::1';
  } catch {
    return false;
  }
}

async function main() {
  const serverUrl = process.env.BRIDGE_SERVER_URL || DEFAULT_SERVER_URL;
  const localPort = Number(process.env.BRIDGE_LOCAL_PORT) || DEFAULT_LOCAL_PORT;
  const apiToken = process.env.BRIDGE_API_TOKEN;

  if (!apiToken && !isLocalhostUrl(serverUrl)) {
    console.error('BRIDGE_API_TOKEN is required when connecting to a remote server');
    process.exit(1);
  }

  if (!apiToken && isLocalhostUrl(serverUrl)) {
    console.warn('Warning: BRIDGE_API_TOKEN not set — running without authentication (local development only)');
  }

  const state = new StateManager();
  console.log(`Browser ID: ${state.browserId}`);
  console.log(`Cloud server: ${serverUrl}`);

  let router: Router;

  const local = new LocalServer(localPort, {
    onCommand: (envelope) => {
      if (envelope.type === 'response') {
        router.handleExtensionResponse(envelope);
      }
    },
    onConnect: () => router.handleExtensionConnect(),
    onDisconnect: () => router.handleExtensionDisconnect(),
  });

  const cloud = new CloudClient({
    serverUrl,
    apiToken: apiToken || '',
    browserId: state.browserId,
    onCommand: (envelope) => router.handleCloudCommand(envelope),
  });

  router = new Router(state, cloud, local);

  local.start();

  try {
    await cloud.connect();
    console.log('Connected to cloud server');
  } catch (err) {
    console.error('Failed to connect to cloud server:', err);
    console.log('Will retry automatically...');
  }
}

main();
```

- [ ] **Step 6: Run client tests**

Run: `bun test apps/websocket/src/client/__tests__/client.test.ts`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add apps/websocket/src/client/index.ts apps/local-proxy/src/cloud-client.ts apps/local-proxy/src/index.ts apps/websocket/src/client/__tests__/client.test.ts
git commit -m "feat(client,proxy): send API key in WebSocket handshake header"
```

---

### Task 5: WS-Server entry point — read `BRIDGE_API_KEYS` env var

**Files:**
- Modify: `apps/websocket/src/index.ts`

- [ ] **Step 1: Update the server entry point to construct `ApiKeyAuthProvider` from env var**

```typescript
import { startServer } from './server';
import { ApiKeyAuthProvider } from '@browser-bridge/shared/auth';

const apiKeys = process.env.BRIDGE_API_KEYS;

if (apiKeys) {
  const keys = apiKeys.split(',').map(k => k.trim()).filter(Boolean);
  startServer(undefined, new ApiKeyAuthProvider(keys));
} else {
  startServer();
}
```

When `BRIDGE_API_KEYS` is not set, the server uses `NoopAuthProvider` (the default) — acceptable for local development.

- [ ] **Step 2: Verify server starts**

Run: `timeout 3 bun apps/websocket/src/index.ts || true`
Expected: Prints "WebSocket server running on ws://localhost:3001"

- [ ] **Step 3: Verify server starts with API keys**

Run: `BRIDGE_API_KEYS=test-key timeout 3 bun apps/websocket/src/index.ts || true`
Expected: Prints "WebSocket server running on ws://localhost:3001" (no crash)

- [ ] **Step 4: Commit**

```bash
git add apps/websocket/src/index.ts
git commit -m "feat(server): read BRIDGE_API_KEYS env var for auth provider configuration"
```

---

### Task 7: Update integration tests

**Files:**
- Modify: `tests/integration.test.ts`

- [ ] **Step 1: Update integration tests to use handshake auth**

The integration tests currently send the token in the `register` message body. Update them to use the `Authorization` header instead, and remove the `token` field from `register` payloads:

In `tests/integration.test.ts`, update all WebSocket connections to the server to include the `Authorization` header when acting as a proxy. CLI connections don't need auth (local-only, spec says no auth needed).

```typescript
import { describe, expect, it, beforeAll, afterAll } from 'bun:test';
import { startServer } from '../apps/websocket/src/server';
import { ApiKeyAuthProvider } from '../packages/shared/src/auth';
import type { Server } from 'bun';

const TEST_PORT = 3080;
const TEST_API_KEY = 'test-key-123';
const TEST_USER_ID = 'test-user';

describe('Integration: CLI → Server → Local Proxy', () => {
  let server: Server;

  beforeAll(() => {
    server = startServer(TEST_PORT, new ApiKeyAuthProvider({ [TEST_API_KEY]: TEST_USER_ID }));
  });

  afterAll(() => {
    server.stop();
  });

  it('rejects command to unregistered browser', async () => {
    const cli = new WebSocket(`ws://localhost:${TEST_PORT}`);

    await new Promise<void>((resolve) => {
      cli.addEventListener('open', () => resolve());
    });

    const response = await new Promise<string>((resolve) => {
      cli.addEventListener('message', (e) => {
        const data = JSON.parse(e.data as string);
        if (data.type === 'response' && data.id === 'test-1') {
          resolve(e.data as string);
        }
      });
      cli.send(JSON.stringify({
        id: 'test-1',
        type: 'command',
        browserId: 'b-nonexistent',
        payload: { command: 'navigate', params: { url: 'https://example.com' } },
        timestamp: Date.now(),
      }));
    });

    const parsed = JSON.parse(response);
    expect(parsed.payload.status).toBe('error');
    expect(parsed.payload.error).toBe('browser_offline');

    cli.close();
  });

  it('registers a Local Proxy and routes command to it', async () => {
    // 1. Connect a mock Local Proxy WITH auth header
    const proxy = new WebSocket(`ws://localhost:${TEST_PORT}`, {
      headers: { Authorization: `Bearer ${TEST_API_KEY}` },
    });
    await new Promise<void>((resolve) => {
      proxy.addEventListener('open', () => resolve());
    });

    // Consume welcome message
    await new Promise<void>((resolve) => {
      proxy.addEventListener('message', function handler() {
        proxy.removeEventListener('message', handler);
        resolve();
      });
    });

    // 2. Register the proxy (no token in message body)
    const registerResponse = await new Promise<string>((resolve) => {
      const handler = (e: MessageEvent) => {
        const data = JSON.parse(e.data as string);
        if (data.id === 'reg-1') {
          proxy.removeEventListener('message', handler);
          resolve(e.data as string);
        }
      };
      proxy.addEventListener('message', handler);
      proxy.send(JSON.stringify({
        id: 'reg-1',
        type: 'event',
        browserId: 'b-integration',
        payload: { event: 'register', browserId: 'b-integration' },
        timestamp: Date.now(),
      }));
    });

    const regParsed = JSON.parse(registerResponse);
    expect(regParsed.payload.status).toBe('ok');

    // 3. Report online
    const onlineResponse = await new Promise<string>((resolve) => {
      const handler = (e: MessageEvent) => {
        const data = JSON.parse(e.data as string);
        if (data.id === 'online-1') {
          proxy.removeEventListener('message', handler);
          resolve(e.data as string);
        }
      };
      proxy.addEventListener('message', handler);
      proxy.send(JSON.stringify({
        id: 'online-1',
        type: 'event',
        browserId: 'b-integration',
        payload: { event: 'online', browserId: 'b-integration' },
        timestamp: Date.now(),
      }));
    });

    expect(JSON.parse(onlineResponse).payload.status).toBe('ok');

    // 4. Connect CLI (no auth needed — local only) and send command
    const cli = new WebSocket(`ws://localhost:${TEST_PORT}`);
    await new Promise<void>((resolve) => {
      cli.addEventListener('open', () => resolve());
    });

    // Consume welcome
    await new Promise<void>((resolve) => {
      cli.addEventListener('message', function handler() {
        cli.removeEventListener('message', handler);
        resolve();
      });
    });

    // Send command
    const cmdId = 'cmd-int-1';
    cli.send(JSON.stringify({
      id: cmdId,
      type: 'command',
      browserId: 'b-integration',
      payload: { command: 'navigate', params: { url: 'https://example.com' } },
      timestamp: Date.now(),
    }));

    // 5. Verify command arrives at proxy
    const proxyMessage = await new Promise<string>((resolve) => {
      const handler = (e: MessageEvent) => {
        const data = JSON.parse(e.data as string);
        if (data.type === 'command' && data.id === cmdId) {
          proxy.removeEventListener('message', handler);
          resolve(e.data as string);
        }
      };
      proxy.addEventListener('message', handler);
    });

    const cmdParsed = JSON.parse(proxyMessage);
    expect(cmdParsed.type).toBe('command');
    expect(cmdParsed.payload.command).toBe('navigate');
    expect(cmdParsed.payload.params.url).toBe('https://example.com');

    // 6. Proxy sends response back
    proxy.send(JSON.stringify({
      id: cmdId,
      type: 'response',
      browserId: 'b-integration',
      payload: { status: 'ok', data: { url: 'https://example.com', title: 'Example Domain' } },
      timestamp: Date.now(),
    }));

    // 7. Verify CLI receives response
    const cliResponse = await new Promise<string>((resolve) => {
      const handler = (e: MessageEvent) => {
        const data = JSON.parse(e.data as string);
        if (data.type === 'response' && data.id === cmdId) {
          cli.removeEventListener('message', handler);
          resolve(e.data as string);
        }
      };
      cli.addEventListener('message', handler);
    });

    const respParsed = JSON.parse(cliResponse);
    expect(respParsed.payload.status).toBe('ok');
    expect(respParsed.payload.data.url).toBe('https://example.com');
    expect(respParsed.payload.data.title).toBe('Example Domain');

    cli.close();
    proxy.close();
  });

  it('rejects connection without valid API key', async () => {
    const ws = new WebSocket(`ws://localhost:${TEST_PORT}`);

    const closeEvent = await new Promise<CloseEvent>((resolve) => {
      ws.addEventListener('close', (e) => resolve(e));
    });

    expect(closeEvent.code).toBe(4001);
    expect(closeEvent.reason).toBe('unauthorized');
  });

  it('rejects connection with wrong API key', async () => {
    const ws = new WebSocket(`ws://localhost:${TEST_PORT}`, {
      headers: { Authorization: 'Bearer wrong-key' },
    });

    const closeEvent = await new Promise<CloseEvent>((resolve) => {
      ws.addEventListener('close', (e) => resolve(e));
    });

    expect(closeEvent.code).toBe(4001);
    expect(closeEvent.reason).toBe('unauthorized');
  });
});
```

- [ ] **Step 2: Run integration test**

Run: `bun test tests/integration.test.ts`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add tests/integration.test.ts
git commit -m "test: update integration tests for handshake-based auth"
```

---

### Task 8: Run full test suite and verify

**Files:** None (verification only)

- [ ] **Step 1: Run all tests**

Run: `bun test`
Expected: All tests pass

- [ ] **Step 2: Final commit if any fixes were needed**

```bash
git add -A
git commit -m "fix: resolve test failures from auth migration"
```

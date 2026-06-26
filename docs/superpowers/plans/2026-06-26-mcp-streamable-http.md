# Browser Bridge MCP Server Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a FastMCP-based Streamable HTTP MCP server to the existing `apps/websocket` package so MCP-compatible agents can list and control connected browsers.

**Architecture:** FastMCP runs in the same Bun process as the WebSocket server on a dedicated port (default 3003). Each MCP tool creates a short-lived `@browser-bridge/websocket/client` connection to the WebSocket server, sends an `Envelope` command, and returns the `ResponsePayload` mapped to MCP content.

**Tech Stack:** Bun, TypeScript, `fastmcp`, `zod`, `@browser-bridge/shared`, `@browser-bridge/websocket/client`.

---

## File Structure

| File | Responsibility |
|------|----------------|
| `apps/websocket/package.json` | Add `fastmcp` and `zod` dependencies |
| `apps/websocket/src/index.ts` | Bootstrap FastMCP alongside Bun.serve |
| `apps/websocket/src/mcp/browser-session.ts` | Per-MCP-session state: immutable session snapshots, mutable store |
| `apps/websocket/src/mcp/tool-context.ts` | `ToolContext` and `ServerContext` types |
| `apps/websocket/src/mcp/browser-resolver.ts` | Resolve target browserId: explicit → auto-detect single → error |
| `apps/websocket/src/mcp/command-client.ts` | Short-lived WebSocket client wrappers: `sendCommand`, `sendEvent` |
| `apps/websocket/src/mcp/browser-lookup.ts` | Fetch browser list from ws-server and resolve target browser |
| `apps/websocket/src/mcp/tools/list-browsers.ts` | `list_browsers` tool |
| `apps/websocket/src/mcp/tools/set-browser.ts` | `set_browser` tool |
| `apps/websocket/src/mcp/tools/navigate.ts` | `navigate` tool |
| `apps/websocket/src/mcp/tools/click.ts` | `click` tool |
| `apps/websocket/src/mcp/tools/type.ts` | `type` tool |
| `apps/websocket/src/mcp/tools/screenshot.ts` | `screenshot` tool |
| `apps/websocket/src/mcp/tools/pageinfo.ts` | `pageinfo` tool |
| `apps/websocket/src/mcp/server.ts` | FastMCP instance, registers all tools, starts `httpStream` transport |
| `apps/websocket/src/mcp/index.ts` | Public MCP exports |
| `apps/websocket/src/mcp/__tests__/browser-session.test.ts` | Unit tests for session state |
| `apps/websocket/src/mcp/__tests__/browser-resolver.test.ts` | Unit tests for browser resolution |
| `apps/websocket/src/mcp/__tests__/command-client.test.ts` | Unit tests for command/event client |
| `apps/websocket/src/mcp/__tests__/browser-lookup.test.ts` | Unit tests for browser lookup helper |
| `apps/websocket/src/mcp/__tests__/tools/*.test.ts` | Tests for each tool |
| `apps/websocket/src/mcp/__tests__/server.test.ts` | Integration test for FastMCP server |
| `docs/mcp-setup.md` | User-facing MCP setup guide |
| `README.md` | Add MCP section |

---

## Task 1: Add Dependencies

**Files:**
- Modify: `apps/websocket/package.json`

**Goal:** Add `fastmcp` and `zod` to `apps/websocket`.

- [ ] **Step 1: Add dependencies**

Update `apps/websocket/package.json`:

```json
{
  "name": "@browser-bridge/websocket",
  "version": "1.0.0",
  "private": true,
  "description": "WebSocket server, client, and protocol for Browser Bridge",
  "exports": {
    "./client": "./src/client/index.ts",
    "./protocol": "./src/protocol/index.ts"
  },
  "scripts": {
    "dev": "bun --watch src/index.ts",
    "start": "bun src/index.ts",
    "type-check": "tsc --noEmit"
  },
  "dependencies": {
    "@browser-bridge/shared": "workspace:*",
    "fastmcp": "^3.35.0",
    "zod": "^3.25.0"
  },
  "devDependencies": {
    "@types/bun": "latest",
    "typescript": "^5.4.0"
  }
}
```

- [ ] **Step 2: Install packages**

Run:

```bash
bun install
```

Expected: `bun.lockb` updated, no errors.

- [ ] **Step 3: Commit**

```bash
git add apps/websocket/package.json bun.lockb
git commit -m "chore: add fastmcp and zod to websocket app"
```

---

## Task 2: Browser Session State

**Files:**
- Create: `apps/websocket/src/mcp/browser-session.ts`
- Create: `apps/websocket/src/mcp/__tests__/browser-session.test.ts`

**Goal:** Store selected browserId and timeout per MCP session. Session snapshots are immutable; the store owns mutable state.

- [ ] **Step 1: Write the failing test**

Create `apps/websocket/src/mcp/__tests__/browser-session.test.ts`:

```typescript
import { describe, expect, it } from 'bun:test';
import { createBrowserSessionStore } from '../browser-session';

describe('BrowserSessionStore', () => {
  it('creates a session with defaults', () => {
    const store = createBrowserSessionStore(15000);
    const session = store.getSession('session-1');
    expect(session.browserId).toBeUndefined();
    expect(session.defaultTimeoutMs).toBe(15000);
  });

  it('sets and retrieves browserId', () => {
    const store = createBrowserSessionStore(10000);
    const session = store.setBrowser('session-1', 'browser-a');
    expect(session.browserId).toBe('browser-a');
    expect(store.getSession('session-1').browserId).toBe('browser-a');
  });

  it('clears browserId', () => {
    const store = createBrowserSessionStore(10000);
    store.setBrowser('session-1', 'browser-a');
    const session = store.clearBrowser('session-1');
    expect(session.browserId).toBeUndefined();
    expect(store.getSession('session-1').browserId).toBeUndefined();
  });

  it('is isolated per session', () => {
    const store = createBrowserSessionStore(10000);
    store.setBrowser('a', 'browser-a');
    expect(store.getSession('b').browserId).toBeUndefined();
  });

  it('returns the same session object for repeated getSession calls', () => {
    const store = createBrowserSessionStore(10000);
    const a = store.getSession('session-1');
    const b = store.getSession('session-1');
    expect(a).toBe(b);
  });

  it('returns an updated immutable session when setBrowser is called', () => {
    const store = createBrowserSessionStore(10000);
    const original = store.getSession('session-1');
    const updated = store.setBrowser('session-1', 'browser-a');
    expect(updated).not.toBe(original);
    expect(original.browserId).toBeUndefined();
    expect(updated.browserId).toBe('browser-a');
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
bun test apps/websocket/src/mcp/__tests__/browser-session.test.ts
```

Expected: FAIL — module not found.

- [ ] **Step 3: Write minimal implementation**

Create `apps/websocket/src/mcp/browser-session.ts`:

```typescript
export interface BrowserSession {
  readonly defaultTimeoutMs: number;
  readonly browserId: string | undefined;
}

export interface BrowserSessionStore {
  getSession(sessionId: string): BrowserSession;
  setBrowser(sessionId: string, browserId: string): BrowserSession;
  clearBrowser(sessionId: string): BrowserSession;
}

export function createBrowserSessionStore(defaultTimeoutMs: number): BrowserSessionStore {
  const sessions = new Map<string, BrowserSession>();

  function ensureSession(sessionId: string): BrowserSession {
    let session = sessions.get(sessionId);
    if (!session) {
      session = { defaultTimeoutMs, browserId: undefined };
      sessions.set(sessionId, session);
    }
    return session;
  }

  return {
    getSession: ensureSession,
    setBrowser(sessionId: string, browserId: string): BrowserSession {
      const session = ensureSession(sessionId);
      const updated = { ...session, browserId };
      sessions.set(sessionId, updated);
      return updated;
    },
    clearBrowser(sessionId: string): BrowserSession {
      const session = ensureSession(sessionId);
      const updated = { ...session, browserId: undefined };
      sessions.set(sessionId, updated);
      return updated;
    },
  };
}
```

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
bun test apps/websocket/src/mcp/__tests__/browser-session.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/websocket/src/mcp/browser-session.ts apps/websocket/src/mcp/__tests__/browser-session.test.ts
git commit -m "feat(mcp): add immutable per-session browser state store"
```

---

## Task 3: Tool Context Types

**Files:**
- Create: `apps/websocket/src/mcp/tool-context.ts`

**Goal:** Define shared context types used by every tool handler.

- [ ] **Step 1: Write implementation**

Create `apps/websocket/src/mcp/tool-context.ts`:

```typescript
import type { BrowserSessionStore } from './browser-session';

export interface ToolContext {
  sessionId: string;
  sessions: BrowserSessionStore;
  websocketUrl: string;
}

export interface ServerContext {
  websocketUrl: string;
  sessions: BrowserSessionStore;
}
```

- [ ] **Step 2: Commit**

```bash
git add apps/websocket/src/mcp/tool-context.ts
git commit -m "feat(mcp): add tool and server context types"
```

---

## Task 4: Browser Resolver

**Files:**
- Create: `apps/websocket/src/mcp/browser-resolver.ts`
- Create: `apps/websocket/src/mcp/__tests__/browser-resolver.test.ts`

**Goal:** Implement browser resolution: explicit → auto-detect single → error.

- [ ] **Step 1: Write the failing test**

Create `apps/websocket/src/mcp/__tests__/browser-resolver.test.ts`:

```typescript
import { describe, expect, it } from 'bun:test';
import { resolveBrowser } from '../browser-resolver';
import type { BrowserConnection } from '@browser-bridge/shared';

const makeBrowser = (id: string, status: BrowserConnection['status']): BrowserConnection => ({
  browserId: id,
  userId: 'user-1',
  status,
  lastSeen: Date.now(),
});

describe('resolveBrowser', () => {
  it('returns explicit browserId when set', () => {
    const result = resolveBrowser('browser-a', [makeBrowser('browser-a', 'online')]);
    expect(result.success).toBe(true);
    if (result.success) expect(result.browserId).toBe('browser-a');
  });

  it('auto-detects single online browser', () => {
    const result = resolveBrowser(undefined, [makeBrowser('browser-a', 'online')]);
    expect(result.success).toBe(true);
    if (result.success) expect(result.browserId).toBe('browser-a');
  });

  it('fails when no browser is online', () => {
    const result = resolveBrowser(undefined, [makeBrowser('browser-a', 'offline')]);
    expect(result.success).toBe(false);
    if (!result.success) expect(result.message).toContain('No browser connected');
  });

  it('fails when multiple browsers are online', () => {
    const browsers = [makeBrowser('browser-a', 'online'), makeBrowser('browser-b', 'online')];
    const result = resolveBrowser(undefined, browsers);
    expect(result.success).toBe(false);
    if (!result.success) {
      expect(result.message).toContain('Multiple browsers');
      expect(result.availableBrowsers).toHaveLength(2);
    }
  });

  it('ignores offline browsers during auto-detect', () => {
    const browsers = [makeBrowser('browser-a', 'online'), makeBrowser('browser-b', 'offline')];
    const result = resolveBrowser(undefined, browsers);
    expect(result.success).toBe(true);
    if (result.success) expect(result.browserId).toBe('browser-a');
  });

  it('fails when explicit browser is not online', () => {
    const result = resolveBrowser('browser-a', [makeBrowser('browser-a', 'offline')]);
    expect(result.success).toBe(false);
    if (!result.success) expect(result.message).toContain('not online');
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
bun test apps/websocket/src/mcp/__tests__/browser-resolver.test.ts
```

Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

Create `apps/websocket/src/mcp/browser-resolver.ts`:

```typescript
import type { BrowserConnection } from '@browser-bridge/shared';

export type BrowserResolutionSuccess = { success: true; browserId: string };
export type BrowserResolutionFailure = {
  success: false;
  message: string;
  availableBrowsers?: BrowserConnection[];
};
export type BrowserResolutionResult = BrowserResolutionSuccess | BrowserResolutionFailure;

function formatBrowserList(browsers: BrowserConnection[]): string {
  return browsers.map((b) => `- ${b.browserId} (${b.status})`).join('\n');
}

export function resolveBrowser(
  explicitBrowserId: string | undefined,
  browsers: BrowserConnection[],
): BrowserResolutionResult {
  if (explicitBrowserId) {
    const match = browsers.find((b) => b.browserId === explicitBrowserId);
    if (!match) {
      return {
        success: false,
        message: `Browser "${explicitBrowserId}" is not connected.`,
        availableBrowsers: browsers,
      };
    }
    if (match.status !== 'online') {
      return {
        success: false,
        message: `Browser "${explicitBrowserId}" is not online (status: ${match.status}).`,
        availableBrowsers: browsers,
      };
    }
    return { success: true, browserId: explicitBrowserId };
  }

  const online = browsers.filter((b) => b.status === 'online');

  if (online.length === 0) {
    return {
      success: false,
      message: 'No browser connected. Start the extension/local-proxy first.',
      availableBrowsers: browsers,
    };
  }

  if (online.length === 1) {
    return { success: true, browserId: online[0].browserId };
  }

  return {
    success: false,
    message: `Multiple browsers are online. Call set_browser with one of:\n${formatBrowserList(online)}`,
    availableBrowsers: online,
  };
}
```

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
bun test apps/websocket/src/mcp/__tests__/browser-resolver.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/websocket/src/mcp/browser-resolver.ts apps/websocket/src/mcp/__tests__/browser-resolver.test.ts
git commit -m "feat(mcp): add browser resolution logic"
```

---

## Task 5: Command/Event Client Wrapper

**Files:**
- Create: `apps/websocket/src/mcp/command-client.ts`
- Create: `apps/websocket/src/mcp/__tests__/command-client.test.ts`

**Goal:** Wrap `@browser-bridge/websocket/client` so each tool call can send commands and events with a timeout.

- [ ] **Step 1: Write the failing test**

Create `apps/websocket/src/mcp/__tests__/command-client.test.ts`:

```typescript
import { describe, expect, it, afterEach } from 'bun:test';
import { sendCommand, sendEvent } from '../command-client';
import { encode, decode } from '@browser-bridge/websocket/protocol';
import type { Envelope } from '@browser-bridge/shared';

describe('sendCommand', () => {
  let server: ReturnType<typeof Bun.serve> | undefined;

  afterEach(() => {
    server?.stop();
    server = undefined;
  });

  it('sends a command and returns the response payload', async () => {
    server = Bun.serve({
      port: 0,
      hostname: '127.0.0.1',
      fetch(req, wsServer) {
        const url = new URL(req.url);
        if (url.pathname === '/ws') {
          const upgraded = wsServer.upgrade(req);
          if (!upgraded) return new Response('Upgrade failed', { status: 400 });
        }
        return new Response('Not found', { status: 404 });
      },
      websocket: {
        open() {},
        message(ws, data) {
          const envelope = decode(data as string) as Envelope;
          const response: Envelope = {
            id: envelope.id,
            type: 'response',
            browserId: envelope.browserId,
            payload: { status: 'ok', data: { title: 'Example' } },
            timestamp: Date.now(),
          };
          ws.send(
            encode('response', response.payload, {
              id: response.id,
              browserId: response.browserId,
            }),
          );
        },
        close() {},
      },
    });

    const result = await sendCommand({
      serverUrl: `ws://127.0.0.1:${server.port}/ws`,
      browserId: 'browser-a',
      command: 'pageinfo',
      params: {},
      timeoutMs: 1000,
    });

    expect(result.status).toBe('ok');
    expect(result.data).toEqual({ title: 'Example' });
  });

  it('rejects on timeout', async () => {
    server = Bun.serve({
      port: 0,
      hostname: '127.0.0.1',
      fetch(req, wsServer) {
        const url = new URL(req.url);
        if (url.pathname === '/ws') {
          const upgraded = wsServer.upgrade(req);
          if (!upgraded) return new Response('Upgrade failed', { status: 400 });
        }
        return new Response('Not found', { status: 404 });
      },
      websocket: { open() {}, message() {}, close() {} },
    });

    await expect(
      sendCommand({
        serverUrl: `ws://127.0.0.1:${server.port}/ws`,
        browserId: 'browser-a',
        command: 'pageinfo',
        params: {},
        timeoutMs: 50,
      }),
    ).rejects.toThrow('timeout');
  });
});

describe('sendEvent', () => {
  let server: ReturnType<typeof Bun.serve> | undefined;

  afterEach(() => {
    server?.stop();
    server = undefined;
  });

  it('sends an event and returns the response payload', async () => {
    server = Bun.serve({
      port: 0,
      hostname: '127.0.0.1',
      fetch(req, wsServer) {
        const url = new URL(req.url);
        if (url.pathname === '/ws') {
          const upgraded = wsServer.upgrade(req);
          if (!upgraded) return new Response('Upgrade failed', { status: 400 });
        }
        return new Response('Not found', { status: 404 });
      },
      websocket: {
        open() {},
        message(ws, data) {
          const envelope = decode(data as string) as Envelope;
          const response: Envelope = {
            id: envelope.id,
            type: 'response',
            browserId: envelope.browserId,
            payload: { status: 'ok', data: [{ browserId: 'a', status: 'online' }] },
            timestamp: Date.now(),
          };
          ws.send(
            encode('response', response.payload, {
              id: response.id,
              browserId: response.browserId,
            }),
          );
        },
        close() {},
      },
    });

    const result = await sendEvent({
      serverUrl: `ws://127.0.0.1:${server.port}/ws`,
      event: 'list_browsers',
      payload: {},
      timeoutMs: 1000,
    });

    expect(result.status).toBe('ok');
    expect(Array.isArray(result.data)).toBe(true);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
bun test apps/websocket/src/mcp/__tests__/command-client.test.ts
```

Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

Create `apps/websocket/src/mcp/command-client.ts`:

```typescript
import { createClient } from '@browser-bridge/websocket/client';
import type { CommandPayload, CommandType, ResponsePayload } from '@browser-bridge/shared';

export interface SendCommandOptions {
  serverUrl: string;
  browserId: string;
  command: CommandType;
  params: Record<string, unknown>;
  timeoutMs: number;
}

export interface SendEventOptions {
  serverUrl: string;
  event: string;
  payload: Record<string, unknown>;
  timeoutMs: number;
}

export async function sendCommand(options: SendCommandOptions): Promise<ResponsePayload> {
  const client = createClient({ url: options.serverUrl });

  try {
    await waitForOpen(client, Math.min(options.timeoutMs, 5000));

    const payload: CommandPayload = {
      command: options.command,
      params: options.params,
    };

    const envelope = await client.sendCommand(options.browserId, payload, {
      timeout: options.timeoutMs,
    });

    return (envelope.payload ?? { status: 'error', error: 'Empty response' }) as ResponsePayload;
  } finally {
    client.close();
  }
}

export async function sendEvent(options: SendEventOptions): Promise<ResponsePayload> {
  const client = createClient({ url: options.serverUrl });

  try {
    await waitForOpen(client, Math.min(options.timeoutMs, 5000));

    const envelope = await client.request(
      'event',
      { event: options.event, ...options.payload },
      { timeout: options.timeoutMs },
    );

    return (envelope.payload ?? { status: 'error', error: 'Empty response' }) as ResponsePayload;
  } finally {
    client.close();
  }
}

function waitForOpen(
  client: ReturnType<typeof createClient>,
  timeoutMs: number,
): Promise<void> {
  return new Promise((resolve, reject) => {
    if (client.readyState === WebSocket.OPEN) {
      resolve();
      return;
    }

    const start = Date.now();
    const interval = setInterval(() => {
      if (client.readyState === WebSocket.OPEN) {
        clearInterval(interval);
        resolve();
        return;
      }
      if (Date.now() - start > timeoutMs) {
        clearInterval(interval);
        client.close();
        reject(new Error('Failed to connect to WebSocket server'));
      }
    }, 10);
  });
}
```

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
bun test apps/websocket/src/mcp/__tests__/command-client.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/websocket/src/mcp/command-client.ts apps/websocket/src/mcp/__tests__/command-client.test.ts
git commit -m "feat(mcp): add command and event client wrappers"
```

---

## Task 6: Browser Lookup Helper

**Files:**
- Create: `apps/websocket/src/mcp/browser-lookup.ts`
- Create: `apps/websocket/src/mcp/__tests__/browser-lookup.test.ts`

**Goal:** Shared functions to fetch browser list and resolve target browser for tools.

- [ ] **Step 1: Write the failing test**

Create `apps/websocket/src/mcp/__tests__/browser-lookup.test.ts`:

```typescript
import { describe, expect, it } from 'bun:test';
import { fetchBrowserList, resolveTargetBrowser } from '../browser-lookup';
import { createBrowserSessionStore } from '../browser-session';
import { encode, decode } from '@browser-bridge/websocket/protocol';
import type { Envelope } from '@browser-bridge/shared';

function createListServer(browsers: unknown[]) {
  return Bun.serve({
    port: 0,
    hostname: '127.0.0.1',
    fetch(req, wsServer) {
      if (new URL(req.url).pathname === '/ws') {
        const upgraded = wsServer.upgrade(req);
        if (!upgraded) return new Response('Upgrade failed', { status: 400 });
      }
      return new Response('Not found', { status: 404 });
    },
    websocket: {
      open() {},
      message(ws, data) {
        const envelope = decode(data as string) as Envelope;
        const response: Envelope = {
          id: envelope.id,
          type: 'response',
          browserId: envelope.browserId,
          payload: { status: 'ok', data: browsers },
          timestamp: Date.now(),
        };
        ws.send(
          encode('response', response.payload, {
            id: response.id,
            browserId: response.browserId,
          }),
        );
      },
      close() {},
    },
  });
}

function makeContext(server: ReturnType<typeof createListServer>, sessionId: string) {
  const sessions = createBrowserSessionStore(10000);
  return {
    sessionId,
    sessions,
    websocketUrl: `ws://127.0.0.1:${server.port}/ws`,
  };
}

describe('fetchBrowserList', () => {
  it('returns parsed browser list', async () => {
    const server = createListServer([{ browserId: 'a', status: 'online' }]);
    const context = makeContext(server, 's1');
    const list = await fetchBrowserList(context, 1000);
    expect(list).toHaveLength(1);
    expect(list[0].browserId).toBe('a');
    server.stop();
  });
});

describe('resolveTargetBrowser', () => {
  it('uses explicit session browser', async () => {
    const server = createListServer([
      { browserId: 'a', userId: 'u', status: 'online', lastSeen: Date.now() },
      { browserId: 'b', userId: 'u', status: 'online', lastSeen: Date.now() },
    ]);
    const context = makeContext(server, 's1');
    context.sessions.setBrowser('s1', 'a');

    const result = await resolveTargetBrowser(context, 1000);

    expect(result.success).toBe(true);
    if (result.success) expect(result.browserId).toBe('a');
    server.stop();
  });

  it('auto-detects single browser', async () => {
    const server = createListServer([
      { browserId: 'a', userId: 'u', status: 'online', lastSeen: Date.now() },
    ]);
    const context = makeContext(server, 's1');

    const result = await resolveTargetBrowser(context, 1000);

    expect(result.success).toBe(true);
    if (result.success) expect(result.browserId).toBe('a');
    server.stop();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
bun test apps/websocket/src/mcp/__tests__/browser-lookup.test.ts
```

Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

Create `apps/websocket/src/mcp/browser-lookup.ts`:

```typescript
import type { BrowserConnection } from '@browser-bridge/shared';
import { sendEvent } from './command-client';
import { resolveBrowser, type BrowserResolutionResult } from './browser-resolver';
import type { ToolContext } from './tool-context';

export async function fetchBrowserList(
  context: ToolContext,
  timeoutMs: number,
): Promise<BrowserConnection[]> {
  const result = await sendEvent({
    serverUrl: context.websocketUrl,
    event: 'list_browsers',
    payload: {},
    timeoutMs,
  });

  if (result.status !== 'ok') {
    throw new Error(result.error ?? 'Failed to list browsers');
  }

  return Array.isArray(result.data) ? (result.data as BrowserConnection[]) : [];
}

export async function resolveTargetBrowser(
  context: ToolContext,
  timeoutMs: number,
): Promise<BrowserResolutionResult> {
  const explicit = context.sessions.getSession(context.sessionId).browserId;
  const browsers = await fetchBrowserList(context, timeoutMs);
  return resolveBrowser(explicit, browsers);
}
```

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
bun test apps/websocket/src/mcp/__tests__/browser-lookup.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/websocket/src/mcp/browser-lookup.ts apps/websocket/src/mcp/__tests__/browser-lookup.test.ts
git commit -m "feat(mcp): add browser lookup helper"
```

---

## Task 7: list_browsers Tool

**Files:**
- Create: `apps/websocket/src/mcp/tools/list-browsers.ts`
- Create: `apps/websocket/src/mcp/__tests__/tools/list-browsers.test.ts`

**Goal:** Expose `list_browsers` MCP tool.

- [ ] **Step 1: Write the failing test**

Create `apps/websocket/src/mcp/__tests__/tools/list-browsers.test.ts`:

```typescript
import { describe, expect, it } from 'bun:test';
import { executeListBrowsers } from '../../tools/list-browsers';
import { createBrowserSessionStore } from '../../browser-session';
import { encode, decode } from '@browser-bridge/websocket/protocol';
import type { Envelope } from '@browser-bridge/shared';

describe('executeListBrowsers', () => {
  it('returns a list of browsers', async () => {
    const server = Bun.serve({
      port: 0,
      hostname: '127.0.0.1',
      fetch(req, wsServer) {
        if (new URL(req.url).pathname === '/ws') {
          const upgraded = wsServer.upgrade(req);
          if (!upgraded) return new Response('Upgrade failed', { status: 400 });
        }
        return new Response('Not found', { status: 404 });
      },
      websocket: {
        open() {},
        message(ws, data) {
          const envelope = decode(data as string) as Envelope;
          const response: Envelope = {
            id: envelope.id,
            type: 'response',
            browserId: envelope.browserId,
            payload: {
              status: 'ok',
              data: [
                { browserId: 'browser-a', userId: 'u1', status: 'online', lastSeen: Date.now() },
              ],
            },
            timestamp: Date.now(),
          };
          ws.send(
            encode('response', response.payload, {
              id: response.id,
              browserId: response.browserId,
            }),
          );
        },
        close() {},
      },
    });

    const sessions = createBrowserSessionStore(10000);
    const result = await executeListBrowsers(
      { sessionId: 's1', sessions, websocketUrl: `ws://127.0.0.1:${server.port}/ws` },
      {},
    );

    expect(result).toContain('browser-a');
    server.stop();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
bun test apps/websocket/src/mcp/__tests__/tools/list-browsers.test.ts
```

Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

Create `apps/websocket/src/mcp/tools/list-browsers.ts`:

```typescript
import { z } from 'zod';
import type { FastMCP } from 'fastmcp';
import { sendEvent } from '../command-client';
import type { ToolContext, ServerContext } from '../tool-context';

export const ListBrowsersInputSchema = z.object({
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeListBrowsers(
  context: ToolContext,
  args: z.infer<typeof ListBrowsersInputSchema>,
): Promise<string> {
  const timeoutMs = args.timeout_ms ?? context.sessions.getSession(context.sessionId).defaultTimeoutMs;

  const result = await sendEvent({
    serverUrl: context.websocketUrl,
    event: 'list_browsers',
    payload: {},
    timeoutMs,
  });

  if (result.status !== 'ok') {
    throw new Error(result.error ?? 'Failed to list browsers');
  }

  const browsers = Array.isArray(result.data) ? result.data : [];
  if (browsers.length === 0) {
    return 'No browsers connected.';
  }

  return browsers
    .map((b: { browserId: string; status: string }) => `- ${b.browserId} (${b.status})`)
    .join('\n');
}

export function registerListBrowsersTool(server: FastMCP, serverContext: ServerContext): void {
  server.addTool({
    name: 'list_browsers',
    description: 'List all browsers connected to Browser Bridge.',
    parameters: ListBrowsersInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeListBrowsers(
        { sessionId: resolvedSessionId, sessions: serverContext.sessions, websocketUrl: serverContext.websocketUrl },
        args,
      );
    },
  });
}
```

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
bun test apps/websocket/src/mcp/__tests__/tools/list-browsers.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/websocket/src/mcp/tools/list-browsers.ts apps/websocket/src/mcp/__tests__/tools/list-browsers.test.ts
git commit -m "feat(mcp): add list_browsers tool"
```

---

## Task 8: set_browser Tool

**Files:**
- Create: `apps/websocket/src/mcp/tools/set-browser.ts`
- Create: `apps/websocket/src/mcp/__tests__/tools/set-browser.test.ts`

**Goal:** Allow agents to pin a browser for the MCP session.

- [ ] **Step 1: Write the failing test**

Create `apps/websocket/src/mcp/__tests__/tools/set-browser.test.ts`:

```typescript
import { describe, expect, it } from 'bun:test';
import { executeSetBrowser } from '../../tools/set-browser';
import { createBrowserSessionStore } from '../../browser-session';

describe('executeSetBrowser', () => {
  it('sets the browser in session state', async () => {
    const sessions = createBrowserSessionStore(10000);
    const result = await executeSetBrowser(
      { sessionId: 's1', sessions, websocketUrl: 'ws://localhost:3001' },
      { browserId: 'browser-a' },
    );
    expect(result).toContain('browser-a');
    expect(sessions.getSession('s1').browserId).toBe('browser-a');
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
bun test apps/websocket/src/mcp/__tests__/tools/set-browser.test.ts
```

Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

Create `apps/websocket/src/mcp/tools/set-browser.ts`:

```typescript
import { z } from 'zod';
import type { FastMCP } from 'fastmcp';
import type { ToolContext, ServerContext } from '../tool-context';

export const SetBrowserInputSchema = z.object({
  browserId: z.string().min(1),
});

export async function executeSetBrowser(
  context: ToolContext,
  args: z.infer<typeof SetBrowserInputSchema>,
): Promise<string> {
  context.sessions.setBrowser(context.sessionId, args.browserId);
  return `Browser set to "${args.browserId}" for this session.`;
}

export function registerSetBrowserTool(server: FastMCP, serverContext: ServerContext): void {
  server.addTool({
    name: 'set_browser',
    description: 'Explicitly choose which connected browser to control for this MCP session.',
    parameters: SetBrowserInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeSetBrowser(
        { sessionId: resolvedSessionId, sessions: serverContext.sessions, websocketUrl: serverContext.websocketUrl },
        args,
      );
    },
  });
}
```

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
bun test apps/websocket/src/mcp/__tests__/tools/set-browser.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/websocket/src/mcp/tools/set-browser.ts apps/websocket/src/mcp/__tests__/tools/set-browser.test.ts
git commit -m "feat(mcp): add set_browser tool"
```

---

## Task 9: navigate Tool

**Files:**
- Create: `apps/websocket/src/mcp/tools/navigate.ts`
- Create: `apps/websocket/src/mcp/__tests__/tools/navigate.test.ts`

**Goal:** Expose `navigate` tool.

- [ ] **Step 1: Write the failing test**

Create `apps/websocket/src/mcp/__tests__/tools/navigate.test.ts`:

```typescript
import { describe, expect, it } from 'bun:test';
import { executeNavigate } from '../../tools/navigate';
import { createBrowserSessionStore } from '../../browser-session';
import { encode, decode } from '@browser-bridge/websocket/protocol';
import type { Envelope } from '@browser-bridge/shared';

describe('executeNavigate', () => {
  it('navigates to url and returns success', async () => {
    const server = Bun.serve({
      port: 0,
      hostname: '127.0.0.1',
      fetch(req, wsServer) {
        if (new URL(req.url).pathname === '/ws') {
          const upgraded = wsServer.upgrade(req);
          if (!upgraded) return new Response('Upgrade failed', { status: 400 });
        }
        return new Response('Not found', { status: 404 });
      },
      websocket: {
        open() {},
        message(ws, data) {
          const envelope = decode(data as string) as Envelope;
          const payload =
            envelope.type === 'event'
              ? {
                  status: 'ok',
                  data: [{ browserId: 'a', userId: 'u', status: 'online', lastSeen: Date.now() }],
                }
              : { status: 'ok', message: 'Navigated' };
          ws.send(
            encode('response', payload, {
              id: envelope.id,
              browserId: envelope.browserId,
            }),
          );
        },
        close() {},
      },
    });

    const sessions = createBrowserSessionStore(10000);

    const result = await executeNavigate(
      { sessionId: 's1', sessions, websocketUrl: `ws://127.0.0.1:${server.port}/ws` },
      { url: 'https://example.com' },
    );

    expect(result).toContain('Navigated');
    server.stop();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
bun test apps/websocket/src/mcp/__tests__/tools/navigate.test.ts
```

Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

Create `apps/websocket/src/mcp/tools/navigate.ts`:

```typescript
import { z } from 'zod';
import type { FastMCP } from 'fastmcp';
import { sendCommand } from '../command-client';
import { resolveTargetBrowser } from '../browser-lookup';
import type { ToolContext, ServerContext } from '../tool-context';

export const NavigateInputSchema = z.object({
  url: z.string().url(),
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeNavigate(
  context: ToolContext,
  args: z.infer<typeof NavigateInputSchema>,
): Promise<string> {
  const timeoutMs = args.timeout_ms ?? context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);

  if (!resolution.success) {
    throw new Error(resolution.message);
  }

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'navigate',
    params: { url: args.url },
    timeoutMs,
  });

  if (result.status !== 'ok') {
    throw new Error(result.error ?? 'Navigation failed');
  }

  return result.message ?? `Navigated to ${args.url}`;
}

export function registerNavigateTool(server: FastMCP, serverContext: ServerContext): void {
  server.addTool({
    name: 'navigate',
    description: 'Navigate the active tab of the selected browser to a URL.',
    parameters: NavigateInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeNavigate(
        { sessionId: resolvedSessionId, sessions: serverContext.sessions, websocketUrl: serverContext.websocketUrl },
        args,
      );
    },
  });
}
```

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
bun test apps/websocket/src/mcp/__tests__/tools/navigate.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/websocket/src/mcp/tools/navigate.ts apps/websocket/src/mcp/__tests__/tools/navigate.test.ts
git commit -m "feat(mcp): add navigate tool"
```

---

## Task 10: click Tool

**Files:**
- Create: `apps/websocket/src/mcp/tools/click.ts`
- Create: `apps/websocket/src/mcp/__tests__/tools/click.test.ts`

**Goal:** Expose `click` tool.

- [ ] **Step 1: Write the failing test**

Create `apps/websocket/src/mcp/__tests__/tools/click.test.ts`:

```typescript
import { describe, expect, it } from 'bun:test';
import { executeClick } from '../../tools/click';
import { createBrowserSessionStore } from '../../browser-session';
import { encode, decode } from '@browser-bridge/websocket/protocol';
import type { Envelope } from '@browser-bridge/shared';

describe('executeClick', () => {
  it('sends click command and returns success', async () => {
    const server = Bun.serve({
      port: 0,
      hostname: '127.0.0.1',
      fetch(req, wsServer) {
        if (new URL(req.url).pathname === '/ws') {
          const upgraded = wsServer.upgrade(req);
          if (!upgraded) return new Response('Upgrade failed', { status: 400 });
        }
        return new Response('Not found', { status: 404 });
      },
      websocket: {
        open() {},
        message(ws, data) {
          const envelope = decode(data as string) as Envelope;
          const payload =
            envelope.type === 'event'
              ? {
                  status: 'ok',
                  data: [{ browserId: 'a', userId: 'u', status: 'online', lastSeen: Date.now() }],
                }
              : { status: 'ok', message: 'Clicked' };
          ws.send(
            encode('response', payload, {
              id: envelope.id,
              browserId: envelope.browserId,
            }),
          );
        },
        close() {},
      },
    });

    const sessions = createBrowserSessionStore(10000);
    const result = await executeClick(
      { sessionId: 's1', sessions, websocketUrl: `ws://127.0.0.1:${server.port}/ws` },
      { selector: '#submit' },
    );
    expect(result).toContain('Clicked');
    server.stop();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
bun test apps/websocket/src/mcp/__tests__/tools/click.test.ts
```

Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

Create `apps/websocket/src/mcp/tools/click.ts`:

```typescript
import { z } from 'zod';
import type { FastMCP } from 'fastmcp';
import { sendCommand } from '../command-client';
import { resolveTargetBrowser } from '../browser-lookup';
import type { ToolContext, ServerContext } from '../tool-context';

export const ClickInputSchema = z.object({
  selector: z.string().min(1),
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeClick(
  context: ToolContext,
  args: z.infer<typeof ClickInputSchema>,
): Promise<string> {
  const timeoutMs = args.timeout_ms ?? context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'click',
    params: { selector: args.selector },
    timeoutMs,
  });

  if (result.status !== 'ok') throw new Error(result.error ?? 'Click failed');
  return result.message ?? `Clicked ${args.selector}`;
}

export function registerClickTool(server: FastMCP, serverContext: ServerContext): void {
  server.addTool({
    name: 'click',
    description: 'Click an element in the selected browser by CSS selector.',
    parameters: ClickInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeClick(
        { sessionId: resolvedSessionId, sessions: serverContext.sessions, websocketUrl: serverContext.websocketUrl },
        args,
      );
    },
  });
}
```

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
bun test apps/websocket/src/mcp/__tests__/tools/click.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/websocket/src/mcp/tools/click.ts apps/websocket/src/mcp/__tests__/tools/click.test.ts
git commit -m "feat(mcp): add click tool"
```

---

## Task 11: type Tool

**Files:**
- Create: `apps/websocket/src/mcp/tools/type.ts`
- Create: `apps/websocket/src/mcp/__tests__/tools/type.test.ts`

**Goal:** Expose `type` tool.

- [ ] **Step 1: Write the failing test**

Create `apps/websocket/src/mcp/__tests__/tools/type.test.ts`:

```typescript
import { describe, expect, it } from 'bun:test';
import { executeType } from '../../tools/type';
import { createBrowserSessionStore } from '../../browser-session';
import { encode, decode } from '@browser-bridge/websocket/protocol';
import type { Envelope } from '@browser-bridge/shared';

describe('executeType', () => {
  it('sends type command and returns success', async () => {
    const server = Bun.serve({
      port: 0,
      hostname: '127.0.0.1',
      fetch(req, wsServer) {
        if (new URL(req.url).pathname === '/ws') {
          const upgraded = wsServer.upgrade(req);
          if (!upgraded) return new Response('Upgrade failed', { status: 400 });
        }
        return new Response('Not found', { status: 404 });
      },
      websocket: {
        open() {},
        message(ws, data) {
          const envelope = decode(data as string) as Envelope;
          const payload =
            envelope.type === 'event'
              ? {
                  status: 'ok',
                  data: [{ browserId: 'a', userId: 'u', status: 'online', lastSeen: Date.now() }],
                }
              : { status: 'ok', message: 'Typed' };
          ws.send(
            encode('response', payload, {
              id: envelope.id,
              browserId: envelope.browserId,
            }),
          );
        },
        close() {},
      },
    });

    const sessions = createBrowserSessionStore(10000);
    const result = await executeType(
      { sessionId: 's1', sessions, websocketUrl: `ws://127.0.0.1:${server.port}/ws` },
      { selector: '#search', text: 'hello' },
    );
    expect(result).toContain('Typed');
    server.stop();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
bun test apps/websocket/src/mcp/__tests__/tools/type.test.ts
```

Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

Create `apps/websocket/src/mcp/tools/type.ts`:

```typescript
import { z } from 'zod';
import type { FastMCP } from 'fastmcp';
import { sendCommand } from '../command-client';
import { resolveTargetBrowser } from '../browser-lookup';
import type { ToolContext, ServerContext } from '../tool-context';

export const TypeInputSchema = z.object({
  selector: z.string().min(1),
  text: z.string(),
  submit: z.boolean().optional(),
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeType(
  context: ToolContext,
  args: z.infer<typeof TypeInputSchema>,
): Promise<string> {
  const timeoutMs = args.timeout_ms ?? context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'type',
    params: { selector: args.selector, text: args.text, submit: args.submit },
    timeoutMs,
  });

  if (result.status !== 'ok') throw new Error(result.error ?? 'Type failed');
  return result.message ?? `Typed into ${args.selector}`;
}

export function registerTypeTool(server: FastMCP, serverContext: ServerContext): void {
  server.addTool({
    name: 'type',
    description: 'Type text into an input element in the selected browser.',
    parameters: TypeInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeType(
        { sessionId: resolvedSessionId, sessions: serverContext.sessions, websocketUrl: serverContext.websocketUrl },
        args,
      );
    },
  });
}
```

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
bun test apps/websocket/src/mcp/__tests__/tools/type.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/websocket/src/mcp/tools/type.ts apps/websocket/src/mcp/__tests__/tools/type.test.ts
git commit -m "feat(mcp): add type tool"
```

---

## Task 12: screenshot Tool

**Files:**
- Create: `apps/websocket/src/mcp/tools/screenshot.ts`
- Create: `apps/websocket/src/mcp/__tests__/tools/screenshot.test.ts`

**Goal:** Expose `screenshot` tool returning image content.

- [ ] **Step 1: Write the failing test**

Create `apps/websocket/src/mcp/__tests__/tools/screenshot.test.ts`:

```typescript
import { describe, expect, it } from 'bun:test';
import { executeScreenshot } from '../../tools/screenshot';
import { createBrowserSessionStore } from '../../browser-session';
import { encode, decode } from '@browser-bridge/websocket/protocol';
import type { Envelope } from '@browser-bridge/shared';

describe('executeScreenshot', () => {
  it('returns image content', async () => {
    const server = Bun.serve({
      port: 0,
      hostname: '127.0.0.1',
      fetch(req, wsServer) {
        if (new URL(req.url).pathname === '/ws') {
          const upgraded = wsServer.upgrade(req);
          if (!upgraded) return new Response('Upgrade failed', { status: 400 });
        }
        return new Response('Not found', { status: 404 });
      },
      websocket: {
        open() {},
        message(ws, data) {
          const envelope = decode(data as string) as Envelope;
          const payload =
            envelope.type === 'event'
              ? {
                  status: 'ok',
                  data: [{ browserId: 'a', userId: 'u', status: 'online', lastSeen: Date.now() }],
                }
              : {
                  status: 'ok',
                  data: 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=',
                };
          ws.send(
            encode('response', payload, {
              id: envelope.id,
              browserId: envelope.browserId,
            }),
          );
        },
        close() {},
      },
    });

    const sessions = createBrowserSessionStore(10000);
    const result = await executeScreenshot(
      { sessionId: 's1', sessions, websocketUrl: `ws://127.0.0.1:${server.port}/ws` },
      {},
    );

    expect(result.content).toHaveLength(1);
    expect(result.content[0].type).toBe('image');
    server.stop();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
bun test apps/websocket/src/mcp/__tests__/tools/screenshot.test.ts
```

Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

Create `apps/websocket/src/mcp/tools/screenshot.ts`:

```typescript
import { z } from 'zod';
import type { FastMCP } from 'fastmcp';
import { sendCommand } from '../command-client';
import { resolveTargetBrowser } from '../browser-lookup';
import type { ToolContext, ServerContext } from '../tool-context';

export const ScreenshotInputSchema = z.object({
  full_page: z.boolean().optional(),
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeScreenshot(
  context: ToolContext,
  args: z.infer<typeof ScreenshotInputSchema>,
): Promise<{ content: Array<{ type: 'image'; data: string; mimeType: 'image/png' }> }> {
  const timeoutMs = args.timeout_ms ?? context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'screenshot',
    params: { fullPage: args.full_page },
    timeoutMs,
  });

  if (result.status !== 'ok') throw new Error(result.error ?? 'Screenshot failed');
  const data = typeof result.data === 'string' ? result.data : '';

  return {
    content: [{ type: 'image', data, mimeType: 'image/png' }],
  };
}

export function registerScreenshotTool(server: FastMCP, serverContext: ServerContext): void {
  server.addTool({
    name: 'screenshot',
    description: 'Take a screenshot of the selected browser.',
    parameters: ScreenshotInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeScreenshot(
        { sessionId: resolvedSessionId, sessions: serverContext.sessions, websocketUrl: serverContext.websocketUrl },
        args,
      );
    },
  });
}
```

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
bun test apps/websocket/src/mcp/__tests__/tools/screenshot.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/websocket/src/mcp/tools/screenshot.ts apps/websocket/src/mcp/__tests__/tools/screenshot.test.ts
git commit -m "feat(mcp): add screenshot tool"
```

---

## Task 13: pageinfo Tool

**Files:**
- Create: `apps/websocket/src/mcp/tools/pageinfo.ts`
- Create: `apps/websocket/src/mcp/__tests__/tools/pageinfo.test.ts`

**Goal:** Expose `pageinfo` tool.

- [ ] **Step 1: Write the failing test**

Create `apps/websocket/src/mcp/__tests__/tools/pageinfo.test.ts`:

```typescript
import { describe, expect, it } from 'bun:test';
import { executePageinfo } from '../../tools/pageinfo';
import { createBrowserSessionStore } from '../../browser-session';
import { encode, decode } from '@browser-bridge/websocket/protocol';
import type { Envelope } from '@browser-bridge/shared';

describe('executePageinfo', () => {
  it('returns page info as JSON string', async () => {
    const server = Bun.serve({
      port: 0,
      hostname: '127.0.0.1',
      fetch(req, wsServer) {
        if (new URL(req.url).pathname === '/ws') {
          const upgraded = wsServer.upgrade(req);
          if (!upgraded) return new Response('Upgrade failed', { status: 400 });
        }
        return new Response('Not found', { status: 404 });
      },
      websocket: {
        open() {},
        message(ws, data) {
          const envelope = decode(data as string) as Envelope;
          const payload =
            envelope.type === 'event'
              ? {
                  status: 'ok',
                  data: [{ browserId: 'a', userId: 'u', status: 'online', lastSeen: Date.now() }],
                }
              : { status: 'ok', data: { title: 'Example', url: 'https://example.com' } };
          ws.send(
            encode('response', payload, {
              id: envelope.id,
              browserId: envelope.browserId,
            }),
          );
        },
        close() {},
      },
    });

    const sessions = createBrowserSessionStore(10000);
    const result = await executePageinfo(
      { sessionId: 's1', sessions, websocketUrl: `ws://127.0.0.1:${server.port}/ws` },
      {},
    );
    expect(result).toContain('Example');
    expect(result).toContain('https://example.com');
    server.stop();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
bun test apps/websocket/src/mcp/__tests__/tools/pageinfo.test.ts
```

Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

Create `apps/websocket/src/mcp/tools/pageinfo.ts`:

```typescript
import { z } from 'zod';
import type { FastMCP } from 'fastmcp';
import { sendCommand } from '../command-client';
import { resolveTargetBrowser } from '../browser-lookup';
import type { ToolContext, ServerContext } from '../tool-context';

export const PageinfoInputSchema = z.object({
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executePageinfo(
  context: ToolContext,
  args: z.infer<typeof PageinfoInputSchema>,
): Promise<string> {
  const timeoutMs = args.timeout_ms ?? context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'pageinfo',
    params: {},
    timeoutMs,
  });

  if (result.status !== 'ok') throw new Error(result.error ?? 'pageinfo failed');
  return JSON.stringify(result.data ?? {}, null, 2);
}

export function registerPageinfoTool(server: FastMCP, serverContext: ServerContext): void {
  server.addTool({
    name: 'pageinfo',
    description: 'Get title, URL, and tab list from the selected browser.',
    parameters: PageinfoInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executePageinfo(
        { sessionId: resolvedSessionId, sessions: serverContext.sessions, websocketUrl: serverContext.websocketUrl },
        args,
      );
    },
  });
}
```

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
bun test apps/websocket/src/mcp/__tests__/tools/pageinfo.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/websocket/src/mcp/tools/pageinfo.ts apps/websocket/src/mcp/__tests__/tools/pageinfo.test.ts
git commit -m "feat(mcp): add pageinfo tool"
```

---

## Task 14: FastMCP Server Bootstrap

**Files:**
- Create: `apps/websocket/src/mcp/server.ts`
- Create: `apps/websocket/src/mcp/index.ts`
- Create: `apps/websocket/src/mcp/__tests__/server.test.ts`

**Goal:** Wire all tools into FastMCP and expose `startMcpServer(options)`.

- [ ] **Step 1: Write the failing test**

Create `apps/websocket/src/mcp/__tests__/server.test.ts`:

```typescript
import { describe, expect, it, afterEach } from 'bun:test';
import type { FastMCP } from 'fastmcp';
import { startMcpServer } from '../server';
import { encode, decode } from '@browser-bridge/websocket/protocol';
import type { Envelope } from '@browser-bridge/shared';

describe('startMcpServer', () => {
  let wsServer: ReturnType<typeof Bun.serve> | undefined;
  let mcpServer: FastMCP | undefined;

  afterEach(async () => {
    await mcpServer?.stop();
    wsServer?.stop();
  });

  it('starts without error', async () => {
    wsServer = Bun.serve({
      port: 0,
      hostname: '127.0.0.1',
      fetch(req, wsServerInstance) {
        if (new URL(req.url).pathname === '/ws') {
          const upgraded = wsServerInstance.upgrade(req);
          if (!upgraded) return new Response('Upgrade failed', { status: 400 });
        }
        return new Response('Not found', { status: 404 });
      },
      websocket: {
        open() {},
        message(ws, data) {
          const envelope = decode(data as string) as Envelope;
          const response: Envelope = {
            id: envelope.id,
            type: 'response',
            browserId: envelope.browserId,
            payload: { status: 'ok', data: [{ browserId: 'a', userId: 'u', status: 'online', lastSeen: Date.now() }] },
            timestamp: Date.now(),
          };
          ws.send(
            encode('response', response.payload, {
              id: response.id,
              browserId: response.browserId,
            }),
          );
        },
        close() {},
      },
    });

    mcpServer = startMcpServer({
      websocketUrl: `ws://127.0.0.1:${wsServer.port}/ws`,
      port: 0,
      hostname: '127.0.0.1',
      defaultTimeoutMs: 1000,
      version: 'test',
    });

    expect(mcpServer).toBeDefined();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
bun test apps/websocket/src/mcp/__tests__/server.test.ts
```

Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

Create `apps/websocket/src/mcp/server.ts`:

```typescript
import { FastMCP } from 'fastmcp';
import { createBrowserSessionStore } from './browser-session';
import { registerListBrowsersTool } from './tools/list-browsers';
import { registerSetBrowserTool } from './tools/set-browser';
import { registerNavigateTool } from './tools/navigate';
import { registerClickTool } from './tools/click';
import { registerTypeTool } from './tools/type';
import { registerScreenshotTool } from './tools/screenshot';
import { registerPageinfoTool } from './tools/pageinfo';

export interface McpServerOptions {
  websocketUrl: string;
  port: number;
  hostname: string;
  defaultTimeoutMs: number;
  version: string;
}

export function startMcpServer(options: McpServerOptions): FastMCP {
  const server = new FastMCP({
    name: 'Browser Bridge',
    version: options.version,
  });

  const sessions = createBrowserSessionStore(options.defaultTimeoutMs);
  const serverContext = {
    websocketUrl: options.websocketUrl,
    sessions,
  };

  registerListBrowsersTool(server, serverContext);
  registerSetBrowserTool(server, serverContext);
  registerNavigateTool(server, serverContext);
  registerClickTool(server, serverContext);
  registerTypeTool(server, serverContext);
  registerScreenshotTool(server, serverContext);
  registerPageinfoTool(server, serverContext);

  server.start({
    transportType: 'httpStream',
    httpStream: {
      port: options.port,
      host: options.hostname,
      endpoint: '/mcp',
    },
  });

  return server;
}
```

Create `apps/websocket/src/mcp/index.ts`:

```typescript
export { startMcpServer, type McpServerOptions } from './server';
```

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
bun test apps/websocket/src/mcp/__tests__/server.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/websocket/src/mcp/server.ts apps/websocket/src/mcp/index.ts apps/websocket/src/mcp/__tests__/server.test.ts
git commit -m "feat(mcp): add FastMCP server bootstrap"
```

---

## Task 15: Integrate MCP Server into WebSocket App

**Files:**
- Modify: `apps/websocket/src/index.ts`

**Goal:** Start FastMCP alongside the WebSocket server when the app launches.

- [ ] **Step 1: Modify index.ts**

Update `apps/websocket/src/index.ts`:

```typescript
import { ApiKeyAuthProvider } from '@browser-bridge/shared/auth';
import { WEBSOCKET_PORT } from '@browser-bridge/shared';
import { startServer } from './server';
import { startMcpServer } from './mcp';
import pkg from '../package.json';

const apiKeys = process.env.BRIDGE_API_KEYS;
const port = process.env.BRIDGE_WS_PORT ? Number(process.env.BRIDGE_WS_PORT) : undefined;
const hostname = process.env.BRIDGE_WS_HOSTNAME;

const authProvider = apiKeys
  ? new ApiKeyAuthProvider(apiKeys.split(',').map((k) => k.trim()).filter(Boolean))
  : undefined;

startServer(port, authProvider, hostname);

const mcpPort = process.env.BRIDGE_MCP_PORT ? Number(process.env.BRIDGE_MCP_PORT) : 3003;
const mcpHostname = process.env.BRIDGE_MCP_HOSTNAME ?? '127.0.0.1';
const mcpTimeout = process.env.BRIDGE_MCP_TIMEOUT_MS
  ? Number(process.env.BRIDGE_MCP_TIMEOUT_MS)
  : 10000;

startMcpServer({
  websocketUrl: `ws://${hostname ?? 'localhost'}:${port ?? WEBSOCKET_PORT}`,
  port: mcpPort,
  hostname: mcpHostname,
  defaultTimeoutMs: mcpTimeout,
  version: pkg.version,
});

console.log(`MCP server listening on http://${mcpHostname}:${mcpPort}/mcp`);
```

- [ ] **Step 2: Run type check**

Run:

```bash
bun run type-check
```

Expected: No errors.

- [ ] **Step 3: Run tests**

Run:

```bash
bun test apps/websocket/src/mcp
```

Expected: All MCP tests pass.

- [ ] **Step 4: Commit**

```bash
git add apps/websocket/src/index.ts
git commit -m "feat(mcp): start MCP server alongside websocket server"
```

---

## Task 16: Documentation

**Files:**
- Create: `docs/mcp-setup.md`
- Modify: `README.md`

**Goal:** Guide users to configure the MCP endpoint in their agent client.

- [ ] **Step 1: Create docs/mcp-setup.md**

Create `docs/mcp-setup.md`:

```markdown
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
```

- [ ] **Step 2: Update README.md**

Add an MCP section near the usage examples. Keep it short and link to `docs/mcp-setup.md`:

```markdown
## Use with MCP

Browser Bridge also exposes a [Streamable HTTP MCP server](docs/mcp-setup.md). Once the WebSocket server is running, add `http://localhost:3003/mcp` to your MCP client (Claude Desktop, Cursor, etc.) to control browsers directly.
```

- [ ] **Step 3: Run format/lint**

Run:

```bash
bunx @biomejs/biome check --write .
```

Expected: No unfixable errors.

- [ ] **Step 4: Commit**

```bash
git add docs/mcp-setup.md README.md
git commit -m "docs: add MCP setup guide and README section"
```

---

## Task 17: Final Verification

**Goal:** Ensure everything works together.

- [ ] **Step 1: Run full test suite**

Run:

```bash
bun run test
```

Expected: All tests pass.

- [ ] **Step 2: Run type check**

Run:

```bash
bun run type-check
```

Expected: No errors.

- [ ] **Step 3: Run Biome check**

Run:

```bash
bunx @biomejs/biome check .
```

Expected: No errors.

- [ ] **Step 4: Smoke test**

Run:

```bash
bun run dev:websocket
```

In another terminal, verify the port is open and responds:

```bash
lsof -i :3003 | grep LISTEN
```

Expected: A process is listening on port 3003.

To verify the MCP protocol works, write a small script using `@modelcontextprotocol/sdk` (or FastMCP's own client) to initialize a session and call `tools/list`. Place this script in `scripts/mcp-smoke-test.ts` if you want to reuse it.

Example smoke test script:

```typescript
import { Client } from '@modelcontextprotocol/sdk/client/index.js';
import { StreamableHTTPClientTransport } from '@modelcontextprotocol/sdk/client/streamableHttp.js';

const client = new Client({ name: 'smoke-test', version: '1.0.0' });
const transport = new StreamableHTTPClientTransport(new URL('http://localhost:3003/mcp'));
await client.connect(transport);
const tools = await client.listTools();
console.log(tools.tools.map((t) => t.name));
await client.close();
```

Run it with `bun run scripts/mcp-smoke-test.ts`. Expected: prints the names of the registered tools.

- [ ] **Step 5: Commit any fixes**

```bash
git add .
git commit -m "chore: final MCP verification fixes"
```

---

## Self-Review Checklist

- [x] Spec coverage: every design requirement maps to one or more tasks.
- [x] No placeholders: each step contains concrete code or commands.
- [x] Type consistency: `ToolContext`, `ServerContext`, `BrowserSession`, and `BrowserSessionStore` are used consistently.
- [x] File paths match the monorepo structure.
- [x] Each task is small enough for one focused implementation session.

**Spec coverage mapping:**

| Spec Section | Plan Task(s) |
|--------------|--------------|
| Co-located MCP in websocket app | 14, 15 |
| Dedicated port 3003 | 14, 15 |
| No localhost auth | 14 (binds to 127.0.0.1) |
| Curated tools | 7, 9–13 |
| Auto-detect single browser | 4, 6, 9–13 |
| Per-tool `timeout_ms` | 7, 9–13 schemas |
| README + docs/mcp-setup.md | 16 |
| 80%+ test coverage | every task includes tests |

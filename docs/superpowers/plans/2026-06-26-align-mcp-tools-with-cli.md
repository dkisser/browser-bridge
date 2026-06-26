# Align MCP Tools with CLI Commands

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add every Browser Bridge CLI command that is missing from the MCP server so that MCP agents can perform the same browser operations as the CLI.

**Architecture:** Each new command maps to a single FastMCP tool in its own file under `apps/websocket/src/mcp/tools/`. Every tool reuses the existing `executeCommand` helper, the `resolveTargetBrowser` session logic, and the shared `CommandType` union. Tool registration is added to `apps/websocket/src/mcp/server.ts`. Tests mirror the pattern used by the existing `navigate`, `click`, and `pageinfo` tools.

**Tech Stack:** Bun, TypeScript, FastMCP, Zod, `@browser-bridge/shared`, `@browser-bridge/websocket`.

---

## File Structure

| File | Change | Responsibility |
|---|---|---|
| `apps/websocket/src/mcp/tools/go-back.ts` | Create | `go_back` MCP tool |
| `apps/websocket/src/mcp/tools/go-forward.ts` | Create | `go_forward` MCP tool |
| `apps/websocket/src/mcp/tools/refresh.ts` | Create | `refresh` MCP tool |
| `apps/websocket/src/mcp/tools/tab-list.ts` | Create | `tab_list` MCP tool |
| `apps/websocket/src/mcp/tools/tab-new.ts` | Create | `tab_new` MCP tool |
| `apps/websocket/src/mcp/tools/tab-close.ts` | Create | `tab_close` MCP tool |
| `apps/websocket/src/mcp/tools/tab-switch.ts` | Create | `tab_switch` MCP tool |
| `apps/websocket/src/mcp/tools/select.ts` | Create | `select` MCP tool |
| `apps/websocket/src/mcp/tools/scroll.ts` | Create | `scroll` MCP tool |
| `apps/websocket/src/mcp/tools/hover.ts` | Create | `hover` MCP tool |
| `apps/websocket/src/mcp/tools/get-text.ts` | Create | `get_text` MCP tool |
| `apps/websocket/src/mcp/tools/get-html.ts` | Create | `get_html` MCP tool |
| `apps/websocket/src/mcp/tools/wait-element.ts` | Create | `wait_element` MCP tool |
| `apps/websocket/src/mcp/tools/wait-navigation.ts` | Create | `wait_navigation` MCP tool |
| `apps/websocket/src/mcp/__tests__/tools/*.test.ts` | Create | One test file per new tool |
| `apps/websocket/src/mcp/server.ts` | Modify | Register all new tools |
| `docs/mcp-setup.md` | Modify | Add new tools to the tool reference |
| `apps/websocket/src/mcp/tools/screenshot.ts` | Modify | Add missing `fullPage` param support documented in the design spec |

---

## Shared patterns used by every tool

Every tool handler follows this shape:

```typescript
import type { FastMCP } from 'fastmcp';
import { z } from 'zod';
import { resolveTargetBrowser } from '../browser-lookup';
import { sendCommand } from '../command-client';
import type { ServerContext, ToolContext } from '../tool-context';

export const SomeInputSchema = z.object({
  // command-specific params
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeSome(
  context: ToolContext,
  args: z.infer<typeof SomeInputSchema>,
): Promise<string> {
  const timeoutMs =
    args.timeout_ms ??
    context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: '<commandType>',
    params: { /* mapped params */ },
    timeoutMs,
  });

  if (result.status !== 'ok') throw new Error(result.error ?? '<Command> failed');
  return result.message ?? `<Human-readable success>`;
}

export function registerSomeTool(
  server: FastMCP,
  serverContext: ServerContext,
): void {
  server.addTool({
    name: '<tool_name>',
    description: '<description>',
    parameters: SomeInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeSome(
        {
          sessionId: resolvedSessionId,
          sessions: serverContext.sessions,
          websocketUrl: serverContext.websocketUrl,
        },
        args,
      );
    },
  });
}
```

Every test uses a real Bun WebSocket server on port `0` and follows the existing pattern in `apps/websocket/src/mcp/__tests__/tools/navigate.test.ts`.

---

### Task 1: `goBack` tool

**Files:**
- Create: `apps/websocket/src/mcp/tools/go-back.ts`
- Create: `apps/websocket/src/mcp/__tests__/tools/go-back.test.ts`

- [ ] **Step 1: Write the failing test**

```typescript
import { describe, expect, it } from 'bun:test';
import type { Envelope } from '@browser-bridge/shared';
import { decode, encode } from '@browser-bridge/websocket/protocol';
import { createBrowserSessionStore } from '../../browser-session';
import { executeGoBack } from '../../tools/go-back';

describe('executeGoBack', () => {
  it('goes back', async () => {
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
                  data: [
                    {
                      browserId: 'a',
                      userId: 'u',
                      status: 'online',
                      lastSeen: Date.now(),
                    },
                  ],
                }
              : { status: 'ok', data: 'back' };
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
    try {
      const result = await executeGoBack(
        {
          sessionId: 's1',
          sessions,
          websocketUrl: `ws://127.0.0.1:${server.port}/ws`,
        },
        {},
      );

      expect(result).toContain('back');
    } finally {
      server.stop();
    }
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `bun test apps/websocket/src/mcp/__tests__/tools/go-back.test.ts`

Expected: FAIL with "Module not found" or "executeGoBack is not defined"

- [ ] **Step 3: Write minimal implementation**

```typescript
import type { FastMCP } from 'fastmcp';
import { z } from 'zod';
import { resolveTargetBrowser } from '../browser-lookup';
import { sendCommand } from '../command-client';
import type { ServerContext, ToolContext } from '../tool-context';

export const GoBackInputSchema = z.object({
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeGoBack(
  context: ToolContext,
  args: z.infer<typeof GoBackInputSchema>,
): Promise<string> {
  const timeoutMs =
    args.timeout_ms ??
    context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'goBack',
    params: {},
    timeoutMs,
  });

  if (result.status !== 'ok') throw new Error(result.error ?? 'goBack failed');
  return result.message ?? 'Navigated back';
}

export function registerGoBackTool(
  server: FastMCP,
  serverContext: ServerContext,
): void {
  server.addTool({
    name: 'go_back',
    description: 'Go back one page in browser history.',
    parameters: GoBackInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeGoBack(
        {
          sessionId: resolvedSessionId,
          sessions: serverContext.sessions,
          websocketUrl: serverContext.websocketUrl,
        },
        args,
      );
    },
  });
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `bun test apps/websocket/src/mcp/__tests__/tools/go-back.test.ts`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add apps/websocket/src/mcp/tools/go-back.ts apps/websocket/src/mcp/__tests__/tools/go-back.test.ts
git commit -m "feat(mcp): add go_back tool"
```

---

### Task 2: `goForward` tool

**Files:**
- Create: `apps/websocket/src/mcp/tools/go-forward.ts`
- Create: `apps/websocket/src/mcp/__tests__/tools/go-forward.test.ts`

- [ ] **Step 1: Write the failing test**

Mirror Task 1, but call `executeGoForward` and assert the result contains `'forward'`.

- [ ] **Step 2: Run test to verify it fails**

Run: `bun test apps/websocket/src/mcp/__tests__/tools/go-forward.test.ts`

Expected: FAIL

- [ ] **Step 3: Write minimal implementation**

Mirror Task 1 implementation, use `command: 'goForward'`, tool name `go_forward`, description `"Go forward one page in browser history."`, and return `'Navigated forward'`.

- [ ] **Step 4: Run test to verify it passes**

Run: `bun test apps/websocket/src/mcp/__tests__/tools/go-forward.test.ts`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add apps/websocket/src/mcp/tools/go-forward.ts apps/websocket/src/mcp/__tests__/tools/go-forward.test.ts
git commit -m "feat(mcp): add go_forward tool"
```

---

### Task 3: `refresh` tool

**Files:**
- Create: `apps/websocket/src/mcp/tools/refresh.ts`
- Create: `apps/websocket/src/mcp/__tests__/tools/refresh.test.ts`

- [ ] **Step 1: Write the failing test**

Mirror Task 1, but call `executeRefresh` and assert the result contains `'refreshed'`.

- [ ] **Step 2: Run test to verify it fails**

Run: `bun test apps/websocket/src/mcp/__tests__/tools/refresh.test.ts`

Expected: FAIL

- [ ] **Step 3: Write minimal implementation**

Mirror Task 1, use `command: 'refresh'`, tool name `refresh`, description `"Refresh the current page."`, and return `'Page refreshed'`.

- [ ] **Step 4: Run test to verify it passes**

Run: `bun test apps/websocket/src/mcp/__tests__/tools/refresh.test.ts`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add apps/websocket/src/mcp/tools/refresh.ts apps/websocket/src/mcp/__tests__/tools/refresh.test.ts
git commit -m "feat(mcp): add refresh tool"
```

---

### Task 4: `tabList` tool

**Files:**
- Create: `apps/websocket/src/mcp/tools/tab-list.ts`
- Create: `apps/websocket/src/mcp/__tests__/tools/tab-list.test.ts`

- [ ] **Step 1: Write the failing test**

```typescript
import { describe, expect, it } from 'bun:test';
import type { Envelope } from '@browser-bridge/shared';
import { decode, encode } from '@browser-bridge/websocket/protocol';
import { createBrowserSessionStore } from '../../browser-session';
import { executeTabList } from '../../tools/tab-list';

describe('executeTabList', () => {
  it('returns tab list as JSON', async () => {
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
                  data: [
                    {
                      browserId: 'a',
                      userId: 'u',
                      status: 'online',
                      lastSeen: Date.now(),
                    },
                  ],
                }
              : {
                  status: 'ok',
                  data: [
                    { id: 1, title: 'One', url: 'https://one.example' },
                    { id: 2, title: 'Two', url: 'https://two.example' },
                  ],
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
    try {
      const result = await executeTabList(
        {
          sessionId: 's1',
          sessions,
          websocketUrl: `ws://127.0.0.1:${server.port}/ws`,
        },
        {},
      );

      expect(result).toContain('One');
      expect(result).toContain('Two');
    } finally {
      server.stop();
    }
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `bun test apps/websocket/src/mcp/__tests__/tools/tab-list.test.ts`

Expected: FAIL

- [ ] **Step 3: Write minimal implementation**

```typescript
import type { FastMCP } from 'fastmcp';
import { z } from 'zod';
import { resolveTargetBrowser } from '../browser-lookup';
import { sendCommand } from '../command-client';
import type { ServerContext, ToolContext } from '../tool-context';

export const TabListInputSchema = z.object({
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeTabList(
  context: ToolContext,
  args: z.infer<typeof TabListInputSchema>,
): Promise<string> {
  const timeoutMs =
    args.timeout_ms ??
    context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'tab:list',
    params: {},
    timeoutMs,
  });

  if (result.status !== 'ok') throw new Error(result.error ?? 'tab:list failed');
  return JSON.stringify(result.data ?? [], null, 2);
}

export function registerTabListTool(
  server: FastMCP,
  serverContext: ServerContext,
): void {
  server.addTool({
    name: 'tab_list',
    description: 'List all open tabs in the selected browser.',
    parameters: TabListInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeTabList(
        {
          sessionId: resolvedSessionId,
          sessions: serverContext.sessions,
          websocketUrl: serverContext.websocketUrl,
        },
        args,
      );
    },
  });
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `bun test apps/websocket/src/mcp/__tests__/tools/tab-list.test.ts`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add apps/websocket/src/mcp/tools/tab-list.ts apps/websocket/src/mcp/__tests__/tools/tab-list.test.ts
git commit -m "feat(mcp): add tab_list tool"
```

---

### Task 5: `tabNew` tool

**Files:**
- Create: `apps/websocket/src/mcp/tools/tab-new.ts`
- Create: `apps/websocket/src/mcp/__tests__/tools/tab-new.test.ts`

- [ ] **Step 1: Write the failing test**

Mirror Task 4 test, but call `executeTabNew({ url: 'https://new.example' })` and assert the result contains `'https://new.example'`.

- [ ] **Step 2: Run test to verify it fails**

Run: `bun test apps/websocket/src/mcp/__tests__/tools/tab-new.test.ts`

Expected: FAIL

- [ ] **Step 3: Write minimal implementation**

```typescript
import type { FastMCP } from 'fastmcp';
import { z } from 'zod';
import { resolveTargetBrowser } from '../browser-lookup';
import { sendCommand } from '../command-client';
import type { ServerContext, ToolContext } from '../tool-context';

export const TabNewInputSchema = z.object({
  url: z.string().url().optional(),
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeTabNew(
  context: ToolContext,
  args: z.infer<typeof TabNewInputSchema>,
): Promise<string> {
  const timeoutMs =
    args.timeout_ms ??
    context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'tab:new',
    params: args.url ? { url: args.url } : {},
    timeoutMs,
  });

  if (result.status !== 'ok') throw new Error(result.error ?? 'tab:new failed');
  return result.message ?? `Opened new tab${args.url ? ` to ${args.url}` : ''}`;
}

export function registerTabNewTool(
  server: FastMCP,
  serverContext: ServerContext,
): void {
  server.addTool({
    name: 'tab_new',
    description: 'Open a new tab, optionally navigating to a URL.',
    parameters: TabNewInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeTabNew(
        {
          sessionId: resolvedSessionId,
          sessions: serverContext.sessions,
          websocketUrl: serverContext.websocketUrl,
        },
        args,
      );
    },
  });
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `bun test apps/websocket/src/mcp/__tests__/tools/tab-new.test.ts`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add apps/websocket/src/mcp/tools/tab-new.ts apps/websocket/src/mcp/__tests__/tools/tab-new.test.ts
git commit -m "feat(mcp): add tab_new tool"
```

---

### Task 6: `tabClose` tool

**Files:**
- Create: `apps/websocket/src/mcp/tools/tab-close.ts`
- Create: `apps/websocket/src/mcp/__tests__/tools/tab-close.test.ts`

- [ ] **Step 1: Write the failing test**

Mirror Task 4, call `executeTabClose({ tabId: 2 })`, mock response message `'Closed tab 2'`, assert result contains `'Closed'`.

- [ ] **Step 2: Run test to verify it fails**

Run: `bun test apps/websocket/src/mcp/__tests__/tools/tab-close.test.ts`

Expected: FAIL

- [ ] **Step 3: Write minimal implementation**

```typescript
import type { FastMCP } from 'fastmcp';
import { z } from 'zod';
import { resolveTargetBrowser } from '../browser-lookup';
import { sendCommand } from '../command-client';
import type { ServerContext, ToolContext } from '../tool-context';

export const TabCloseInputSchema = z.object({
  tabId: z.number().int().min(0),
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeTabClose(
  context: ToolContext,
  args: z.infer<typeof TabCloseInputSchema>,
): Promise<string> {
  const timeoutMs =
    args.timeout_ms ??
    context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'tab:close',
    params: { tabId: args.tabId },
    timeoutMs,
  });

  if (result.status !== 'ok')
    throw new Error(result.error ?? 'tab:close failed');
  return result.message ?? `Closed tab ${args.tabId}`;
}

export function registerTabCloseTool(
  server: FastMCP,
  serverContext: ServerContext,
): void {
  server.addTool({
    name: 'tab_close',
    description: 'Close a tab by its ID.',
    parameters: TabCloseInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeTabClose(
        {
          sessionId: resolvedSessionId,
          sessions: serverContext.sessions,
          websocketUrl: serverContext.websocketUrl,
        },
        args,
      );
    },
  });
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `bun test apps/websocket/src/mcp/__tests__/tools/tab-close.test.ts`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add apps/websocket/src/mcp/tools/tab-close.ts apps/websocket/src/mcp/__tests__/tools/tab-close.test.ts
git commit -m "feat(mcp): add tab_close tool"
```

---

### Task 7: `tabSwitch` tool

**Files:**
- Create: `apps/websocket/src/mcp/tools/tab-switch.ts`
- Create: `apps/websocket/src/mcp/__tests__/tools/tab-switch.test.ts`

- [ ] **Step 1: Write the failing test**

Mirror Task 6, call `executeTabSwitch({ tabId: 1 })`, mock response message `'Switched to tab 1'`, assert result contains `'Switched'`.

- [ ] **Step 2: Run test to verify it fails**

Run: `bun test apps/websocket/src/mcp/__tests__/tools/tab-switch.test.ts`

Expected: FAIL

- [ ] **Step 3: Write minimal implementation**

Mirror Task 6, use `command: 'tab:switch'`, tool name `tab_switch`, description `"Switch to a tab by its ID."`, and return `Switched to tab ${args.tabId}`.

- [ ] **Step 4: Run test to verify it passes**

Run: `bun test apps/websocket/src/mcp/__tests__/tools/tab-switch.test.ts`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add apps/websocket/src/mcp/tools/tab-switch.ts apps/websocket/src/mcp/__tests__/tools/tab-switch.test.ts
git commit -m "feat(mcp): add tab_switch tool"
```

---

### Task 8: `select` tool

**Files:**
- Create: `apps/websocket/src/mcp/tools/select.ts`
- Create: `apps/websocket/src/mcp/__tests__/tools/select.test.ts`

- [ ] **Step 1: Write the failing test**

```typescript
import { describe, expect, it } from 'bun:test';
import type { Envelope } from '@browser-bridge/shared';
import { decode, encode } from '@browser-bridge/websocket/protocol';
import { createBrowserSessionStore } from '../../browser-session';
import { executeSelect } from '../../tools/select';

describe('executeSelect', () => {
  it('selects an option', async () => {
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
                  data: [
                    {
                      browserId: 'a',
                      userId: 'u',
                      status: 'online',
                      lastSeen: Date.now(),
                    },
                  ],
                }
              : { status: 'ok', message: 'Selected option' };
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
    try {
      const result = await executeSelect(
        {
          sessionId: 's1',
          sessions,
          websocketUrl: `ws://127.0.0.1:${server.port}/ws`,
        },
        { selector: '#color', value: 'blue' },
      );

      expect(result).toContain('Selected');
    } finally {
      server.stop();
    }
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `bun test apps/websocket/src/mcp/__tests__/tools/select.test.ts`

Expected: FAIL

- [ ] **Step 3: Write minimal implementation**

```typescript
import type { FastMCP } from 'fastmcp';
import { z } from 'zod';
import { resolveTargetBrowser } from '../browser-lookup';
import { sendCommand } from '../command-client';
import type { ServerContext, ToolContext } from '../tool-context';

export const SelectInputSchema = z.object({
  selector: z.string().min(1),
  value: z.string().min(1),
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeSelect(
  context: ToolContext,
  args: z.infer<typeof SelectInputSchema>,
): Promise<string> {
  const timeoutMs =
    args.timeout_ms ??
    context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'select',
    params: { selector: args.selector, value: args.value },
    timeoutMs,
  });

  if (result.status !== 'ok') throw new Error(result.error ?? 'select failed');
  return result.message ?? `Selected ${args.value} in ${args.selector}`;
}

export function registerSelectTool(
  server: FastMCP,
  serverContext: ServerContext,
): void {
  server.addTool({
    name: 'select',
    description: 'Select an option in a dropdown by CSS selector and value.',
    parameters: SelectInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeSelect(
        {
          sessionId: resolvedSessionId,
          sessions: serverContext.sessions,
          websocketUrl: serverContext.websocketUrl,
        },
        args,
      );
    },
  });
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `bun test apps/websocket/src/mcp/__tests__/tools/select.test.ts`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add apps/websocket/src/mcp/tools/select.ts apps/websocket/src/mcp/__tests__/tools/select.test.ts
git commit -m "feat(mcp): add select tool"
```

---

### Task 9: `scroll` tool

**Files:**
- Create: `apps/websocket/src/mcp/tools/scroll.ts`
- Create: `apps/websocket/src/mcp/__tests__/tools/scroll.test.ts`

- [ ] **Step 1: Write the failing test**

Mirror Task 8, call `executeScroll({ x: 0, y: 100 })`, mock message `'Scrolled'`, assert result contains `'Scrolled'`.

- [ ] **Step 2: Run test to verify it fails**

Run: `bun test apps/websocket/src/mcp/__tests__/tools/scroll.test.ts`

Expected: FAIL

- [ ] **Step 3: Write minimal implementation**

```typescript
import type { FastMCP } from 'fastmcp';
import { z } from 'zod';
import { resolveTargetBrowser } from '../browser-lookup';
import { sendCommand } from '../command-client';
import type { ServerContext, ToolContext } from '../tool-context';

export const ScrollInputSchema = z.object({
  selector: z.string().min(1).optional(),
  x: z.number().int(),
  y: z.number().int(),
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeScroll(
  context: ToolContext,
  args: z.infer<typeof ScrollInputSchema>,
): Promise<string> {
  const timeoutMs =
    args.timeout_ms ??
    context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'scroll',
    params: {
      selector: args.selector ?? 'page',
      x: args.x,
      y: args.y,
    },
    timeoutMs,
  });

  if (result.status !== 'ok') throw new Error(result.error ?? 'scroll failed');
  return result.message ?? `Scrolled by (${args.x}, ${args.y})`;
}

export function registerScrollTool(
  server: FastMCP,
  serverContext: ServerContext,
): void {
  server.addTool({
    name: 'scroll',
    description:
      'Scroll an element or the page by x,y pixels. Defaults to scrolling the page.',
    parameters: ScrollInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeScroll(
        {
          sessionId: resolvedSessionId,
          sessions: serverContext.sessions,
          websocketUrl: serverContext.websocketUrl,
        },
        args,
      );
    },
  });
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `bun test apps/websocket/src/mcp/__tests__/tools/scroll.test.ts`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add apps/websocket/src/mcp/tools/scroll.ts apps/websocket/src/mcp/__tests__/tools/scroll.test.ts
git commit -m "feat(mcp): add scroll tool"
```

---

### Task 10: `hover` tool

**Files:**
- Create: `apps/websocket/src/mcp/tools/hover.ts`
- Create: `apps/websocket/src/mcp/__tests__/tools/hover.test.ts`

- [ ] **Step 1: Write the failing test**

Mirror Task 8, call `executeHover({ selector: '#btn' })`, mock message `'Hovered'`, assert result contains `'Hovered'`.

- [ ] **Step 2: Run test to verify it fails**

Run: `bun test apps/websocket/src/mcp/__tests__/tools/hover.test.ts`

Expected: FAIL

- [ ] **Step 3: Write minimal implementation**

Mirror Task 8 `click.ts` pattern, use `command: 'hover'`, tool name `hover`, description `"Hover over an element by CSS selector."`, params `{ selector: args.selector }`, return `Hovered ${args.selector}`.

- [ ] **Step 4: Run test to verify it passes**

Run: `bun test apps/websocket/src/mcp/__tests__/tools/hover.test.ts`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add apps/websocket/src/mcp/tools/hover.ts apps/websocket/src/mcp/__tests__/tools/hover.test.ts
git commit -m "feat(mcp): add hover tool"
```

---

### Task 11: `getText` tool

**Files:**
- Create: `apps/websocket/src/mcp/tools/get-text.ts`
- Create: `apps/websocket/src/mcp/__tests__/tools/get-text.test.ts`

- [ ] **Step 1: Write the failing test**

```typescript
import { describe, expect, it } from 'bun:test';
import type { Envelope } from '@browser-bridge/shared';
import { decode, encode } from '@browser-bridge/websocket/protocol';
import { createBrowserSessionStore } from '../../browser-session';
import { executeGetText } from '../../tools/get-text';

describe('executeGetText', () => {
  it('returns element text', async () => {
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
                  data: [
                    {
                      browserId: 'a',
                      userId: 'u',
                      status: 'online',
                      lastSeen: Date.now(),
                    },
                  ],
                }
              : { status: 'ok', data: 'Hello world' };
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
    try {
      const result = await executeGetText(
        {
          sessionId: 's1',
          sessions,
          websocketUrl: `ws://127.0.0.1:${server.port}/ws`,
        },
        { selector: '#msg' },
      );

      expect(result).toContain('Hello world');
    } finally {
      server.stop();
    }
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `bun test apps/websocket/src/mcp/__tests__/tools/get-text.test.ts`

Expected: FAIL

- [ ] **Step 3: Write minimal implementation**

```typescript
import type { FastMCP } from 'fastmcp';
import { z } from 'zod';
import { resolveTargetBrowser } from '../browser-lookup';
import { sendCommand } from '../command-client';
import type { ServerContext, ToolContext } from '../tool-context';

export const GetTextInputSchema = z.object({
  selector: z.string().min(1),
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeGetText(
  context: ToolContext,
  args: z.infer<typeof GetTextInputSchema>,
): Promise<string> {
  const timeoutMs =
    args.timeout_ms ??
    context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'gettext',
    params: { selector: args.selector },
    timeoutMs,
  });

  if (result.status !== 'ok')
    throw new Error(result.error ?? 'gettext failed');
  return String(result.data ?? '');
}

export function registerGetTextTool(
  server: FastMCP,
  serverContext: ServerContext,
): void {
  server.addTool({
    name: 'get_text',
    description: 'Get the text content of an element by CSS selector.',
    parameters: GetTextInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeGetText(
        {
          sessionId: resolvedSessionId,
          sessions: serverContext.sessions,
          websocketUrl: serverContext.websocketUrl,
        },
        args,
      );
    },
  });
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `bun test apps/websocket/src/mcp/__tests__/tools/get-text.test.ts`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add apps/websocket/src/mcp/tools/get-text.ts apps/websocket/src/mcp/__tests__/tools/get-text.test.ts
git commit -m "feat(mcp): add get_text tool"
```

---

### Task 12: `getHtml` tool

**Files:**
- Create: `apps/websocket/src/mcp/tools/get-html.ts`
- Create: `apps/websocket/src/mcp/__tests__/tools/get-html.test.ts`

- [ ] **Step 1: Write the failing test**

Mirror Task 11, call `executeGetHtml({ selector: '#msg' })`, mock data `'<p>Hello</p>'`, assert result contains `'<p>Hello</p>'`.

- [ ] **Step 2: Run test to verify it fails**

Run: `bun test apps/websocket/src/mcp/__tests__/tools/get-html.test.ts`

Expected: FAIL

- [ ] **Step 3: Write minimal implementation**

Mirror Task 11, use `command: 'gethtml'`, tool name `get_html`, description `"Get the inner HTML of an element by CSS selector."`, and return `String(result.data ?? '')`.

- [ ] **Step 4: Run test to verify it passes**

Run: `bun test apps/websocket/src/mcp/__tests__/tools/get-html.test.ts`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add apps/websocket/src/mcp/tools/get-html.ts apps/websocket/src/mcp/__tests__/tools/get-html.test.ts
git commit -m "feat(mcp): add get_html tool"
```

---

### Task 13: `waitElement` tool

**Files:**
- Create: `apps/websocket/src/mcp/tools/wait-element.ts`
- Create: `apps/websocket/src/mcp/__tests__/tools/wait-element.test.ts`

- [ ] **Step 1: Write the failing test**

```typescript
import { describe, expect, it } from 'bun:test';
import type { Envelope } from '@browser-bridge/shared';
import { decode, encode } from '@browser-bridge/websocket/protocol';
import { createBrowserSessionStore } from '../../browser-session';
import { executeWaitElement } from '../../tools/wait-element';

describe('executeWaitElement', () => {
  it('waits for an element', async () => {
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
                  data: [
                    {
                      browserId: 'a',
                      userId: 'u',
                      status: 'online',
                      lastSeen: Date.now(),
                    },
                  ],
                }
              : { status: 'ok', message: 'Element appeared' };
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
    try {
      const result = await executeWaitElement(
        {
          sessionId: 's1',
          sessions,
          websocketUrl: `ws://127.0.0.1:${server.port}/ws`,
        },
        { selector: '#target' },
      );

      expect(result).toContain('appeared');
    } finally {
      server.stop();
    }
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `bun test apps/websocket/src/mcp/__tests__/tools/wait-element.test.ts`

Expected: FAIL

- [ ] **Step 3: Write minimal implementation**

```typescript
import type { FastMCP } from 'fastmcp';
import { z } from 'zod';
import { resolveTargetBrowser } from '../browser-lookup';
import { sendCommand } from '../command-client';
import type { ServerContext, ToolContext } from '../tool-context';

export const WaitElementInputSchema = z.object({
  selector: z.string().min(1),
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeWaitElement(
  context: ToolContext,
  args: z.infer<typeof WaitElementInputSchema>,
): Promise<string> {
  const timeoutMs =
    args.timeout_ms ??
    context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'wait:element',
    params: { selector: args.selector, timeout: timeoutMs },
    timeoutMs,
  });

  if (result.status !== 'ok')
    throw new Error(result.error ?? 'wait:element failed');
  return result.message ?? `Element ${args.selector} appeared`;
}

export function registerWaitElementTool(
  server: FastMCP,
  serverContext: ServerContext,
): void {
  server.addTool({
    name: 'wait_element',
    description: 'Wait for an element to appear in the DOM.',
    parameters: WaitElementInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeWaitElement(
        {
          sessionId: resolvedSessionId,
          sessions: serverContext.sessions,
          websocketUrl: serverContext.websocketUrl,
        },
        args,
      );
    },
  });
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `bun test apps/websocket/src/mcp/__tests__/tools/wait-element.test.ts`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add apps/websocket/src/mcp/tools/wait-element.ts apps/websocket/src/mcp/__tests__/tools/wait-element.test.ts
git commit -m "feat(mcp): add wait_element tool"
```

---

### Task 14: `waitNavigation` tool

**Files:**
- Create: `apps/websocket/src/mcp/tools/wait-navigation.ts`
- Create: `apps/websocket/src/mcp/__tests__/tools/wait-navigation.test.ts`

- [ ] **Step 1: Write the failing test**

Mirror Task 13, call `executeWaitNavigation({})`, mock message `'Navigation complete'`, assert result contains `'Navigation'`.

- [ ] **Step 2: Run test to verify it fails**

Run: `bun test apps/websocket/src/mcp/__tests__/tools/wait-navigation.test.ts`

Expected: FAIL

- [ ] **Step 3: Write minimal implementation**

Mirror Task 13, use `command: 'wait:navigation'`, tool name `wait_navigation`, description `"Wait for the current page navigation to complete."`, params `{ timeout: timeoutMs }`, return `Navigation complete`.

- [ ] **Step 4: Run test to verify it passes**

Run: `bun test apps/websocket/src/mcp/__tests__/tools/wait-navigation.test.ts`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add apps/websocket/src/mcp/tools/wait-navigation.ts apps/websocket/src/mcp/__tests__/tools/wait-navigation.test.ts
git commit -m "feat(mcp): add wait_navigation tool"
```

---

### Task 15: Register all new tools in `server.ts`

**Files:**
- Modify: `apps/websocket/src/mcp/server.ts`

- [ ] **Step 1: Add imports and registrations**

Add these imports after the existing tool imports:

```typescript
import { registerGetHtmlTool } from './tools/get-html';
import { registerGetTextTool } from './tools/get-text';
import { registerGoBackTool } from './tools/go-back';
import { registerGoForwardTool } from './tools/go-forward';
import { registerHoverTool } from './tools/hover';
import { registerRefreshTool } from './tools/refresh';
import { registerScrollTool } from './tools/scroll';
import { registerSelectTool } from './tools/select';
import { registerTabCloseTool } from './tools/tab-close';
import { registerTabListTool } from './tools/tab-list';
import { registerTabNewTool } from './tools/tab-new';
import { registerTabSwitchTool } from './tools/tab-switch';
import { registerWaitElementTool } from './tools/wait-element';
import { registerWaitNavigationTool } from './tools/wait-navigation';
```

Add these registrations inside `startMcpServer` after the existing registrations:

```typescript
registerGoBackTool(server, serverContext);
registerGoForwardTool(server, serverContext);
registerRefreshTool(server, serverContext);
registerTabListTool(server, serverContext);
registerTabNewTool(server, serverContext);
registerTabCloseTool(server, serverContext);
registerTabSwitchTool(server, serverContext);
registerSelectTool(server, serverContext);
registerScrollTool(server, serverContext);
registerHoverTool(server, serverContext);
registerGetTextTool(server, serverContext);
registerGetHtmlTool(server, serverContext);
registerWaitElementTool(server, serverContext);
registerWaitNavigationTool(server, serverContext);
```

- [ ] **Step 2: Run type check and tests**

Run:

```bash
bun run type-check
bun test apps/websocket/src/mcp
```

Expected: type-check clean; all MCP tests pass.

- [ ] **Step 3: Commit**

```bash
git add apps/websocket/src/mcp/server.ts
git commit -m "feat(mcp): register all new CLI-aligned tools"
```

---

### Task 16: Add `fullPage` parameter to `screenshot` tool

**Files:**
- Modify: `apps/websocket/src/mcp/tools/screenshot.ts`
- Modify: `apps/websocket/src/mcp/__tests__/tools/screenshot.test.ts`

The design spec documents a `fullPage?: boolean` parameter for `screenshot`, but the initial implementation omitted it.

- [ ] **Step 1: Update the test to assert fullPage is forwarded**

In `screenshot.test.ts`, update the `executeScreenshot` call to pass `{ fullPage: true }` and assert that the command params received by the mock include `fullPage: true`. The simplest way is to capture the last command params in the mock and assert after `executeScreenshot` returns.

Example:

```typescript
let lastCommandParams: Record<string, unknown> | undefined;
// inside message(ws, data):
if (envelope.type === 'command') {
  lastCommandParams = envelope.payload;
}
```

Then after the call:

```typescript
expect(lastCommandParams).toEqual({ fullPage: true });
```

Run the test; it should FAIL because the implementation does not accept `fullPage`.

- [ ] **Step 2: Update the implementation**

```typescript
export const ScreenshotInputSchema = z.object({
  fullPage: z.boolean().optional(),
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});
```

Forward the param:

```typescript
const result = await sendCommand({
  serverUrl: context.websocketUrl,
  browserId: resolution.browserId,
  command: 'screenshot',
  params: args.fullPage === undefined ? {} : { fullPage: args.fullPage },
  timeoutMs,
});
```

- [ ] **Step 3: Run test**

Run: `bun test apps/websocket/src/mcp/__tests__/tools/screenshot.test.ts`

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add apps/websocket/src/mcp/tools/screenshot.ts apps/websocket/src/mcp/__tests__/tools/screenshot.test.ts
git commit -m "feat(mcp): add fullPage option to screenshot tool"
```

---

### Task 17: Update MCP setup documentation

**Files:**
- Modify: `docs/mcp-setup.md`

- [ ] **Step 1: Add the new tools to the tool reference table**

Insert new rows after the existing tool reference table so it reads:

| Tool | Description | Parameters |
|---|---|---|
| `list_browsers` | List connected browsers | — |
| `set_browser` | Pin a browser for this session | `browserId: string` |
| `navigate` | Navigate active tab to URL | `url: string`, `timeout_ms?: number` |
| `go_back` | Go back in browser history | `timeout_ms?: number` |
| `go_forward` | Go forward in browser history | `timeout_ms?: number` |
| `refresh` | Refresh the current page | `timeout_ms?: number` |
| `tab_list` | List all open tabs | `timeout_ms?: number` |
| `tab_new` | Open a new tab | `url?: string`, `timeout_ms?: number` |
| `tab_close` | Close a tab by ID | `tabId: number`, `timeout_ms?: number` |
| `tab_switch` | Switch to a tab by ID | `tabId: number`, `timeout_ms?: number` |
| `click` | Click an element | `selector: string`, `timeout_ms?: number` |
| `type` | Type text into an input | `selector: string`, `text: string`, `submit?: boolean`, `timeout_ms?: number` |
| `select` | Select a dropdown option | `selector: string`, `value: string`, `timeout_ms?: number` |
| `scroll` | Scroll element or page | `x: number`, `y: number`, `selector?: string`, `timeout_ms?: number` |
| `hover` | Hover over an element | `selector: string`, `timeout_ms?: number` |
| `get_text` | Get element text content | `selector: string`, `timeout_ms?: number` |
| `get_html` | Get element inner HTML | `selector: string`, `timeout_ms?: number` |
| `screenshot` | Capture base64 PNG screenshot | `fullPage?: boolean`, `timeout_ms?: number` |
| `pageinfo` | Get title, URL, and tabs | `timeout_ms?: number` |
| `wait_element` | Wait for an element to appear | `selector: string`, `timeout_ms?: number` |
| `wait_navigation` | Wait for navigation to complete | `timeout_ms?: number` |

- [ ] **Step 2: Commit**

```bash
git add docs/mcp-setup.md
git commit -m "docs(mcp): document all CLI-aligned tools"
```

---

### Task 18: Final verification

- [ ] **Step 1: Run full test suite**

Run: `bun run test`

Expected: all tests pass.

- [ ] **Step 2: Run type check**

Run: `bun run type-check`

Expected: clean.

- [ ] **Step 3: Run Biome**

Run: `bunx @biomejs/biome check --write .`

Expected: no diagnostics after fixes.

- [ ] **Step 4: Check test coverage**

Run: `bun test --coverage apps/websocket/src/mcp`

Expected: 80%+ coverage for new MCP code.

- [ ] **Step 5: Commit any formatting fixes**

```bash
git add .
git commit -m "chore(mcp): apply Biome formatting fixes" || echo "No changes to commit"
```

---

## Self-Review

**1. Spec coverage:** Every CLI command except the reserved `bridge-host` (explicitly marked "not yet implemented") now has a corresponding MCP tool. The `screenshot` tool is updated to match the design spec's `fullPage` parameter.

**2. Placeholder scan:** No TBD/TODO placeholders. Each task contains concrete code, exact commands, and expected outcomes.

**3. Type consistency:** All tools use `CommandType` values exactly as defined in `packages/shared/src/types.ts`. Tool names use underscores to match MCP naming conventions. Param names (`selector`, `value`, `tabId`, `x`, `y`, `url`, `timeout_ms`, `fullPage`) match the CLI's parameter names where applicable.

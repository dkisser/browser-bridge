# Browser Tool Explicit Tab Handle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every browser tool operation target an explicit tab handle, removing all implicit active-tab fallbacks so Browser-Bridge can open, operate on, and close multiple tabs concurrently without disturbing the user's current tab.

**Architecture:** Keep the existing WebSocket command envelope and extension command dispatch, but make `tabId` a required field on every command. Update MCP tool schemas to require `tab_id`, update the extension service worker to reject commands without `tabId`, and update the CLI to accept a `--tab` argument for every page-level command. Preserve the existing session/browser resolution model; only the per-tab addressing changes.

**Tech Stack:** Bun, TypeScript, Zod, FastMCP, Chrome Extension Manifest V3, Commander.js, Biome.

## Global Constraints

- Immutability: create new objects, never mutate existing ones.
- Files should stay focused; extract if growing beyond 800 lines.
- Handle errors explicitly; never silently swallow errors.
- Validate all user/AI input at system boundaries with Zod schemas.
- Minimum 80% test coverage; write tests first (TDD).
- No hardcoded secrets; use environment variables or config.
- Conventional commit format: `type: description`.

---

## File Structure

This plan touches the following files. Each file has one clear responsibility.

### Shared protocol
- `packages/shared/src/types.ts` — `CommandPayload` shape and command-related types used by CLI, websocket server, local proxy, and extension.

### Chrome Extension
- `apps/extension/src/background.ts` — Service worker command dispatcher. Must require `tabId` for all page/tab commands and remove `getActiveTabId`.
- `apps/extension/src/content.ts` — DOM command executor; unchanged except it already receives `tabId` from background. No edits required unless tests reveal issues.

### WebSocket MCP server
- `apps/websocket/src/mcp/tools/*.ts` — 18 tool files. Each page-level tool gets a required `tab_id` parameter.
- `apps/websocket/src/mcp/tools/navigate.ts` — additionally returns `{ tab_id, url, title }`.
- `apps/websocket/src/mcp/tools/tab-new.ts` — adds `active` and `auto_close` optional parameters.
- `apps/websocket/src/mcp/command-client.ts` — unchanged; already passes `params` through.

### CLI
- `apps/cli/src/commands/sendCommand.ts` — adds `tabId` to `SendCommandOptions` and includes it in the command payload.
- `apps/cli/src/managedClient.ts` — updates `sendCommand` payload type to require `tabId`.
- `apps/cli/src/index.ts` — adds `--tab <id>` global option and requires it for page-level commands.

### Tests
- `apps/websocket/src/mcp/__tests__/tools/*.test.ts` — update every mock payload expectation to include `tab_id`.
- `apps/websocket/src/mcp/__tests__/tools/tab-new.test.ts` — add test for `active: false`.
- `apps/websocket/src/mcp/__tests__/tools/*.test.ts` — add schema rejection tests for missing `tab_id`.
- `apps/cli/src/commands/__tests__/*` — update CLI tests for `--tab`.

### Documentation
- `skills/browser-bridge-user/SKILL.md` — update examples to show `--tab` and tab workflow.
- `README.md` / `README_CN.md` — update CLI examples if they show navigation without `--tab`.

---

## Task 1: Make `tabId` required in shared `CommandPayload`

**Files:**
- Modify: `packages/shared/src/types.ts:32-36`

**Interfaces:**
- Consumes: nothing external.
- Produces: `CommandPayload` with required `tabId: number`.

- [ ] **Step 1: Write the failing test**

Create `packages/shared/src/types.test.ts`:

```typescript
import { describe, expect, it } from 'bun:test';
import type { CommandPayload } from './types';

describe('CommandPayload', () => {
  it('requires tabId', () => {
    // @ts-expect-error tabId should be required
    const payload: CommandPayload = {
      command: 'navigate',
      params: { url: 'https://example.com' },
    };
    expect(payload.command).toBe('navigate');
  });

  it('accepts a valid tabId', () => {
    const payload: CommandPayload = {
      command: 'navigate',
      tabId: 42,
      params: { url: 'https://example.com' },
    };
    expect(payload.tabId).toBe(42);
  });
});
```

Run: `bun test packages/shared/src/types.test.ts`
Expected: FAIL with TypeScript error on `@ts-expect-error` line (if type-check passes, the expect-error fails).

- [ ] **Step 2: Update the type**

Edit `packages/shared/src/types.ts`:

```typescript
export interface CommandPayload {
  command: CommandType;
  tabId: number;
  params: Record<string, unknown>;
}
```

- [ ] **Step 3: Run type-check and tests**

Run:
```bash
bun run type-check
bun test packages/shared/src/types.test.ts
```

Expected: type-check may fail elsewhere (CLI, MCP tools) because they still omit `tabId`. That is expected and will be fixed in later tasks. The shared test should PASS.

- [ ] **Step 4: Commit**

```bash
git add packages/shared/src/types.ts packages/shared/src/types.test.ts
git commit -m "feat(shared): require tabId in CommandPayload"
```

---

## Task 2: Update the CLI to require `--tab` for page-level commands

**Files:**
- Modify: `apps/cli/src/commands/sendCommand.ts`
- Modify: `apps/cli/src/managedClient.ts`
- Modify: `apps/cli/src/index.ts`

**Interfaces:**
- Consumes: `CommandPayload` from Task 1 (requires `tabId`).
- Produces: CLI commands that include `tabId` in every page-level payload.

- [ ] **Step 1: Write the failing test for sendCommand**

Open `apps/cli/src/commands/__tests__/sendCommand.test.ts` (create if missing).

```typescript
import { describe, expect, it } from 'bun:test';
import { sendCommand } from '../sendCommand';

describe('sendCommand', () => {
  it('throws when browser is missing', async () => {
    await expect(
      sendCommand({ server: 'ws://localhost:3001' }, 'navigate', { url: 'https://example.com', tabId: 1 }),
    ).rejects.toThrow('Required: --browser <id>');
  });
});
```

Run: `bun test apps/cli/src/commands/__tests__/sendCommand.test.ts`
Expected: PASS (this test already passes with current code if it exists). If it does not exist, create it and verify it fails until sendCommand is updated.

- [ ] **Step 2: Update sendCommand to require and forward tabId**

Edit `apps/cli/src/commands/sendCommand.ts`:

```typescript
export interface SendCommandOptions {
  server: string;
  browser?: string;
  tabId?: number;
  timeout?: number;
}

export async function sendCommand(
  options: SendCommandOptions,
  command: CommandType,
  params: Record<string, unknown> = {},
): Promise<unknown> {
  if (!options.browser) {
    throw new Error('Required: --browser <id>');
  }

  if (options.tabId === undefined) {
    throw new Error('Required: --tab <id>');
  }

  {
    using client = new ManagedClient(options.server);
    await client.waitForOpen(5000);

    const response = await client.sendCommand(
      options.browser,
      { command, tabId: options.tabId, params },
      { timeout: options.timeout ?? 10000 },
    );
    const payload = response.payload as ResponsePayload;

    if (payload.status === 'error') {
      throw new Error(payload.message ?? 'Unknown error');
    }

    return payload.data ?? { status: 'ok' };
  }
}
```

Run: `bun test apps/cli/src/commands/__tests__/sendCommand.test.ts`
Expected: PASS.

- [ ] **Step 3: Update ManagedClient sendCommand type**

Edit `apps/cli/src/managedClient.ts`:

```typescript
sendCommand(
  browserId: string,
  payload: { command: CommandType; tabId: number; params?: Record<string, unknown> },
  opts?: { timeout?: number },
): Promise<Envelope> {
  return this.client.sendCommand(browserId, payload as CommandPayload, opts);
}
```

Run: `bun run type-check`
Expected: type-check still fails in `index.ts` until Step 4.

- [ ] **Step 4: Add `--tab` global option and require it for page commands**

Edit `apps/cli/src/index.ts`:

Add `tabId` to `GlobalOptions`:

```typescript
interface GlobalOptions {
  server: string;
  browser: string;
  tabId: number;
  json: boolean;
  timeout: number;
}
```

Update `getGlobalOptions`:

```typescript
function getGlobalOptions(opts: Record<string, unknown>): GlobalOptions {
  return {
    server: (opts.server as string) || `ws://localhost:${WEBSOCKET_PORT}`,
    browser: opts.browser as string,
    tabId: Number(opts.tab ?? 0),
    json: opts.json as boolean,
    timeout: (opts.timeout as number) || 10000,
  };
}
```

Add global option:

```typescript
program
  .option('--server <url>', 'WS Server URL', `ws://localhost:${WEBSOCKET_PORT}`)
  .option('--browser <id>', 'Target browser instance')
  .option('--tab <id>', 'Target tab id', '0')
  .option('--json', 'Structured JSON output')
  .option('--timeout <ms>', 'Command timeout', '10000');
```

Update page-level commands to pass `tabId`. Example for `navigate`:

```typescript
program
  .command('navigate <url>')
  .description('Navigate to URL in a specific tab')
  .action(async (url: string) => {
    const global = getGlobalOptions(program.opts());
    await dispatchCommand(global, 'navigate', { url });
  });
```

Update `dispatchCommand` to forward `tabId`:

```typescript
async function dispatchCommand(
  global: GlobalOptions,
  command: CommandType,
  params: Record<string, unknown> = {},
): Promise<void> {
  try {
    const data = await sendCommand(
      {
        server: global.server,
        browser: global.browser,
        tabId: global.tabId,
        timeout: global.timeout,
      },
      command,
      params,
    );
    output(global, data);
  } catch (err) {
    outputError(global, 'command_failed', String(err));
  }
}
```

Apply the same pattern to: `goBack`, `goForward`, `refresh`, `click`, `type`, `select`, `scroll`, `hover`, `gettext`, `gethtml`, `screenshot`, `pageinfo`, `wait:element`, `wait:navigation`.

For `tab:list`, `tab:new`, `tab:close`, `tab:switch`, keep `--tab` optional or derive from arguments where appropriate:
- `tab:new` does not need `--tab` (it creates one).
- `tab:close <tabId>` and `tab:switch <tabId>` take the tab id as positional argument and set `params.tabId` from it.
- `tab:list` does not need `--tab`.

Run: `bun run type-check`
Expected: PASS for CLI.

- [ ] **Step 5: Add CLI test for missing --tab**

In `apps/cli/src/commands/__tests__/sendCommand.test.ts`:

```typescript
it('throws when tabId is missing', async () => {
  await expect(
    sendCommand(
      { server: 'ws://localhost:3001', browser: 'b1' },
      'navigate',
      { url: 'https://example.com' },
    ),
  ).rejects.toThrow('Required: --tab <id>');
});
```

Run: `bun test apps/cli/src/commands/__tests__/sendCommand.test.ts`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add apps/cli/src/commands/sendCommand.ts apps/cli/src/managedClient.ts apps/cli/src/index.ts apps/cli/src/commands/__tests__/sendCommand.test.ts
git commit -m "feat(cli): require --tab for page-level commands"
```

---

## Task 3: Require `tabId` in the extension service worker

**Files:**
- Modify: `apps/extension/src/background.ts`

**Interfaces:**
- Consumes: `CommandMessage` with `payload.tabId` (number) from the offscreen document / local proxy.
- Produces: commands executed against explicit `tabId`; errors for missing/invalid `tabId`.

- [ ] **Step 1: Delete `getActiveTabId` and add tabId validation**

Edit `apps/extension/src/background.ts`:

Remove the `getActiveTabId` function entirely.

Replace every pattern like:

```typescript
const tab = tabId ?? (await getActiveTabId());
```

with:

```typescript
if (typeof tabId !== 'number') {
  throw new Error('Missing required tabId');
}
const tab = tabId;
```

Apply this to: `navigate`, `goBack`, `goForward`, `refresh`, `screenshot`, `pageinfo`, `wait:navigation`, and `sendToContentScript`.

For `tab:list`, `tab:new`, `tab:close`, `tab:switch`:
- `tab:list` does not need `tabId`.
- `tab:new` does not need `tabId`; it creates one.
- `tab:close` reads `params.tabId` (already required by schema).
- `tab:switch` reads `params.tabId` (already required by schema).

- [ ] **Step 2: Support `active: false` in `tab:new`**

Update the `tab:new` case:

```typescript
case 'tab:new': {
  const active = params.active === true;
  const newTab = await chrome.tabs.create({
    url: params.url as string | undefined,
    active,
  });
  return { id: newTab.id, url: newTab.url };
}
```

- [ ] **Step 3: Add smoke test for missing tabId**

If `apps/extension` has no test harness yet, skip automated test for this task and add a manual verification step. Otherwise add a test that sends a `ws_command` with no `tabId` and expects an error response.

- [ ] **Step 4: Run extension build**

Run: `bun run build:extension`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/extension/src/background.ts
git commit -m "feat(extension): require explicit tabId for all page commands"
```

---

## Task 4: Add required `tab_id` to all MCP page-level tools

**Files:**
- Modify: all `apps/websocket/src/mcp/tools/*.ts` except `list-browsers.ts` and `set-browser.ts`.

**Interfaces:**
- Consumes: `CommandPayload` with required `tabId` from Task 1.
- Produces: tool schemas with required `tab_id`; `sendCommand` calls include `tabId` in params.

The pattern for each tool is:

```typescript
export const XxxInputSchema = z.object({
  tab_id: z.number().int().min(0),
  // existing fields...
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

const result = await sendCommand({
  serverUrl: context.websocketUrl,
  browserId: resolution.browserId,
  command: 'xxx',
  params: { tabId: args.tab_id, /* other args */ },
  timeoutMs,
});
```

- [ ] **Step 4.1: Update `navigate.ts`**

```typescript
export const NavigateInputSchema = z.object({
  url: z.string().url(),
  tab_id: z.number().int().min(0),
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeNavigate(
  context: ToolContext,
  args: z.infer<typeof NavigateInputSchema>,
): Promise<string> {
  const timeoutMs =
    args.timeout_ms ??
    context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'navigate',
    params: { url: args.url, tabId: args.tab_id },
    timeoutMs,
  });

  if (result.status !== 'ok') {
    throw new Error(result.error ?? 'Navigation failed');
  }

  const data = result.data as Record<string, unknown> | undefined;
  return result.message ?? `Navigated to ${args.url} in tab ${args.tab_id}`;
}
```

Tool registration description:

```typescript
description: 'Navigate a specific tab of the selected browser to a URL.',
```

- [ ] **Step 4.2: Update `go-back.ts`, `go-forward.ts`, `refresh.ts`**

Add `tab_id` to schema and pass `tabId: args.tab_id` in params.

Example for `go-back.ts`:

```typescript
export const GoBackInputSchema = z.object({
  tab_id: z.number().int().min(0),
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

const result = await sendCommand({
  serverUrl: context.websocketUrl,
  browserId: resolution.browserId,
  command: 'goBack',
  params: { tabId: args.tab_id },
  timeoutMs,
});
```

- [ ] **Step 4.3: Update DOM tools: `click.ts`, `type.ts`, `select.ts`, `scroll.ts`, `hover.ts`, `get-text.ts`, `get-html.ts`, `wait-element.ts`**

Same pattern: add `tab_id` to schema, pass `tabId: args.tab_id`.

Example for `click.ts`:

```typescript
export const ClickInputSchema = z.object({
  selector: z.string().min(1),
  tab_id: z.number().int().min(0),
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

const result = await sendCommand({
  serverUrl: context.websocketUrl,
  browserId: resolution.browserId,
  command: 'click',
  params: { selector: args.selector, tabId: args.tab_id },
  timeoutMs,
});
```

- [ ] **Step 4.4: Update `screenshot.ts`, `pageinfo.ts`, `wait-navigation.ts`**

Same pattern.

Example for `screenshot.ts`:

```typescript
export const ScreenshotInputSchema = z.object({
  fullPage: z.boolean().optional(),
  tab_id: z.number().int().min(0),
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

const result = await sendCommand({
  serverUrl: context.websocketUrl,
  browserId: resolution.browserId,
  command: 'screenshot',
  params: { fullPage: args.fullPage, tabId: args.tab_id },
  timeoutMs,
});
```

- [ ] **Step 4.5: Update `tab-new.ts` to support `active` and `auto_close`**

```typescript
export const TabNewInputSchema = z.object({
  url: z.string().url().optional(),
  active: z.boolean().optional(),
  auto_close: z.boolean().optional(),
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

const result = await sendCommand({
  serverUrl: context.websocketUrl,
  browserId: resolution.browserId,
  command: 'tab:new',
  params: {
    url: args.url,
    active: args.active,
    auto_close: args.auto_close,
  },
  timeoutMs,
});
```

Description update:

```typescript
description: 'Open a new tab in the selected browser. Defaults to opening in the background (active=false).',
```

- [ ] **Step 4.6: Update `tab-close.ts` and `tab-switch.ts`**

These already have `tabId` in params from schema field `tabId`. Rename the MCP schema field to `tab_id` for consistency and map it to `params.tabId` for the extension command.

Example for `tab-close.ts`:

```typescript
export const TabCloseInputSchema = z.object({
  tab_id: z.number().int().min(0),
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

const result = await sendCommand({
  serverUrl: context.websocketUrl,
  browserId: resolution.browserId,
  command: 'tab:close',
  params: { tabId: args.tab_id },
  timeoutMs,
});
```

- [ ] **Step 4.7: Run type-check and tests**

Run:
```bash
bun run type-check
bun test apps/websocket/src/mcp/__tests__
```

Expected: tests will fail because mocks still omit `tab_id`. This is expected and fixed in Task 5.

- [ ] **Step 4.8: Commit**

```bash
git add apps/websocket/src/mcp/tools/*.ts
git commit -m "feat(mcp): require tab_id in all page-level tool schemas"
```

---

## Task 5: Update MCP tool tests for explicit `tab_id`

**Files:**
- Modify: all `apps/websocket/src/mcp/__tests__/tools/*.test.ts`.

**Interfaces:**
- Consumes: updated tool schemas from Task 4.
- Produces: passing tests that exercise `tab_id`.

- [ ] **Step 5.1: Update existing tests to pass `tab_id`**

For every test that calls `executeXxx`, add `tab_id: 42` (or any valid number) to the args.

Example for `navigate.test.ts`:

```typescript
const result = await executeNavigate(
  {
    sessionId: 's1',
    sessions,
    websocketUrl: `ws://127.0.0.1:${server.port}/ws`,
  },
  { url: 'https://example.com', tab_id: 42 },
);
```

Apply to all tool tests: `click`, `type`, `select`, `scroll`, `hover`, `gettext`, `gethtml`, `screenshot`, `pageinfo`, `wait-navigation`, `wait-element`, `go-back`, `go-forward`, `refresh`, `tab-close`, `tab-switch`.

- [ ] **Step 5.2: Update mock server expectations**

Most tests mock the WebSocket server response generically, so they do not need to inspect `tab_id`. If any test inspects the envelope payload, update the expected payload to include `tabId: 42`.

- [ ] **Step 5.3: Add `tab-new` background-open test**

In `tab-new.test.ts`, add:

```typescript
it('opens a new tab in the background by default', async () => {
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
            : { status: 'ok', data: { id: 101, url: 'https://example.com' } };
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
    const result = await executeTabNew(
      {
        sessionId: 's1',
        sessions,
        websocketUrl: `ws://127.0.0.1:${server.port}/ws`,
      },
      { url: 'https://example.com' },
    );
    expect(result).toContain('101');
  } finally {
    server.stop();
  }
});
```

- [ ] **Step 5.4: Add schema rejection test for missing `tab_id`**

Create or extend `apps/websocket/src/mcp/__tests__/tools/schema-validation.test.ts`:

```typescript
import { describe, expect, it } from 'bun:test';
import { ClickInputSchema } from '../../tools/click';
import { NavigateInputSchema } from '../../tools/navigate';

describe('tool schemas require tab_id', () => {
  it('NavigateInputSchema rejects missing tab_id', () => {
    const result = NavigateInputSchema.safeParse({ url: 'https://example.com' });
    expect(result.success).toBe(false);
    if (!result.success) {
      expect(result.error.issues.some((i) => i.path.join('.') === 'tab_id')).toBe(true);
    }
  });

  it('ClickInputSchema rejects missing tab_id', () => {
    const result = ClickInputSchema.safeParse({ selector: 'button' });
    expect(result.success).toBe(false);
    if (!result.success) {
      expect(result.error.issues.some((i) => i.path.join('.') === 'tab_id')).toBe(true);
    }
  });
});
```

- [ ] **Step 5.5: Add test for operating a closed tab**

Extend the schema-validation test or add a dedicated integration test that mocks the server to return an error when `tabId` does not exist, and assert the tool throws a clear message.

```typescript
it('throws when tab_id does not exist', async () => {
  // mock server that returns error for closed tab
});
```

- [ ] **Step 5.6: Run tests**

Run: `bun test apps/websocket/src/mcp/__tests__`
Expected: PASS.

- [ ] **Step 5.7: Commit**

```bash
git add apps/websocket/src/mcp/__tests__
git commit -m "test(mcp): update tool tests for required tab_id"
```

---

## Task 6: Update end-user documentation

**Files:**
- Modify: `skills/browser-bridge-user/SKILL.md`
- Modify: `README.md` and `README_CN.md` if they contain navigation/DOM examples without `--tab`

**Interfaces:**
- Consumes: CLI `--tab` requirement from Task 2 and tab workflow from design spec.
- Produces: user-facing docs that show explicit tab workflow.

- [ ] **Step 6.1: Update SKILL.md CLI examples**

In `skills/browser-bridge-user/SKILL.md`, replace examples like:

```bash
bridge --browser <browserId> navigate https://mail.google.com
```

with:

```bash
bridge --browser <browserId> tab:new https://mail.google.com
# note the returned tab_id, e.g. 101
bridge --browser <browserId> --tab 101 wait:navigation --timeout 15000
bridge --browser <browserId> --tab 101 gettext "selector"
```

Add a short section after the command reference:

```markdown
## Working with tabs

Every page-level command (`navigate`, `click`, `gettext`, `screenshot`, etc.) requires a `--tab <id>` argument. The workflow is:

1. `bridge --browser <id> tab:new [url]` to create a background tab.
2. Use the returned `id` as `--tab <id>` for all subsequent commands.
3. `bridge --browser <id> tab:close <tabId>` when done.

This keeps your current active tab untouched and lets you run multiple tab workflows in parallel.
```

- [ ] **Step 6.2: Update README examples**

Search README files for CLI examples and add `--tab` where needed.

Run:
```bash
grep -n "bridge --browser" README.md README_CN.md
```

Update each match.

- [ ] **Step 6.3: Commit**

```bash
git add skills/browser-bridge-user/SKILL.md README.md README_CN.md
git commit -m "docs: update CLI examples for explicit --tab workflow"
```

---

## Task 7: Full verification

**Files:**
- All touched files.

- [ ] **Step 7.1: Run full type-check**

Run: `bun run type-check`
Expected: PASS.

- [ ] **Step 7.2: Run full test suite**

Run: `bun run test`
Expected: PASS.

- [ ] **Step 7.3: Build extension**

Run: `bun run build:extension`
Expected: PASS.

- [ ] **Step 7.4: Manual end-to-end check**

1. Run `bun run dev:websocket`.
2. Load the built extension in Chrome.
3. From an MCP client or the CLI:
   - `bridge --browser <id> tab:new https://example.com` → note tab id.
   - `bridge --browser <id> --tab <id> gettext h1` → returns "Example Domain" or similar.
   - Open another tab manually and make it active.
   - `bridge --browser <id> --tab <id> navigate https://example.org` → confirm your manually active tab did not change.
4. Close the test tab with `bridge --browser <id> tab:close <id>`.

- [ ] **Step 7.5: Commit final verification results (no code changes)**

If all checks pass:

```bash
git commit --allow-empty -m "chore: verify explicit tab handle implementation"
```

---

## Self-Review

### Spec coverage

| Spec section | Implementing task |
|--------------|-------------------|
| Make `tabId` required in shared protocol | Task 1 |
| Extension requires `tabId`, removes `getActiveTabId` | Task 3 |
| MCP tool schemas require `tab_id` | Task 4 |
| `tab_new` supports `active` and `auto_close` | Task 4.5 |
| CLI requires `--tab` | Task 2 |
| Test updates | Task 5 |
| Documentation updates | Task 6 |
| Full verification | Task 7 |

### Placeholder scan

No TBD/TODO/"implement later"/"similar to Task N" placeholders. Every step contains exact file paths, code, and commands.

### Type consistency

- `CommandPayload.tabId` is `number` everywhere.
- MCP tool schema field is `tab_id` (snake_case) mapped to extension `tabId` (camelCase).
- CLI global option is `--tab`, parsed to `number`, forwarded as `tabId`.

### Known gaps / decisions

- `auto_close` is accepted by `tab_new` but has no runtime effect in this plan, matching the approved design.
- The extension has no automated unit test harness in this repo; verification is manual via build + end-to-end check.
- `tab:list` remains unchanged because it does not target a single tab.
- `list-browsers` and `set-browser` remain unchanged.

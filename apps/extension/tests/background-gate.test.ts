// Orchestration-layer tests for the background policy gate: commands driven
// through handleCommand against a mocked chrome.* surface. The storage-layer
// queue (policy-state.test.ts) and the policy core (shared) are tested in
// isolation; what only this file covers is the pipeline between them —
// origin lookup → preflight → decide+consume → execute — including the two
// paths the review called out: concurrent singleUse consumption and the
// execution-time sensitive-field recheck rejection.

import { beforeAll, beforeEach, describe, expect, it } from 'bun:test';
import {
  type CommandResultMap,
  type CommandType,
  SENSITIVE_FIELD_RECHECK_ERROR,
} from '@browser-bridge/shared';
import type * as background from '../src/background';

const HOUR = 60 * 60 * 1000;

// In-memory chrome stand-ins. storage.local is async but not transactional;
// the policy-state write queue is what keeps concurrent commands correct.
const store = new Map<string, unknown>();
const tabUrls = new Map<number, string>();
const sentToContentScript: Record<string, unknown>[] = [];
let badgeTextUpdates = 0;
let lastBadgeText: { text: string } | null = null;

// Test-controlled content script behaviour behind the single tabs.sendMessage
// seam: ping (listener liveness), preflight (sensitive classification), and
// command dispatch.
let preflightResult: { sensitive: boolean } = { sensitive: false };

// Per-command mock data must match `CommandResultMap[CommandType]` — AGENTS.md
// is explicit that test mocks are constructed from the typed contract, and
// `handleCommand` is annotated `Promise<CommandResultMap[CommandType]>`. A
// blanket `{ ok: true }` was a latent bug: any future `expect(result).toEqual<
// ClickResult>(…)` or downstream typed consumer would hit a confusing mismatch.
function mockResultFor(
  cmd: CommandType,
  params: Record<string, unknown>,
): CommandResultMap[CommandType] {
  switch (cmd) {
    case 'click':
      return { clicked: stringParam(params, 'selector') };
    case 'type':
      return { typed: stringParam(params, 'text') };
    case 'select':
      return { selected: stringParam(params, 'value') };
    case 'scroll':
      return { scrolled: true };
    case 'hover':
      return { hovered: stringParam(params, 'selector') };
    case 'gettext':
      return { text: null };
    case 'gethtml':
      return { html: '' };
    case 'snapshot':
      return {
        snapshot: '',
        truncated: false,
        nodes_total: 0,
        nodes_emitted: 0,
        tier: 0,
      };
    case 'screenshot':
      return { dataUrl: '' };
    case 'pageinfo':
      return { active: false };
    case 'navigate':
    case 'wait:navigation':
      return { url: undefined, title: undefined };
    case 'tab:list':
      return [];
    case 'tab:new':
    case 'tab:switch':
      return {};
    case 'tab:close':
    case 'goBack':
    case 'goForward':
    case 'refresh':
      return { ok: true };
    case 'wait:element':
      return { found: true, selector: '' };
    default: {
      const _exhaustive: never = cmd;
      return _exhaustive;
    }
  }
}

function stringParam(params: Record<string, unknown>, key: string): string {
  return typeof params[key] === 'string' ? (params[key] as string) : '';
}

function defaultContentScriptResponse(
  message: Record<string, unknown>,
): Promise<unknown> {
  const payload = message.payload as
    | { command?: CommandType; params?: Record<string, unknown> }
    | undefined;
  if (!payload?.command) {
    return Promise.resolve({ status: 'ok', data: { ok: true } });
  }
  return Promise.resolve({
    status: 'ok',
    data: mockResultFor(payload.command, payload.params ?? {}),
  });
}

let contentScriptResponse: (
  message: Record<string, unknown>,
) => Promise<unknown> = defaultContentScriptResponse;

let handleCommand: typeof background.handleCommand;

function makeCommand(
  command: CommandType,
  tabId: number,
  params: Record<string, unknown> = {},
): Parameters<typeof background.handleCommand>[0] {
  return {
    id: `test-${command}`,
    type: 'command',
    browserId: 'test-browser',
    payload: { command, tabId, params },
    timestamp: Date.now(),
  };
}

beforeAll(async () => {
  (globalThis as Record<string, unknown>).chrome = {
    storage: {
      local: {
        get: async (key: string): Promise<Record<string, unknown>> =>
          store.has(key) ? { [key]: store.get(key) } : {},
        set: async (entries: Record<string, unknown>): Promise<void> => {
          for (const [key, value] of Object.entries(entries)) {
            store.set(key, value);
          }
        },
      },
      onChanged: { addListener: () => {} },
    },
    runtime: {
      // Listener registrations and the module-bottom connectOffscreen() are
      // import-time side effects of background.ts; they only need inert
      // stand-ins.
      onMessage: { addListener: () => {} },
      onInstalled: { addListener: () => {} },
      onStartup: { addListener: () => {} },
      getContexts: async () => [],
      getURL: (path: string) => `chrome-extension://test/${path}`,
      sendMessage: async () => ({ connected: false }),
      ContextType: { OFFSCREEN_DOCUMENT: 'OFFSCREEN_DOCUMENT' },
    },
    offscreen: {
      createDocument: async () => {},
      Reason: { WEB_RTC: 'WEB_RTC' },
    },
    tabs: {
      get: async (tabId: number) => ({
        id: tabId,
        url: tabUrls.get(tabId) ?? 'about:blank',
        windowId: 1,
        active: true,
      }),
      sendMessage: async (_tabId: number, message: Record<string, unknown>) => {
        if (message.type === 'ping') return { type: 'pong' };
        if (message.type === 'preflight') {
          return { status: 'ok', data: preflightResult };
        }
        sentToContentScript.push(message);
        return contentScriptResponse(message);
      },
      onUpdated: { addListener: () => {}, removeListener: () => {} },
      onRemoved: { addListener: () => {} },
    },
    scripting: { executeScript: async () => {} },
    action: {
      setBadgeText: async (details: { text: string }) => {
        badgeTextUpdates += 1;
        lastBadgeText = { text: details.text };
      },
      setBadgeBackgroundColor: async () => {},
      setTitle: async () => {},
    },
    downloads: { onCreated: { addListener: () => {} } },
    windows: { onRemoved: { addListener: () => {} } },
    sidePanel: { setPanelBehavior: async () => {} },
  };

  // Static imports would evaluate background.ts before the chrome global
  // exists (the module registers listeners and calls connectOffscreen at load
  // time), so the import is dynamic and ordered after the mock.
  ({ handleCommand } = await import('../src/background'));
});

beforeEach(() => {
  store.clear();
  tabUrls.clear();
  sentToContentScript.length = 0;
  badgeTextUpdates = 0;
  lastBadgeText = null;
  preflightResult = { sensitive: false };
  contentScriptResponse = defaultContentScriptResponse;
});

describe('background policy gate orchestration', () => {
  it('consumes a singleUse origin grant exactly once across concurrent commands', async () => {
    tabUrls.set(1, 'https://unapproved.site/page');
    store.set('policyState', {
      grants: [
        {
          capability: 'origin',
          origin: 'https://unapproved.site',
          expiresAt: Date.now() + HOUR,
          singleUse: true,
        },
      ],
    });

    const results = await Promise.allSettled(
      Array.from({ length: 5 }, () => handleCommand(makeCommand('click', 1))),
    );

    const fulfilled = results.filter((r) => r.status === 'fulfilled');
    const rejected = results.filter((r) => r.status === 'rejected');
    expect(fulfilled).toHaveLength(1);
    expect(rejected).toHaveLength(4);
    for (const r of rejected) {
      const reason = (r as PromiseRejectedResult).reason as {
        name: string;
        denial: { reason: string };
      };
      expect(reason.name).toBe('PolicyDeniedError');
      expect(reason.denial.reason).toBe('origin_not_approved');
    }

    const stored = store.get('policyState') as { grants: unknown[] };
    expect(stored.grants).toHaveLength(0);
    expect(badgeTextUpdates).toBeGreaterThan(0);
  });

  it('authorizes exactly one of two concurrent type commands with a single sensitive-field grant', async () => {
    tabUrls.set(1, 'https://approved.site/form');
    store.set('policyState', {
      origins: { 'https://approved.site': 'always' },
      grants: [
        {
          capability: 'sensitive-field',
          expiresAt: Date.now() + HOUR,
          singleUse: true,
        },
      ],
    });
    preflightResult = { sensitive: true };

    const results = await Promise.allSettled([
      handleCommand(makeCommand('type', 1, { selector: '#pw', text: 'a' })),
      handleCommand(makeCommand('type', 1, { selector: '#pw', text: 'b' })),
    ]);

    expect(results.filter((r) => r.status === 'fulfilled')).toHaveLength(1);
    const loser = results.find(
      (r) => r.status === 'rejected',
    ) as PromiseRejectedResult;
    expect(loser.reason.name).toBe('PolicyDeniedError');
    expect(loser.reason.denial.reason).toBe('approval_required');
    expect(loser.reason.denial.capability).toBe('sensitive-field');

    // Only the winning command reached the content script, carrying the
    // sensitiveApproved flag that authorizes the one write.
    expect(sentToContentScript).toHaveLength(1);
    const forwarded = sentToContentScript[0] as {
      payload: { params: Record<string, unknown> };
    };
    expect(forwarded.payload.params.sensitiveApproved).toBe(true);

    const stored = store.get('policyState') as { grants: unknown[] };
    expect(stored.grants).toHaveLength(0);
  });

  it('surfaces an execution-time sensitive-field recheck as an approval_required denial', async () => {
    // TOCTOU: the preflight classifies the field as ordinary, the gate
    // allows, then the content script re-classifies at write time and
    // reports the recheck error — the orchestrator must turn that into a
    // recorded policy denial, not a bare error.
    //
    // The mock returns the wire shape that `chrome.tabs.sendMessage` would
    // deliver to content-bridge.ts — i.e. the post-`sendResponse` payload,
    // not the raw `String(err)` form. `content.ts:551` sends `err.message`
    // (not `String(err)`) so the wire carries the bare constant; if it ever
    // regressed to `String(err)`, the prefix `Error: ` would break the
    // equality check below and this test would still pass (the SUT would
    // throw a bare Error instead of PolicyDeniedError). The companion test
    // `does not treat an Error-prefixed error as a recheck` pins the prefix
    // invariant from the receiver side.
    tabUrls.set(1, 'https://approved.site/form');
    store.set('policyState', {
      origins: { 'https://approved.site': 'always' },
    });
    contentScriptResponse = async () => ({
      status: 'error',
      error: SENSITIVE_FIELD_RECHECK_ERROR,
    });

    const failure = await handleCommand(
      makeCommand('type', 1, { selector: '#field', text: 'x' }),
    ).then(
      () => null,
      (err: Error & { denial?: { reason: string; capability?: string } }) =>
        err,
    );

    expect(failure).not.toBeNull();
    expect(failure?.name).toBe('PolicyDeniedError');
    expect(failure?.denial?.reason).toBe('approval_required');
    expect(failure?.denial?.capability).toBe('sensitive-field');

    const stored = store.get('policyState') as {
      recentDenials: { reason: string }[];
    };
    expect(stored.recentDenials.map((d) => d.reason)).toContain(
      'approval_required',
    );
    expect(badgeTextUpdates).toBeGreaterThan(0);
    // The badge text must reflect the denial count (`String(count)` per
    // policy-state.ts:201) so the side-panel 'X actions need attention' cue
    // surfaces the right number — not the action title, not empty, not a
    // hard-coded 'Browser Bridge' label.
    expect(lastBadgeText).not.toBeNull();
    expect(lastBadgeText?.text).toBe('1');
  });

  it('does not treat an Error-prefixed error as a recheck', async () => {
    // Companion to the recheck test: pins the invariant that the wire
    // contract requires `err.message` (bare constant), not `String(err)`,
    // on the content-script -> background boundary. If `content.ts:551`
    // regresses to `error: String(err)`, the receiver sees `"Error:
    // bb_sensitive_field_at_execution"` — the equality check at
    // `background.ts:444` must NOT match, and the orchestrator must surface
    // a bare (non-PolicyDenied) error instead of mis-recording an
    // approval_required denial. This test would fail under the regression.
    tabUrls.set(1, 'https://approved.site/form');
    store.set('policyState', {
      origins: { 'https://approved.site': 'always' },
    });
    contentScriptResponse = async () => ({
      status: 'error',
      error: `Error: ${SENSITIVE_FIELD_RECHECK_ERROR}`,
    });

    const failure = await handleCommand(
      makeCommand('type', 1, { selector: '#field', text: 'x' }),
    ).then(
      () => null,
      (err: Error & { denial?: { reason: string } }) => err,
    );

    expect(failure).not.toBeNull();
    expect(failure?.name).not.toBe('PolicyDeniedError');

    const stored = store.get('policyState') as {
      recentDenials?: { reason: string }[];
    };
    expect(stored.recentDenials ?? []).toHaveLength(0);
  });
});

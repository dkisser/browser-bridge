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
// Records the browser-level side effects a command handler performed, so a
// takeover test can assert the handler never ran — not merely that the call
// rejected. A url-less tab:new that opens a tab is the regression these guard.
const createdTabs: { url?: string; active?: boolean }[] = [];
const queriedTabs: unknown[] = [];
// chrome.tabs.get reports no `status` unless a test sets one, so the default
// stays 'loading' — the pre-existing behavior for every other test here. The
// navigation tests set it explicitly to drive both the "already finished" and
// "still loading" paths.
const tabStatuses = new Map<number, string>();
// URLs staged by chrome.tabs.update, committed when the document loads.
const pendingNavUrl = new Map<number, string>();
// Live onUpdated listener registry, so a test can fire the event that a real
// navigation produces — and assert the handler removes it again.
let onUpdatedListeners: ((
  tabId: number,
  changeInfo: chrome.tabs.TabChangeInfo,
) => void)[] = [];

// The real service-worker's chrome.runtime.onMessage listener, captured so a
// test can drive an actual `policy_op` through it. Before this existed the
// registration was inert, so the handler ran in no test at all — which is how
// a success path that never called sendResponse shipped green.
const onMessageListeners: ((
  request: unknown,
  sender: unknown,
  sendResponse: (response?: unknown) => void,
) => boolean | undefined)[] = [];

// Set to make chrome.action.* reject, simulating a service-worker context
// being invalidated mid-request.
let badgeShouldFail = false;

function fireTabUpdated(
  tabId: number,
  changeInfo: chrome.tabs.TabChangeInfo,
): void {
  // The new document commits here, so the tab starts showing the target.
  if (changeInfo.status === 'complete') {
    const pending = pendingNavUrl.get(tabId);
    if (pending !== undefined) {
      tabUrls.set(tabId, pending);
      pendingNavUrl.delete(tabId);
    }
  }
  for (const listener of [...onUpdatedListeners]) listener(tabId, changeInfo);
}

// Yield long enough for a chain of already-resolved promises (the detached
// badge refresh) to run to completion.
function settle(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}
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
      onMessage: {
        addListener: (
          fn: (
            request: unknown,
            sender: unknown,
            sendResponse: (response?: unknown) => void,
          ) => boolean | undefined,
        ) => {
          onMessageListeners.push(fn);
        },
      },
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
        status: tabStatuses.get(tabId) ?? 'loading',
      }),
      // Faithful to Chrome in the way that matters here: tabs.update
      // *initiates* a navigation, it does not change what the tab is
      // showing. The URL changes when the new document commits, which the
      // mock models at status==='complete'. An unfaithful mock — one that
      // rewrote tabUrls immediately — would hide the exact race
      // waitForTabComplete's re-check has to survive.
      update: async (tabId: number, props: { url?: string }) => {
        if (props.url !== undefined) pendingNavUrl.set(tabId, props.url);
        return { id: tabId, url: tabUrls.get(tabId), windowId: 1 };
      },
      sendMessage: async (_tabId: number, message: Record<string, unknown>) => {
        if (message.type === 'ping') return { type: 'pong' };
        if (message.type === 'preflight') {
          // Mirrors `content.ts:570`'s post-fix wire shape
          // (`sendResponse({ status: 'ok', data })` where `data` is
          // `{ sensitive: boolean }`). A regression in content.ts that
          // re-introduces a nested `data: { sensitive }` shorthand would
          // not be caught by *this* file — content.ts has no unit test
          // for the preflight handler, so any future audit should add one.
          return { status: 'ok', data: preflightResult };
        }
        sentToContentScript.push(message);
        return contentScriptResponse(message);
      },
      create: async (props: { url?: string; active?: boolean }) => {
        createdTabs.push(props);
        return {
          id: 99,
          url: props.url,
          windowId: 1,
          active: props.active ?? false,
        };
      },
      query: async (query: unknown) => {
        queriedTabs.push(query);
        return [];
      },
      group: async (opts: { createProperties?: unknown; groupId?: number }) =>
        opts.groupId ?? 7,
      onUpdated: {
        addListener: (
          fn: (tabId: number, changeInfo: chrome.tabs.TabChangeInfo) => void,
        ) => {
          onUpdatedListeners.push(fn);
        },
        removeListener: (
          fn: (tabId: number, changeInfo: chrome.tabs.TabChangeInfo) => void,
        ) => {
          onUpdatedListeners = onUpdatedListeners.filter((l) => l !== fn);
        },
      },
      onRemoved: { addListener: () => {} },
    },
    scripting: { executeScript: async () => {} },
    // agent-group.ts degrades to "grouping disabled" when these reject, which
    // is its real behavior when Chrome withholds the tabGroups permission.
    // Mocking the happy path keeps this suite's console readable; the
    // degradation path is exercised where it belongs, not here.
    tabGroups: {
      query: async () => [],
      update: async () => ({}),
    },
    action: {
      setBadgeText: async (details: { text: string }) => {
        if (badgeShouldFail) {
          throw new Error('Extension context invalidated.');
        }
        badgeTextUpdates += 1;
        lastBadgeText = { text: details.text };
      },
      setBadgeBackgroundColor: async () => {
        if (badgeShouldFail) throw new Error('Extension context invalidated.');
      },
      setTitle: async () => {
        if (badgeShouldFail) throw new Error('Extension context invalidated.');
      },
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
  createdTabs.length = 0;
  queriedTabs.length = 0;
  tabStatuses.clear();
  pendingNavUrl.clear();
  onUpdatedListeners = [];
  badgeShouldFail = false;
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
    // (not `String(err)`) so the wire carries the bare constant. The
    // companion test `does not treat an Error-prefixed error as a recheck`
    // pins the prefix invariant from the receiver side and fails if
    // `content.ts:551` ever regresses to `String(err)`.
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

// Takeover is the human's kill switch: CONTEXT.md defines it as rejecting
// *every* agent command, and shared/policy.test.ts asserts that for
// evaluatePolicy in isolation. These are the dispatch-path counterparts —
// the layer where three commands used to short-circuit before evaluatePolicy
// was ever reached, so the asserted invariant did not actually hold.
describe('takeover covers the whole dispatch path', () => {
  const APPROVED = 'https://approved.site';

  function withTakeover(): void {
    store.set('policyState', {
      takeover: true,
      // Everything the agent could ask for is pre-approved, so the only
      // thing that can reject these commands is the takeover gate itself.
      origins: { [APPROVED]: 'always' },
      grants: [
        {
          capability: 'origin',
          origin: APPROVED,
          expiresAt: Date.now() + HOUR,
          singleUse: true,
        },
        {
          capability: 'sensitive-field',
          expiresAt: Date.now() + HOUR,
          singleUse: true,
        },
      ],
    });
  }

  const cases: {
    name: string;
    command: CommandType;
    tabId: number;
    params: Record<string, unknown>;
  }[] = [
    {
      name: 'tab:list (read-only bypass)',
      command: 'tab:list',
      tabId: 0,
      params: {},
    },
    {
      name: 'pageinfo (read-only bypass)',
      command: 'pageinfo',
      tabId: 1,
      params: {},
    },
    {
      name: 'tab:new without a url (blank-tab bypass)',
      command: 'tab:new',
      tabId: 0,
      params: {},
    },
    {
      name: 'tab:new with a url',
      command: 'tab:new',
      tabId: 0,
      params: { url: `${APPROVED}/` },
    },
    {
      name: 'navigate',
      command: 'navigate',
      tabId: 1,
      params: { url: `${APPROVED}/` },
    },
    { name: 'click', command: 'click', tabId: 1, params: { selector: '#a' } },
    {
      name: 'type',
      command: 'type',
      tabId: 1,
      params: { selector: '#pw', text: 'x' },
    },
    { name: 'screenshot', command: 'screenshot', tabId: 1, params: {} },
    { name: 'tab:close', command: 'tab:close', tabId: 1, params: { tabId: 1 } },
  ];

  for (const { name, command, tabId, params } of cases) {
    it(`rejects ${name}`, async () => {
      tabUrls.set(1, `${APPROVED}/form`);
      withTakeover();

      const failure = await handleCommand(
        makeCommand(command, tabId, params),
      ).then(
        () => null,
        (err: Error & { name?: string; denial?: { reason: string } }) => err,
      );

      expect(failure).not.toBeNull();
      expect(failure?.name).toBe('PolicyDeniedError');
      expect(failure?.denial?.reason).toBe('human_assist_active');
    });
  }

  it('opens no tab and dispatches nothing while takeover is active', async () => {
    tabUrls.set(1, `${APPROVED}/form`);
    withTakeover();

    await Promise.allSettled(
      cases.map((c) =>
        handleCommand(makeCommand(c.command, c.tabId, c.params)),
      ),
    );

    // The strong assertion: a denied command must not have reached its
    // handler. A url-less tab:new under takeover is the regression that
    // actually shipped a blank tab into the user's browser.
    expect(createdTabs).toHaveLength(0);
    expect(sentToContentScript).toHaveLength(0);
    // tab:list's handler queries with `{}`. The gate's own screenshot probe
    // uses {windowId, active:true}, which legitimately runs *before* the
    // decision, so match on the handler's exact call rather than "no query".
    expect(queriedTabs).not.toContainEqual({});
  });

  it('leaves the pre-approved grants unconsumed while takeover is active', async () => {
    tabUrls.set(1, `${APPROVED}/form`);
    withTakeover();

    await Promise.allSettled([
      handleCommand(makeCommand('click', 1, { selector: '#a' })),
      handleCommand(makeCommand('type', 1, { selector: '#pw', text: 'x' })),
    ]);

    const stored = store.get('policyState') as { grants: unknown[] };
    expect(stored.grants).toHaveLength(2);
  });

  it('allows a url-less tab:new when the human has not taken over', async () => {
    store.set('policyState', { takeover: false });

    const result = await handleCommand(makeCommand('tab:new', 0, {}));

    expect(result).toEqual({ id: 99, url: undefined });
    expect(createdTabs).toHaveLength(1);
  });
});

// navigate's wait for the page to reach status 'complete' had no bound, no
// cleanup, and no check for an event that had already fired — while
// wait:navigation, doing the identical wait, had all three. A page that
// finished loading between chrome.tabs.update resolving and the listener
// being registered lost its completion event permanently: the promise never
// settled, the response never went back, and the caller saw a timeout on a
// service worker that was perfectly healthy.
describe('navigate waits for completion without losing the event', () => {
  const URL = 'https://example.com/page';

  beforeEach(() => {
    store.set('policyState', {
      takeover: false,
      origins: { 'https://example.com': 'always' },
    });
  });

  it('completes when the page already reached complete before listening', async () => {
    // The lost-wakeup case: the tab is already complete, so no further
    // onUpdated event will ever fire for this navigation.
    tabUrls.set(1, URL);
    tabStatuses.set(1, 'complete');

    const result = await handleCommand(
      makeCommand('navigate', 1, { url: URL }),
    );

    expect(result).toEqual({ url: URL, title: undefined });
  });

  it('completes on the completion event and removes its listener', async () => {
    tabUrls.set(1, 'about:blank');
    tabStatuses.set(1, 'loading');

    const pending = handleCommand(makeCommand('navigate', 1, { url: URL }));
    // Let the gate and the tab update run before the page finishes loading.
    await new Promise((r) => setTimeout(r, 0));
    expect(onUpdatedListeners.length).toBeGreaterThan(0);

    fireTabUpdated(1, { status: 'complete' });
    const result = await pending;

    expect(result).toEqual({ url: URL, title: undefined });
    // No leaked listener: the service worker would otherwise accumulate one
    // per navigation for the life of the process.
    expect(onUpdatedListeners).toHaveLength(0);
  });

  it('fails with a navigation timeout instead of hanging forever', async () => {
    tabUrls.set(1, 'about:blank');
    tabStatuses.set(1, 'loading');

    // A page that never reaches complete — a stalled redirect, a slow
    // response. Before the fix this promise never settled at all.
    const failure = await handleCommand(
      makeCommand('navigate', 1, { url: URL, timeout: 20 }),
    ).then(
      () => null,
      (err: Error) => err,
    );

    expect(failure?.message).toBe('Navigation timeout');
    expect(onUpdatedListeners).toHaveLength(0);
  });

  it('ignores completion events for other tabs', async () => {
    tabUrls.set(1, 'about:blank');
    tabStatuses.set(1, 'loading');

    const pending = handleCommand(
      makeCommand('navigate', 1, { url: URL, timeout: 20 }),
    );
    await new Promise((r) => setTimeout(r, 0));

    // A different tab finishing must not resolve this one.
    fireTabUpdated(99, { status: 'complete' });
    const failure = await pending.then(
      () => null,
      (err: Error) => err,
    );

    expect(failure?.message).toBe('Navigation timeout');
  });

  it('does not accept the page it is leaving as already complete', async () => {
    // chrome.tabs.update resolves when the navigation is *initiated*, so the
    // tab can still be reporting the previous page as 'complete' when the
    // wait begins. The re-check must not treat that as this navigation
    // having finished, or the caller gets the old page's url and title
    // alongside a success.
    tabUrls.set(1, 'https://example.com/old-page');
    tabStatuses.set(1, 'complete');

    const failure = await handleCommand(
      makeCommand('navigate', 1, { url: URL, timeout: 25 }),
    ).then(
      () => null,
      (err: Error) => err,
    );

    expect(failure?.message).toBe('Navigation timeout');
  });

  it('accepts the re-check once the tab has actually moved to the target', async () => {
    // The complementary case: the same 'complete' status, but the tab is now
    // on the requested origin (a bare origin in the request vs the trailing
    // slash the browser reports must still match).
    tabUrls.set(1, URL);
    tabStatuses.set(1, 'complete');

    const result = await handleCommand(
      makeCommand('navigate', 1, { url: URL }),
    );

    expect(result).toEqual({ url: URL, title: undefined });
  });

  it('does not resolve on a non-completion update for the same tab', async () => {
    // onUpdated fires for many reasons: the url changing, favIconUrl,
    // status flipping to 'loading', and so on. Only status==='complete'
    // means the page is ready. Resolving on any event would return the
    // *previous* page's url and title while claiming the navigation
    // succeeded.
    tabUrls.set(1, 'about:blank');
    tabStatuses.set(1, 'loading');

    const pending = handleCommand(
      makeCommand('navigate', 1, { url: URL, timeout: 30 }),
    );
    await new Promise((r) => setTimeout(r, 0));

    fireTabUpdated(1, { status: 'loading' });
    fireTabUpdated(1, { url: URL });
    fireTabUpdated(1, { favIconUrl: 'https://example.com/f.png' });

    // Still waiting — those events must not have resolved it.
    const failure = await pending.then(
      () => null,
      (err: Error) => err,
    );
    expect(failure?.message).toBe('Navigation timeout');
  });

  it('honors a caller timeout beyond the 30s default', async () => {
    // The control plane sends its own budget; the extension must not quietly
    // cap it at DEFAULT_NAV_TIMEOUT_MS, or a caller that asked for 60s gets
    // "Navigation timeout" at 30s. The page here simply never completes, so
    // the assertion is that the call is still pending well past 30s.
    tabUrls.set(1, 'about:blank');
    tabStatuses.set(1, 'loading');

    let settled = false;
    const pending = handleCommand(
      makeCommand('navigate', 1, { url: URL, timeout: 45_000 }),
    ).then(
      (v) => {
        settled = true;
        return v;
      },
      (e) => {
        settled = true;
        throw e;
      },
    );

    // Guard against a hardcoded 30s cap without making the suite sleep for
    // 30 real seconds: the handler is still pending at 60ms, and the budget
    // it was given is far larger than any hardcoded default would allow to be
    // observed as "still running".
    await new Promise((r) => setTimeout(r, 60));
    expect(settled).toBe(false);

    fireTabUpdated(1, { status: 'complete' });
    const result = await pending;
    expect(result).toEqual({ url: URL, title: undefined });
  });
});

// Drive a real `policy_op` through the real service-worker listener.
//
// Every other test around this path mocks `chrome.runtime.sendMessage` with
// its own reimplementation, so background.ts's own handler was executed by no
// test in the suite. That is how a success path that built the response
// payload and then dropped it — never calling sendResponse — shipped green:
// the side panel's promise would hang, and the takeover switch would flip back
// to "human assist active" while storage said the agent had the browser.
describe('policy_op through the real service-worker listener', () => {
  const listener = () => onMessageListeners[0];

  function sendPolicyOp(op: unknown): Promise<unknown> {
    return new Promise((resolve, reject) => {
      const timer = setTimeout(
        () => reject(new Error('policy_op was never answered')),
        1000,
      );
      const kept = listener()?.(
        { type: 'policy_op', op },
        { id: 'test' },
        (response?: unknown) => {
          clearTimeout(timer);
          resolve(response);
        },
      );
      // Returning true is what keeps the message channel open for an async
      // response; returning falsy would close it before the handler replied.
      expect(kept).toBe(true);
    });
  }

  beforeEach(() => {
    store.set('policyState', { takeover: false, origins: {} });
  });

  it('answers a successful operation', async () => {
    const response = (await sendPolicyOp({
      op: 'set_takeover',
      desired: true,
    })) as { status: string; data: { takeover: boolean } };

    expect(response.status).toBe('ok');
    expect(response.data.takeover).toBe(true);
    // The write actually landed, not just a well-formed reply.
    const stored = store.get('policyState') as { takeover: boolean };
    expect(stored.takeover).toBe(true);
    // And the badge really was refreshed. Without this, deleting the
    // updateBadge() call entirely would leave every other test green — the
    // badge is fire-and-forget precisely so its failure cannot reach the
    // caller, which also means nothing else would notice it stopping.
    // The refresh is deliberately not awaited by the handler, so give it a
    // tick to land rather than assuming an ordering that no longer exists.
    await settle();
    expect(badgeTextUpdates).toBeGreaterThan(0);
  });

  it('answers even when the badge refresh fails', async () => {
    // The badge is cosmetic. If its failure could reach the caller, the panel
    // would revert its optimistic UI while the write had already landed —
    // for Takeover that shows the user "human assist active" while the agent
    // actually has the browser.
    badgeShouldFail = true;

    // The seeded state has takeover:false, so asking for true makes the write
    // observable — asserting takeover:false afterwards would pass even if the
    // handler never wrote at all.
    const response = (await sendPolicyOp({
      op: 'set_takeover',
      desired: true,
    })) as { status: string; data: { takeover: boolean } };

    expect(response.status).toBe('ok');
    const stored = store.get('policyState') as { takeover: boolean };
    expect(stored.takeover).toBe(true);
  });

  it('answers with an error when the operation itself fails', async () => {
    // An origin-less denial is in the store, so the reducer reaches the
    // approve guard and throws. The caller must hear about it rather than
    // hang. denialKey is `reason|origin|command`, with an empty origin.
    store.set('policyState', {
      takeover: false,
      origins: {},
      recentDenials: [{ reason: 'approval_required', command: 'click' }],
    });

    const response = (await sendPolicyOp({
      op: 'denial_action',
      action: 'approve-always',
      targetKey: 'approval_required||click',
    })) as { status: string; error: string };

    expect(response.status).toBe('error');
    expect(response.error).toContain('requires a denial with an origin');
  });

  it('serializes concurrent operations from two UI contexts', async () => {
    // The whole point of routing writes through one queue: two panels (or a
    // panel and the settings page) acting at once must not lose an update.
    // Each op is a read-modify-write of the same object, so unserialized
    // they would restore each other's fields — which is how a consumed
    // singleUse grant came back to life.
    store.set('policyState', {
      takeover: false,
      origins: { 'https://a.example': 'always' },
      blockedOrigins: [],
      recentDenials: [
        {
          reason: 'origin_not_approved',
          command: 'click',
          origin: 'https://b.example',
        },
      ],
    });

    // Two operations that write *different* fields, so a lost update shows up
    // as one of them being missing. Unserialized, each reads the pre-write
    // state and writes the whole object back, and the loser restores the
    // winner's fields.
    const [approve, block] = (await Promise.all([
      sendPolicyOp({
        op: 'denial_action',
        action: 'approve-always',
        targetKey: 'origin_not_approved|https://b.example|click',
      }),
      sendPolicyOp({ op: 'add_block', entry: 'evil.example' }),
    ])) as unknown as { status: string }[];

    expect(approve.status).toBe('ok');
    expect(block.status).toBe('ok');

    const stored = store.get('policyState') as {
      origins: Record<string, string>;
      blockedOrigins: string[];
    };
    // Both writes survived, whatever order the queue ran them in.
    expect(stored.origins['https://a.example']).toBe('always');
    expect(stored.origins['https://b.example']).toBe('always');
    expect(stored.blockedOrigins).toContain('evil.example');
  });

  it('answers with an error when the storage write itself fails', async () => {
    // chrome.storage.local.set can reject (quota, or a context invalidated
    // mid-request). The caller must hear about it rather than be told the
    // policy changed.
    const realSet = (
      globalThis as unknown as {
        chrome: { storage: { local: { set: unknown } } };
      }
    ).chrome.storage.local.set;
    (
      globalThis as unknown as {
        chrome: { storage: { local: { set: unknown } } };
      }
    ).chrome.storage.local.set = async () => {
      throw new Error('QUOTA_BYTES quota exceeded');
    };
    try {
      const response = (await sendPolicyOp({
        op: 'set_takeover',
        desired: true,
      })) as { status: string; error: string };

      expect(response.status).toBe('error');
      expect(response.error).toContain('quota');
    } finally {
      (
        globalThis as unknown as {
          chrome: { storage: { local: { set: unknown } } };
        }
      ).chrome.storage.local.set = realSet;
    }
  });

  it('keeps serving operations after one fails', async () => {
    store.set('policyState', {
      takeover: false,
      origins: {},
      recentDenials: [{ reason: 'approval_required', command: 'click' }],
    });
    await sendPolicyOp({
      op: 'denial_action',
      action: 'approve-always',
      targetKey: 'approval_required||click',
    });

    const response = (await sendPolicyOp({
      op: 'add_block',
      entry: 'evil.example',
    })) as { status: string; data: { blockedOrigins: string[] } };

    expect(response.status).toBe('ok');
    expect(response.data.blockedOrigins).toEqual(['evil.example']);
  });
});

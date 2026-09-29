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

// Fires inside the Nth chrome.storage.local read of a test, so the world can
// change between two of the handler's *own* reads.
//
// This is not mock-cooking the decision: the handler really does read policy
// state twice on a DOM command — once for the gate's verdict, once for the
// Takeover re-check at the execution point — and flipping takeover between
// them is exactly the real-world sequence, a user reaching for the kill
// switch while a command is already in flight. The mock stays faithful (reads
// are reads, storage is the single source of truth); it only chooses *when*
// the world changes.
let storageReads = 0;
let onNthStorageRead: { n: number; run: () => void } | null = null;
// The injection boundary, which is a different seam from a storage read: it is
// inside the dispatch path, after the execution-point re-check that a
// read-ordinal flip targets. See the cold-tab test below.
let onExecuteScript: (() => void) | null = null;
let pingFailuresBeforeInjection = 0;

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
let assertTakeoverUnchanged: typeof background.assertTakeoverUnchanged;

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
        get: async (key: string): Promise<Record<string, unknown>> => {
          storageReads += 1;
          if (
            onNthStorageRead !== null &&
            storageReads === onNthStorageRead.n
          ) {
            const { run } = onNthStorageRead;
            onNthStorageRead = null;
            run();
          }
          return store.has(key) ? { [key]: store.get(key) } : {};
        },
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
      // The listener now checks the sender's url against this id, so the
      // mock needs one; without it every policy_op in this file is rejected.
      id: 'test',
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
        if (message.type === 'ping') {
          // Failing the first N pings is how a test reaches the *injection*
          // path: ensureContentScript returns early when the listener already
          // answers, so the default mock never injects, and a test that cannot
          // inject cannot observe anything that happens around the injection.
          // waitForListener pings again afterwards, so only the pre-injection
          // pings need to fail.
          if (pingFailuresBeforeInjection > 0) {
            pingFailuresBeforeInjection -= 1;
            throw new Error(
              'Could not establish connection. Receiving end does not exist.',
            );
          }
          return { type: 'pong' };
        }
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
    scripting: {
      executeScript: async () => {
        // The injection boundary. Everything the content script needs before
        // it can listen happens inside this call, and it is the slowest await
        // in the dispatch path on a cold tab — which is exactly why a
        // takeover check that runs *before* it covers a window that has
        // already been closed by the time the command is sent.
        const hook = onExecuteScript;
        onExecuteScript = null;
        if (hook) hook();
      },
    },
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
  ({ handleCommand, assertTakeoverUnchanged } = await import(
    '../src/background'
  ));
});

beforeEach(() => {
  store.clear();
  tabUrls.clear();
  onExecuteScript = null;
  pingFailuresBeforeInjection = 0;
  sentToContentScript.length = 0;
  createdTabs.length = 0;
  queriedTabs.length = 0;
  tabStatuses.clear();
  pendingNavUrl.clear();
  onUpdatedListeners = [];
  badgeShouldFail = false;
  badgeTextUpdates = 0;
  storageReads = 0;
  onNthStorageRead = null;
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

// Takeover was enforced at the gate and nowhere else. The gate reads policy
// state inside the write queue, and a takeover flip is applied in that same
// queue, so a command still queued when the user hit the switch does see it —
// but nothing re-checked after the decision, and DOM commands have a real
// window between the two: sendToContentScript calls ensureContentScript
// first, which injects the content script when the tab does not have one yet.
describe('takeover is re-checked at the execution point', () => {
  const APPROVED = 'https://approved.site';

  beforeEach(() => {
    store.set('policyState', {
      takeover: false,
      origins: { [APPROVED]: 'always' },
    });
    tabUrls.set(1, `${APPROVED}/form`);
  });

  it('withdraws a command when the human takes over after the gate allowed it', async () => {
    // Read 1 is the gate's verdict, read 2 is the execution-point re-check.
    // Flipping between them is the sequence this whole re-check exists for:
    // the user reaching for the kill switch while a command is in flight.
    onNthStorageRead = {
      n: 2,
      run: () => {
        store.set('policyState', {
          takeover: true,
          origins: { [APPROVED]: 'always' },
        });
      },
    };

    const failure = await handleCommand(
      makeCommand('click', 1, { selector: '#a' }),
    ).then(
      () => null,
      (err: Error & { name?: string; denial?: { reason: string } }) => err,
    );

    expect(failure?.name).toBe('PolicyDeniedError');
    expect(failure?.denial?.reason).toBe('human_assist_active');
    // The point of the re-check: the click never reached the page. A
    // withdrawn command that still dispatched would leave the user watching
    // a page act on the browser they just took back.
    expect(sentToContentScript).toHaveLength(0);

    // What this test cannot do, stated here because it is the trap: it
    // cannot tell you *which* denial site fired. The gate and the re-check
    // both throw PolicyDeniedError with reason human_assist_active, and
    // nothing observable happens between them — no event, no await — so a
    // flip keyed on a read ordinal can land on the gate instead and this
    // still passes. The read count does not separate the two either; it is
    // the same under both orderings.
    //
    // So this is an integration test of the window: takeover is engaged
    // during command handling and the command does not reach the page. The
    // re-check's own behaviour is pinned by the direct test below.
  });

  it('withdraws a command when the human takes over during content-script injection', async () => {
    // The window the re-check's own comment claims to close.
    //
    // That comment says the gate "cannot cover the stretch between its
    // decision and the DOM actually changing" because sendToContentScript
    // injects the content script first, "which is a real await — long enough
    // on a cold tab". But the re-check ran *before* that await, not across
    // it: ensureContentScript (a tabs.get, a ping, an executeScript, and a
    // listener wait that can take up to 2s) all happened between the check
    // and the dispatch. Engaging Takeover anywhere in there was a no-op.
    //
    // Keyed on the injection rather than on a storage read ordinal on
    // purpose. A read ordinal pins the test to an internal call count, and
    // the read that mattered was never in the window at all — the existing
    // test above flips on read 2, which is the one instant the check does
    // cover, so it passed while the seam sat in the wrong place.
    pingFailuresBeforeInjection = 1;
    onExecuteScript = () => {
      store.set('policyState', {
        takeover: true,
        origins: { [APPROVED]: 'always' },
      });
    };

    const failure = await handleCommand(
      makeCommand('click', 1, { selector: '#a' }),
    ).then(
      () => null,
      (err: Error & { name?: string; denial?: { reason: string } }) => err,
    );

    expect(failure?.name).toBe('PolicyDeniedError');
    expect(failure?.denial?.reason).toBe('human_assist_active');
    // The whole point: the click did not reach the page. The user took the
    // browser back while it was being prepared, and it clicked anyway.
    expect(sentToContentScript).toHaveLength(0);
  });

  it('re-checks every command that shares the DOM dispatch path', async () => {
    // The re-check is attached to a `case` block listing nine commands, and
    // the tests above drive exactly one of them. A refactor that split the
    // block — a read group and a mutation group is the obvious split, and the
    // comment above it already argues per-group reasoning — and passed
    // `beforeSend` in only one arm would leave seven commands with no
    // execution-point re-check while every test stayed green. Verified: with
    // the re-check narrowed to `click || type`, the full suite passes.
    //
    // Driven off the same seam as the test above, because the seam is the
    // thing under test. Each command needs params its own handler would
    // accept; `type` additionally needs a preflight answer, which the mock
    // already returns as non-sensitive by default.
    const DOM_COMMANDS: [CommandType, Record<string, unknown>][] = [
      ['click', { selector: '#a' }],
      ['type', { selector: '#a', text: 'hi' }],
      ['select', { selector: '#a', value: 'v' }],
      ['scroll', { selector: '#a', direction: 'down' }],
      ['hover', { selector: '#a' }],
      ['gettext', { selector: '#a' }],
      ['gethtml', { selector: '#a' }],
      ['snapshot', {}],
      ['wait:element', { selector: '#a' }],
    ];

    for (const [command, params] of DOM_COMMANDS) {
      store.set('policyState', {
        takeover: false,
        origins: { [APPROVED]: 'always' },
      });
      tabUrls.set(1, `${APPROVED}/form`);
      sentToContentScript.length = 0;
      pingFailuresBeforeInjection = 1;
      onExecuteScript = () => {
        store.set('policyState', {
          takeover: true,
          origins: { [APPROVED]: 'always' },
        });
      };

      const failure = await handleCommand(makeCommand(command, 1, params)).then(
        () => null,
        (err: Error & { name?: string }) => err,
      );

      // Named per command, so a failure says which arm lost its re-check
      // rather than pointing at the loop.
      expect({ command, denied: failure?.name }).toEqual({
        command,
        denied: 'PolicyDeniedError',
      });
      expect({ command, dispatched: sentToContentScript.length }).toEqual({
        command,
        dispatched: 0,
      });
    }
  });

  it('still injects and dispatches when nobody takes over mid-flight', async () => {
    // The other direction for the same seam. A `beforeSend` that refused
    // unconditionally would pass the test above by refusing everything, and
    // the existing "leaves the command alone" test does not reach the
    // injection path at all, so nothing would notice.
    pingFailuresBeforeInjection = 1;
    const result = await handleCommand(
      makeCommand('click', 1, { selector: '#a' }),
    );

    expect(result).toEqual({ clicked: '#a' });
    expect(sentToContentScript.length).toBeGreaterThan(0);
  });

  it('leaves the command alone when the human does not take over', async () => {
    // The other direction. A re-check that denied unconditionally would pass
    // the test above by refusing everything, and this is what says it is
    // reading the state rather than always saying no.
    const result = await handleCommand(
      makeCommand('click', 1, { selector: '#a' }),
    );

    expect(result).toEqual({ clicked: '#a' });
    expect(sentToContentScript.length).toBeGreaterThan(0);
  });

  it('leaves no approval card and no badge count for a takeover refusal', async () => {
    // A recentDenials entry is an approval request — a card with action
    // buttons, a number on the badge, something the user is meant to act
    // on. A takeover refusal is none of those: the user engaged the kill
    // switch and every command after it was refused as intended. Recording
    // them made the agent's retries walk the list up to its cap while the
    // user had nothing to dismiss, and left the panel still saying the
    // human was in control after they released the browser.
    onNthStorageRead = {
      n: 2,
      run: () => {
        store.set('policyState', {
          takeover: true,
          origins: { [APPROVED]: 'always' },
        });
      },
    };

    await handleCommand(makeCommand('click', 1, { selector: '#a' })).catch(
      () => undefined,
    );

    const stored = store.get('policyState') as { recentDenials?: unknown[] };
    expect(stored.recentDenials ?? []).toHaveLength(0);
    // The badge is not merely refreshed to empty — it is never touched. There
    // is nothing to clear, because a takeover refusal never added to the
    // count, and a redundant chrome.action call here would be a round-trip
    // whose only possible outcome is writing back the same number.
    expect(lastBadgeText).toBeNull();
  });

  it('still records an ordinary refusal, so the new rule is not a blanket one', async () => {
    // recordDenial dropping takeover denials must not have swallowed the
    // refusals that *are* approval requests. This is the direction that says
    // the filter keys on the reason rather than short-circuiting the write.
    store.set('policyState', {
      takeover: true,
      origins: { [APPROVED]: 'always' },
    });
    await handleCommand(makeCommand('click', 1, { selector: '#a' })).catch(
      () => undefined,
    );
    const afterTakeover = store.get('policyState') as {
      recentDenials?: unknown[];
    };
    expect(afterTakeover.recentDenials ?? []).toHaveLength(0);

    // Now a refusal the user can actually act on: an unapproved origin.
    store.set('policyState', { takeover: false, origins: {} });
    await handleCommand(makeCommand('click', 1, { selector: '#a' })).catch(
      () => undefined,
    );
    const afterOrigin = store.get('policyState') as {
      recentDenials?: { reason: string }[];
    };
    expect(afterOrigin.recentDenials).toHaveLength(1);
    expect(afterOrigin.recentDenials?.[0].reason).toBe('origin_not_approved');
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
    // on the requested page. The request is a *bare origin* while the browser
    // reports it with a trailing slash — the two are different strings, so
    // reachingUrl's normalization is what makes this resolve. Pointing both
    // sides at the same constant would pass even with the normalization
    // deleted, which is why they deliberately differ here.
    tabUrls.set(1, 'https://example.com/');
    tabStatuses.set(1, 'complete');

    const result = await handleCommand(
      makeCommand('navigate', 1, { url: 'https://example.com' }),
    );

    expect(result).toEqual({ url: 'https://example.com/', title: undefined });
  });

  it('still waits when the tab is on the target url but has not loaded', async () => {
    // The url property commits before the document finishes loading, so a tab
    // can sit on exactly the requested page while status is still 'loading'.
    // Every other "still loading" case in this file leaves the tab on
    // about:blank or the old page, which means the url check alone already
    // rejects them — a re-check that accepted 'loading' outright would pass
    // all of them and only fail here. Status and url are independent
    // conditions and this is the case that pins them apart.
    tabUrls.set(1, URL);
    tabStatuses.set(1, 'loading');

    const failure = await handleCommand(
      makeCommand('navigate', 1, { url: URL, timeout: 25 }),
    ).then(
      () => null,
      (err: Error) => err,
    );

    expect(failure?.message).toBe('Navigation timeout');
    expect(onUpdatedListeners).toHaveLength(0);
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

  // What the side panel looks like to the worker: one of the extension's own
  // pages. The sender check rejects anything else, so every policy_op test
  // here has to present a real one or it is testing the rejection path.
  const extensionPageSender = {
    id: 'test',
    url: 'chrome-extension://test/sidepanel.html',
  };

  function sendPolicyOp(op: unknown): Promise<unknown> {
    return new Promise((resolve, reject) => {
      const timer = setTimeout(
        () => reject(new Error('policy_op was never answered')),
        1000,
      );
      const kept = listener()?.(
        { type: 'policy_op', op },
        extensionPageSender,
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
    // would show an error for an operation whose write had already landed —
    // and since the switch no longer renders ahead of the write, it would
    // still read "human assist active" while the engine had already handed
    // the browser to the agent. A kill switch the user is told failed, and
    // then tries again, is worse than one that is merely slow.
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

// The withdrawal above is fail-closed but not free: the gate consumed and
// persisted the grant before the re-check ran, so a command the re-check then
// refuses has spent a one-shot approval without ever reaching the page. The
// surrounding test uses `click`, which consumes nothing, so this interaction
// was invisible until it was written down.
//
// Refunding is not the fix — consumption is what makes a grant
// un-double-spendable, and putting it back after an await would resurrect one
// a concurrent command is entitled to believe is gone. So the behaviour is
// pinned instead: one-shot means one attempt, and the wrong direction to fail
// in, which is worth a test precisely because it is easy to "fix" later
// without noticing what the fix would re-open.
describe('a withdrawn command has still spent its grant', () => {
  const APPROVED = 'https://approved.site';

  beforeEach(() => {
    store.set('policyState', {
      takeover: false,
      origins: { [APPROVED]: 'always' },
    });
    tabUrls.set(1, `${APPROVED}/form`);
    // The preflight has to classify the target as sensitive, or no
    // sensitive-field grant is needed and none is ever consumed — the test
    // would pass for the wrong reason.
    preflightResult = { sensitive: true };
  });

  it('burns a one-shot sensitive-field grant without reaching the page', async () => {
    store.set('policyState', {
      takeover: false,
      origins: { [APPROVED]: 'always' },
      grants: [
        {
          capability: 'sensitive-field',
          expiresAt: Date.now() + 60 * 60 * 1000,
          singleUse: true,
        },
      ],
    });

    // The user engages takeover after the gate has already consumed, but
    // before the command would have been dispatched.
    //
    // The hook must SPREAD the state it is replacing. This used to set a fresh
    // object literal with no `grants` key at all, which made the assertion
    // below read `?? []` on an object that had never held a grant — so it
    // passed even with the gate's consumption branch deleted outright, which
    // is the stronger half of the claim it was making. Spreading keeps the
    // consumed array visible as an empty one, which is what "burned" means.
    onNthStorageRead = {
      n: 2,
      run: () => {
        store.set('policyState', {
          ...(store.get('policyState') as Record<string, unknown>),
          takeover: true,
          origins: { [APPROVED]: 'always' },
        });
      },
    };

    const failure = await handleCommand(
      makeCommand('type', 1, { selector: '#pw', text: 'x' }),
    ).then(
      () => null,
      (err: Error & { name?: string; denial?: { reason: string } }) => err,
    );

    expect(failure?.denial?.reason).toBe('human_assist_active');
    // Never dispatched — the control worked.
    expect(sentToContentScript).toHaveLength(0);
    // And the grant is spent anyway. This is the assertion that makes the
    // behaviour a decision: it fails if someone "fixes" the asymmetry by
    // refunding, which is the fix that would re-open double-spend, and it
    // fails if the gate stops consuming at all. Verified: with the
    // consumption branch removed from `decideWithState`, this is red.
    const stored = store.get('policyState') as { grants?: unknown[] };
    expect(stored.grants ?? []).toHaveLength(0);
  });

  it('leaves the grant alone when the re-check does not fire', async () => {
    // The direction that would be a real regression: a healthy command must
    // still be able to spend exactly one grant, and a second identical
    // command must be denied.
    store.set('policyState', {
      takeover: false,
      origins: { [APPROVED]: 'always' },
      grants: [
        {
          capability: 'sensitive-field',
          expiresAt: Date.now() + 60 * 60 * 1000,
          singleUse: true,
        },
      ],
    });

    const first = await handleCommand(
      makeCommand('type', 1, { selector: '#pw', text: 'x' }),
    ).then(
      () => null,
      (err: Error) => err,
    );
    expect(first).toBeNull();

    const afterFirst = store.get('policyState') as { grants?: unknown[] };
    expect(afterFirst.grants ?? []).toHaveLength(0);

    const second = await handleCommand(
      makeCommand('type', 1, { selector: '#pw', text: 'x' }),
    ).then(
      () => null,
      (err: Error & { denial?: { reason: string } }) => err,
    );
    expect(second?.denial?.reason).toBe('approval_required');
  });
});

// The re-check's own behaviour, tested directly.
//
// The integration test above cannot identify which denial site fired: the gate
// and this function throw the same error with the same reason, and nothing
// observable happens between them, so a read-ordinal flip can land on the
// gate and still satisfy every assertion there. That is not a gap that a
// cleverer mock closes — it is a property of the code, and the only honest
// response is to test this function without going through the gate.
describe('assertTakeoverUnchanged on its own', () => {
  beforeEach(() => {
    store.set('policyState', {
      takeover: false,
      origins: { 'https://a.test': 'always' },
    });
  });

  it('throws human_assist_active when takeover is on', async () => {
    store.set('policyState', {
      takeover: true,
      origins: { 'https://a.test': 'always' },
    });

    const failure = await assertTakeoverUnchanged(
      'click',
      'https://a.test',
    ).then(
      () => null,
      (err: Error & { name?: string; denial?: { reason: string } }) => err,
    );

    expect(failure?.name).toBe('PolicyDeniedError');
    expect(failure?.denial?.reason).toBe('human_assist_active');
  });

  it('resolves when takeover is off', async () => {
    // The negative direction. A re-check that always denied would pass the
    // test above while making every DOM command unusable.
    expect(
      await assertTakeoverUnchanged('click', 'https://a.test'),
    ).toBeUndefined();
  });

  it('carries the origin so the denial names what was refused', async () => {
    store.set('policyState', {
      takeover: true,
      origins: { 'https://a.test': 'always' },
    });

    const failure = await assertTakeoverUnchanged(
      'type',
      'https://a.test',
    ).then(
      () => null,
      (
        err: Error & {
          denial?: { reason: string; command: string; origin?: string };
        },
      ) => err,
    );

    expect(failure?.denial?.command).toBe('type');
    expect(failure?.denial?.origin).toBe('https://a.test');
  });

  it('consumes nothing', async () => {
    // It must not be able to spend a grant. decideWithState only writes when
    // a decision carries `consume`, and takeoverDenied never does — so a
    // re-check that could consume would be a second way to drain one-shot
    // approvals, from a place with no gate in front of it.
    store.set('policyState', {
      takeover: true,
      origins: { 'https://a.test': 'always' },
      grants: [
        {
          capability: 'sensitive-field',
          expiresAt: Date.now() + 60_000,
          singleUse: true,
        },
      ],
    });

    await assertTakeoverUnchanged('type', 'https://a.test').catch(
      () => undefined,
    );

    const stored = store.get('policyState') as { grants?: unknown[] };
    expect(stored.grants ?? []).toHaveLength(1);
  });
});

// `policy_op` can switch Takeover off and mint a `sensitive-field` grant;
// `ws_command` reaches handleCommand. A page that could send either would
// release the user's kill switch and grant itself a password-field
// capability in one message.
//
// Nothing on the web can reach this listener today — no
// `externally_connectable`, no onMessageExternal, no postMessage bridge — so
// these are defence in depth. The point of testing defence in depth is that
// it is the half nobody checks after the day it is quietly made reachable.
describe('the worker rejects privileged messages from a non-extension sender', () => {
  const listener = () => onMessageListeners[0];

  beforeEach(() => {
    // Takeover on, so a released policy_op would be visible as a state change
    // and a rejected one is not.
    store.set('policyState', { takeover: true, origins: {}, grants: [] });
  });

  function send(request: unknown, sender: unknown): Promise<unknown> {
    return new Promise((resolve, reject) => {
      // A listener that never calls its sendResponse leaves this pending
      // forever, and the case then fails as a whole-suite timeout that names
      // no test. Rejecting here names the case, and the window is short
      // because nothing in this file does real I/O — the handler is either
      // synchronous or a resolved microtask away.
      const timer = setTimeout(
        () => reject(new Error('the worker listener never answered')),
        500,
      );
      listener()?.(request, sender, (response?: unknown) => {
        clearTimeout(timer);
        resolve(response);
      });
    });
  }

  // For handlers that legitimately never reply. Resolves with a sentinel
  // rather than hanging, because "no response" is the pass condition there
  // and a plain await cannot tell it from a listener that forgot to answer.
  const NO_REPLY = Symbol('no-reply');
  async function sendExpectingSilence(
    request: unknown,
    sender: unknown,
  ): Promise<unknown | typeof NO_REPLY> {
    const pending = send(request, sender);
    // The race below is what decides this case. If it loses, the rejection
    // `send` is about to produce has no consumer left, and an unhandled
    // rejection fails the run even though this case passed.
    pending.catch(() => {});
    const raced = await Promise.race([
      pending,
      new Promise((resolve) => setTimeout(() => resolve(NO_REPLY), 25)),
    ]);
    return raced;
  }

  const spoofed: [string, Record<string, unknown>][] = [
    [
      'a content script, which carries our own id and is still not a page',
      // The important case. sender.id is this extension's id for a content
      // script too, so an id-only check would wave this through; what
      // separates it is that sender.url is the page, not a
      // chrome-extension:// one.
      { id: 'test', url: 'https://evil.example/page', tab: { id: 7 } },
    ],
    [
      'another extension',
      {
        id: 'someotherid',
        url: 'chrome-extension://someotherid/sidepanel.html',
      },
    ],
    ['no sender at all', {}],
    [
      'a bare origin that merely starts with our id',
      { id: 'test', url: 'chrome-extension://testevil/x' },
    ],
    [
      // The one that separates `startsWith` from `includes`. The case above
      // cannot: `chrome-extension://testevil/x` contains no
      // `chrome-extension://test/` substring, so a substring check rejects it
      // for the same reason the prefix check does. This URL is an ordinary
      // https page that happens to *embed* our origin in a query string,
      // which a content script on it would report. Verified against an
      // `includes` implementation: rejected by `startsWith`, admitted by
      // `includes`.
      'a hostile page whose URL merely embeds our origin',
      { id: 'test', url: 'https://evil.example/?r=chrome-extension://test/x' },
    ],
    [
      // The reverse direction. Chrome assigns `sender.url` from the sending
      // extension, so a mismatched id is not reachable today — but the check
      // is there, and without this row deleting it changed nothing the suite
      // could see.
      'a mismatched id carrying our own origin in the url',
      { id: 'someotherid', url: 'chrome-extension://test/sidepanel.html' },
    ],
    ['a sender with no url', { id: 'test' }],
  ];

  for (const [name, sender] of spoofed) {
    it(`refuses policy_op from ${name}`, async () => {
      const response = (await send(
        { type: 'policy_op', op: { op: 'set_takeover', desired: false } },
        sender,
      )) as { error?: string } | undefined;

      expect(response?.error).toBe('forbidden_sender');
      // The state must be untouched: the whole point is that the operation
      // did not happen, not merely that the reply was rude.
      const stored = store.get('policyState') as { takeover?: boolean };
      expect(stored?.takeover).toBe(true);
    });
  }

  it('refuses a grant-minting policy_op from a spoofed sender', async () => {
    store.set('policyState', { takeover: false, origins: {}, grants: [] });

    const response = (await send(
      {
        type: 'policy_op',
        op: {
          op: 'grant_sensitive_field',
          targetKey: 'x',
          origin: 'https://evil.example',
        },
      },
      { id: 'test', url: 'https://evil.example/page' },
    )) as { error?: string } | undefined;

    expect(response?.error).toBe('forbidden_sender');
    const stored = store.get('policyState') as { grants?: unknown[] };
    expect(stored?.grants ?? []).toHaveLength(0);
  });

  it('refuses ws_command from a spoofed sender', async () => {
    // The origin is approved on purpose. With an unapproved origin the gate
    // denies first and the content-script array is empty whichever way the
    // guard goes, so the "never dispatched" assertion below was passing
    // because of the seed rather than because of the guard — and it would
    // have passed with the guard deleted entirely.
    store.set('policyState', {
      takeover: false,
      origins: { 'https://approved.site': 'always' },
    });
    tabUrls.set(1, 'https://approved.site/form');
    sentToContentScript.length = 0;

    const response = (await send(
      {
        type: 'ws_command',
        envelope: makeCommand('click', 1, { selector: '#a' }),
      },
      { id: 'test', url: 'https://evil.example/page' },
    )) as { error?: string } | undefined;

    expect(response?.error).toBe('forbidden_sender');
    // Drain the event loop before asserting. The dispatch path is several
    // awaits deep — policy gate, serialized queue, a storage read, then the
    // injection sequence — so a guard that ran *after* the handler had
    // already started would still produce the right reply, and a single tick
    // would still see an empty array because the dispatch had not landed.
    // Verified: with the guard moved after the handler starts, the click
    // reaches the page and this assertion is green unless the loop is
    // drained. 20 ticks is well past the chain's depth; it is a bound, not a
    // guess at a magic number that would need re-tuning if a step were added.
    for (let i = 0; i < 20; i += 1) await settle();
    expect(sentToContentScript).toHaveLength(0);
  });

  it('still admits the side panel and the settings page, and the write lands', async () => {
    // The negative half without the positive half is a worker that refuses
    // everything, which would look identical in production to a worker whose
    // policy layer is dead. So this asserts the *state* moved, not merely
    // that nothing was rejected: the previous version checked only the
    // absence of an error, so making applySetTakeover a no-op left it green.
    for (const url of [
      'chrome-extension://test/sidepanel.html',
      'chrome-extension://test/settings.html',
    ]) {
      store.set('policyState', { takeover: true, origins: {}, grants: [] });
      const response = (await send(
        { type: 'policy_op', op: { op: 'set_takeover', desired: false } },
        { id: 'test', url },
      )) as { error?: string } | undefined;

      expect(response?.error).toBeUndefined();
      const stored = store.get('policyState') as { takeover?: boolean };
      expect(stored.takeover).toBe(false);
    }
  });

  it('still admits the offscreen document on the message it actually sends', async () => {
    // The offscreen document is the only sender of `ws_command` — it holds
    // the WebSocket and relays what the proxy sends. The previous version
    // sent it a `policy_op`, which the offscreen document never sends, so a
    // guard that severed the command channel for `offscreen.html` still
    // passed: the extension's entire agent-facing path was untested.
    store.set('policyState', {
      takeover: false,
      origins: { 'https://approved.site': 'always' },
    });
    // The gate reads the origin off the tab, so approving an origin in
    // storage is not enough — without this the tab is about:blank, the
    // origin resolves to null and the command is denied as origin_blocked
    // before dispatch, for a reason that has nothing to do with the guard.
    tabUrls.set(1, 'https://approved.site/form');
    sentToContentScript.length = 0;

    const response = (await send(
      {
        type: 'ws_command',
        envelope: makeCommand('click', 1, { selector: '#a' }),
      },
      { id: 'test', url: 'chrome-extension://test/offscreen.html' },
    )) as { error?: string; status?: string } | undefined;

    expect(response?.error).toBeUndefined();
    // It reached the content script, which is the whole point.
    expect(sentToContentScript.length).toBeGreaterThan(0);
  });

  it('leaves the other message types alone', async () => {
    // The guard is scoped to the two privileged types. Widening it to every
    // type would break the offscreen handshake for no security gain, so ping
    // has to still get through from a content script.
    //
    // Asserting the *reply shape*, not merely that something came back: the
    // rejection is also a defined response, so `toBeDefined()` passed even
    // with the guard widened to everything — which is the exact regression
    // this case exists to catch. The pong marker is the discriminator.
    const response = (await send(
      { type: 'ping' },
      { id: 'test', url: 'https://evil.example/page' },
    )) as { type?: string; error?: string } | undefined;

    expect(response?.error).toBeUndefined();
    expect(response?.type).toBe('pong');
  });

  it('leaves ws_status alone, which returns no reply at all', async () => {
    // The offscreen document reports its socket state with this, and the
    // handler returns false without calling sendResponse — so the only thing
    // observable is the *service worker state* it records. A guard that
    // swallowed it would silently freeze the status indicator, with no
    // response to assert on.
    store.set('policyState', { takeover: false, origins: {} });
    sentToContentScript.length = 0;

    const response = await sendExpectingSilence(
      { type: 'ws_status', connected: true },
      { id: 'test', url: 'https://evil.example/page' },
    );
    // Silence is the pass condition, and specifically not a forbidden_sender
    // reply: had the guard been widened to cover this type, it would have
    // answered, and the offscreen socket state would have stopped updating
    // with nothing in any response to show for it.
    expect(response).toBe(NO_REPLY);
  });
});

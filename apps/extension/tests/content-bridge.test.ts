import { beforeEach, describe, expect, it } from 'bun:test';
import {
  type ChromeLike,
  ContentScriptUnavailableError,
  dispatchToContentScript,
  ensureContentScript,
} from '../src/content-bridge';

// ---- Mock chrome plumbing ----------------------------------------------------

interface ScriptingCall {
  target: { tabId: number };
  files: string[];
}

interface SendCall {
  tabId: number;
  message: Record<string, unknown>;
}

interface MockState {
  // tabId → Tab. Absence means "tab does not exist".
  tabs: Map<number, chrome.tabs.Tab>;
  // If true, non-ping sendMessage calls reject with "Receiving end does not exist."
  dropNonPing: boolean;
  // If true, chrome.scripting.executeScript rejects with the supplied message.
  injectionFailsWith: string | null;
  // If true, ping rejects until executeScript has injected the tab.
  requireInjectionForPing: boolean;
  // If true, ping always rejects regardless of injection state — simulates
  // a tab where the script ran but its listener never registered.
  pingAlwaysFails: boolean;
  // Recorded calls
  scriptingCalls: ScriptingCall[];
  sendCalls: SendCall[];
  // Tabs that executeScript has injected into.
  injected: Set<number>;
}

function createMock(initial?: Partial<MockState>): {
  chrome: ChromeLike;
  state: MockState;
} {
  const state: MockState = {
    tabs: new Map(),
    dropNonPing: false,
    injectionFailsWith: null,
    requireInjectionForPing: true,
    pingAlwaysFails: false,
    scriptingCalls: [],
    sendCalls: [],
    injected: new Set(),
    ...initial,
  };

  const chrome: ChromeLike = {
    tabs: {
      get: async (tabId) => {
        const tab = state.tabs.get(tabId);
        if (!tab) {
          throw new Error(`No tab with id: ${tabId}`);
        }
        return tab;
      },
      sendMessage: async (tabId, message) => {
        state.sendCalls.push({
          tabId,
          message: message as Record<string, unknown>,
        });
        const msg = message as { type?: string };
        if (msg.type === 'ping') {
          if (state.pingAlwaysFails) {
            throw new Error(
              'Could not establish connection. Receiving end does not exist.',
            );
          }
          if (state.requireInjectionForPing && !state.injected.has(tabId)) {
            throw new Error(
              'Could not establish connection. Receiving end does not exist.',
            );
          }
          return { type: 'pong' };
        }
        if (state.dropNonPing) {
          throw new Error(
            'Could not establish connection. Receiving end does not exist.',
          );
        }
        if (msg.type === 'command') {
          return {
            status: 'ok',
            data: {
              ok: true,
              command: (msg.payload as { command?: string })?.command,
            },
          };
        }
        if (msg.type === 'preflight') {
          return { status: 'ok', data: { sensitive: false } };
        }
        return undefined;
      },
    },
    scripting: {
      executeScript: async (injection) => {
        state.scriptingCalls.push(injection);
        if (state.injectionFailsWith) {
          throw new Error(state.injectionFailsWith);
        }
        state.injected.add(injection.target.tabId);
        return [{ result: undefined }];
      },
    },
  };

  return { chrome, state };
}

function tab(id: number, url = 'https://example.com/'): chrome.tabs.Tab {
  return {
    id,
    index: 0,
    windowId: 1,
    highlighted: false,
    active: false,
    pinned: false,
    url,
  } as chrome.tabs.Tab;
}

// ---- Tests -------------------------------------------------------------------

describe('ensureContentScript', () => {
  describe('happy path', () => {
    it('injects and waits when no listener is present', async () => {
      const { chrome, state } = createMock();
      state.tabs.set(1, tab(1));

      await ensureContentScript(chrome, 1);

      expect(state.scriptingCalls).toHaveLength(1);
      expect(state.scriptingCalls[0].files).toEqual(['content.js']);
    });

    it('skips injection when a listener is already responding', async () => {
      const { chrome, state } = createMock({ requireInjectionForPing: false });
      state.tabs.set(1, tab(1));

      await ensureContentScript(chrome, 1);

      expect(state.scriptingCalls).toHaveLength(0);
    });

    it('does not throw for about:blank when injection is still possible', async () => {
      const { chrome, state } = createMock();
      state.tabs.set(1, tab(1, 'about:blank'));

      await ensureContentScript(chrome, 1);

      expect(state.scriptingCalls).toHaveLength(1);
    });
  });

  describe('error reporting', () => {
    it('throws tab_not_found when the tab is gone', async () => {
      const { chrome } = createMock();

      const err = await ensureContentScript(chrome, 999).catch((e) => e);
      expect(err).toBeInstanceOf(ContentScriptUnavailableError);
      expect(err).toMatchObject({ reason: 'tab_not_found', tabId: 999 });
    });

    it('throws restricted_page for chrome:// URLs without injecting', async () => {
      const { chrome, state } = createMock();
      state.tabs.set(1, tab(1, 'chrome://settings/'));

      const err = await ensureContentScript(chrome, 1).catch((e) => e);
      expect(err).toMatchObject({
        reason: 'restricted_page',
        tabId: 1,
        url: 'chrome://settings/',
      });
      expect(state.scriptingCalls).toHaveLength(0);
    });

    it('throws restricted_page for chrome-extension:// URLs without injecting', async () => {
      const { chrome, state } = createMock();
      state.tabs.set(1, tab(1, 'chrome-extension://abc/popup.html'));

      const err = await ensureContentScript(chrome, 1).catch((e) => e);
      expect(err).toMatchObject({
        reason: 'restricted_page',
        url: 'chrome-extension://abc/popup.html',
      });
      expect(state.scriptingCalls).toHaveLength(0);
    });

    it('throws restricted_page for every other Chrome internal / browser-specific scheme', async () => {
      // Keep this in sync with RESTRICTED_URL_PREFIXES in content-bridge.ts.
      // Each entry MUST be rejected without calling executeScript — Chrome
      // throws "Cannot access contents of the page" on these schemes, and
      // surfacing that as `injection_failed` would hide the real reason.
      for (const url of [
        'chrome-devtools://devtools/bundled/devtools_app.html',
        'chrome-error://chromewebdata/',
        'chrome-search://local-ntp/local-ntp.html',
        'chrome-untrusted://terminal/terminal.html',
        'edge://settings/',
        'brave://settings/',
      ]) {
        const { chrome, state } = createMock();
        state.tabs.set(1, tab(1, url));

        const err = await ensureContentScript(chrome, 1).catch((e) => e);
        expect(err).toMatchObject({ reason: 'restricted_page', url });
        expect(state.scriptingCalls).toHaveLength(0);
      }
    });

    it('throws restricted_page when the tab has no URL yet', async () => {
      const { chrome, state } = createMock();
      state.tabs.set(1, { id: 1 } as chrome.tabs.Tab);

      const err = await ensureContentScript(chrome, 1).catch((e) => e);
      expect(err).toMatchObject({ reason: 'restricted_page', tabId: 1 });
      expect(state.scriptingCalls).toHaveLength(0);
    });

    it('throws injection_failed when executeScript rejects', async () => {
      const { chrome, state } = createMock({
        injectionFailsWith:
          'Cannot access contents of the page. Extension manifest must request permission to access this host.',
      });
      state.tabs.set(1, tab(1));

      const err = await ensureContentScript(chrome, 1).catch((e) => e);
      expect(err).toMatchObject({
        reason: 'injection_failed',
        tabId: 1,
        url: 'https://example.com/',
      });
    });

    it('throws no_listener when injection succeeds but the listener never responds', async () => {
      const { chrome, state } = createMock({
        pingAlwaysFails: true,
      });
      state.tabs.set(1, tab(1));

      const err = await ensureContentScript(chrome, 1).catch((e) => e);
      expect(err).toMatchObject({
        reason: 'no_listener',
        tabId: 1,
        url: 'https://example.com/',
      });
    });
  });
});

describe('dispatchToContentScript', () => {
  it('returns the content script data on success', async () => {
    const { chrome, state } = createMock();
    state.tabs.set(1, tab(1));

    const result = await dispatchToContentScript(chrome, 1, {
      type: 'command',
      payload: {
        command: 'scroll',
        tabId: 1,
        params: { selector: 'page', x: 0, y: 500 },
      },
    });

    expect(result).toEqual({ ok: true, command: 'scroll' });
    expect(state.sendCalls.some((c) => c.message.type === 'command')).toBe(
      true,
    );
  });

  it('wraps the mid-command "Receiving end does not exist" as no_listener when the tab is still alive', async () => {
    const { chrome, state } = createMock({ requireInjectionForPing: false });
    state.tabs.set(1, tab(1));
    // Ping succeeds so ensureContentScript passes; commands then fail.
    // Tab stays registered in `state.tabs`, so chrome.tabs.get(tabId)
    // still resolves — listener is gone, not the tab.
    state.dropNonPing = true;

    const err = await dispatchToContentScript(chrome, 1, {
      type: 'command',
      payload: { command: 'scroll' },
    }).catch((e) => e);
    expect(err).toBeInstanceOf(ContentScriptUnavailableError);
    expect(err).toMatchObject({ reason: 'no_listener', tabId: 1 });
  });

  it('classifies the same mid-command error as tab_not_found when the tab was closed', async () => {
    // Chrome emits "Receiving end does not exist" for both "listener gone"
    // and "tab gone"; without the re-check, an agent would retry blindly
    // on a tab that will never come back. Simulate "tab closed mid-command"
    // by making `tabs.get` reject on the second call (the catch-block's
    // re-check) while succeeding on the first (ensureContentScript's
    // existence check).
    const { chrome, state } = createMock({ requireInjectionForPing: false });
    state.tabs.set(1, tab(1));
    const realGet = chrome.tabs.get;
    let getCalls = 0;
    (chrome.tabs as unknown as { get: typeof realGet }).get = async (
      tabId: number,
    ) => {
      getCalls += 1;
      if (getCalls >= 2) throw new Error(`No tab with id: ${tabId}`);
      return realGet(tabId);
    };
    state.dropNonPing = true;

    const err = await dispatchToContentScript(chrome, 1, {
      type: 'command',
      payload: { command: 'scroll' },
    }).catch((e) => e);
    expect(err).toBeInstanceOf(ContentScriptUnavailableError);
    expect(err).toMatchObject({ reason: 'tab_not_found', tabId: 1 });
  });

  it('propagates content script status:error responses as Error', async () => {
    const { chrome, state } = createMock({ requireInjectionForPing: false });
    state.tabs.set(1, tab(1));
    // Override sendMessage to return a status:error for commands.
    (
      chrome.tabs as unknown as {
        sendMessage: (tabId: number, message: unknown) => Promise<unknown>;
      }
    ).sendMessage = async (tabId, message) => {
      state.sendCalls.push({
        tabId,
        message: message as Record<string, unknown>,
      });
      const msg = message as { type?: string };
      if (msg.type === 'ping') return { type: 'pong' };
      return { status: 'error', error: 'Element not found: #missing' };
    };

    await expect(
      dispatchToContentScript(chrome, 1, {
        type: 'command',
        payload: { command: 'click', selector: '#missing' },
      }),
    ).rejects.toThrow('Element not found: #missing');
  });
});

beforeEach(() => {});

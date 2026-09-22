// Content-script bridge: encapsulates the inject → wait-for-listener → send
// loop so the rest of the SW can call it through a single, testable seam.
//
// The previous implementation blocked on a hard 100ms sleep after
// `executeScript` and let every failure surface as the generic
// "Could not establish connection. Receiving end does not exist." — which
// masked tab-closed, restricted-page, and CSP-denied failures behind one
// opaque message. This wrapper classifies each failure mode so callers (and
// end users via the MCP tool error path) get an actionable reason code.

export interface ChromeLike {
  tabs: {
    sendMessage: (tabId: number, message: unknown) => Promise<unknown>;
    get: (tabId: number) => Promise<chrome.tabs.Tab>;
  };
  scripting: {
    executeScript: (injection: {
      target: { tabId: number };
      files: string[];
    }) => Promise<unknown>;
  };
}

const CONTENT_SCRIPT_FILES = ['content.js'];

const PING_INTERVAL_MS = 50;
const LISTENER_WAIT_TIMEOUT_MS = 2000;

// URLs where chrome.scripting.executeScript is not allowed. Content
// scripts can never reach these pages — surface a clean error instead of
// letting the generic "Receiving end does not exist" mask the real cause.
const RESTRICTED_URL_PREFIXES = [
  'chrome://',
  'chrome-extension://',
  'edge://',
  'devtools://',
  'view-source:',
  'data:',
];

const RECEIVING_END_PATTERN = /Receiving end does not exist/i;

export type ContentScriptUnavailableReason =
  | 'tab_not_found'
  | 'restricted_page'
  | 'injection_failed'
  | 'no_listener';

export class ContentScriptUnavailableError extends Error {
  readonly reason: ContentScriptUnavailableReason;
  readonly tabId: number;
  readonly url: string | undefined;

  constructor(
    message: string,
    reason: ContentScriptUnavailableReason,
    tabId: number,
    url?: string,
  ) {
    super(message);
    this.name = 'ContentScriptUnavailableError';
    this.reason = reason;
    this.tabId = tabId;
    this.url = url;
  }
}

function isRestrictedUrl(url: string | undefined): boolean {
  if (!url) return true;
  return RESTRICTED_URL_PREFIXES.some((prefix) => url.startsWith(prefix));
}

function isReceivingEndError(err: unknown): boolean {
  return err instanceof Error && RECEIVING_END_PATTERN.test(err.message);
}

async function pingContentScript(
  chrome: ChromeLike,
  tabId: number,
): Promise<boolean> {
  try {
    const response = (await chrome.tabs.sendMessage(tabId, {
      type: 'ping',
    })) as { type?: string } | undefined;
    return response?.type === 'pong';
  } catch {
    return false;
  }
}

async function waitForListener(
  chrome: ChromeLike,
  tabId: number,
  timeoutMs: number,
): Promise<boolean> {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    if (await pingContentScript(chrome, tabId)) return true;
    await new Promise((resolve) => setTimeout(resolve, PING_INTERVAL_MS));
  }
  return false;
}

/**
 * Make sure a content-script listener is registered on the given tab.
 *
 * Distinguishes between tab-gone, restricted-page, injection-failed, and
 * listener-never-registered. Each path produces a distinct reason code on
 * {@link ContentScriptUnavailableError} so the upstream tool layer can
 * surface an actionable message instead of the generic Chrome error.
 */
export async function ensureContentScript(
  chrome: ChromeLike,
  tabId: number,
): Promise<void> {
  let tab: chrome.tabs.Tab;
  try {
    tab = await chrome.tabs.get(tabId);
  } catch (err) {
    throw new ContentScriptUnavailableError(
      `tab ${tabId} is no longer available${
        err instanceof Error ? `: ${err.message}` : ''
      }`,
      'tab_not_found',
      tabId,
    );
  }

  if (isRestrictedUrl(tab.url)) {
    throw new ContentScriptUnavailableError(
      `cannot interact with restricted page${
        tab.url ? ` (${tab.url})` : ' — tab has no URL yet'
      }`,
      'restricted_page',
      tabId,
      tab.url,
    );
  }

  // Listener already responsive — nothing to do.
  if (await pingContentScript(chrome, tabId)) return;

  try {
    await chrome.scripting.executeScript({
      target: { tabId },
      files: CONTENT_SCRIPT_FILES,
    });
  } catch (err) {
    throw new ContentScriptUnavailableError(
      `failed to inject content script into ${tab.url}: ${
        err instanceof Error ? err.message : String(err)
      }`,
      'injection_failed',
      tabId,
      tab.url,
    );
  }

  if (!(await waitForListener(chrome, tabId, LISTENER_WAIT_TIMEOUT_MS))) {
    throw new ContentScriptUnavailableError(
      `content script did not register a listener in ${tab.url} within ${LISTENER_WAIT_TIMEOUT_MS}ms`,
      'no_listener',
      tabId,
      tab.url,
    );
  }
}

/**
 * Send a message to the content script on the given tab, ensuring it's
 * loaded and listening first.
 *
 * Wraps a mid-command "Receiving end does not exist" as
 * {@link ContentScriptUnavailableError} with reason `no_listener` so the
 * caller doesn't have to pattern-match the Chrome error string.
 */
export async function dispatchToContentScript(
  chrome: ChromeLike,
  tabId: number,
  message: Record<string, unknown>,
): Promise<unknown> {
  await ensureContentScript(chrome, tabId);

  try {
    const response = (await chrome.tabs.sendMessage(tabId, message)) as
      | { status?: string; data?: unknown; error?: string }
      | undefined;
    if (response?.status === 'error') {
      throw new Error(response.error ?? 'Content script command failed');
    }
    if (response?.status === 'ok') {
      return response.data;
    }
    throw new Error('Content script did not respond');
  } catch (err) {
    if (isReceivingEndError(err)) {
      throw new ContentScriptUnavailableError(
        `content script on tab ${tabId} stopped responding mid-command`,
        'no_listener',
        tabId,
      );
    }
    throw err;
  }
}

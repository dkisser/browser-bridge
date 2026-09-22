// Service Worker: manages the offscreen document and executes browser commands.
// The WebSocket connection lives in the offscreen document (it persists when SW sleeps).
// Commands arrive from offscreen via chrome.runtime.sendMessage, are executed here
// using Chrome APIs, and responses are returned via the sendResponse callback.

import {
  blocklistHit,
  type CommandResultMap,
  type CommandType,
  type Denial,
  evaluatePolicy,
  humanDenialMessage,
  originOf,
  type PolicyContext,
  SENSITIVE_FIELD_RECHECK_ERROR,
} from '@browser-bridge/shared';
import {
  type ChromeLike,
  ContentScriptUnavailableError,
  dispatchToContentScript as dispatchToContentScriptRaw,
} from './content-bridge';
import {
  clearSessionScoped,
  decideWithState,
  getPolicyState,
  type PolicyState,
  recordDenial,
  updateBadge,
  updatePolicyState,
} from './policy-state';

// Bind the testable dispatch helper to the live chrome global once so the
// service-worker hot path doesn't pay the lookup on every command.
const dispatchToContentScript = (
  tabId: number,
  message: Record<string, unknown>,
): Promise<unknown> =>
  dispatchToContentScriptRaw(
    globalThis.chrome as unknown as ChromeLike,
    tabId,
    message,
  );

const OFFSCREEN_DOCUMENT_URL = 'offscreen.html';

class PolicyDeniedError extends Error {
  readonly denial: Denial;

  constructor(denial: Denial) {
    super(humanDenialMessage(denial));
    this.name = 'PolicyDeniedError';
    this.denial = denial;
  }
}

let _wsConnected = false;
let creatingOffscreen: Promise<void> | null = null;

async function ensureOffscreenDocument(): Promise<void> {
  const existingContexts = await chrome.runtime.getContexts({
    contextTypes: [chrome.runtime.ContextType.OFFSCREEN_DOCUMENT],
    documentUrls: [chrome.runtime.getURL(OFFSCREEN_DOCUMENT_URL)],
  });

  if (existingContexts.length > 0) return;

  if (creatingOffscreen) {
    await creatingOffscreen;
    return;
  }

  creatingOffscreen = chrome.offscreen
    .createDocument({
      url: OFFSCREEN_DOCUMENT_URL,
      reasons: [chrome.offscreen.Reason.WEB_RTC],
      justification: 'Maintain persistent WebSocket connection to Local Proxy',
    })
    .then(() => {
      creatingOffscreen = null;
    })
    .catch((err: Error) => {
      creatingOffscreen = null;
      throw err;
    });

  await creatingOffscreen;
}

interface CommandMessage {
  id: string;
  type: 'command';
  browserId: string;
  payload: {
    command: CommandType;
    tabId: number;
    params: Record<string, unknown>;
  };
  timestamp: number;
}

async function queryOffscreenStatus(): Promise<boolean> {
  try {
    await ensureOffscreenDocument();
    const response = await chrome.runtime.sendMessage({ type: 'ws_ping' });
    return response?.connected ?? false;
  } catch {
    return false;
  }
}

// The offscreen document cannot read chrome.storage, so the SW owns the
// pairing token and pushes it here. Idempotent: safe to call on every
// service-worker wake and on every pairing-token change.
async function connectOffscreen(): Promise<void> {
  await ensureOffscreenDocument();
  const state = await getPolicyState();
  await chrome.runtime
    .sendMessage({ type: 'connect_ws', token: state.pairingToken })
    .catch(() => {});
}

// Read-only commands bypass the gate entirely (no origin lookups at all).
const GATE_EXEMPT_COMMANDS = new Set<CommandType>(['tab:list', 'pageinfo']);

// Policy enforcement point (ADR-0006..0009): every command is evaluated
// against the shared policy core before it executes. Throws PolicyDeniedError
// when the decision is a denial; returns the policy state the decision was
// made against so handlers can reuse it.
async function applyPolicyGate(
  command: CommandType,
  tabId: number | undefined,
  params: Record<string, unknown>,
): Promise<{
  state: PolicyState;
  origin: string | null;
  sensitiveApproved: boolean;
}> {
  const notApproved = async () => ({
    state: await getPolicyState(),
    origin: null,
    sensitiveApproved: false,
  });
  if (GATE_EXEMPT_COMMANDS.has(command)) return notApproved();

  let origin: string | null = null;
  let isActiveTab = false;

  if (command === 'navigate' || command === 'tab:new') {
    // A blank tab:new opens no page — there is no navigation target to gate.
    if (command === 'tab:new' && !params.url) return notApproved();
    origin = originOf(params.url as string | undefined);
  } else if (typeof tabId === 'number') {
    const tab = await chrome.tabs.get(tabId);
    origin = originOf(tab.url);
    if (command === 'screenshot') {
      const [activeTab] = await chrome.tabs.query({
        windowId: tab.windowId,
        active: true,
      });
      isActiveTab = activeTab?.id === tabId;
    }
  }

  let sensitiveField: boolean | undefined;
  if (command === 'type' && origin !== null) {
    // Protected contexts deny `type` below regardless of the field kind, and
    // content scripts cannot be injected there — skip the preflight.
    const preflight = await preflightSelector(tabId, params.selector as string);
    sensitiveField = preflight.sensitive;
  }

  // Decide against fresh state inside the serialized write queue: the
  // decision and any grant consumption below are atomic with respect to
  // concurrent commands, so a singleUse grant cannot be double-consumed.
  const { decision, state } = await decideWithState((fresh) => {
    const ctx: PolicyContext = {
      takeover: fresh.takeover,
      origin,
      blocklistHit: blocklistHit(origin, fresh.blockedOrigins),
      grants: fresh.grants,
      ...(typeof tabId === 'number' ? { tabId } : {}),
      ...(sensitiveField !== undefined ? { sensitiveField } : {}),
      ...(params.submit === true ? { submit: true } : {}),
    };
    if (origin !== null) {
      if (fresh.deniedOrigins[origin] !== undefined) {
        ctx.originState = 'denied';
      } else if (fresh.origins[origin] !== undefined) {
        ctx.originState = 'approved';
      }
    }
    if (command === 'screenshot') {
      ctx.isVisibleApprovedTab = isActiveTab && ctx.originState === 'approved';
    }
    if (command === 'tab:close') {
      ctx.isAgentTab = fresh.agentTabs.includes(params.tabId as number);
    }
    return evaluatePolicy(command, ctx);
  });

  if (!decision.allow) {
    await recordDenial(decision.denial);
    await updateBadge();
    throw new PolicyDeniedError(decision.denial);
  }
  const sensitiveApproved = (decision.consume ?? []).some(
    (grant) => grant.capability === 'sensitive-field',
  );
  return { state, origin, sensitiveApproved };
}

async function handleCommand(
  msg: CommandMessage,
): Promise<CommandResultMap[CommandType]> {
  const { payload } = msg;
  const { command, tabId, params } = payload;

  const { origin, sensitiveApproved } = await applyPolicyGate(
    command,
    tabId,
    params,
  );

  switch (command) {
    case 'navigate': {
      if (typeof tabId !== 'number') {
        throw new Error('Missing required tabId');
      }
      const tab = tabId;
      await chrome.tabs.update(tab, { url: params.url as string });
      await new Promise<void>((resolve) => {
        const listener = (
          updatedTabId: number,
          changeInfo: chrome.tabs.TabChangeInfo,
        ) => {
          if (updatedTabId === tab && changeInfo.status === 'complete') {
            chrome.tabs.onUpdated.removeListener(listener);
            resolve();
          }
        };
        chrome.tabs.onUpdated.addListener(listener);
      });
      const updatedTab = await chrome.tabs.get(tab);
      return { url: updatedTab.url, title: updatedTab.title };
    }

    case 'goBack': {
      if (typeof tabId !== 'number') {
        throw new Error('Missing required tabId');
      }
      const tab = tabId;
      await chrome.tabs.goBack(tab);
      return { ok: true };
    }

    case 'goForward': {
      if (typeof tabId !== 'number') {
        throw new Error('Missing required tabId');
      }
      const tab = tabId;
      await chrome.tabs.goForward(tab);
      return { ok: true };
    }

    case 'refresh': {
      if (typeof tabId !== 'number') {
        throw new Error('Missing required tabId');
      }
      const tab = tabId;
      await chrome.tabs.reload(tab);
      return { ok: true };
    }

    case 'tab:list': {
      const tabs = await chrome.tabs.query({});
      return tabs.map((t) => ({
        id: t.id,
        url: t.url,
        title: t.title,
        active: t.active,
        windowId: t.windowId,
      }));
    }

    case 'tab:new': {
      const active = params.active === true;
      const newTab = await chrome.tabs.create({
        url: params.url as string | undefined,
        active,
      });
      if (newTab.id !== undefined) {
        await updatePolicyState((fresh) => ({
          agentTabs: [...fresh.agentTabs, newTab.id as number],
        }));
      }
      return { id: newTab.id, url: newTab.url };
    }

    case 'tab:close': {
      await chrome.tabs.remove(params.tabId as number);
      await updatePolicyState((fresh) => ({
        agentTabs: fresh.agentTabs.filter((id) => id !== params.tabId),
      }));
      return { ok: true };
    }

    case 'tab:switch': {
      const targetTabId = params.tabId as number;
      const tab = await chrome.tabs.update(targetTabId, { active: true });
      return { id: tab.id, url: tab.url, title: tab.title };
    }

    case 'pageinfo': {
      if (typeof tabId !== 'number') {
        throw new Error('Missing required tabId');
      }
      const tab = tabId;
      const t = await chrome.tabs.get(tab);
      return { id: t.id, url: t.url, title: t.title, active: t.active };
    }

    case 'screenshot': {
      if (typeof tabId !== 'number') {
        throw new Error('Missing required tabId');
      }
      const tab = tabId;
      const activeTab = await chrome.tabs.get(tab);
      const dataUrl = await chrome.tabs.captureVisibleTab(activeTab.windowId, {
        format: 'png',
      });
      if (!dataUrl) {
        throw new Error(
          'Unable to capture screenshot: tab must be active in a visible window',
        );
      }
      return { dataUrl };
    }

    case 'wait:navigation': {
      const timeout = (params.timeout as number) || 10000;
      if (typeof tabId !== 'number') {
        throw new Error('Missing required tabId');
      }
      const tab = tabId;
      await new Promise<void>((resolve, reject) => {
        let settled = false;
        const cleanup = (): void => {
          clearTimeout(timer);
          chrome.tabs.onUpdated.removeListener(listener);
        };
        const finish = (error?: Error): void => {
          if (settled) return;
          settled = true;
          cleanup();
          if (error) {
            reject(error);
          } else {
            resolve();
          }
        };
        const timer = setTimeout(() => {
          finish(new Error('Navigation timeout'));
        }, timeout);
        const listener = (
          updatedTabId: number,
          changeInfo: chrome.tabs.TabChangeInfo,
        ) => {
          if (updatedTabId === tab && changeInfo.status === 'complete') {
            finish();
          }
        };
        chrome.tabs.onUpdated.addListener(listener);
        // The navigation may already be complete when this command arrives
        // (e.g. a remedial wait after `navigate` timed out on a redirect
        // chain) — no future onUpdated event will fire, so check the current
        // status instead of waiting for an event that never comes.
        chrome.tabs
          .get(tab)
          .then((current) => {
            if (current.status === 'complete') finish();
          })
          .catch(() => {
            // Tab lookup failed; rely on the event listener and timeout.
          });
      });
      const t = await chrome.tabs.get(tab);
      return { url: t.url, title: t.title };
    }

    // DOM commands — forward to content script
    case 'click':
    case 'type':
    case 'select':
    case 'scroll':
    case 'hover':
    case 'gettext':
    case 'gethtml':
    case 'snapshot':
    case 'wait:element': {
      // A sensitive-field grant consumed at the gate authorizes this one type
      // command; the content script re-verifies the field at execution time.
      const forwarded =
        command === 'type' && sensitiveApproved
          ? {
              ...payload,
              params: { ...payload.params, sensitiveApproved: true },
            }
          : payload;
      try {
        return (await sendToContentScript(
          tabId,
          forwarded,
        )) as CommandResultMap[typeof command];
      } catch (err) {
        // Execution-point recheck: the field became sensitive between the
        // preflight and the write. Surface it as a policy denial so the agent
        // asks for approval instead of retrying blindly.
        if (
          err instanceof Error &&
          err.message === SENSITIVE_FIELD_RECHECK_ERROR
        ) {
          const denial: Denial = {
            reason: 'approval_required',
            command,
            ...(origin !== null ? { origin } : {}),
            capability: 'sensitive-field',
            detail:
              'target field was classified sensitive at execution time — obtain approval and retry',
          };
          await recordDenial(denial);
          await updateBadge();
          throw new PolicyDeniedError(denial);
        }
        throw err;
      }
    }

    default:
      throw new Error(`Unknown command: ${command}`);
  }
}

async function sendToContentScript(
  tabId: number | undefined,
  payload: Record<string, unknown>,
): Promise<unknown> {
  if (typeof tabId !== 'number') {
    throw new Error('Missing required tabId');
  }
  return await dispatchToContentScript(tabId, { type: 'command', payload });
}

// Policy preflight for `type`: asks the content script to classify the
// target element (password / credit-card) before the policy decision.
async function preflightSelector(
  tabId: number | undefined,
  selector: string,
): Promise<{ sensitive: boolean }> {
  if (typeof tabId !== 'number') {
    throw new Error('Missing required tabId');
  }
  const data = await dispatchToContentScript(tabId, {
    type: 'preflight',
    selector,
  });
  return data as { sensitive: boolean };
}

// Message handler: receives commands from offscreen doc, side panel, and content scripts
chrome.runtime.onMessage.addListener((request, _sender, sendResponse) => {
  // Command from offscreen document (originating from Local Proxy)
  if (request.type === 'ws_command') {
    const envelope = request.envelope as CommandMessage;
    handleCommand(envelope)
      .then((data) => sendResponse({ status: 'ok', data }))
      .catch((err: Error) => {
        if (err instanceof PolicyDeniedError) {
          sendResponse({
            status: 'error',
            error: err.denial.reason,
            message: humanDenialMessage(err.denial),
            denied: err.denial,
          });
          return;
        }
        // ContentScriptUnavailableError carries a structured reason so MCP
        // tools can attach a recovery hint (see withRecoveryHint in the
        // websocket command-client). Surface it alongside the message.
        if (err instanceof ContentScriptUnavailableError) {
          sendResponse({
            status: 'error',
            error: err.reason,
            message: err.message,
            reason: err.reason,
          });
          return;
        }
        sendResponse({ status: 'error', error: err.message });
      });
    return true; // async response
  }

  // Status update from offscreen document
  if (request.type === 'ws_status') {
    _wsConnected = request.connected;
    return false;
  }

  // Popup: ping connection status — query offscreen for real status
  if (request.type === 'ping') {
    queryOffscreenStatus()
      .then((connected) => sendResponse({ type: 'pong', connected }))
      .catch(() => sendResponse({ type: 'pong', connected: false }));
    return true;
  }

  // Popup: trigger connection
  if (request.type === 'connect') {
    connectOffscreen()
      .then(() => sendResponse({ type: 'connected' }))
      .catch((err: Error) => {
        sendResponse({ type: 'error', message: err.message });
      });
    return true;
  }

  // Side panel: open the read-only settings page in a new tab. The page
  // itself is a separate extension options page (see manifest options_ui).
  if (request.type === 'open-options') {
    chrome.runtime
      .openOptionsPage()
      .then(() => sendResponse({ status: 'ok' }))
      .catch((err: Error) =>
        sendResponse({ status: 'error', error: err.message }),
      );
    return true;
  }

  return false;
});

// Keep agentTabs in sync with reality: tabs the user closed are no longer
// agent tabs.
chrome.tabs.onRemoved.addListener((tabId) => {
  updatePolicyState((state) => ({
    agentTabs: state.agentTabs.filter((id) => id !== tabId),
  })).catch(console.error);
});

// Downloads started by agent-created tabs are paused until the human
// resumes or cancels them in the side panel (unless human assist is active,
// in which case the user is in charge already).
chrome.downloads.onCreated.addListener((item) => {
  // Installed @types/chrome predates DownloadItem.tabId (Chrome 116+).
  const download = item as chrome.downloads.DownloadItem & {
    tabId?: number;
  };
  if (download.tabId === undefined || download.tabId < 0) return;
  void (async () => {
    try {
      await chrome.downloads.pause(download.id);
    } catch {
      // Pause can race with a download that already completed; ignore.
    }
    const recorded = await updatePolicyState((state) => {
      if (
        state.takeover ||
        !state.agentTabs.includes(download.tabId as number)
      ) {
        return null;
      }
      return {
        pendingDownloads: [
          ...state.pendingDownloads,
          {
            id: download.id,
            filename: download.filename,
            url: download.url,
          },
        ],
      };
    }).then((state) =>
      state.pendingDownloads.some((entry) => entry.id === download.id),
    );
    if (recorded) await updateBadge();
  })().catch(console.error);
});

// Re-pairing rotates the pairing token. The offscreen cannot watch storage
// itself (only chrome.runtime is available there), so the SW forwards token
// changes to it — this also covers clears (token → null), which tear the
// socket down until a new pairing completes.
chrome.storage.onChanged.addListener((changes, area) => {
  if (area !== 'local' || !changes.policyState) return;
  const oldToken = (
    changes.policyState.oldValue as { pairingToken?: string | null } | undefined
  )?.pairingToken;
  const newToken = (
    changes.policyState.newValue as { pairingToken?: string | null } | undefined
  )?.pairingToken;
  if (newToken === oldToken) return;
  connectOffscreen().catch(console.error);
});

async function initialize(): Promise<void> {
  await clearSessionScoped();
  await updateBadge();
  await connectOffscreen();
}

// The human surface is the side panel (ADR-0010). Make the extension icon
// a one-click trigger to open the panel on the active tab. Re-applied on
// every SW wake so a disable/re-enable in chrome://extensions recovers the
// behaviour without waiting for the next browser restart.
async function ensurePanelBehavior(): Promise<void> {
  try {
    await chrome.sidePanel.setPanelBehavior({ openPanelOnActionClick: true });
  } catch (error: unknown) {
    console.error(error);
  }
}

// Initialize offscreen document on extension install/startup
chrome.runtime.onInstalled.addListener(() => {
  initialize().catch(console.error);
  ensurePanelBehavior().catch(console.error);
});

chrome.runtime.onStartup.addListener(() => {
  initialize().catch(console.error);
  ensurePanelBehavior().catch(console.error);
});

// Also try on SW wake — if the offscreen was killed or the proxy restarted
// while we slept, this reconnects it with the stored token. The panel
// behaviour must also be re-applied on every wake (findings #4, #5).
connectOffscreen().catch(console.error);
ensurePanelBehavior().catch(console.error);

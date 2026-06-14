// Service Worker: manages the offscreen document and executes browser commands.
// The WebSocket connection lives in the offscreen document (it persists when SW sleeps).
// Commands arrive from offscreen via chrome.runtime.sendMessage, are executed here
// using Chrome APIs, and responses are returned via the sendResponse callback.

const OFFSCREEN_DOCUMENT_URL = 'offscreen.html';

let wsConnected = false;
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
    command: string;
    tabId?: number;
    params: Record<string, unknown>;
  };
  timestamp: number;
}

async function handleCommand(msg: CommandMessage): Promise<unknown> {
  const { payload } = msg;
  const { command, tabId, params } = payload;

  switch (command) {
    case 'navigate': {
      const tab = tabId ?? (await getActiveTabId());
      await chrome.tabs.update(tab, { url: params.url as string });
      await new Promise<void>((resolve) => {
        const listener = (updatedTabId: number, changeInfo: chrome.tabs.TabChangeInfo) => {
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
      const tab = tabId ?? (await getActiveTabId());
      await chrome.tabs.goBack(tab);
      return { ok: true };
    }

    case 'goForward': {
      const tab = tabId ?? (await getActiveTabId());
      await chrome.tabs.goForward(tab);
      return { ok: true };
    }

    case 'refresh': {
      const tab = tabId ?? (await getActiveTabId());
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
      const newTab = await chrome.tabs.create({ url: params.url as string | undefined });
      return { id: newTab.id, url: newTab.url };
    }

    case 'tab:close': {
      await chrome.tabs.remove(params.tabId as number);
      return { ok: true };
    }

    case 'tab:switch': {
      const targetTabId = params.tabId as number;
      const tab = await chrome.tabs.update(targetTabId, { active: true });
      return { id: tab.id, url: tab.url, title: tab.title };
    }

    case 'pageinfo': {
      const tab = tabId ?? (await getActiveTabId());
      const t = await chrome.tabs.get(tab);
      return { id: t.id, url: t.url, title: t.title, active: t.active };
    }

    case 'screenshot': {
      const tab = tabId ?? (await getActiveTabId());
      const activeTab = await chrome.tabs.get(tab);
      const dataUrl = await chrome.tabs.captureVisibleTab(activeTab.windowId, { format: 'png' });
      return { dataUrl };
    }

    case 'wait:navigation': {
      const timeout = (params.timeout as number) || 10000;
      const tab = tabId ?? (await getActiveTabId());
      await new Promise<void>((resolve, reject) => {
        const timer = setTimeout(() => {
          chrome.tabs.onUpdated.removeListener(listener);
          reject(new Error('Navigation timeout'));
        }, timeout);
        const listener = (updatedTabId: number, changeInfo: chrome.tabs.TabChangeInfo) => {
          if (updatedTabId === tab && changeInfo.status === 'complete') {
            clearTimeout(timer);
            chrome.tabs.onUpdated.removeListener(listener);
            resolve();
          }
        };
        chrome.tabs.onUpdated.addListener(listener);
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
    case 'wait:element':
      return await sendToContentScript(tabId, payload);

    default:
      throw new Error(`Unknown command: ${command}`);
  }
}

async function sendToContentScript(tabId: number | undefined, payload: Record<string, unknown>): Promise<unknown> {
  const tab = tabId ?? (await getActiveTabId());

  try {
    const response = await chrome.tabs.sendMessage(tab, { type: 'ping' });
    if (response?.type === 'pong') {
      return await chrome.tabs.sendMessage(tab, { type: 'command', payload });
    }
  } catch {
    // Content script not injected, inject it
  }

  await chrome.scripting.executeScript({
    target: { tabId: tab },
    files: ['content.js'],
  });

  await new Promise((resolve) => setTimeout(resolve, 100));
  return await chrome.tabs.sendMessage(tab, { type: 'command', payload });
}

async function getActiveTabId(): Promise<number> {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  if (!tab?.id) throw new Error('No active tab found');
  return tab.id;
}

// Message handler: receives commands from offscreen doc, popup, and content scripts
chrome.runtime.onMessage.addListener((request, _sender, sendResponse) => {
  // Command from offscreen document (originating from Local Proxy)
  if (request.type === 'ws_command') {
    const envelope = request.envelope as CommandMessage;
    handleCommand(envelope)
      .then((data) => sendResponse({ status: 'ok', data }))
      .catch((err: Error) => sendResponse({ status: 'error', error: err.message }));
    return true; // async response
  }

  // Status update from offscreen document
  if (request.type === 'ws_status') {
    wsConnected = request.connected;
    return false;
  }

  // Popup: ping connection status
  if (request.type === 'ping') {
    sendResponse({ type: 'pong', connected: wsConnected });
    return false;
  }

  // Popup: trigger connection
  if (request.type === 'connect') {
    ensureOffscreenDocument()
      .then(() => {
        // Ask offscreen to connect
        chrome.runtime.sendMessage({ type: 'connect_ws' }).catch(() => {});
        sendResponse({ type: 'connected' });
      })
      .catch((err: Error) => {
        sendResponse({ type: 'error', message: err.message });
      });
    return true;
  }

  return false;
});

// Initialize offscreen document on extension install/startup
chrome.runtime.onInstalled.addListener(() => {
  ensureOffscreenDocument().catch(console.error);
});

chrome.runtime.onStartup.addListener(() => {
  ensureOffscreenDocument().catch(console.error);
});

// Also try on SW wake — if offscreen was somehow killed, recreate it
ensureOffscreenDocument().catch(console.error);

import { LOCAL_WS_PORT } from '@my/shared';

const LOCAL_WS_URL = `ws://localhost:${LOCAL_WS_PORT}`;

let ws: WebSocket | null = null;
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;

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

function connectToLocalProxy(): void {
  if (ws && ws.readyState === WebSocket.OPEN) return;

  ws = new WebSocket(LOCAL_WS_URL);

  ws.addEventListener('open', () => {
    console.log('[bg] Connected to Local Proxy');
    if (reconnectTimer) {
      clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
  });

  ws.addEventListener('message', async (event) => {
    try {
      const envelope = JSON.parse(event.data as string);
      if (envelope.type === 'command') {
        await handleCommand(envelope as CommandMessage);
      }
    } catch (err) {
      console.error('[bg] Error processing message:', err);
    }
  });

  ws.addEventListener('close', () => {
    console.log('[bg] Disconnected from Local Proxy');
    ws = null;
    scheduleReconnect();
  });

  ws.addEventListener('error', () => {
    console.error('[bg] WebSocket error');
    ws = null;
    scheduleReconnect();
  });
}

function scheduleReconnect(): void {
  if (reconnectTimer) return;
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null;
    connectToLocalProxy();
  }, 3000);
}

function sendResponse(id: string, browserId: string, payload: unknown): void {
  if (!ws || ws.readyState !== WebSocket.OPEN) return;
  ws.send(JSON.stringify({
    id,
    type: 'response',
    browserId,
    payload,
    timestamp: Date.now(),
  }));
}

async function handleCommand(msg: CommandMessage): Promise<void> {
  const { id, browserId, payload } = msg;
  const { command, tabId, params } = payload;

  try {
    let result: unknown;

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
        result = { url: updatedTab.url, title: updatedTab.title };
        break;
      }

      case 'goBack': {
        const tab = tabId ?? (await getActiveTabId());
        await chrome.tabs.goBack(tab);
        result = { ok: true };
        break;
      }

      case 'goForward': {
        const tab = tabId ?? (await getActiveTabId());
        await chrome.tabs.goForward(tab);
        result = { ok: true };
        break;
      }

      case 'refresh': {
        const tab = tabId ?? (await getActiveTabId());
        await chrome.tabs.reload(tab);
        result = { ok: true };
        break;
      }

      case 'tab:list': {
        const tabs = await chrome.tabs.query({});
        result = tabs.map((t) => ({
          id: t.id,
          url: t.url,
          title: t.title,
          active: t.active,
          windowId: t.windowId,
        }));
        break;
      }

      case 'tab:new': {
        const newTab = await chrome.tabs.create({ url: params.url as string | undefined });
        result = { id: newTab.id, url: newTab.url };
        break;
      }

      case 'tab:close': {
        await chrome.tabs.remove(params.tabId as number);
        result = { ok: true };
        break;
      }

      case 'tab:switch': {
        const targetTabId = params.tabId as number;
        const tab = await chrome.tabs.update(targetTabId, { active: true });
        result = { id: tab.id, url: tab.url, title: tab.title };
        break;
      }

      case 'pageinfo': {
        const tab = tabId ?? (await getActiveTabId());
        const t = await chrome.tabs.get(tab);
        result = { id: t.id, url: t.url, title: t.title, active: t.active };
        break;
      }

      case 'screenshot': {
        const tab = tabId ?? (await getActiveTabId());
        const activeTab = await chrome.tabs.get(tab);
        const dataUrl = await chrome.tabs.captureVisibleTab(activeTab.windowId, { format: 'png' });
        result = { dataUrl };
        break;
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
        result = { url: t.url, title: t.title };
        break;
      }

      // DOM commands — forward to content script
      case 'click':
      case 'type':
      case 'select':
      case 'scroll':
      case 'hover':
      case 'gettext':
      case 'gethtml':
      case 'wait:element': {
        result = await sendToContentScript(tabId, payload);
        break;
      }

      default:
        sendResponse(id, browserId, { status: 'error', error: 'unknown_command', message: `Unknown command: ${command}` });
        return;
    }

    sendResponse(id, browserId, { status: 'ok', data: result });
  } catch (err) {
    sendResponse(id, browserId, { status: 'error', error: 'execution_error', message: String(err) });
  }
}

async function sendToContentScript(tabId: number | undefined, payload: Record<string, unknown>): Promise<unknown> {
  const tab = tabId ?? (await getActiveTabId());

  // Try to ping content script
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

  // Small delay to let content script initialize
  await new Promise((resolve) => setTimeout(resolve, 100));
  return await chrome.tabs.sendMessage(tab, { type: 'command', payload });
}

async function getActiveTabId(): Promise<number> {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  if (!tab?.id) throw new Error('No active tab found');
  return tab.id;
}

// Start connection on extension load
connectToLocalProxy();

// Handle messages from popup
chrome.runtime.onMessage.addListener((request, _sender, sendResponse) => {
  if (request.type === 'ping') {
    sendResponse({ type: 'pong', connected: ws?.readyState === WebSocket.OPEN });
  }
  if (request.type === 'connect') {
    connectToLocalProxy();
    sendResponse({ type: 'connected' });
  }
  return true;
});

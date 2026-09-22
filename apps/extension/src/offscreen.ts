// Offscreen document: maintains persistent WebSocket connection to Local Proxy.
// The Service Worker can't hold persistent connections (it sleeps after ~30s idle),
// so we move the WebSocket here. Commands from the proxy are relayed to the SW
// via chrome.runtime.sendMessage, and responses are sent back through this doc.
// Only chrome.runtime is available in this context — chrome.storage is not, so
// the SW pushes the pairing token here via the connect_ws message.

import { LOCAL_WS_PORT } from '@browser-bridge/shared';

const LOCAL_WS_URL = `ws://localhost:${LOCAL_WS_PORT}`;

let ws: WebSocket | null = null;
let currentToken: string | null = null;
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;

function connect(): void {
  if (ws && ws.readyState === WebSocket.OPEN) return;
  if (!currentToken) {
    notifyStatus(false);
    return;
  }
  openSocket(currentToken);
}

function openSocket(token: string): void {
  // The token travels in the Sec-WebSocket-Protocol header, not the URL:
  // URLs end up in logs and caches, a bearer token must not.
  const socket = new WebSocket(LOCAL_WS_URL, [token]);
  ws = socket;

  socket.addEventListener('open', () => {
    console.log('[offscreen] Connected to Local Proxy');
    if (reconnectTimer) {
      clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
    notifyStatus(true);
  });

  socket.addEventListener('message', (event) => {
    try {
      const envelope = JSON.parse(event.data as string);
      if (envelope.type === 'command') {
        // Forward command to Service Worker for execution
        chrome.runtime.sendMessage(
          { type: 'ws_command', envelope },
          (response) => {
            if (chrome.runtime.lastError) {
              sendResponse(envelope.id, envelope.browserId, {
                status: 'error',
                error: 'sw_error',
                message: chrome.runtime.lastError.message,
              });
              return;
            }
            if (response) {
              sendResponse(envelope.id, envelope.browserId, response);
            }
          },
        );
      }
    } catch (err) {
      console.error('[offscreen] Error processing message:', err);
    }
  });

  socket.addEventListener('close', () => {
    console.log('[offscreen] Disconnected from Local Proxy');
    if (ws !== socket) return; // replaced by a newer socket (e.g. re-pair)
    ws = null;
    notifyStatus(false);
    scheduleReconnect();
  });

  socket.addEventListener('error', () => {
    console.error('[offscreen] WebSocket error');
    if (ws !== socket) return;
    ws = null;
    notifyStatus(false);
    scheduleReconnect();
  });
}

function scheduleReconnect(): void {
  if (reconnectTimer) return;
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null;
    connect();
  }, 3000);
}

function sendResponse(id: string, browserId: string, payload: unknown): void {
  if (!ws || ws.readyState !== WebSocket.OPEN) return;
  ws.send(
    JSON.stringify({
      id,
      type: 'response',
      browserId,
      payload,
      timestamp: Date.now(),
    }),
  );
}

function notifyStatus(connected: boolean): void {
  try {
    chrome.runtime
      .sendMessage({ type: 'ws_status', connected })
      .catch(() => {});
  } catch {
    // SW might be asleep, that's fine
  }
}

// Handle messages from the Service Worker. Re-pairing rotates the token:
// the SW watches storage (unavailable in this context) and pushes the new
// token via connect_ws; connect() caches it for reconnects.
chrome.runtime.onMessage.addListener((request, _sender, sendResponse) => {
  if (request.type === 'connect_ws') {
    currentToken =
      typeof request.token === 'string' && request.token !== ''
        ? request.token
        : null;
    if (ws) {
      const stale = ws;
      ws = null;
      stale.close();
    }
    connect();
    sendResponse({ ok: currentToken !== null });
    return true;
  }

  if (request.type === 'ws_ping') {
    sendResponse({
      type: 'ws_pong',
      connected: ws?.readyState === WebSocket.OPEN,
    });
    return true;
  }

  if (request.type === 'ws_send') {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify(request.envelope));
      sendResponse({ ok: true });
    } else {
      sendResponse({ ok: false, error: 'not_connected' });
    }
    return true;
  }

  return false;
});

import { createClient } from '@browser-bridge/websocket/client';

console.log('Background service worker started');

let client: ReturnType<typeof createClient> | null = null;

function connect() {
  client = createClient({
    onMessage(envelope) {
      console.log(`[ws] ${envelope.type}:`, envelope.data);
    },
    onError(error) {
      console.error('[ws] error:', error);
      client = null;
    },
    onClose() {
      console.log('[ws] disconnected');
      client = null;
    },
  });
}

chrome.runtime.onMessage.addListener((request, _sender, sendResponse) => {
  if (request.type === 'ping') {
    sendResponse({ type: 'pong', data: 'WebSocket client ready' });
  }
  if (request.type === 'connect') {
    connect();
    sendResponse({ type: 'connected' });
  }
  if (request.type === 'send' && client) {
    client.send('message', request.data);
  }
  return true;
});

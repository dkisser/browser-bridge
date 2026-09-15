import type { Envelope, ResponsePayload } from '@browser-bridge/shared';
import { decode, encode } from '@browser-bridge/websocket/protocol';

export interface MockWsServer {
  server: ReturnType<typeof Bun.serve>;
  getLastCommandPayload: () => unknown;
}

// Minimal stand-in for the Browser Bridge WebSocket server: answers the
// list_browsers event with one online browser, and every command with the
// given response. Tests declare command responses with the shared contract
// types so a contract change breaks test compilation.
export function createMockWsServer(
  commandResponse: ResponsePayload,
): MockWsServer {
  let lastCommandPayload: unknown;
  const server = Bun.serve({
    port: 0,
    hostname: '127.0.0.1',
    fetch(req, wsServer) {
      if (new URL(req.url).pathname === '/ws') {
        const upgraded = wsServer.upgrade(req);
        if (!upgraded) return new Response('Upgrade failed', { status: 400 });
      }
      return new Response('Not found', { status: 404 });
    },
    websocket: {
      open() {},
      message(ws, data) {
        const envelope = decode(data as string) as Envelope;
        const payload: ResponsePayload =
          envelope.type === 'event'
            ? {
                status: 'ok',
                data: [
                  {
                    browserId: 'a',
                    userId: 'u',
                    status: 'online',
                    lastSeen: Date.now(),
                  },
                ],
              }
            : commandResponse;
        if (envelope.type === 'command') {
          lastCommandPayload = envelope.payload;
        }
        ws.send(
          encode('response', payload, {
            id: envelope.id,
            browserId: envelope.browserId,
          }),
        );
      },
      close() {},
    },
  });

  return {
    server,
    getLastCommandPayload: () => lastCommandPayload,
  };
}

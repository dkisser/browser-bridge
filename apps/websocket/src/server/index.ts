import { WEBSOCKET_PORT } from '@browser-bridge/shared';
import type { AuthProvider } from '@browser-bridge/shared/auth';
import { NoopAuthProvider } from '@browser-bridge/shared/auth';
import type { Envelope } from '@browser-bridge/shared/types';
import { isLocalhost } from '@browser-bridge/shared/utils';
import type { ServerWebSocket } from 'bun';
import { decode, encode } from '../protocol';
import { ConnectionRegistry } from './registry';
import type { WsData } from './types';

export function startServer(
  port = WEBSOCKET_PORT,
  authProvider: AuthProvider = new NoopAuthProvider(),
) {
  const registry = new ConnectionRegistry(authProvider);
  const cliConnections = new Set<ServerWebSocket<WsData>>();

  const server = Bun.serve<WsData>({
    port,
    async fetch(req, server) {
      const url = new URL(req.url);
      const host = url.hostname;

      if (url.protocol === 'ws:' && !isLocalhost(host)) {
        return new Response('TLS required', { status: 426 });
      }

      const authHeader = req.headers.get('Authorization') ?? '';
      const authResult = await authProvider.validateHeader(authHeader);

      if (
        server.upgrade(req, {
          data: {
            connectionId: crypto.randomUUID(),
            authenticated: authResult.valid,
            userId: authResult.userId,
          },
        })
      ) {
        return;
      }
      return new Response('Browser Bridge WebSocket server', { status: 200 });
    },
    websocket: {
      open(ws) {
        if (!ws.data.authenticated) {
          ws.close(4001, 'unauthorized');
          return;
        }

        console.log(
          `Client connected: ${ws.data.connectionId} (user: ${ws.data.userId})`,
        );
        cliConnections.add(ws);
        ws.send(encode('event', { event: 'welcome' }));
      },

      async message(ws, message) {
        const text =
          typeof message === 'string'
            ? message
            : new TextDecoder().decode(message);

        let envelope: Envelope;
        try {
          envelope = decode(text);
        } catch {
          ws.send(
            encode(
              'response',
              { status: 'error', error: 'invalid_json' },
              { id: '' },
            ),
          );
          return;
        }

        switch (envelope.type) {
          case 'event': {
            const event = envelope.payload as Record<string, unknown>;

            if (event.event === 'register') {
              const browserId = event.browserId as string;
              const result = await registry.register(ws, browserId);
              if (result.success) {
                ws.send(
                  encode(
                    'response',
                    { status: 'ok' },
                    { id: envelope.id, browserId },
                  ),
                );
              } else {
                ws.send(
                  encode(
                    'response',
                    { status: 'error', error: result.error },
                    { id: envelope.id },
                  ),
                );
              }
            }

            if (event.event === 'online') {
              const browserId =
                (event.browserId as string) || ws.data.browserId;
              if (browserId) {
                registry.setStatus(browserId, 'online');
                ws.send(
                  encode(
                    'response',
                    { status: 'ok' },
                    { id: envelope.id, browserId },
                  ),
                );
              }
            }

            if (event.event === 'offline') {
              const browserId =
                (event.browserId as string) || ws.data.browserId;
              if (browserId) {
                registry.setStatus(browserId, 'offline');
                ws.send(
                  encode(
                    'response',
                    { status: 'ok' },
                    { id: envelope.id, browserId },
                  ),
                );
              }
            }

            if (event.event === 'list_browsers') {
              const browsers = registry.listBrowsers();
              ws.send(
                encode(
                  'response',
                  { status: 'ok', data: browsers },
                  { id: envelope.id },
                ),
              );
            }
            break;
          }

          case 'command': {
            const browserId = envelope.browserId;
            const status = registry.getStatus(browserId);

            if (!status || status === 'offline') {
              ws.send(
                encode(
                  'response',
                  {
                    status: 'error',
                    error: 'browser_offline',
                    message: `Browser ${browserId} is offline`,
                  },
                  { id: envelope.id, browserId },
                ),
              );
              break;
            }

            const targetWs = registry.getWebSocket(browserId);
            if (!targetWs) {
              ws.send(
                encode(
                  'response',
                  { status: 'error', error: 'browser_not_found' },
                  { id: envelope.id, browserId },
                ),
              );
              break;
            }

            // Forward command to Local Proxy
            targetWs.send(text);
            break;
          }

          case 'response': {
            // Response from Local Proxy — forward back to CLI connections
            for (const cliWs of cliConnections) {
              if (cliWs !== ws && cliWs.readyState === 1) {
                cliWs.send(text);
              }
            }
            break;
          }
        }
      },

      close(ws) {
        cliConnections.delete(ws);
        const browserId = registry.removeByWebSocket(ws);
        if (browserId) {
          console.log(`Browser disconnected: ${browserId}`);
        }
        console.log(`Client disconnected: ${ws.data.connectionId}`);
      },
    },
  });

  console.log(`WebSocket server running on ws://localhost:${server.port}`);
  return server;
}

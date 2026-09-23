import { WEBSOCKET_PORT } from '@browser-bridge/shared';
import type { AuthProvider } from '@browser-bridge/shared/auth';
import { NoopAuthProvider } from '@browser-bridge/shared/auth';
import type { Envelope } from '@browser-bridge/shared/types';
import { isLocalhost } from '@browser-bridge/shared/utils';
import type { ServerWebSocket } from 'bun';
import type { Router } from '../router';
import { decode, encode } from '../protocol';
import { ConnectionRegistry } from './registry';
import type { WsData } from './types';

export interface InboundServerHandle {
  /** The upstream Socket for the inbound server; null until start() returns. */
  readonly server: ReturnType<typeof Bun.serve> | null;
}

export interface InboundServerOptions {
  port?: number;
  hostname?: string;
  authProvider?: AuthProvider;
  router: Router;
  registry: ConnectionRegistry;
}

/**
 * Inbound WebServer listening on 3001 (default). Accepts CLI / MCP client
 * connections, authenticates them, and dispatches commands to the in-process
 * router. Pre-merge this was the separate `ws-server` binary; after the
 * bridge-core merge it lives next to the BrowserServer in one process.
 *
 * Note on responses: responses coming back from the BrowserServer (in-process
 * router notification) are forwarded to all open CLI connections matching the
 * envelope id — preserving ws-server's fan-out behavior. Single-user setups
 * have at most one CLI at a time, so the fan-out is harmless.
 */
export class InboundServer {
  private readonly port: number;
  private readonly hostname: string;
  private readonly authProvider: AuthProvider;
  private readonly router: Router;
  private readonly registry: ConnectionRegistry;
  private readonly cliConnections = new Set<ServerWebSocket<WsData>>();
  private server: ReturnType<typeof Bun.serve> | null = null;

  constructor(options: InboundServerOptions) {
    this.port = options.port ?? WEBSOCKET_PORT;
    this.hostname = options.hostname ?? '127.0.0.1';
    this.authProvider = options.authProvider ?? new NoopAuthProvider();
    this.router = options.router;
    this.registry = options.registry;
  }

  start(): ReturnType<typeof Bun.serve> {
    const registry = this.registry;
    const router = this.router;
    const cliConnections = this.cliConnections;
    const authProvider = this.authProvider;

    const server = Bun.serve<WsData>({
      port: this.port,
      hostname: this.hostname,
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
        return new Response('Browser Bridge control plane', { status: 200 });
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

        message(ws, message) {
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

              // list_browsers: respond with the current registry state.
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

              // In-process dispatch: was a forwarded text frame to
              // local-proxy's WS pre-merge; now a function call into the
              // router, which in turn talks to the BrowserServer.
              router.handleInboundCommand(envelope, ws);
              break;
            }

            case 'response': {
              // Forward CLI→browser responses (rare; the browser normally
              // produces responses) to all other CLI sockets.
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
          console.log(`Client disconnected: ${ws.data.connectionId}`);
        },
      },
    });

    this.server = server;
    console.log(`Inbound server running on ws://localhost:${server.port}`);
    return server;
  }

  stop(): void {
    this.server?.stop();
    this.server = null;
  }
}
import { LOCAL_WS_PORT } from '@browser-bridge/shared';
import type { Envelope } from '@browser-bridge/shared/types';
import type { ServerWebSocket } from 'bun';
import type { Router } from './router';
import { decode, encode } from './protocol';
import type { PairingManager } from './pairing';

export interface BrowserServerOptions {
  port?: number;
  hostname?: string;
  getRouter: () => Router;
  pairing: PairingManager;
}

const EXTENSION_ORIGIN_PREFIX = 'chrome-extension://';

/**
 * BrowserServer listens on port 3002 (default) for the Chrome extension.
 *
 * Pre-merge this was the LocalServer in apps/local-proxy/src/local-server.ts;
 * the merge dropped its `cloud` controller dependency (no more outbound WS to
 * ws-server — commands flow in-process through Router.handleInboundCommand)
 * and added the responsibilities the extension side used to ask the local-proxy
 * to forward on its behalf: the pairing handshake now lives here directly.
 *
 * The pairing token travels in the Sec-WebSocket-Protocol header (browser
 * WebSockets cannot set arbitrary headers); it is echoed back as the selected
 * subprotocol so the handshake completes. Everything else gets 403.
 */
export class BrowserServer {
  private readonly port: number;
  private readonly hostname: string;
  private readonly getRouter: () => Router;
  private readonly pairing: PairingManager;
  private server: ReturnType<typeof Bun.serve> | null = null;
  private extensionWs: ServerWebSocket<undefined> | null = null;

  constructor(options: BrowserServerOptions) {
    this.port = options.port ?? LOCAL_WS_PORT;
    this.hostname = options.hostname ?? '127.0.0.1';
    this.getRouter = options.getRouter;
    this.pairing = options.pairing;
  }

  start(): ReturnType<typeof Bun.serve> {
    const self = this;
    const getRouter = self.getRouter;
    const pairing = self.pairing;

    this.server = Bun.serve<undefined>({
      port: self.port,
      hostname: self.hostname,
      async fetch(req, server) {
        if (req.headers.get('upgrade')?.toLowerCase() === 'websocket') {
          const protocol = req.headers.get('sec-websocket-protocol');
          const token = protocol
            ?.split(',')
            .map((entry) => entry.trim())
            .find((entry) => entry !== '');
          if (!token || !pairing.verify(token)) {
            return new Response('unauthorized', { status: 403 });
          }
          if (
            server.upgrade(req, {
              data: undefined,
              headers: { 'Sec-WebSocket-Protocol': token },
            })
          )
            return;
          return new Response('upgrade failed', { status: 500 });
        }

        const url = new URL(req.url);
        const corsHeaders = self.corsHeaders(req);

        if (req.method === 'OPTIONS') {
          return new Response(null, { status: 204, headers: corsHeaders });
        }

        // A web page (non-extension origin) must not drive the proxy's API:
        // no CORS headers means the browser blocks the read, and this guard
        // blocks the write side of simple requests.
        if (self.isWebOrigin(req)) {
          return Response.json(
            { success: false, error: 'forbidden_origin' },
            { status: 403 },
          );
        }

        // Status / pairing endpoints — read by the extension popup and the
        // `bridge service status` CLI.
        if (url.pathname === '/api/status') {
          return Response.json(
            {
              success: true,
              data: {
                paired: pairing.isPaired,
                hasExtension: self.hasExtension(),
                browserId: getRouter().browserId,
              },
            },
            { headers: corsHeaders },
          );
        }

        if (url.pathname === '/api/pair/start' && req.method === 'POST') {
          const { code, expiresIn } = pairing.start();
          return Response.json(
            { success: true, data: { code, expiresIn } },
            { headers: corsHeaders },
          );
        }

        if (url.pathname === '/api/pair/confirm' && req.method === 'POST') {
          let payload: { code?: string } = {};
          try {
            payload = (await req.json()) as { code?: string };
          } catch {
            // malformed body falls through to the invalid-code path
          }
          const result = pairing.confirm(
            typeof payload.code === 'string' ? payload.code : '',
          );
          if (result.ok) {
            return Response.json(
              { success: true, data: { token: result.token } },
              { headers: corsHeaders },
            );
          }
          return Response.json(
            {
              success: false,
              error: result.error,
              ...(result.attemptsRemaining !== undefined
                ? { attemptsRemaining: result.attemptsRemaining }
                : {}),
            },
            { status: 401, headers: corsHeaders },
          );
        }

        return new Response('Browser Bridge Browser Server', { status: 200 });
      },
      websocket: {
        open(ws) {
          console.log('[browser] Extension connected');
          self.extensionWs = ws;
          ws.send(encode('event', { event: 'connected' }));
          getRouter().handleBrowserConnect();
        },
        message(_ws, message) {
          const text =
            typeof message === 'string'
              ? message
              : new TextDecoder().decode(message);
          try {
            const envelope = decode(text);
            if (envelope.type === 'response') {
              getRouter().handleBrowserResponse(envelope);
            } else if (envelope.type === 'event') {
              getRouter().handleBrowserEvent(envelope);
            }
          } catch {
            console.error('[browser] invalid message from Extension');
          }
        },
        close() {
          console.log('[browser] Extension disconnected');
          self.extensionWs = null;
          getRouter().handleBrowserDisconnect();
        },
      },
    });

    console.log(`Browser server listening on ws://localhost:${this.port}`);
    return this.server;
  }

  // CORS: only extension pages may read API responses. Requests without an
  // Origin (curl, the CLI, local probes) are served but get no CORS headers.
  private corsHeaders(req: Request): Record<string, string> {
    const origin = req.headers.get('origin');
    if (origin?.startsWith(EXTENSION_ORIGIN_PREFIX)) {
      return {
        'Access-Control-Allow-Origin': origin,
        Vary: 'Origin',
        'Access-Control-Allow-Methods': 'GET, POST, OPTIONS',
        'Access-Control-Allow-Headers': 'Content-Type',
      };
    }
    return {};
  }

  private isWebOrigin(req: Request): boolean {
    const origin = req.headers.get('origin');
    return origin !== null && !origin.startsWith(EXTENSION_ORIGIN_PREFIX);
  }

  hasExtension(): boolean {
    return (
      this.extensionWs !== null &&
      this.extensionWs.readyState === WebSocket.OPEN
    );
  }

  sendToExtension(envelope: string): boolean {
    if (!this.extensionWs || this.extensionWs.readyState !== WebSocket.OPEN) {
      return false;
    }
    this.extensionWs.send(envelope);
    return true;
  }

  sendEventToExtension(envelope: Envelope): boolean {
    return this.sendToExtension(encode(envelope.type, envelope.payload, {
      id: envelope.id,
      browserId: envelope.browserId,
    }));
  }

  stop(): void {
    this.server?.stop();
    this.server = null;
  }
}
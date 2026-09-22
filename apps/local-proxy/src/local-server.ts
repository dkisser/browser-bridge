import type { Envelope } from '@browser-bridge/shared/types';
import { decode, encode } from '@browser-bridge/websocket/protocol';
import type { ServerWebSocket } from 'bun';
import type { PairingManager } from './pairing';

interface CloudController {
  isConnected: () => boolean;
  isManualDisconnect: () => boolean;
  connect: () => Promise<void>;
  disconnect: () => void;
  browserId: string;
  serverUrl: string;
}

interface LocalServerHandlers {
  onCommand: (envelope: Envelope) => void;
  onConnect: () => void;
  onDisconnect: () => void;
  cloud?: CloudController;
}

const EXTENSION_ORIGIN_PREFIX = 'chrome-extension://';

export class LocalServer {
  private server: ReturnType<typeof Bun.serve> | null = null;
  private extensionWs: ServerWebSocket<undefined> | null = null;
  private handlers: LocalServerHandlers;
  private port: number;
  private hostname: string;
  private pairing: PairingManager;

  constructor(
    port: number,
    handlers: LocalServerHandlers,
    pairing: PairingManager,
    hostname = '127.0.0.1',
  ) {
    this.port = port;
    this.handlers = handlers;
    this.hostname = hostname;
    this.pairing = pairing;
  }

  start(): void {
    const self = this;
    this.server = Bun.serve<undefined>({
      port: this.port,
      hostname: this.hostname,
      async fetch(req, server) {
        // WebSocket upgrade: only a connection carrying the pairing token
        // may claim to be the extension. Everything else gets 403.
        if (req.headers.get('upgrade')?.toLowerCase() === 'websocket') {
          const token = new URL(req.url).searchParams.get('token');
          if (!self.pairing.verify(token)) {
            return new Response('unauthorized', { status: 403 });
          }
          if (server.upgrade(req, { data: undefined })) return;
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

        const cloud = self.handlers.cloud;

        if (url.pathname === '/api/status') {
          return Response.json(
            {
              success: true,
              data: {
                connected: cloud?.isConnected() ?? false,
                browserId: cloud?.browserId ?? '',
                serverUrl: cloud?.serverUrl ?? '',
                manualDisconnect: cloud?.isManualDisconnect() ?? false,
                paired: self.pairing.isPaired,
                hasExtension: self.hasExtension,
              },
            },
            { headers: corsHeaders },
          );
        }

        if (url.pathname === '/api/connect' && req.method === 'POST') {
          try {
            await cloud?.connect();
            return Response.json(
              {
                success: true,
                data: { connected: cloud?.isConnected() ?? false },
              },
              { headers: corsHeaders },
            );
          } catch (error) {
            const message =
              error instanceof Error ? error.message : String(error);
            return Response.json(
              { success: false, error: message },
              { status: 500, headers: corsHeaders },
            );
          }
        }

        if (url.pathname === '/api/disconnect' && req.method === 'POST') {
          cloud?.disconnect();
          return Response.json(
            { success: true, data: { connected: false } },
            { headers: corsHeaders },
          );
        }

        if (url.pathname === '/api/pair/start' && req.method === 'POST') {
          const { code, expiresIn } = self.pairing.start();
          return Response.json(
            { success: true, data: { code, expiresIn } },
            { headers: corsHeaders },
          );
        }

        if (url.pathname === '/api/pair/confirm' && req.method === 'POST') {
          let body: { code?: string } = {};
          try {
            body = (await req.json()) as { code?: string };
          } catch {
            // malformed body falls through to the invalid-code path
          }
          const result = self.pairing.confirm(
            typeof body.code === 'string' ? body.code : '',
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

        return new Response('Browser Bridge Local Proxy', { status: 200 });
      },
      websocket: {
        open(ws) {
          console.log('[local] Extension connected');
          self.extensionWs = ws;
          ws.send(encode('event', { event: 'connected' }));
          self.handlers.onConnect();
        },
        message(_ws, message) {
          const text =
            typeof message === 'string'
              ? message
              : new TextDecoder().decode(message);
          try {
            const envelope = decode(text);
            self.handlers.onCommand(envelope);
          } catch {
            console.error('[local] invalid message from Extension');
          }
        },
        close() {
          console.log('[local] Extension disconnected');
          self.extensionWs = null;
          self.handlers.onDisconnect();
        },
      },
    });

    console.log(`[local] Listening on ws://localhost:${this.port}`);
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

  sendToExtension(envelope: string): boolean {
    if (!this.extensionWs || this.extensionWs.readyState !== WebSocket.OPEN) {
      return false;
    }
    this.extensionWs.send(envelope);
    return true;
  }

  get hasExtension(): boolean {
    return (
      this.extensionWs !== null &&
      this.extensionWs.readyState === WebSocket.OPEN
    );
  }

  stop(): void {
    this.server?.stop();
  }
}

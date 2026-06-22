import type { Envelope } from '@browser-bridge/shared/types';
import { decode, encode } from '@browser-bridge/websocket/protocol';
import type { ServerWebSocket } from 'bun';

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

export class LocalServer {
  private server: ReturnType<typeof Bun.serve> | null = null;
  private extensionWs: ServerWebSocket<undefined> | null = null;
  private handlers: LocalServerHandlers;
  private port: number;
  private hostname: string;

  constructor(
    port: number,
    handlers: LocalServerHandlers,
    hostname = '127.0.0.1',
  ) {
    this.port = port;
    this.handlers = handlers;
    this.hostname = hostname;
  }

  start(): void {
    const self = this;
    this.server = Bun.serve<undefined>({
      port: this.port,
      hostname: this.hostname,
      async fetch(req, server) {
        if (server.upgrade(req, { data: undefined })) return;

        const url = new URL(req.url);
        const corsHeaders = {
          'Access-Control-Allow-Origin': '*',
          'Access-Control-Allow-Methods': 'GET, POST, OPTIONS',
          'Access-Control-Allow-Headers': 'Content-Type',
        };

        if (req.method === 'OPTIONS') {
          return new Response(null, { status: 204, headers: corsHeaders });
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

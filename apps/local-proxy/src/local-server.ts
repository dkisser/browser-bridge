import type { Envelope } from '@browser-bridge/shared/types';
import { decode, encode } from '@browser-bridge/websocket/protocol';
import type { ServerWebSocket } from 'bun';

interface LocalServerHandlers {
  onCommand: (envelope: Envelope) => void;
  onConnect: () => void;
  onDisconnect: () => void;
}

export class LocalServer {
  private server: ReturnType<typeof Bun.serve> | null = null;
  private extensionWs: ServerWebSocket<undefined> | null = null;
  private handlers: LocalServerHandlers;
  private port: number;

  constructor(port: number, handlers: LocalServerHandlers) {
    this.port = port;
    this.handlers = handlers;
  }

  start(): void {
    const self = this;
    this.server = Bun.serve<undefined>({
      port: this.port,
      fetch(_req, server) {
        if (server.upgrade(_req, { data: undefined })) return;
        return new Response('Browser Bridge Local Proxy', { status: 200 });
      },
      websocket: {
        open(ws) {
          console.log('[local] Extension connected');
          self.extensionWs = ws;
          ws.send(encode('event', { event: 'connected' }));
          self.handlers.onConnect();
        },
        message(ws, message) {
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

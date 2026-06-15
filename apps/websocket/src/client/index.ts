import { WEBSOCKET_PORT } from '@my/shared';
import { decode, encode } from '../protocol';
import type { Envelope, CommandPayload, ResponsePayload } from '@my/shared/types';

export interface ClientOptions {
  url?: string;
  headers?: Record<string, string>;
  onMessage?: (envelope: Envelope) => void;
  onError?: (error: Event) => void;
  onClose?: () => void;
}

interface PendingRequest {
  resolve: (envelope: Envelope) => void;
  reject: (error: Error) => void;
  timer: ReturnType<typeof setTimeout>;
}

export function createClient(options: ClientOptions = {}) {
  const {
    url = `ws://localhost:${WEBSOCKET_PORT}`,
    headers,
    onMessage,
    onError,
    onClose,
  } = options;

  const socket = headers
    ? new WebSocket(url, { headers } as any)
    : new WebSocket(url);
  const pending = new Map<string, PendingRequest>();

  socket.addEventListener('open', () => {
    console.log('Connected to server');
  });

  socket.addEventListener('message', (event) => {
    try {
      const envelope = decode(event.data as string);
      // Resolve pending request if this is a response
      if (envelope.type === 'response' && pending.has(envelope.id)) {
        const req = pending.get(envelope.id)!;
        clearTimeout(req.timer);
        pending.delete(envelope.id);
        req.resolve(envelope);
      }
      onMessage?.(envelope);
    } catch {
      onMessage?.({ id: '', type: 'event', browserId: '', payload: event.data, timestamp: 0 });
    }
  });

  socket.addEventListener('error', (error) => {
    onError?.(error);
  });

  socket.addEventListener('close', () => {
    // Reject all pending requests
    for (const [id, req] of pending) {
      clearTimeout(req.timer);
      req.reject(new Error('connection closed'));
      pending.delete(id);
    }
    onClose?.();
  });

  return {
    send(type: Envelope['type'], payload: unknown, opts?: { id?: string; browserId?: string }) {
      socket.send(encode(type, payload, opts));
    },

    sendCommand(
      browserId: string,
      payload: CommandPayload,
      opts: { timeout?: number } = {},
    ): Promise<Envelope> {
      const timeout = opts.timeout ?? 10000;
      const id = crypto.randomUUID();
      return new Promise((resolve, reject) => {
        const timer = setTimeout(() => {
          pending.delete(id);
          reject(new Error(`timeout: no response for command ${payload.command} within ${timeout}ms`));
        }, timeout);

        pending.set(id, { resolve, reject, timer });
        socket.send(encode('command', payload, { id, browserId }));
      });
    },

    sendRaw(text: string) {
      socket.send(text);
    },

    close() {
      socket.close();
    },

    get readyState() {
      return socket.readyState;
    },
  };
}

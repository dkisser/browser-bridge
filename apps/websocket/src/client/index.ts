import { WEBSOCKET_PORT } from '@my/shared';
import { decode, encode } from '../protocol';

export interface ClientOptions {
  url?: string;
  onMessage?: (envelope: { type: string; data: unknown }) => void;
  onError?: (error: Event) => void;
  onClose?: () => void;
}

export function createClient(options: ClientOptions = {}) {
  const {
    url = `ws://localhost:${WEBSOCKET_PORT}`,
    onMessage,
    onError,
    onClose,
  } = options;

  const socket = new WebSocket(url);

  socket.addEventListener('open', () => {
    console.log('Connected to server');
  });

  socket.addEventListener('message', (event) => {
    try {
      const envelope = decode(event.data as string);
      onMessage?.(envelope);
    } catch {
      onMessage?.({ type: 'raw', data: event.data });
    }
  });

  socket.addEventListener('error', (error) => {
    onError?.(error);
  });

  socket.addEventListener('close', () => {
    onClose?.();
  });

  return {
    send(type: string, data: unknown) {
      socket.send(encode(type, data));
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

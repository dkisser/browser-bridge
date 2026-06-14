import { WEBSOCKET_PORT } from '@my/shared';
import { decode, encode } from '../protocol';

export function startServer(port = WEBSOCKET_PORT) {
  const server = Bun.serve<{ connectionId: string }>({
    port,
    fetch(_req, server) {
      if (
        server.upgrade(_req, {
          data: { connectionId: crypto.randomUUID() },
        })
      ) {
        return;
      }
      return new Response('Browser Bridge WebSocket server', { status: 200 });
    },
    websocket: {
      open(ws) {
        console.log(`Client connected: ${ws.data.connectionId}`);
        ws.send(encode('welcome', 'Connected'));
      },
      message(ws, message) {
        const text =
          typeof message === 'string'
            ? message
            : new TextDecoder().decode(message);
        console.log(`Received from ${ws.data.connectionId}: ${text}`);
        try {
          const envelope = decode(text);
          // Echo back as a response with the same id for request-response correlation
          ws.send(encode('response', envelope.payload, { id: envelope.id, browserId: envelope.browserId }));
        } catch {
          ws.send(encode('echo', text));
        }
      },
      close(ws) {
        console.log(`Client disconnected: ${ws.data.connectionId}`);
      },
    },
  });

  console.log(`WebSocket server running on ws://localhost:${server.port}`);
  return server;
}

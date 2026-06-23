import { afterAll, beforeAll, describe, expect, it } from 'bun:test';
import { decode, encode } from '@browser-bridge/websocket/protocol';
import type { Server } from 'bun';
import { ManagedClient } from '../managedClient';

describe('ManagedClient', () => {
  let server: Server<undefined>;
  let silentServer: Server<undefined>;
  const PORT = 3200;
  const SILENT_PORT = 3201;

  beforeAll(() => {
    server = Bun.serve({
      port: PORT,
      fetch(req, s) {
        if (s.upgrade(req)) return;
        return new Response('ok', { status: 200 });
      },
      websocket: {
        open() {},
        message(ws, message) {
          const request = decode(message as string);
          ws.send(
            encode(
              'response',
              { status: 'ok', data: request.payload },
              { id: request.id },
            ),
          );
        },
        close() {},
      },
    });

    // Server that accepts TCP but never completes WebSocket handshake
    silentServer = Bun.serve({
      port: SILENT_PORT,
      fetch() {
        return new Response('ok', { status: 200 });
      },
    });
  });

  afterAll(() => {
    server.stop();
    silentServer.stop();
  });

  it('waitForOpen resolves when connection is established', async () => {
    const client = new ManagedClient(`ws://localhost:${PORT}`);
    try {
      await client.waitForOpen(5000);
      expect(client.readyState).toBe(WebSocket.OPEN);
    } finally {
      client[Symbol.dispose]();
    }
  });

  it('waitForOpen rejects after timeout when connection is silent', async () => {
    const client = new ManagedClient(`ws://localhost:${SILENT_PORT}`);
    try {
      await expect(client.waitForOpen(100)).rejects.toThrow(
        'Connection timeout',
      );
    } finally {
      client[Symbol.dispose]();
    }
  });

  it('request works after connection', async () => {
    const client = new ManagedClient(`ws://localhost:${PORT}`);
    try {
      await client.waitForOpen(5000);
      const response = await client.request(
        'event',
        { event: 'list_browsers' },
        { timeout: 1000 },
      );
      expect(response.type).toBe('response');
    } finally {
      client[Symbol.dispose]();
    }
  });

  it('disposing closes socket and clears timers so process can exit', async () => {
    const client = new ManagedClient(`ws://localhost:${PORT}`);
    await client.waitForOpen(5000);
    client[Symbol.dispose]();
    expect(client.readyState).toBe(WebSocket.CLOSED);
  });

  it('auto-disposes with using statement', async () => {
    let disposedClient: ManagedClient | undefined;

    {
      using client = new ManagedClient(`ws://localhost:${PORT}`);
      disposedClient = client;
      await client.waitForOpen(5000);
      expect(client.readyState).toBe(WebSocket.OPEN);
    }

    expect(disposedClient?.readyState).toBe(WebSocket.CLOSED);
  });
});

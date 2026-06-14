import { describe, expect, it, beforeAll, afterAll } from 'bun:test';
import { createClient } from '../index';
import { startServer } from '../../server';
import type { Server } from 'bun';

describe('WS client', () => {
  let server: Server;
  let silentServer: Server;

  beforeAll(() => {
    server = startServer(3099);
    // Server that accepts connections but never responds (for timeout test)
    silentServer = Bun.serve({
      port: 3100,
      fetch(_req, server) {
        if (server.upgrade(_req)) return;
        return new Response('ok', { status: 200 });
      },
      websocket: {
        open() {},
        message() {},
        close() {},
      },
    });
  });

  afterAll(() => {
    server.stop();
    silentServer.stop();
  });

  it('sendCommand returns response correlated by id', async () => {
    const client = createClient({ url: 'ws://localhost:3099' });

    // Wait for connection
    await new Promise((resolve) => {
      const check = setInterval(() => {
        if (client.readyState === WebSocket.OPEN) {
          clearInterval(check);
          resolve(undefined);
        }
      }, 50);
    });

    const response = await client.sendCommand('b-123', {
      command: 'navigate',
      params: { url: 'https://example.com' },
    }, { timeout: 2000 });

    expect(response.id).toBeDefined();
    expect(response.type).toBe('response');
    client.close();
  });

  it('sendCommand rejects on timeout', async () => {
    const client = createClient({ url: 'ws://localhost:3100' });

    await new Promise((resolve) => {
      const check = setInterval(() => {
        if (client.readyState === WebSocket.OPEN) {
          clearInterval(check);
          resolve(undefined);
        }
      }, 50);
    });

    await expect(
      client.sendCommand('b-123', { command: 'navigate', params: {} }, { timeout: 100 }),
    ).rejects.toThrow('timeout');

    client.close();
  });
});

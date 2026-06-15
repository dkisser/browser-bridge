import { describe, expect, it, beforeAll, afterAll } from 'bun:test';
import { createClient } from '../index';
import { startServer } from '../../server';
import { ApiKeyAuthProvider } from '@my/shared/auth';
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
    expect(typeof response.id).toBe('string');
    expect(response.id.length).toBeGreaterThan(0);
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

  it('rejects pending requests on connection close', async () => {
    // Use a server that accepts but doesn't respond
    const closeServer = Bun.serve({
      port: 3101,
      fetch(_req, server) {
        if (server.upgrade(_req)) return;
        return new Response('ok', { status: 200 });
      },
      websocket: {
        open(ws) {
          // Immediately close to trigger rejection
          setTimeout(() => ws.close(), 50);
        },
        message() {},
      },
    });

    const client = createClient({ url: 'ws://localhost:3101' });

    await new Promise((resolve) => {
      const check = setInterval(() => {
        if (client.readyState === WebSocket.OPEN) {
          clearInterval(check);
          resolve(undefined);
        }
      }, 50);
    });

    await expect(
      client.sendCommand('b-123', { command: 'navigate', params: {} }, { timeout: 5000 }),
    ).rejects.toThrow('connection closed');

    client.close();
    closeServer.stop();
  });
});

describe('WS client with API key auth', () => {
  const AUTH_PORT = 3102;
  const VALID_KEY = 'client-test-key';
  let authServer: Server;

  beforeAll(() => {
    authServer = startServer(AUTH_PORT, new ApiKeyAuthProvider({ [VALID_KEY]: 'user-1' }));
  });

  afterAll(() => {
    authServer.stop();
  });

  it('connects successfully with valid API key', async () => {
    const client = createClient({
      url: `ws://localhost:${AUTH_PORT}`,
      headers: { Authorization: `Bearer ${VALID_KEY}` },
    });

    await new Promise<void>((resolve) => {
      const check = setInterval(() => {
        if (client.readyState === WebSocket.OPEN) {
          clearInterval(check);
          resolve();
        }
      }, 50);
    });

    // If we got here, the connection was accepted (not closed with 4001)
    client.close();
  });

  it('fails to connect without API key', async () => {
    const client = createClient({ url: `ws://localhost:${AUTH_PORT}` });

    const closed = await new Promise<boolean>((resolve) => {
      const check = setInterval(() => {
        if (client.readyState === WebSocket.OPEN) {
          clearInterval(check);
          // Connection opened briefly but should close with 4001
        }
        if (client.readyState === WebSocket.CLOSED) {
          clearInterval(check);
          resolve(true);
        }
      }, 50);

      setTimeout(() => {
        clearInterval(check);
        resolve(false);
      }, 3000);
    });

    expect(closed).toBe(true);
    client.close();
  });
});

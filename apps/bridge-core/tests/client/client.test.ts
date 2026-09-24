import { afterAll, beforeAll, describe, expect, it } from 'bun:test';
import { ApiKeyAuthProvider } from '@browser-bridge/shared/auth';
import type { Server } from 'bun';
import { createClient } from '../../src/client';
import { ConnectionRegistry } from '../../src/server/registry';
import { InboundServer } from '../../src/server/inbound';
import type { Router } from '../../src/router';

function buildInboundServer(port: number, authProvider?: ApiKeyAuthProvider) {
  const registry = new ConnectionRegistry();
  const router = {
    handleInboundCommand: () => undefined,
    handleBrowserResponse: () => undefined,
    handleBrowserEvent: () => undefined,
    handleBrowserConnect: () => undefined,
    handleBrowserDisconnect: () => undefined,
    browserId: 'b-test',
  } as unknown as Router;
  const server = new InboundServer({
    port,
    authProvider,
    router,
    registry,
  });
  return { server, registry, router };
}

describe('WS client', () => {
  let server: ReturnType<typeof InboundServer.prototype.start>;
  let silentServer: Server<undefined>;

  beforeAll(() => {
    const built = buildInboundServer(3099);
    server = built.server.start();
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

    await new Promise((resolve) => {
      const check = setInterval(() => {
        if (client.readyState === WebSocket.OPEN) {
          clearInterval(check);
          resolve(undefined);
        }
      }, 50);
    });

    const response = await client.sendCommand(
      'b-123',
      {
        command: 'navigate',
        tabId: 1,
        params: { url: 'https://example.com' },
      },
      { timeout: 2000 },
    );

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
      client.sendCommand(
        'b-123',
        { command: 'navigate', tabId: 1, params: {} },
        { timeout: 100 },
      ),
    ).rejects.toThrow('timeout');

    client.close();
  });

  it('rejects pending requests on connection close', async () => {
    const closeServer = Bun.serve({
      port: 3101,
      fetch(_req, server) {
        if (server.upgrade(_req)) return;
        return new Response('ok', { status: 200 });
      },
      websocket: {
        open(ws) {
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
      client.sendCommand(
        'b-123',
        { command: 'navigate', tabId: 1, params: {} },
        { timeout: 5000 },
      ),
    ).rejects.toThrow('connection closed');

    client.close();
    closeServer.stop();
  });
});

describe('WS client with API key auth', () => {
  const AUTH_PORT = 3102;
  const VALID_KEY = 'client-test-key';
  let authServer: ReturnType<typeof InboundServer.prototype.start>;

  beforeAll(() => {
    const authProvider = new ApiKeyAuthProvider({ [VALID_KEY]: 'user-1' });
    const built = buildInboundServer(AUTH_PORT, authProvider);
    authServer = built.server.start();
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

    client.close();
  });

  it('fails to connect without API key', async () => {
    const client = createClient({ url: `ws://localhost:${AUTH_PORT}` });

    const closed = await new Promise<boolean>((resolve) => {
      const check = setInterval(() => {
        if (client.readyState === WebSocket.OPEN) {
          clearInterval(check);
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
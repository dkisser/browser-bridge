import { afterAll, beforeAll, describe, expect, it } from 'bun:test';
import { ApiKeyAuthProvider } from '@browser-bridge/shared/auth';
import type { ServerWebSocket } from 'bun';
import { ConnectionRegistry } from '../../src/server/registry';
import { InboundServer } from '../../src/server/inbound';
import type { Router } from '../../src/router';
import type { WsData } from '../../src/server/types';

function wsWithAuth(url: string, key: string): WebSocket {
  const opts = { headers: { Authorization: `Bearer ${key}` } };
  return new WebSocket(url, opts as never);
}

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
  return { server, registry };
}

describe('Inbound server routing', () => {
  let server: ReturnType<typeof InboundServer.prototype.start>;

  beforeAll(() => {
    const built = buildInboundServer(3098);
    server = built.server.start();
  });

  afterAll(() => {
    server.stop();
  });

  it('echoes welcome on connect', async () => {
    const ws = new WebSocket('ws://localhost:3098');
    const message = await new Promise<string>((resolve) => {
      ws.addEventListener('message', (e) => {
        resolve(e.data as string);
        ws.close();
      });
    });
    const envelope = JSON.parse(message);
    expect(envelope.type).toBe('event');
    expect(envelope.payload).toEqual({ event: 'welcome' });
  });

  it('returns error for command to offline browser', async () => {
    const ws = new WebSocket('ws://localhost:3098');

    await new Promise<void>((resolve) => {
      ws.addEventListener('open', () => resolve());
    });

    const commandEnvelope = JSON.stringify({
      id: 'test-1',
      type: 'command',
      browserId: 'b-nonexistent',
      payload: { command: 'navigate', params: {} },
      timestamp: Date.now(),
    });

    const response = await new Promise<string>((resolve) => {
      ws.addEventListener('message', (e) => {
        const data = JSON.parse(e.data as string);
        if (data.id === 'test-1') {
          resolve(e.data as string);
          ws.close();
        }
      });
      ws.send(commandEnvelope);
    });

    const parsed = JSON.parse(response);
    expect(parsed.type).toBe('response');
    expect(parsed.payload.status).toBe('error');
    expect(parsed.payload.error).toBe('browser_offline');
  });
});

describe('Inbound server handshake auth', () => {
  const AUTH_PORT = 3097;
  const VALID_KEY = 'server-test-key';
  let server: ReturnType<typeof InboundServer.prototype.start>;

  beforeAll(() => {
    const authProvider = new ApiKeyAuthProvider({ [VALID_KEY]: 'user-1' });
    const built = buildInboundServer(AUTH_PORT, authProvider);
    server = built.server.start();
  });

  afterAll(() => {
    server.stop();
  });

  it('closes connection without Authorization header', async () => {
    const ws = new WebSocket(`ws://localhost:${AUTH_PORT}`);

    const closeEvent = await new Promise<CloseEvent>((resolve) => {
      ws.addEventListener('close', (e) => resolve(e));
    });

    expect(closeEvent.code).toBe(4001);
    expect(closeEvent.reason).toBe('unauthorized');
  });

  it('closes connection with wrong API key', async () => {
    const ws = wsWithAuth(`ws://localhost:${AUTH_PORT}`, 'wrong-key');

    const closeEvent = await new Promise<CloseEvent>((resolve) => {
      ws.addEventListener('close', (e) => resolve(e));
    });

    expect(closeEvent.code).toBe(4001);
    expect(closeEvent.reason).toBe('unauthorized');
  });

  it('accepts connection with valid API key', async () => {
    const ws = wsWithAuth(`ws://localhost:${AUTH_PORT}`, VALID_KEY);

    const message = await new Promise<string>((resolve) => {
      ws.addEventListener('message', (e) => {
        resolve(e.data as string);
        ws.close();
      });
    });

    const envelope = JSON.parse(message);
    expect(envelope.type).toBe('event');
    expect(envelope.payload).toEqual({ event: 'welcome' });
  });
});

describe('ConnectionRegistry', () => {
  it('registers a browser and tracks status', async () => {
    const registry = new ConnectionRegistry();
    const mockWs = { data: {} } as unknown as ServerWebSocket<WsData>;

    const result = await registry.register(mockWs, 'b-1');
    expect(result.success).toBe(true);
    expect(registry.getStatus('b-1')).toBe('offline');

    registry.setStatus('b-1', 'online');
    expect(registry.getStatus('b-1')).toBe('online');
  });

  it('always succeeds (auth happened at handshake)', async () => {
    const registry = new ConnectionRegistry();
    const mockWs = {
      data: { userId: 'user-1' },
    } as unknown as ServerWebSocket<WsData>;

    const result = await registry.register(mockWs, 'b-2');
    expect(result.success).toBe(true);
  });

  it('rejects duplicate registration from a different open connection', async () => {
    const registry = new ConnectionRegistry();
    const first = {
      data: { userId: 'user-1' },
      readyState: 1,
    } as unknown as ServerWebSocket<WsData>;
    const second = {
      data: { userId: 'user-1' },
      readyState: 1,
    } as unknown as ServerWebSocket<WsData>;

    expect((await registry.register(first, 'b-dup')).success).toBe(true);
    const result = await registry.register(second, 'b-dup');
    expect(result.success).toBe(false);
    expect(result.error).toBe('browser_id_in_use');
    expect(registry.getWebSocket('b-dup')).toBe(first);
  });

  it('allows registration when the previous connection closed', async () => {
    const registry = new ConnectionRegistry();
    const first = {
      data: { userId: 'user-1' },
      readyState: 3,
    } as unknown as ServerWebSocket<WsData>;
    const second = {
      data: { userId: 'user-1' },
      readyState: 1,
    } as unknown as ServerWebSocket<WsData>;

    expect((await registry.register(first, 'b-stale')).success).toBe(true);
    expect((await registry.register(second, 'b-stale')).success).toBe(true);
    expect(registry.getWebSocket('b-stale')).toBe(second);
  });

  it('allows the same connection to re-register idempotently', async () => {
    const registry = new ConnectionRegistry();
    const ws = {
      data: { userId: 'user-1' },
      readyState: 1,
    } as unknown as ServerWebSocket<WsData>;

    expect((await registry.register(ws, 'b-re')).success).toBe(true);
    expect((await registry.register(ws, 'b-re')).success).toBe(true);
    expect(registry.getWebSocket('b-re')).toBe(ws);
  });
});
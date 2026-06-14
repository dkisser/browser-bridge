import { describe, expect, it, beforeAll, afterAll } from 'bun:test';
import { NoopAuthProvider } from '@my/shared/auth';
import { ApiKeyAuthProvider } from '@my/shared/auth';
import { startServer } from '../index';
import { ConnectionRegistry } from '../registry';
import type { Server } from 'bun';

describe('WS server routing', () => {
  let server: Server;

  beforeAll(() => {
    server = startServer(3098);
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

describe('ConnectionRegistry', () => {
  it('registers a browser and tracks status', async () => {
    const registry = new ConnectionRegistry(new NoopAuthProvider());
    const mockWs = { data: {} } as any;

    const result = await registry.register(mockWs, 'b-1', 'any-token');
    expect(result.success).toBe(true);
    expect(registry.getStatus('b-1')).toBe('offline');

    registry.setStatus('b-1', 'online');
    expect(registry.getStatus('b-1')).toBe('online');
  });

  it('rejects invalid token', async () => {
    const registry = new ConnectionRegistry(new ApiKeyAuthProvider({ 'good-key': 'user-1' }));
    const mockWs = { data: {} } as any;

    const result = await registry.register(mockWs, 'b-2', 'bad-key');
    expect(result.success).toBe(false);
    expect(result.error).toBe('invalid_token');
  });
});

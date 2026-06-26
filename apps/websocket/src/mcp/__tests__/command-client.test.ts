import { describe, expect, it, afterEach } from 'bun:test';
import type { Server } from 'bun';
import { sendCommand, sendEvent } from '../command-client';
import { encode, decode } from '@browser-bridge/websocket/protocol';
import type { Envelope } from '@browser-bridge/shared';

describe('sendCommand', () => {
  let server: Server<undefined> | undefined;

  afterEach(() => {
    server?.stop();
    server = undefined;
  });

  it('sends a command and returns the response payload', async () => {
    server = Bun.serve({
      port: 0,
      hostname: '127.0.0.1',
      fetch(req, wsServer) {
        const url = new URL(req.url);
        if (url.pathname === '/ws') {
          const upgraded = wsServer.upgrade(req);
          if (!upgraded) return new Response('Upgrade failed', { status: 400 });
        }
        return new Response('Not found', { status: 404 });
      },
      websocket: {
        open() {},
        message(ws, data) {
          const envelope = decode(data as string) as Envelope;
          const response: Envelope = {
            id: envelope.id,
            type: 'response',
            browserId: envelope.browserId,
            payload: { status: 'ok', data: { title: 'Example' } },
            timestamp: Date.now(),
          };
          ws.send(
            encode('response', response.payload, {
              id: response.id,
              browserId: response.browserId,
            }),
          );
        },
        close() {},
      },
    });

    const result = await sendCommand({
      serverUrl: `ws://127.0.0.1:${server.port}/ws`,
      browserId: 'browser-a',
      command: 'pageinfo',
      params: {},
      timeoutMs: 1000,
    });

    expect(result.status).toBe('ok');
    expect(result.data).toEqual({ title: 'Example' });
  });

  it('rejects on timeout', async () => {
    server = Bun.serve({
      port: 0,
      hostname: '127.0.0.1',
      fetch(req, wsServer) {
        const url = new URL(req.url);
        if (url.pathname === '/ws') {
          const upgraded = wsServer.upgrade(req);
          if (!upgraded) return new Response('Upgrade failed', { status: 400 });
        }
        return new Response('Not found', { status: 404 });
      },
      websocket: { open() {}, message() {}, close() {} },
    });

    await expect(
      sendCommand({
        serverUrl: `ws://127.0.0.1:${server.port}/ws`,
        browserId: 'browser-a',
        command: 'pageinfo',
        params: {},
        timeoutMs: 50,
      }),
    ).rejects.toThrow('timeout');
  });
});

describe('sendEvent', () => {
  let server: Server<undefined> | undefined;

  afterEach(() => {
    server?.stop();
    server = undefined;
  });

  it('sends an event and returns the response payload', async () => {
    server = Bun.serve({
      port: 0,
      hostname: '127.0.0.1',
      fetch(req, wsServer) {
        const url = new URL(req.url);
        if (url.pathname === '/ws') {
          const upgraded = wsServer.upgrade(req);
          if (!upgraded) return new Response('Upgrade failed', { status: 400 });
        }
        return new Response('Not found', { status: 404 });
      },
      websocket: {
        open() {},
        message(ws, data) {
          const envelope = decode(data as string) as Envelope;
          const response: Envelope = {
            id: envelope.id,
            type: 'response',
            browserId: envelope.browserId,
            payload: {
              status: 'ok',
              data: [{ browserId: 'a', status: 'online' }],
            },
            timestamp: Date.now(),
          };
          ws.send(
            encode('response', response.payload, {
              id: response.id,
              browserId: response.browserId,
            }),
          );
        },
        close() {},
      },
    });

    const result = await sendEvent({
      serverUrl: `ws://127.0.0.1:${server.port}/ws`,
      event: 'list_browsers',
      payload: {},
      timeoutMs: 1000,
    });

    expect(result.status).toBe('ok');
    expect(Array.isArray(result.data)).toBe(true);
  });
});

import { describe, expect, it } from 'bun:test';
import type { Envelope, ResponsePayload } from '@browser-bridge/shared';
import { decode, encode } from '@browser-bridge/websocket/protocol';
import type { Server } from 'bun';
import { fetchBrowserList, resolveTargetBrowser } from '../browser-lookup';
import { createBrowserSessionStore } from '../browser-session';

function createListServer(responsePayload: ResponsePayload) {
  return Bun.serve({
    port: 0,
    hostname: '127.0.0.1',
    fetch(req, wsServer) {
      if (new URL(req.url).pathname === '/ws') {
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
          payload: responsePayload,
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
}

function createListServerWithBrowsers(browsers: unknown[]) {
  return createListServer({ status: 'ok', data: browsers });
}

function makeContext(server: Server<undefined>, sessionId: string) {
  const sessions = createBrowserSessionStore(10000);
  return {
    sessionId,
    sessions,
    websocketUrl: `ws://127.0.0.1:${server.port}/ws`,
  };
}

describe('fetchBrowserList', () => {
  it('returns parsed browser list', async () => {
    const server = createListServerWithBrowsers([
      { browserId: 'a', userId: 'u', status: 'online', lastSeen: Date.now() },
    ]);
    const context = makeContext(server, 's1');
    try {
      const list = await fetchBrowserList(context, 1000);
      expect(list).toHaveLength(1);
      expect(list[0].browserId).toBe('a');
    } finally {
      server.stop();
    }
  });

  it('filters out malformed browser objects', async () => {
    const server = createListServerWithBrowsers([
      { browserId: 'a', userId: 'u', status: 'online', lastSeen: Date.now() },
      { notABrowser: true },
      { browserId: 'b', userId: 'u', status: 'online', lastSeen: Date.now() },
    ]);
    const context = makeContext(server, 's1');
    try {
      const list = await fetchBrowserList(context, 1000);
      expect(list).toHaveLength(2);
      expect(list.map((b) => b.browserId)).toEqual(['a', 'b']);
    } finally {
      server.stop();
    }
  });

  it('throws when server returns error', async () => {
    const server = createListServer({
      status: 'error',
      error: 'something went wrong',
    });
    const context = makeContext(server, 's1');
    try {
      await expect(fetchBrowserList(context, 1000)).rejects.toThrow(
        'something went wrong',
      );
    } finally {
      server.stop();
    }
  });

  it('returns empty array when data is not an array', async () => {
    const server = createListServer({
      status: 'ok',
      data: { notAnArray: true },
    });
    const context = makeContext(server, 's1');
    try {
      const list = await fetchBrowserList(context, 1000);
      expect(list).toEqual([]);
    } finally {
      server.stop();
    }
  });
});

describe('resolveTargetBrowser', () => {
  it('uses explicit session browser', async () => {
    const server = createListServerWithBrowsers([
      { browserId: 'a', userId: 'u', status: 'online', lastSeen: Date.now() },
      { browserId: 'b', userId: 'u', status: 'online', lastSeen: Date.now() },
    ]);
    const context = makeContext(server, 's1');
    context.sessions.setBrowser('s1', 'a');

    try {
      const result = await resolveTargetBrowser(context, 1000);

      expect(result.success).toBe(true);
      if (result.success) expect(result.browserId).toBe('a');
    } finally {
      server.stop();
    }
  });

  it('auto-detects single browser', async () => {
    const server = createListServerWithBrowsers([
      { browserId: 'a', userId: 'u', status: 'online', lastSeen: Date.now() },
    ]);
    const context = makeContext(server, 's1');

    try {
      const result = await resolveTargetBrowser(context, 1000);

      expect(result.success).toBe(true);
      if (result.success) expect(result.browserId).toBe('a');
    } finally {
      server.stop();
    }
  });

  it('fails when no browsers are connected', async () => {
    const server = createListServerWithBrowsers([]);
    const context = makeContext(server, 's1');

    try {
      const result = await resolveTargetBrowser(context, 1000);

      expect(result.success).toBe(false);
      if (!result.success) {
        expect(result.message).toContain('No browser connected');
      }
    } finally {
      server.stop();
    }
  });

  it('returns failure when listing browsers fails', async () => {
    const server = createListServer({
      status: 'error',
      error: 'server blew up',
    });
    const context = makeContext(server, 's1');
    try {
      const result = await resolveTargetBrowser(context, 1000);
      expect(result.success).toBe(false);
      if (!result.success) {
        expect(result.message).toBe('server blew up');
      }
    } finally {
      server.stop();
    }
  });
});

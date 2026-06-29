import { describe, expect, it } from 'bun:test';
import type { Envelope, ResponsePayload } from '@browser-bridge/shared';
import { decode, encode } from '@browser-bridge/websocket/protocol';
import { createBrowserSessionStore } from '../../browser-session';
import { executeListBrowsers } from '../../tools/list-browsers';

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

describe('executeListBrowsers', () => {
  it('returns a list of browsers', async () => {
    const server = createListServer({
      status: 'ok',
      data: [
        {
          browserId: 'browser-a',
          userId: 'u1',
          status: 'online',
          lastSeen: Date.now(),
        },
      ],
    });

    const sessions = createBrowserSessionStore(10000);
    try {
      const result = await executeListBrowsers(
        {
          sessionId: 's1',
          sessions,
          websocketUrl: `ws://127.0.0.1:${server.port}/ws`,
        },
        {},
      );

      expect(result).toContain('browser-a');
    } finally {
      server.stop();
    }
  });

  it('returns "No browsers connected" when list is empty', async () => {
    const server = createListServer({ status: 'ok', data: [] });
    const sessions = createBrowserSessionStore(10000);
    try {
      const result = await executeListBrowsers(
        {
          sessionId: 's1',
          sessions,
          websocketUrl: `ws://127.0.0.1:${server.port}/ws`,
        },
        {},
      );
      expect(result).toBe('No browsers connected.');
    } finally {
      server.stop();
    }
  });

  it('throws when server returns an error', async () => {
    const server = createListServer({
      status: 'error',
      error: 'ws server down',
    });
    const sessions = createBrowserSessionStore(10000);
    try {
      await expect(
        executeListBrowsers(
          {
            sessionId: 's1',
            sessions,
            websocketUrl: `ws://127.0.0.1:${server.port}/ws`,
          },
          {},
        ),
      ).rejects.toThrow('ws server down');
    } finally {
      server.stop();
    }
  });
});

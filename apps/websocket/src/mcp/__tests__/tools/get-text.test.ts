import { describe, expect, it } from 'bun:test';
import type { Envelope } from '@browser-bridge/shared';
import { decode, encode } from '@browser-bridge/websocket/protocol';
import { createBrowserSessionStore } from '../../browser-session';
import { executeGettext } from '../../tools/get-text';

describe('executeGettext', () => {
  it('returns text content as string', async () => {
    const server = Bun.serve({
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
          const payload =
            envelope.type === 'event'
              ? {
                  status: 'ok',
                  data: [
                    {
                      browserId: 'a',
                      userId: 'u',
                      status: 'online',
                      lastSeen: Date.now(),
                    },
                  ],
                }
              : {
                  status: 'ok',
                  data: 'Hello World',
                };
          ws.send(
            encode('response', payload, {
              id: envelope.id,
              browserId: envelope.browserId,
            }),
          );
        },
        close() {},
      },
    });

    const sessions = createBrowserSessionStore(10000);
    try {
      const result = await executeGettext(
        {
          sessionId: 's1',
          sessions,
          websocketUrl: `ws://127.0.0.1:${server.port}/ws`,
        },
        { selector: 'h1' },
      );
      expect(result).toBe('Hello World');
    } finally {
      server.stop();
    }
  });
});

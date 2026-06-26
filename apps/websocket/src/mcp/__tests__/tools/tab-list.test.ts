import { describe, expect, it } from 'bun:test';
import type { Envelope } from '@browser-bridge/shared';
import { decode, encode } from '@browser-bridge/websocket/protocol';
import { createBrowserSessionStore } from '../../browser-session';
import { executeTabList } from '../../tools/tab-list';

describe('executeTabList', () => {
  it('returns tab list as JSON string', async () => {
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
                  data: [
                    { id: 1, title: 'Tab 1', url: 'https://example.com/1' },
                    { id: 2, title: 'Tab 2', url: 'https://example.com/2' },
                  ],
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
      const result = await executeTabList(
        {
          sessionId: 's1',
          sessions,
          websocketUrl: `ws://127.0.0.1:${server.port}/ws`,
        },
        {},
      );
      expect(result).toContain('Tab 1');
      expect(result).toContain('Tab 2');
      expect(result).toContain('https://example.com/1');
    } finally {
      server.stop();
    }
  });
});

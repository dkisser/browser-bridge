import { describe, expect, it } from 'bun:test';
import type { Envelope } from '@browser-bridge/shared';
import { decode, encode } from '../../../src/protocol';
import { createBrowserSessionStore } from '../../../src/mcp/browser-session';
import { executeNavigate } from '../../../src/mcp/tools/navigate';

describe('executeNavigate', () => {
  it('navigates to url and returns success', async () => {
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
              : { status: 'ok', message: 'Navigated' };
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
      const result = await executeNavigate(
        {
          sessionId: 's1',
          sessions,
          websocketUrl: `ws://127.0.0.1:${server.port}/ws`,
        },
        { tab_id: 42, url: 'https://example.com' },
      );

      expect(result).toContain('Navigated');
    } finally {
      server.stop();
    }
  });

  it('surfaces the human guidance from a policy denial instead of the bare reason code', async () => {
    const denialMessage =
      'Origin not approved: https://example.com. A human must approve this ' +
      'origin in the Browser Bridge extension popup, then the command can be retried.';
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
                  status: 'error',
                  error: 'origin_not_approved',
                  message: denialMessage,
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
      await expect(
        executeNavigate(
          {
            sessionId: 's1',
            sessions,
            websocketUrl: `ws://127.0.0.1:${server.port}/ws`,
          },
          { tab_id: 42, url: 'https://example.com' },
        ),
      ).rejects.toThrow(denialMessage);
    } finally {
      server.stop();
    }
  });
});

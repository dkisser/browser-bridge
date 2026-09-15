import { describe, expect, it } from 'bun:test';
import type { Envelope } from '@browser-bridge/shared';
import { decode, encode } from '@browser-bridge/websocket/protocol';
import { createBrowserSessionStore } from '../../../src/mcp/browser-session';
import { executeClick } from '../../../src/mcp/tools/click';
import { createMockWsServer } from './mock-ws-server';

describe('executeClick', () => {
  it('sends click command and returns success', async () => {
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
              : { status: 'ok', message: 'Clicked' };
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
      const result = await executeClick(
        {
          sessionId: 's1',
          sessions,
          websocketUrl: `ws://127.0.0.1:${server.port}/ws`,
        },
        { tab_id: 42, selector: '#submit' },
      );
      expect(result).toContain('Clicked');
    } finally {
      server.stop();
    }
  });

  it('points to tab_list when the tab id does not exist', async () => {
    const { server } = createMockWsServer({
      status: 'error',
      error: 'No tab with id: 0',
    });
    const sessions = createBrowserSessionStore(10000);
    try {
      await expect(
        executeClick(
          {
            sessionId: 's1',
            sessions,
            websocketUrl: `ws://127.0.0.1:${server.port}/ws`,
          },
          { tab_id: 0, selector: '#submit' },
        ),
      ).rejects.toThrow('tab_list');
    } finally {
      server.stop();
    }
  });
});

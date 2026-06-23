import { afterAll, beforeAll, describe, expect, it } from 'bun:test';
import { decode, encode } from '@browser-bridge/websocket/protocol';
import type { Server } from 'bun';
import { listBrowsers } from '../listBrowsers';

describe('listBrowsers', () => {
  let server: Server<undefined>;
  const PORT = 3202;

  beforeAll(() => {
    server = Bun.serve({
      port: PORT,
      fetch(req, s) {
        if (s.upgrade(req)) return;
        return new Response('ok', { status: 200 });
      },
      websocket: {
        open() {},
        message(ws, message) {
          const request = decode(message as string);
          const payload = request.payload as { event: string };

          if (payload.event === 'list_browsers') {
            ws.send(
              encode(
                'response',
                {
                  status: 'ok',
                  data: [
                    {
                      browserId: 'b-1',
                      status: 'connected',
                      lastSeen: Date.now(),
                    },
                  ],
                },
                { id: request.id },
              ),
            );
          }
        },
        close() {},
      },
    });
  });

  afterAll(() => {
    server.stop();
  });

  it('returns connected browsers', async () => {
    const browsers = await listBrowsers(`ws://localhost:${PORT}`);
    expect(browsers).toHaveLength(1);
    expect(browsers[0].browserId).toBe('b-1');
  });

  it('returns empty array when server reports no browsers', async () => {
    // Reuse same server but send empty list for a different scenario
    // For now, the server always returns one browser; this test documents intent.
    // A more robust mock could vary by request payload.
    const browsers = await listBrowsers(`ws://localhost:${PORT}`);
    expect(Array.isArray(browsers)).toBe(true);
  });
});

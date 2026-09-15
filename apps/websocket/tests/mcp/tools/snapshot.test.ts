import { describe, expect, it } from 'bun:test';
import type { CommandPayload, Envelope } from '@browser-bridge/shared';
import { decode, encode } from '@browser-bridge/websocket/protocol';
import { createBrowserSessionStore } from '../../../src/mcp/browser-session';
import { executeSnapshot } from '../../../src/mcp/tools/snapshot';

function startMockServer(captured: CommandPayload[]) {
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
                data: {
                  snapshot: 'Page: Example | https://example.com\nlink [Docs]',
                  truncated: false,
                  nodes_total: 2,
                  nodes_emitted: 1,
                  tier: 0,
                },
              };
        if (envelope.type === 'command') {
          captured.push(envelope.payload as CommandPayload);
        }
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
}

describe('executeSnapshot', () => {
  it('sends snapshot command and returns text with stats', async () => {
    const captured: CommandPayload[] = [];
    const server = startMockServer(captured);

    const sessions = createBrowserSessionStore(10000);
    try {
      const result = await executeSnapshot(
        {
          sessionId: 's1',
          sessions,
          websocketUrl: `ws://127.0.0.1:${server.port}/ws`,
        },
        { tab_id: 42, max_chars: 3000 },
      );
      expect(result).toContain('link [Docs]');
      expect(result).toContain('[1/2 nodes | tier=0]');
      expect(captured).toHaveLength(1);
      expect(captured[0].params.filter).toBe('interactive');
      expect(captured[0].params.max_chars).toBe(3000);
    } finally {
      server.stop();
    }
  });

  it('passes filter=full through and defaults its budget to 3000', async () => {
    const captured: CommandPayload[] = [];
    const server = startMockServer(captured);

    const sessions = createBrowserSessionStore(10000);
    try {
      await executeSnapshot(
        {
          sessionId: 's1',
          sessions,
          websocketUrl: `ws://127.0.0.1:${server.port}/ws`,
        },
        { tab_id: 42, filter: 'full' },
      );
      expect(captured).toHaveLength(1);
      expect(captured[0].params.filter).toBe('full');
      expect(captured[0].params.max_chars).toBe(3000);
    } finally {
      server.stop();
    }
  });

  it('defaults the interactive budget to 8000', async () => {
    const captured: CommandPayload[] = [];
    const server = startMockServer(captured);

    const sessions = createBrowserSessionStore(10000);
    try {
      await executeSnapshot(
        {
          sessionId: 's1',
          sessions,
          websocketUrl: `ws://127.0.0.1:${server.port}/ws`,
        },
        { tab_id: 42 },
      );
      expect(captured).toHaveLength(1);
      expect(captured[0].params.max_chars).toBe(8000);
    } finally {
      server.stop();
    }
  });
});

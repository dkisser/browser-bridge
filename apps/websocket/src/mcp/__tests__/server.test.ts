import { afterEach, describe, expect, it } from 'bun:test';
import type { Envelope } from '@browser-bridge/shared';
import { decode, encode } from '@browser-bridge/websocket/protocol';
import type { FastMCP } from 'fastmcp';
import { startMcpServer } from '../server';

describe('startMcpServer', () => {
  let wsServer: ReturnType<typeof Bun.serve> | undefined;
  let mcpServer: FastMCP | undefined;

  afterEach(async () => {
    await mcpServer?.stop();
    wsServer?.stop();
  });

  it('starts without error', async () => {
    wsServer = Bun.serve({
      port: 0,
      hostname: '127.0.0.1',
      fetch(req, wsServerInstance) {
        if (new URL(req.url).pathname === '/ws') {
          const upgraded = wsServerInstance.upgrade(req, { data: {} });
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
              data: [
                {
                  browserId: 'a',
                  userId: 'u',
                  status: 'online',
                  lastSeen: Date.now(),
                },
              ],
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

    mcpServer = await startMcpServer({
      websocketUrl: `ws://127.0.0.1:${wsServer.port}/ws`,
      port: 0,
      hostname: '127.0.0.1',
      defaultTimeoutMs: 1000,
      version: '0.0.0',
    });

    expect(mcpServer).toBeDefined();
  });
});

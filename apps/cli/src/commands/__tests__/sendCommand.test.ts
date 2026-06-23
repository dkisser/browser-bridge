import { afterAll, beforeAll, describe, expect, it } from 'bun:test';
import type { CommandType } from '@browser-bridge/shared/types';
import { decode, encode } from '@browser-bridge/websocket/protocol';
import type { Server } from 'bun';
import { sendCommand } from '../sendCommand';

describe('sendCommand', () => {
  let server: Server<undefined>;
  const PORT = 3203;

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
          const payload = request.payload as { command: string };

          if (payload.command === 'pageinfo') {
            ws.send(
              encode(
                'response',
                {
                  status: 'ok',
                  data: { title: 'Test Page', url: 'https://example.com' },
                },
                { id: request.id },
              ),
            );
          } else if (payload.command === 'fail') {
            ws.send(
              encode(
                'response',
                {
                  status: 'error',
                  error: 'command_error',
                  message: 'It failed',
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

  it('throws when browser is missing', async () => {
    await expect(
      sendCommand({ server: `ws://localhost:${PORT}` }, 'pageinfo'),
    ).rejects.toThrow('Required: --browser <id>');
  });

  it('returns data on success', async () => {
    const result = await sendCommand(
      { server: `ws://localhost:${PORT}`, browser: 'b-1' },
      'pageinfo',
    );
    expect(result).toEqual({ title: 'Test Page', url: 'https://example.com' });
  });

  it('throws when server reports command error', async () => {
    await expect(
      sendCommand(
        { server: `ws://localhost:${PORT}`, browser: 'b-1' },
        'fail' as CommandType,
      ),
    ).rejects.toThrow('It failed');
  });
});

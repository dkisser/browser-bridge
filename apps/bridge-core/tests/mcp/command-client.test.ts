import { afterEach, describe, expect, it } from 'bun:test';
import type { Envelope } from '@browser-bridge/shared';
import { decode, encode } from '../../src/protocol';
import type { Server } from 'bun';
import {
  commandErrorMessage,
  sendCommand,
  sendEvent,
  withRecoveryHint,
} from '../../src/mcp/command-client';

describe('sendCommand', () => {
  let server: Server<undefined> | undefined;

  afterEach(() => {
    server?.stop();
    server = undefined;
  });

  it('sends a command and returns the response payload', async () => {
    server = Bun.serve({
      port: 0,
      hostname: '127.0.0.1',
      fetch(req, wsServer) {
        const url = new URL(req.url);
        if (url.pathname === '/ws') {
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
            payload: { status: 'ok', data: { title: 'Example' } },
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

    const result = await sendCommand({
      serverUrl: `ws://127.0.0.1:${server.port}/ws`,
      browserId: 'browser-a',
      command: 'pageinfo',
      params: {},
      timeoutMs: 1000,
    });

    expect(result.status).toBe('ok');
    expect(result.data).toEqual({ title: 'Example' });
  });

  it('rejects on timeout', async () => {
    server = Bun.serve({
      port: 0,
      hostname: '127.0.0.1',
      fetch(req, wsServer) {
        const url = new URL(req.url);
        if (url.pathname === '/ws') {
          const upgraded = wsServer.upgrade(req);
          if (!upgraded) return new Response('Upgrade failed', { status: 400 });
        }
        return new Response('Not found', { status: 404 });
      },
      websocket: { open() {}, message() {}, close() {} },
    });

    await expect(
      sendCommand({
        serverUrl: `ws://127.0.0.1:${server.port}/ws`,
        browserId: 'browser-a',
        command: 'pageinfo',
        params: {},
        timeoutMs: 50,
      }),
    ).rejects.toThrow('timeout');
  });
});

describe('sendEvent', () => {
  let server: Server<undefined> | undefined;

  afterEach(() => {
    server?.stop();
    server = undefined;
  });

  it('sends an event and returns the response payload', async () => {
    server = Bun.serve({
      port: 0,
      hostname: '127.0.0.1',
      fetch(req, wsServer) {
        const url = new URL(req.url);
        if (url.pathname === '/ws') {
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
            payload: {
              status: 'ok',
              data: [{ browserId: 'a', status: 'online' }],
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

    const result = await sendEvent({
      serverUrl: `ws://127.0.0.1:${server.port}/ws`,
      event: 'list_browsers',
      payload: {},
      timeoutMs: 1000,
    });

    expect(result.status).toBe('ok');
    expect(Array.isArray(result.data)).toBe(true);
  });
});

describe('withRecoveryHint', () => {
  it('appends the tab_list hint for the legacy "No tab with id: N" pattern', () => {
    const out = withRecoveryHint({
      status: 'error',
      error: 'No tab with id: 5',
    });
    expect(out.error).toContain('Call tab_list');
  });

  it('does not duplicate the tab_list hint', () => {
    const out = withRecoveryHint({
      status: 'error',
      error: 'No tab with id: 5. Call tab_list to discover valid tab ids.',
    });
    expect(out.error?.match(/tab_list/g)).toHaveLength(1);
  });

  it('appends a hint when the SW sent a structured reason', () => {
    for (const reason of [
      'tab_not_found',
      'restricted_page',
      'injection_failed',
      'no_listener',
    ] as const) {
      const out = withRecoveryHint({
        status: 'error',
        error: 'some upstream message',
        reason,
      });
      expect(out.error).toContain('some upstream message');
      // Every reason maps to a non-empty hint.
      expect((out.error?.length ?? 0) > 'some upstream message'.length).toBe(
        true,
      );
    }
  });

  it('does not duplicate the hint when called twice', () => {
    const once = withRecoveryHint({
      status: 'error',
      error: 'msg',
      reason: 'restricted_page',
    });
    const twice = withRecoveryHint(once);
    expect(twice.error).toBe(once.error);
  });

  it('lands the hint where the consumer reads it when the SW sent a message', () => {
    // The SW reports content-script failures as
    // { error: reason, message: <detail>, reason } — see background.ts. The
    // consumer is commandErrorMessage, which returns `message ?? error`, so a
    // hint appended to `error` alone is never surfaced to the agent.
    const out = withRecoveryHint({
      status: 'error',
      error: 'no_listener',
      message: 'content script did not register a listener',
      reason: 'no_listener',
    });

    expect(out.message).toContain('Reload the page or retry');
    expect(commandErrorMessage(out, 'fallback')).toContain(
      'Reload the page or retry',
    );
  });

  it('does not duplicate the hint on message when called twice', () => {
    const once = withRecoveryHint({
      status: 'error',
      error: 'no_listener',
      message: 'detail',
      reason: 'no_listener',
    });
    const twice = withRecoveryHint(once);
    expect(twice.message).toBe(once.message);
  });

  it('passes status:ok payloads through unchanged', () => {
    const out = withRecoveryHint({ status: 'ok', data: { ok: true } });
    expect(out).toEqual({ status: 'ok', data: { ok: true } });
  });
});

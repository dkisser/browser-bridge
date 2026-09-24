import { afterEach, beforeEach, describe, expect, it } from 'bun:test';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import type { Envelope } from '@browser-bridge/shared';
import type { ServerWebSocket } from 'bun';
import type { BrowserServer } from '../src/browser-server';
import { Router } from '../src/router';
import { StateManager } from '../src/state';

/**
 * Unit doubles for the three collaborators. Only the members the router
 * actually touches in these paths are implemented; anything else would be
 * scaffolding the tests do not exercise.
 */
function makeRouter() {
  const sent: string[] = [];
  const browser = {
    hasExtension: () => false,
    sendToExtension: (text: string) => {
      sent.push(text);
    },
  } as unknown as BrowserServer;

  const statuses: Array<[string, string]> = [];
  const registry = {
    setStatus: (browserId: string, status: string) => {
      statuses.push([browserId, status]);
      return true;
    },
  } as unknown as import('../src/server/registry').ConnectionRegistry;

  const state = new StateManager();
  return {
    router: new Router(state, browser, registry),
    state,
    sent,
    statuses,
  };
}

function makeInboundWs() {
  const responses: string[] = [];
  const ws = {
    readyState: 1,
    send: (text: string) => {
      responses.push(text);
    },
  } as unknown as ServerWebSocket<unknown>;
  return { ws, responses };
}

function commandEnvelope(browserId: string, id = 'c1'): Envelope {
  return {
    id,
    type: 'command',
    browserId,
    payload: { command: 'navigate', params: {} },
    timestamp: Date.now(),
  } as Envelope;
}

describe('Router command buffering', () => {
  let dir: string;
  const originalHome = process.env.BB_HOME;

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), 'bb-router-'));
    process.env.BB_HOME = dir;
  });

  afterEach(() => {
    rmSync(dir, { recursive: true, force: true });
    if (originalHome === undefined) delete process.env.BB_HOME;
    else process.env.BB_HOME = originalHome;
  });

  it('buffers instead of rejecting while the extension is between reconnects', () => {
    const { router, state } = makeRouter();
    state.status = 'idle_wait';
    const { ws, responses } = makeInboundWs();

    router.handleInboundCommand(commandEnvelope(state.browserId), ws);

    // No immediate browser_offline / cannot_buffer: the command is held for a
    // fast reconnect.
    expect(responses).toEqual([]);
    expect(state.getBufferedCommand()).not.toBeNull();
  });

  it('delivers a command buffered while the extension was away', () => {
    const { router, state, sent } = makeRouter();
    state.status = 'idle_wait';
    let timedOut = false;
    expect(
      state.bufferCommand('{"id":"buffered"}', () => {
        timedOut = true;
      }),
    ).toBe(true);

    router.handleBrowserConnect();

    // Regression: flipping status before reading the buffer cleared it (and
    // cancelled its timeout) on the way in, so the command was dropped with no
    // response at all.
    expect(sent).toEqual(['{"id":"buffered"}']);
    expect(timedOut).toBe(false);
  });

  it('consumes the buffer rather than leaving it set', () => {
    const { router, state } = makeRouter();
    state.status = 'idle_wait';
    state.bufferCommand('{"id":"buffered"}', () => undefined);

    router.handleBrowserConnect();

    expect(state.getBufferedCommand()).toBeNull();
    // Widened: the assignment above narrows the getter's type to the literal
    // 'idle_wait', which hides the post-connect value from the checker.
    const statusAfterConnect: string = state.status;
    expect(statusAfterConnect).toBe('online');
  });
});

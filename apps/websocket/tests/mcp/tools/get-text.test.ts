import { describe, expect, it } from 'bun:test';
import type { GettextResult } from '@browser-bridge/shared';
import { createBrowserSessionStore } from '../../../src/mcp/browser-session';
import { executeGettext } from '../../../src/mcp/tools/get-text';
import { createMockWsServer } from './mock-ws-server';

describe('executeGettext', () => {
  it('returns text content from the contract result', async () => {
    const mockResult: GettextResult = { text: 'Hello World' };
    const { server } = createMockWsServer({ status: 'ok', data: mockResult });
    const sessions = createBrowserSessionStore(10000);
    try {
      const result = await executeGettext(
        {
          sessionId: 's1',
          sessions,
          websocketUrl: `ws://127.0.0.1:${server.port}/ws`,
        },
        { tab_id: 42, selector: 'h1' },
      );
      expect(result).toBe('Hello World');
    } finally {
      server.stop();
    }
  });

  it('returns empty string when textContent is null', async () => {
    const mockResult: GettextResult = { text: null };
    const { server } = createMockWsServer({ status: 'ok', data: mockResult });
    const sessions = createBrowserSessionStore(10000);
    try {
      const result = await executeGettext(
        {
          sessionId: 's1',
          sessions,
          websocketUrl: `ws://127.0.0.1:${server.port}/ws`,
        },
        { tab_id: 42, selector: 'h1' },
      );
      expect(result).toBe('');
    } finally {
      server.stop();
    }
  });
});

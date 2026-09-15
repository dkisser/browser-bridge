import { describe, expect, it } from 'bun:test';
import type { GethtmlResult } from '@browser-bridge/shared';
import { createBrowserSessionStore } from '../../../src/mcp/browser-session';
import { executeGethtml } from '../../../src/mcp/tools/get-html';
import { createMockWsServer } from './mock-ws-server';

describe('executeGethtml', () => {
  it('returns html content from the contract result', async () => {
    const mockResult: GethtmlResult = { html: '<div>Hello World</div>' };
    const { server } = createMockWsServer({ status: 'ok', data: mockResult });
    const sessions = createBrowserSessionStore(10000);
    try {
      const result = await executeGethtml(
        {
          sessionId: 's1',
          sessions,
          websocketUrl: `ws://127.0.0.1:${server.port}/ws`,
        },
        { tab_id: 42, selector: 'div' },
      );
      expect(result).toBe('<div>Hello World</div>');
    } finally {
      server.stop();
    }
  });
});

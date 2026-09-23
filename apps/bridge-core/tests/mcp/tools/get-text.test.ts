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

  it('explains when the element matched but has no text', async () => {
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
      expect(result).toContain('h1');
      expect(result).toContain('no text content');
      expect(result).not.toBe('');
    } finally {
      server.stop();
    }
  });

  it('explains when the element text is empty or whitespace-only', async () => {
    for (const text of ['', '   ']) {
      const mockResult: GettextResult = { text };
      const { server } = createMockWsServer({
        status: 'ok',
        data: mockResult,
      });
      const sessions = createBrowserSessionStore(10000);
      try {
        const result = await executeGettext(
          {
            sessionId: 's1',
            sessions,
            websocketUrl: `ws://127.0.0.1:${server.port}/ws`,
          },
          { tab_id: 42, selector: '.row' },
        );
        expect(result).toContain('no text content');
      } finally {
        server.stop();
      }
    }
  });

  it('rejects whole-page-sized text instead of dumping it into context', async () => {
    const mockResult: GettextResult = { text: 'x'.repeat(100_001) };
    const { server } = createMockWsServer({ status: 'ok', data: mockResult });
    const sessions = createBrowserSessionStore(10000);
    try {
      await expect(
        executeGettext(
          {
            sessionId: 's1',
            sessions,
            websocketUrl: `ws://127.0.0.1:${server.port}/ws`,
          },
          { tab_id: 42, selector: 'body' },
        ),
      ).rejects.toThrow(/100001 chars/);
    } finally {
      server.stop();
    }
  });
});

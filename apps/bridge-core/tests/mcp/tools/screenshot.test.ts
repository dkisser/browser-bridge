import { describe, expect, it } from 'bun:test';
import type { ScreenshotResult } from '@browser-bridge/shared';
import { createBrowserSessionStore } from '../../../src/mcp/browser-session';
import { executeScreenshot } from '../../../src/mcp/tools/screenshot';
import { createMockWsServer } from './mock-ws-server';

const PNG_BASE64 =
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=';

describe('executeScreenshot', () => {
  it('returns image content with the data-url prefix stripped', async () => {
    const mockResult: ScreenshotResult = {
      dataUrl: `data:image/png;base64,${PNG_BASE64}`,
    };
    const { server, getLastCommandPayload } = createMockWsServer({
      status: 'ok',
      data: mockResult,
    });
    const sessions = createBrowserSessionStore(10000);
    try {
      const result = await executeScreenshot(
        {
          sessionId: 's1',
          sessions,
          websocketUrl: `ws://127.0.0.1:${server.port}/ws`,
        },
        { tab_id: 42, fullPage: true },
      );

      expect(result.content).toHaveLength(1);
      expect(result.content[0].type).toBe('image');
      expect(result.content[0].data).toBe(PNG_BASE64);
      expect(getLastCommandPayload()).toEqual({
        command: 'screenshot',
        tabId: 42,
        params: { fullPage: true, tabId: 42 },
      });
    } finally {
      server.stop();
    }
  });

  it('throws when the browser returns no image data', async () => {
    const mockResult: ScreenshotResult = { dataUrl: '' };
    const { server } = createMockWsServer({ status: 'ok', data: mockResult });
    const sessions = createBrowserSessionStore(10000);
    try {
      await expect(
        executeScreenshot(
          {
            sessionId: 's1',
            sessions,
            websocketUrl: `ws://127.0.0.1:${server.port}/ws`,
          },
          { tab_id: 42 },
        ),
      ).rejects.toThrow('browser returned no image data');
    } finally {
      server.stop();
    }
  });
});

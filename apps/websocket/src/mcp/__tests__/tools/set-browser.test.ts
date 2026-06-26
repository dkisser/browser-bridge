import { describe, expect, it } from 'bun:test';
import { createBrowserSessionStore } from '../../browser-session';
import { executeSetBrowser } from '../../tools/set-browser';

describe('executeSetBrowser', () => {
  it('sets the browser in session state', async () => {
    const sessions = createBrowserSessionStore(10000);
    const result = await executeSetBrowser(
      { sessionId: 's1', sessions, websocketUrl: 'ws://localhost:3001' },
      { browserId: 'browser-a' },
    );
    expect(result).toContain('browser-a');
    expect(sessions.getSession('s1').browserId).toBe('browser-a');
  });
});

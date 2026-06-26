import { describe, expect, it } from 'bun:test';
import type { BrowserConnection } from '@browser-bridge/shared';
import { resolveBrowser } from '../browser-resolver';

const makeBrowser = (
  id: string,
  status: BrowserConnection['status'],
): BrowserConnection => ({
  browserId: id,
  userId: 'user-1',
  status,
  lastSeen: Date.now(),
});

describe('resolveBrowser', () => {
  it('returns explicit browserId when set', () => {
    const result = resolveBrowser('browser-a', [
      makeBrowser('browser-a', 'online'),
    ]);
    expect(result.success).toBe(true);
    if (result.success) expect(result.browserId).toBe('browser-a');
  });

  it('auto-detects single online browser', () => {
    const result = resolveBrowser(undefined, [
      makeBrowser('browser-a', 'online'),
    ]);
    expect(result.success).toBe(true);
    if (result.success) expect(result.browserId).toBe('browser-a');
  });

  it('fails when no browser is online', () => {
    const result = resolveBrowser(undefined, [
      makeBrowser('browser-a', 'offline'),
    ]);
    expect(result.success).toBe(false);
    if (!result.success)
      expect(result.message).toContain('No browser connected');
  });

  it('fails when multiple browsers are online', () => {
    const browsers = [
      makeBrowser('browser-a', 'online'),
      makeBrowser('browser-b', 'online'),
    ];
    const result = resolveBrowser(undefined, browsers);
    expect(result.success).toBe(false);
    if (!result.success) {
      expect(result.message).toContain('Multiple browsers');
      expect(result.availableBrowsers).toHaveLength(2);
    }
  });

  it('ignores offline browsers during auto-detect', () => {
    const browsers = [
      makeBrowser('browser-a', 'online'),
      makeBrowser('browser-b', 'offline'),
    ];
    const result = resolveBrowser(undefined, browsers);
    expect(result.success).toBe(true);
    if (result.success) expect(result.browserId).toBe('browser-a');
  });

  it('fails when explicit browser is not online', () => {
    const result = resolveBrowser('browser-a', [
      makeBrowser('browser-a', 'offline'),
    ]);
    expect(result.success).toBe(false);
    if (!result.success) expect(result.message).toContain('not online');
  });
});

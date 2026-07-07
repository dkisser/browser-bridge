import { describe, expect, it } from 'bun:test';
import type { CommandPayload } from './types';

describe('CommandPayload', () => {
  it('requires tabId', () => {
    // @ts-expect-error tabId should be required
    const payload: CommandPayload = {
      command: 'navigate',
      params: { url: 'https://example.com' },
    };
    expect(payload.command).toBe('navigate');
  });

  it('accepts a valid tabId', () => {
    const payload: CommandPayload = {
      command: 'navigate',
      tabId: 42,
      params: { url: 'https://example.com' },
    };
    expect(payload.tabId).toBe(42);
  });
});

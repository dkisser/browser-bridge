import { describe, expect, it } from 'bun:test';
import { createBrowserSessionStore } from '../browser-session';

describe('BrowserSessionStore', () => {
  it('creates a session with defaults', () => {
    const store = createBrowserSessionStore(15000);
    const session = store.getSession('session-1');
    expect(session.browserId).toBeUndefined();
    expect(session.defaultTimeoutMs).toBe(15000);
  });

  it('sets and retrieves browserId', () => {
    const store = createBrowserSessionStore(10000);
    const session = store.setBrowser('session-1', 'browser-a');
    expect(session.browserId).toBe('browser-a');
    expect(store.getSession('session-1').browserId).toBe('browser-a');
  });

  it('clears browserId', () => {
    const store = createBrowserSessionStore(10000);
    store.setBrowser('session-1', 'browser-a');
    const session = store.clearBrowser('session-1');
    expect(session.browserId).toBeUndefined();
    expect(store.getSession('session-1').browserId).toBeUndefined();
  });

  it('is isolated per session', () => {
    const store = createBrowserSessionStore(10000);
    store.setBrowser('a', 'browser-a');
    expect(store.getSession('b').browserId).toBeUndefined();
  });

  it('returns the same session object for repeated getSession calls', () => {
    const store = createBrowserSessionStore(10000);
    const a = store.getSession('session-1');
    const b = store.getSession('session-1');
    expect(a).toBe(b);
  });

  it('returns an updated immutable session when setBrowser is called', () => {
    const store = createBrowserSessionStore(10000);
    const original = store.getSession('session-1');
    const updated = store.setBrowser('session-1', 'browser-a');
    expect(updated).not.toBe(original);
    expect(original.browserId).toBeUndefined();
    expect(updated.browserId).toBe('browser-a');
  });
});

import { describe, expect, it } from 'bun:test';
import { createBrowserSessionStore } from '../browser-session';

describe('BrowserSessionStore', () => {
  it('creates a session with defaults', () => {
    const store = createBrowserSessionStore(15000);
    const session = store.createSession('session-1');
    expect(session.getBrowser()).toBeUndefined();
    expect(session.defaultTimeoutMs).toBe(15000);
  });

  it('sets and retrieves browserId', () => {
    const store = createBrowserSessionStore(10000);
    const session = store.createSession('session-1');
    session.setBrowser('browser-a');
    expect(session.getBrowser()).toBe('browser-a');
  });

  it('clears browserId', () => {
    const store = createBrowserSessionStore(10000);
    const session = store.createSession('session-1');
    session.setBrowser('browser-a');
    session.clearBrowser();
    expect(session.getBrowser()).toBeUndefined();
  });

  it('is isolated per session', () => {
    const store = createBrowserSessionStore(10000);
    const a = store.createSession('a');
    const b = store.createSession('b');
    a.setBrowser('browser-a');
    expect(b.getBrowser()).toBeUndefined();
  });
});

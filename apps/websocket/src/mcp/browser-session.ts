export interface BrowserSession {
  readonly defaultTimeoutMs: number;
  readonly browserId: string | undefined;
}

export interface BrowserSessionStore {
  getSession(sessionId: string): BrowserSession;
  setBrowser(sessionId: string, browserId: string): BrowserSession;
  clearBrowser(sessionId: string): BrowserSession;
}

export function createBrowserSessionStore(
  defaultTimeoutMs: number,
): BrowserSessionStore {
  const sessions = new Map<string, BrowserSession>();

  function ensureSession(sessionId: string): BrowserSession {
    let session = sessions.get(sessionId);
    if (!session) {
      session = { defaultTimeoutMs, browserId: undefined };
      sessions.set(sessionId, session);
    }
    return session;
  }

  return {
    getSession: ensureSession,
    setBrowser(sessionId: string, browserId: string): BrowserSession {
      const session = ensureSession(sessionId);
      const updated = { ...session, browserId };
      sessions.set(sessionId, updated);
      return updated;
    },
    clearBrowser(sessionId: string): BrowserSession {
      const session = ensureSession(sessionId);
      const updated = { ...session, browserId: undefined };
      sessions.set(sessionId, updated);
      return updated;
    },
  };
}

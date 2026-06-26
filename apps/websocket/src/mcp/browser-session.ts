export interface BrowserSession {
  readonly defaultTimeoutMs: number;
  getBrowser(): string | undefined;
  setBrowser(browserId: string): void;
  clearBrowser(): void;
}

export interface BrowserSessionStore {
  createSession(sessionId: string): BrowserSession;
}

export function createBrowserSessionStore(
  defaultTimeoutMs: number,
): BrowserSessionStore {
  const sessions = new Map<string, BrowserSession>();

  return {
    createSession(sessionId: string): BrowserSession {
      const existing = sessions.get(sessionId);
      if (existing) return existing;

      let browserId: string | undefined;

      const session: BrowserSession = {
        defaultTimeoutMs,
        getBrowser: () => browserId,
        setBrowser: (id: string) => {
          browserId = id;
        },
        clearBrowser: () => {
          browserId = undefined;
        },
      };

      sessions.set(sessionId, session);
      return session;
    },
  };
}

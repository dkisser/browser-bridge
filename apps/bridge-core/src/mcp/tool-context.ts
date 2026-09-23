import type { BrowserSessionStore } from './browser-session';

export interface ToolContext {
  sessionId: string;
  sessions: BrowserSessionStore;
  websocketUrl: string;
}

export interface ServerContext {
  websocketUrl: string;
  sessions: BrowserSessionStore;
}

import type { BrowserStatus, BrowserConnection, AuthProvider } from '@my/shared';
import type { ServerWebSocket } from 'bun';

interface RegistryEntry {
  browserId: string;
  userId: string;
  ws: ServerWebSocket;
  status: BrowserStatus;
  lastSeen: number;
}

export class ConnectionRegistry {
  private browsers = new Map<string, RegistryEntry>();
  private authProvider: AuthProvider;

  constructor(authProvider: AuthProvider) {
    this.authProvider = authProvider;
  }

  async register(
    ws: ServerWebSocket,
    browserId: string,
    token: string,
  ): Promise<{ success: boolean; error?: string }> {
    const result = await this.authProvider.validateToken(token);
    if (!result.valid) {
      return { success: false, error: 'invalid_token' };
    }

    this.browsers.set(browserId, {
      browserId,
      userId: result.userId,
      ws,
      status: 'offline',
      lastSeen: Date.now(),
    });

    ws.data = { ...ws.data, browserId, userId: result.userId };
    return { success: true };
  }

  setStatus(browserId: string, status: BrowserStatus): boolean {
    const entry = this.browsers.get(browserId);
    if (!entry) return false;
    entry.status = status;
    entry.lastSeen = Date.now();
    return true;
  }

  getStatus(browserId: string): BrowserStatus | undefined {
    return this.browsers.get(browserId)?.status;
  }

  getWebSocket(browserId: string): ServerWebSocket | undefined {
    return this.browsers.get(browserId)?.ws;
  }

  removeByWebSocket(ws: ServerWebSocket): string | undefined {
    for (const [browserId, entry] of this.browsers) {
      if (entry.ws === ws) {
        this.browsers.delete(browserId);
        return browserId;
      }
    }
    return undefined;
  }

  listBrowsers(): BrowserConnection[] {
    return Array.from(this.browsers.values()).map(({ ws: _, ...rest }) => rest);
  }
}

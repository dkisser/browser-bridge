import type { BrowserConnection, BrowserStatus } from '@browser-bridge/shared';
import type { ServerWebSocket } from 'bun';
import type { WsData } from './types';

/**
 * Tracks inbound extension WebSocket connections, indexed by browserId.
 *
 * Pre-merge history: this class lived on the WebSocket Server and tracked
 * the upstream `ServerWebSocket<WsData>` instances that local-proxy dialed
 * into ws-server. After the bridge-core merge the equivalent of that handle
 * is the inbound `ServerWebSocket<undefined>` that the extension itself
 * dials into bridge-core on port 3002. The class name and surface stay;
 * what the tracked handle *points at* is now a different WebSocket. See
 * `docs/adr/0010-bridge-core-merges-ws-server-and-local-proxy.md`.
 */
interface RegistryEntry {
  browserId: string;
  userId: string;
  ws: ServerWebSocket<WsData>;
  status: BrowserStatus;
  lastSeen: number;
}

export class ConnectionRegistry {
  private browsers = new Map<string, RegistryEntry>();

  async register(
    ws: ServerWebSocket<WsData>,
    browserId: string,
  ): Promise<{ success: boolean; error?: string }> {
    const existing = this.browsers.get(browserId);
    if (existing && existing.ws !== ws && existing.ws.readyState === 1) {
      return { success: false, error: 'browser_id_in_use' };
    }

    const userId = ws.data.userId ?? 'unknown';

    this.browsers.set(browserId, {
      browserId,
      userId,
      ws,
      status: 'offline',
      lastSeen: Date.now(),
    });

    ws.data = { ...ws.data, browserId, userId };
    return { success: true };
  }

  setStatus(browserId: string, status: BrowserStatus): boolean {
    const entry = this.browsers.get(browserId);
    if (!entry) {
      // The pre-merge ws-server required an explicit `register` event before
      // any status could be set; the bridge-core merge dropped that
      // handshake — extensions connect directly to BrowserServer — so the
      // router now writes the entry lazily on the first status update.
      this.browsers.set(browserId, {
        browserId,
        userId: 'extension',
        // We do not have a reference to the extension WS here; the browser
        // connection lives in BrowserServer.extensionWs, not the registry.
        // Listing still works; getWebSocket() will return undefined.
        ws: null as unknown as ServerWebSocket<WsData>,
        status,
        lastSeen: Date.now(),
      });
      return true;
    }
    entry.status = status;
    entry.lastSeen = Date.now();
    return true;
  }

  getStatus(browserId: string): BrowserStatus | undefined {
    return this.browsers.get(browserId)?.status;
  }

  getWebSocket(browserId: string): ServerWebSocket<WsData> | undefined {
    return this.browsers.get(browserId)?.ws;
  }

  removeByWebSocket(ws: ServerWebSocket<WsData>): string | undefined {
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
import type { Envelope } from '@browser-bridge/shared/types';
import { createClient } from '@browser-bridge/websocket/client';

export class CloudClient {
  private client: ReturnType<typeof createClient> | null = null;
  private onCommand: ((envelope: Envelope) => void) | null = null;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private serverUrl: string;
  private apiToken: string;
  private browserId: string;
  private reconnectAttempts = 0;
  private manualDisconnect = false;

  get isManualDisconnect(): boolean {
    return this.manualDisconnect;
  }

  constructor(opts: {
    serverUrl: string;
    apiToken: string;
    browserId: string;
    onCommand: (envelope: Envelope) => void;
  }) {
    this.serverUrl = opts.serverUrl;
    this.apiToken = opts.apiToken;
    this.browserId = opts.browserId;
    this.onCommand = opts.onCommand;
  }

  connect(): Promise<void> {
    this.manualDisconnect = false;
    return new Promise((resolve, reject) => {
      this.client = createClient({
        url: this.serverUrl,
        headers: { Authorization: `Bearer ${this.apiToken}` },
        onMessage: (envelope) => this.handleMessage(envelope),
        onError: (error) => {
          console.error('[cloud] connection error:', error);
          reject(error);
        },
        onClose: () => {
          console.log('[cloud] disconnected');
          this.client = null;
          this.scheduleReconnect();
        },
      });

      const check = setInterval(() => {
        if (this.client && this.client.readyState === WebSocket.OPEN) {
          clearInterval(check);
          this.register();
          this.reconnectAttempts = 0;
          resolve();
        }
      }, 50);

      setTimeout(() => {
        clearInterval(check);
        reject(new Error('Connection timeout'));
      }, 10000);
    });
  }

  private handleMessage(envelope: Envelope): void {
    if (envelope.type === 'command') {
      this.onCommand?.(envelope);
    }
  }

  private register(): void {
    if (!this.client) return;
    this.client.send('event', {
      event: 'register',
      browserId: this.browserId,
    });
  }

  reportStatus(status: 'online' | 'offline'): void {
    if (!this.client || this.client.readyState !== WebSocket.OPEN) return;
    this.client.send('event', {
      event: status,
      browserId: this.browserId,
    });
  }

  sendResponse(envelope: Envelope): void {
    if (!this.client || this.client.readyState !== WebSocket.OPEN) return;
    this.client.send('response', envelope.payload, {
      id: envelope.id,
      browserId: this.browserId,
    });
  }

  private scheduleReconnect(): void {
    if (this.reconnectTimer) return;
    const delay = Math.min(1000 * 2 ** this.reconnectAttempts, 30000);
    this.reconnectAttempts++;
    console.log(
      `[cloud] reconnecting in ${delay}ms (attempt ${this.reconnectAttempts})`,
    );
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      this.connect().catch(() => {});
    }, delay);
  }

  close(): void {
    this.manualDisconnect = true;
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
    this.client?.close();
    this.client = null;
  }
}

import type { Envelope } from '@browser-bridge/shared/types';
import { createClient } from '@browser-bridge/websocket/client';

export class CloudClient {
  private client: ReturnType<typeof createClient> | null = null;
  private onCommand: ((envelope: Envelope) => void) | null = null;
  private onConnect: (() => void) | null = null;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private connectTimeout: ReturnType<typeof setTimeout> | null = null;
  private checkInterval: ReturnType<typeof setInterval> | null = null;
  private connectReject: ((reason: Error) => void) | null = null;
  private serverUrl: string;
  private apiToken: string;
  private browserId: string;
  private reconnectAttempts = 0;
  private manualDisconnect = false;

  get isConnected(): boolean {
    return this.client !== null && this.client.readyState === WebSocket.OPEN;
  }

  get isManualDisconnect(): boolean {
    return this.manualDisconnect;
  }

  get reconnectAttemptsForTest(): number {
    return this.reconnectAttempts;
  }

  constructor(opts: {
    serverUrl: string;
    apiToken: string;
    browserId: string;
    onCommand: (envelope: Envelope) => void;
    onConnect?: () => void;
  }) {
    this.serverUrl = opts.serverUrl;
    this.apiToken = opts.apiToken;
    this.browserId = opts.browserId;
    this.onCommand = opts.onCommand;
    this.onConnect = opts.onConnect ?? null;
  }

  connect(): Promise<void> {
    this.manualDisconnect = false;
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    return new Promise((resolve, reject) => {
      this.connectReject = reject;
      this.client = createClient({
        url: this.serverUrl,
        headers: { Authorization: `Bearer ${this.apiToken}` },
        onMessage: (envelope) => this.handleMessage(envelope),
        onError: (error) => {
          console.error('[cloud] connection error:', error);
          this.clearConnectWatchers();
          reject(error);
        },
        onClose: () => {
          console.log('[cloud] disconnected');
          this.client = null;
          if (!this.manualDisconnect) {
            this.scheduleReconnect();
          }
        },
      });

      this.checkInterval = setInterval(() => {
        if (this.client && this.client.readyState === WebSocket.OPEN) {
          this.clearConnectWatchers();
          this.register();
          this.onConnect?.();
          this.reconnectAttempts = 0;
          resolve();
        }
      }, 50);

      this.connectTimeout = setTimeout(() => {
        this.clearConnectWatchers();
        reject(new Error('Connection timeout'));
      }, 10000);
    });
  }

  private clearConnectWatchers(): void {
    if (this.checkInterval) {
      clearInterval(this.checkInterval);
      this.checkInterval = null;
    }
    if (this.connectTimeout) {
      clearTimeout(this.connectTimeout);
      this.connectTimeout = null;
    }
    this.connectReject = null;
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
    if (this.reconnectTimer || this.manualDisconnect) return;
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
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    const rejectInFlight = this.connectReject;
    this.clearConnectWatchers();
    rejectInFlight?.(new Error('Closed during connect'));
    this.client?.close();
    this.client = null;
  }
}

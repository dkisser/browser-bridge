import { WEBSOCKET_PORT } from '@browser-bridge/shared';
import type {
  CommandPayload,
  CommandType,
  Envelope,
} from '@browser-bridge/shared/types';
import { createClient } from '@browser-bridge/websocket/client';

export class ManagedClient implements Disposable {
  private client: ReturnType<typeof createClient>;
  private timers = new Set<ReturnType<typeof setTimeout>>();
  private intervals = new Set<ReturnType<typeof setInterval>>();

  constructor(url = `ws://localhost:${WEBSOCKET_PORT}`) {
    this.client = createClient({ url });
  }

  get readyState(): number {
    return this.client.readyState;
  }

  async waitForOpen(timeoutMs = 5000): Promise<void> {
    return new Promise<void>((resolve, reject) => {
      const check = setInterval(() => {
        if (this.client.readyState === WebSocket.OPEN) {
          this.clearTimers(check, timeout);
          resolve();
        }
      }, 50);
      this.intervals.add(check);

      const timeout = setTimeout(() => {
        this.clearTimers(check, timeout);
        reject(new Error('Connection timeout'));
      }, timeoutMs);
      this.timers.add(timeout);
    });
  }

  request(
    type: Envelope['type'],
    payload: unknown,
    opts?: { id?: string; browserId?: string; timeout?: number },
  ): Promise<Envelope> {
    return this.client.request(type, payload, opts);
  }

  sendCommand(
    browserId: string,
    payload: { command: CommandType; params?: Record<string, unknown> },
    opts?: { timeout?: number },
  ): Promise<Envelope> {
    return this.client.sendCommand(browserId, payload as CommandPayload, opts);
  }

  send(
    type: Envelope['type'],
    payload: unknown,
    opts?: { id?: string; browserId?: string },
  ): void {
    this.client.send(type, payload, opts);
  }

  [Symbol.dispose](): void {
    this.client.close();
    for (const timer of this.timers) {
      clearTimeout(timer);
    }
    for (const interval of this.intervals) {
      clearInterval(interval);
    }
    this.timers.clear();
    this.intervals.clear();
  }

  private clearTimers(
    interval: ReturnType<typeof setInterval>,
    timeout: ReturnType<typeof setTimeout>,
  ): void {
    clearInterval(interval);
    this.intervals.delete(interval);
    clearTimeout(timeout);
    this.timers.delete(timeout);
  }
}

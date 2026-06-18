import type { Envelope } from '@browser-bridge/shared/types';
import type { CloudClient } from './cloud-client';
import type { LocalServer } from './local-server';
import type { StateManager } from './state';

export class Router {
  constructor(
    private state: StateManager,
    private cloud: CloudClient,
    private local: LocalServer,
  ) {}

  handleCloudCommand(envelope: Envelope): void {
    if (!this.state.canAcceptCommand()) {
      this.cloud.sendResponse({
        ...envelope,
        type: 'response',
        payload: {
          status: 'error',
          error: 'browser_offline',
          message: 'Browser is offline',
        },
      } as Envelope);
      return;
    }

    if (this.local.hasExtension) {
      this.local.sendToExtension(JSON.stringify(envelope));
    } else {
      const buffered = this.state.bufferCommand(
        JSON.stringify(envelope),
        () => {
          this.cloud.sendResponse({
            ...envelope,
            type: 'response',
            payload: {
              status: 'error',
              error: 'sw_timeout',
              message: 'Service worker did not wake up',
            },
          } as Envelope);
        },
      );

      if (!buffered) {
        this.cloud.sendResponse({
          ...envelope,
          type: 'response',
          payload: {
            status: 'error',
            error: 'cannot_buffer',
            message: 'Cannot buffer command',
          },
        } as Envelope);
      }
    }
  }

  handleExtensionResponse(envelope: Envelope): void {
    this.cloud.sendResponse(envelope);
  }

  handleExtensionConnect(): void {
    const buffered = this.state.getBufferedCommand();
    this.state.status = 'online';
    this.cloud.reportStatus('online');

    if (buffered) {
      this.local.sendToExtension(buffered);
    }
  }

  handleExtensionDisconnect(): void {
    this.state.status = 'idle_wait';
    this.cloud.reportStatus('offline');
  }
}

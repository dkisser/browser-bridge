import type { Envelope } from '@browser-bridge/shared/types';
import type { ServerWebSocket } from 'bun';
import type { BrowserServer } from './browser-server';
import type { ConnectionRegistry } from './server/registry';
import { encode } from './protocol';
import type { StateManager } from './state';

/**
 * Router coordinates the in-process flow between the InboundServer (CLI/MCP)
 * and the BrowserServer (extension). Pre-merge the equivalent work lived
 * split between the WS Server and Local Proxy with the CloudClient as the
 * bridge between them; after the merge all hops collapse into this object.
 *
 * In particular:
 * - `handleInboundCommand` was: WS Server case 'command' -> text frame to
 *   local-proxy WS. Now: function call into BrowserServer.sendToExtension.
 * - `handleBrowserResponse` was: Local Proxy Router.handleExtensionResponse
 *   -> cloud.sendResponse -> WS Server case 'response' broadcast. Now:
 *   function call back to the originating InboundServer WS, looked up by
 *   the command's envelope id.
 */
export class Router {
  /**
   * Tracks which inbound WS submitted which envelope id, so that a response
   * coming back from the extension can be routed to the exact caller instead
   * of being broadcast to all open CLI connections (which is what the old
   * ws-server did — kept as a fallback for tests / mismatched ids).
   */
  private readonly inboundById = new Map<
    string,
    ServerWebSocket<unknown>
  >();

  constructor(
    private readonly state: StateManager,
    private readonly browser: BrowserServer,
    private readonly registry: ConnectionRegistry,
  ) {}

  get browserId(): string {
    return this.state.browserId;
  }

  /**
   * Inbound WS (CLI / MCP) sent a command envelope. Look up the matching
   * extension WS through the registry and forward; if the extension is not
   * connected yet (state == idle_wait), buffer the command for 5 seconds so
   * a fast reconnect picks it up instead of bouncing immediately.
   */
  handleInboundCommand(
    envelope: Envelope,
    inboundWs: ServerWebSocket<unknown>,
  ): void {
    this.inboundById.set(envelope.id, inboundWs);

    if (!this.state.canAcceptCommand()) {
      inboundWs.send(
        encode(
          'response',
          {
            status: 'error',
            error: 'browser_offline',
            message: 'Browser is offline',
          },
          { id: envelope.id, browserId: envelope.browserId },
        ),
      );
      this.inboundById.delete(envelope.id);
      return;
    }

    if (this.browser.hasExtension()) {
      const text = encode(envelope.type, envelope.payload, {
        id: envelope.id,
        browserId: envelope.browserId,
      });
      this.browser.sendToExtension(text);
      return;
    }

    // Buffer once; reject re-buffers within the budget.
    const buffered = this.state.bufferCommand(
      encode(envelope.type, envelope.payload, {
        id: envelope.id,
        browserId: envelope.browserId,
      }),
      () => {
        const target = this.inboundById.get(envelope.id);
        if (target) {
          target.send(
            encode(
              'response',
              {
                status: 'error',
                error: 'sw_timeout',
                message: 'Service worker did not wake up',
              },
              { id: envelope.id, browserId: envelope.browserId },
            ),
          );
          this.inboundById.delete(envelope.id);
        }
      },
    );

    if (!buffered) {
      inboundWs.send(
        encode(
          'response',
          {
            status: 'error',
            error: 'cannot_buffer',
            message: 'Cannot buffer command',
          },
          { id: envelope.id, browserId: envelope.browserId },
        ),
      );
      this.inboundById.delete(envelope.id);
    }
  }

  /**
   * Extension sent a response envelope. Route it back to the originating
   * inbound WS by envelope id (the command's id field).
   */
  handleBrowserResponse(envelope: Envelope): void {
    const target = this.inboundById.get(envelope.id);
    if (target) {
      this.inboundById.delete(envelope.id);
      target.send(
        encode(envelope.type, envelope.payload, {
          id: envelope.id,
          browserId: envelope.browserId,
        }),
      );
    }
  }

  /**
   * Extension sent an event envelope: register, online, offline. Update
   * the registry state accordingly.
   */
  handleBrowserEvent(envelope: Envelope): void {
    const event = envelope.payload as Record<string, unknown>;
    const browserId = (event.browserId as string) || this.state.browserId;
    if (!browserId) return;

    switch (event.event) {
      case 'register':
        // The extension identifies itself for routing.
        this.registry.setStatus(browserId, 'online');
        break;
      case 'online':
        this.registry.setStatus(browserId, 'online');
        break;
      case 'offline':
        this.registry.setStatus(browserId, 'offline');
        break;
      default:
        break;
    }
  }

  /** Extension WS upgrade succeeded — extension is up. */
  handleBrowserConnect(): void {
    this.state.status = 'online';
    this.registry.setStatus(this.state.browserId, 'online');

    // Drain the buffered command (if any) immediately.
    const buffered = this.state.getBufferedCommand();
    if (buffered) {
      this.browser.sendToExtension(buffered);
    }
  }

  /** Extension WS closed — go back to idle_wait so the next command buffers. */
  handleBrowserDisconnect(): void {
    this.state.status = 'idle_wait';
    this.registry.setStatus(this.state.browserId, 'offline');
  }
}
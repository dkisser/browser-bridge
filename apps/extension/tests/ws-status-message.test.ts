import { describe, expect, it } from 'bun:test';
import {
  readWsStatusChange,
  WS_STATUS_CHANGED,
} from '../src/ui/ws-status-message';

// The side panel receives every message the service worker broadcasts, so the
// narrowing has to reject everything that is not a socket-state change. The
// regression this guards: an unrelated broadcast flipping the connection
// indicator to Connected while no browser is actually attached.
describe('readWsStatusChange', () => {
  it('reads both directions', () => {
    expect(
      readWsStatusChange({ type: WS_STATUS_CHANGED, connected: true }),
    ).toBe(true);
    expect(
      readWsStatusChange({ type: WS_STATUS_CHANGED, connected: false }),
    ).toBe(false);
  });

  it('rejects the offscreen-to-worker message the panel never sees', () => {
    // `ws_status` is what the offscreen document sends the *worker*. If the
    // panel ever matched it too, and the worker echoed rather than renamed,
    // the indicator would depend on which context answered first.
    expect(
      readWsStatusChange({ type: 'ws_status', connected: true }),
    ).toBeNull();
  });

  it('ignores other runtime traffic', () => {
    for (const request of [
      { type: 'ws_command', envelope: { id: 'a' } },
      { type: 'connect' },
      { type: 'policy_op', op: 'set_pairing_token' },
      {},
    ]) {
      expect(readWsStatusChange(request)).toBeNull();
    }
  });

  it('refuses a payload that does not carry a real boolean', () => {
    // A truthy cast here would render "Connected" for any non-empty string.
    for (const connected of ['true', 1, 0, null, undefined, {}, []]) {
      expect(
        readWsStatusChange({ type: WS_STATUS_CHANGED, connected }),
      ).toBeNull();
    }
  });

  it('survives non-object payloads', () => {
    for (const request of [null, undefined, 'ws_status_changed', 42, true]) {
      expect(readWsStatusChange(request)).toBeNull();
    }
  });
});

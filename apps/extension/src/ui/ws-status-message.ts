// The side panel is a runtime.onMessage listener like every other extension
// context, so it receives every message the service worker broadcasts — tab
// traffic included. Narrowing is separated from the hook so the part that can
// regress (which payloads are treated as a connection change) is unit-testable
// without a DOM.

export const WS_STATUS_CHANGED = 'ws_status_changed';

/**
 * Read a socket-state change off an arbitrary runtime message.
 *
 * Returns the new connection state, or null when the message is not one of
 * ours or does not carry a real boolean. The extension tsconfig declares
 * `types: ["chrome"]` only, so `request` arrives as `unknown` — an unvalidated
 * cast would let an unrelated broadcast flip the connection indicator.
 */
export function readWsStatusChange(request: unknown): boolean | null {
  if (typeof request !== 'object' || request === null) return null;
  const message = request as { type?: unknown; connected?: unknown };
  if (message.type !== WS_STATUS_CHANGED) return null;
  if (typeof message.connected !== 'boolean') return null;
  return message.connected;
}

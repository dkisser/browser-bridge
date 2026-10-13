import { useEffect, useState } from 'react';
import { fetchStatus, queryBrowserConnection } from '../bridge-api';
import { toErrorMessage } from '../format';
import { readWsStatusChange } from '../ws-status-message';

const POLL_INTERVAL_MS = 5000;

export interface BridgeStatus {
  browserConnected: boolean;
  browserId: string | null;
}

// Polls the bridge control plane and the extension service worker for the
// connection row. Polls every 5s while the document is visible and catches
// up immediately on visibility restore — the side panel document persists
// across tab switches, so polling pauses instead of tearing down.
//
// The poll is the floor, not the mechanism: the service worker also relays
// every offscreen socket transition (see background.ts `ws_status`), so the
// row moves the moment the socket settles rather than up to 5s later.
export function useBridgeStatus(
  setMessage: (text: string, persistent?: boolean) => void,
  clearTransientMessage: () => void,
): BridgeStatus {
  const [browserConnected, setBrowserConnected] = useState(false);
  const [browserId, setBrowserId] = useState<string | null>(null);

  useEffect(() => {
    // Split out from refreshConnection so a pushed socket change can refresh
    // the bridge identity (an HTTP round trip the push payload cannot carry)
    // without also paying for the ping it would duplicate.
    const refreshIdentity = async (): Promise<void> => {
      const result = await fetchStatus();
      if (
        result.success &&
        result.data &&
        typeof result.data.browserId === 'string'
      ) {
        setBrowserId(result.data.browserId);
        // Transient connection diagnostics from a previous failed poll get
        // cleared on this successful poll; persistent messages (from
        // user-initiated actions) are kept.
        clearTransientMessage();
      } else {
        setMessage(result.error ?? 'Unknown error', false);
      }
    };

    const refreshConnection = async (): Promise<void> => {
      setBrowserConnected(await queryBrowserConnection());
      await refreshIdentity();
    };

    // Sequence the connect handshake before the first ping: the background's
    // ping handler reads the offscreen document's WebSocket connection
    // state, which only flips to 'connected' after the WS handshake settles
    // (tens of ms). Without the await, the first poll almost always reports
    // browserConnected=false and the hero flashes Disconnected on every
    // panel open, recovering on the 5 s tick or a visibility-change refresh.
    void chrome.runtime
      .sendMessage({ type: 'connect' })
      .catch(() => {})
      .then(() => {
        void refreshConnection().catch((error: unknown) =>
          setMessage(toErrorMessage(error)),
        );
      });

    const pollTimer = setInterval(() => {
      if (document.visibilityState !== 'visible') return;
      void refreshConnection().catch((error: unknown) =>
        setMessage(toErrorMessage(error)),
      );
    }, POLL_INTERVAL_MS);

    const handleVisibility = (): void => {
      if (document.visibilityState === 'visible') {
        // Catch up immediately after becoming visible again.
        void refreshConnection().catch((error: unknown) =>
          setMessage(toErrorMessage(error)),
        );
      }
    };
    document.addEventListener('visibilitychange', handleVisibility);

    // Applied straight from the payload — no ping round trip — so pairing
    // shows Connected as soon as the socket opens instead of on the next
    // poll. The identity refresh covers what the payload cannot carry.
    const handleWsStatus = (request: unknown): void => {
      const connected = readWsStatusChange(request);
      if (connected === null) return;
      setBrowserConnected(connected);
      void refreshIdentity().catch((error: unknown) =>
        setMessage(toErrorMessage(error)),
      );
    };
    chrome.runtime.onMessage.addListener(handleWsStatus);

    return () => {
      clearInterval(pollTimer);
      document.removeEventListener('visibilitychange', handleVisibility);
      chrome.runtime.onMessage.removeListener(handleWsStatus);
    };
  }, [setMessage, clearTransientMessage]);

  return { browserConnected, browserId };
}

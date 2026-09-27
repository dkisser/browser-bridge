import { useEffect, useState } from 'react';
import { fetchStatus, queryBrowserConnection } from '../bridge-api';
import { toErrorMessage } from '../format';

const POLL_INTERVAL_MS = 5000;

export interface BridgeStatus {
  browserConnected: boolean;
  browserId: string | null;
}

// Polls the bridge control plane and the extension service worker for the
// connection row. Polls every 5s while the document is visible and catches
// up immediately on visibility restore — the side panel document persists
// across tab switches, so polling pauses instead of tearing down.
export function useBridgeStatus(
  setMessage: (text: string, persistent?: boolean) => void,
  clearTransientMessage: () => void,
): BridgeStatus {
  const [browserConnected, setBrowserConnected] = useState(false);
  const [browserId, setBrowserId] = useState<string | null>(null);

  useEffect(() => {
    const refreshConnection = async (): Promise<void> => {
      setBrowserConnected(await queryBrowserConnection());

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

    void chrome.runtime.sendMessage({ type: 'connect' }).catch(() => {});
    void refreshConnection().catch((error: unknown) =>
      setMessage(toErrorMessage(error)),
    );

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

    return () => {
      clearInterval(pollTimer);
      document.removeEventListener('visibilitychange', handleVisibility);
    };
  }, [setMessage, clearTransientMessage]);

  return { browserConnected, browserId };
}

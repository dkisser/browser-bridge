import { useEffect, useState } from 'react';
import { getPolicyState, type PolicyState } from '../../policy-state';
import { toErrorMessage } from '../format';

export interface PolicyStateResult {
  state: PolicyState | null;
  error: string | null;
}

// Live view of the persisted policy state: initial read plus re-renders on
// chrome.storage changes, matching the old storage.onChanged listener. An
// initial-read failure is surfaced through `error` so the panel can fall
// back to its default tab instead of staying blank.
export function usePolicyState(
  onError: (message: string) => void,
): PolicyStateResult {
  const [state, setState] = useState<PolicyState | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    void getPolicyState()
      .then((initial) => {
        if (cancelled) return;
        setState(initial);
        // A later successful read clears any prior initial-read failure,
        // so the panel can re-apply its default-view selection instead of
        // being permanently pinned to the 'approvals' fallback tab.
        setError(null);
      })
      .catch((err: unknown) => {
        const message = toErrorMessage(err);
        if (cancelled) return;
        setError(message);
        onError(message);
      });

    const listener = (
      changes: Record<string, chrome.storage.StorageChange>,
      area: string,
    ): void => {
      if (area === 'local' && changes.policyState) {
        void getPolicyState()
          .then((next) => {
            if (cancelled) return;
            setState(next);
            setError(null);
          })
          .catch((err: unknown) => {
            if (cancelled) return;
            // Keep the previous state on a failed re-read; surface the
            // error via the standard channel and the onError callback.
            // Without this, a rejected re-read would leave stale state,
            // leave the prior error in place, and emit an unhandled
            // promise rejection.
            const message = toErrorMessage(err);
            setError(message);
            onError(message);
          });
      }
    };
    chrome.storage.onChanged.addListener(listener);
    return () => {
      cancelled = true;
      chrome.storage.onChanged.removeListener(listener);
    };
  }, [onError]);

  return { state, error };
}

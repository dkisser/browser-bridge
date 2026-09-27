import { useEffect, useState } from 'react';
import { getPolicyState, type PolicyState } from '../../policy-state';

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
        if (!cancelled) setState(initial);
      })
      .catch((err: unknown) => {
        const message = toErrorMessage(err);
        if (!cancelled) setError(message);
        onError(message);
      });

    const listener = (
      changes: Record<string, chrome.storage.StorageChange>,
      area: string,
    ): void => {
      if (area === 'local' && changes.policyState) {
        void getPolicyState().then((next) => {
          if (!cancelled) setState(next);
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

function toErrorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

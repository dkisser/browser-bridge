import type { Denial, Grant } from '@browser-bridge/shared';
import { denialKey } from '@browser-bridge/shared';
import { updateBadge, updatePolicyState } from '../policy-state';

const GRANT_TTL_MS = 5 * 60 * 1000;

export type DenialAction =
  | 'approve-session'
  | 'approve-always'
  | 'deny-origin'
  | 'allow-once'
  | 'dismiss';

// Button sets vary by denial reason — order and labels are user-visible
// contract (copy freeze), do not reorder or rename.
export function denialCardButtons(denial: Denial): [DenialAction, string][] {
  switch (denial.reason) {
    case 'origin_not_approved':
      return [
        ['approve-session', 'Session'],
        ['approve-always', 'Always'],
        ['deny-origin', 'Deny'],
      ];
    case 'approval_required':
    case 'action_out_of_scope':
      return [
        ['allow-once', 'Allow once'],
        ['dismiss', 'Dismiss'],
      ];
    default:
      return [['dismiss', 'Dismiss']];
  }
}

// Resolves the target denial by stable key (reason|origin|command) inside
// the serialized update — the array index from render time may no longer
// point at the same denial if a new one was prepended between render and
// click. Passing the key keeps the action bound to the card the user saw.
export async function handleDenialAction(
  action: DenialAction,
  targetKey: string,
): Promise<void> {
  if (action === 'dismiss') {
    await updatePolicyState((state) => ({
      recentDenials: state.recentDenials.filter(
        (d) => denialKey(d) !== targetKey,
      ),
    }));
    await updateBadge();
    return;
  }
  await updatePolicyState((state) => {
    const index = state.recentDenials.findIndex(
      (d) => denialKey(d) === targetKey,
    );
    if (index === -1) return null;
    const denial = state.recentDenials[index];
    const remaining = state.recentDenials.filter((_, i) => i !== index);
    // The two maps must stay mutually exclusive — OriginsPanel renders
    // them together and would otherwise show the same origin twice.
    const clearedOrigins =
      denial.origin !== undefined && state.origins[denial.origin] !== undefined
        ? Object.fromEntries(
            Object.entries(state.origins).filter(
              ([origin]) => origin !== denial.origin,
            ),
          )
        : state.origins;
    const clearedDeniedOrigins =
      denial.origin !== undefined &&
      state.deniedOrigins[denial.origin] !== undefined
        ? Object.fromEntries(
            Object.entries(state.deniedOrigins).filter(
              ([origin]) => origin !== denial.origin,
            ),
          )
        : state.deniedOrigins;
    switch (action) {
      case 'approve-session':
      case 'approve-always': {
        if (denial.origin === undefined) {
          // These actions require an origin. denialCardButtons never
          // surfaces them for origin-less denials, but a stale UI
          // (extension update, script injection) could still invoke
          // them. Surface the rejection so the caller can show an
          // error banner instead of silently leaving the card.
          throw new Error('approve requires a denial with an origin');
        }
        return {
          origins: {
            ...clearedOrigins,
            [denial.origin]:
              action === 'approve-session' ? 'session' : 'always',
          },
          deniedOrigins: clearedDeniedOrigins,
          recentDenials: remaining,
        };
      }
      case 'deny-origin': {
        if (denial.origin === undefined) {
          throw new Error('deny-origin requires a denial with an origin');
        }
        return {
          origins: clearedOrigins,
          deniedOrigins: {
            ...clearedDeniedOrigins,
            [denial.origin]: 'always',
          },
          recentDenials: remaining,
        };
      }
      case 'allow-once': {
        const grant: Grant = {
          capability: denial.capability ?? 'submit',
          ...(denial.origin !== undefined ? { origin: denial.origin } : {}),
          expiresAt: Date.now() + GRANT_TTL_MS,
          singleUse: true,
        };
        return { grants: [...state.grants, grant], recentDenials: remaining };
      }
      default:
        return null;
    }
  });
  await updateBadge();
}

export type OriginKind = 'origins' | 'deniedOrigins';

export async function removeOriginEntry(
  kind: OriginKind,
  origin: string,
): Promise<void> {
  await updatePolicyState((state) => {
    const map = kind === 'origins' ? state.origins : state.deniedOrigins;
    const next = Object.fromEntries(
      Object.entries(map).filter(([key]) => key !== origin),
    );
    return kind === 'origins' ? { origins: next } : { deniedOrigins: next };
  });
}

export async function addBlockEntry(entry: string): Promise<void> {
  await updatePolicyState((state) => {
    if (state.blockedOrigins.includes(entry)) return null;
    return { blockedOrigins: [...state.blockedOrigins, entry] };
  });
}

export async function removeBlockEntry(entry: string): Promise<void> {
  await updatePolicyState((state) => ({
    blockedOrigins: state.blockedOrigins.filter((item) => item !== entry),
  }));
}

export async function handleDownloadAction(
  action: 'resume' | 'cancel-download',
  id: number,
): Promise<void> {
  try {
    if (action === 'resume') {
      await chrome.downloads.resume(id);
    } else {
      await chrome.downloads.cancel(id);
    }
  } catch {
    // chrome.downloads.resume/cancel rejects when the download is gone
    // (already completed or canceled via the browser UI before us) — that
    // is the expected terminal state, so swallow silently and let the
    // policy-state cleanup below drop the pending entry from the list.
    // Per project coding rules (no console statements in production),
    // no diagnostic log is emitted here; if a future investigation needs
    // to surface this, do so via the panel's message line, not console.
  }
  await updatePolicyState((state) => ({
    pendingDownloads: state.pendingDownloads.filter((d) => d.id !== id),
  }));
  await updateBadge();
}

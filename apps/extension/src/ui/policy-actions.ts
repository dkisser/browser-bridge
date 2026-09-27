import type { Denial, Grant } from '@browser-bridge/shared';
import { removeDenial, updateBadge, updatePolicyState } from '../policy-state';

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

export async function handleDenialAction(
  action: DenialAction,
  index: number,
): Promise<void> {
  if (action === 'dismiss') {
    await removeDenial(index);
    await updateBadge();
    return;
  }
  await updatePolicyState((state) => {
    const denial = state.recentDenials[index];
    if (!denial) return null;
    const remaining = state.recentDenials.filter((_, i) => i !== index);
    switch (action) {
      case 'approve-session':
      case 'approve-always': {
        if (denial.origin === undefined) return null;
        return {
          origins: {
            ...state.origins,
            [denial.origin]:
              action === 'approve-session' ? 'session' : 'always',
          },
          recentDenials: remaining,
        };
      }
      case 'deny-origin': {
        if (denial.origin === undefined) return null;
        return {
          deniedOrigins: { ...state.deniedOrigins, [denial.origin]: 'always' },
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
    // The download may have finished or been cancelled already.
  }
  await updatePolicyState((state) => ({
    pendingDownloads: state.pendingDownloads.filter((d) => d.id !== id),
  }));
  await updateBadge();
}

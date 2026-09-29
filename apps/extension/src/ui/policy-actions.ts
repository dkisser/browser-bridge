import type { Denial } from '@browser-bridge/shared';
import type { DenialAction, OriginKind, PolicyOp } from '../policy-operations';
import { requestPolicyOp } from './policy-ops';

// User actions on the side panel. Each one names a policy operation for the
// service worker to apply inside its own serialization queue; none of them
// writes storage directly (see ui/policy-ops.ts for why), and none of them
// refreshes the badge — the service worker does that as part of the same
// operation, so the two can never disagree.

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

// targetKey is the stable denial key (reason|origin|command) the card was
// rendered with, so the action stays bound to the card the user saw even if a
// new denial was prepended between render and click.
export async function handleDenialAction(
  action: DenialAction,
  targetKey: string,
): Promise<void> {
  await requestPolicyOp({ op: 'denial_action', action, targetKey });
}

export async function removeOriginEntry(
  kind: OriginKind,
  origin: string,
): Promise<void> {
  await requestPolicyOp({ op: 'remove_origin', kind, origin });
}

export async function addBlockEntry(entry: string): Promise<void> {
  await requestPolicyOp({ op: 'add_block', entry });
}

export async function removeBlockEntry(entry: string): Promise<void> {
  await requestPolicyOp({ op: 'remove_block', entry });
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
  } catch (error) {
    if (
      error instanceof Error &&
      /not found|gone|no longer/i.test(error.message)
    ) {
      // chrome.downloads.resume/cancel rejects when the download is gone
      // (already completed or cancelled via the browser UI before us) —
      // that is the expected terminal state. Drop the pending entry
      // silently so the panel does not keep showing a download the user
      // can no longer act on from here.
      await requestPolicyOp({ op: 'remove_download', id });
      return;
    }
    // Non-terminal failure (host tab closed, lost extension context,
    // permission revoked, etc.) — the operation did not take effect, so
    // keep the entry so the user can retry from the panel and re-throw
    // so the caller's .catch surfaces the failure.
    throw error;
  }
  await requestPolicyOp({ op: 'remove_download', id });
}

// Re-exported so the panels keep importing the action vocabulary from the
// module they already use for the actions themselves.
export type { DenialAction, OriginKind };

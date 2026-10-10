import type { Denial, Grant, PermissionMode } from '@browser-bridge/shared';
import { denialKey } from '@browser-bridge/shared';
import type { PolicyState } from './policy-state';

// Pure policy-state mutations, owned by the service worker.
//
// These used to live in ui/policy-actions.ts as callbacks passed to
// updatePolicyState — and the UI ran them itself. That was the bug: the
// serialization queue in policy-state.ts is a module-level variable, so
// every JS context (service worker, side panel, settings page) has its own
// copy. Two contexts could therefore read the same policy state, compute
// their own patches, and write the whole object back, and the loser's write
// would silently restore fields the winner had already changed — including a
// singleUse grant the service worker had just consumed, turning a one-shot
// approval into a reusable one. The user's takeover toggle could vanish the
// same way.
//
// A callback cannot cross the runtime message boundary, so the fix is to
// invert the shape: the operations are plain data-in/data-out functions that
// the service worker runs inside its own queue, and the UI names the
// operation it wants instead of supplying the code to perform it. The
// service worker is now the only writer, and its queue is the only queue
// that matters.

const GRANT_TTL_MS = 5 * 60 * 1000;

export type DenialAction =
  | 'approve-session'
  | 'approve-always'
  | 'deny-origin'
  | 'allow-once'
  | 'dismiss';

export type OriginKind = 'origins' | 'deniedOrigins';

// The wire shape of a policy operation request from the UI to the service
// worker. Discriminated on `op` so the service worker's handler is exhaustive
// and a new operation cannot be added without handling it.
export type PolicyOp =
  | { op: 'denial_action'; action: DenialAction; targetKey: string }
  | { op: 'remove_origin'; kind: OriginKind; origin: string }
  | { op: 'add_block'; entry: string }
  | { op: 'remove_block'; entry: string }
  | { op: 'remove_download'; id: number }
  | { op: 'set_takeover'; desired: boolean }
  | { op: 'set_permission_mode'; mode: PermissionMode }
  | { op: 'set_pairing_token'; token: string };

// denyChanges resolves the target denial by stable key rather than array
// index: the index captured at render time may no longer point at the same
// denial if a new one was prepended between render and click. Keying keeps
// the action bound to the card the user actually saw.
function findDenial(
  state: PolicyState,
  targetKey: string,
): { index: number; denial: Denial; remaining: Denial[] } | null {
  const index = state.recentDenials.findIndex(
    (d) => denialKey(d) === targetKey,
  );
  if (index === -1) return null;
  return {
    index,
    denial: state.recentDenials[index],
    remaining: state.recentDenials.filter((_, i) => i !== index),
  };
}

// withoutOrigin drops one key from a map, leaving it untouched when the key
// is not there. Generic so it works for the origins map ('always' | 'session')
// and the deniedOrigins map (same shape) without widening either.
function withoutOrigin<T>(
  map: Record<string, T>,
  origin: string | undefined,
): Record<string, T> {
  if (origin === undefined || map[origin] === undefined) return map;
  return Object.fromEntries(
    Object.entries(map).filter(([key]) => key !== origin),
  );
}

/**
 * Applies a denial-card action. Returns null when the target denial is gone
 * (already resolved by another click) or the action is not applicable, so the
 * service worker leaves the state untouched.
 */
export function applyDenialAction(
  state: PolicyState,
  action: DenialAction,
  targetKey: string,
): Partial<PolicyState> | null {
  if (action === 'dismiss') {
    return {
      recentDenials: state.recentDenials.filter(
        (d) => denialKey(d) !== targetKey,
      ),
    };
  }

  const found = findDenial(state, targetKey);
  if (!found) return null;
  const { denial, remaining } = found;
  // The two maps must stay mutually exclusive — OriginsPanel renders them
  // together and would otherwise show the same origin twice.
  const clearedOrigins = withoutOrigin(state.origins, denial.origin);
  const clearedDeniedOrigins = withoutOrigin(
    state.deniedOrigins,
    denial.origin,
  );

  switch (action) {
    case 'approve-session':
    case 'approve-always': {
      if (denial.origin === undefined) {
        // The button set never offers these for origin-less denials, but a
        // stale UI could still invoke them. Refuse rather than write a grant
        // with no origin.
        throw new Error('approve requires a denial with an origin');
      }
      return {
        origins: {
          ...clearedOrigins,
          [denial.origin]: action === 'approve-session' ? 'session' : 'always',
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
}

export function applyRemoveOrigin(
  state: PolicyState,
  kind: OriginKind,
  origin: string,
): Partial<PolicyState> {
  const map = kind === 'origins' ? state.origins : state.deniedOrigins;
  const next = withoutOrigin(map, origin);
  return kind === 'origins' ? { origins: next } : { deniedOrigins: next };
}

export function applyAddBlock(
  state: PolicyState,
  entry: string,
): Partial<PolicyState> | null {
  if (state.blockedOrigins.includes(entry)) return null;
  return { blockedOrigins: [...state.blockedOrigins, entry] };
}

export function applyRemoveBlock(
  state: PolicyState,
  entry: string,
): Partial<PolicyState> {
  return {
    blockedOrigins: state.blockedOrigins.filter((item) => item !== entry),
  };
}

export function applyRemoveDownload(
  state: PolicyState,
  id: number,
): Partial<PolicyState> {
  return {
    pendingDownloads: state.pendingDownloads.filter((d) => d.id !== id),
  };
}

// Releasing Takeover also drops the denials the takeover itself produced.
//
// recordDenial no longer writes them, but state written before that still
// carries them, and there is no other path that would ever remove one. Left
// alone, a user who had takeover engaged once comes back to an Approvals
// panel full of refusals worded as though the human were still in control —
// the panel contradicting the switch they just turned off. Only a release
// clears them: while takeover is on they would be suppressed anyway, and
// dropping them on engage would hide a list the user has not asked about.
export function applySetTakeover(
  state: PolicyState,
  desired: boolean,
): Partial<PolicyState> {
  if (desired) {
    return { takeover: true };
  }
  return {
    takeover: false,
    recentDenials: state.recentDenials.filter(
      (d) => d.reason !== 'human_assist_active',
    ),
  };
}

// The Permission mode (ADR-0038). Human-only by construction: the operation
// exists on the side-panel → service-worker channel, which is gated on the
// sender being an extension page, and no MCP tool or command can name it.
//
// A no-op patch when the mode is already what is stored keeps the write off
// storage entirely, so clicking the selected option does not churn the
// policy state (and with it every chrome.storage.onChanged listener).
//
// The mode is re-checked here rather than trusted from the message body: the
// op arrives as `request.op as PolicyOp`, an unchecked assertion on data that
// crossed a process boundary, and isPermissionMode is the same guard the read
// path already applies. Without it a malformed value persists, and the read
// path then resolves it to the permissive upgrade default — an unknown mode
// silently widening what runs silent is exactly what policy.ts's own comment
// says an omitted field must never do.
export function applySetPermissionMode(
  state: PolicyState,
  mode: PermissionMode,
): Partial<PolicyState> | null {
  if (mode !== 'strict' && mode !== 'standard' && mode !== 'relaxed') {
    throw new Error(`permission mode is not recognized: ${String(mode)}`);
  }
  if (state.permissionMode === mode) return null;
  return { permissionMode: mode };
}

export function applySetPairingToken(
  _state: PolicyState,
  token: string,
): Partial<PolicyState> {
  return { pairingToken: token };
}

/**
 * The single reducer the service worker applies for every `policy_op`
 * message. Exhaustive over PolicyOp, so adding an operation without
 * implementing it is a compile error rather than a silently ignored click.
 */
export function applyPolicyOp(
  state: PolicyState,
  request: PolicyOp,
): Partial<PolicyState> | null {
  switch (request.op) {
    case 'denial_action':
      return applyDenialAction(state, request.action, request.targetKey);
    case 'remove_origin':
      return applyRemoveOrigin(state, request.kind, request.origin);
    case 'add_block':
      return applyAddBlock(state, request.entry);
    case 'remove_block':
      return applyRemoveBlock(state, request.entry);
    case 'remove_download':
      return applyRemoveDownload(state, request.id);
    case 'set_takeover':
      return applySetTakeover(state, request.desired);
    case 'set_permission_mode':
      return applySetPermissionMode(state, request.mode);
    case 'set_pairing_token':
      return applySetPairingToken(state, request.token);
  }
}

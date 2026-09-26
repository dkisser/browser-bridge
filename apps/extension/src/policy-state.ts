// Policy state: the extension's persisted policy data (ADR-0006..0009).
// Single chrome.storage.local key, deep-merged over defaults so older or
// partial stored objects still yield a complete PolicyState.

import type { Denial, Grant, PolicyDecision } from '@browser-bridge/shared';

export interface PendingDownload {
  id: number;
  filename: string;
  url: string;
}

export interface PolicyState {
  // Origins the human approved for the agent, and ones they denied.
  origins: Record<string, 'always' | 'session'>;
  deniedOrigins: Record<string, 'always' | 'session'>;
  // One-shot approvals granted from the side panel (singleUse, expiring).
  grants: Grant[];
  // Human assist: while true, the browser is under user control and all
  // commands are rejected.
  takeover: boolean;
  // Tabs created by the agent via tab:new — closable without approval.
  agentTabs: number[];
  // Pairing token for the local-proxy WebSocket upgrade.
  pairingToken: string | null;
  // Recent denials, surfaced as approval cards in the side panel.
  recentDenials: Denial[];
  // User blocklist entries on top of the built-in list.
  blockedOrigins: string[];
  // Downloads paused by the policy gate, awaiting human resume/cancel.
  pendingDownloads: PendingDownload[];
  // Whether `chrome.tabGroups` is callable right now (ADR-0014). Chrome
  // revokes the permission silently on extension update if the user
  // declines the new prompt — the API then rejects every call. Defaults to
  // true; flipped to false on the first rejection and restored on the
  // next success. updateBadge() reads this to render a '!' indicator so
  // the user has a visible signal that grouping is disabled, instead of
  // having to open the service-worker DevTools console.
  agentGroupAvailable: boolean;
}

const STORAGE_KEY = 'policyState';
const MAX_RECENT_DENIALS = 20;

const DEFAULT_STATE: PolicyState = {
  origins: {},
  deniedOrigins: {},
  grants: [],
  takeover: false,
  agentTabs: [],
  pairingToken: null,
  recentDenials: [],
  blockedOrigins: [],
  pendingDownloads: [],
  agentGroupAvailable: true,
};

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function mergeDeep<T>(defaults: T, stored: unknown): T {
  if (isPlainObject(defaults) && isPlainObject(stored)) {
    const merged: Record<string, unknown> = { ...defaults };
    for (const [key, value] of Object.entries(stored)) {
      merged[key] = key in defaults ? mergeDeep(defaults[key], value) : value;
    }
    return merged as T;
  }
  return (stored === undefined ? defaults : stored) as T;
}

export async function getPolicyState(): Promise<PolicyState> {
  const result = await chrome.storage.local.get(STORAGE_KEY);
  return mergeDeep(DEFAULT_STATE, result[STORAGE_KEY]);
}

// chrome.storage has no transactions, so read-modify-write cycles (grant
// consumption, denial recording, agent-tab bookkeeping) are serialized here.
// Without this, two commands processed concurrently could both pass the
// policy check against the same grant state and double-consume a singleUse
// grant, or overwrite each other's fields.
let writeQueue: Promise<unknown> = Promise.resolve();

function enqueue<T>(task: () => Promise<T>): Promise<T> {
  const run = writeQueue.then(task, task);
  writeQueue = run.catch(() => {
    // Keep the queue alive even when a task rejects.
  });
  return run;
}

export async function setPolicyState(
  patch: Partial<PolicyState>,
): Promise<void> {
  await updatePolicyState(() => patch);
}

// Read-modify-write with fresh state: `update` sees the latest stored state
// and its patch is merged and persisted in the same serialized step.
export async function updatePolicyState(
  update: (state: PolicyState) => Partial<PolicyState> | null,
): Promise<PolicyState> {
  return enqueue(async () => {
    const current = await getPolicyState();
    const patch = update(current);
    if (patch === null) return current;
    const next = { ...current, ...patch };
    await chrome.storage.local.set({ [STORAGE_KEY]: next });
    return next;
  });
}

// The policy decision and any grant consumption are evaluated against fresh
// state and persisted atomically with respect to other callers: the callback
// runs inside the serialized queue, so a grant found by `evaluate` is
// guaranteed to still be there when the consumption is written — a
// concurrent command sees the post-consumption state and is denied.
export async function decideWithState(
  evaluate: (state: PolicyState) => PolicyDecision,
): Promise<{ decision: PolicyDecision; state: PolicyState }> {
  return enqueue(async () => {
    const state = await getPolicyState();
    const decision = evaluate(state);
    if (decision.allow && decision.consume && decision.consume.length > 0) {
      const grants = state.grants.filter(
        (grant) => !decision.consume?.includes(grant),
      );
      const next = { ...state, grants };
      await chrome.storage.local.set({ [STORAGE_KEY]: next });
      return { decision, state: next };
    }
    return { decision, state };
  });
}

function denialKey(denial: Denial): string {
  return `${denial.reason}|${denial.origin ?? ''}|${denial.command}`;
}

export async function recordDenial(denial: Denial): Promise<void> {
  await updatePolicyState((state) => ({
    recentDenials: [
      denial,
      ...state.recentDenials.filter((d) => denialKey(d) !== denialKey(denial)),
    ].slice(0, MAX_RECENT_DENIALS),
  }));
}

export async function removeDenial(index: number): Promise<void> {
  await updatePolicyState((state) => ({
    recentDenials: state.recentDenials.filter((_, i) => i !== index),
  }));
}

// Session-scoped approvals expire with the browser session: drop every
// 'session'-valued origin entry plus all one-shot grants, agent tabs, and
// paused downloads.
export async function clearSessionScoped(): Promise<void> {
  await updatePolicyState((state) => {
    const keepAlways = (
      map: Record<string, 'always' | 'session'>,
    ): Record<string, 'always' | 'session'> =>
      Object.fromEntries(
        Object.entries(map).filter(([, scope]) => scope !== 'session'),
      );
    return {
      origins: keepAlways(state.origins),
      deniedOrigins: keepAlways(state.deniedOrigins),
      grants: [],
      agentTabs: [],
      pendingDownloads: [],
    };
  });
}

export async function updateBadge(): Promise<void> {
  const state = await getPolicyState();
  const count = state.recentDenials.length + state.pendingDownloads.length;
  if (!state.agentGroupAvailable) {
    // Permissions revoked (typically Chrome's post-update prompt the user
    // dismissed). Prefix the denial count with '!' and tint the badge red
    // so the disabled-feature signal survives on top of any denial counts;
    // setBadgeBackgroundColor is sticky, so we also have to restore the
    // default blue when grouping comes back.
    await chrome.action.setBadgeBackgroundColor({ color: '#d93025' });
    await chrome.action.setBadgeText({
      text: count > 0 ? `!${String(count)}` : '!',
    });
    await chrome.action.setTitle({
      title:
        'Browser Bridge — agent tab group permission denied (grouping disabled; see chrome://extensions)',
    });
  } else {
    await chrome.action.setBadgeBackgroundColor({ color: '#1a73e8' });
    await chrome.action.setBadgeText({
      text: count > 0 ? String(count) : '',
    });
    await chrome.action.setTitle({ title: 'Browser Bridge' });
  }
}

// Flip the agentGroupAvailable bit and refresh the badge. No-ops when the
// value is already in the desired state — avoids a write+badge churn on
// every chrome.tabGroups.query call when the permission is healthy.
export async function setAgentGroupAvailability(
  available: boolean,
  error?: unknown,
): Promise<void> {
  if (!available && error !== undefined) {
    console.error(
      'browser-bridge: chrome.tabGroups unavailable (agent grouping disabled)',
      error,
    );
  }
  const state = await getPolicyState();
  if (state.agentGroupAvailable === available) return;
  await updatePolicyState((fresh) => ({
    ...fresh,
    agentGroupAvailable: available,
  }));
  await updateBadge();
}

// Policy state: the extension's persisted policy data (ADR-0006..0009).
// Single chrome.storage.local key, deep-merged over defaults so older or
// partial stored objects still yield a complete PolicyState.

import type { Denial, Grant } from '@browser-bridge/shared';

export interface PendingDownload {
  id: number;
  filename: string;
  url: string;
}

export interface PolicyState {
  // Origins the human approved for the agent, and ones they denied.
  origins: Record<string, 'always' | 'session'>;
  deniedOrigins: Record<string, 'always' | 'session'>;
  // One-shot approvals granted from the popup (singleUse, expiring).
  grants: Grant[];
  // Human assist: while true, the browser is under user control and all
  // commands are rejected.
  takeover: boolean;
  // Tabs created by the agent via tab:new — closable without approval.
  agentTabs: number[];
  // Pairing token for the local-proxy WebSocket upgrade.
  pairingToken: string | null;
  // Recent denials, surfaced as approval cards in the popup.
  recentDenials: Denial[];
  // User blocklist entries on top of the built-in list.
  blockedOrigins: string[];
  // Downloads paused by the policy gate, awaiting human resume/cancel.
  pendingDownloads: PendingDownload[];
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

export async function setPolicyState(
  patch: Partial<PolicyState>,
): Promise<void> {
  const current = await getPolicyState();
  await chrome.storage.local.set({
    [STORAGE_KEY]: { ...current, ...patch },
  });
}

function denialKey(denial: Denial): string {
  return `${denial.reason}|${denial.origin ?? ''}|${denial.command}`;
}

export async function recordDenial(denial: Denial): Promise<void> {
  const state = await getPolicyState();
  const recentDenials = [
    denial,
    ...state.recentDenials.filter((d) => denialKey(d) !== denialKey(denial)),
  ].slice(0, MAX_RECENT_DENIALS);
  await setPolicyState({ recentDenials });
}

export async function removeDenial(index: number): Promise<void> {
  const state = await getPolicyState();
  await setPolicyState({
    recentDenials: state.recentDenials.filter((_, i) => i !== index),
  });
}

// Session-scoped approvals expire with the browser session: drop every
// 'session'-valued origin entry plus all one-shot grants, agent tabs, and
// paused downloads.
export async function clearSessionScoped(): Promise<void> {
  const state = await getPolicyState();
  const keepAlways = (
    map: Record<string, 'always' | 'session'>,
  ): Record<string, 'always' | 'session'> =>
    Object.fromEntries(
      Object.entries(map).filter(([, scope]) => scope !== 'session'),
    );
  await setPolicyState({
    origins: keepAlways(state.origins),
    deniedOrigins: keepAlways(state.deniedOrigins),
    grants: [],
    agentTabs: [],
    pendingDownloads: [],
  });
}

export async function updateBadge(): Promise<void> {
  const state = await getPolicyState();
  const count = state.recentDenials.length + state.pendingDownloads.length;
  await chrome.action.setBadgeText({ text: count > 0 ? String(count) : '' });
}

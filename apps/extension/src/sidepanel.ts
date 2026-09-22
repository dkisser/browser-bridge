import {
  type Denial,
  type Grant,
  humanDenialMessage,
  LOCAL_WS_PORT,
} from '@browser-bridge/shared';
import {
  getPolicyState,
  type PolicyState,
  removeDenial,
  setPolicyState,
  updateBadge,
  updatePolicyState,
} from './policy-state';
import { type SidePanelTab, selectDefaultView } from './side-panel-state';

const API_BASE = `http://localhost:${LOCAL_WS_PORT}`;
const POLL_INTERVAL_MS = 5000;
const GRANT_TTL_MS = 5 * 60 * 1000;

interface StatusResponse {
  success: boolean;
  data?: {
    connected: boolean;
    browserId: string;
    serverUrl: string;
    manualDisconnect: boolean;
    paired: boolean;
  };
  error?: string;
}

interface PairConfirmResponse {
  success: boolean;
  data?: { token: string };
  error?: string;
  attemptsRemaining?: number;
}

interface PongResponse {
  connected?: boolean;
}

function getElement<T extends HTMLElement>(id: string): T {
  const el = document.getElementById(id);
  if (!el) {
    throw new Error(`Missing required element: ${id}`);
  }
  return el as T;
}

const browserDot = getElement<HTMLSpanElement>('browserDot');
const browserStatusLabel = getElement<HTMLSpanElement>('browserStatus');
const cloudDot = getElement<HTMLSpanElement>('cloudDot');
const cloudStatusLabel = getElement<HTMLSpanElement>('cloudStatus');
const uidEl = getElement<HTMLDivElement>('uid');
const cloudSwitch = getElement<HTMLInputElement>('cloudSwitch');
const takeoverSwitch = getElement<HTMLInputElement>('takeoverSwitch');
const messageEl = getElement<HTMLDivElement>('message');
const settingsLink = getElement<HTMLAnchorElement>('settingsLink');
const approvalsBadge = getElement<HTMLSpanElement>('approvalsBadge');

const denialsList = getElement<HTMLDivElement>('denialsList');
const originsList = getElement<HTMLDivElement>('originsList');
const blockInput = getElement<HTMLInputElement>('blockInput');
const blockAdd = getElement<HTMLButtonElement>('blockAdd');
const blocklistDiv = getElement<HTMLDivElement>('blocklist');
const downloadsList = getElement<HTMLDivElement>('downloadsList');

const pairCode = getElement<HTMLInputElement>('pairCode');
const pairButton = getElement<HTMLButtonElement>('pairButton');
const pairError = getElement<HTMLDivElement>('pairError');
const rePairButton = getElement<HTMLButtonElement>('rePairButton');
const pairInputCard = getElement<HTMLDivElement>('pairInputCard');

const tabButtons = Array.from(
  document.querySelectorAll<HTMLButtonElement>('.tab[data-tab]'),
);
const panels: Record<SidePanelTab, HTMLElement> = {
  approvals: getElement<HTMLElement>('panel-approvals'),
  origins: getElement<HTMLElement>('panel-origins'),
  blocklist: getElement<HTMLElement>('panel-blocklist'),
  downloads: getElement<HTMLElement>('panel-downloads'),
};

function setMessage(text: string): void {
  messageEl.textContent = text;
}

function setConnectionDot(
  dot: HTMLSpanElement,
  label: HTMLSpanElement,
  connected: boolean,
): void {
  dot.className = `state-dot ${connected ? 'connected' : 'disconnected'}`;
  label.textContent = connected ? 'Connected' : 'Disconnected';
}

async function fetchStatus(): Promise<StatusResponse> {
  try {
    const response = await fetch(`${API_BASE}/api/status`);
    return (await response.json()) as StatusResponse;
  } catch {
    return { success: false, error: 'Local proxy unreachable' };
  }
}

async function setCloudConnection(connect: boolean): Promise<void> {
  const endpoint = connect ? '/api/connect' : '/api/disconnect';
  try {
    const response = await fetch(`${API_BASE}${endpoint}`, { method: 'POST' });
    const result = (await response.json()) as StatusResponse;
    if (!result.success) {
      setMessage(result.error ?? 'Request failed');
    }
  } catch {
    setMessage('Local proxy unreachable');
  }
}

async function queryBrowserConnection(): Promise<boolean> {
  try {
    const response = (await chrome.runtime.sendMessage({
      type: 'ping',
    })) as PongResponse | undefined;
    return response?.connected ?? false;
  } catch {
    return false;
  }
}

async function refreshConnection(): Promise<void> {
  const browserConnected = await queryBrowserConnection();
  setConnectionDot(browserDot, browserStatusLabel, browserConnected);

  const result = await fetchStatus();
  if (result.success && result.data) {
    cloudSwitch.checked = result.data.connected;
    setConnectionDot(cloudDot, cloudStatusLabel, result.data.connected);
    uidEl.textContent = result.data.browserId;
    uidEl.title = result.data.browserId;
    // Don't clear message here: a poll every 5 s would wipe transient
    // diagnostics from setCloudConnection before the user can read them.
    cloudSwitch.disabled = false;
  } else {
    // Failure path: the cloud dot is red, and the switch's checked state
    // must reflect reality, not the optimistic click that triggered this.
    cloudSwitch.checked = false;
    setConnectionDot(cloudDot, cloudStatusLabel, false);
    setMessage(result.error ?? 'Unknown error');
    cloudSwitch.disabled = true;
  }
}

cloudSwitch.addEventListener('change', () => {
  cloudSwitch.disabled = true;
  // Optimistically reflect the click in the dot, then sync checked-state on
  // failure inside refreshConnection (which sets checked=false).
  void setCloudConnection(cloudSwitch.checked).finally(refreshConnection);
});

takeoverSwitch.addEventListener('change', () => {
  void setPolicyState({ takeover: takeoverSwitch.checked }).catch(
    (error: unknown) => setMessage(toErrorMessage(error)),
  );
});

settingsLink.addEventListener('click', (event) => {
  event.preventDefault();
  void chrome.runtime
    .sendMessage({ type: 'open-options' })
    .then((response) => {
      const r = response as { status?: string; error?: string } | undefined;
      if (r?.status === 'error' && r.error !== undefined) {
        setMessage(r.error);
      }
    })
    .catch((error: unknown) => setMessage(toErrorMessage(error)));
});

// --- Tab strip ---

// Roving tabindex per the WAI-ARIA tabs pattern: only the active tab is in
// the tab order (tabindex=0); the rest are tabindex=-1 so keyboard users
// reach the active tab next, then arrow-key between tabs.
function activateTab(tab: SidePanelTab): void {
  for (const button of tabButtons) {
    const isActive = button.dataset.tab === tab;
    button.setAttribute('aria-selected', isActive ? 'true' : 'false');
    button.setAttribute('tabindex', isActive ? '0' : '-1');
  }
  for (const [key, panel] of Object.entries(panels)) {
    panel.classList.toggle('active', key === tab);
  }
}

for (const button of tabButtons) {
  const tab = button.dataset.tab as SidePanelTab | undefined;
  if (!tab) continue;
  button.addEventListener('click', () => activateTab(tab));
}

// Arrow-key navigation between tabs (WAI-ARIA tabs pattern).
const tabStrip = document.querySelector<HTMLElement>('.tab-strip');
if (tabStrip) {
  tabStrip.addEventListener('keydown', (event: KeyboardEvent) => {
    if (
      event.key !== 'ArrowLeft' &&
      event.key !== 'ArrowRight' &&
      event.key !== 'Home' &&
      event.key !== 'End'
    ) {
      return;
    }
    const currentIndex = tabButtons.findIndex(
      (button) => button.getAttribute('aria-selected') === 'true',
    );
    if (currentIndex < 0) return;
    let nextIndex = currentIndex;
    if (event.key === 'ArrowLeft')
      nextIndex = (currentIndex - 1 + tabButtons.length) % tabButtons.length;
    if (event.key === 'ArrowRight')
      nextIndex = (currentIndex + 1) % tabButtons.length;
    if (event.key === 'Home') nextIndex = 0;
    if (event.key === 'End') nextIndex = tabButtons.length - 1;
    const nextButton = tabButtons[nextIndex];
    const nextTab = nextButton.dataset.tab as SidePanelTab | undefined;
    if (!nextTab) return;
    event.preventDefault();
    activateTab(nextTab);
    nextButton.focus();
  });
}

// --- Pairing ---

async function confirmPairing(code: string): Promise<void> {
  pairError.textContent = '';
  try {
    const response = await fetch(`${API_BASE}/api/pair/confirm`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ code }),
    });
    const result = (await response.json()) as PairConfirmResponse;
    if (result.success && result.data?.token) {
      await setPolicyState({ pairingToken: result.data.token });
      void chrome.runtime.sendMessage({ type: 'connect' }).catch(() => {});
      pairCode.value = '';
    } else {
      const attempts =
        result.attemptsRemaining !== undefined
          ? ` (${result.attemptsRemaining} attempts remaining)`
          : '';
      pairError.textContent = `Pairing failed: ${result.error ?? 'unknown'}${attempts}`;
    }
  } catch {
    pairError.textContent = 'Local proxy unreachable';
  }
}

pairButton.addEventListener('click', () => {
  const code = pairCode.value.trim();
  if (code !== '') void confirmPairing(code);
});

pairCode.addEventListener('keydown', (event) => {
  if (event.key !== 'Enter') return;
  const code = pairCode.value.trim();
  if (code !== '') void confirmPairing(code);
});

rePairButton.addEventListener('click', () => {
  void setPolicyState({ pairingToken: null }).catch((error: unknown) =>
    setMessage(toErrorMessage(error)),
  );
});

// --- Policy panels ---

function appendEmpty(container: HTMLElement, text: string): void {
  const empty = document.createElement('div');
  empty.className = 'empty';
  empty.textContent = text;
  container.appendChild(empty);
}

function denialCardButtons(denial: Denial): string[][] {
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

function renderPairing(state: PolicyState): void {
  const paired = state.pairingToken !== null;
  // Once paired, only the Re-pair escape hatch can rotate the token. Hide
  // the always-visible Pair input/button so a stray code paste cannot
  // silently overwrite the live pairing token and tear down the socket.
  rePairButton.classList.toggle('hidden', paired);
  pairInputCard.classList.toggle('hidden', paired);
}

function renderTakeover(state: PolicyState): void {
  takeoverSwitch.checked = state.takeover;
}

function renderDenials(state: PolicyState): void {
  denialsList.replaceChildren();
  if (state.recentDenials.length === 0) {
    appendEmpty(denialsList, 'Nothing waiting for approval.');
  } else {
    state.recentDenials.forEach((denial, index) => {
      const card = document.createElement('div');
      card.className = 'card';
      const title = document.createElement('div');
      title.className = 'card-title';
      title.textContent =
        denial.origin !== undefined
          ? `${denial.command} — ${denial.origin}`
          : denial.command;
      const msg = document.createElement('div');
      msg.className = 'card-msg';
      msg.textContent = humanDenialMessage(denial);
      const actions = document.createElement('div');
      actions.className = 'card-actions';
      for (const [action, label] of denialCardButtons(denial)) {
        const button = document.createElement('button');
        button.type = 'button';
        button.textContent = label;
        button.setAttribute('data-action', action);
        button.setAttribute('data-index', String(index));
        actions.appendChild(button);
      }
      card.append(title, msg, actions);
      denialsList.appendChild(card);
    });
  }
  // Badge counts denials only: the Approvals panel renders denials, while
  // downloads have their own tab and their own visible list. Mixing the two
  // would draw attention to Approvals for items it does not show.
  const badgeValue = state.recentDenials.length;
  if (badgeValue > 0) {
    approvalsBadge.textContent = String(badgeValue);
    approvalsBadge.classList.remove('hidden');
  } else {
    approvalsBadge.classList.add('hidden');
  }
}

function renderOrigins(state: PolicyState): void {
  originsList.replaceChildren();
  const entries = [
    ...Object.entries(state.origins).map(([origin, scope]) => ({
      origin,
      scope,
      kind: 'origins' as const,
    })),
    ...Object.entries(state.deniedOrigins).map(([origin, scope]) => ({
      origin,
      scope,
      kind: 'deniedOrigins' as const,
    })),
  ];
  if (entries.length === 0) {
    appendEmpty(originsList, 'No origins approved or denied yet.');
    return;
  }
  for (const entry of entries) {
    const row = document.createElement('div');
    row.className = 'row';
    const labelDiv = document.createElement('div');
    labelDiv.className =
      `row-label ${entry.kind === 'deniedOrigins' ? 'denied' : ''}`.trim();
    labelDiv.textContent = `${entry.origin} (${entry.scope})`;
    labelDiv.title = `${entry.origin} (${entry.scope})`;
    const button = document.createElement('button');
    button.type = 'button';
    button.textContent = 'Remove';
    button.className = 'small';
    button.setAttribute('data-action', 'remove-origin');
    button.setAttribute('data-kind', entry.kind);
    button.setAttribute('data-origin', entry.origin);
    row.append(labelDiv, button);
    originsList.appendChild(row);
  }
}

function renderBlocklist(state: PolicyState): void {
  blocklistDiv.replaceChildren();
  if (state.blockedOrigins.length === 0) {
    appendEmpty(blocklistDiv, 'No custom entries.');
    return;
  }
  for (const entry of state.blockedOrigins) {
    const row = document.createElement('div');
    row.className = 'row';
    const labelDiv = document.createElement('div');
    labelDiv.className = 'row-label';
    labelDiv.textContent = entry;
    labelDiv.title = entry;
    const button = document.createElement('button');
    button.type = 'button';
    button.textContent = 'Remove';
    button.className = 'small';
    button.setAttribute('data-action', 'remove-block');
    button.setAttribute('data-entry', entry);
    row.append(labelDiv, button);
    blocklistDiv.appendChild(row);
  }
}

function renderDownloads(state: PolicyState): void {
  downloadsList.replaceChildren();
  if (state.pendingDownloads.length === 0) {
    appendEmpty(downloadsList, 'No paused downloads.');
    return;
  }
  for (const download of state.pendingDownloads) {
    const card = document.createElement('div');
    card.className = 'card';
    const title = document.createElement('div');
    title.className = 'card-title';
    title.textContent = download.filename || download.url;
    const actions = document.createElement('div');
    actions.className = 'card-actions';
    for (const [action, label] of [
      ['resume', 'Resume'],
      ['cancel-download', 'Cancel'],
    ] as const) {
      const button = document.createElement('button');
      button.type = 'button';
      button.textContent = label;
      button.className = 'small';
      button.setAttribute('data-action', action);
      button.setAttribute('data-id', String(download.id));
      actions.appendChild(button);
    }
    card.append(title, actions);
    downloadsList.appendChild(card);
  }
}

async function renderPolicy(): Promise<void> {
  const state = await getPolicyState();
  renderPairing(state);
  renderTakeover(state);
  renderDenials(state);
  renderOrigins(state);
  renderBlocklist(state);
  renderDownloads(state);
  // Activate the default view only on the very first paint — subsequent
  // storage changes (denials, downloads, agent-tab bookkeeping) re-render
  // the panel contents but must not yank the user off the tab they chose.
}

async function handleDenialAction(
  action: string,
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

denialsList.addEventListener('click', (event) => {
  const button = (event.target as HTMLElement).closest('button');
  if (!button) return;
  const index = Number(button.getAttribute('data-index'));
  const action = button.getAttribute('data-action') ?? '';
  void handleDenialAction(action, index).catch((error: unknown) =>
    setMessage(toErrorMessage(error)),
  );
});

originsList.addEventListener('click', (event) => {
  const button = (event.target as HTMLElement).closest('button');
  if (!button) return;
  const kind = button.getAttribute('data-kind');
  const origin = button.getAttribute('data-origin');
  if (origin === null || (kind !== 'origins' && kind !== 'deniedOrigins')) {
    return;
  }
  void (async () => {
    await updatePolicyState((state) => {
      const map = kind === 'origins' ? state.origins : state.deniedOrigins;
      const next = Object.fromEntries(
        Object.entries(map).filter(([key]) => key !== origin),
      );
      return kind === 'origins' ? { origins: next } : { deniedOrigins: next };
    });
  })().catch((error: unknown) => setMessage(toErrorMessage(error)));
});

async function addBlockEntry(): Promise<void> {
  const entry = blockInput.value.trim();
  if (entry === '') return;
  await updatePolicyState((state) => {
    if (state.blockedOrigins.includes(entry)) return null;
    return { blockedOrigins: [...state.blockedOrigins, entry] };
  });
  blockInput.value = '';
}

blockAdd.addEventListener('click', () => {
  void addBlockEntry().catch((error: unknown) =>
    setMessage(toErrorMessage(error)),
  );
});

blockInput.addEventListener('keydown', (event) => {
  if (event.key === 'Enter') void addBlockEntry();
});

blocklistDiv.addEventListener('click', (event) => {
  const button = (event.target as HTMLElement).closest('button');
  if (!button) return;
  const entry = button.getAttribute('data-entry');
  if (entry === null) return;
  void (async () => {
    await updatePolicyState((state) => ({
      blockedOrigins: state.blockedOrigins.filter((item) => item !== entry),
    }));
  })().catch((error: unknown) => setMessage(toErrorMessage(error)));
});

downloadsList.addEventListener('click', (event) => {
  const button = (event.target as HTMLElement).closest('button');
  if (!button) return;
  const id = Number(button.getAttribute('data-id'));
  // Number.isFinite(0) is true, so guard with id > 0 explicitly — chrome
  // download ids are positive integers, and a missing attribute yields 0.
  if (!Number.isFinite(id) || id <= 0) return;
  const action = button.getAttribute('data-action');
  void (async () => {
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
  })().catch((error: unknown) => setMessage(toErrorMessage(error)));
});

// --- Wiring ---

chrome.storage.onChanged.addListener((changes, area) => {
  if (area === 'local' && changes.policyState) {
    void renderPolicy().catch((error: unknown) =>
      setMessage(toErrorMessage(error)),
    );
  }
});

void chrome.runtime.sendMessage({ type: 'connect' }).catch(() => {});
void refreshConnection().catch((error: unknown) =>
  setMessage(toErrorMessage(error)),
);
// Initial paint: render panel contents, then set the active tab to the
// default view. After this, storage-change listeners re-render contents
// only — the user's manual tab selection sticks.
void (async (): Promise<void> => {
  await renderPolicy();
  const state = await getPolicyState();
  activateTab(selectDefaultView(state));
})().catch((error: unknown) => setMessage(toErrorMessage(error)));
void updateBadge().catch((error: unknown) => setMessage(toErrorMessage(error)));

const poll = setInterval(() => {
  void refreshConnection().catch((error: unknown) =>
    setMessage(toErrorMessage(error)),
  );
}, POLL_INTERVAL_MS);

window.addEventListener('unload', () => {
  clearInterval(poll);
});

// Helpers

function toErrorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

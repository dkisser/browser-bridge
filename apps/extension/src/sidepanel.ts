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
    browserId: string;
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
const uidEl = getElement<HTMLDivElement>('uid');
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

let messagePersistent = false;

function setMessage(text: string, persistent = true): void {
  messageEl.textContent = text;
  messagePersistent = persistent;
}

function clearTransientMessage(): void {
  if (messagePersistent) return;
  messageEl.textContent = '';
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
    return { success: false, error: 'Control plane unreachable' };
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
  if (
    result.success &&
    result.data &&
    typeof result.data.browserId === 'string'
  ) {
    uidEl.textContent = result.data.browserId;
    uidEl.title = result.data.browserId;
    // Transient connection diagnostics from a previous failed poll get
    // cleared on this successful poll; persistent messages (from
    // user-initiated actions) are kept.
    clearTransientMessage();
  } else {
    setMessage(result.error ?? 'Unknown error', false);
  }
}

takeoverSwitch.addEventListener('change', () => {
  const desired = takeoverSwitch.checked;
  void setPolicyState({ takeover: desired }).catch((error: unknown) => {
    // Storage write failed; revert the switch so the UI matches persisted
    // state and the user is not misled about whether takeover is active.
    takeoverSwitch.checked = !desired;
    setMessage(toErrorMessage(error));
  });
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

function isSidePanelTab(value: string | undefined): value is SidePanelTab {
  return (
    value === 'approvals' ||
    value === 'origins' ||
    value === 'blocklist' ||
    value === 'downloads'
  );
}

for (const button of tabButtons) {
  const tab = button.dataset.tab;
  if (!isSidePanelTab(tab)) continue;
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
    const nextTab = nextButton.dataset.tab;
    if (!isSidePanelTab(nextTab)) return;
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
    pairError.textContent = 'Control plane unreachable';
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
  // Pairing input and Re-pair button are mutually exclusive: only one is
  // visible at any time. Both initial HTML and the JS update use explicit
  // inline display values (block / inline-block / none) so the visibility
  // does not depend on CSS cascade or empty-string fallback behaviour.
  const paired = state.pairingToken !== null;
  pairInputCard.style.display = paired ? 'none' : 'block';
  rePairButton.style.display = paired ? 'inline-block' : 'none';
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

async function renderPolicy(): Promise<PolicyState> {
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
  return state;
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
  // Number(null) === 0 and Number.isFinite(0) is true, so guard with >= 0.
  // Mirrors the downloads handler at line ~618.
  const index = Number(button.getAttribute('data-index'));
  const action = button.getAttribute('data-action') ?? '';
  if (!Number.isFinite(index) || index < 0) return;
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
  if (event.key === 'Enter') {
    void addBlockEntry().catch((error: unknown) =>
      setMessage(toErrorMessage(error)),
    );
  }
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
  const state = await renderPolicy();
  activateTab(selectDefaultView(state));
})().catch((error: unknown) => {
  // If the very first storage read fails, fall back to the HTML default
  // (Approvals tab) so the panel is never blank. Only surface the error if
  // the panel can still render — otherwise the message is invisible anyway.
  activateTab('approvals');
  setMessage(toErrorMessage(error));
});
void updateBadge().catch((error: unknown) => setMessage(toErrorMessage(error)));

// Side panels persist across tab switches and browser restarts of the host
// tab, so the document is rarely torn down — only unloaded when the host
// tab closes. Pause polling while the document is hidden so we do not
// burn /api/status and chrome.runtime.sendMessage round-trips indefinitely.
let pollTimer: ReturnType<typeof setInterval> | null = null;
function startPoll(): void {
  if (pollTimer !== null) return;
  pollTimer = setInterval(() => {
    if (document.visibilityState !== 'visible') return;
    void refreshConnection().catch((error: unknown) =>
      setMessage(toErrorMessage(error)),
    );
  }, POLL_INTERVAL_MS);
}
function stopPoll(): void {
  if (pollTimer === null) return;
  clearInterval(pollTimer);
  pollTimer = null;
}
startPoll();
document.addEventListener('visibilitychange', () => {
  if (document.visibilityState === 'visible') {
    // Catch up immediately after becoming visible again.
    void refreshConnection().catch((error: unknown) =>
      setMessage(toErrorMessage(error)),
    );
  }
});

window.addEventListener('unload', () => {
  stopPoll();
});

// Helpers

function toErrorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

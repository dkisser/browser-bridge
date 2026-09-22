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

const API_BASE = `http://localhost:${LOCAL_WS_PORT}`;
const POLL_INTERVAL_MS = 5000;
const GRANT_TTL_MS = 5 * 60 * 1000;

const browserDot = document.getElementById('browserDot') as HTMLDivElement;
const browserStatusLabel = document.getElementById(
  'browserStatus',
) as HTMLSpanElement;
const cloudDot = document.getElementById('cloudDot') as HTMLDivElement;
const cloudStatusLabel = document.getElementById(
  'cloudStatus',
) as HTMLSpanElement;
const browserIdDiv = document.getElementById('browserId') as HTMLDivElement;
const cloudSwitch = document.getElementById('cloudSwitch') as HTMLInputElement;
const messageDiv = document.getElementById('message') as HTMLDivElement;
const pairingPanel = document.getElementById('pairingPanel') as HTMLElement;
const pairCode = document.getElementById('pairCode') as HTMLInputElement;
const pairButton = document.getElementById('pairButton') as HTMLButtonElement;
const pairError = document.getElementById('pairError') as HTMLDivElement;
const rePairButton = document.getElementById(
  'rePairButton',
) as HTMLButtonElement;
const takeoverToggle = document.getElementById(
  'takeoverToggle',
) as HTMLInputElement;
const denialsList = document.getElementById('denialsList') as HTMLDivElement;
const originsList = document.getElementById('originsList') as HTMLDivElement;
const blockInput = document.getElementById('blockInput') as HTMLInputElement;
const blockAdd = document.getElementById('blockAdd') as HTMLButtonElement;
const blocklistDiv = document.getElementById('blocklist') as HTMLDivElement;
const downloadsList = document.getElementById(
  'downloadsList',
) as HTMLDivElement;

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

function updateBrowserStatus(connected: boolean): void {
  browserDot.className = `dot ${connected ? 'connected' : 'disconnected'}`;
  browserStatusLabel.textContent = connected ? 'Connected' : 'Disconnected';
}

function updateCloudStatus(connected: boolean): void {
  cloudDot.className = `dot ${connected ? 'connected' : 'disconnected'}`;
  cloudStatusLabel.textContent = connected ? 'Connected' : 'Disconnected';
}

function setMessage(text: string): void {
  messageDiv.textContent = text;
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

interface PongResponse {
  connected?: boolean;
}

// Real extension↔proxy link state, tracked by the service worker.
// queryOffscreenStatus() also recreates the offscreen document when it
// died, so merely asking for status revives the connection.
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

async function refresh(): Promise<void> {
  updateBrowserStatus(await queryBrowserConnection());

  const result = await fetchStatus();
  if (result.success && result.data) {
    cloudSwitch.checked = result.data.connected;
    updateCloudStatus(result.data.connected);
    browserIdDiv.textContent = result.data.browserId;
    setMessage('');
    cloudSwitch.disabled = false;
  } else {
    updateCloudStatus(false);
    setMessage(result.error ?? 'Unknown error');
    cloudSwitch.disabled = true;
  }
}

cloudSwitch.addEventListener('change', async () => {
  cloudSwitch.disabled = true;
  await setCloudConnection(cloudSwitch.checked);
  await refresh();
});

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
      // Route through the service worker: it recreates the offscreen
      // document when it died and then asks it to connect with the new
      // token (the storage change listener reconnects it too).
      chrome.runtime.sendMessage({ type: 'connect' }).catch(() => {});
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

// Escape hatch for a stale or rotated token: clearing it tears the socket
// down (the offscreen storage listener) and brings the pairing panel back.
rePairButton.addEventListener('click', () => {
  void setPolicyState({ pairingToken: null });
});

// --- Policy panels ---

function appendEmpty(container: HTMLElement, text: string): void {
  const empty = document.createElement('div');
  empty.className = 'empty';
  empty.textContent = text;
  container.appendChild(empty);
}

function appendRow(
  container: HTMLElement,
  label: string,
  className: string,
  action: string,
  extraAttrs: Record<string, string>,
  buttonLabel: string,
): void {
  const row = document.createElement('div');
  row.className = 'row';
  const labelDiv = document.createElement('div');
  labelDiv.className = `row-label ${className}`.trim();
  labelDiv.textContent = label;
  labelDiv.title = label;
  const button = document.createElement('button');
  button.type = 'button';
  button.textContent = buttonLabel;
  button.setAttribute('data-action', action);
  for (const [name, value] of Object.entries(extraAttrs)) {
    button.setAttribute(name, value);
  }
  row.append(labelDiv, button);
  container.appendChild(row);
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
      // origin_blocked, origin_denied, human_assist_active — read-only.
      return [['dismiss', 'Dismiss']];
  }
}

function renderPairing(state: PolicyState): void {
  pairingPanel.classList.toggle('hidden', state.pairingToken !== null);
  rePairButton.classList.toggle('hidden', state.pairingToken === null);
}

function renderTakeover(state: PolicyState): void {
  takeoverToggle.checked = state.takeover;
}

function renderDenials(state: PolicyState): void {
  denialsList.textContent = '';
  if (state.recentDenials.length === 0) {
    appendEmpty(denialsList, 'Nothing waiting for approval.');
    return;
  }
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

function renderOrigins(state: PolicyState): void {
  originsList.textContent = '';
  const entries: { origin: string; scope: string; kind: string }[] = [
    ...Object.entries(state.origins).map(([origin, scope]) => ({
      origin,
      scope,
      kind: 'origins',
    })),
    ...Object.entries(state.deniedOrigins).map(([origin, scope]) => ({
      origin,
      scope,
      kind: 'deniedOrigins',
    })),
  ];
  if (entries.length === 0) {
    appendEmpty(originsList, 'No origins approved or denied yet.');
    return;
  }
  for (const entry of entries) {
    appendRow(
      originsList,
      `${entry.origin} (${entry.scope})`,
      entry.kind === 'deniedOrigins' ? 'denied' : '',
      'remove-origin',
      { 'data-kind': entry.kind, 'data-origin': entry.origin },
      'Remove',
    );
  }
}

function renderBlocklist(state: PolicyState): void {
  blocklistDiv.textContent = '';
  if (state.blockedOrigins.length === 0) {
    appendEmpty(blocklistDiv, 'No custom entries.');
    return;
  }
  for (const entry of state.blockedOrigins) {
    appendRow(
      blocklistDiv,
      entry,
      '',
      'remove-block',
      { 'data-entry': entry },
      'Remove',
    );
  }
}

function renderDownloads(state: PolicyState): void {
  downloadsList.textContent = '';
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
  // Fresh state inside the serialized queue: a concurrent grant consumption
  // cannot be overwritten by a stale read-modify-write here.
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
  void handleDenialAction(
    button.getAttribute('data-action') ?? '',
    index,
  ).catch(console.error);
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
  })().catch(console.error);
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
  void addBlockEntry().catch(console.error);
});

blockInput.addEventListener('keydown', (event) => {
  if (event.key === 'Enter') void addBlockEntry().catch(console.error);
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
  })().catch(console.error);
});

downloadsList.addEventListener('click', (event) => {
  const button = (event.target as HTMLElement).closest('button');
  if (!button) return;
  const id = Number(button.getAttribute('data-id'));
  if (!Number.isFinite(id)) return;
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
  })().catch(console.error);
});

takeoverToggle.addEventListener('change', () => {
  void setPolicyState({ takeover: takeoverToggle.checked }).catch(
    console.error,
  );
});

chrome.storage.onChanged.addListener((changes, area) => {
  if (area === 'local' && changes.policyState) {
    void renderPolicy().catch(console.error);
  }
});

// Opening the popup is the reliable moment to ask the service worker to
// resurrect the offscreen WebSocket — nothing else is guaranteed to wake it.
chrome.runtime.sendMessage({ type: 'connect' }).catch(() => {});

refresh();
void renderPolicy().catch(console.error);
// The popup surfaces denials and paused downloads, so clear the badge.
void updateBadge().catch(console.error);

const poll = setInterval(refresh, POLL_INTERVAL_MS);

window.addEventListener('unload', () => {
  clearInterval(poll);
});

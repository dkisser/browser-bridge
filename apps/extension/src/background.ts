// Service Worker: manages the offscreen document and executes browser commands.
// The WebSocket connection lives in the offscreen document (it persists when SW sleeps).
// Commands arrive from offscreen via chrome.runtime.sendMessage, are executed here
// using Chrome APIs, and responses are returned via the sendResponse callback.

import {
  blocklistHit,
  type CommandResultMap,
  type CommandType,
  type Denial,
  evaluatePolicy,
  humanDenialMessage,
  isReadOnlyCommand,
  originOf,
  type PolicyContext,
  SENSITIVE_FIELD_RECHECK_ERROR,
  takeoverDenied,
} from '@browser-bridge/shared';
import { addTabToAgentGroup, queryAgentGroupIds } from './agent-group';
import {
  type ChromeLike,
  ContentScriptUnavailableError,
  dispatchToContentScript as dispatchToContentScriptRaw,
} from './content-bridge';
import { applyPolicyOp, type PolicyOp } from './policy-operations';
import {
  clearSessionScoped,
  decideWithState,
  getPolicyState,
  type PolicyState,
  recordDenial,
  updateBadge,
  updatePolicyState,
} from './policy-state';

// Bind the testable dispatch helper to the live chrome global once so the
// service-worker hot path doesn't pay the lookup on every command.
const dispatchToContentScript = (
  tabId: number,
  message: Record<string, unknown>,
  beforeSend?: () => void | Promise<void>,
): Promise<unknown> =>
  dispatchToContentScriptRaw(
    globalThis.chrome as unknown as ChromeLike,
    tabId,
    message,
    beforeSend,
  );

const OFFSCREEN_DOCUMENT_URL = 'offscreen.html';

class PolicyDeniedError extends Error {
  readonly denial: Denial;

  constructor(denial: Denial) {
    super(humanDenialMessage(denial));
    this.name = 'PolicyDeniedError';
    this.denial = denial;
  }
}

let _wsConnected = false;
let creatingOffscreen: Promise<void> | null = null;

async function ensureOffscreenDocument(): Promise<void> {
  const existingContexts = await chrome.runtime.getContexts({
    contextTypes: [chrome.runtime.ContextType.OFFSCREEN_DOCUMENT],
    documentUrls: [chrome.runtime.getURL(OFFSCREEN_DOCUMENT_URL)],
  });

  if (existingContexts.length > 0) return;

  if (creatingOffscreen) {
    await creatingOffscreen;
    return;
  }

  creatingOffscreen = chrome.offscreen
    .createDocument({
      url: OFFSCREEN_DOCUMENT_URL,
      reasons: [chrome.offscreen.Reason.WEB_RTC],
      justification: 'Maintain persistent WebSocket connection to Local Proxy',
    })
    .then(() => {
      creatingOffscreen = null;
    })
    .catch((err: Error) => {
      creatingOffscreen = null;
      throw err;
    });

  await creatingOffscreen;
}

interface CommandMessage {
  id: string;
  type: 'command';
  browserId: string;
  payload: {
    command: CommandType;
    tabId: number;
    params: Record<string, unknown>;
  };
  timestamp: number;
}

async function queryOffscreenStatus(): Promise<boolean> {
  try {
    await ensureOffscreenDocument();
    const response = await chrome.runtime.sendMessage({ type: 'ws_ping' });
    return response?.connected ?? false;
  } catch {
    return false;
  }
}

// Fallback when the control plane sends no `timeout` for navigate. Only an
// older control plane reaches this path — current ones always send their own
// budget — and the degradation is from "hangs forever" to "bounded", which is
// the point. The value matches the Go router's historical 30s route TTL; that
// is a coincidence of history, not a guarantee, since the router now takes
// the caller's deadline plus its own backstop margin. Set it for what a slow
// page plausibly needs, not to match anything on the other side.
const DEFAULT_NAV_TIMEOUT_MS = 30_000;

// The offscreen document cannot read chrome.storage, so the SW owns the
// pairing token and pushes it here. Idempotent: safe to call on every
// service-worker wake and on every pairing-token change.
async function connectOffscreen(): Promise<void> {
  await ensureOffscreenDocument();
  const state = await getPolicyState();
  await chrome.runtime
    .sendMessage({ type: 'connect_ws', token: state.pairingToken })
    .catch(() => {});
}

// reachedUrl reports whether a tab is now showing `target`.
//
// Compares the whole URL, not just the origin: a same-origin navigation
// (`/old-page` → `/page`) is exactly the case this must not accept, and an
// origin comparison would wave it through.
//
// Comparison goes through the URL parser, which canonicalizes a bare
// `https://example.com` to the `https://example.com/` the browser reports —
// the trailing-slash case is the parser's job, not something to redo by hand
// here. An earlier version normalized both sides manually (forcing an empty
// pathname to `/`, then stripping a trailing slash); every pair that version
// judged equal or unequal, this one judges the same, because both sides went
// through identical normalization either way. Anything the parser rejects
// (data:, about:) falls back to exact equality.
function reachedUrl(actual: string | undefined, target: string): boolean {
  if (actual === undefined) return false;
  const canonical = (raw: string): string | null => {
    try {
      return new URL(raw).href;
    } catch {
      return null;
    }
  };
  const a = canonical(actual);
  const b = canonical(target);
  if (a !== null && b !== null) return a === b;
  return actual === target;
}

// waitForTabComplete resolves once the tab reports status 'complete'.
//
// The listener can only be registered *after* the navigation is kicked off
// (chrome.tabs.update resolves once the navigation is initiated, not once it
// loads), so a page that finishes first loses its event entirely — a lost
// wakeup that left the promise pending forever and the listener leaked. Three
// things prevent that, and all three are needed:
//
//   - the current status is re-checked right after registration, covering the
//     event that already fired;
//   - a timeout bounds the wait, so a navigation that stalls mid-chain
//     (a slow redirect, a download response) fails with a real error instead
//     of pinning the command until the caller's own deadline;
//   - finish() is idempotent and always removes the listener, so no path
//     leaks one.
//
// `expectUrl` constrains only the re-check, and only for callers that know
// where they are going. chrome.tabs.update resolves while the tab may still
// be showing the *previous* page as 'complete', so an unconstrained re-check
// can resolve against the old page and hand the caller its url and title
// while reporting success. Requiring the tab to have actually moved to the
// target removes that. The event path is unaffected — a 'complete' event
// after the update is the new page — so a redirect to a different origin
// still resolves normally through the listener, and only a redirect *and* a
// fast cached load together would fall through to the timeout, which is a
// clear error rather than a confidently wrong answer.
function waitForTabComplete(
  tabId: number,
  timeout: number,
  expectUrl?: string,
): Promise<void> {
  return new Promise<void>((resolve, reject) => {
    let settled = false;
    let timer: ReturnType<typeof setTimeout>;
    const listener = (
      updatedTabId: number,
      changeInfo: chrome.tabs.TabChangeInfo,
    ) => {
      if (updatedTabId === tabId && changeInfo.status === 'complete') finish();
    };
    function finish(error?: Error): void {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      chrome.tabs.onUpdated.removeListener(listener);
      if (error) {
        reject(error);
      } else {
        resolve();
      }
    }
    timer = setTimeout(() => {
      finish(new Error('Navigation timeout'));
    }, timeout);
    chrome.tabs.onUpdated.addListener(listener);
    chrome.tabs
      .get(tabId)
      .then((current) => {
        if (current.status !== 'complete') return;
        if (expectUrl !== undefined && !reachedUrl(current.url, expectUrl)) {
          return;
        }
        finish();
      })
      .catch(() => {
        // Tab lookup failed; rely on the event listener and timeout.
      });
  });
}

// assertTakeoverUnchanged re-reads policy state and refuses to proceed if
// Takeover was engaged after applyPolicyGate allowed the command.
//
// The verdict comes from the shared policy core's takeoverDenied rather than
// from reading `state.takeover` here, so this cannot drift from the rule the
// gate applied. See the call site for why only the DOM commands need it.
//
// Exported for its own test. Nothing observable happens between the gate's
// decision and the read below — no event, no await — so a test cannot drive
// this function through handleCommand and then prove *this* was the denial
// rather than the gate's, which throws the same error with the same reason.
// Testing it directly is the only way to pin what it does, and leaving the
// integration test to imply otherwise is how it came to be over-claimed.
export async function assertTakeoverUnchanged(
  command: CommandType,
  origin: string | null,
): Promise<void> {
  const { decision } = await decideWithState(
    (fresh) =>
      takeoverDenied(command, fresh.takeover, { origin }) ?? { allow: true },
  );
  if (decision.allow || !decision.denial) return;

  // Withdrawn silently, and that is deliberate rather than an oversight.
  // takeoverDenied only ever produces human_assist_active, and recordDenial
  // drops exactly that reason — so recording it here would be a call that
  // cannot write anything, and refreshing the badge after it would recompute
  // a count that did not move. This comment previously claimed the user would
  // see the denial in the badge and the Approvals panel, which the very rule
  // that makes the re-check possible makes false: a takeover refusal is the
  // user's own kill switch firing, not a request for a decision. What they do
  // get is the standing Takeover indicator in the side panel, and the agent
  // still gets the full denial text.
  throw new PolicyDeniedError(decision.denial);
}

// Policy enforcement point (ADR-0006..0009): every command is evaluated
// against the shared policy core before it executes. Throws PolicyDeniedError
// when the decision is a denial; returns the policy state the decision was
// made against so handlers can reuse it.
async function applyPolicyGate(
  command: CommandType,
  tabId: number | undefined,
  params: Record<string, unknown>,
): Promise<{
  state: PolicyState;
  origin: string | null;
  sensitiveApproved: boolean;
}> {
  // Read-only commands read browser state rather than acting on a page, so
  // they skip the origin lookups below — but they still run through
  // evaluatePolicy, because the Takeover gate has to apply to them too.
  // They used to return early here, before evaluatePolicy was ever reached,
  // which let `bridge` read every open tab's URL and title while the user
  // believed the human had the browser.
  const readOnly = isReadOnlyCommand(command);
  // A `tab:new` with no url opens a blank tab: no navigation target exists to
  // gate. The policy core handles that case explicitly (blankNewTab) so the
  // shortcut lives there, behind the takeover check, instead of here in front
  // of it.
  const blankNewTab = command === 'tab:new' && !params.url;

  let origin: string | null = null;
  let isActiveTab = false;

  if (!readOnly && (command === 'navigate' || command === 'tab:new')) {
    origin = originOf(params.url as string | undefined);
  } else if (!readOnly && typeof tabId === 'number') {
    const tab = await chrome.tabs.get(tabId);
    origin = originOf(tab.url);
    if (command === 'screenshot') {
      const [activeTab] = await chrome.tabs.query({
        windowId: tab.windowId,
        active: true,
      });
      isActiveTab = activeTab?.id === tabId;
    }
  }

  let sensitiveField: boolean | undefined;
  if (command === 'type' && origin !== null) {
    // Protected contexts deny `type` below regardless of the field kind, and
    // content scripts cannot be injected there — skip the preflight.
    const preflight = await preflightSelector(tabId, params.selector as string);
    sensitiveField = preflight.sensitive;
  }

  // Decide against fresh state inside the serialized write queue: the
  // decision and any grant consumption below are atomic with respect to
  // concurrent commands, so a singleUse grant cannot be double-consumed.
  const { decision, state } = await decideWithState((fresh) => {
    const ctx: PolicyContext = {
      takeover: fresh.takeover,
      origin,
      blocklistHit: blocklistHit(origin, fresh.blockedOrigins),
      grants: fresh.grants,
      ...(blankNewTab ? { blankNewTab: true } : {}),
      ...(typeof tabId === 'number' ? { tabId } : {}),
      ...(sensitiveField !== undefined ? { sensitiveField } : {}),
      ...(params.submit === true ? { submit: true } : {}),
    };
    if (origin !== null) {
      if (fresh.deniedOrigins[origin] !== undefined) {
        ctx.originState = 'denied';
      } else if (fresh.origins[origin] !== undefined) {
        ctx.originState = 'approved';
      }
    }
    if (command === 'screenshot') {
      ctx.isVisibleApprovedTab = isActiveTab && ctx.originState === 'approved';
    }
    if (command === 'tab:close') {
      ctx.isAgentTab = fresh.agentTabs.includes(params.tabId as number);
    }
    return evaluatePolicy(command, ctx);
  });

  if (!decision.allow) {
    await recordDenial(decision.denial);
    await updateBadge();
    throw new PolicyDeniedError(decision.denial);
  }
  const sensitiveApproved = (decision.consume ?? []).some(
    (grant) => grant.capability === 'sensitive-field',
  );
  return { state, origin, sensitiveApproved };
}

/**
 * Internal entry point used by the chrome.runtime.onMessage('command') path
 * (which wraps `PolicyDeniedError` in the WS-contract `{status:'error',
 * denial:{...}}` envelope) and by the orchestration-layer test in
 * `tests/background-gate.test.ts`. Not a public API for side-panel/settings/
 * content modules — those go through the WS message channel so the denial
 * envelope reaches the bridge-core consumer intact.
 *
 * @internal
 */
export async function handleCommand(
  msg: CommandMessage,
): Promise<CommandResultMap[CommandType]> {
  const { payload } = msg;
  const { command, tabId, params } = payload;

  const { origin, sensitiveApproved } = await applyPolicyGate(
    command,
    tabId,
    params,
  );

  switch (command) {
    case 'navigate': {
      if (typeof tabId !== 'number') {
        throw new Error('Missing required tabId');
      }
      const tab = tabId;
      const target = params.url as string;
      await chrome.tabs.update(tab, { url: target });
      // The wait is bounded and cannot lose its completion event; passing the
      // target keeps the re-check from accepting the page we are leaving.
      // See waitForTabComplete.
      await waitForTabComplete(
        tab,
        (params.timeout as number) || DEFAULT_NAV_TIMEOUT_MS,
        target,
      );
      const updatedTab = await chrome.tabs.get(tab);
      return { url: updatedTab.url, title: updatedTab.title };
    }

    case 'goBack': {
      if (typeof tabId !== 'number') {
        throw new Error('Missing required tabId');
      }
      const tab = tabId;
      await chrome.tabs.goBack(tab);
      return { ok: true };
    }

    case 'goForward': {
      if (typeof tabId !== 'number') {
        throw new Error('Missing required tabId');
      }
      const tab = tabId;
      await chrome.tabs.goForward(tab);
      return { ok: true };
    }

    case 'refresh': {
      if (typeof tabId !== 'number') {
        throw new Error('Missing required tabId');
      }
      const tab = tabId;
      await chrome.tabs.reload(tab);
      return { ok: true };
    }

    case 'tab:list': {
      const [tabs, agentGroupIds] = await Promise.all([
        chrome.tabs.query({}),
        // Querying the agent group requires the `tabGroups` permission,
        // which Chrome may prompt for (and the user may deny) on extension
        // update. A rejection here would otherwise fail the whole command,
        // so we degrade to an empty set and log — tab:list still returns
        // tabs, just without the inAgentGroup mark.
        queryAgentGroupIds().catch((error) => {
          console.error(
            'browser-bridge: failed to query agent groups (degrading tab:list)',
            error,
          );
          return new Set<number>();
        }),
      ]);
      return tabs.map((t) => ({
        id: t.id,
        url: t.url,
        title: t.title,
        active: t.active,
        windowId: t.windowId,
        inAgentGroup: agentGroupIds.has(t.groupId),
      }));
    }

    case 'tab:new': {
      const active = params.active === true;
      const newTab = await chrome.tabs.create({
        url: params.url as string | undefined,
        active,
      });
      if (newTab.id !== undefined) {
        await updatePolicyState((fresh) => ({
          agentTabs: [...fresh.agentTabs, newTab.id as number],
        }));
        // Visual grouping (ADR-0014). Awaited (not fire-and-forget) so that
        // an immediate follow-up tab:list sees the freshly-created tab in
        // the agent group — otherwise the tab is returned with
        // inAgentGroup=false until the chrome.tabs.group IPC round-trip
        // completes, and any agent that gates behavior on inAgentGroup
        // makes the wrong decision. addTabToAgentGroup still swallows its
        // own errors so a failed group never fails tab:new.
        await addTabToAgentGroup(newTab.id, newTab.windowId);
      }
      return { id: newTab.id, url: newTab.url };
    }

    case 'tab:close': {
      await chrome.tabs.remove(params.tabId as number);
      await updatePolicyState((fresh) => ({
        agentTabs: fresh.agentTabs.filter((id) => id !== params.tabId),
      }));
      return { ok: true };
    }

    case 'tab:switch': {
      const targetTabId = params.tabId as number;
      const tab = await chrome.tabs.update(targetTabId, { active: true });
      return { id: tab.id, url: tab.url, title: tab.title };
    }

    case 'pageinfo': {
      if (typeof tabId !== 'number') {
        throw new Error('Missing required tabId');
      }
      const tab = tabId;
      const t = await chrome.tabs.get(tab);
      return { id: t.id, url: t.url, title: t.title, active: t.active };
    }

    case 'screenshot': {
      if (typeof tabId !== 'number') {
        throw new Error('Missing required tabId');
      }
      const tab = tabId;
      const activeTab = await chrome.tabs.get(tab);
      const dataUrl = await chrome.tabs.captureVisibleTab(activeTab.windowId, {
        format: 'png',
      });
      if (!dataUrl) {
        throw new Error(
          'Unable to capture screenshot: tab must be active in a visible window',
        );
      }
      return { dataUrl };
    }

    case 'wait:navigation': {
      const timeout = (params.timeout as number) || 10000;
      if (typeof tabId !== 'number') {
        throw new Error('Missing required tabId');
      }
      const tab = tabId;
      await waitForTabComplete(tab, timeout);
      const t = await chrome.tabs.get(tab);
      return { url: t.url, title: t.title };
    }

    // DOM commands — forward to content script
    case 'click':
    case 'type':
    case 'select':
    case 'scroll':
    case 'hover':
    case 'gettext':
    case 'gethtml':
    case 'snapshot':
    case 'wait:element': {
      // Takeover re-check at the execution point.
      //
      // The gate above read `takeover` inside the write queue, and a
      // takeover flip is applied in that same queue, so a command that was
      // still queued when the user hit the switch does see it. What the
      // gate cannot cover is the stretch between its decision and the DOM
      // actually changing — and that stretch is not short. Before the
      // message goes out, sendToContentScript calls ensureContentScript,
      // which on a cold tab does a tabs.get, a ping, an executeScript, and
      // a listener wait of up to two seconds. A user reaching for the kill
      // switch during that is looking at a page that clicks itself
      // afterwards.
      //
      // So the check is passed down as `beforeSend` and runs *inside*
      // dispatchToContentScript, between ensureContentScript and
      // tabs.sendMessage. It used to run here, before that whole block —
      // which meant it covered a window that had already closed by the time
      // the command was delivered. The comment above used to claim
      // otherwise; the test that now keys on the injection boundary is what
      // makes the difference observable.
      //
      // This is the only command group that needs it, and saying so
      // matters more than the check itself. Every other branch's remaining
      // window is far narrower than this one: `screenshot` does await
      // `chrome.tabs.get` before capturing, and that is an IPC round trip to
      // the browser process which a busy or wedged browser can delay
      // arbitrarily — it is not the non-blocking read an earlier version of
      // this comment called it. What keeps it out of scope is that the window
      // is short and its payoff is bounded: `captureVisibleTab` only ever
      // photographs the window's currently active tab, so the worst case is a
      // screenshot of a different tab than the one that was approved, not a
      // mutation the user did not authorize. That is a narrower exposure than
      // an agent clicking a page they just took back, and paying for it with
      // a second queue round trip on every screenshot is the worse trade. The
      // commands that genuinely do wait for a long time, navigate and
      // wait:navigation, spend it *after* their effect has already happened,
      // where no re-check could undo anything. wait:element rides along only
      // because it shares this dispatch path; its own wait happens in the
      // page, after this check has already run.
      //
      // What this cannot do: recall a mutation that already reached the
      // page. It closes the window, it does not make the control absolute.
      //
      // And a command withdrawn here has still spent its grant. The gate
      // consumed and persisted any single-use capability inside the write
      // queue, before this point, so a `type` that the re-check then refuses
      // burns a one-shot sensitive-field approval without ever reaching the
      // page. Refunding it is not available: the consumption is what makes it
      // un-double-spendable, and putting the grant back after an await would
      // resurrect one a concurrent command is entitled to believe is gone.
      // Spending early fails closed — the user re-approves, nothing the
      // agent did not authorize ever happened — which is the right direction
      // for a control whose job is to be inconvenient under pressure. The
      // cost is that "one-shot" means one *attempt*, not one execution, and
      // the test below pins that so it stays a decision.
      //
      // A sensitive-field grant consumed at the gate authorizes this one type
      // command; the content script re-verifies the field at execution time.
      const forwarded =
        command === 'type' && sensitiveApproved
          ? {
              ...payload,
              params: { ...payload.params, sensitiveApproved: true },
            }
          : payload;
      try {
        return (await sendToContentScript(tabId, forwarded, () =>
          assertTakeoverUnchanged(command, origin),
        )) as CommandResultMap[typeof command];
      } catch (err) {
        // Execution-point recheck: the field became sensitive between the
        // preflight and the write. Surface it as a policy denial so the agent
        // asks for approval instead of retrying blindly.
        if (
          err instanceof Error &&
          err.message === SENSITIVE_FIELD_RECHECK_ERROR
        ) {
          const denial: Denial = {
            reason: 'approval_required',
            command,
            ...(origin !== null ? { origin } : {}),
            capability: 'sensitive-field',
            detail:
              'target field was classified sensitive at execution time — obtain approval and retry',
          };
          await recordDenial(denial);
          await updateBadge();
          throw new PolicyDeniedError(denial);
        }
        throw err;
      }
    }

    default:
      throw new Error(`Unknown command: ${command}`);
  }
}

async function sendToContentScript(
  tabId: number | undefined,
  payload: Record<string, unknown>,
  beforeSend?: () => void | Promise<void>,
): Promise<unknown> {
  if (typeof tabId !== 'number') {
    throw new Error('Missing required tabId');
  }
  return await dispatchToContentScript(
    tabId,
    { type: 'command', payload },
    beforeSend,
  );
}

// Policy preflight for `type`: asks the content script to classify the
// target element (password / credit-card) before the policy decision.
async function preflightSelector(
  tabId: number | undefined,
  selector: string,
): Promise<{ sensitive: boolean }> {
  if (typeof tabId !== 'number') {
    throw new Error('Missing required tabId');
  }
  const data = await dispatchToContentScript(tabId, {
    type: 'preflight',
    selector,
  });
  return data as { sensitive: boolean };
}

// Message handler: receives commands from offscreen doc, side panel, and content scripts
// isOwnExtensionContext reports whether a message came from one of this
// extension's own pages rather than from a content script or another
// extension.
//
// The check exists for the two messages below, which are the most privileged
// things this service worker accepts: `policy_op` can switch Takeover off and
// mint a `sensitive-field` or `submit` grant, and `ws_command` reaches
// handleCommand. Neither is reachable by a web page today — the manifest
// declares no `externally_connectable` and there is no onMessageExternal and
// no window.postMessage bridge — so this is defence in depth rather than a
// fix for a live hole. That is the point: the day any of those three is added
// for an unrelated reason, "a page can release the user's kill switch and
// grant itself a password-field capability" should already be closed.
//
// `sender.id` alone is not enough. A content script's sender carries this
// extension's own id, so it passes that test; what separates a content script
// from a panel is `sender.url`, which for a content script is the page's URL
// rather than a chrome-extension:// one.
function isOwnExtensionContext(sender: chrome.runtime.MessageSender): boolean {
  if (sender.id !== chrome.runtime.id) return false;
  const url = sender.url;
  if (typeof url !== 'string') return false;
  return url.startsWith(`chrome-extension://${chrome.runtime.id}/`);
}

chrome.runtime.onMessage.addListener((request, sender, sendResponse) => {
  if (
    (request.type === 'policy_op' || request.type === 'ws_command') &&
    !isOwnExtensionContext(sender)
  ) {
    // No detail in the reply: a page that can send this can read the reply,
    // and telling it which check failed is a free oracle. The user is not
    // the audience for a spoofed sender.
    sendResponse({
      status: 'error',
      error: 'forbidden_sender',
      message: 'Message rejected: not an extension context.',
    });
    return false;
  }

  // Command from offscreen document (originating from Local Proxy)
  if (request.type === 'ws_command') {
    const envelope = request.envelope as CommandMessage;
    handleCommand(envelope)
      .then((data) => sendResponse({ status: 'ok', data }))
      .catch((err: Error) => {
        if (err instanceof PolicyDeniedError) {
          sendResponse({
            status: 'error',
            error: err.denial.reason,
            message: humanDenialMessage(err.denial),
            denied: err.denial,
          });
          return;
        }
        // ContentScriptUnavailableError carries a structured reason so MCP
        // tools can attach a recovery hint (see withRecoveryHint in the
        // control plane, internal/http). Surface it alongside the message.
        if (err instanceof ContentScriptUnavailableError) {
          sendResponse({
            status: 'error',
            error: err.reason,
            message: err.message,
            reason: err.reason,
          });
          return;
        }
        sendResponse({ status: 'error', error: err.message });
      });
    return true; // async response
  }

  // Policy mutation requested by a UI context (side panel / pairing).
  //
  // The UI deliberately does not write chrome.storage.local itself. The
  // serialization queue in policy-state.ts is a module-level variable, so
  // each JS context has its own copy and its own last-writer-wins window
  // over the whole state object — a UI write could resurrect a singleUse
  // grant the service worker had just consumed, or drop the user's takeover
  // toggle. Routing the mutation through here makes the service worker the
  // only writer, so its queue is the only one that matters.
  //
  // A callback could not cross this boundary, so the UI names an operation
  // (see policy-operations.ts) instead of supplying the code to run.
  if (request.type === 'policy_op') {
    const op = request.op as PolicyOp;
    updatePolicyState((state) => applyPolicyOp(state, op))
      .then((state) => {
        // The badge is cosmetic and must never be able to fail the
        // operation's acknowledgement. updateBadge awaits a storage read and
        // three chrome.action calls, all of which can reject. If a badge
        // failure could reach the caller, an operation whose write had
        // already landed would be reported as failed — and the panel does
        // not render ahead of the write, so its switch would still be
        // showing the old value while the engine enforced the new one. For
        // Takeover that is "your kill switch did not move, try again" about a
        // change that did take effect, which is the one way this control can
        // make things worse than showing nothing at all. Refresh the badge,
        // but never let it answer for the write.
        void updateBadge().catch((err: unknown) => {
          console.error(
            'browser-bridge: badge refresh failed after policy op',
            err,
          );
        });
        sendResponse({ status: 'ok', data: state });
      })
      .catch((err: Error) => {
        sendResponse({ status: 'error', error: err.message });
      });
    return true; // async response
  }

  // Status update from offscreen document
  if (request.type === 'ws_status') {
    _wsConnected = request.connected;
    return false;
  }

  // Popup: ping connection status — query offscreen for real status
  if (request.type === 'ping') {
    queryOffscreenStatus()
      .then((connected) => sendResponse({ type: 'pong', connected }))
      .catch(() => sendResponse({ type: 'pong', connected: false }));
    return true;
  }

  // Popup: trigger connection
  if (request.type === 'connect') {
    connectOffscreen()
      .then(() => sendResponse({ type: 'connected' }))
      .catch((err: Error) => {
        sendResponse({ type: 'error', message: err.message });
      });
    return true;
  }

  // Side panel: open the read-only settings page in a new tab. The page
  // itself is a separate extension options page (see manifest options_ui).
  if (request.type === 'open-options') {
    chrome.runtime
      .openOptionsPage()
      .then(() => sendResponse({ status: 'ok' }))
      .catch((err: Error) =>
        sendResponse({ status: 'error', error: err.message }),
      );
    return true;
  }

  return false;
});

// Keep agentTabs in sync with reality: tabs the user closed are no longer
// agent tabs.
chrome.tabs.onRemoved.addListener((tabId) => {
  updatePolicyState((state) => ({
    agentTabs: state.agentTabs.filter((id) => id !== tabId),
  })).catch(console.error);
});

// Downloads started by agent-created tabs are paused until the human
// resumes or cancels them in the side panel (unless human assist is active,
// in which case the user is in charge already).
chrome.downloads.onCreated.addListener((item) => {
  // Installed @types/chrome predates DownloadItem.tabId (Chrome 116+).
  const download = item as chrome.downloads.DownloadItem & {
    tabId?: number;
  };
  if (download.tabId === undefined || download.tabId < 0) return;
  void (async () => {
    try {
      await chrome.downloads.pause(download.id);
    } catch {
      // Pause can race with a download that already completed; ignore.
    }
    const recorded = await updatePolicyState((state) => {
      if (
        state.takeover ||
        !state.agentTabs.includes(download.tabId as number)
      ) {
        return null;
      }
      return {
        pendingDownloads: [
          ...state.pendingDownloads,
          {
            id: download.id,
            filename: download.filename,
            url: download.url,
          },
        ],
      };
    }).then((state) =>
      state.pendingDownloads.some((entry) => entry.id === download.id),
    );
    if (recorded) await updateBadge();
  })().catch(console.error);
});

// Re-pairing rotates the pairing token. The offscreen cannot watch storage
// itself (only chrome.runtime is available there), so the SW forwards token
// changes to it — this also covers clears (token → null), which tear the
// socket down until a new pairing completes.
chrome.storage.onChanged.addListener((changes, area) => {
  if (area !== 'local' || !changes.policyState) return;
  const oldToken = (
    changes.policyState.oldValue as { pairingToken?: string | null } | undefined
  )?.pairingToken;
  const newToken = (
    changes.policyState.newValue as { pairingToken?: string | null } | undefined
  )?.pairingToken;
  if (newToken === oldToken) return;
  connectOffscreen().catch(console.error);
});

async function initialize(): Promise<void> {
  await clearSessionScoped();
  await updateBadge();
  await connectOffscreen();
}

// The human surface is the side panel (ADR-0010). Make the extension icon
// a one-click trigger to open the panel on the active tab. Re-applied on
// every SW wake so a disable/re-enable in chrome://extensions recovers the
// behaviour without waiting for the next browser restart.
async function ensurePanelBehavior(): Promise<void> {
  try {
    await chrome.sidePanel.setPanelBehavior({ openPanelOnActionClick: true });
  } catch (error: unknown) {
    console.error(error);
  }
}

// Initialize offscreen document on extension install/startup
chrome.runtime.onInstalled.addListener(() => {
  initialize().catch(console.error);
  ensurePanelBehavior().catch(console.error);
});

chrome.runtime.onStartup.addListener(() => {
  initialize().catch(console.error);
  ensurePanelBehavior().catch(console.error);
});

// Also try on SW wake — if the offscreen was killed or the proxy restarted
// while we slept, this reconnects it with the stored token. The panel
// behaviour must also be re-applied on every wake (findings #4, #5).
connectOffscreen().catch(console.error);
ensurePanelBehavior().catch(console.error);

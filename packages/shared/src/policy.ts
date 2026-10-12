import type { CommandType } from './types';

// The human's three-way supervision switch (ADR-0038). It moves the threshold
// at which commands must clear the Working scope; it never moves the boundary
// — deny, the blocklist, protected origins, unknown commands and Takeover hold
// in every mode.
export type PermissionMode = 'strict' | 'standard' | 'relaxed';

// Fail-safe default: a caller that does not pass a mode gets today's behavior
// (reads and writes both gated), so omitting it can only hold more back.
const DEFAULT_PERMISSION_MODE: PermissionMode = 'strict';

export type DenyReason =
  | 'human_assist_active'
  | 'origin_not_approved'
  // The human closed the request without answering it (ADR-0040). Distinct
  // from origin_denied, which is a refusal that holds in every mode: this one
  // is scoped to (origin, command, mode) and expires with the session.
  | 'origin_ignored'
  | 'origin_denied'
  | 'origin_blocked'
  | 'action_out_of_scope'
  | 'approval_required'
  | 'unknown_command';

export type GrantCapability =
  | 'origin'
  | 'submit'
  | 'sensitive-field'
  | 'screenshot'
  | 'tab-close';

export interface Grant {
  capability: GrantCapability;
  origin?: string;
  tabId?: number;
  expiresAt: number;
  singleUse: boolean;
}

/**
 * Whether this reason is a request the human can answer about an origin —
 * the two reasons that offer Session / Always / Deny, and the two reasons a
 * dismissal (ADR-0040) may be recorded against.
 *
 * One predicate rather than the set spelled out at each of its call sites.
 * The sites are: the reducer deciding whether a dismissal records a stance,
 * the close affordance deciding whether to render, the button set deciding
 * which answers to offer, and the denial dedupe deciding what counts as the
 * same outstanding request. They spelled it out separately once, and the first
 * implementation of this feature passed an eleven-test suite while three of
 * them disagreed — a new origin-scoped reason would now have to be added to
 * four places and each has its own tests, so a miss stays green. Adding a
 * reason here is the single edit that reaches all of them.
 */
export function isOriginRequestReason(reason: DenyReason): boolean {
  return reason === 'origin_not_approved' || reason === 'origin_ignored';
}

export interface Denial {
  reason: DenyReason;
  command: CommandType;
  origin?: string;
  // The permission mode the decision was made under, resolved to a concrete
  // value. Set by evaluatePolicy for every denial it produces; ADR-0040 keys a
  // dismissal on it, and a card can be closed long after the mode changed.
  mode?: PermissionMode;
  capability?: GrantCapability;
  detail?: string;
}

export type PolicyDecision =
  | { allow: true; consume?: Grant[] }
  | { allow: false; denial: Denial };

export interface PolicyContext {
  takeover: boolean;
  // Omitted means 'strict' — the mode that gates both reads and writes. The
  // extension always passes a concrete mode; the default exists so an omitted
  // field can never quietly widen what runs silent.
  permissionMode?: PermissionMode;
  // Origin of the target URL (navigate/tab:new) or of the tab the command
  // would act on. null means a protected or non-http(s) context — such
  // commands are hard-denied with 'origin_blocked', no approval path.
  origin: string | null;
  // True for a `tab:new` that carries no url: the command opens a blank tab
  // and there is no navigation target to gate, so the origin_blocked rule
  // below must not apply. This is deliberately distinct from "a url was
  // supplied but parses to a non-http(s) origin" (file://, data:, about:),
  // which stays hard-denied — a caller that named a target still gets the
  // protected-context check.
  blankNewTab?: boolean;
  originState?: 'approved' | 'denied' | 'ignored';
  // Whether this origin carries a dismissed stance (ADR-0040) *for this
  // command and this mode* is the caller's to make: it owns the stored key
  // and is the only context that can read it. The policy core takes the
  // matched result, the same shape it takes for 'approved' and 'denied', so
  // that the rule for what a dismissal *means* stays here rather than in
  // whichever context happened to compute it.
  blocklistHit?: boolean;
  isAgentTab?: boolean;
  isVisibleApprovedTab?: boolean;
  sensitiveField?: boolean;
  submit?: boolean;
  tabId?: number;
  grants?: Grant[];
  now?: number;
}

// Browser-level reads; these need no origin lookup before evaluation. Derived
// into isReadOnlyCommand below.
const READONLY_COMMANDS_ARR = ['tab:list', 'pageinfo'] as const;

// Commands that act on or read page content; denied outright in protected
// contexts and on blocklist hits, before any other rule, in every mode.
const PAGE_CONTEXT_COMMANDS_ARR = [
  'navigate',
  'tab:new',
  'goBack',
  'goForward',
  'refresh',
  'click',
  'type',
  'select',
  'scroll',
  'hover',
  'gettext',
  'gethtml',
  'snapshot',
  'wait:element',
  'screenshot',
] as const;

// Safety levels (ADR-0038). The Permission mode decides which of these must
// clear the Working scope before running silent; it does not decide whether
// they are safe. The four arrays are exhaustive over CommandType and the
// check below makes a new command uncompilable until it is classified — a
// command nobody thought about must not inherit a level by accident.

// Browser state and passive waits. Never gated in any mode: they reveal
// nothing the human has not already put on screen, and holding them would
// mean the agent cannot even read the tab list while a gate is pending.
//
// wait:element is here by decision (ADR-0038), and the argument for it is
// narrower than "it returns a boolean, never content" — that claim does not
// hold, because the selector is agent-chosen and found/not-found discloses
// content one bit at a time. What it actually gets is that it is a *passive
// wait*, needed to drive the reads that do ask: gating it would mean a read
// command had to be approved twice before it could even see the page. The
// cost is that an approved-origin gate no longer applies to an existence
// probe, so it is called out in the CHANGELOG rather than left implicit.
const OBSERVER_COMMANDS_ARR = [
  'tab:list',
  'pageinfo',
  'tab:switch',
  'wait:element',
  'wait:navigation',
  'goBack',
  'goForward',
  'refresh',
] as const;

// Reaching a new origin or carrying page content out of one. `tab:new` with a
// url classifies here rather than as observer: renaming `navigate` must not
// walk around the gate. (A url-less `tab:new` is an observer in effect — see
// the blankNewTab branch, which allows it before this classification is read.)
const READ_COMMANDS_ARR = [
  'navigate',
  'tab:new',
  'snapshot',
  'gettext',
  'gethtml',
] as const;

// Acting on page elements. Changes what the page holds but is confined to it.
const WRITE_COMMANDS_ARR = [
  'click',
  'type',
  'select',
  'scroll',
  'hover',
] as const;

// Safe or dangerous by context rather than by label (ADR-0007's evidence):
// whose tab, whose origin, whether the keystroke submits. These keep their own
// binding rules in every mode, and `type` reaches this level through its
// submit / sensitive-field flags rather than by sitting in this array.
const SENSITIVE_COMMANDS_ARR = ['screenshot', 'tab:close'] as const;

type SafetyLevelCommand =
  | (typeof OBSERVER_COMMANDS_ARR)[number]
  | (typeof READ_COMMANDS_ARR)[number]
  | (typeof WRITE_COMMANDS_ARR)[number]
  | (typeof SENSITIVE_COMMANDS_ARR)[number];
type AllCommandsClassified = [CommandType] extends [SafetyLevelCommand]
  ? true
  : never;
const _allCommandsClassified: AllCommandsClassified = true;
void _allCommandsClassified;

const READONLY_COMMANDS = new Set<CommandType>(READONLY_COMMANDS_ARR);
const PAGE_CONTEXT_COMMANDS = new Set<CommandType>(PAGE_CONTEXT_COMMANDS_ARR);
const OBSERVER_COMMANDS = new Set<CommandType>(OBSERVER_COMMANDS_ARR);
const READ_COMMANDS = new Set<CommandType>(READ_COMMANDS_ARR);
const WRITE_COMMANDS = new Set<CommandType>(WRITE_COMMANDS_ARR);

// An explicit human denial is a boundary, not a threshold, so it holds in
// every mode. wait:element is in this rail despite being an observer: a denied
// origin must not become reachable just because the mode stopped asking.
const DENIED_ORIGIN_RAIL = new Set<CommandType>([
  ...READ_COMMANDS_ARR,
  ...WRITE_COMMANDS_ARR,
  'wait:element',
]);

// Which levels the current mode holds for an origin approval. Reads and writes
// move independently; observer never gates and sensitive never does here (its
// branches ask on their own terms).
function originApprovalGated(
  command: CommandType,
  mode: PermissionMode,
): boolean {
  if (READ_COMMANDS.has(command)) return mode === 'strict';
  if (WRITE_COMMANDS.has(command)) return mode !== 'relaxed';
  return false;
}

/**
 * Reports a command that needs no origin lookup before it can be evaluated —
 * it reads browser-level state rather than acting on a page.
 *
 * The extension uses this only to skip the chrome.tabs lookups, never to skip
 * evaluatePolicy: every command still passes through the policy core so the
 * takeover gate applies. It used to keep its own copy of the list, and that
 * second list — not the policy — decided which commands bypassed the gate
 * entirely, which is how Takeover ended up bypassed for tab:list / pageinfo.
 * Deriving it from READONLY_COMMANDS_ARR keeps the two from diverging.
 */
export function isReadOnlyCommand(command: CommandType): boolean {
  return READONLY_COMMANDS.has(command);
}

function denial(
  command: CommandType,
  reason: DenyReason,
  ctx: Pick<PolicyContext, 'origin' | 'permissionMode'>,
  capability?: GrantCapability,
  detail?: string,
): PolicyDecision {
  return {
    allow: false,
    denial: {
      reason,
      command,
      // The mode this decision was made under (ADR-0040). A dismissal is keyed
      // on the mode it was made in, and the human may change mode between the
      // request being raised and the card being closed — so the stance has to
      // carry the mode the *request* was made under, which is not knowable
      // later. Optional only so hand-built denials stay convenient; every one
      // produced by evaluatePolicy carries it.
      mode: ctx.permissionMode ?? DEFAULT_PERMISSION_MODE,
      ...(ctx.origin !== null ? { origin: ctx.origin } : {}),
      ...(capability !== undefined ? { capability } : {}),
      ...(detail !== undefined ? { detail } : {}),
    },
  };
}

function findGrant(
  ctx: PolicyContext,
  now: number,
  capability: GrantCapability,
): Grant | undefined {
  return ctx.grants?.find(
    (grant) =>
      grant.capability === capability &&
      grant.expiresAt > now &&
      (grant.origin === undefined || grant.origin === ctx.origin) &&
      (grant.tabId === undefined || grant.tabId === ctx.tabId),
  );
}

export function originOf(url: string | null | undefined): string | null {
  if (!url) return null;
  try {
    const parsed = new URL(url);
    return parsed.protocol === 'https:' || parsed.protocol === 'http:'
      ? parsed.origin
      : null;
  } catch {
    return null;
  }
}

// takeoverDenied is the denial Takeover produces, or null when it is off.
//
// Split out of evaluatePolicy so a caller re-checking Takeover at an execution
// point asks the gate's own question instead of reading `state.takeover` and
// hand-rolling a second verdict next to it. A re-check that reimplemented the
// rule would be free to drift from it, and the drift would be a security
// control quietly disagreeing with itself.
//
// Note what this deliberately does not do: it takes the state read from
// whatever queue the caller is in. Reading fresh state is the caller's half —
// this is only the rule.
export function takeoverDenied(
  command: CommandType,
  takeover: boolean,
  // permissionMode is in the Pick for the mode stamped on the denial (F5):
  // omitting it type-checks because the field is optional, and then every
  // Takeover refusal falls back to DEFAULT_PERMISSION_MODE — so a profile in
  // relaxed reported 'strict' on the wire, which is the one value the new
  // field is guaranteed to get wrong.
  ctx: Pick<PolicyContext, 'origin' | 'permissionMode'>,
): PolicyDecision | null {
  if (!takeover) return null;
  return denial(command, 'human_assist_active', ctx);
}

export function evaluatePolicy(
  command: CommandType,
  ctx: PolicyContext,
): PolicyDecision {
  const takeover = takeoverDenied(command, ctx.takeover, ctx);
  if (takeover) {
    return takeover;
  }

  // A url-less `tab:new` opens a blank tab: there is no navigation target, so
  // the protected-origin rule below has nothing to test. This branch must sit
  // *after* the takeover check — it is the reason the extension used to skip
  // evaluatePolicy entirely for this command, which left Takeover (the
  // human's kill switch) bypassed: a blank tab would keep appearing while the
  // user believed the agent was locked out.
  if (command === 'tab:new' && ctx.blankNewTab === true) {
    return { allow: true };
  }

  if (
    PAGE_CONTEXT_COMMANDS.has(command) &&
    (ctx.origin === null || ctx.blocklistHit === true)
  ) {
    return denial(
      command,
      'origin_blocked',
      ctx,
      undefined,
      ctx.blocklistHit === true
        ? 'origin is on the blocklist'
        : 'protected or non-http(s) URL',
    );
  }

  if (READONLY_COMMANDS.has(command)) {
    return { allow: true };
  }

  const now = ctx.now ?? Date.now();
  const mode = ctx.permissionMode ?? DEFAULT_PERMISSION_MODE;

  // An explicit denial outlives the mode: the switch moves which commands ask,
  // not which origins the human has closed off.
  if (ctx.originState === 'denied' && DENIED_ORIGIN_RAIL.has(command)) {
    return denial(command, 'origin_denied', ctx);
  }

  // Read and write commands other than `type`: approved origins pass,
  // unapproved ones need a one-shot origin grant — but only when the current
  // mode holds this safety level. `type` is excluded — its origin gate is
  // folded into its own branch so an origin grant cannot short-circuit the
  // submit / sensitive-field checks.
  if (
    command !== 'type' &&
    (READ_COMMANDS.has(command) || WRITE_COMMANDS.has(command))
  ) {
    if (originApprovalGated(command, mode) && ctx.originState !== 'approved') {
      // Inside the gate, not before it: a mode that does not gate this
      // command has no request to have been declined, and reaching here in
      // relaxed would hold back a command the mode released.
      if (ctx.originState === 'ignored') {
        return denial(command, 'origin_ignored', ctx, 'origin');
      }
      const grant = findGrant(ctx, now, 'origin');
      if (grant) return { allow: true, consume: [grant] };
      return denial(command, 'origin_not_approved', ctx, 'origin');
    }
    return { allow: true };
  }

  if (command === 'type') {
    const consume: Grant[] = [];
    if (originApprovalGated(command, mode) && ctx.originState !== 'approved') {
      // Same position as the read/write branch above, and for the same
      // reason: the origin comes first, so a dismissal is the answer the
      // agent gets before submit and sensitive-field ever come into it.
      if (ctx.originState === 'ignored') {
        return denial(command, 'origin_ignored', ctx, 'origin');
      }
      const originGrant = findGrant(ctx, now, 'origin');
      if (!originGrant) {
        return denial(command, 'origin_not_approved', ctx, 'origin');
      }
      consume.push(originGrant);
    }
    if (ctx.submit === true) {
      const grant = findGrant(ctx, now, 'submit');
      if (!grant) {
        return denial(
          command,
          'approval_required',
          ctx,
          'submit',
          'form submission always requires approval',
        );
      }
      consume.push(grant);
    }
    if (ctx.sensitiveField === true) {
      const grant = findGrant(ctx, now, 'sensitive-field');
      if (!grant) {
        return denial(
          command,
          'approval_required',
          ctx,
          'sensitive-field',
          'password or credit-card fields always require approval',
        );
      }
      consume.push(grant);
    }
    return { allow: true, ...(consume.length > 0 ? { consume } : {}) };
  }

  if (command === 'screenshot') {
    if (ctx.isVisibleApprovedTab === true) {
      return { allow: true };
    }
    const grant = findGrant(ctx, now, 'screenshot');
    if (grant) return { allow: true, consume: [grant] };
    return denial(
      command,
      'action_out_of_scope',
      ctx,
      'screenshot',
      'target tab is not the visible tab of its window or its origin is not approved',
    );
  }

  if (command === 'tab:close') {
    if (ctx.isAgentTab === true) {
      return { allow: true };
    }
    const grant = findGrant(ctx, now, 'tab-close');
    if (grant) return { allow: true, consume: [grant] };
    return denial(
      command,
      'action_out_of_scope',
      ctx,
      'tab-close',
      'tab was not opened by the agent',
    );
  }

  // Observer commands are allowed on any http(s) origin once takeover and the
  // protected / blocklist checks above are cleared, in every mode.
  if (OBSERVER_COMMANDS.has(command)) {
    return { allow: true };
  }

  // Fail closed: anything that reached this point is not recognized by the
  // policy (a command from a newer client, or malformed wire data) — deny it
  // rather than letting it through by default.
  return denial(
    command,
    'unknown_command',
    ctx,
    undefined,
    'command is not recognized by the policy',
  );
}

export function humanDenialMessage(d: Denial): string {
  switch (d.reason) {
    case 'human_assist_active':
      return 'Human assist is active: the browser is under user control. Commands are rejected until takeover is released.';
    case 'origin_not_approved':
      return (
        `Origin not approved: ${d.origin ?? 'unknown'}. ` +
        'A human must approve this origin in the Browser Bridge side panel ' +
        '(click the Browser Bridge icon in the browser toolbar to open the panel; choose Session or Always), ' +
        'then the command can be retried. ' +
        'Do not bypass the gate with WebFetch or other tools — accessing this origin requires approval.'
      );
    case 'origin_ignored':
      // A dismissal, not a refusal. The human closed the card, which is a
      // different thing from denying the origin: it lasts for this command at
      // this permission mode and ends with the session. So the text tells the
      // agent to stop, and — because "not right now" is not "never" — hands
      // the decision back to the human instead of quietly retiring the origin.
      // The "go get it approved, then retry" half of the unapproved text above
      // would instruct the agent to do precisely what the human closed.
      return (
        `Origin ignored by the human: ${d.origin ?? 'unknown'}. ` +
        'A human saw this request in the Browser Bridge side panel and closed it without answering. ' +
        'That means "not right now", not a refusal: the origin can still be approved there, and the ' +
        'dismissal ends with the browser session. ' +
        'Do not retry this command on this origin. If it blocks your task, say so and let the human decide. ' +
        'Do not bypass the gate with WebFetch or other tools — this origin still requires the human.'
      );
    case 'origin_denied':
      return (
        `Origin was denied by the user: ${d.origin ?? 'unknown'}. ` +
        'Only the user can reverse this: open the Browser Bridge side panel ' +
        '(click the Browser Bridge icon in the browser toolbar) and remove the denial, ' +
        'then retry. Do not bypass the gate with WebFetch or other tools.'
      );
    case 'origin_blocked':
      return `Blocked: ${d.origin ?? 'this target'} must not be accessed by the agent (${d.detail ?? 'blocked'}).`;
    case 'action_out_of_scope':
      return (
        `Out of the agent's working scope (${d.detail ?? d.capability ?? 'restricted action'}). ` +
        'A human must approve this action once in the Browser Bridge side panel ' +
        '(click the Browser Bridge icon in the browser toolbar), then the command can be retried.'
      );
    case 'approval_required':
      return (
        `Approval required: ${d.detail ?? 'this action always needs a one-time approval'}. ` +
        'A human must approve it once in the Browser Bridge side panel ' +
        '(click the Browser Bridge icon in the browser toolbar), then the command can be retried.'
      );
    case 'unknown_command':
      return `Command "${d.command}" is not recognized by the installed Browser Bridge policy. Update the extension and the local proxy to matching versions, then retry.`;
  }
}

// Stable per-denial key (reason|origin|command). Lives in the shared
// package so both the extension (React keys in ApprovalsPanel, target
// resolution in handleDenialAction) and any other consumer can reuse the
// same definition without each one inventing its own.
export function denialKey(denial: Denial): string {
  return `${denial.reason}|${denial.origin ?? ''}|${denial.command}`;
}

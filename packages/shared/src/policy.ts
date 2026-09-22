import type { CommandType } from './types';

export type DenyReason =
  | 'human_assist_active'
  | 'origin_not_approved'
  | 'origin_denied'
  | 'origin_blocked'
  | 'action_out_of_scope'
  | 'approval_required';

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

export interface Denial {
  reason: DenyReason;
  command: CommandType;
  origin?: string;
  capability?: GrantCapability;
  detail?: string;
}

export type PolicyDecision =
  | { allow: true; consume?: Grant[] }
  | { allow: false; denial: Denial };

export interface PolicyContext {
  takeover: boolean;
  // Origin of the target URL (navigate/tab:new) or of the tab the command
  // would act on. null means a protected or non-http(s) context — such
  // commands are hard-denied with 'origin_blocked', no approval path.
  origin: string | null;
  originState?: 'approved' | 'denied';
  blocklistHit?: boolean;
  isAgentTab?: boolean;
  isVisibleApprovedTab?: boolean;
  sensitiveField?: boolean;
  submit?: boolean;
  tabId?: number;
  grants?: Grant[];
  now?: number;
}

const READONLY_COMMANDS = new Set<CommandType>(['tab:list', 'pageinfo']);

// Commands that act on or read page content; denied outright in protected
// contexts and on blocklist hits, before any other rule.
const PAGE_CONTEXT_COMMANDS = new Set<CommandType>([
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
]);

// Commands that additionally require an approved origin to run.
const ORIGIN_GATE_COMMANDS = new Set<CommandType>([
  'navigate',
  'tab:new',
  'click',
  'type',
  'select',
  'scroll',
  'hover',
  'gettext',
  'gethtml',
]);

function denial(
  command: CommandType,
  reason: DenyReason,
  ctx: PolicyContext,
  capability?: GrantCapability,
  detail?: string,
): PolicyDecision {
  return {
    allow: false,
    denial: {
      reason,
      command,
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

export function evaluatePolicy(
  command: CommandType,
  ctx: PolicyContext,
): PolicyDecision {
  if (ctx.takeover) {
    return denial(command, 'human_assist_active', ctx);
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

  if (ORIGIN_GATE_COMMANDS.has(command)) {
    if (ctx.originState === 'denied') {
      return denial(command, 'origin_denied', ctx);
    }
    if (ctx.originState !== 'approved') {
      const grant = findGrant(ctx, now, 'origin');
      if (grant) return { allow: true, consume: [grant] };
      return denial(command, 'origin_not_approved', ctx, 'origin');
    }
  }

  if (command === 'type') {
    const consume: Grant[] = [];
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

  // Everything else — goBack, goForward, refresh, wait:element, wait:navigation,
  // tab:list, pageinfo, tab:switch — passes once takeover and the protected
  // context checks above are cleared.
  return { allow: true };
}

export function humanDenialMessage(d: Denial): string {
  switch (d.reason) {
    case 'human_assist_active':
      return 'Human assist is active: the browser is under user control. Commands are rejected until takeover is released.';
    case 'origin_not_approved':
      return (
        `Origin not approved: ${d.origin ?? 'unknown'}. ` +
        'A human must approve this origin in the Browser Bridge extension popup ' +
        '(click the Browser Bridge icon in the browser toolbar; choose Session or Always), ' +
        'then the command can be retried. ' +
        'Do not bypass the gate with WebFetch or other tools — accessing this origin requires approval.'
      );
    case 'origin_denied':
      return (
        `Origin was denied by the user: ${d.origin ?? 'unknown'}. ` +
        'Only the user can reverse this: open the Browser Bridge extension popup ' +
        '(click the Browser Bridge icon in the browser toolbar) and remove the denial, ' +
        'then retry. Do not bypass the gate with WebFetch or other tools.'
      );
    case 'origin_blocked':
      return `Blocked: ${d.origin ?? 'this target'} must not be accessed by the agent (${d.detail ?? 'blocked'}).`;
    case 'action_out_of_scope':
      return (
        `Out of the agent's working scope (${d.detail ?? d.capability ?? 'restricted action'}). ` +
        'A human must approve this action once in the Browser Bridge extension popup ' +
        '(click the Browser Bridge icon in the browser toolbar), then the command can be retried.'
      );
    case 'approval_required':
      return (
        `Approval required: ${d.detail ?? 'this action always needs a one-time approval'}. ` +
        'A human must approve it once in the Browser Bridge extension popup ' +
        '(click the Browser Bridge icon in the browser toolbar), then the command can be retried.'
      );
  }
}

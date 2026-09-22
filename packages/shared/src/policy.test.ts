import { describe, expect, it } from 'bun:test';
import {
  blocklistHit,
  type DenyReason,
  evaluatePolicy,
  type Grant,
  humanDenialMessage,
  originOf,
  type PolicyContext,
} from './index';
import type { CommandType } from './types';

const NOW = 1_700_000_000_000;
const HOUR = 60 * 60 * 1000;

function ctx(overrides: Partial<PolicyContext> = {}): PolicyContext {
  return {
    takeover: false,
    origin: 'https://example.com',
    originState: 'approved',
    now: NOW,
    ...overrides,
  };
}

function grant(overrides: Partial<Grant> = {}): Grant {
  return {
    capability: 'origin',
    origin: 'https://example.com',
    expiresAt: NOW + HOUR,
    singleUse: true,
    ...overrides,
  };
}

function expectDeny(
  decision: ReturnType<typeof evaluatePolicy>,
  reason: DenyReason,
) {
  expect(decision.allow).toBe(false);
  if (!decision.allow) expect(decision.denial.reason).toBe(reason);
}

describe('originOf', () => {
  it('returns origin for http and https URLs', () => {
    expect(originOf('https://a.b/example?x=1')).toBe('https://a.b');
    expect(originOf('http://a.b:8080/path')).toBe('http://a.b:8080');
  });

  it('returns null for protected or non-http(s) schemes', () => {
    for (const url of [
      'chrome://settings',
      'chrome-extension://abc/page.html',
      'chrome-devtools://x',
      'edge://settings',
      'about:blank',
      'file:///etc/passwd',
      'data:text/html,<h1>x</h1>',
      'javascript:alert(1)',
      'view-source:https://example.com',
      'brave://settings',
    ]) {
      expect(originOf(url)).toBeNull();
    }
  });

  it('returns null for unparseable URLs', () => {
    expect(originOf('not a url')).toBeNull();
    expect(originOf(undefined)).toBeNull();
  });
});

describe('takeover', () => {
  it('rejects every command while takeover is active', () => {
    const commands: CommandType[] = [
      'tab:list',
      'pageinfo',
      'snapshot',
      'goBack',
      'refresh',
      'navigate',
      'tab:new',
      'click',
      'type',
      'gettext',
      'screenshot',
      'tab:close',
      'tab:switch',
      'wait:element',
      'wait:navigation',
    ];
    for (const command of commands) {
      expectDeny(
        evaluatePolicy(command, ctx({ takeover: true })),
        'human_assist_active',
      );
    }
  });
});

describe('protected context and blocklist', () => {
  const pageCommands: CommandType[] = [
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
  ];

  it('hard-denies page commands in protected (null origin) contexts', () => {
    for (const command of pageCommands) {
      const decision = evaluatePolicy(command, ctx({ origin: null }));
      expectDeny(decision, 'origin_blocked');
      if (!decision.allow) {
        expect(decision.denial.detail).toContain('protected');
      }
    }
  });

  it('hard-denies page commands on blocklist hits, even if origin approved', () => {
    const decision = evaluatePolicy(
      'click',
      ctx({
        origin: 'https://chromewebstore.google.com',
        originState: 'approved',
        blocklistHit: true,
      }),
    );
    expectDeny(decision, 'origin_blocked');
  });

  it('blocklist denial has no grant path', () => {
    const decision = evaluatePolicy(
      'navigate',
      ctx({
        origin: 'https://chromewebstore.google.com',
        blocklistHit: true,
        grants: [grant()],
      }),
    );
    expectDeny(decision, 'origin_blocked');
  });

  it('does not deny tab-management commands on protected tabs', () => {
    expect(evaluatePolicy('tab:switch', ctx({ origin: null })).allow).toBe(
      true,
    );
  });
});

describe('read-only commands', () => {
  it('allows tab:list and pageinfo anywhere', () => {
    expect(evaluatePolicy('tab:list', ctx({ origin: null })).allow).toBe(true);
    expect(evaluatePolicy('pageinfo', ctx({ origin: null })).allow).toBe(true);
  });
});

describe('origin gate', () => {
  it('allows gated commands on approved origins', () => {
    for (const command of [
      'navigate',
      'click',
      'type',
      'gettext',
      'snapshot',
      'wait:element',
    ] as const) {
      expect(evaluatePolicy(command, ctx()).allow).toBe(true);
    }
  });

  it('denies page-content reads (snapshot, wait:element, gettext, gethtml) on unapproved origins', () => {
    for (const command of [
      'snapshot',
      'wait:element',
      'gettext',
      'gethtml',
    ] as const) {
      expectDeny(
        evaluatePolicy(
          command,
          ctx({ origin: 'https://unapproved.site', originState: undefined }),
        ),
        'origin_not_approved',
      );
    }
  });

  it('denies with origin_not_approved on unknown origins', () => {
    const decision = evaluatePolicy(
      'click',
      ctx({ origin: 'https://new.site', originState: undefined }),
    );
    expectDeny(decision, 'origin_not_approved');
    if (!decision.allow)
      expect(decision.denial.origin).toBe('https://new.site');
  });

  it('denies navigation to unapproved origins', () => {
    expectDeny(
      evaluatePolicy(
        'navigate',
        ctx({ origin: 'https://new.site', originState: undefined }),
      ),
      'origin_not_approved',
    );
    expectDeny(
      evaluatePolicy(
        'tab:new',
        ctx({ origin: 'https://new.site', originState: undefined }),
      ),
      'origin_not_approved',
    );
  });

  it('denies with origin_denied on denied origins', () => {
    expectDeny(
      evaluatePolicy('click', ctx({ originState: 'denied' })),
      'origin_denied',
    );
  });

  it('origin grant bypasses origin_not_approved and is consumed', () => {
    const g = grant();
    const decision = evaluatePolicy(
      'click',
      ctx({ originState: undefined, grants: [g] }),
    );
    expect(decision.allow).toBe(true);
    if (decision.allow) expect(decision.consume).toEqual([g]);
  });

  it('origin grant does not override an explicit denial', () => {
    expectDeny(
      evaluatePolicy(
        'click',
        ctx({ originState: 'denied', grants: [grant()] }),
      ),
      'origin_denied',
    );
  });

  it('expired grants are ignored', () => {
    expectDeny(
      evaluatePolicy(
        'click',
        ctx({
          originState: undefined,
          grants: [grant({ expiresAt: NOW - 1 })],
        }),
      ),
      'origin_not_approved',
    );
  });

  it('grants scoped to another origin do not apply', () => {
    expectDeny(
      evaluatePolicy(
        'click',
        ctx({
          origin: 'https://other.site',
          originState: undefined,
          grants: [grant({ origin: 'https://example.com' })],
        }),
      ),
      'origin_not_approved',
    );
  });
});

describe('type: submit and sensitive fields', () => {
  it('allows plain typing on approved origins without preflight flags', () => {
    const decision = evaluatePolicy('type', ctx());
    expect(decision.allow).toBe(true);
    if (decision.allow) expect(decision.consume).toBeUndefined();
  });

  it('requires a submit grant for submit=true', () => {
    expectDeny(
      evaluatePolicy('type', ctx({ submit: true })),
      'approval_required',
    );
  });

  it('requires a sensitive-field grant for password fields', () => {
    expectDeny(
      evaluatePolicy('type', ctx({ sensitiveField: true })),
      'approval_required',
    );
  });

  it('an origin grant does not short-circuit the sensitive-field check', () => {
    // Regression: the origin gate used to return early with the origin grant,
    // letting `type` into a password field through with only origin approval.
    const decision = evaluatePolicy(
      'type',
      ctx({
        originState: undefined,
        sensitiveField: true,
        grants: [grant()],
      }),
    );
    expectDeny(decision, 'approval_required');
    if (!decision.allow)
      expect(decision.denial.capability).toBe('sensitive-field');
  });

  it('consumes the origin grant together with submit/sensitive grants', () => {
    const originGrant = grant();
    const g1 = grant({ capability: 'submit' });
    const g2 = grant({ capability: 'sensitive-field' });
    const decision = evaluatePolicy(
      'type',
      ctx({
        originState: undefined,
        submit: true,
        sensitiveField: true,
        grants: [originGrant, g1, g2],
      }),
    );
    expect(decision.allow).toBe(true);
    if (decision.allow) expect(decision.consume).toEqual([originGrant, g1, g2]);
  });

  it('consumes both grants when submit and sensitive apply together', () => {
    const g1 = grant({ capability: 'submit' });
    const g2 = grant({ capability: 'sensitive-field' });
    const decision = evaluatePolicy(
      'type',
      ctx({ submit: true, sensitiveField: true, grants: [g1, g2] }),
    );
    expect(decision.allow).toBe(true);
    if (decision.allow) expect(decision.consume).toEqual([g1, g2]);
  });
});

describe('screenshot', () => {
  it('allows when the target tab is the visible approved tab', () => {
    expect(
      evaluatePolicy('screenshot', ctx({ isVisibleApprovedTab: true })).allow,
    ).toBe(true);
  });

  it('denies out-of-scope screenshots with the screenshot capability', () => {
    const decision = evaluatePolicy(
      'screenshot',
      ctx({ isVisibleApprovedTab: false }),
    );
    expectDeny(decision, 'action_out_of_scope');
    if (!decision.allow) expect(decision.denial.capability).toBe('screenshot');
  });

  it('screenshot grant allows a one-time bypass and is consumed', () => {
    const g = grant({ capability: 'screenshot' });
    const decision = evaluatePolicy(
      'screenshot',
      ctx({ isVisibleApprovedTab: false, grants: [g] }),
    );
    expect(decision.allow).toBe(true);
    if (decision.allow) expect(decision.consume).toEqual([g]);
  });
});

describe('tab:close', () => {
  it('allows closing agent-opened tabs', () => {
    expect(evaluatePolicy('tab:close', ctx({ isAgentTab: true })).allow).toBe(
      true,
    );
  });

  it('denies closing user tabs with the tab-close capability', () => {
    const decision = evaluatePolicy('tab:close', ctx({ isAgentTab: false }));
    expectDeny(decision, 'action_out_of_scope');
    if (!decision.allow) expect(decision.denial.capability).toBe('tab-close');
  });

  it('tab-close grant allows a one-time bypass', () => {
    const g = grant({ capability: 'tab-close' });
    const decision = evaluatePolicy(
      'tab:close',
      ctx({ isAgentTab: false, grants: [g] }),
    );
    expect(decision.allow).toBe(true);
  });
});

describe('unrestricted commands', () => {
  it('allows goBack/goForward/refresh/wait:navigation/tab:switch on normal origins', () => {
    for (const command of [
      'goBack',
      'goForward',
      'refresh',
      'wait:navigation',
      'tab:switch',
    ] as const) {
      expect(evaluatePolicy(command, ctx()).allow).toBe(true);
    }
  });
});

describe('fail-closed policy', () => {
  it('denies commands it does not recognize instead of allowing them', () => {
    const decision = evaluatePolicy('wipeHistory' as CommandType, ctx());
    expectDeny(decision, 'unknown_command');
  });

  it('denies unknown commands even on approved origins with grants', () => {
    const decision = evaluatePolicy(
      'eval' as CommandType,
      ctx({ grants: [grant()], sensitiveField: true, submit: true }),
    );
    expectDeny(decision, 'unknown_command');
  });
});

describe('blocklistHit', () => {
  it('matches hostnames and subdomains', () => {
    expect(blocklistHit('https://chromewebstore.google.com')).toBe(true);
    expect(blocklistHit('https://sub.chromewebstore.google.com')).toBe(true);
    expect(blocklistHit('https://example.com')).toBe(false);
  });

  it('matches extra entries, including full origins', () => {
    expect(blocklistHit('https://evil.example', ['evil.example'])).toBe(true);
    expect(blocklistHit('https://evil.example', ['https://evil.example'])).toBe(
      true,
    );
    expect(
      blocklistHit('https://other.example', ['https://evil.example']),
    ).toBe(false);
  });

  it('ignores null origins (protected denial is a separate rule)', () => {
    expect(blocklistHit(null)).toBe(false);
  });
});

describe('humanDenialMessage', () => {
  it('produces non-empty text for every reason', () => {
    const reasons = [
      'human_assist_active',
      'origin_not_approved',
      'origin_denied',
      'origin_blocked',
      'action_out_of_scope',
      'approval_required',
      'unknown_command',
    ] as const;
    for (const reason of reasons) {
      const message = humanDenialMessage({
        reason,
        command: 'click',
        origin: 'https://example.com',
      });
      expect(message.length).toBeGreaterThan(10);
    }
  });

  it('tells the agent a human must approve the origin in the extension popup', () => {
    const message = humanDenialMessage({
      reason: 'origin_not_approved',
      command: 'navigate',
      origin: 'https://example.com',
    });
    expect(message).toContain('https://example.com');
    expect(message).toContain('Browser Bridge extension popup');
    expect(message).toContain('WebFetch');
  });
});

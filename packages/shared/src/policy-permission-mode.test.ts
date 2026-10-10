import { describe, expect, it } from 'bun:test';
import {
  type DenyReason,
  evaluatePolicy,
  type Grant,
  type PermissionMode,
  type PolicyContext,
} from './index';
import type { CommandType } from './types';

const NOW = 1_700_000_000_000;
const HOUR = 60 * 60 * 1000;
const MODES: PermissionMode[] = ['strict', 'standard', 'relaxed'];

// An origin the human has neither approved nor denied: the state a gated
// command has to ask about.
const UNAPPROVED_ORIGIN = {
  origin: 'https://unapproved.site',
  originState: undefined,
};

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

describe('permission mode: safety levels', () => {
  const observerCommands: CommandType[] = [
    'tab:list',
    'pageinfo',
    'tab:switch',
    'wait:element',
    'wait:navigation',
    'goBack',
    'goForward',
    'refresh',
  ];
  const readCommands: CommandType[] = [
    'navigate',
    'snapshot',
    'gettext',
    'gethtml',
  ];
  const writeCommands: CommandType[] = [
    'click',
    'type',
    'select',
    'scroll',
    'hover',
  ];

  function askFor(command: CommandType, mode?: PermissionMode) {
    return evaluatePolicy(
      command,
      ctx({
        ...UNAPPROVED_ORIGIN,
        ...(mode ? { permissionMode: mode } : {}),
      }),
    );
  }

  it('never gates observer commands on an unapproved origin, in any mode', () => {
    for (const command of observerCommands) {
      for (const mode of MODES) {
        expect(askFor(command, mode).allow).toBe(true);
      }
      expect(askFor(command).allow).toBe(true);
    }
  });

  it('gates read commands only in strict', () => {
    for (const command of readCommands) {
      expectDeny(askFor(command, 'strict'), 'origin_not_approved');
      expect(askFor(command, 'standard').allow).toBe(true);
      expect(askFor(command, 'relaxed').allow).toBe(true);
    }
  });

  it('gates a url-carrying tab:new only in strict', () => {
    expectDeny(askFor('tab:new', 'strict'), 'origin_not_approved');
    expect(askFor('tab:new', 'standard').allow).toBe(true);
    expect(askFor('tab:new', 'relaxed').allow).toBe(true);
  });

  it('gates write commands in strict and standard, not in relaxed', () => {
    for (const command of writeCommands) {
      expectDeny(askFor(command, 'strict'), 'origin_not_approved');
      expectDeny(askFor(command, 'standard'), 'origin_not_approved');
      expect(askFor(command, 'relaxed').allow).toBe(true);
    }
  });

  it('lets an origin grant stand in the modes that gate it', () => {
    const g = grant({ origin: 'https://unapproved.site' });
    for (const mode of ['strict', 'standard'] as const) {
      const decision = evaluatePolicy(
        'click',
        ctx({
          ...UNAPPROVED_ORIGIN,
          permissionMode: mode,
          grants: [g],
        }),
      );
      expect(decision.allow).toBe(true);
      if (decision.allow) expect(decision.consume).toEqual([g]);
    }
  });

  it('treats an omitted permissionMode as strict', () => {
    for (const command of [...readCommands, ...writeCommands]) {
      expectDeny(askFor(command), 'origin_not_approved');
    }
    const explicit = evaluatePolicy(
      'click',
      ctx({ ...UNAPPROVED_ORIGIN, permissionMode: 'strict' }),
    );
    const omitted = evaluatePolicy('click', ctx(UNAPPROVED_ORIGIN));
    expect(omitted).toEqual(explicit);
  });

  it('allows on approved origins in every mode, at every level', () => {
    for (const command of [
      ...observerCommands,
      ...readCommands,
      ...writeCommands,
      'screenshot',
      'tab:close',
    ] as CommandType[]) {
      for (const mode of MODES) {
        expect(
          evaluatePolicy(
            command,
            ctx({
              permissionMode: mode,
              isVisibleApprovedTab: true,
              isAgentTab: true,
            }),
          ).allow,
        ).toBe(true);
      }
    }
  });
});

describe('permission mode: hard rails', () => {
  it('denies read, write and wait:element on a denied origin in every mode', () => {
    for (const command of [
      'navigate',
      'tab:new',
      'snapshot',
      'gettext',
      'gethtml',
      'click',
      'type',
      'select',
      'scroll',
      'hover',
      'wait:element',
    ] as const) {
      for (const mode of MODES) {
        expectDeny(
          evaluatePolicy(
            command,
            ctx({
              permissionMode: mode,
              origin: 'https://denied.site',
              originState: 'denied',
            }),
          ),
          'origin_denied',
        );
      }
    }
  });

  it('denies an origin grant cannot override the denied-origin rail', () => {
    for (const mode of MODES) {
      expectDeny(
        evaluatePolicy(
          'click',
          ctx({
            permissionMode: mode,
            originState: 'denied',
            grants: [grant()],
          }),
        ),
        'origin_denied',
      );
    }
  });

  it('denies protected and blocklist origins in every mode', () => {
    for (const mode of MODES) {
      for (const command of [
        'navigate',
        'snapshot',
        'click',
        'type',
        'wait:element',
        'screenshot',
      ] as const) {
        expectDeny(
          evaluatePolicy(command, ctx({ permissionMode: mode, origin: null })),
          'origin_blocked',
        );
        expectDeny(
          evaluatePolicy(
            command,
            ctx({
              permissionMode: mode,
              origin: 'https://chromewebstore.google.com',
              blocklistHit: true,
            }),
          ),
          'origin_blocked',
        );
      }
    }
  });

  it('keeps Takeover denying every command in every mode', () => {
    for (const mode of MODES) {
      for (const command of [
        'navigate',
        'click',
        'type',
        'wait:element',
        'screenshot',
        'tab:close',
      ] as const) {
        expectDeny(
          evaluatePolicy(
            command,
            ctx({ permissionMode: mode, takeover: true }),
          ),
          'human_assist_active',
        );
      }
    }
  });

  it('allows a blank tab:new in every mode outside takeover', () => {
    for (const mode of MODES) {
      expect(
        evaluatePolicy(
          'tab:new',
          ctx({ permissionMode: mode, origin: null, blankNewTab: true }),
        ),
      ).toEqual({ allow: true });
    }
  });

  it('fails closed on unknown commands in every mode', () => {
    for (const mode of MODES) {
      expectDeny(
        evaluatePolicy(
          'wipeHistory' as CommandType,
          ctx({ permissionMode: mode }),
        ),
        'unknown_command',
      );
    }
  });
});

describe('permission mode: sensitive bindings', () => {
  it('requires a submit grant in every mode', () => {
    for (const mode of MODES) {
      expectDeny(
        evaluatePolicy('type', ctx({ permissionMode: mode, submit: true })),
        'approval_required',
      );
    }
  });

  it('requires a sensitive-field grant in every mode, origin grant or not', () => {
    for (const mode of MODES) {
      expectDeny(
        evaluatePolicy(
          'type',
          ctx({
            permissionMode: mode,
            sensitiveField: true,
            grants: [grant()],
          }),
        ),
        'approval_required',
      );
    }
  });

  it('does not ask for an origin in relaxed, but still asks for submit', () => {
    // Relaxed drops the origin ask for a write command; the submit grant is
    // what carries it now, and it is still required.
    expectDeny(
      evaluatePolicy(
        'type',
        ctx({
          permissionMode: 'relaxed',
          ...UNAPPROVED_ORIGIN,
          submit: true,
        }),
      ),
      'approval_required',
    );
  });

  it('keeps the screenshot and tab:close bindings in every mode', () => {
    for (const mode of MODES) {
      expectDeny(
        evaluatePolicy(
          'screenshot',
          ctx({ permissionMode: mode, isVisibleApprovedTab: false }),
        ),
        'action_out_of_scope',
      );
      expectDeny(
        evaluatePolicy(
          'tab:close',
          ctx({ permissionMode: mode, isAgentTab: false }),
        ),
        'action_out_of_scope',
      );
      expect(
        evaluatePolicy(
          'screenshot',
          ctx({ permissionMode: mode, isVisibleApprovedTab: true }),
        ).allow,
      ).toBe(true);
      expect(
        evaluatePolicy(
          'tab:close',
          ctx({ permissionMode: mode, isAgentTab: true }),
        ).allow,
      ).toBe(true);
    }
  });
});

describe('permission mode: wait:element', () => {
  it('never asks for origin approval, in any mode or with no mode at all', () => {
    for (const mode of MODES) {
      expect(
        evaluatePolicy(
          'wait:element',
          ctx({ permissionMode: mode, ...UNAPPROVED_ORIGIN }),
        ).allow,
      ).toBe(true);
      expect(evaluatePolicy('wait:element', ctx(UNAPPROVED_ORIGIN)).allow).toBe(
        true,
      );
    }
  });

  it('still denies a denied origin in every mode', () => {
    for (const mode of MODES) {
      expectDeny(
        evaluatePolicy(
          'wait:element',
          ctx({ permissionMode: mode, originState: 'denied' }),
        ),
        'origin_denied',
      );
    }
  });
});

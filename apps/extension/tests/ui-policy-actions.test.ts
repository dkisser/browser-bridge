import { beforeEach, describe, expect, it } from 'bun:test';
import { applyPolicyOp, type PolicyOp } from '../src/policy-operations';
import {
  getPolicyState,
  normalizePolicyState,
  recordDenial,
  updatePolicyState,
} from '../src/policy-state';
import { handleDenialAction } from '../src/ui/policy-actions';

// In-memory chrome.storage.local stand-in. Mirrors the fixture used by
// tests/policy-state.test.ts so the two suites stay self-contained.
const store = new Map<string, unknown>();

(globalThis as Record<string, unknown>).chrome = {
  storage: {
    local: {
      get: async (key: string): Promise<Record<string, unknown>> =>
        store.has(key) ? { [key]: store.get(key) } : {},
      set: async (entries: Record<string, unknown>): Promise<void> => {
        for (const [key, value] of Object.entries(entries)) {
          store.set(key, value);
        }
      },
    },
  },
  // The side panel does not write policy state itself: it sends a `policy_op`
  // message and the service worker applies it inside its own write queue
  // (see src/policy-operations.ts). This mock stands in for that handler so
  // the test still exercises the real reducer end to end, including the
  // message shape the UI actually sends.
  runtime: {
    sendMessage: async (message: { type: string; op: PolicyOp }) => {
      if (message.type !== 'policy_op') return undefined;
      const current = normalizePolicyState(store.get('policyState'));
      const patch = applyPolicyOp(current, message.op);
      if (patch === null) {
        return { status: 'ok', data: current };
      }
      const next = { ...current, ...patch };
      store.set('policyState', next);
      return { status: 'ok', data: next };
    },
  },
  // The service worker refreshes the badge as part of the same operation;
  // the action API is irrelevant here, so stub every method it can call.
  action: {
    setBadgeBackgroundColor: async () => {},
    setBadgeText: async () => {},
    setTitle: async () => {},
  },
};

function clickDenial(
  origin: string,
  command: Parameters<typeof recordDenial>[0]['command'] = 'click',
): Parameters<typeof recordDenial>[0] {
  return {
    reason: 'origin_not_approved',
    command,
    origin,
  };
}

describe('handleDenialAction — stable key resolution', () => {
  beforeEach(() => {
    store.clear();
  });

  it('approves the denial the user actually clicked, not the one that ends up at the same index', async () => {
    // recordDenial prepends and dedupes, so seeding order produces
    // [B, A] — most-recent first. Capture B's stable key.
    const denialA = clickDenial('https://a.example');
    const denialB = clickDenial('https://b.example');
    await recordDenial(denialA);
    await recordDenial(denialB);
    const before = await getPolicyState();
    expect(before.recentDenials.map((d) => d.origin)).toEqual([
      'https://b.example',
      'https://a.example',
    ]);
    const keyB = `${denialB.reason}|${denialB.origin ?? ''}|${denialB.command}`;

    // The panel would render [B, A]. The user clicks 'Always' on B
    // (visible at index 0). Between render and click, the agent records
    // a new denial C, which prepends and shifts B from index 0 to index 1.
    await recordDenial(clickDenial('https://c.example'));
    const mid = await getPolicyState();
    expect(mid.recentDenials.map((d) => d.origin)).toEqual([
      'https://c.example',
      'https://b.example',
      'https://a.example',
    ]);

    // The click handler still has the key it captured for B — verify that
    // handleDenialAction resolves against B, not whatever now sits at the
    // original positional index (which would be C, the freshly recorded
    // denial the user did not click on).
    await handleDenialAction('approve-always', keyB);

    const after = await getPolicyState();
    expect(after.origins['https://b.example']).toBe('always');
    expect(after.origins['https://a.example']).toBeUndefined();
    expect(after.origins['https://c.example']).toBeUndefined();
    expect(after.recentDenials.map((d) => d.origin)).toEqual([
      'https://c.example',
      'https://a.example',
    ]);
  });

  it('dismisses the targeted denial without shifting siblings', async () => {
    const denialA = clickDenial('https://a.example', 'click');
    // 'submit' is a GrantCapability, not a CommandType — passing it here was
    // invisible until these tests were type-checked.
    const denialB = clickDenial('https://b.example', 'type');
    await recordDenial(denialA);
    await recordDenial(denialB);
    const keyA = `${denialA.reason}|${denialA.origin ?? ''}|${denialA.command}`;

    await handleDenialAction('dismiss', keyA);

    const after = await getPolicyState();
    expect(after.recentDenials.map((d) => d.origin)).toEqual([
      'https://b.example',
    ]);
  });

  it('is a no-op when the key no longer exists (already dismissed elsewhere)', async () => {
    await recordDenial(clickDenial('https://a.example'));
    const keyA = 'origin_not_approved|https://a.example|click';
    // Pretend another surface already cleared this denial between render
    // and click — the action must not throw and must not mutate state.
    await updatePolicyState((_state) => ({
      recentDenials: [],
    }));

    await expect(
      handleDenialAction('approve-always', keyA),
    ).resolves.toBeUndefined();

    const after = await getPolicyState();
    expect(after.origins).toEqual({});
    expect(after.recentDenials).toEqual([]);
  });

  it('approve-always is a no-op when the denial carries no origin', async () => {
    const denial: Parameters<typeof recordDenial>[0] = {
      reason: 'approval_required',
      command: 'type',
    };
    await recordDenial(denial);
    const key = `${denial.reason}|${denial.command}`;
    const before = await getPolicyState();

    await handleDenialAction('approve-always', key);

    const after = await getPolicyState();
    expect(after.origins).toEqual(before.origins);
    expect(after.deniedOrigins).toEqual(before.deniedOrigins);
    expect(after.recentDenials.length).toBe(before.recentDenials.length);
  });

  it('allow-once installs a singleUse grant scoped to the denial origin', async () => {
    await recordDenial(clickDenial('https://a.example'));
    const key = 'origin_not_approved|https://a.example|click';

    await handleDenialAction('allow-once', key);

    const after = await getPolicyState();
    expect(after.recentDenials).toEqual([]);
    expect(after.grants).toHaveLength(1);
    const grant = after.grants[0];
    expect(grant.singleUse).toBe(true);
    expect(grant.origin).toBe('https://a.example');
    expect(grant.expiresAt).toBeGreaterThan(Date.now());
  });
});

// Quick sanity check that the imports + exports wired by the rename still
// flow through the serialized write queue.
describe('handleDenialAction — interaction with policy state', () => {
  beforeEach(() => {
    store.clear();
  });

  it('dismissing a denial updates the badge path without throwing', async () => {
    await recordDenial(clickDenial('https://a.example'));
    const key = 'origin_not_approved|https://a.example|click';
    await expect(handleDenialAction('dismiss', key)).resolves.toBeUndefined();
    const after = await getPolicyState();
    expect(after.recentDenials).toEqual([]);
  });

  it('deny-origin records into deniedOrigins under the same stable key', async () => {
    await recordDenial(clickDenial('https://a.example'));
    const key = 'origin_not_approved|https://a.example|click';

    await handleDenialAction('deny-origin', key);

    const after = await getPolicyState();
    expect(after.deniedOrigins['https://a.example']).toBe('always');
    expect(after.recentDenials).toEqual([]);
  });
});

// origins / deniedOrigins must stay mutually exclusive — OriginsPanel
// renders both maps together and would show the same origin twice if a
// later action on the same origin did not clear the prior opposite entry.
describe('handleDenialAction — origin map hygiene', () => {
  beforeEach(() => {
    store.clear();
  });

  it('approve-always on an origin removes any prior deniedOrigins entry', async () => {
    // Seed: origin is currently in deniedOrigins.
    await updatePolicyState((state) => ({
      deniedOrigins: { ...state.deniedOrigins, 'https://a.example': 'always' },
    }));
    await recordDenial(clickDenial('https://a.example'));
    const key = 'origin_not_approved|https://a.example|click';

    await handleDenialAction('approve-always', key);

    const after = await getPolicyState();
    expect(after.origins['https://a.example']).toBe('always');
    expect(after.deniedOrigins['https://a.example']).toBeUndefined();
  });

  it('approve-session on an origin removes any prior deniedOrigins entry', async () => {
    await updatePolicyState((state) => ({
      deniedOrigins: { ...state.deniedOrigins, 'https://a.example': 'always' },
    }));
    await recordDenial(clickDenial('https://a.example'));
    const key = 'origin_not_approved|https://a.example|click';

    await handleDenialAction('approve-session', key);

    const after = await getPolicyState();
    expect(after.origins['https://a.example']).toBe('session');
    expect(after.deniedOrigins['https://a.example']).toBeUndefined();
  });

  it('deny-origin on an origin removes any prior origins entry', async () => {
    await updatePolicyState((state) => ({
      origins: { ...state.origins, 'https://a.example': 'session' },
    }));
    await recordDenial(clickDenial('https://a.example'));
    const key = 'origin_not_approved|https://a.example|click';

    await handleDenialAction('deny-origin', key);

    const after = await getPolicyState();
    expect(after.deniedOrigins['https://a.example']).toBe('always');
    expect(after.origins['https://a.example']).toBeUndefined();
  });

  it('leaves other origins untouched when clearing the conflict on one', async () => {
    await updatePolicyState((state) => ({
      origins: {
        ...state.origins,
        'https://a.example': 'always',
        'https://b.example': 'session',
      },
      deniedOrigins: { ...state.deniedOrigins, 'https://a.example': 'always' },
    }));
    await recordDenial(clickDenial('https://a.example'));
    const key = 'origin_not_approved|https://a.example|click';

    await handleDenialAction('approve-always', key);

    const after = await getPolicyState();
    expect(after.origins['https://a.example']).toBe('always');
    expect(after.origins['https://b.example']).toBe('session');
    expect(after.deniedOrigins['https://a.example']).toBeUndefined();
    expect(after.deniedOrigins['https://b.example']).toBeUndefined();
  });
});

describe('dismissed stance (ADR-0040)', () => {
  const ORIGIN = 'https://a.example';

  beforeEach(() => {
    store.clear();
  });

  it('records the stance keyed on origin, command and the mode it was given in', async () => {
    await updatePolicyState(() => ({ permissionMode: 'standard' }));
    await recordDenial(clickDenial(ORIGIN, 'click'));

    await handleDenialAction(
      'dismiss',
      'origin_not_approved|https://a.example|click',
    );

    const after = await getPolicyState();
    expect(after.ignoredOrigins[ORIGIN]).toEqual([
      { command: 'click', mode: 'standard' },
    ]);
    expect(after.recentDenials).toEqual([]);
  });

  it('keeps the stance scoped to the command that was closed', async () => {
    await updatePolicyState(() => ({ permissionMode: 'standard' }));
    await recordDenial(clickDenial(ORIGIN, 'tab:new'));

    await handleDenialAction(
      'dismiss',
      'origin_not_approved|https://a.example|tab:new',
    );

    const after = await getPolicyState();
    expect(after.ignoredOrigins[ORIGIN]).toEqual([
      { command: 'tab:new', mode: 'standard' },
    ]);
  });

  it('records nothing for a denial that is not about an origin', async () => {
    // Allow-once cards carry a dismiss button too, and a human closing one
    // of those has not declined to answer anything about an origin.
    await recordDenial({
      reason: 'approval_required',
      command: 'type',
      origin: ORIGIN,
      capability: 'submit',
    });

    await handleDenialAction(
      'dismiss',
      'approval_required|https://a.example|type',
    );

    const after = await getPolicyState();
    expect(after.recentDenials).toEqual([]);
    expect(after.ignoredOrigins[ORIGIN]).toBeUndefined();
  });

  it('drops the stance when the human grants the origin', async () => {
    await updatePolicyState(() => ({ permissionMode: 'standard' }));
    await recordDenial(clickDenial(ORIGIN));
    await handleDenialAction(
      'dismiss',
      'origin_not_approved|https://a.example|click',
    );
    expect((await getPolicyState()).ignoredOrigins[ORIGIN]).toBeDefined();

    // The agent retries, raises the card again, and the human approves.
    await recordDenial(clickDenial(ORIGIN));
    await handleDenialAction(
      'approve-always',
      'origin_not_approved|https://a.example|click',
    );

    const after = await getPolicyState();
    expect(after.ignoredOrigins[ORIGIN]).toBeUndefined();
    expect(after.origins[ORIGIN]).toBe('always');
  });

  it('drops the stance when the human denies the origin outright', async () => {
    await updatePolicyState(() => ({ permissionMode: 'standard' }));
    await recordDenial(clickDenial(ORIGIN));
    await handleDenialAction(
      'dismiss',
      'origin_not_approved|https://a.example|click',
    );

    await recordDenial(clickDenial(ORIGIN));
    await handleDenialAction(
      'deny-origin',
      'origin_not_approved|https://a.example|click',
    );

    const after = await getPolicyState();
    expect(after.ignoredOrigins[ORIGIN]).toBeUndefined();
    expect(after.deniedOrigins[ORIGIN]).toBe('always');
  });

  it('is idempotent across repeated dismissals of the same request', async () => {
    // The agent retries and raises the card again; closing it a second time
    // must not stack a second stance or widen one.
    await updatePolicyState(() => ({ permissionMode: 'standard' }));
    for (let i = 0; i < 2; i++) {
      await recordDenial(clickDenial(ORIGIN));
      await handleDenialAction(
        'dismiss',
        'origin_not_approved|https://a.example|click',
      );
    }

    const after = await getPolicyState();
    expect(Object.keys(after.ignoredOrigins)).toEqual([ORIGIN]);
    expect(after.ignoredOrigins[ORIGIN]).toEqual([
      { command: 'click', mode: 'standard' },
    ]);
  });

  it('does not touch other origins stances', async () => {
    await updatePolicyState(() => ({ permissionMode: 'standard' }));
    await recordDenial(clickDenial('https://b.example'));
    await handleDenialAction(
      'dismiss',
      'origin_not_approved|https://b.example|click',
    );

    await recordDenial(clickDenial(ORIGIN));
    await handleDenialAction(
      'dismiss',
      'origin_not_approved|https://a.example|click',
    );

    const after = await getPolicyState();
    expect(Object.keys(after.ignoredOrigins).sort()).toEqual([
      'https://a.example',
      'https://b.example',
    ]);
  });

  it('works on a profile stored before stances existed', async () => {
    // A pre-ADR-0040 profile has no ignoredOrigins key at all. The merge has
    // to supply the empty map: the gate reads this field on every command,
    // so a missing key read off undefined is a TypeError that takes the whole
    // gate down on the first command after the upgrade.
    store.set('policyState', { origins: {}, deniedOrigins: {} });

    expect((await getPolicyState()).ignoredOrigins).toEqual({});

    await recordDenial(clickDenial(ORIGIN));
    await handleDenialAction(
      'dismiss',
      'origin_not_approved|https://a.example|click',
    );

    // And the mode it records is the one this profile resolves to, which for
    // a pre-mode profile is the upgrade default rather than strict.
    expect((await getPolicyState()).ignoredOrigins[ORIGIN]).toEqual([
      { command: 'click', mode: 'standard' },
    ]);
  });
});

describe('dismissed stance — regressions for the ways it first shipped wrong', () => {
  const ORIGIN = 'https://a.example';

  beforeEach(() => {
    store.clear();
  });

  it('keeps a second dismissal from retiring the first', async () => {
    // One slot per origin made the second close overwrite the first, which is
    // exactly the widening ADR-0040 rejects: closing `click` must never decide
    // anything about `type` on the same origin.
    await updatePolicyState(() => ({ permissionMode: 'standard' }));
    await recordDenial(clickDenial(ORIGIN, 'click'));
    await handleDenialAction(
      'dismiss',
      'origin_not_approved|https://a.example|click',
    );

    // The agent retries with a different command at a stricter mode, is asked
    // again, and the human closes that one too.
    await recordDenial({ ...clickDenial(ORIGIN, 'type'), mode: 'strict' });
    await handleDenialAction(
      'dismiss',
      'origin_not_approved|https://a.example|type',
    );

    const after = await getPolicyState();
    expect(after.ignoredOrigins[ORIGIN]).toEqual([
      { command: 'click', mode: 'standard' },
      { command: 'type', mode: 'strict' },
    ]);
  });

  it('records the mode the request was raised under, not the mode at click time', async () => {
    // The card was raised in standard; the human switches to relaxed before
    // pressing the close. Keying on the mode at click time would produce a
    // stance that can never match — relaxed gates no writes — so the human's
    // action would be a silent no-op.
    await recordDenial({ ...clickDenial(ORIGIN), mode: 'standard' });
    await updatePolicyState(() => ({ permissionMode: 'relaxed' }));

    await handleDenialAction(
      'dismiss',
      'origin_not_approved|https://a.example|click',
    );

    expect((await getPolicyState()).ignoredOrigins[ORIGIN]).toEqual([
      { command: 'click', mode: 'standard' },
    ]);
  });

  it('keeps one card when a dismissed request is retried', async () => {
    // The retry comes back as origin_ignored and the earlier card as
    // origin_not_approved. denialKey includes the reason, so keying on it
    // alone would leave the human two cards for one request, one calling the
    // origin unapproved and the other calling it closed.
    await recordDenial({ ...clickDenial(ORIGIN), mode: 'standard' });
    await handleDenialAction(
      'dismiss',
      'origin_not_approved|https://a.example|click',
    );

    await recordDenial({
      reason: 'origin_ignored',
      command: 'click',
      origin: ORIGIN,
      mode: 'standard',
    });

    const after = await getPolicyState();
    expect(after.recentDenials).toHaveLength(1);
    expect(after.recentDenials[0].reason).toBe('origin_ignored');
  });
});

describe('allow-once is not an answer about the origin (ADR-0040)', () => {
  const ORIGIN = 'https://a.example';

  beforeEach(() => {
    store.clear();
  });

  it('leaves a dismissal standing when the human allows one action instead', async () => {
    // The human closed `click` on this origin. The agent then asked for a
    // screenshot, which is a different question — scoped to a capability, not
    // to the origin — and the human allowed it once. That answer is not about
    // click, so it must not retire the click dismissal: doing so is exactly
    // the widening withStance exists to prevent, reached from the other
    // direction.
    await updatePolicyState(() => ({ permissionMode: 'standard' }));
    await recordDenial({ ...clickDenial(ORIGIN), mode: 'standard' });
    await handleDenialAction(
      'dismiss',
      'origin_not_approved|https://a.example|click',
    );

    await recordDenial({
      reason: 'action_out_of_scope',
      command: 'screenshot',
      origin: ORIGIN,
      capability: 'screenshot',
    });
    await handleDenialAction(
      'allow-once',
      'action_out_of_scope|https://a.example|screenshot',
    );

    const after = await getPolicyState();
    expect(after.ignoredOrigins[ORIGIN]).toEqual([
      { command: 'click', mode: 'standard' },
    ]);
    // The one-shot grant was still written — that is what was asked for.
    expect(after.grants).toHaveLength(1);
  });
});

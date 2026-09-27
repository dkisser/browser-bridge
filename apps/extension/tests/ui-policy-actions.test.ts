import { beforeEach, describe, expect, it } from 'bun:test';
import {
  decideWithState,
  getPolicyState,
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
  // handleDenialAction -> updateBadge touches chrome.action. The action API
  // is irrelevant to these tests; stub every method it can call.
  action: {
    setBadgeBackgroundColor: async () => {},
    setBadgeText: async () => {},
    setTitle: async () => {},
  },
};

function clickDenial(
  origin: string,
  command = 'click',
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
    const denialA = clickDenial('https://a.example', 'type');
    const denialB = clickDenial('https://b.example', 'submit');
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
// flow through decideWithState — the existing serialization suite already
// covers decideWithState, so this just guards against accidental breakage.
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

// Keep decideWithState referenced so biome / tsc don't flag the import as
// unused if a future edit reorders this file.
void decideWithState;

import { beforeEach, describe, expect, it } from 'bun:test';
import { evaluatePolicy } from '@browser-bridge/shared';
import { applyPolicyOp } from '../src/policy-operations';
import {
  getPolicyState,
  normalizePolicyState,
  persistPermissionModeOnce,
} from '../src/policy-state';

// In-memory chrome.storage.local stand-in, mirroring policy-state.test.ts.
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
};

const STORAGE_KEY = 'policyState';

describe('Permission mode defaults (ADR-0038)', () => {
  beforeEach(() => {
    store.clear();
  });

  it('a fresh install — nothing persisted at all — resolves to strict', async () => {
    expect(store.has(STORAGE_KEY)).toBe(false);
    const state = await getPolicyState();
    expect(state.permissionMode).toBe('strict');
  });

  it('an upgrade — policy state present, no mode — resolves to standard', () => {
    // What a pre-ADR-0038 profile looks like: origins approved, no mode key.
    const stored = { origins: { 'https://example.com': 'always' } };
    expect(normalizePolicyState(stored).permissionMode).toBe('standard');
    expect(normalizePolicyState({}).permissionMode).toBe('standard');
  });

  it('a stored mode wins over both defaults', () => {
    expect(
      normalizePolicyState({ permissionMode: 'relaxed' }).permissionMode,
    ).toBe('relaxed');
    expect(
      normalizePolicyState({ origins: {}, permissionMode: 'strict' })
        .permissionMode,
    ).toBe('strict');
  });

  it('an unrecognized stored value is not trusted', () => {
    // A future value written by a newer extension must not be handed to the
    // policy core as if it were one of the three this build knows.
    expect(
      normalizePolicyState({ permissionMode: 'yolo' }).permissionMode,
    ).toBe('standard');
    expect(normalizePolicyState({ permissionMode: 7 }).permissionMode).toBe(
      'standard',
    );
  });

  it('the resolved mode is pinned once, and stays put afterwards', async () => {
    await persistPermissionModeOnce();
    expect(store.has(STORAGE_KEY)).toBe(true);
    expect((await getPolicyState()).permissionMode).toBe('strict');

    // A later read must not re-derive the default from a state that now
    // exists: that is the whole failure this pinning prevents.
    await persistPermissionModeOnce();
    expect((await getPolicyState()).permissionMode).toBe('strict');
  });

  it('pinning does not overwrite a mode the user already chose', async () => {
    store.set(STORAGE_KEY, { permissionMode: 'relaxed' });
    await persistPermissionModeOnce();
    expect((await getPolicyState()).permissionMode).toBe('relaxed');
  });

  it('an upgraded profile is pinned to standard, not strict', async () => {
    store.set(STORAGE_KEY, { origins: { 'https://example.com': 'always' } });
    await persistPermissionModeOnce();
    expect((await getPolicyState()).permissionMode).toBe('standard');
  });
});

describe('Permission mode at the policy gate', () => {
  const ctx = {
    takeover: false,
    origin: 'https://unapproved.example',
    now: 1_700_000_000_000,
  };

  it('strict gates a content read; standard does not; relaxed does not', () => {
    const attempt = (permissionMode: 'strict' | 'standard' | 'relaxed') =>
      evaluatePolicy('snapshot', { ...ctx, permissionMode });

    expect(attempt('strict').allow).toBe(false);
    expect(attempt('standard').allow).toBe(true);
    expect(attempt('relaxed').allow).toBe(true);
  });

  it('standard still gates a write; only relaxed lets it through', () => {
    const attempt = (permissionMode: 'strict' | 'standard' | 'relaxed') =>
      evaluatePolicy('click', { ...ctx, permissionMode });

    expect(attempt('strict').allow).toBe(false);
    expect(attempt('standard').allow).toBe(false);
    expect(attempt('relaxed').allow).toBe(true);
  });

  it('a denied origin is refused in every mode', () => {
    for (const permissionMode of ['strict', 'standard', 'relaxed'] as const) {
      const decision = evaluatePolicy('snapshot', {
        ...ctx,
        permissionMode,
        originState: 'denied',
      });
      expect(decision.allow).toBe(false);
    }
  });
});

describe('set_permission_mode operation', () => {
  const base = normalizePolicyState({ permissionMode: 'standard' });

  it('writes the chosen mode', () => {
    const patch = applyPolicyOp(base, {
      op: 'set_permission_mode',
      mode: 'relaxed',
    });
    expect(patch).toEqual({ permissionMode: 'relaxed' });
  });

  it('is a no-op when the mode is already selected', () => {
    expect(
      applyPolicyOp(base, { op: 'set_permission_mode', mode: 'standard' }),
    ).toBeNull();
  });
});

import { beforeEach, describe, expect, it } from 'bun:test';
import { evaluatePolicy, type PolicyContext } from '@browser-bridge/shared';
import {
  decideWithState,
  getPolicyState,
  updatePolicyState,
} from '../src/policy-state';

// In-memory chrome.storage.local stand-in. Async by construction, so a
// non-serialized read-modify-write would lose updates under concurrency.
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

const NOW = 1_700_000_000_000;
const HOUR = 60 * 60 * 1000;

function clickOnUnapprovedOrigin(
  stateGrants: unknown,
): ReturnType<typeof evaluatePolicy> {
  const ctx: PolicyContext = {
    takeover: false,
    origin: 'https://unapproved.site',
    grants: stateGrants as PolicyContext['grants'],
    now: NOW,
  };
  return evaluatePolicy('click', ctx);
}

describe('policy-state serialization', () => {
  beforeEach(() => {
    store.clear();
  });

  it('a singleUse grant is consumed exactly once under concurrent decisions', async () => {
    await updatePolicyState(() => ({
      grants: [
        {
          capability: 'origin',
          origin: 'https://unapproved.site',
          expiresAt: NOW + HOUR,
          singleUse: true,
        },
      ],
    }));

    const outcomes = await Promise.all(
      Array.from({ length: 5 }, () =>
        decideWithState((state) => clickOnUnapprovedOrigin(state.grants)),
      ),
    );

    const allowed = outcomes.filter((outcome) => outcome.decision.allow);
    expect(allowed).toHaveLength(1);
    for (const outcome of outcomes) {
      if (!outcome.decision.allow) {
        expect(outcome.decision.denial.reason).toBe('origin_not_approved');
      }
    }
    const stored = await getPolicyState();
    expect(stored.grants).toHaveLength(0);
  });

  it('updatePolicyState applies concurrent read-modify-writes without lost updates', async () => {
    await Promise.all(
      Array.from({ length: 10 }, (_, i) =>
        updatePolicyState((state) => ({
          agentTabs: [...state.agentTabs, i + 1],
        })),
      ),
    );
    const stored = await getPolicyState();
    expect(stored.agentTabs).toHaveLength(10);
    expect([...stored.agentTabs].sort((a, b) => a - b)).toEqual(
      Array.from({ length: 10 }, (_, i) => i + 1),
    );
  });

  it('decideWithState does not consume anything when the decision is a denial', async () => {
    await updatePolicyState(() => ({
      grants: [
        {
          capability: 'origin',
          origin: 'https://unapproved.site',
          expiresAt: NOW + HOUR,
          singleUse: true,
        },
      ],
    }));
    const { decision } = await decideWithState((state) => {
      // 'type' into a sensitive field with no sensitive-field grant: denial
      // even though an origin grant exists.
      const ctx: PolicyContext = {
        takeover: false,
        origin: 'https://unapproved.site',
        grants: state.grants,
        sensitiveField: true,
        now: NOW,
      };
      return evaluatePolicy('type', ctx);
    });
    expect(decision.allow).toBe(false);
    const stored = await getPolicyState();
    expect(stored.grants).toHaveLength(1);
  });
});

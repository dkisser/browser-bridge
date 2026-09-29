import { beforeEach, describe, expect, it } from 'bun:test';
import { evaluatePolicy, type PolicyContext } from '@browser-bridge/shared';
import { applyPolicyOp } from '../src/policy-operations';
import {
  decideWithState,
  getPolicyState,
  recordDenial,
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

// A recentDenials entry is an approval request: the side panel renders it as a
// card with action buttons and the badge counts it as work waiting on the
// user. A takeover refusal is the user's own kill switch firing, and recording
// it manufactured requests nobody made — the agent's retries walked the list
// to its cap, and after the user released the browser the panel and the badge
// went on reporting refusals worded as though the human were still in control.
describe('takeover refusals are not approval requests', () => {
  beforeEach(() => {
    store.clear();
  });

  it('does not record a human_assist_active denial', async () => {
    await recordDenial({
      reason: 'human_assist_active',
      command: 'click',
      origin: 'https://example.com',
    });

    const state = await getPolicyState();
    expect(state.recentDenials).toEqual([]);
  });

  it('records every other denial unchanged', async () => {
    // The filter keys on the reason. A version that short-circuited the whole
    // write would pass the test above by recording nothing ever, and the
    // Approvals panel would go silent for the refusals it exists to show.
    for (const reason of [
      'origin_not_approved',
      'approval_required',
      'action_out_of_scope',
      'origin_denied',
    ] as const) {
      await recordDenial({
        reason,
        command: 'click',
        origin: 'https://a.test',
      });
    }

    const state = await getPolicyState();
    expect(state.recentDenials).toHaveLength(4);
    expect(state.recentDenials.map((d) => d.reason)).not.toContain(
      'human_assist_active',
    );
  });

  it('clears takeover refusals that were recorded before the rule existed', async () => {
    // State written by an older build still carries these, and nothing else
    // would ever remove one. This is the path that unsticks a user who
    // already engaged takeover once.
    await updatePolicyState(() => ({
      recentDenials: [
        {
          reason: 'origin_not_approved',
          command: 'click',
          origin: 'https://a.test',
        },
        {
          reason: 'human_assist_active',
          command: 'click',
          origin: 'https://a.test',
        },
        {
          reason: 'human_assist_active',
          command: 'type',
          origin: 'https://b.test',
        },
        {
          reason: 'approval_required',
          command: 'gettext',
          origin: 'https://b.test',
        },
      ],
    }));

    const after = applyPolicyOp(await getPolicyState(), {
      op: 'set_takeover',
      desired: false,
    });
    const next = { ...(await getPolicyState()), ...after };

    expect(next.takeover).toBe(false);
    expect(next.recentDenials.map((d) => d.reason)).toEqual([
      'origin_not_approved',
      'approval_required',
    ]);
  });

  it('leaves the list alone while takeover is being engaged', async () => {
    // Clearing on engage would quietly delete a list the user has not asked
    // about. The suppression already happens at record time; the release is
    // the only moment that owes them a clean slate.
    await updatePolicyState(() => ({
      recentDenials: [
        {
          reason: 'origin_not_approved',
          command: 'click',
          origin: 'https://a.test',
        },
        {
          reason: 'human_assist_active',
          command: 'click',
          origin: 'https://a.test',
        },
      ],
    }));

    const engaged = applyPolicyOp(await getPolicyState(), {
      op: 'set_takeover',
      desired: true,
    });

    expect(engaged).not.toBeNull();
    expect(engaged?.recentDenials).toBeUndefined();
  });
});

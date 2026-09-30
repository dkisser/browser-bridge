import { beforeEach, describe, expect, it } from 'bun:test';
import { requestPolicyOp } from '../src/ui/policy-ops';

// The client half of the single-writer migration: the side panel does not
// write policy state, it names an operation and the service worker applies
// it. That makes this file the whole argument's landing point, and it is the
// only piece of the migration with no behavioural coverage — the denial-action
// suite mocks sendMessage with its own reimplementation of the worker, so the
// real error handling here was executed by no test.
//
// The two failure branches matter most. "The worker answered with an error" is
// the ordinary path, and it is the one the panel now trusts: it shows the
// message and leaves the switch where the engine left it. "The worker never
// answered" is the path that shipped as a silent hang: a handler that forgot
// to call sendResponse leaves the port open until Chrome tears it down, which
// is minutes, not seconds.

let sendMessage: (message: unknown) => Promise<unknown> | undefined;
let sent: unknown[] = [];

// A whole PolicyState, so the client's return type is exercised honestly
// rather than satisfied with a cast.
const POLICY_STATE = {
  origins: {},
  deniedOrigins: {},
  grants: [],
  takeover: true,
  agentTabs: [],
  pairingToken: null,
  recentDenials: [],
  blockedOrigins: [],
  pendingDownloads: [],
  agentGroupAvailable: true,
};

(globalThis as Record<string, unknown>).chrome = {
  runtime: {
    sendMessage: (message: unknown): Promise<unknown> | undefined => {
      sent.push(message);
      return sendMessage(message);
    },
  },
};

beforeEach(() => {
  sent = [];
  sendMessage = async () => ({ status: 'ok', data: POLICY_STATE });
});

describe('requestPolicyOp', () => {
  it('sends the operation and returns the state the worker applied', async () => {
    const state = await requestPolicyOp({
      op: 'set_takeover',
      desired: true,
    });

    expect(sent).toEqual([
      { type: 'policy_op', op: { op: 'set_takeover', desired: true } },
    ]);
    expect(state).toEqual(POLICY_STATE);
  });

  it('throws the worker error message', async () => {
    sendMessage = async () => ({
      status: 'error',
      error: 'approve requires a denial with an origin',
    });

    await expect(
      requestPolicyOp({
        op: 'denial_action',
        action: 'approve-always',
        targetKey: 'k',
      }),
    ).rejects.toThrow('approve requires a denial with an origin');
  });

  it('throws when the worker never answers', async () => {
    // The service worker forgot to respond, or the port was torn down before
    // it could. Without this the caller's promise would simply never settle,
    // which is how a broken handler looks from the UI's side.
    sendMessage = () => undefined;

    await expect(
      requestPolicyOp({ op: 'set_takeover', desired: false }),
    ).rejects.toThrow(/did not respond/i);
  });

  it('propagates a rejected sendMessage', async () => {
    sendMessage = async () => {
      throw new Error('Receiving end does not exist.');
    };

    await expect(
      requestPolicyOp({ op: 'add_block', entry: 'evil.example' }),
    ).rejects.toThrow('Receiving end does not exist.');
  });

  it('rejects a response that is neither ok nor error', async () => {
    // A malformed or future-shaped reply must not read as a completed write.
    // The panel would otherwise go on as though the policy changed when the
    // worker never applied anything.
    sendMessage = async () => ({ status: 'something-else' });

    await expect(
      requestPolicyOp({ op: 'remove_block', entry: 'x' }),
    ).rejects.toThrow(/unexpected response/i);
  });
});

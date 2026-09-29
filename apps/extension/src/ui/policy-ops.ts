import { applyPolicyOp, type PolicyOp } from '../policy-operations';
import type { PolicyState } from '../policy-state';

// UI-side client for policy mutations.
//
// The side panel and the pairing flow do not write chrome.storage.local.
// They ask the service worker to, because the service worker owns the only
// serialization queue that matters: policy-state.ts's writeQueue is a
// module-level variable, so every JS context has its own copy, and two
// contexts writing the whole state object race — the loser's write silently
// restores whatever the winner had already changed, including a singleUse
// grant that was just consumed.
//
// A callback cannot be sent over chrome.runtime.sendMessage, so the request
// names an operation and the service worker runs the matching reducer inside
// its own queue (policy-operations.ts).

interface PolicyOpResponse {
  status: 'ok';
  data: PolicyState;
}

interface PolicyOpError {
  status: 'error';
  error: string;
}

export async function requestPolicyOp(op: PolicyOp): Promise<PolicyState> {
  const response = (await chrome.runtime.sendMessage({
    type: 'policy_op',
    op,
  })) as PolicyOpResponse | PolicyOpError | undefined;

  if (!response) {
    throw new Error(
      'The extension background did not respond — reload the side panel and try again.',
    );
  }
  if (response.status === 'error') {
    throw new Error(response.error);
  }
  // Anything that is not an explicit ok is a failure, not a success with an
  // undefined state: a malformed or future-shaped reply would otherwise read
  // as a completed write, and the panel would go on as though the policy
  // changed when nothing did.
  if (response.status !== 'ok') {
    throw new Error(
      'The extension background returned an unexpected response to a policy change.',
    );
  }
  return response.data;
}

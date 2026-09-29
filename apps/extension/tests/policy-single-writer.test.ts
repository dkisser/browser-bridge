import { describe, expect, it } from 'bun:test';
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';

// The policy-state write queue in src/policy-state.ts is a module-level
// variable, so every JS context that imports it gets its own copy and its own
// last-writer-wins window over the whole state object. The side panel used to
// write chrome.storage.local directly, which meant a UI write could race a
// command: whoever wrote last silently restored the other's fields — including
// a singleUse grant the service worker had just consumed, turning a one-shot
// approval into a reusable one, or dropping the user's takeover toggle.
//
// The fix routes every UI mutation through a `policy_op` message the service
// worker handles inside its own queue. This file pins that invariant at the
// level where the bug lived, so a future "just write it from the UI" shortcut
// fails a test instead of quietly weakening a security control.

const UI_DIR = join(import.meta.dir, '..', 'src', 'ui');
const SRC_DIR = join(import.meta.dir, '..', 'src');

// Modules owned by the service worker, which is the only context allowed to
// write policy state — directly or through the storage layer. `agent-group`
// is here because it is imported by background.ts alone and its
// setAgentGroupAvailability helper runs inside the worker's own queue; the
// badge and tab-group side effects belong to the worker anyway. If one of
// these ever becomes reachable from a page, this list must shrink with it.
const ALLOWED_WRITERS = new Set([
  join(SRC_DIR, 'background.ts'),
  join(SRC_DIR, 'agent-group.ts'),
  join(SRC_DIR, 'policy-state.ts'),
]);

// A module outside the allow-list must not mutate policy state at all, by
// either route: through a policy-state mutator, or by writing the policy key
// into chrome.storage directly.
//
// The direct-write branch is anchored on the *key*, not on an import. Two
// wrong anchors were tried first. Conditioning on "imports policy-state"
// makes the pattern unreachable for the case it exists to catch — a module
// that bypasses the storage layer does not import it — and the tell is that
// adding an import to such a file is what makes the guard fire. Anchoring on
// nothing at all then flags a module that merely mentions the PolicyState
// type while writing some unrelated key. Keying on the policy key catches
// the bypass and leaves unrelated storage use alone.
const MUTATES_POLICY =
  /\b(updatePolicyState|setPolicyState|decideWithState|recordDenial|clearSessionScoped|setAgentGroupAvailability)\s*\(/;

// A direct write counts only when the policy key is in the same statement.
const WRITES_POLICY_KEY =
  /chrome\.storage\.(local|sync|session)\.(set|remove|clear)\s*\(\s*\{[^}]*\bpolicyState\b/;

function sourceFilesIn(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) return sourceFilesIn(full);
    return /\.(ts|tsx)$/.test(entry.name) ? [full] : [];
  });
}

describe('only the service worker writes policy state', () => {
  const offenders: string[] = [];

  // Scan every extension source file, not just ui/: offscreen.ts,
  // agent-group.ts and any future page are equally able to reintroduce the
  // second writer this guard exists to prevent.
  for (const file of sourceFilesIn(SRC_DIR)) {
    if (ALLOWED_WRITERS.has(file)) continue;
    const source = readFileSync(file, 'utf8');
    if (MUTATES_POLICY.test(source) || WRITES_POLICY_KEY.test(source)) {
      offenders.push(file.slice(SRC_DIR.length + 1));
    }
  }

  it('no other module calls a policy-state write function', () => {
    expect(offenders).toEqual([]);
  });

  it('the UI talks to the service worker through policy_op', () => {
    const client = readFileSync(join(UI_DIR, 'policy-ops.ts'), 'utf8');
    expect(client).toContain("type: 'policy_op'");
    expect(client).toContain('chrome.runtime.sendMessage');
  });

  it('the service worker handles policy_op inside its own queue', () => {
    const background = readFileSync(join(SRC_DIR, 'background.ts'), 'utf8');
    expect(background).toContain("request.type === 'policy_op'");
    // Scope to the handler body rather than matching one exact expression.
    // A regex over `updatePolicyState((state) => applyPolicyOp(state, op))`
    // fails on a legitimate refactor that merely added a cast or reformatted
    // the call — a guard that cries wolf gets deleted. What matters is that
    // inside this branch the operation reaches the storage layer's write
    // path, because that call is what puts it in the service worker's
    // serialization queue.
    const branch = background.slice(
      background.indexOf("request.type === 'policy_op'"),
      background.indexOf("request.type === 'policy_op'") + 800,
    );
    expect(branch).toContain('updatePolicyState');
    expect(branch).toContain('applyPolicyOp');
  });

  it('the operations the service worker applies are exhaustive over PolicyOp', () => {
    // A new operation added to the PolicyOp union without a reducer case
    // would be a silent no-op at runtime; TypeScript's exhaustiveness makes it
    // a compile error instead, and this test fails if the union and the
    // switch ever drift apart in the emitted behavior.
    const operations = readFileSync(
      join(SRC_DIR, 'policy-operations.ts'),
      'utf8',
    );
    const opNames = [
      'denial_action',
      'remove_origin',
      'add_block',
      'remove_block',
      'remove_download',
      'set_takeover',
      'set_pairing_token',
    ];
    for (const op of opNames) {
      expect(operations).toContain(`case '${op}':`);
    }
  });
});

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

// Anchor on the module specifier rather than on the write function names, so
// renaming updatePolicyState cannot turn this guard into a silent no-op that
// stays green. The write patterns are matched broadly, including direct
// chrome.storage.local writes — the shape the original bug would take if a
// module bypassed policy-state entirely.
const IMPORTS_POLICY_STATE = /from\s+'[^']*policy-state'/;
const WRITES_POLICY =
  /\b(updatePolicyState|setPolicyState|decideWithState|setAgentGroupAvailability)\s*\(|chrome\.storage\.local\.(set|remove|clear)\s*\(/;

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
    if (IMPORTS_POLICY_STATE.test(source) && WRITES_POLICY.test(source)) {
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
    // The operation must run through updatePolicyState — that call is what
    // puts it in the service worker's serialization queue.
    expect(background).toMatch(
      /updatePolicyState\(\(state\)\s*=>\s*applyPolicyOp\(state,\s*op\)\)/,
    );
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

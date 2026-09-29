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

// A direct write counts when the policy key is named in the call's arguments —
// which is how both real shapes reach it: `set({ policyState: next })` and
// `remove('policyState')`.
//
// An earlier version required an object literal, and listed `remove|clear` as
// alternatives anyway. That was dead surface reading as coverage: neither API
// takes an object literal, so both alternatives could never match, and a UI
// module calling `chrome.storage.local.remove('policyState')` or
// `chrome.storage.local.clear()` — either of which really does destroy the
// user's approvals and takeover toggle — passed the guard. `clear()` takes no
// argument at all, so it gets its own branch rather than a key it cannot
// contain.
//
// Known limits, so this is not read as more than it is. The argument scan is
// `[^\)]*`, so a call that nests a `)` before naming the key is missed, and
// MUTATES_POLICY has no lexer behind it, so a mutator's name inside a comment
// or string counts as a call. Both err toward passing a real file silently.
const WRITES_POLICY_KEY =
  /chrome\.storage\.(local|sync|session)\.(set|remove)\s*\([^)]*['"`\s{,]policyState\b|chrome\.storage\.(local|sync|session)\.clear\s*\(/;

// The scan's one rule, as a pure function over source text, so the fixtures
// below can exercise it directly. A guard that only ever runs against the real
// tree passes for the wrong reason whenever its matching rules are weakened —
// it still finds nothing in a clean tree, and only turns red once a real
// bypass is *also* present. That is two failures for one signal. Testing the
// classifier against inline sources makes a broken anchor fail on its own.
function isPolicyWriter(source: string): boolean {
  return MUTATES_POLICY.test(source) || WRITES_POLICY_KEY.test(source);
}

function sourceFilesIn(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) return sourceFilesIn(full);
    return /\.(ts|tsx)$/.test(entry.name) ? [full] : [];
  });
}

describe('the guard recognises the writes it exists to catch', () => {
  // Every case is a source string the classifier has to judge correctly, in
  // both directions. The directory scan above cannot do this: it only ever
  // runs against a tree that is already clean, so it stays green whether the
  // patterns are sharp or blunt.
  const cases: { name: string; writer: boolean; source: string }[] = [
    // Must be flagged. The first two are the original bug and the shortcut
    // someone reaches for when the message round-trip feels like overkill.
    {
      name: 'a UI module writing the policy key straight into storage',
      writer: true,
      source: `
        export async function setTakeover(on: boolean) {
          await chrome.storage.local.set({ policyState: { ...next, takeover: on } });
        }`,
    },
    {
      name: 'a UI module calling a policy-state mutator directly',
      writer: true,
      source: `
        import { updatePolicyState } from '../policy-state';
        await updatePolicyState((state) => applyPolicyOp(state, op));`,
    },
    // Both of these destroy the user's approvals and takeover toggle just as
    // thoroughly as a write does. The object-literal-only version of this
    // pattern listed `remove|clear` as alternatives that could never match.
    {
      name: 'a UI module removing the policy key by name',
      writer: true,
      source: `await chrome.storage.local.remove('policyState');`,
    },
    {
      name: 'a UI module clearing local storage outright',
      writer: true,
      source: `await chrome.storage.local.clear();`,
    },
    {
      name: 'the same bypass against sync storage',
      writer: true,
      source: `await chrome.storage.sync.set({ policyState: next });`,
    },
    {
      name: 'a single-use grant consumed outside the worker queue',
      writer: true,
      source: `
        const state = await getPolicyState();
        state.sensitiveGrants = {};
        await chrome.storage.session.set({ policyState: state });`,
    },

    // Must not be flagged. These are what a guard blunt enough to be
    // "always red" would catch instead, and a guard that cries wolf is a
    // guard that gets deleted.
    {
      name: 'an unrelated storage key',
      writer: false,
      source: `await chrome.storage.local.set({ sidebarOpen: true });`,
    },
    {
      name: 'naming the PolicyState type without writing it',
      writer: false,
      source: `
        import type { PolicyState } from '../policy-state';
        export function describe(s: PolicyState): string { return s.takeover ? 'on' : 'off'; }`,
    },
    {
      name: 'reading policy state',
      writer: false,
      source: `
        import { getPolicyState } from '../policy-state';
        const state = await getPolicyState();
        return state.origins;`,
    },
  ];

  for (const { name, writer, source } of cases) {
    it(`${writer ? 'flags' : 'passes'} ${name}`, () => {
      expect(isPolicyWriter(source)).toBe(writer);
    });
  }
});

describe('only the service worker writes policy state', () => {
  const offenders: string[] = [];

  // Scan every extension source file, not just ui/: offscreen.ts,
  // agent-group.ts and any future page are equally able to reintroduce the
  // second writer this guard exists to prevent.
  for (const file of sourceFilesIn(SRC_DIR)) {
    if (ALLOWED_WRITERS.has(file)) continue;
    const source = readFileSync(file, 'utf8');
    if (isPolicyWriter(source)) {
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
    //
    // The opening anchor includes the `if` and the brace, not just the test
    // expression. A sender guard in front of the handler also names
    // `request.type === 'policy_op'` — in a boolean expression, without the
    // brace — and anchoring on the bare substring found *that* one, so the
    // window was the guard's few lines and the guard went red for a change
    // that had not broken anything it claims.
    //
    // The window ends where the branch's own braces close, matched by
    // counting. It used to end at the next `if (request.type ===`, which is
    // wrong twice over: that anchor sits *after* this branch, so the window
    // ran on past the closing brace and could be satisfied by a later
    // handler; and when no such anchor followed, `indexOf` returned -1 and
    // `slice(start, -1)` handed back the rest of the file — where
    // `updatePolicyState` appears in the `chrome.tabs.onRemoved` listener. A
    // guard that quietly stops guarding is worse than no guard, because
    // renaming every later branch was enough to make it vacuous.
    //
    // Counting braces is not exact — a brace inside a string or comment
    // throws it off. That fails in the safe direction: a count that lands
    // early makes these assertions red, and one that runs long still stops at
    // a `}` it did not open, so the window stays inside the file rather than
    // escaping to the end.
    const start = background.indexOf("if (request.type === 'policy_op') {");
    expect(start).toBeGreaterThan(-1);
    let depth = 0;
    let end = -1;
    for (let i = start; i < background.length; i += 1) {
      if (background[i] === '{') depth += 1;
      else if (background[i] === '}') {
        depth -= 1;
        if (depth === 0) {
          end = i;
          break;
        }
      }
    }
    expect(end).toBeGreaterThan(start);
    // Comments are stripped so an explanatory note naming the two calls
    // cannot satisfy the assertions on its own. Verified: the calls moved
    // out of the branch and the branch given a comment that names both —
    // green before this line, red after it. The `//` is not taken for a
    // comment start when it is part of `://`, the same trap the
    // localhost sweep in offscreen-endpoint.test.ts documents.
    const branch = background
      .slice(start, end + 1)
      .split('\n')
      .map((line) =>
        line.replace(/\/\*.*?\*\//g, '').replace(/(^|[^:])\/\/.*$/, '$1'),
      )
      .join('\n');
    expect(branch).toContain('updatePolicyState');
    expect(branch).toContain('applyPolicyOp');
  });

  it('the service worker handles every operation the UI can name', () => {
    // What this actually guarantees, stated plainly because the stronger
    // claim is false: every operation currently declared in the PolicyOp
    // union has a case in the service worker's reducer, so naming it in the
    // UI cannot be a silent no-op.
    //
    // It does NOT detect drift in either direction. An operation added to the
    // union *and* implemented would leave this test green, and one added to
    // the union without a case is a TypeScript error that this test never
    // sees — the exhaustiveness of `applyPolicyOp` is enforced by the
    // compiler, not here. Claiming otherwise would be a test that sounds
    // load-bearing and is not.
    const operations = readFileSync(
      join(SRC_DIR, 'policy-operations.ts'),
      'utf8',
    );
    const union = operations.slice(
      operations.indexOf('export type PolicyOp'),
      operations.indexOf('// denyChanges'),
    );
    const declared = [...union.matchAll(/op: '([a-z_]+)'/g)].map((m) => m[1]);
    expect(declared.length).toBeGreaterThan(0);
    for (const op of declared) {
      expect(operations).toContain(`case '${op}':`);
    }
  });
});

import { describe, expect, it } from 'bun:test';
import { readFileSync } from 'node:fs';

// The Takeover switch must never show "the human has the browser" while the
// policy gate — which reads persisted state, not the panel — still sees
// takeover off and lets the command through.
//
// The panel used to render on click and persist on the second half of the
// round trip. The gap is one message round trip: sub-millisecond with a warm
// service worker, longer with a cold one, since the request has to wake it.
// For any other control an optimistic render is a nicety; for a kill switch
// it is the switch lying to the one person reaching for it under pressure.
//
// Why this is a source-shape guard rather than a rendered one: there is no DOM
// harness in this package. The UI tests use react-dom/server, which cannot
// run effects or simulate a click, so nothing here can observe what the
// switch displays after a click. Adding happy-dom or testing-library for a
// single ordering property is a larger change than the property, and it would
// put a new test surface in the package to cover four lines of ordering.
// The same tradeoff the single-writer guard makes, for the same reason.
//
// What this can catch: someone re-adding an optimistic render, or moving the
// apply ahead of the write. What it cannot: a wrong value applied after a
// correct write, which is a behaviour test and would need the DOM.
const SIDE_PANEL = readFileSync(
  new URL('../src/ui/SidePanel.tsx', import.meta.url),
  'utf8',
);

function takeoverHandler(): string {
  const start = SIDE_PANEL.indexOf('const handleTakeoverChange');
  expect(start).toBeGreaterThan(-1);
  const end = SIDE_PANEL.indexOf('const handleOpenSettings', start);
  expect(end).toBeGreaterThan(start);
  return SIDE_PANEL.slice(start, end);
}

describe('the takeover switch does not run ahead of the write that backs it', () => {
  it('persists before it renders, not the other way round', () => {
    const body = takeoverHandler();
    const write = body.indexOf('requestPolicyOp(');
    const render = body.indexOf('setTakeover(');

    expect(write).toBeGreaterThan(-1);
    expect(render).toBeGreaterThan(-1);
    // Reverted to optimistic, this reads `render < write` and fails. The
    // error names both, because "expected render after write" on its own
    // does not say which one moved.
    expect(render).toBeGreaterThan(write);
  });

  it('applies the value the user last asked for, not a superseded one', () => {
    const body = takeoverHandler();
    // A rapid off/on/off must not leave the switch showing the first click's
    // value. Every settle path is guarded by a generation comparison; if one
    // of them is not, the guard is gone even though the code still mentions
    // the counter.
    const settles = body.match(/\.then\(|\.catch\(/g) ?? [];
    expect(settles.length).toBeGreaterThanOrEqual(2);
    const comparisons = body.match(/writeGen\.current !== myGen/g) ?? [];
    // One per settle path. Fewer means a path applies a stale value.
    expect(comparisons.length).toBe(settles.length);
  });

  it('no longer reverts on failure, because it never moved', () => {
    const body = takeoverHandler();
    // The old code rendered first and undid the render in .catch. That
    // pattern is the bug in a different costume: it is "do not move until it
    // worked" with an extra step and a window in which the two disagree.
    expect(body).not.toMatch(/setTakeover\(!desired\)/);
  });
});

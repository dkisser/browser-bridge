import { describe, expect, it } from 'bun:test';
import {
  click,
  createTakeoverSync,
  observe,
  settle,
  type TakeoverSync,
} from '../src/ui/takeover-sync';

// The takeover switch must never show "the human has the browser" while the
// policy gate — which reads persisted state, not this panel — still sees
// takeover off. Nothing here moves the display on the strength of a click:
// the display moves when the engine reports a value, or when a write of ours
// is known to have landed.
//
// These are the cases that used to be unrunnable. The bookkeeping lived in
// refs inside SidePanel.tsx, so the only available check was a source-shape
// guard that counted occurrences of `writeGen.current !== myGen` and
// `setTakeover(!desired)`. Such a guard goes green when the behaviour is
// wrong — it is a fingerprint of the current text, not a statement about
// what the switch does. The logic is a pure module now, so the switch's
// behaviour is executed here instead of described.

function settled(initial: boolean): TakeoverSync {
  return observe(createTakeoverSync(initial), initial);
}

describe('the switch does not move on a click', () => {
  it('a click alone leaves the display where the engine put it', () => {
    // The regression this file exists for. Reverted to an optimistic render,
    // `displayed` becomes `desired` here and every case below still reads
    // plausibly — which is why the assertion is on the value, not on the
    // absence of a call.
    const s = click(settled(false), true);

    expect(s.displayed).toBe(false);
    expect(s.pending).toBe(true);
    expect(s.desired).toBe(true);
  });

  it('a write that lands moves it to what was asked for', () => {
    const opened = click(settled(false), true);
    const s = settle(opened, opened.gen, true);

    expect(s.displayed).toBe(true);
    expect(s.pending).toBe(false);
  });

  it('a write that fails leaves it, and still releases the window', () => {
    // Releasing matters as much as the value: a window that never closes
    // parks every later observation forever, so the switch stops following
    // the engine for the rest of the session.
    const opened = click(settled(true), false);
    const s = settle(opened, opened.gen, false);

    expect(s.displayed).toBe(true);
    expect(s.pending).toBe(false);
  });
});

describe('an observation that arrives during our own write', () => {
  it('is consumed when the reply is lost, so the switch still moves', () => {
    // The write lands but sendResponse never arrives — the service worker is
    // torn down between chrome.storage.local.set and the reply. The storage
    // event is the only other carrier of the new value, and dropping it is
    // what left the panel reading "human assist active" while the engine
    // enforced takeover off. Verified against the pre-fix code, which stayed
    // on `true` here.
    const opened = click(settled(true), false);
    const observed = observe(opened, false);
    const s = settle(observed, opened.gen, false);

    expect(s.displayed).toBe(false);
    expect(s.parked).toBe(false);
  });

  it('wins over our own request when a second surface wrote after us', () => {
    // Our write is confirmed, but a later write from another surface already
    // replaced it. Applying what *we* asked for here is the stale stamp: the
    // switch would show a value the engine moved on from, with no event left
    // to correct it.
    const opened = click(settled(false), true);
    const observed = observe(opened, false);
    const s = settle(observed, opened.gen, true);

    expect(s.displayed).toBe(false);
  });

  it('is not applied the moment it arrives, only when the write settles', () => {
    // Parking rather than applying is the whole mechanism. If observe() moved
    // the display directly during the window, an out-of-order event landing
    // mid-write would show the user a value their own click has not produced.
    //
    // The observed value must DIFFER from the current display, or the test
    // cannot tell the two behaviours apart: an implementation that applies it
    // immediately and one that parks it both leave `displayed` alone when the
    // two values are equal. The earlier version of this case observed `true`
    // against a display already showing `true`, so it passed against a
    // `parked` branch that did nothing. Verified: with observe() rewritten to
    // apply during the window, this case was green for the full suite.
    const s = observe(click(settled(true), true), false);

    expect(s.displayed).toBe(true);
    expect(s.observed).toBe(false);
    expect(s.parked).toBe(true);
  });

  it('parks an observation that disagrees with the display in either direction', () => {
    // The other direction. An implementation that parked only "the value the
    // user asked for", or that compared against something other than the
    // display, would pass the single case above and fail here.
    const off = observe(click(settled(false), false), true);
    expect(off.displayed).toBe(false);
    expect(off.observed).toBe(true);
    expect(off.parked).toBe(true);

    // And the parked value is what the settle applies, so a display that
    // agreed by coincidence cannot pass this by accident either.
    expect(settle(off, 1, false).displayed).toBe(true);
  });
});

describe('ordering against our own settle', () => {
  it('an observation arriving after the settle does not rewind the display', () => {
    // chrome.storage.onChanged and the sendMessage reply are both async and
    // neither is ordered against the other. The common case is our own
    // write's event landing just after we already applied the same value.
    const opened = click(settled(false), true);
    const s = observe(settle(opened, opened.gen, true), true);

    expect(s.displayed).toBe(true);
  });

  it('an external write after ours is still followed', () => {
    const opened = click(settled(false), true);
    const after = settle(opened, opened.gen, true);
    const s = observe(after, false);

    expect(s.displayed).toBe(false);
  });
});

describe('a superseded click', () => {
  it('does not release the newer write’s window', () => {
    // The older settle used to clear the pending flag unconditionally. That
    // let a parked observation apply while a write was still in flight — the
    // exact interleaving the park exists to prevent.
    const first = click(settled(false), true);
    const second = click(first, true);
    // The state is the newest one; the generation is the click being settled.
    const s = settle(second, first.gen, true);

    expect(s.pending).toBe(true);
  });

  it('does not move the display on its own', () => {
    const first = click(settled(false), true);
    const second = click(first, false);
    const s = settle(second, first.gen, true);

    expect(s.displayed).toBe(false);
    expect(s.pending).toBe(true);
  });

  it('leaves the last click in charge of the value', () => {
    const first = click(settled(false), true);
    const second = click(first, false);
    const s = settle(settle(second, second.gen, true), second.gen, true);

    expect(s.displayed).toBe(false);
    expect(s.pending).toBe(false);
  });
});

describe('the seed value', () => {
  it('an initial observation of the engine’s value moves the display', () => {
    // A panel that opens while takeover is already on must show it on. The
    // previous code seeded `lastAppliedTakeover` to false and relied on the
    // first effect run to correct it; if that comparison were ever dropped
    // the switch would read "the human has the browser" on open.
    const s = observe(createTakeoverSync(false), true);

    expect(s.displayed).toBe(true);
    expect(s.applied).toBe(true);
  });
});

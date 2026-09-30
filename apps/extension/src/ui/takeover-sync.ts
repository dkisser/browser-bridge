// What the takeover switch displays, and what it is allowed to believe.
//
// The switch must never read "the human has the browser" while the policy
// gate — which reads persisted state, not this panel — still sees takeover
// off and lets the command through. So nothing here moves the display on the
// strength of a click. A click starts a write; the display moves when the
// engine says what it persisted, or when a write of ours is known to have
// landed.
//
// Three inputs drive it:
//
//   click()   — the user asked for the other value. Opens a write window.
//   observe() — the engine reported a new persisted value. The only thing
//               that moves the display on its own authority.
//   settle()  — our write finished. Releases the window.
//
// The part that is easy to get wrong is an observation that arrives while our
// own write is in flight. It used to be dropped, and dropping it is wrong in
// two directions at once:
//
//   - If the write lands but its reply is lost (the service worker torn down
//     between chrome.storage.local.set and sendResponse), the only event
//     carrying the new value is the one that gets dropped. The switch never
//     moves, the error path says the write failed, and the panel is left
//     claiming the human has a browser the agent already has.
//   - If a second surface writes after ours and its event lands during our
//     window, our settle then stamps our now-stale request over a state that
//     has already been replaced.
//
// So an observation during the window is parked rather than applied, and the
// settle consumes it: an observation that arrived during the write is at
// least as new as the write, so it wins over what we asked for.
//
// Pure functions on purpose. This logic used to live in refs inside
// SidePanel.tsx, where the only way to test it was to read the source and
// count the occurrences of a variable — a guard that passes when the
// behaviour is wrong. It is pure so the cases above can be executed.

export interface TakeoverSync {
  /** What the switch shows. */
  displayed: boolean;
  /** A write of ours is in flight. Observations are parked, not applied. */
  pending: boolean;
  /** Generation of the most recent click. */
  gen: number;
  /** The value `displayed` was last synced to. */
  applied: boolean;
  /** The value our in-flight write is asking for, or null outside a window. */
  desired: boolean | null;
  /** The newest value the engine has reported, or null if none yet. */
  observed: boolean | null;
  /** An observation arrived while `pending` was set. */
  parked: boolean;
}

export function createTakeoverSync(initial: boolean): TakeoverSync {
  return {
    displayed: initial,
    pending: false,
    gen: 0,
    applied: initial,
    desired: null,
    observed: initial,
    parked: false,
  };
}

/** The engine reported `takeover` as the persisted value. */
export function observe(sync: TakeoverSync, takeover: boolean): TakeoverSync {
  if (sync.pending) {
    return { ...sync, observed: takeover, parked: true };
  }
  if (takeover === sync.applied) {
    return { ...sync, observed: takeover };
  }
  return {
    ...sync,
    displayed: takeover,
    applied: takeover,
    observed: takeover,
  };
}

/** The user asked for `desired`. Nothing moves yet. */
export function click(sync: TakeoverSync, desired: boolean): TakeoverSync {
  return {
    ...sync,
    // `displayed` is deliberately untouched. This is the whole point: the
    // switch shows what the engine enforces, and the engine has not been
    // asked yet.
    pending: true,
    gen: sync.gen + 1,
    desired,
    // No observation is known for this window yet. Carrying the previous
    // value forward would let a settle with parked === false read a
    // pre-write value as though it were a post-write one.
    observed: null,
    parked: false,
  };
}

/**
 * Our write for generation `gen` finished. `ok` is false when the request
 * failed outright.
 */
export function settle(
  sync: TakeoverSync,
  gen: number,
  ok: boolean,
): TakeoverSync {
  // A superseded click no longer owns the window. Releasing it here would
  // let a parked observation apply while a newer write is still in flight,
  // and would let this click's own settle report a banner about an action
  // that no longer matches what the user last asked for. The newer click's
  // settle releases it.
  if (gen !== sync.gen) return sync;

  const base: TakeoverSync = {
    ...sync,
    pending: false,
    desired: null,
    parked: false,
  };

  // A parked observation is at least as new as our own write, so it is the
  // truth about the engine. This is the branch that keeps a lost reply from
  // stranding the switch on the pre-write value, and the one that stops this
  // settle from stamping a request a second surface has already replaced.
  if (sync.parked && sync.observed !== null) {
    const value = sync.observed;
    return { ...base, displayed: value, applied: value, observed: value };
  }

  // A failed write never moved the switch, and the caller reports why. There
  // is nothing to undo and nothing new to show.
  if (!ok) return base;

  const desired = sync.desired;
  if (desired === null || desired === base.applied) return base;
  return { ...base, displayed: desired, applied: desired, observed: desired };
}

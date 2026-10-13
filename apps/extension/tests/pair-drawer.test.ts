import { describe, expect, it } from 'bun:test';
import {
  initialPairDrawerState,
  type PairDrawerEvent,
  type PairDrawerState,
  pairDrawerReducer,
  shouldShowPairForm,
} from '../src/ui/components/pair-drawer';

const send = (
  state: PairDrawerState,
  ...events: PairDrawerEvent[]
): PairDrawerState => events.reduce(pairDrawerReducer, state);

describe('pair drawer visibility', () => {
  // Unpaired is the blocking state the user must resolve, so the drawer is
  // forced open regardless of what the toggle last did.
  it('shows the drawer whenever the browser is not paired', () => {
    expect(shouldShowPairForm(false, false)).toBe(true);
    expect(shouldShowPairForm(false, true)).toBe(true);
  });

  it('hides the drawer when paired and closed, shows it when re-pairing', () => {
    expect(shouldShowPairForm(true, false)).toBe(false);
    expect(shouldShowPairForm(true, true)).toBe(true);
  });
});

describe('pairDrawerReducer', () => {
  it('toggles open and closed', () => {
    const open = send(initialPairDrawerState, { type: 'toggle' });
    expect(open.open).toBe(true);
    expect(send(open, { type: 'toggle' }).open).toBe(false);
  });

  // The reported bug. On a re-pair `paired` is already true, so `!paired`
  // cannot collapse the drawer on its own — a successful submit that left
  // the drawer open made the row stick around forever.
  it('closes on a successful re-pair', () => {
    const drawer = send(initialPairDrawerState, { type: 'toggle' });
    expect(drawer.open).toBe(true);
    expect(send(drawer, { type: 'pairing-succeeded' }).open).toBe(false);
  });

  // The mirror image: on a first pairing `paired` is still false when the
  // submit resolves, so collapsing the drawer must not blank the form —
  // `shouldShowPairForm` keeps it visible until the token lands in storage.
  it('leaves the first pairing visible until paired flips', () => {
    const drawer = send(initialPairDrawerState, { type: 'pairing-succeeded' });
    expect(drawer.open).toBe(false);
    expect(shouldShowPairForm(false, drawer.open)).toBe(true);
    expect(shouldShowPairForm(true, drawer.open)).toBe(false);
  });

  it('does not mutate the state it is given', () => {
    const before: PairDrawerState = { open: true };
    const after = pairDrawerReducer(before, { type: 'pairing-succeeded' });
    expect(before.open).toBe(true);
    expect(after).not.toBe(before);
  });
});

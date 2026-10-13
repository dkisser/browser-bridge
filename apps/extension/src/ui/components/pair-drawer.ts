// Visibility rules for the "Pair with bridge-core" drawer in the hero card.
// Split out of the component so the state machine is unit-testable without a
// DOM: the drawer has no DOM affordance of its own, only this rule.

export interface PairDrawerState {
  readonly open: boolean;
}

export type PairDrawerEvent =
  | { readonly type: 'toggle' }
  | { readonly type: 'pairing-succeeded' };

export const initialPairDrawerState: PairDrawerState = { open: false };

export function pairDrawerReducer(
  state: PairDrawerState,
  event: PairDrawerEvent,
): PairDrawerState {
  switch (event.type) {
    case 'toggle':
      return { open: !state.open };
    case 'pairing-succeeded':
      // Close on a re-pair as well as a first pairing. A re-pair already had
      // `paired === true`, so `!paired` never becomes true to collapse the
      // drawer — leaving it open here is what kept the row on screen after a
      // successful re-pair. On a first pairing this is a no-op for the user:
      // `paired` is still false, so shouldShowPairForm keeps the form shown
      // until the token reaches storage.
      return { open: false };
  }
}

export function shouldShowPairForm(
  paired: boolean,
  drawerOpen: boolean,
): boolean {
  return !paired || drawerOpen;
}

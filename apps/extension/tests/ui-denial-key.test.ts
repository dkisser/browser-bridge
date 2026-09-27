import { describe, expect, it } from 'bun:test';
import { type Denial, denialKey } from '@browser-bridge/shared';

function denial(partial: Partial<Denial>): Denial {
  return {
    reason: 'origin_not_approved',
    command: 'click',
    ...partial,
  } as Denial;
}

describe('denialKey', () => {
  it('encodes reason|origin|command in a stable string', () => {
    expect(
      denialKey(denial({ origin: 'https://a.example', command: 'click' })),
    ).toBe('origin_not_approved|https://a.example|click');
  });

  it('treats a missing origin as an empty string', () => {
    expect(denialKey(denial({ origin: undefined, command: 'type' }))).toBe(
      'origin_not_approved||type',
    );
  });

  it('returns the same key for two denials that differ only in identity', () => {
    // recordDenial dedupes on this key, so two denials with the same
    // payload must collide regardless of object identity.
    const a = denial({ origin: 'https://a.example' });
    const b = denial({ origin: 'https://a.example' });
    expect(denialKey(a)).toBe(denialKey(b));
  });

  it('distinguishes denials with different commands', () => {
    expect(denialKey(denial({ command: 'click' }))).not.toBe(
      denialKey(denial({ command: 'type' })),
    );
  });

  it('distinguishes denials with different reasons', () => {
    expect(denialKey(denial({ reason: 'origin_not_approved' }))).not.toBe(
      denialKey(denial({ reason: 'approval_required' })),
    );
  });
});

import { describe, expect, it } from 'bun:test';
import { hashToken, PairingManager } from '../src/pairing';

function makeManager(): {
  manager: PairingManager;
  storedHash: () => string | undefined;
} {
  let hash: string | undefined;
  return {
    manager: new PairingManager(
      () => hash,
      (h) => {
        hash = h;
      },
    ),
    storedHash: () => hash,
  };
}

const NOW = 1_700_000_000_000;

describe('PairingManager', () => {
  it('start returns an 8-char code from the unambiguous alphabet', () => {
    const { manager } = makeManager();
    const { code } = manager.start(NOW);
    expect(code).toMatch(/^[0-9A-HJ-NP-TV-Z]{8}$/);
    expect(code.includes('I')).toBe(false);
    expect(code.includes('O')).toBe(false);
  });

  it('confirm with the right code issues a token and stores only its hash', () => {
    const { manager, storedHash } = makeManager();
    const { code } = manager.start(NOW);
    const result = manager.confirm(code, NOW);
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.token).toMatch(/^[0-9a-f]{64}$/);
      expect(storedHash()).toBe(hashToken(result.token));
    }
    expect(manager.isPaired).toBe(true);
  });

  it('confirm is case-insensitive and tolerates dashes', () => {
    const { manager } = makeManager();
    const { code } = manager.start(NOW);
    const result = manager.confirm(code.toLowerCase().split('').join('-'), NOW);
    expect(result.ok).toBe(true);
  });

  it('verify accepts the paired token and rejects others', () => {
    const { manager } = makeManager();
    const { code } = manager.start(NOW);
    const result = manager.confirm(code, NOW);
    if (!result.ok) throw new Error('pairing failed');
    expect(manager.verify(result.token)).toBe(true);
    expect(manager.verify('deadbeef'.repeat(8))).toBe(false);
    expect(manager.verify(undefined)).toBe(false);
    expect(manager.verify('')).toBe(false);
  });

  it('rejects confirm when no code is pending', () => {
    const { manager } = makeManager();
    expect(manager.confirm('ABCDEFGH', NOW)).toEqual({
      ok: false,
      error: 'invalid_code',
    });
  });

  it('expires the code after the TTL', () => {
    const { manager } = makeManager();
    const { code } = manager.start(NOW);
    const result = manager.confirm(code, NOW + 5 * 60 * 1000 + 1);
    expect(result).toEqual({ ok: false, error: 'code_expired' });
  });

  it('gives up after 5 wrong attempts', () => {
    const { manager } = makeManager();
    const { code } = manager.start(NOW);
    for (let i = 1; i <= 4; i++) {
      const result = manager.confirm('ZZZZZZZZ', NOW);
      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.error).toBe('invalid_code');
        expect(result.attemptsRemaining).toBe(5 - i);
      }
    }
    const fifth = manager.confirm('ZZZZZZZZ', NOW);
    expect(fifth).toEqual({ ok: false, error: 'too_many_attempts' });
    // pending code is cleared — the right code no longer works either
    expect(manager.confirm(code, NOW).ok).toBe(false);
  });

  it('re-pairing rotates the token and invalidates the old one', () => {
    const { manager } = makeManager();
    const first = manager.confirm(manager.start(NOW).code, NOW);
    if (!first.ok) throw new Error('pairing failed');
    const second = manager.confirm(manager.start(NOW).code, NOW);
    if (!second.ok) throw new Error('pairing failed');
    expect(manager.verify(second.token)).toBe(true);
    expect(manager.verify(first.token)).toBe(false);
  });
});

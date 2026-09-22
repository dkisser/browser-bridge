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
    // failure budget still exhausted — the right code no longer works either
    expect(manager.confirm(code, NOW).ok).toBe(false);
  });

  it('start() does not reset the failure budget', () => {
    const { manager } = makeManager();
    // 4 failures against the first code...
    manager.start(NOW);
    for (let i = 0; i < 4; i++) {
      manager.confirm('ZZZZZZZZ', NOW);
    }
    // ...then a fresh code must NOT reopen a full 5-try window: one more
    // failure inside the window exhausts the budget...
    manager.start(NOW + 1000);
    expect(manager.confirm('ZZZZZZZZ', NOW + 1000)).toEqual({
      ok: false,
      error: 'too_many_attempts',
    });
    // ...and the fresh code is locked out with it.
    const { code } = manager.start(NOW + 2000);
    expect(manager.confirm(code, NOW + 2000)).toEqual({
      ok: false,
      error: 'too_many_attempts',
    });
  });

  it('failures expire from the rolling window', () => {
    const { manager } = makeManager();
    manager.start(NOW);
    for (let i = 0; i < 4; i++) {
      manager.confirm('ZZZZZZZZ', NOW);
    }
    // After the window passes, only failures still inside it count.
    const { code } = manager.start(NOW + 11 * 60 * 1000);
    const result = manager.confirm(code, NOW + 11 * 60 * 1000);
    expect(result.ok).toBe(true);
  });

  it('successful pairing clears the failure budget', () => {
    const { manager } = makeManager();
    const first = manager.start(NOW);
    manager.confirm('ZZZZZZZZ', NOW);
    expect(manager.confirm(first.code, NOW).ok).toBe(true);
    // A later pairing round starts with a clean budget.
    const second = manager.start(NOW + 60_000);
    for (let i = 1; i <= 4; i++) {
      const result = manager.confirm('YYYYYYYY', NOW + 60_000);
      expect(result.ok).toBe(false);
      if (!result.ok) expect(result.attemptsRemaining).toBe(5 - i);
    }
    expect(manager.confirm(second.code, NOW + 60_000).ok).toBe(true);
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

import {
  createHash,
  randomBytes,
  randomInt,
  timingSafeEqual,
} from 'node:crypto';

const CODE_LENGTH = 8;
const CODE_TTL_MS = 5 * 60 * 1000;
const MAX_CONFIRM_ATTEMPTS = 5;
// Failed confirmations count against a rolling window that start() must not
// reset — otherwise alternating start/confirm re-opens a fresh 5-try budget
// for every new code.
const ATTEMPT_WINDOW_MS = 10 * 60 * 1000;
// Crockford Base32: no I, L, O, U — unambiguous to read and type.
const CODE_ALPHABET = '0123456789ABCDEFGHJKMNPQRSTVWXYZ';

export type PairingError =
  | 'invalid_code'
  | 'code_expired'
  | 'too_many_attempts';

export type PairConfirmResult =
  | { ok: true; token: string }
  | { ok: false; error: PairingError; attemptsRemaining?: number };

interface PendingCode {
  code: string;
  expiresAt: number;
}

export function hashToken(token: string): string {
  return createHash('sha256').update(token).digest('hex');
}

export class PairingManager {
  private pending: PendingCode | null = null;
  // Timestamps of failed confirmations inside the rolling window. start()
  // intentionally does NOT clear this: the failure budget is per-window, not
  // per-code.
  private failures: number[] = [];

  constructor(
    private getTokenHash: () => string | undefined,
    private setTokenHash: (hash: string) => void,
  ) {}

  get isPaired(): boolean {
    return this.getTokenHash() !== undefined;
  }

  start(now = Date.now()): { code: string; expiresIn: number } {
    let code = '';
    for (let i = 0; i < CODE_LENGTH; i++) {
      code += CODE_ALPHABET[randomInt(CODE_ALPHABET.length)];
    }
    this.pending = { code, expiresAt: now + CODE_TTL_MS };
    return { code, expiresIn: CODE_TTL_MS };
  }

  confirm(code: string, now = Date.now()): PairConfirmResult {
    const pending = this.pending;
    if (!pending) {
      return { ok: false, error: 'invalid_code' };
    }
    if (now > pending.expiresAt) {
      this.pending = null;
      return { ok: false, error: 'code_expired' };
    }
    this.failures = this.failures.filter((at) => now - at < ATTEMPT_WINDOW_MS);
    if (this.failures.length >= MAX_CONFIRM_ATTEMPTS) {
      return { ok: false, error: 'too_many_attempts' };
    }
    if (!codesEqual(code, pending.code)) {
      this.failures.push(now);
      if (this.failures.length >= MAX_CONFIRM_ATTEMPTS) {
        return { ok: false, error: 'too_many_attempts' };
      }
      return {
        ok: false,
        error: 'invalid_code',
        attemptsRemaining: MAX_CONFIRM_ATTEMPTS - this.failures.length,
      };
    }
    const token = randomBytes(32).toString('hex');
    this.setTokenHash(hashToken(token));
    this.pending = null;
    // A successful pairing clears the failure budget for the next round.
    this.failures = [];
    return { ok: true, token };
  }

  verify(token: string | null | undefined): boolean {
    const expectedHex = this.getTokenHash();
    if (!expectedHex || !token) return false;
    const actual = createHash('sha256').update(token).digest();
    const expected = Buffer.from(expectedHex, 'hex');
    return (
      actual.length === expected.length && timingSafeEqual(actual, expected)
    );
  }
}

function normalizeCode(code: string): string {
  return code.toUpperCase().replace(/-/g, '').trim();
}

// Constant-time comparison, matching the discipline of verify(): the pairing
// code is a bearer secret while it lives, so its check must not short-circuit
// on the first differing character.
function codesEqual(input: string, expected: string): boolean {
  const a = Buffer.from(normalizeCode(input));
  const b = Buffer.from(expected);
  return a.length === b.length && timingSafeEqual(a, b);
}
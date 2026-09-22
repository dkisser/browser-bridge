import {
  createHash,
  randomBytes,
  randomInt,
  timingSafeEqual,
} from 'node:crypto';

const CODE_LENGTH = 8;
const CODE_TTL_MS = 5 * 60 * 1000;
const MAX_CONFIRM_ATTEMPTS = 5;
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
  attempts: number;
}

export function hashToken(token: string): string {
  return createHash('sha256').update(token).digest('hex');
}

export class PairingManager {
  private pending: PendingCode | null = null;

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
    this.pending = { code, expiresAt: now + CODE_TTL_MS, attempts: 0 };
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
    if (pending.attempts >= MAX_CONFIRM_ATTEMPTS) {
      this.pending = null;
      return { ok: false, error: 'too_many_attempts' };
    }
    if (normalizeCode(code) !== pending.code) {
      pending.attempts += 1;
      if (pending.attempts >= MAX_CONFIRM_ATTEMPTS) {
        this.pending = null;
        return { ok: false, error: 'too_many_attempts' };
      }
      return {
        ok: false,
        error: 'invalid_code',
        attemptsRemaining: MAX_CONFIRM_ATTEMPTS - pending.attempts,
      };
    }
    const token = randomBytes(32).toString('hex');
    this.setTokenHash(hashToken(token));
    this.pending = null;
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

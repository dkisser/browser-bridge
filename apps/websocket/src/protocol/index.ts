import type { Envelope } from '@browser-bridge/shared/types';

export type { Envelope };

export interface EncodeOptions {
  id?: string;
  browserId?: string;
}

export function encode(
  type: Envelope['type'],
  payload: unknown,
  opts: EncodeOptions = {},
): string {
  return JSON.stringify({
    id: opts.id ?? crypto.randomUUID(),
    type,
    browserId: opts.browserId ?? '',
    payload,
    timestamp: Date.now(),
  });
}

export function decode(raw: string): Envelope {
  return JSON.parse(raw) as Envelope;
}

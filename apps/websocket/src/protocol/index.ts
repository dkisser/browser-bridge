export interface Envelope {
  type: string;
  data: unknown;
}

export function encode(type: string, data: unknown): string {
  return JSON.stringify({ type, data });
}

export function decode(raw: string): Envelope {
  return JSON.parse(raw) as Envelope;
}

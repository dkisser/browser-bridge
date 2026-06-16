import { describe, expect, it } from 'bun:test';
import { encode, decode } from '../index';

describe('protocol', () => {
  describe('encode', () => {
    it('creates envelope with id, type, browserId, payload, timestamp', () => {
      const result = JSON.parse(encode('command', { command: 'navigate', params: { url: 'https://example.com' } }, { browserId: 'b-123' }));
      expect(result.id).toBeDefined();
      expect(result.type).toBe('command');
      expect(result.browserId).toBe('b-123');
      expect(result.payload).toEqual({ command: 'navigate', params: { url: 'https://example.com' } });
      expect(result.timestamp).toBeGreaterThan(0);
    });

    it('uses provided id when given', () => {
      const result = JSON.parse(encode('response', { status: 'ok' }, { id: 'custom-id' }));
      expect(result.id).toBe('custom-id');
    });

    it('defaults browserId to empty string', () => {
      const result = JSON.parse(encode('command', {}));
      expect(result.browserId).toBe('');
    });
  });

  describe('decode', () => {
    it('parses envelope JSON', () => {
      const envelope = { id: 'abc', type: 'command' as const, browserId: 'b-1', payload: { x: 1 }, timestamp: 1000 };
      const result = decode(JSON.stringify(envelope));
      expect(result).toEqual(envelope);
    });

    it('round-trips with encode', () => {
      const original = encode('response', { status: 'ok', data: 'hello' }, { id: 'test-id', browserId: 'b-99' });
      const decoded = decode(original);
      expect(decoded.id).toBe('test-id');
      expect(decoded.type).toBe('response');
      expect(decoded.browserId).toBe('b-99');
      expect(decoded.payload).toEqual({ status: 'ok', data: 'hello' });
    });
  });
});

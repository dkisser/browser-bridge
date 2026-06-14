import { describe, expect, it } from 'bun:test';
import { WEBSOCKET_PORT, LOCAL_WS_PORT, NoopAuthProvider, ApiKeyAuthProvider } from '../index';

describe('shared', () => {
  describe('constants', () => {
    it('exports WEBSOCKET_PORT', () => {
      expect(WEBSOCKET_PORT).toBe(3001);
    });

    it('exports LOCAL_WS_PORT', () => {
      expect(LOCAL_WS_PORT).toBe(3002);
    });
  });

  describe('NoopAuthProvider', () => {
    it('validates any token', async () => {
      const provider = new NoopAuthProvider();
      const result = await provider.validateToken('anything');
      expect(result.valid).toBe(true);
      expect(result.userId).toBe('local');
    });
  });

  describe('ApiKeyAuthProvider', () => {
    it('validates correct API key', async () => {
      const provider = new ApiKeyAuthProvider({ 'key-123': 'user-1' });
      const result = await provider.validateToken('key-123');
      expect(result.valid).toBe(true);
      expect(result.userId).toBe('user-1');
    });

    it('rejects invalid API key', async () => {
      const provider = new ApiKeyAuthProvider({ 'key-123': 'user-1' });
      const result = await provider.validateToken('wrong');
      expect(result.valid).toBe(false);
    });
  });
});

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

    it('returns id "noop"', () => {
      const provider = new NoopAuthProvider();
      expect(provider.id).toBe('noop');
    });

    it('refreshToken returns the same token', async () => {
      const provider = new NoopAuthProvider();
      const token = { accessToken: 'a', refreshToken: 'r', expiresAt: 0, userId: 'u' };
      const result = await provider.refreshToken(token);
      expect(result).toEqual(token);
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

    it('returns id "api-key"', () => {
      const provider = new ApiKeyAuthProvider({});
      expect(provider.id).toBe('api-key');
    });

    it('refreshToken returns the same token', async () => {
      const provider = new ApiKeyAuthProvider({});
      const token = { accessToken: 'a', refreshToken: 'r', expiresAt: 0, userId: 'u' };
      const result = await provider.refreshToken(token);
      expect(result).toEqual(token);
    });
  });
});

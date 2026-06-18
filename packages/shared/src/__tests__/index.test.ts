import { describe, expect, it } from 'bun:test';
import {
  ApiKeyAuthProvider,
  LOCAL_WS_PORT,
  NoopAuthProvider,
  WEBSOCKET_PORT,
} from '../index';
import { isLocalhost } from '../utils';

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
      const token = {
        accessToken: 'a',
        refreshToken: 'r',
        expiresAt: 0,
        userId: 'u',
      };
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
      const token = {
        accessToken: 'a',
        refreshToken: 'r',
        expiresAt: 0,
        userId: 'u',
      };
      const result = await provider.refreshToken(token);
      expect(result).toEqual(token);
    });

    it('validateHeader accepts valid Bearer token', async () => {
      const provider = new ApiKeyAuthProvider({ 'key-123': 'user-1' });
      const result = await provider.validateHeader('Bearer key-123');
      expect(result.valid).toBe(true);
      expect(result.userId).toBe('user-1');
    });

    it('validateHeader rejects invalid Bearer token', async () => {
      const provider = new ApiKeyAuthProvider({ 'key-123': 'user-1' });
      const result = await provider.validateHeader('Bearer wrong');
      expect(result.valid).toBe(false);
    });

    it('validateHeader rejects malformed header', async () => {
      const provider = new ApiKeyAuthProvider({ 'key-123': 'user-1' });
      const result = await provider.validateHeader('key-123');
      expect(result.valid).toBe(false);
    });

    it('validateHeader rejects empty string', async () => {
      const provider = new ApiKeyAuthProvider({ 'key-123': 'user-1' });
      const result = await provider.validateHeader('');
      expect(result.valid).toBe(false);
    });

    it('accepts string array of keys', async () => {
      const provider = new ApiKeyAuthProvider(['key-a', 'key-b']);
      const resultA = await provider.validateToken('key-a');
      expect(resultA.valid).toBe(true);
      const resultB = await provider.validateToken('key-b');
      expect(resultB.valid).toBe(true);
      const resultC = await provider.validateToken('key-c');
      expect(resultC.valid).toBe(false);
    });
  });

  describe('isLocalhost', () => {
    it('recognizes localhost', () => {
      expect(isLocalhost('localhost')).toBe(true);
    });

    it('recognizes 127.0.0.1', () => {
      expect(isLocalhost('127.0.0.1')).toBe(true);
    });

    it('recognizes [::1]', () => {
      expect(isLocalhost('[::1]')).toBe(true);
    });

    it('recognizes ::1 without brackets', () => {
      expect(isLocalhost('::1')).toBe(true);
    });

    it('rejects remote host', () => {
      expect(isLocalhost('example.com')).toBe(false);
    });

    it('rejects IP that is not loopback', () => {
      expect(isLocalhost('192.168.1.1')).toBe(false);
    });
  });
});

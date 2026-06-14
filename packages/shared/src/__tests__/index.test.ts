import { describe, expect, it } from 'bun:test';
import { WEBSOCKET_PORT } from '../index';

describe('shared', () => {
  describe('WEBSOCKET_PORT', () => {
    it('exports the expected default port', () => {
      expect(WEBSOCKET_PORT).toBe(3001);
    });
  });
});

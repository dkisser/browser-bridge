import { describe, expect, it } from 'bun:test';
import { toErrorMessage } from '../src/ui/format';

describe('toErrorMessage', () => {
  it('extracts message from an Error instance', () => {
    expect(toErrorMessage(new Error('boom'))).toBe('boom');
  });

  it('extracts message from an Error subclass', () => {
    class CustomError extends Error {
      constructor(message: string) {
        super(message);
        this.name = 'CustomError';
      }
    }
    expect(toErrorMessage(new CustomError('nope'))).toBe('nope');
  });

  it('falls back to String() for non-Error values', () => {
    expect(toErrorMessage('plain string')).toBe('plain string');
    expect(toErrorMessage(42)).toBe('42');
    expect(toErrorMessage(null)).toBe('null');
    expect(toErrorMessage(undefined)).toBe('undefined');
  });

  it('handles objects via String() coercion', () => {
    // String({a: 1}) === '[object Object]' — this is the same behavior the
    // five inline copies had, and the test pins it so any future change is
    // intentional.
    expect(toErrorMessage({ a: 1 })).toBe('[object Object]');
  });
});

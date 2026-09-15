import { describe, expect, it } from 'bun:test';
import {
  selectorMatchedButEmptyMessage,
  selectorNotFoundMessage,
} from '../src/messages';

describe('selectorNotFoundMessage', () => {
  it('names the selector and both lookup attempts', () => {
    const message = selectorNotFoundMessage('table tr');

    expect(message).toContain('table tr');
    expect(message).toContain('CSS selector');
    expect(message).toContain('visible text');
  });

  it('points at virtualized lists and snapshot as the next step', () => {
    const message = selectorNotFoundMessage('.bog, .y6');

    expect(message).toContain('virtualized');
    expect(message).toContain('snapshot');
  });
});

describe('selectorMatchedButEmptyMessage', () => {
  it('states the element matched but has no text', () => {
    const message = selectorMatchedButEmptyMessage('[role="row"]');

    expect(message).toContain('[role="row"]');
    expect(message).toContain('matched');
    expect(message).toContain('no text content');
  });

  it('differs from the not-found message so callers can tell them apart', () => {
    const selector = '.y6';
    const empty = selectorMatchedButEmptyMessage(selector);
    const notFound = selectorNotFoundMessage(selector);

    expect(empty).not.toBe(notFound);
    expect(notFound).not.toContain('has no text content');
    expect(empty).not.toContain('No element found');
  });
});

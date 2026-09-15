import { describe, expect, it } from 'bun:test';
import {
  selectorMatchedButEmptyMessage,
  selectorNotFoundMessage,
  textContainerCandidatesHint,
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

  it('tells the caller how to recover: snapshot, ref, and tab check', () => {
    const message = selectorNotFoundMessage('article');

    expect(message).toContain('@eN');
    expect(message).toContain('tab_id');
  });
});

describe('textContainerCandidatesHint', () => {
  it('lists candidates as a numbered block with a re-run prompt', () => {
    const hint = textContainerCandidatesHint([
      '#main-content (~8.4K chars)',
      'div.post__body (~8.1K chars)',
    ]);

    expect(hint).toContain('Largest text containers on this page:');
    expect(hint).toContain('1. #main-content (~8.4K chars)');
    expect(hint).toContain('2. div.post__body (~8.1K chars)');
    expect(hint).toContain('Re-run with one of these selectors.');
  });

  it('returns empty string when there is nothing to suggest', () => {
    expect(textContainerCandidatesHint([])).toBe('');
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

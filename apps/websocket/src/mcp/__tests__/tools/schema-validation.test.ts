import { describe, expect, it } from 'bun:test';
import { ClickInputSchema } from '../../tools/click';
import { NavigateInputSchema } from '../../tools/navigate';

describe('tool schemas require tab_id', () => {
  it('NavigateInputSchema rejects missing tab_id', () => {
    const result = NavigateInputSchema.safeParse({
      url: 'https://example.com',
    });
    expect(result.success).toBe(false);
    if (!result.success) {
      expect(
        result.error.issues.some((issue) => issue.path.join('.') === 'tab_id'),
      ).toBe(true);
    }
  });

  it('ClickInputSchema rejects missing tab_id', () => {
    const result = ClickInputSchema.safeParse({ selector: 'button' });
    expect(result.success).toBe(false);
    if (!result.success) {
      expect(
        result.error.issues.some((issue) => issue.path.join('.') === 'tab_id'),
      ).toBe(true);
    }
  });
});

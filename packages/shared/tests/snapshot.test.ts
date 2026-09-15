import { describe, expect, it } from 'bun:test';
import {
  DEFAULT_SNAPSHOT_MAX_CHARS,
  renderSnapshotTree,
  type SnapshotNode,
} from '../src/snapshot';

const META = { title: 'Example', url: 'https://example.com' };

function node(
  partial: Partial<SnapshotNode> & { role: SnapshotNode['role'] },
): SnapshotNode {
  return { children: [], ...partial };
}

function text(content: string): SnapshotNode {
  return node({ role: 'text', text: content });
}

describe('renderSnapshotTree', () => {
  it('starts with the metadata line', () => {
    const result = renderSnapshotTree(node({ role: 'generic' }), 3000, META);
    expect(result.snapshot).toBe('Page: Example | https://example.com');
  });

  it('formats heading with level and name', () => {
    const tree = node({
      role: 'generic',
      children: [node({ role: 'heading', level: 1, name: 'Welcome' })],
    });
    const result = renderSnapshotTree(tree, 3000, META);
    expect(result.snapshot).toContain('heading(1) [Welcome]');
  });

  it('formats link href and img src as attrs', () => {
    const tree = node({
      role: 'generic',
      children: [
        node({
          role: 'link',
          name: 'Docs',
          attrs: { href: '/docs' },
          ref: 'e1',
        }),
        node({ role: 'img', attrs: { src: 'logo.png' }, ref: 'e2' }),
      ],
    });
    const result = renderSnapshotTree(tree, 3000, META);
    const [meta, link, img] = result.snapshot.split('\n');
    expect(meta).toBe('Page: Example | https://example.com');
    expect(link).toBe('link [Docs] href="/docs" @e1');
    expect(img).toBe('img src="logo.png" @e2');
  });

  it('indents two spaces per level', () => {
    const tree = node({
      role: 'generic',
      children: [
        node({
          role: 'list',
          children: [node({ role: 'listitem', children: [text('Item one')] })],
        }),
      ],
    });
    const result = renderSnapshotTree(tree, 3000, META);
    expect(result.snapshot).toBe(
      [
        'Page: Example | https://example.com',
        'list',
        '  listitem',
        '    text [Item one]',
      ].join('\n'),
    );
  });

  it('truncates attr values at 60 chars with an ellipsis', () => {
    const long = 'v'.repeat(70);
    const tree = node({
      role: 'generic',
      children: [node({ role: 'link', name: 'x', attrs: { href: long } })],
    });
    const result = renderSnapshotTree(tree, 3000, META);
    expect(result.snapshot).toContain(`href="${'v'.repeat(60)}…"`);
  });

  it('normalizes whitespace in names', () => {
    const tree = node({
      role: 'generic',
      children: [node({ role: 'button', name: '  Buy\n now  ' })],
    });
    const result = renderSnapshotTree(tree, 3000, META);
    expect(result.snapshot).toContain('button [Buy now]');
  });

  it('never emits empty non-interactive containers', () => {
    const tree = node({
      role: 'generic',
      children: [node({ role: 'generic' })],
    });
    const result = renderSnapshotTree(tree, 3000, META);
    expect(result.snapshot).toBe('Page: Example | https://example.com');
    expect(result.nodes_total).toBe(2);
    expect(result.nodes_emitted).toBe(0);
  });

  it('counts all tree nodes in nodes_total and lines in nodes_emitted', () => {
    const tree = node({
      role: 'generic',
      children: [
        node({ role: 'button', name: 'OK' }),
        node({ role: 'generic' }), // visited but never emitted
        node({ role: 'text', text: 'hi' }),
      ],
    });
    const result = renderSnapshotTree(tree, 3000, META);
    expect(result.nodes_total).toBe(4);
    expect(result.nodes_emitted).toBe(2);
    expect(result.truncated).toBe(false);
  });

  it('falls back to tier 1 and truncates text runs to 40 chars', () => {
    const longText = 'word '.repeat(20).trim(); // 99 chars
    const tree = node({
      role: 'generic',
      children: [text(longText)],
    });
    const result = renderSnapshotTree(tree, 50, META);
    expect(result.truncated).toBe(true);
    expect(result.snapshot).toContain(`text [${longText.slice(0, 40)}…]`);
    expect(result.nodes_emitted).toBe(1);
  });

  it('falls back to tier 2 and drops text lines but keeps interactive names', () => {
    const longText = 'word '.repeat(20).trim();
    const tree = node({
      role: 'generic',
      children: [
        node({ role: 'button', name: 'Submit order' }),
        text(longText),
      ],
    });
    const result = renderSnapshotTree(tree, 30, META);
    expect(result.truncated).toBe(true);
    expect(result.snapshot).toContain('button [Submit order]');
    expect(result.snapshot).not.toContain('text [');
    expect(result.nodes_emitted).toBe(1);
  });

  it('hard-truncates with the marker when tier 2 still exceeds the budget', () => {
    const tree = node({
      role: 'generic',
      children: [
        node({ role: 'button', name: 'First button here' }),
        node({ role: 'button', name: 'Second button here' }),
        node({ role: 'button', name: 'Third button here' }),
      ],
    });
    const maxChars = 20;
    const result = renderSnapshotTree(tree, maxChars, META);
    expect(result.truncated).toBe(true);
    expect(result.snapshot.endsWith('\n... [truncated]')).toBe(true);
    const body = result.snapshot.slice(result.snapshot.indexOf('\n') + 1);
    expect(body.length).toBe(maxChars + '\n... [truncated]'.length);
  });

  it('keeps the metadata line outside the budget', () => {
    const tree = node({
      role: 'generic',
      children: [node({ role: 'button', name: 'A very long button name' })],
    });
    const result = renderSnapshotTree(tree, 5, META);
    expect(
      result.snapshot.startsWith('Page: Example | https://example.com\n'),
    ).toBe(true);
  });

  it('handles an empty page (bare generic root)', () => {
    const result = renderSnapshotTree(node({ role: 'generic' }), 3000, META);
    expect(result.snapshot).toBe('Page: Example | https://example.com');
    expect(result.nodes_total).toBe(1);
    expect(result.nodes_emitted).toBe(0);
    expect(result.truncated).toBe(false);
  });

  it('defaults the budget to 3000 chars', () => {
    expect(DEFAULT_SNAPSHOT_MAX_CHARS).toBe(3000);
    const under = node({
      role: 'generic',
      children: [text('a'.repeat(2900))],
    });
    expect(renderSnapshotTree(under, undefined, META).truncated).toBe(false);
    const over = node({
      role: 'generic',
      children: [text('a'.repeat(3100))],
    });
    const result = renderSnapshotTree(over, undefined, META);
    expect(result.truncated).toBe(true);
    expect(result.snapshot).toContain(`text [${'a'.repeat(40)}…]`);
  });
});

import { describe, expect, it } from 'bun:test';
import {
  DEFAULT_INTERACTIVE_MAX_CHARS,
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

  it('formats link href as attr and img line with alt name and ref', () => {
    const tree = node({
      role: 'generic',
      children: [
        node({
          role: 'link',
          name: 'Docs',
          attrs: { href: '/docs' },
          ref: 'e1',
        }),
        node({ role: 'img', name: 'Logo', ref: 'e2' }),
      ],
    });
    const result = renderSnapshotTree(tree, 3000, META, 'full');
    const [meta, link, img] = result.snapshot.split('\n');
    expect(meta).toBe('Page: Example | https://example.com');
    expect(link).toBe('link [Docs] href="/docs" @e1');
    expect(img).toBe('img [Logo] @e2');
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
    const result = renderSnapshotTree(tree, 3000, META, 'full');
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
    const result = renderSnapshotTree(tree, 3000, META, 'full');
    expect(result.nodes_total).toBe(4);
    expect(result.nodes_emitted).toBe(2);
    expect(result.truncated).toBe(false);
    expect(result.tier).toBe(0);
  });

  it('falls back to tier 1 and truncates text runs to 40 chars', () => {
    const longText = 'word '.repeat(20).trim(); // 99 chars
    const tree = node({
      role: 'generic',
      children: [text(longText)],
    });
    const result = renderSnapshotTree(tree, 50, META, 'full');
    expect(result.truncated).toBe(true);
    expect(result.tier).toBe(1);
    expect(result.snapshot).toContain(`text [${longText.slice(0, 40)}…]`);
    expect(result.nodes_emitted).toBe(1);
  });

  it('falls back to tier 2, marking suppressed text with a placeholder', () => {
    const longText = 'word '.repeat(20).trim();
    const tree = node({
      role: 'generic',
      children: [node({ role: 'button', name: 'Submit' }), text(longText)],
    });
    const result = renderSnapshotTree(tree, 40, META, 'full');
    expect(result.truncated).toBe(true);
    expect(result.tier).toBe(2);
    expect(result.snapshot).toContain('button [Submit]');
    expect(result.snapshot).toContain('text [text suppressed]');
    expect(result.snapshot).not.toContain('word ');
    expect(result.nodes_emitted).toBe(2);
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

  it('defaults the budget by filter: 3000 for full, 8000 for interactive', () => {
    expect(DEFAULT_SNAPSHOT_MAX_CHARS).toBe(3000);
    expect(DEFAULT_INTERACTIVE_MAX_CHARS).toBe(8000);
    const bigName = node({
      role: 'generic',
      children: [node({ role: 'button', name: 'x'.repeat(5000) })],
    });
    expect(renderSnapshotTree(bigName, undefined, META, 'full').truncated).toBe(
      true,
    );
    expect(renderSnapshotTree(bigName, undefined, META).truncated).toBe(false);
  });

  describe('interactive filter (the default)', () => {
    it('surfaces nested interactive children at re-based depth', () => {
      const tree = node({
        role: 'generic',
        children: [
          node({
            role: 'table',
            children: [
              node({
                role: 'row',
                children: [
                  node({
                    role: 'cell',
                    children: [
                      node({
                        role: 'link',
                        name: 'Open',
                        attrs: { href: '/x' },
                        ref: 'e1',
                      }),
                    ],
                  }),
                ],
              }),
            ],
          }),
          text('body copy here'),
        ],
      });
      const result = renderSnapshotTree(tree, 3000, META);
      expect(result.snapshot).toBe(
        [
          'Page: Example | https://example.com',
          'link [Open] href="/x" @e1',
        ].join('\n'),
      );
    });

    it('keeps named generics but drops images', () => {
      const tree = node({
        role: 'generic',
        children: [
          node({ role: 'generic', name: 'Inbox', ref: 'e2' }),
          node({ role: 'img', attrs: { src: 'logo.png' }, ref: 'e3' }),
        ],
      });
      const result = renderSnapshotTree(tree, 3000, META);
      expect(result.snapshot).toBe(
        ['Page: Example | https://example.com', 'generic [Inbox] @e2'].join(
          '\n',
        ),
      );
    });

    it('drops text runs without emitting tier-2 placeholders', () => {
      const tree = node({
        role: 'generic',
        children: [text('word '.repeat(20).trim())],
      });
      const result = renderSnapshotTree(tree, 30, META);
      expect(result.snapshot).toBe('Page: Example | https://example.com');
      expect(result.nodes_emitted).toBe(0);
      expect(result.snapshot).not.toContain('text suppressed');
    });
  });

  describe('full filter: wrapper generic collapsing', () => {
    it('collapses chains of single-child nameless generics', () => {
      const tree = node({
        role: 'generic',
        children: [
          node({
            role: 'generic',
            children: [
              node({
                role: 'generic',
                children: [node({ role: 'button', name: 'OK' })],
              }),
            ],
          }),
        ],
      });
      const result = renderSnapshotTree(tree, 3000, META, 'full');
      expect(result.snapshot).toBe(
        ['Page: Example | https://example.com', 'button [OK]'].join('\n'),
      );
      expect(result.nodes_total).toBe(4);
      expect(result.nodes_emitted).toBe(1);
    });

    it('keeps ref-bearing generics addressable', () => {
      const tree = node({
        role: 'generic',
        children: [
          node({
            role: 'generic',
            ref: 'e5',
            children: [node({ role: 'button', name: 'OK' })],
          }),
        ],
      });
      const result = renderSnapshotTree(tree, 3000, META, 'full');
      expect(result.snapshot).toBe(
        ['Page: Example | https://example.com', 'generic @e5', '  button [OK]'].join(
          '\n',
        ),
      );
    });

    it('keeps named generics and multi-child generics as structure', () => {
      const tree = node({
        role: 'generic',
        children: [
          node({
            role: 'generic',
            name: 'Inbox',
            children: [node({ role: 'button', name: 'Open' })],
          }),
          node({
            role: 'generic',
            children: [text('one'), text('two')],
          }),
        ],
      });
      const result = renderSnapshotTree(tree, 3000, META, 'full');
      expect(result.snapshot).toBe(
        [
          'Page: Example | https://example.com',
          'generic [Inbox]',
          '  button [Open]',
          'generic',
          '  text [one]',
          '  text [two]',
        ].join('\n'),
      );
    });

    it('does not collapse wrappers in the interactive filter', () => {
      const tree = node({
        role: 'generic',
        children: [
          node({
            role: 'generic',
            children: [node({ role: 'button', name: 'OK' })],
          }),
        ],
      });
      const result = renderSnapshotTree(tree, 3000, META, 'interactive');
      expect(result.snapshot).toBe(
        ['Page: Example | https://example.com', 'button [OK]'].join('\n'),
      );
    });
  });
});

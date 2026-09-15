// Pure snapshot serializer: renders an intermediate SnapshotNode tree into the
// compact text representation consumed by agents. No DOM access here — the
// content script builds the tree; this module only renders, applies the
// filter (interactive by default, full on request), and enforces the
// character budget via deterministic tier fallback.

export type SnapshotRole =
  | 'heading'
  | 'link'
  | 'button'
  | 'textbox'
  | 'checkbox'
  | 'combobox'
  | 'img'
  | 'list'
  | 'listitem'
  | 'table'
  | 'row'
  | 'cell'
  | 'text'
  | 'generic';

export interface SnapshotNode {
  role: SnapshotRole;
  level?: number;
  name?: string;
  attrs?: Record<string, string>;
  ref?: string;
  text?: string;
  children: SnapshotNode[];
}

export interface SnapshotMeta {
  title: string;
  url: string;
}

export interface SnapshotResult {
  snapshot: string;
  truncated: boolean;
  nodes_total: number;
  nodes_emitted: number;
  tier: SnapshotTier;
}

export type SnapshotFilter = 'interactive' | 'full';

export type SnapshotTier = 0 | 1 | 2;

export const DEFAULT_SNAPSHOT_MAX_CHARS = 3000;
export const DEFAULT_INTERACTIVE_MAX_CHARS = 8000;

export function defaultMaxCharsForFilter(filter: SnapshotFilter): number {
  return filter === 'full'
    ? DEFAULT_SNAPSHOT_MAX_CHARS
    : DEFAULT_INTERACTIVE_MAX_CHARS;
}

const ATTR_VALUE_MAX_CHARS = 60;
const TIER1_TEXT_MAX_CHARS = 40;
const HARD_TRUNCATION_MARKER = '\n... [truncated]';
const TIER2_TEXT_PLACEHOLDER = 'text [text suppressed]';
const INDENT = '  ';

const INTERACTIVE_ROLES: ReadonlySet<SnapshotRole> = new Set([
  'link',
  'button',
  'textbox',
  'checkbox',
  'combobox',
]);

function displayName(node: SnapshotNode): string | undefined {
  return node.name ?? (node.role === 'text' ? node.text : undefined);
}

function hasAttrs(node: SnapshotNode): boolean {
  return node.attrs !== undefined && Object.keys(node.attrs).length > 0;
}

// Empty non-interactive containers are never emitted.
function isEmittable(node: SnapshotNode): boolean {
  if (node.role !== 'generic') return true;
  if (displayName(node) !== undefined || hasAttrs(node)) return true;
  return node.children.some(isEmittable);
}

// Interactive-filter pass: interactive elements and headings always emit;
// named or attributed generics often act as clickable nav items (e.g. Gmail
// labels), so they stay addressable. Text runs, images and bare structural
// containers are dropped as lines, but their descendants are still walked.
function isInteractiveKept(node: SnapshotNode): boolean {
  if (INTERACTIVE_ROLES.has(node.role)) return true;
  if (node.role === 'heading') return true;
  if (node.role === 'generic') {
    return displayName(node) !== undefined || hasAttrs(node);
  }
  return false;
}

function truncateChars(value: string, maxChars: number): string {
  if (value.length <= maxChars) return value;
  return `${value.slice(0, maxChars)}…`;
}

function formatAttrs(node: SnapshotNode): string {
  if (!node.attrs) return '';
  let out = '';
  for (const [key, value] of Object.entries(node.attrs)) {
    out += ` ${key}="${truncateChars(value, ATTR_VALUE_MAX_CHARS)}"`;
  }
  return out;
}

function formatName(node: SnapshotNode, tier: SnapshotTier): string {
  const name = displayName(node);
  if (name === undefined) return '';
  const normalized = name.replace(/\s+/g, ' ').trim();
  if (tier >= 1 && node.role === 'text') {
    return ` [${truncateChars(normalized, TIER1_TEXT_MAX_CHARS)}]`;
  }
  return normalized === '' ? '' : ` [${normalized}]`;
}

function formatLine(
  node: SnapshotNode,
  depth: number,
  tier: SnapshotTier,
): string {
  const role = node.role + (node.level !== undefined ? `(${node.level})` : '');
  const ref = node.ref !== undefined ? ` @${node.ref}` : '';
  return `${INDENT.repeat(depth)}${role}${formatName(node, tier)}${formatAttrs(node)}${ref}`;
}

interface Rendered {
  body: string;
  nodesEmitted: number;
}

function renderNode(
  node: SnapshotNode,
  depth: number,
  tier: SnapshotTier,
  filter: SnapshotFilter,
  lines: string[],
): void {
  const suppressedByTier =
    filter === 'full' && tier >= 2 && node.role === 'text';
  const emit =
    !suppressedByTier &&
    (filter === 'full' ? isEmittable(node) : isInteractiveKept(node));
  if (emit) {
    lines.push(formatLine(node, depth, tier));
  } else if (suppressedByTier) {
    // Leave a placeholder so a suppressed cell is distinguishable from one
    // that was genuinely empty.
    lines.push(`${INDENT.repeat(depth)}${TIER2_TEXT_PLACEHOLDER}`);
  }
  const childDepth = emit ? depth + 1 : depth;
  for (const child of node.children) {
    renderNode(child, childDepth, tier, filter, lines);
  }
}

function countNodes(node: SnapshotNode): number {
  let total = 1;
  for (const child of node.children) {
    total += countNodes(child);
  }
  return total;
}

// Full filter only: a nameless, attr-less, ref-less generic with exactly one
// child is a pure wrapper — its line carries no information. Replace it with
// its child so chains of layout divs collapse into the content they wrap.
// Refs are never collapsed away: an addressable line always survives.
function collapseWrapperGenerics(node: SnapshotNode): SnapshotNode {
  const children = node.children.map(collapseWrapperGenerics);
  let current: SnapshotNode = { ...node, children };
  while (
    current.role === 'generic' &&
    displayName(current) === undefined &&
    !hasAttrs(current) &&
    current.ref === undefined &&
    current.children.length === 1
  ) {
    current = current.children[0];
  }
  return current;
}

function renderAtTier(
  root: SnapshotNode,
  tier: SnapshotTier,
  filter: SnapshotFilter,
): Rendered {
  const lines: string[] = [];
  // A nameless, attr-less generic root (e.g. document.body) is a pure
  // container: skip its own line so children start at depth 0.
  const suppressRoot =
    root.role === 'generic' &&
    displayName(root) === undefined &&
    !hasAttrs(root) &&
    root.ref === undefined;
  if (suppressRoot) {
    for (const child of root.children) {
      renderNode(child, 0, tier, filter, lines);
    }
  } else {
    renderNode(root, 0, tier, filter, lines);
  }
  return { body: lines.join('\n'), nodesEmitted: lines.length };
}

export function renderSnapshotTree(
  root: SnapshotNode,
  maxChars?: number,
  meta?: SnapshotMeta,
  filter: SnapshotFilter = 'interactive',
): SnapshotResult {
  const budget = maxChars ?? defaultMaxCharsForFilter(filter);
  const nodesTotal = countNodes(root);
  const tree = filter === 'full' ? collapseWrapperGenerics(root) : root;
  const metaLine =
    meta !== undefined ? `Page: ${meta.title} | ${meta.url}` : '';
  const prefix = metaLine === '' ? '' : `${metaLine}\n`;

  let tier: SnapshotTier = 0;
  let { body, nodesEmitted } = renderAtTier(tree, tier, filter);
  let truncated = false;

  if (body.length > budget) {
    truncated = true;
    tier = 1;
    ({ body, nodesEmitted } = renderAtTier(tree, tier, filter));
  }
  if (body.length > budget) {
    tier = 2;
    ({ body, nodesEmitted } = renderAtTier(tree, tier, filter));
  }
  if (body.length > budget) {
    const cut = body.slice(0, budget);
    nodesEmitted =
      cut === '' ? 0 : cut.split('\n').length - (cut.endsWith('\n') ? 1 : 0);
    body = `${cut}${HARD_TRUNCATION_MARKER}`;
  }

  return {
    snapshot: body === '' ? prefix.trimEnd() : `${prefix}${body}`,
    truncated,
    nodes_total: nodesTotal,
    nodes_emitted: nodesEmitted,
    tier,
  };
}

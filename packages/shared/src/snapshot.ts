// Pure snapshot serializer: renders an intermediate SnapshotNode tree into the
// compact text representation consumed by agents. No DOM access here — the
// content script builds the tree; this module only renders and enforces the
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
}

export const DEFAULT_SNAPSHOT_MAX_CHARS = 3000;

const ATTR_VALUE_MAX_CHARS = 60;
const TIER1_TEXT_MAX_CHARS = 40;
const HARD_TRUNCATION_MARKER = '\n... [truncated]';
const INDENT = '  ';

type Tier = 0 | 1 | 2;

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

function formatName(node: SnapshotNode, tier: Tier): string {
  const name = displayName(node);
  if (name === undefined) return '';
  const normalized = name.replace(/\s+/g, ' ').trim();
  if (tier >= 1 && node.role === 'text') {
    return ` [${truncateChars(normalized, TIER1_TEXT_MAX_CHARS)}]`;
  }
  return normalized === '' ? '' : ` [${normalized}]`;
}

function formatLine(node: SnapshotNode, depth: number, tier: Tier): string {
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
  tier: Tier,
  lines: string[],
): void {
  if (!isEmittable(node)) return;
  if (tier >= 2 && node.role === 'text') return;
  lines.push(formatLine(node, depth, tier));
  for (const child of node.children) {
    renderNode(child, depth + 1, tier, lines);
  }
}

function countNodes(node: SnapshotNode): number {
  let total = 1;
  for (const child of node.children) {
    total += countNodes(child);
  }
  return total;
}

function renderAtTier(root: SnapshotNode, tier: Tier): Rendered {
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
      renderNode(child, 0, tier, lines);
    }
  } else {
    renderNode(root, 0, tier, lines);
  }
  return { body: lines.join('\n'), nodesEmitted: lines.length };
}

export function renderSnapshotTree(
  root: SnapshotNode,
  maxChars: number = DEFAULT_SNAPSHOT_MAX_CHARS,
  meta?: SnapshotMeta,
): SnapshotResult {
  const nodesTotal = countNodes(root);
  const metaLine =
    meta !== undefined ? `Page: ${meta.title} | ${meta.url}` : '';
  const prefix = metaLine === '' ? '' : `${metaLine}\n`;

  let { body, nodesEmitted } = renderAtTier(root, 0);
  let truncated = false;

  if (body.length > maxChars) {
    truncated = true;
    ({ body, nodesEmitted } = renderAtTier(root, 1));
  }
  if (body.length > maxChars) {
    ({ body, nodesEmitted } = renderAtTier(root, 2));
  }
  if (body.length > maxChars) {
    const cut = body.slice(0, maxChars);
    nodesEmitted =
      cut === '' ? 0 : cut.split('\n').length - (cut.endsWith('\n') ? 1 : 0);
    body = `${cut}${HARD_TRUNCATION_MARKER}`;
  }

  return {
    snapshot: body === '' ? prefix.trimEnd() : `${prefix}${body}`,
    truncated,
    nodes_total: nodesTotal,
    nodes_emitted: nodesEmitted,
  };
}

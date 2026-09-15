import {
  type DomCommandResult,
  type DomCommandType,
  defaultMaxCharsForFilter,
  renderSnapshotTree,
  selectorNotFoundMessage,
  type SnapshotFilter,
  type SnapshotNode,
  type SnapshotRole,
  type WaitElementResult,
} from '@browser-bridge/shared';

interface DomCommand {
  command: DomCommandType;
  tabId?: number;
  params: Record<string, unknown>;
}

function querySelector(selector: string): Element {
  const el = document.querySelector(selector);
  if (!el) throw new Error(`Element not found: ${selector}`);
  return el;
}

function querySelectorByText(text: string): Element {
  const walker = document.createTreeWalker(
    document.body,
    NodeFilter.SHOW_ELEMENT,
  );
  let node: Element | null = walker.nextNode() as Element | null;
  while (node) {
    if (node.textContent?.trim() === text) return node;
    node = walker.nextNode() as Element | null;
  }
  throw new Error(`Element with text not found: ${text}`);
}

function resolveSelector(selector: string): Element {
  if (selector.startsWith('@')) {
    const ref = selector.slice(1);
    if (!/^e\d+$/.test(ref)) {
      throw new Error(`Invalid ref selector: ${selector} (expected @e<N>)`);
    }
    const el = document.querySelector(`[data-bb-ref="${ref}"]`);
    if (!el) {
      throw new Error(
        `Ref ${selector} not found on page. Take a fresh snapshot: refs are re-assigned on each snapshot.`,
      );
    }
    return el;
  }
  try {
    return querySelector(selector);
  } catch {
    try {
      return querySelectorByText(selector);
    } catch {
      throw new Error(selectorNotFoundMessage(selector));
    }
  }
}

const SKIP_SUBTREE_TAGS = new Set([
  'SCRIPT',
  'STYLE',
  'HEAD',
  'NOSCRIPT',
  'TEMPLATE',
  'SVG',
  'MATH',
]);
const MAX_VISITED_NODES = 20000;
const TEXT_INPUT_TYPES = new Set([
  'text',
  'search',
  'email',
  'url',
  'tel',
  'password',
  'number',
]);
const REF_ROLES = new Set<SnapshotRole>([
  'link',
  'button',
  'textbox',
  'checkbox',
  'combobox',
  'heading',
  'img',
]);

interface WalkState {
  visited: number;
  refCounter: number;
  truncated: boolean;
}

function collapseWhitespace(value: string): string {
  return value.replace(/\s+/g, ' ').trim();
}

function mapRole(el: Element): { role: SnapshotRole; level?: number } {
  const tag = el.tagName.toLowerCase();
  if (/^h[1-6]$/.test(tag)) return { role: 'heading', level: Number(tag[1]) };
  if (tag === 'a' && el.hasAttribute('href')) return { role: 'link' };
  if (tag === 'button') return { role: 'button' };
  if (tag === 'textarea') return { role: 'textbox' };
  if (tag === 'select') return { role: 'combobox' };
  if (tag === 'img') return { role: 'img' };
  if (tag === 'ul' || tag === 'ol') return { role: 'list' };
  if (tag === 'li') return { role: 'listitem' };
  if (tag === 'table') return { role: 'table' };
  if (tag === 'tr') return { role: 'row' };
  if (tag === 'td' || tag === 'th') return { role: 'cell' };
  if (tag === 'input') {
    const type = (el.getAttribute('type') ?? 'text').toLowerCase();
    if (type === 'button' || type === 'submit' || type === 'reset') {
      return { role: 'button' };
    }
    if (type === 'checkbox') return { role: 'checkbox' };
    if (TEXT_INPUT_TYPES.has(type)) return { role: 'textbox' };
  }
  return { role: 'generic' };
}

function textName(value: string | null | undefined): string | undefined {
  const text = collapseWhitespace(value ?? '');
  return text === '' ? undefined : text;
}

function attrName(value: string | null): string | undefined {
  return value?.trim() ? collapseWhitespace(value) : undefined;
}

function resolveName(el: Element, role: SnapshotRole): string | undefined {
  const aria = attrName(el.getAttribute('aria-label'));
  if (aria !== undefined) return aria;
  switch (role) {
    case 'heading':
    case 'link':
      return textName(el.textContent);
    case 'button': {
      const text = textName(el.textContent);
      if (text !== undefined) return text;
      return attrName(el.getAttribute('value'));
    }
    case 'textbox':
      return attrName(el.getAttribute('placeholder'));
    case 'combobox': {
      const selected = el.querySelector('option:checked');
      return textName(selected?.textContent);
    }
    case 'img':
      return attrName(el.getAttribute('alt'));
    default:
      return undefined;
  }
}

function collectAttrs(
  el: Element,
  role: SnapshotRole,
): Record<string, string> | undefined {
  const attrs: Record<string, string> = {};
  if (role === 'link') {
    const href = el.getAttribute('href');
    if (href) attrs.href = href;
  }
  if (role === 'textbox' || role === 'checkbox' || role === 'combobox') {
    const name = el.getAttribute('name');
    if (name) attrs.name = name;
  }
  return Object.keys(attrs).length > 0 ? attrs : undefined;
}

function isNotable(el: Element, role: SnapshotRole): boolean {
  if (REF_ROLES.has(role)) return true;
  if (el.id !== '') return true;
  for (const attr of Array.from(el.attributes)) {
    if (attr.name.startsWith('data-')) return true;
  }
  return false;
}

function isHidden(el: Element, allowFallback: boolean): boolean {
  if (typeof el.checkVisibility === 'function') {
    return !el.checkVisibility({ checkVisibilityCSS: true });
  }
  if (allowFallback) {
    const style = window.getComputedStyle(el);
    return (
      style.display === 'none' ||
      style.visibility === 'hidden' ||
      style.visibility === 'collapse'
    );
  }
  return false;
}

function walkElement(
  el: Element,
  depth: number,
  state: WalkState,
  ancestorName?: string,
): SnapshotNode | null {
  if (state.visited >= MAX_VISITED_NODES) {
    state.truncated = true;
    return null;
  }
  state.visited++;
  if (isHidden(el, depth <= 1)) return null;

  const { role, level } = mapRole(el);
  const name = resolveName(el, role);
  const attrs = collectAttrs(el, role);

  let ref: string | undefined;
  if (isNotable(el, role)) {
    state.refCounter++;
    ref = `e${state.refCounter}`;
    el.setAttribute('data-bb-ref', ref);
  }

  const children: SnapshotNode[] = [];
  // Carry the nearest named ancestor down the walk: a text run that is a
  // substring of that name is already represented by it (e.g. the runs
  // inside a named link or button, including ones wrapped in spans), so
  // emitting them again would duplicate content.
  const inheritedName = name ?? ancestorName;
  for (const child of Array.from(el.childNodes)) {
    if (state.truncated) break;
    if (child.nodeType === Node.ELEMENT_NODE) {
      const childEl = child as Element;
      if (SKIP_SUBTREE_TAGS.has(childEl.tagName)) continue;
      const childNode = walkElement(childEl, depth + 1, state, inheritedName);
      if (childNode) children.push(childNode);
    } else if (child.nodeType === Node.TEXT_NODE) {
      const run = collapseWhitespace(child.textContent ?? '');
      if (run === '') continue;
      if (inheritedName !== undefined && inheritedName.includes(run)) continue;
      children.push({ role: 'text', text: run, children: [] });
    }
  }

  return {
    role,
    children,
    ...(level !== undefined ? { level } : {}),
    ...(name !== undefined ? { name } : {}),
    ...(attrs !== undefined ? { attrs } : {}),
    ...(ref !== undefined ? { ref } : {}),
  };
}

function normalizeFilter(value: unknown): SnapshotFilter {
  return value === 'full' ? 'full' : 'interactive';
}

function normalizeMaxChars(value: unknown, filter: SnapshotFilter): number {
  return typeof value === 'number' && Number.isFinite(value) && value > 0
    ? Math.floor(value)
    : defaultMaxCharsForFilter(filter);
}

function executeCommand(
  payload: DomCommand,
): DomCommandResult | Promise<WaitElementResult> {
  const { command, params } = payload;

  switch (command) {
    case 'click': {
      const el = resolveSelector(params.selector as string);
      (el as HTMLElement).click();
      return { clicked: params.selector as string };
    }

    case 'type': {
      const el = resolveSelector(params.selector as string);
      const input = el as HTMLInputElement;
      input.focus();
      input.value = params.text as string;
      input.dispatchEvent(new Event('input', { bubbles: true }));
      input.dispatchEvent(new Event('change', { bubbles: true }));
      return { typed: params.text as string };
    }

    case 'select': {
      const el = resolveSelector(params.selector as string);
      const select = el as HTMLSelectElement;
      select.value = params.value as string;
      select.dispatchEvent(new Event('change', { bubbles: true }));
      return { selected: params.value as string };
    }

    case 'scroll': {
      if (params.selector === 'page' || !params.selector) {
        window.scrollBy(params.x as number, params.y as number);
      } else {
        const el = resolveSelector(params.selector as string);
        el.scrollBy(params.x as number, params.y as number);
      }
      return { scrolled: true };
    }

    case 'hover': {
      const el = resolveSelector(params.selector as string);
      el.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }));
      el.dispatchEvent(new MouseEvent('mouseenter', { bubbles: true }));
      return { hovered: params.selector as string };
    }

    case 'gettext': {
      const el = resolveSelector(params.selector as string);
      return { text: el.textContent };
    }

    case 'gethtml': {
      const el = resolveSelector(params.selector as string);
      return { html: el.innerHTML };
    }

    case 'snapshot': {
      const selector = params.selector as string | undefined;
      const rootEl = selector ? resolveSelector(selector) : document.body;
      const filter = normalizeFilter(params.filter);
      const state: WalkState = {
        visited: 0,
        refCounter: 0,
        truncated: false,
      };
      const tree = walkElement(rootEl, 0, state) ?? {
        role: 'generic' as SnapshotRole,
        children: [],
      };
      const result = renderSnapshotTree(
        tree,
        normalizeMaxChars(params.max_chars, filter),
        { title: document.title, url: location.href },
        filter,
      );
      return { ...result, truncated: result.truncated || state.truncated };
    }

    case 'wait:element': {
      const selector = params.selector as string;
      const timeout = (params.timeout as number) || 10000;

      const tryResolve = (): Element | null => {
        try {
          return resolveSelector(selector);
        } catch {
          return null;
        }
      };

      if (tryResolve()) {
        return { found: true, selector };
      }

      return new Promise<WaitElementResult>((resolve, reject) => {
        const timer = setTimeout(() => {
          observer.disconnect();
          reject(
            new Error(`Element not found within ${timeout}ms: ${selector}`),
          );
        }, timeout);

        const observer = new MutationObserver(() => {
          if (tryResolve()) {
            clearTimeout(timer);
            observer.disconnect();
            resolve({ found: true, selector });
          }
        });

        observer.observe(document.body, { childList: true, subtree: true });
      });
    }

    default:
      throw new Error(`Unknown DOM command: ${command}`);
  }
}

chrome.runtime.onMessage.addListener((request, _sender, sendResponse) => {
  if (request.type === 'ping') {
    sendResponse({ type: 'pong' });
    return true;
  }

  if (request.type === 'command') {
    const payload = request.payload as DomCommand;
    Promise.resolve()
      .then(() => executeCommand(payload))
      .then((data) => sendResponse({ status: 'ok', data }))
      .catch((err) => sendResponse({ status: 'error', error: String(err) }));
    return true;
  }

  return false;
});

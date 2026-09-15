// User/model-facing message builders for DOM command failures.
// Kept as pure functions so both the extension content script and the
// WebSocket MCP tools produce identical wording.

export function selectorNotFoundMessage(selector: string): string {
  return (
    `No element found for "${selector}" — tried it as a CSS selector and ` +
    'as exact visible text. On pages with virtualized lists (e.g. Gmail), ' +
    'rows mount only while visible: take a full-page snapshot to see what ' +
    'is actually in the DOM, then target a selector that is rendered.'
  );
}

export function selectorMatchedButEmptyMessage(selector: string): string {
  return (
    `The element matched by "${selector}" has no text content — it is ` +
    'empty, hidden, or not yet rendered. If you expected list or feed ' +
    'items here, the page may virtualize its list; a full-page snapshot ' +
    'shows what is actually in the DOM right now.'
  );
}

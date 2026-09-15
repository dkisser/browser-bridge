// User/model-facing message builders for DOM command failures.
// Kept as pure functions so both the extension content script and the
// WebSocket MCP tools produce identical wording.

export function selectorNotFoundMessage(selector: string): string {
  return (
    `No element found for "${selector}" — tried it as a CSS selector and ` +
    'as exact visible text. To fix: take a full-page snapshot to see what ' +
    'is actually in the DOM, then re-run with a rendered selector or its ' +
    '@eN ref. Also check that tab_id is the page you expect — links that ' +
    "open in a new tab need that tab's id. On pages with virtualized " +
    'lists (e.g. Gmail), rows mount only while visible: scroll the target ' +
    'into view first.'
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

// Appended to the not-found error so the caller gets the DOM observation
// in-band instead of having to snapshot in a second round-trip. Empty when
// nothing qualifies — don't send a useless section.
export function textContainerCandidatesHint(candidates: string[]): string {
  if (candidates.length === 0) return '';
  const list = candidates.map((c, i) => `  ${i + 1}. ${c}`).join('\n');
  return (
    '\n\nLargest text containers on this page:\n' +
    `${list}\n` +
    'Re-run with one of these selectors.'
  );
}

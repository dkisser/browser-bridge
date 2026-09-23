// Shared description fragments for MCP tools. Keep these short: they are
// read by the model when deciding which tool to call, and they encode the
// discovery workflow (list browsers first, then list tabs) so the first
// call lands on list_browsers/tab_list instead of a guessed id.

export const TAB_ID_GUIDANCE =
  'Requires a valid tab_id — call tab_list first to discover open tabs.';

// Selector-taking tools fail outright when the selector is absent from the
// DOM, and models commonly guess semantic selectors ("article", ".content")
// that the page does not actually use. Same trick as TAB_ID_GUIDANCE: push
// the discovery call (snapshot) before the first selector use.
export const SELECTOR_GUIDANCE =
  'The selector must exist on the page — if you have not seen the DOM yet, ' +
  'call snapshot first and use a rendered selector or its @eN ref. A ' +
  'semantic guess like "article" only matches real <article> tags.';

# Browser Bridge

Browser Bridge lets LLM agents drive a user-controlled browser: reading pages, acting on elements, and switching tabs through a single extension-mediated channel.

## Language

### Page representation

**Snapshot**:
The compact representation of a web page's action surface — an indented pseudo-tree plus per-element refs, produced on demand rather than shipped raw. Biased toward interactive elements and structure; when the budget tightens, body text is trimmed before interactive elements are. Agents read it to find what they can act on; reading what a page says is the Page text job.
_Avoid_: fetch, page fetch, scrape, accessibility snapshot

**Page text**:
The rendered plain-text content of a page or an element, extracted on demand: line breaks follow what is on screen, hidden content is excluded. The reading counterpart to the Snapshot: when the task is "what does the page say" rather than "what can I click", this is the primary source. Deliberately plain text, not markdown — structure for acting lives in the Snapshot.
_Avoid_: fetch, scrape, dump, text snapshot

**Pseudo-tree**:
The Snapshot's format: one line per element carrying role, name, and the attributes agents act on (href, values, ref), with indentation for nesting. Deliberately not the browser's accessibility tree — roles come from a small custom vocabulary and attribute retention is chosen by us, not by Chrome.
_Avoid_: aria tree, DOM tree, outline

**Ref**:
A short handle (`@e12`) assigned to an element when it appears in a Snapshot; every element-addressing command accepts it in place of a selector.
_Avoid_: locator, xpath, element id

### System shape

**Inbound adapter**:
The stateless entry point through which external callers drive Browser Bridge. The CLI and the MCP server are the two inbound adapters; both translate caller requests onto the WebSocket protocol and hold no browser state.
_Avoid_: access layer, frontend, gateway, entry point

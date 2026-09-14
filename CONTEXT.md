# Browser Bridge

Browser Bridge lets LLM agents drive a user-controlled browser: reading pages, acting on elements, and switching tabs through a single extension-mediated channel.

## Language

### Page representation

**Snapshot**:
The compact representation of a web page that agents read to understand it — an indented pseudo-tree plus per-element refs, produced on demand rather than shipped raw.
_Avoid_: fetch, page fetch, scrape, accessibility snapshot

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

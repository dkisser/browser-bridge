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

**Login auto-start**:
Starting bridge services automatically at macOS login via a per-user LaunchAgent — login-scoped and per-user, never a boot-time system daemon.
_Avoid_: daemon mode, daemon, autostart

### Control & safety

**Approval**:
A per-action gate: before the extension executes a gated command, it asks the human once, who allows or denies that single action. The agent keeps the browser; exactly one action is held for confirmation.
_Avoid_: confirmation prompt, human-in-the-loop, manual gate

**Takeover**:
A mode switch, not a per-action question: while Takeover is active, every agent command is rejected with a machine-readable reason and the human operates the browser directly; when the human releases it, the agent resumes. Approval governs one action; Takeover governs the whole session.
_Avoid_: handoff, human assist, pause mode, manual mode

**Working scope**:
The boundary of what the agent may touch without asking: the tabs it opened itself plus the origins a human approved. Commands inside the scope run silent; anything that would cross it triggers Approval.
_Avoid_: allowlist, permission set, trust zone

**Service command**:
The `bridge service …` half of the CLI: everything that manages the service lifecycle (up/down/status/logs/update/enable). Kept strictly separate from browser commands, which never manage services and never fall through to them.
_Avoid_: daemon command, autostart command

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
The stateless entry point through which external callers drive Browser Bridge. The CLI and the MCP server are the two inbound adapters; both translate caller requests onto the WebSocket protocol and hold no browser state. The CLI is a separate process reaching bridge-core over the WebSocket protocol; the MCP server is an HTTP endpoint inside bridge-core itself — both ultimately end up as browser-bound commands.
_Avoid_: access layer, frontend, gateway, entry point

**Control plane**:
The bridge-core process that accepts external commands on the local machine and dispatches them to the browser connection. Replaces the previously separate "WebSocket Server" and "Local Proxy" roles; today both responsibilities live in one binary on the loopback port (3001 for the WebSocket adapter).
_Avoid_: ws-server, WebSocket Server, routing layer

**Browser connection**:
The bridge-core's outbound WebSocket client to the Chrome extension. The direction is fixed: the extension cannot host a server (browser host-permission limits), so bridge-core always dials out to it on a loopback port. One Browser connection per registered browserId.
_Avoid_: Local Proxy, extension socket, browser socket

**Pairing**:
A one-time enrollment handshake between the extension and the control plane: a short-lived code (5 min TTL) is exchanged for a long-lived bearer token, kept in the extension's `chrome.storage` and stored only as a SHA-256 hash on the control-plane side. The token does not expire; revoking it means removing `extensionTokenHash` from `~/.browser-bridge/config.json`, which makes the next extension reconnect fail with 403 and walks the user through the popup-driven re-pairing flow.
_Avoid_: auth, authentication, login

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

### Human surface

**Side panel**:
The browser-managed right-side panel of the extension, opened by clicking the extension icon (after `chrome.sidePanel.setPanelBehavior({ openPanelOnActionClick: true })`). Per-tab; survives in-tab navigations; browser-owned lifecycle, so it does not vanish on focus loss the way the legacy popup did. Hosts the human surface: connection state, approval cards, origins, blocklist, paused downloads, takeover, and the entry point to the settings tab.
_Avoid_: popup, drawer, side drawer, sidebar

**State bar**:
The top strip of the side panel, always visible. Shows the Browser connection dot, the browser UID, the Takeover switch, and a link to settings.
_Avoid_: header, toolbar

**Side panel tab**:
One of the tabs in the strip below the state bar — Approvals, Origins, Blocklist, Downloads, Settings. Side panel tabs are view selectors, not extension UI surfaces; the same policy state is shown across them.
_Avoid_: tab page, workspace tab

**Default view**:
What the user sees first when the side panel opens. State bar plus the Approvals tab when there are pending denials or paused downloads; otherwise the Origins tab. UI state (selected tab, scroll position, transient filters) does not persist across reopens — the default view always returns.
_Avoid_: landing view, last-view

**Settings tab**:
A separate extension options page opened via `chrome.runtime.openOptionsPage()` from the side panel's state bar. Dedicated to read-only display of hard configuration (WebSocket port, local-proxy URL, LaunchAgent plist path, log path, CLI binary path). Inert: never edits. Not a side panel tab — the side panel's tabs are Approvals, Origins, Blocklist, Downloads only.
_Avoid_: options page, preferences

**Hard configuration**:
Network endpoints, paths, and other values that are normally hard-coded or set at install time. Read-only in the settings tab; edits happen through file edits or installer commands, not the UI.
_Avoid_: constants, defaults, env vars

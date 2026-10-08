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
The local process that accepts external commands and dispatches them to the browser connection — it runs as the hidden `bridge serve` subprocess of the single bridge binary, and the *service* still answers to the name bridge-core (logs, pidfile, status text). Replaces the previously separate "WebSocket Server" and "Local Proxy" roles; today both responsibilities live in one process on the loopback port (3001 for the WebSocket adapter).
_Avoid_: ws-server, WebSocket Server, routing layer

**Browser connection**:
The WebSocket link between the Chrome extension and bridge-core's browser-facing server (loopback port 3002). The direction is fixed: the extension cannot host a server (browser host-permission limits), so the extension always dials out to bridge-core, never the reverse. One Browser connection per registered browserId.
_Avoid_: Local Proxy, extension socket, browser socket

**Pairing**:
A one-time enrollment handshake between the extension and the control plane: a short-lived code (5 min TTL) is exchanged for a long-lived bearer token, kept in the extension's `chrome.storage` and stored only as a SHA-256 hash on the control-plane side. The token does not expire; revoking it means removing `extensionTokenHash` from `~/.browser-bridge/data/config.json` (ADR-0017), which makes the next extension reconnect fail with 403 and walks the user through the side-panel-driven re-pairing flow.
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

**Agent tab group**:
The per-window Chrome tab group (titled `browser-bridge`) that the extension automatically places every agent-opened tab into, so the human can see the agent's operating area at a glance. Purely visual organization: it is not part of Working scope and membership never feeds policy decisions.
_Avoid_: workgroup, lane, trust group

**Service command**:
The `bridge service …` half of the CLI: everything that manages the service lifecycle (up/down/status/logs/update/enable). Kept strictly separate from browser commands, which never manage services and never fall through to them.
_Avoid_: daemon command, autostart command

### Logs and records

**Operational log**:
Diagnostic output written by the control plane and its supervisors under `$BB_HOME/logs/` (`bridge-core.log`, `launchagent.log`). For developers debugging a misbehaving install: disposable, rotatable, safe to delete. Never a record of what the agent did — that is the Audit trail.
_Avoid_: log, debug log, diagnostics

**Audit trail**:
The durable record of what the agent did in the browser and when — the accountability counterpart to Approval and Takeover (planned, not yet implemented; the Trace is its raw material). Data, not an Operational log: preserved across upgrades, never rotated away, and read by the user rather than by developers.
_Avoid_: audit log, activity log

### Learned knowledge

**Trace**:
The append-only record of what the agent actually did in the browser, captured at the Control plane and reduced to structure: the commands issued, their outcome, and a structural digest of the page — never page text, never what the user typed. The raw material a Memory is derived from, and preserved after derivation. An Operational log answers "why is it misbehaving"; a Trace answers "what did the agent do, and did it work". The Audit trail is a view over a Trace, not a separate record.
_Avoid_: log, event log, session log, history

**Memory**:
Durable, derived knowledge the control plane carries between sessions about how to drive a given site, produced from what the agent actually did. Lives under `$BB_HOME/data/` on the same preservation contract as the Audit trail, and is a different kind of thing from it: the Audit trail records *what happened*, a Memory records *what the control plane now believes because of it*. Owned by Browser Bridge — the agent's own runtime memory store is a separate system and is not read.
_Avoid_: self-learning, self-evolution, experience base, training data

**Site card**:
The Memory for one host: what its pages are laid out like, which calls work there, and which ones have been observed to fail. Injected back to the agent as a labelled reference when it lands on a host it has a card for, and rewritten when the host stops matching. A card is *advice about where to look first*, never a replacement for looking: the agent keeps its own list of what to try, and the card's shortlist goes in front of it. A card that is stale must cost calls, not correctness.
_Avoid_: site profile, site model, recipe file, playbook

**Baseline**:
The measurement of whether a Site card changes what an agent does — a delta between two runs that differ only in whether the card was read, averaged over repetitions so a trend can be read. Distinct from a test: a test asks whether the mechanism still works, a Baseline asks whether it is worth anything, and a mechanism can pass every test and be worth nothing. Two kinds, and the difference is the whole point: a *fixture* baseline drives a synthetic page through the real learner and is deterministic, so it measures the plumbing; a *live* baseline runs real tasks in a real agent, and the only things taken on trust are the ones the trace cannot know — whether the task succeeded, and whether the agent used the card.
_Avoid_: benchmark, eval, score, metrics

_Benchmark_ is the avoided word in current prose, and it is still the word in ADR-0025 and in the `bench` subcommand's own name. Both are history, not exceptions: an ADR records what was believed when it was written, and a shipped command name is not worth breaking for a vocabulary rule. When reading an older document, read *benchmark* as *Baseline*.

**Site guide**:
Curated, human-initiated knowledge about how to operate one host, kept as plain markdown under `$BB_HOME/data/guides/<host>.md` — the semantics a Site card cannot learn: why a step exists, which banner to dismiss first, what the page's business objects are. Written through the browser-bridge-memory skill at the human's request; cards are machine-owned and rebuilt, guides are human-owned and stable. Read by riding the card's pull: `bridge memory show <host>` prints the guide after the card (capped at 8 KiB), so the skill's existing recall step covers both (ADR-0033). The daemon never injects the prose, but every landing announces the guide's path as a one-line pointer when one exists (ADR-0034). When a guide contradicts the live page, the page wins, and the guide must be fixed or deleted.
_Avoid_: notes, site notes, playbook

**Routine**:
A crystallized, replayable sequence of `bridge` CLI calls for a repeated flow on one host, stored under `$BB_HOME/data/routines/<host>/`. Replayed verbatim until it breaks: a Routine self-verifies an early landmark before acting, and on mismatch it is thawed — abandoned for live exploration, then fixed or deleted — never retried blind. Phase 0 form is a shell script (ADR-0029).
_Avoid_: macro, artifact, program, script

### Human surface

**Side panel**:
The browser-managed right-side panel of the extension, opened by clicking the extension icon (after `chrome.sidePanel.setPanelBehavior({ openPanelOnActionClick: true })`). Per-tab; survives in-tab navigations; browser-owned lifecycle, so it does not vanish on focus loss the way the legacy popup did. Hosts the human surface: connection state, approval cards, origins, blocklist, paused downloads, takeover, and the entry point to the settings tab.
_Avoid_: popup, drawer, side drawer, sidebar

**State bar**:
The top region of the side panel, always visible. Shows the Browser connection dot, the browser UID, the Takeover switch, and a link to settings. Rendered as the takeover hero card in the side panel — the same connection+takeover content (dot, UID, switch, settings entry) now presented inside a single elevated glass card together with the pairing drawer.
_Avoid_: header, toolbar

**Side panel tab**:
One of the tabs in the strip below the state bar — Approvals, Origins, Blocklist, Downloads. Side panel tabs are view selectors, not extension UI surfaces; the same policy state is shown across them.
_Avoid_: tab page, workspace tab

**Default view**:
What the user sees first when the side panel opens. State bar plus the Approvals tab when there are pending denials or paused downloads; otherwise the Origins tab. UI state (selected tab, scroll position, transient filters) does not persist across reopens — the default view always returns.
_Avoid_: landing view, last-view

**Settings tab**:
A separate extension options page opened via `chrome.runtime.openOptionsPage()` from the side panel's state bar. Dedicated to read-only display of hard configuration (WebSocket port, local-proxy URL, log path, data path, LaunchAgent plist path, CLI binary path). Inert: never edits. Not a side panel tab — the side panel's tabs are Approvals, Origins, Blocklist, Downloads only.
_Avoid_: options page, preferences

**Hard configuration**:
Network endpoints, paths, and other values that are normally hard-coded or set at install time. Read-only in the settings tab; edits happen through file edits or installer commands, not the UI.
_Avoid_: constants, defaults, env vars

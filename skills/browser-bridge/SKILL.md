---
name: browser-bridge
description: |
  Use this skill to control a real web browser through browser-bridge — searching, navigating, clicking, filling forms, reading or scraping page content, taking screenshots, or managing tabs. Typical requests: "open Gmail", "check my GitHub notifications", "search for X and open the first result", "fill out this form", "take a screenshot of this page", "get the text of this article".

  Prefer it over writing custom browser automation scripts or using generic web search when the task needs the user's real login state, dynamic JavaScript, or visual layout.
---

# Browser Bridge Skill

Browser Bridge lets any Agent control a real Chrome browser. The browser keeps the user's real login state, cookies, and tabs — so it can access Gmail, GitHub, internal dashboards, or any site the user is already logged into.

## When to use this skill

Use it for anything that requires a live browser: search and navigation, forms, clicking, reading or scraping page content, screenshots, tab management, site-specific workflows ("open Gmail and mark GitHub notifications as read"). Do not use it for pure coding tasks with no browser involved.

## Access: MCP first, CLI as an optional adapter

**The browser-bridge MCP server is the primary interface** (`http://localhost:3003/mcp`), and this skill is written for it: typed tools with structured arguments — plus one extra the CLI cannot receive in-band: learned site cards are injected automatically into `navigate` and the following `snapshot` on known hosts (see "Site memory" below).

**The `bridge` CLI carries the same commands in stateless form** — one command per job, `--json` for structured output — and it owns what no MCP tool exposes: service management (`bridge service up` / `down` / `restart` / `status` / `logs [name]` / `enable` / `disable` / `update [version]` / `doctor` / `version`) and the memory *writes* (`bridge memory rm <host>`, `bridge memory learn`). Memory reads are on both sides: `memory_list` / `memory_show` on MCP, `bridge memory list` / `bridge memory show <host>` on the CLI.

Both interfaces drive the same browser; the tables below map every MCP tool to its CLI equivalent.

## Before any browser command

1. **Services running?** Any successful MCP call proves they are — the control plane serves MCP itself. If every call fails to connect, bring the stack up with the CLI (service management is CLI-only):
   ```bash
   bridge service up
   ```
   If already running, `bridge service up` reports that and does nothing harmful.
2. **Pick a browser**: `list_browsers`. No browsers → ask the user to load and authenticate the extension. Several → ask which one, then `set_browser(browserId=...)`. (CLI: `bridge browser:list`, then `--browser <id>` on every command.)
3. **Pick a tab**: `tab_list`, then pass `tab_id` on every page-level call — never guess a tab id. (CLI: `tab:list`, then `--tab <id>`.)

## Command reference (MCP ⇄ CLI)

Every browser command exists in both interfaces; examples in this skill use the MCP tools. Every tab-scoped call takes `tab_id: number` (CLI: `--tab <id>`), plus an optional `timeout_ms` (CLI: `--timeout <ms>`, default 10000).

### Browser and tabs

| MCP tool | CLI equivalent | Notes |
|---|---|---|
| `list_browsers` | `bridge browser:list` | call first |
| `set_browser` | `--browser <id>` flag | pin one browser when several are online |
| `tab_list` | `tab:list` | source of valid `tab_id`s; each entry reports `inAgentGroup` |
| `tab_new(url?, active?)` | `tab:new [url]` | background by default; returns the new id; the tab joins the 'browser-bridge' tab group |
| `tab_close` / `tab_switch` | `tab:close <id>` / `tab:switch <id>` | |

### Navigation

| MCP tool | CLI equivalent |
|---|---|
| `navigate(url)` | `navigate <url>` |
| `go_back` / `go_forward` / `refresh` | `goBack` / `goForward` / `refresh` |

### Page interaction

| MCP tool | CLI equivalent |
|---|---|
| `click(selector)` | `click <selector>` |
| `type(selector, text, submit?)` | `type <selector> <text>` |
| `select(selector, value)` | `select <selector> <value>` |
| `scroll(x, y, selector?)` | `scroll <x> <y>` |
| `hover(selector)` | `hover <selector>` |
| `wait_element(selector)` | `wait:element <selector>` |
| `wait_navigation` | `wait:navigation` |

### Reading the page

| MCP tool | CLI equivalent |
|---|---|
| `snapshot(selector?, filter?, max_chars?)` | `snapshot [--selector <sel>] [--filter <interactive\|full>] [--max-chars <n>]` |
| `get_text(selector)` | `gettext <selector>` |
| `get_html(selector)` | `gethtml <selector>` |
| `screenshot` | `screenshot` |
| `pageinfo` | `pageinfo` |

`@eN` refs from a snapshot work as `selector` in both interfaces.

### Site memory (read-only; no `tab_id`, no `timeout_ms`)

| MCP tool | CLI equivalent | Notes |
|---|---|---|
| `memory_list` | `bridge memory list` | every host with a learned card |
| `memory_show(host, raw?)` | `bridge memory show <host> --resolve [--raw]` | card + site guide; `raw` gives the structured card. `--resolve` is required on the CLI — without it the map is dropped |

These two read the store rather than the browser, so they never reach the browser at all and take no `tab_id`. `memory_show` is the pull path — see "Site memory" below.

## Working with tabs

Create a fresh tab per workflow with `tab_new` (CLI: `tab:new`) and pass its `tab_id` to every page-level call; close it with `tab_close` when done. This keeps the user's active tab untouched and lets you run several tab workflows in parallel.

Opening your own tab does **not** buy you a quieter run. Approval is keyed on the **origin**, not on tab ownership: page commands need the origin approved or a one-shot origin grant, and an agent tab changes nothing there. Only two commands are tab-scoped — `tab_close` on a tab you did not open, and `screenshot` on a tab that is not the visible one in its window — and both pause the same way. So open a tab for the reasons above (parallelism, not disturbing the user), not expecting it to be the thing that stops the prompts.

Tabs opened via `tab_new` are automatically grouped per window into the 'browser-bridge' tab group (orange) — purely visual organization, no action or judgment needed from you, and the user's own tabs are never grouped or moved. `tab_list` reports `inAgentGroup: true` for each tab in that group.

## Site memory: pull the card before you explore or code

The control plane learns how each site works from what agents actually do and keeps a per-host *site card*: which containers hold the content, which selector shapes have failed there, which sequences have worked.

- **MCP path**: the card is injected automatically into the `navigate` result and the first `snapshot` after landing, labelled `learned site patterns`. The injection is a convenience, not the recall — it only fires on a landing, and advice gets ignored under pressure. Pull it explicitly with `memory_show(host)` when you land, and read the result before you explore. The guide's prose is not injected, but the landing tells you when one exists — a `[site guide]` line carrying the file's path; `memory_show` prints the guide itself after the card.
- **CLI path**: pull it explicitly right after landing on a host: `bridge memory show <host> --resolve`. The `--resolve` matters — without it the command renders the card with no resolver and drops the site map entirely, which is the part that says where the content lives. A `no card for <host>` answer is normal — continue without it. When a curated **site guide** exists for the host, the same command prints it after the card, labelled `[site guide]` — layout, gotchas, and why steps are ordered, written by the human (or a previous agent at the human's request). A successful `navigate`/`tab:new` also prints the guide's path as a one-line pointer, so skipping the pull still leaves the door visible.
- **Before writing a routine** (a saved multi-step script for a host — see the `browser-bridge-memory` skill): always pull the card *first* with `memory_show(host)` (CLI: `bridge memory show <host> --json` for the card's structure and the guide together), at coding time, so selectors and layout come from memory instead of guesses. At coding time no `navigate` has happened yet, so nothing will be injected — this pull is the only way to get it.
- Either way the refs are resolved against a page that is not the one in front of you. Treat them as where to start looking, not as things to click.

A card is advice about where to look first, never ground truth: sites change, and a wrong card costs a call, not correctness. Confirm against a live `snapshot` before acting. The same holds for a guide — if it contradicts the live page, the page wins; report the mismatch instead of following the artifact.

## Snapshot first: pick the follow-up by goal

Every page-level task starts with `snapshot`. It is the only tool that shows the page's structure — a compact pseudo-tree of interactive elements, headings, and (with `filter="full"`) text runs — and it is the ground truth for everything that follows. Never act on, or read through, a selector you have not confirmed on the current page.

Once you have the structure, choose the follow-up by what you came to do:

- **Scraping page data** — article bodies, listings, email subjects, comments, prices. Locate the content container in the snapshot, then `get_text` (CLI: `gettext`) on that container. Plain text is the right shape for data; snapshot text runs are truncated and not meant for reading.
- **Operating the page** — clicking, filling, selecting, hovering. Find the target element in the snapshot (its `@eN` ref is the fastest handle), then `get_html` (CLI: `gethtml`) on the target to inspect its real DOM — tag, `id`, `class`, `name`, form structure — and build a precise CSS selector from those attributes for `click`/`type`/`select`/`hover`. The snapshot shows what is on the page; `get_html` reveals the attributes you can act on durably.

### Virtualized lists (Gmail, large tables)

Rows in virtualized lists mount and unmount as you scroll, and different rows may carry different classes (e.g. in Gmail only the expanded unread email keeps the legacy classes). Consequences:

- A class selector in `get_text` may match only some rows or none — "not found" can mean the row is not mounted, not that your selector is wrong.
- Prefer a whole-page `snapshot` (or `get_text` on a container) over per-row class selectors for these pages.

### Reading truncated output

The snapshot stats line tells you what happened — `[12/484 nodes | tier=1 | truncated]` (MCP) or `[nodes: 12/484 | tier: 1 | truncated: true]` (CLI):

- `tier=1`: text runs are truncated to 40 characters each.
- `tier=2` (full filter): text runs are replaced in place by `text [text suppressed]` — a cell that looks empty may be suppressed, not actually empty.
- `tier=2` (interactive filter): text runs are dropped by design, so no placeholders appear.
- If the output is truncated, narrow with `selector` / `--selector` or raise `max_chars` / `--max-chars` rather than re-running unchanged.

### Confirm selectors before using them

`get_text`/`get_html` find nothing unless the selector exists in the DOM, and many sites (e.g. eastmoney, most portal CMSs) wrap article bodies in plain `<div>` containers instead of semantic tags like `<article>`. The same applies to acting on elements (`click`, `type`, `select`, `hover`, `wait_element`): a guessed selector either misses or waits forever. Do not pass a semantic guess (`article`, `.content`, `.post`) on a page you have not inspected. Before acting on an unfamiliar page:

1. `snapshot` with `filter="full"` to see what is actually rendered — a `@eN` ref from the snapshot is the most reliable selector.
2. Follow up by goal: `get_text` on the confirmed container to read it, or `get_html` on the confirmed element to derive a durable CSS selector for interaction.

The robust sequence is always navigate → `wait_navigation` → `snapshot` → follow-up: `get_text` for data scraping, `get_html` before clicking or filling. If a link opened the target in a new tab, run `tab_list` and use the new tab's id first — the original tab still shows the old page.

Two failure anti-patterns, both observed in the wild:

- **Shotgun guessing** — after one failed selector, immediately trying `.entry-content`, `.article-body`, `.post-content`, ... One failure means you have no information about the DOM; stop and observe. The not-found error already lists the page's largest text containers — re-run with one of those, or take a snapshot. Never guess a second selector without new information.
- **Greedy compensation** — after failed precise reads, grabbing whole-page data with `get_html` on `main` / `#content` / `body`. That returns hundreds of KB of nav, ads, and sidebars that drowns the content, and the MCP server rejects text results over 100K chars anyway. Snapshot, find the container, narrow.

## Extraction strategy

Start from the `snapshot` and extract through `get_text`/`get_html` — text-based extraction is faster, cheaper, and easier to summarize, search, or act on than screenshots.

1. **Snapshot first, then `get_text` or `get_html`.** Follow the scenario rules above: `get_text` to read data, `get_html` when you need DOM structure to act on.
2. **Use `screenshot` only when necessary**, such as:
   - The user explicitly asked for a screenshot.
   - You need visual or layout information that text cannot convey (colors, positioning, whether an element is visible, etc.).
   - `get_text`/`get_html` returned empty, insufficient, or unclear results after a reasonable attempt.

When a tool returns no data, try a broader selector or `pageinfo` to verify the current URL/title before falling back to a screenshot.

## Multi-step workflows

Most browser tasks need several calls. Plan the sequence, run them in order, and verify state between steps. For example:

**Open Gmail and mark GitHub pipeline notifications as read:**

1. `list_browsers` → pick the `browserId`; `set_browser` if several are online. If the call cannot connect, run `bridge service up` first (CLI — service management).
2. `tab_new(url="https://mail.google.com")` — note the returned `tab_id`, e.g. `101`
3. `wait_navigation(tab_id=101)` — raise `timeout_ms` to ~15000 if needed
4. `snapshot(tab_id=101)` to find the notification rows and the mark-as-read control
5. `get_html` on the confirmed `@eN` ref to derive a durable CSS selector for the control
6. `click(selector, tab_id=101)` with that selector
7. Confirm with another `snapshot` or `get_text` if needed
8. `tab_close(tab_id=101)` when done

CLI equivalent of steps 2-8: `bridge --browser <id> tab:new https://mail.google.com`, then pass `--tab <id>` to each command (`wait:navigation`, `snapshot`, `gethtml`, `click`, `tab:close`).

## Handling large outputs

Screenshots come back as image content (MCP) or base64 `dataUrl` (CLI); HTML and text come back as strings. Follow these rules:

1. **If the user explicitly asked to save something** (e.g. "save the screenshot", "download the page"), write it to a file in the current working directory or a path the user specified.
   - Screenshots: save as PNG (decode the base64 data URL on the CLI path).
   - HTML / text: save as `.html` or `.txt`.
   - Tell the user the exact file path.
2. **If the output is only needed as an intermediate step** (e.g. reading a value to decide what to click next), keep it in memory and do not write a file.
3. **If the output is long but the user asked a question about it**, summarize the relevant parts in your response and optionally save the full content to a file for later reference.

## Error handling

- MCP calls fail to connect → services are down → bring them up with `bridge service up` (CLI — service management is CLI-only), then retry.
- `list_browsers` returns no browsers → stop and ask the user to load the extension in Chrome and authenticate.
- A call times out → retry once with a larger `timeout_ms` (CLI: `--timeout`), then report failure.
- Selector not found → report the exact selector, take a `snapshot`, and ask for a better one.
- An error payload → surface the `error` and `message` fields clearly.

## Example prompts this skill handles

- "Open Gmail and mark GitHub pipeline notifications as read."
- "Take a screenshot of example.com and save it."
- "Search for 'Claude Code release notes' on Google and open the first result."
- "Fill out the contact form on example.com with my name and email."
- "List my open tabs and close the ones on Twitter."
- "Get the inner HTML of the main article on this page."

## Security notes

- You can only control browsers the user has authenticated and connected. Do not attempt to control a browser the user did not authorize.
- Do not silently execute destructive actions (closing many tabs, deleting content) without confirming with the user.

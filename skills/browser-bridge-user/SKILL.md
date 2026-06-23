---
name: browser-bridge-user
description: |
  Use this skill whenever the user needs to control a real web browser through the installed `bridge` command — searching, navigating, clicking, filling forms, taking screenshots, reading page content, managing tabs, marking notifications as read, or any other task that requires interacting with a live web page.

  This includes requests like "open Gmail", "check my GitHub notifications", "search for X", "fill out this form", "take a screenshot of a page", "download this page content", or any workflow where a headless or text-only approach would miss logged-in state, dynamic JavaScript, or visual layout.

  Always prefer this skill over writing custom browser automation scripts, over suggesting the user do it manually, or over using generic web search when the task clearly needs interaction with a specific site or user session. Use it when the user has already installed Browser Bridge and is running the `bridge` command, not when they are developing or running from source.
---

# Browser Bridge User Skill

Browser Bridge lets any Agent control a real Chrome browser through the `bridge` CLI. The browser keeps the user's real login state, cookies, and tabs — so it can access Gmail, GitHub, internal dashboards, or any site the user is already logged into.

This skill is for end users who have installed Browser Bridge via the one-line installer. If you are working in the source repository and need `bun run cli`, use the `browser-bridge` skill instead.

## When to use this skill

Use this skill when the user asks for anything that requires a browser, especially:

- **Web search and navigation**: "search for X", "open example.com", "go back", "refresh"
- **Form interaction**: "fill in the login form", "click submit", "select the dropdown"
- **Data extraction**: "get the text of this element", "save this page's HTML", "screenshot this page"
- **Tab management**: "open a new tab", "close tab 123", "switch to the GitHub tab"
- **Site-specific workflows**: "open Gmail and mark GitHub pipeline notifications as read", "check my calendar"

Do **not** use this skill for pure coding tasks that do not need a browser (writing files, running local scripts, analyzing code).

## Before running any command

### 1. Make sure the bridge is running

If the user has not started Browser Bridge yet, tell them to run:

```bash
bridge up
```

If the services are already running, `bridge up` will report that and do nothing harmful.

### 2. Check available browsers

Always start by listing connected browsers. The user may have zero, one, or many browsers connected.

```bash
bridge browser:list
```

Parse the output. Possible cases:

- **No browsers**: tell the user no browser is connected and ask them to load the unpacked extension in Chrome and authenticate it.
- **One browser**: automatically use its `browserId`, but still tell the user which browser you selected.
- **Multiple browsers**: show the list and ask the user which `browserId` to use. Do not guess.

### 3. Build the `--browser` argument

Once a browser is selected, pass it to every subsequent command:

```bash
bridge --browser <browserId> <command>
```

## CLI command reference

The `bridge` command is invoked as:

```bash
bridge --browser <browserId> [global-options] <command> [args]
```

Global options:

- `--server <url>` — WebSocket server URL (default: `ws://localhost:3001`)
- `--browser <id>` — target browser instance (required for all commands except `browser:list`)
- `--json` — output structured JSON
- `--timeout <ms>` — command timeout (default: 10000)

### Service management

- `up` — start ws-server and local-proxy
- `down` — stop both services
- `restart` — down then up
- `status` — show service state
- `logs [name]` — tail logs (`ws-server` | `local-proxy`)
- `doctor` — diagnose the install
- `version` — print installed and latest version

### Navigation

- `navigate <url>` — open a URL in the current tab
- `goBack` — browser history back
- `goForward` — browser history forward
- `refresh` — reload current page

### Tabs

- `tab:list` — list all open tabs
- `tab:new [url]` — open a new tab
- `tab:close <tabId>` — close a tab by id
- `tab:switch <tabId>` — switch to a tab by id

### DOM interaction

- `click <selector>` — click an element (CSS selector)
- `type <selector> <text>` — type text into an input
- `select <selector> <value>` — select a dropdown option
- `scroll <x> <y>` — scroll by pixel offset
- `hover <selector>` — hover over an element

### Data extraction

- `gettext <selector>` — get text content of an element
- `gethtml <selector>` — get inner HTML of an element
- `screenshot` — take a screenshot of the visible page
- `pageinfo` — get current URL, title, and tab id

### Waiting

- `wait:element <selector>` — wait until an element appears
- `wait:navigation` — wait until page navigation completes

## Multi-step workflows

Most browser tasks need several commands. Plan the sequence, run them in order, and verify state between steps. For example:

**Open Gmail and mark GitHub pipeline notifications as read:**

1. `bridge up` (if not already running)
2. `bridge browser:list` to choose the browser
3. `bridge --browser <id> navigate https://mail.google.com`
4. `bridge --browser <id> wait:navigation --timeout 15000`
5. Find the notification rows with `gettext` or `gethtml` on selectors that contain GitHub pipeline text.
6. Click the row or its mark-as-read control with `bridge --browser <id> click <selector>`.
7. Confirm with another `gettext` or `screenshot` if needed.

## Handling large outputs

The CLI returns screenshots as base64 `dataUrl`, HTML as strings, and page text as strings. Follow these rules:

1. **If the user explicitly asked to save something** (e.g., "save the screenshot", "download the page", "keep a copy of this HTML"), write it to a file in the current working directory or a path the user specified.
   - Screenshots: decode the base64 data URL and save as PNG.
   - HTML / text: save as `.html` or `.txt`.
   - Tell the user the exact file path.

2. **If the output is only needed as an intermediate step** (e.g., reading a value to decide what to click next), keep it in memory and do not write a file.

3. **If the output is long but the user asked a question about it** (e.g., "what does this page say?"), summarize the relevant parts in your response and optionally save the full content to a file if it would be useful for later reference.

## Error handling

- If `bridge browser:list` returns no browsers, stop and ask the user to load the extension in Chrome and authenticate.
- If services are not running, tell the user to run `bridge up` first.
- If a command times out, retry once with a longer `--timeout`, then report failure.
- If a selector is not found, report the exact selector and ask the user for a better one.
- If the command returns an error payload, surface the `error` and `message` fields clearly.

## Example prompts this skill handles

- "Open Gmail and mark GitHub pipeline notifications as read."
- "Take a screenshot of example.com and save it."
- "Search for 'Claude Code release notes' on Google and open the first result."
- "Fill out the contact form on example.com with my name and email."
- "List my open tabs and close the ones on Twitter."
- "Get the inner HTML of the main article on this page."

## Security notes

- The CLI only controls browsers the user has authenticated and connected. Do not attempt to control a browser the user did not authorize.
- Commands are routed through the WebSocket server; never try to construct raw WebSocket messages unless the CLI cannot do what you need.
- Do not silently execute destructive actions (closing many tabs, deleting content) without confirming with the user.

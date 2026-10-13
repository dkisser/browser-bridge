# A Dedicated Chrome Instance for Automation

Browser Bridge drives your real browser with your real logins — that is the point, and it is also the risk. An agent acting on the web can be steered by the web (indirect prompt injection), and the blast radius is whatever that browser is logged into.

If you would rather not hand an agent your everyday browser, don't: run a **dedicated Chrome instance** that holds only the logins you choose, and let Browser Bridge drive that one. The isolation is physical, not policy — a separate Chrome data directory means separate cookies, storage, history, and extensions. Your everyday browser is untouched because nothing in this setup can reach it.

## Step 1 — Launch Chrome with its own data directory

You must pass `--user-data-dir` explicitly. Without it Chrome reuses your everyday profile, which defeats the entire purpose. Pick a stable, dedicated path and reuse it on every launch — your logins live there.

**macOS**

```bash
open -na "Google Chrome" --args \
  --user-data-dir="$HOME/chrome-automation" \
  --no-first-run --no-default-browser-check
```

`open -n` starts a new instance even if your everyday Chrome is already running; the two run side by side as separate processes with separate Dock icons.

**Linux**

```bash
google-chrome \
  --user-data-dir="$HOME/chrome-automation" \
  --no-first-run --no-default-browser-check
```

**Windows (PowerShell)**

```powershell
& "C:\Program Files\Google\Chrome\Application\chrome.exe" `
  --user-data-dir="$env:USERPROFILE\chrome-automation" `
  --no-first-run --no-default-browser-check
```

Rules for the data directory:

- **Dedicated** — never point it at your everyday profile, and never point two running instances at the same directory.
- **Reused** — same path on every launch, or each launch starts from a blank browser.

Tip — save it as an alias (macOS/Linux):

```bash
alias automation-chrome='open -na "Google Chrome" --args --user-data-dir="$HOME/chrome-automation" --no-first-run --no-default-browser-check'
```

## Step 2 — Install the extension in that instance only

Extensions are scoped to the data directory: what you install here does not appear in your everyday Chrome.

1. In the dedicated instance's window, open `chrome://extensions`.
2. Enable **Developer mode** (top right).
3. Click **Load unpacked** and select `~/Browser-Bridge/extension/` (created by the installer).

## Step 3 — Pair

1. In a terminal, run `bridge pair` — it prints a short-lived pairing code.
2. In the dedicated instance, open the Browser Bridge side panel (click the extension icon) and enter the code.
3. Verify with `bridge browser:list` — the connected browser should appear.

Now log in, by hand, to only the sites your automation needs. That set of logins is the blast radius: an injected page can reach those sites and nothing else in your digital life.

## One browser at a time

Browser Bridge currently drives **one connected browser at a time** — the most recently connected one. So:

- Keep the extension **disabled or uninstalled in your everyday Chrome** while you use the dedicated instance. If two browsers run with the extension enabled, the last one to connect wins, and pairing a second browser invalidates the first one's token.
- To move the bridge back to your everyday browser, re-enable the extension there and pair again. Switching is pairing, not configuration.

## Managing the instance's data

- **Inspect or clear cookies per site** — `chrome://settings/content/all` inside the dedicated instance.
- **Wipe everything** — close the instance, then delete the data directory (`rm -rf ~/chrome-automation`). The next launch is a clean browser; pair again. There is no other state — this is the entire reset.

## Alternative: a Chrome profile instead of a data directory

Chrome's built-in profiles (`chrome://settings/manageProfile`) also isolate cookies, storage, and extensions, with no command line: a profile named "Automation" with the extension installed only there works the same way. We recommend the `--user-data-dir` form above because the instance is unambiguously separate — its own process and Dock icon, no chance of opening the wrong profile in a hurry — and because wiping it is deleting exactly one directory.

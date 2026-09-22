# The human surface moves from popup to side panel

The extension's human-facing panel — connection state, approval cards, origins, blocklist, paused downloads, takeover — moves off the legacy `popup.html` (a 360 px modal anchored to the toolbar icon) onto the Chrome MV3 `chrome.sidePanel` API. Clicking the extension icon now opens the side panel for the active tab via `chrome.sidePanel.setPanelBehavior({ openPanelOnActionClick: true })`. The `action.default_popup` field is removed from the manifest.

The side panel is per-tab and survives in-tab navigations (browser-managed lifecycle). It hosts four tabs: Approvals, Origins, Blocklist, Downloads. A state bar at the top always shows Browser/Cloud connection dots, the browser UID, and the Cloud/Takeover switches; the state bar also carries a link to the settings page.

Hard configuration (WebSocket port, local-proxy URL, LaunchAgent plist path, log path, CLI binary path) lives in a separate options page, opened through `chrome.runtime.openOptionsPage()` from the side panel's state bar. That page is inert — it formats the configuration for humans to read and never accepts edits.

## Considered Options

- **Keep the popup, add a side panel beside it (rejected) — duplicate panels holding the same policy data is a UX trap. Two places to look for the same approval card. Easy to drift: a denial approved in one place and dismissed in the other has no shared "current state" story.**
- **Side panel only (chosen) — one panel, one place. Click icon, get the panel; it does not vanish when the user clicks anywhere on the page. Approval cards and blocklist changes survive the natural user rhythm (refresh, navigate, scroll) instead of disappearing the moment focus leaves the popup.**
- **Move the panel onto the local-proxy web UI (rejected) — would have re-opened ADR-0006. The proxy is a dumb pipe; the control plane stays in the extension.**

## Consequences

- `manifest.json`: `action.default_popup` removed; `side_panel.default_path` set to `sidepanel.html`; `options_ui.page` set to `settings.html`.
- New files: `sidepanel.html`, `sidepanel.ts`, `settings.html`, `settings.ts`. Old `popup.html` / `popup.ts` removed once content is migrated (no value keeping a dormant 360 px shell).
- Side panel layout: a fixed state bar at the top (Browser/Cloud dots, UID, Cloud switch, Takeover switch, settings-page link) plus a tab strip below with four panels (Approvals | Origins | Blocklist | Downloads). Default view on open is Approvals when there are pending denials or paused downloads; otherwise Origins. UI state (selected tab, scroll position, transient filters) does not persist — refresh or tab re-opening returns to the default view. Policy state itself continues to live in `chrome.storage.local` and is unaffected.
- Settings page is inert: it formats the hard configuration for humans to read, and emits nothing back. Editing happens through file edits or installer commands, not the UI.
- Side panel does not drive the page. It reads policy state and issues chrome.* API calls (takeover set, grant consumption, origin / blocklist mutation, download resume / cancel). It does not send `click` / `type` / `snapshot` commands to the content script — that path stays the agent's via background → content. This avoids bypassing the policy gate.
- Chrome version floor rises to 114 (the version that shipped `chrome.sidePanel`). Document this in `README.md` install steps.
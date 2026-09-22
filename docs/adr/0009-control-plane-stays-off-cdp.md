# The control plane stays off chrome.debugger

The safety layer (Approval prompts, Takeover, scope checks, download gating) is built exclusively on extension APIs — content scripts, service worker, popup, `storage`, `downloads` — and `chrome.debugger`/CDP remains off-limits for it, extending the constraint ADR-0001 recorded for snapshots. A future need that seems to require CDP-level power (freezing a page's JavaScript during Takeover, network-level blocking) must be redesigned on extension primitives or escalated to a human decision, not quietly implemented through the debugger.

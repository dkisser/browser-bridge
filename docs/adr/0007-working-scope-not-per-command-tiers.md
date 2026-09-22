# Working scope, not per-command tiers

Silent trust is scoped to the agent's *working scope* — tabs it opened itself plus human-approved origins — instead of static per-command permission tiers. Commands inside the scope run without interrupting the human; anything that would cross the scope triggers Approval; a new origin is approved once (this action / this session / always / deny). Two action classes always require Approval regardless of scope: typing into password or credit-card fields, and form submission (`type` with `submit: true`).

## Considered Options

- **Static per-command tiers**: rejected on evidence. `chrome.tabs.captureVisibleTab` captures the window's *visible* tab, not the agent's target tab — so an agent approved on origin A can silently screenshot the banking tab the human switched to; no per-command label captures "screenshot is safe iff it is my tab and my origin". Likewise `tab:close` is irreversible state loss on user-opened tabs yet trivially safe for agent-opened ones. Static tiers force a choice between noisy prompts and open holes; binding rules (scope in → silent, scope out → ask) match both.

## Consequences

- The service worker must track agent-created tab ids to distinguish "close my tab" from "close yours".
- `screenshot` requires a check that the target window's visible tab is the agent's own approved-origin tab before running silent.
- No execute-script command exists today; if one is ever added, it ships default-deny under this model.

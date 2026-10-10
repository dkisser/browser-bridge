# The mode moves the threshold, not the boundary

A human-only three-way switch — **Permission mode**, `strict` / `standard` / `relaxed` — decides which safety levels must clear the Working scope before running silent: strict gates read and write, standard gates write only, relaxed gates neither. Origin approval (session / always / deny) and the Working scope itself are unchanged: the mode moves the threshold, the boundary stays. Commands sort into four safety levels: *observer* (browser state and passive waits — `tab:list`, `pageinfo`, `tab:switch`, `wait:*`, blank `tab:new`, history navigation — never asks in any mode), *read* (`navigate`, `tab:new` carrying a URL, `snapshot`, `gettext`, `gethtml`), *write* (`click`, `type`, `select`, `scroll`, `hover`), and *sensitive* (form submit, password/credit-card fields, `screenshot`, closing a human's tab), which keeps its own binding rules in every mode. Deny, the blocklist, protected origins, fail-closed unknowns, and Takeover hold regardless of mode. The switch lives in the side panel's State bar next to Takeover, persists in the extension; new installs default to `strict`, upgrades to `standard`.

This narrows ADR-0007, which rejected per-command tiers as the *sole* axis of silent trust. Its evidence still holds — `screenshot` and `tab:close` are safe or dangerous by context (whose tab, whose origin), not by label — and the sensitive class answers it the same way ADR-0007 did: with binding rules. What tiers add here is not a replacement for scope but a movable threshold over it, the case ADR-0007 never considered. ADR-0008's denial contract is untouched.

## Considered Options

- **Pure tiers, scope retired** ("the mode is the permission; origins stop asking"): rejected mid-design. It deleted the human's per-origin trust memory and blurred the deny/blocklist distinction for a simpler story, and under it `relaxed` would have been indistinguishable from yolo — which the user explicitly did not want.
- **Coding-agent names** (`always` / `when-needed` / `silent`): rejected as inaccurate in both directions. Strict stops asking once an origin is approved, so it is not "always"; relaxed still asks for sensitive actions, so it is not "don't ask".

## Consequences

- The safety-level classification is exhaustive and compile-enforced: a new `CommandType` without a level is a compile error, extending the existing `as const` + exhaustiveness pattern in `packages/shared/src/policy.ts`.
- A `tab:new` carrying a URL classifies as read, not observer — otherwise renaming `navigate` to `tab:new` walks around the gate.
- `strict` is deliberately close to the pre-existing behavior (reads and writes both origin-gated), so upgrades land on `standard` — content reads go silent, writes keep asking — and the CHANGELOG must say so.
- Batched approvals, agent-requested mode changes, and stamping the current mode onto the Trace are explicitly out of scope for v1.

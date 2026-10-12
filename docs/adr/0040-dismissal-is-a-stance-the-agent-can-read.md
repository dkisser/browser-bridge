# Dismissal is a stance the agent can read

An Approvals card gets a close affordance in its top-right corner. Pressing it does not delete the record and does not approve or deny anything: it records that a human has seen this request and does not want to answer it now, and it makes that stance readable to the agent on its next attempt. This amends ADR-0038, which decided where the permission mode sits relative to the Working scope, by making the mode part of what a dismissal is keyed on.

The key is `(origin, command, mode)`. A dismissal made in `standard` says "at this threshold, stop asking me about this"; switching to `strict` asks again, because that is a tighter threshold and the human has not restated their answer there. This makes the mode part of a dismissal rather than a property of one, which is the same move ADR-0038 made for the gate itself: the mode moves what an approval means, so it belongs in the key rather than beside it.

A dismissed request still produces a card. `recordDenial` is untouched: the same `denialKey` (`reason|origin|command`, `packages/shared/src/policy.ts:467`) means the retried request replaces the old card rather than stacking under it, so a retried origin holds exactly one card. The human keeps the visibility to change their mind, and the agent is told to stop knocking. Suppressing the card would have made a dismissal a quieter deny, which is the one thing the affordance must not be.

The agent reads the stance through text, because there is no push channel: `humanDenialMessage` is a terminal string built in the shared package (`packages/shared/src/policy.ts:425`), Go only passes it through (`apps/bridge-core/internal/cli/send.go:110`), and the offscreen document never emits unsolicited events. The text says the human has declined to answer for now, and explicitly hands the decision back — "if this blocks your task, tell the human" — because a dismissal that reads as a refusal converts "I am busy" into "never ask again". That word is also why the current ending, "then the command can be retried", is wrong here and must be replaced.

A dismissal is cleared when the origin is granted (`approve-session` / `approve-always` drop it, since ignored and approved at once is a contradiction the human cannot have meant) and when the browser session ends (`clearSessionScoped`, alongside session-scoped approvals). There is no manual "un-ignore" control in v1: a grant already covers the case where the human changes their mind, and an extra surface for it is a decision to make when there is a use for it.

## Considered Options

- **Pure UI dismissal** (remove the card, touch no state): rejected, and not for cost. `dismiss` only filters the entry out of `recentDenials` (`apps/extension/src/policy-operations.ts:91`), and `recordDenial` re-creates it on the next identical rejection, so the card returns in place and the agent's text is byte-identical. Against an origin that keeps getting rejected it buys nothing — the noise leaves the screen and stays in the agent's loop.
- **Whole-origin dismissal** rather than per command: rejected. A human who is tired of `tab:new` opening new tabs has not decided anything about `click` on that origin, and a narrower human decision should not widen itself.
- **Keep an audit trail of dismissals now**: deferred to ADR-0017's audit work. The stance is already in policy state under this design, so the trail has something to read; writing a second record now reserves a schema for storage that does not exist yet.
- **A red `×` matching the side-panel message dismiss**: rejected on color. Red is the Deny semantic in this UI, and a dismissal is not a denial; the icon is neutral (`--text-faint`), reusing TakeoverHero's `.iconButton` geometry (22×22, ghost surface on hover, visible focus ring), permanently visible rather than hover-revealed, because a control that only exists under the cursor is not a control you can aim at.

## Consequences

- The close affordance appears on `origin_not_approved` cards only. Every other reason already carries a Dismiss text button (`apps/extension/src/ui/policy-actions.ts:11`), and the same action in two forms in one panel is a way to have two ideas about one button.
- `denialCardButtons` is documented as copy-frozen ("order and labels are user-visible contract"); the new affordance is outside that function and does not disturb the frozen triads.
- A new `DenyReason` (the ignored stance) is distinct from `origin_denied`, which stays a hard rail in every mode (`packages/shared/src/policy.ts:318`). Ignoring is not refusing: the origin can still be granted later, and the dismissal itself expires with the session.
- The badge counts `recentDenials.length + pendingDownloads.length` (`apps/extension/src/policy-state.ts:280`), so a dismissed-then-retried request still shows in the count. That is intended — the human is being asked again.

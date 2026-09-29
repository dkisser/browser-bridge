# Denials are fail-closed and non-blocking

When a gated command is stopped, it is denied immediately with a machine-readable reason code (e.g. `human_assist_active`, `origin_not_approved`, `action_out_of_scope`, `approval_denied`) and an Approval card is shown in the extension side panel; the agent relays the reason to the human through its own channel and retries after approval. The request is never held open waiting for the human. Decided alongside the working-scope model: with Approval and Takeover both in play, the denial contract is the seams between agent, extension, and human.

**Amended: not every denial produces an Approval card.** The clause above originally named `human_assist_active` in the same breath as the card, and that was wrong. An Approval card is a *request for a decision*; a Takeover denial is the user's own kill switch firing as intended, and there is no decision for them to make. Recording them meant the agent's retries walked the card list to its cap while the user did nothing, and once the user released the browser the panel and the badge went on reporting refusals worded as though the human were still in control. `recordDenial` now drops `human_assist_active`, and releasing Takeover purges any written before that rule existed.

The decision itself is unchanged: still denied immediately, still with the reason code, still nothing held open. What changed is that the denial stops manufacturing work for the user. A card appears for a denial the user can act on — an unapproved origin, a required capability.

The panel name changed from "popup" to "side panel" in ADR-0010; this line was never updated, which is the sort of thing that makes a reader distrust the rest of the paragraph.

## Considered Options

- **Blocking hold**: keep the MCP tool call pending until the human answers, timeout → deny. Rejected: a human away for ten minutes freezes the agent's tool call for ten minutes; MCP clients behave inconsistently under very long-running tool calls; and a timeout that denies still leaves the agent with a generic failure it cannot distinguish from a real error. Immediate denial with a reason enum keeps the failure explainable, the channel free, and every client's behavior identical.

## Consequences

- Denial reasons become part of the typed command contract in `packages/shared` — an extension of the error surface, not a breaking change.
- Takeover rejects commands instantly rather than queueing them; nothing auto-resumes after a timeout because nothing waits.
- Batched approvals (one prompt for several pending crossings) are a possible v2; v1 prompts per crossing.

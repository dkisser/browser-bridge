# Curated knowledge lives beside the card: guides and routines

A Site card is machine-owned: the learner rebuilds it from the Trace, and a hand edit is lost on the next rebuild. But the most valuable residue of a workflow is often *semantic* — why a step exists, which cookie banner must be dismissed first, what the page's business objects are — which structural learning cannot capture (ADR-0018 records shape, never meaning). And a flow that has become fixed deserves a replayable form, not re-derivation every time.

**Decision:** two new artifacts live under `$BB_HOME/data/` on the ADR-0017 preservation contract, keyed by host like cards:

- **Site guides** — `guides/<host>.md`: curated markdown written by the human, or by an agent at the human's request, through the `browser-bridge-memory` skill.
- **Routines** — `routines/<host>/`: crystallized, replayable sequences of `bridge` CLI calls for a repeated flow on that host (execution form: ADR-0029).

The write path is the skill, and the gate is the human's request. There are no autonomous writes and no per-workflow approval prompts — the maintainer rejected both: an agent that writes memory under the influence of untrusted page content is an indirect prompt-injection persistence channel, and a prompt per flow is noise that trains dismissal.

## Consequences

- Trust is asymmetric by construction. Cards are machine-trusted because structural redaction bounds what poisoning can enter (ADR-0025); guides and routines are human-initiated, so the review happens at the moment of writing, not at the moment of reading.
- Staleness mirrors the card's own property (ADR-0024): a Routine must self-verify an early landmark before acting, and on mismatch it is *thawed* — abandoned for live exploration and fixed or deleted — never retried blind. A guide that contradicts the live page is wrong in the same way.
- Cards stay machine-owned: the skill never writes `cards/`, and teaches the agent to repair a wrong card indirectly (browse correctly, let the learner rebuild, or `bridge memory rm`) rather than edit it.

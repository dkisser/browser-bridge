# The guide's read path rides the card's pull

ADR-0028 gave curated site guides a write path — the `browser-bridge-memory` skill, gated on the human's request — but never said how a guide gets *read*. What happened without that decision: nothing. The daemon did not know guides existed, the driver skill's recall step named only the card, and the one reader of `guides/<host>.md` was the crystallize-a-routine flow. A guide written on Monday was invisible on Tuesday, which is the quiet way a curated layer dies.

Two read paths were considered. Pushing the guide into the landing injection, beside the card, repeats the token mistake the injection budget exists to prevent: a guide is unbounded human prose, and `DefaultInjectTokens` is the reason the card's rendering is trimmed at all. The guide is also *durable* advice — it changes only when a human rewrites it — while the injection is sized and tiered for per-visit claims.

**Decision:** the guide rides the card's existing pull. `bridge memory show <host>` prints the guide after the card whenever `data/guides/<host>.md` exists, and the driver skill's recall step — which already mandates that pull on landing — now says so. Three properties fall out:

- `--raw` stays the card alone: it is the editing surface, and prose in it would break the pipe it was asked for.
- A host with a guide but no card still prints the guide; the missing card is reported exactly as before (exit 1, `no card for <host>`), because guides are written at a human's request while cards are earned by traffic, so the guide can legitimately arrive first.
- The print is capped (`MaxGuideBytes`, cut at a line boundary, marked when truncated): the guide rides a context-path on every visit, so its size is bounded for the same reason the card's rendering is.

## Consequences

- Guides join the recall path with zero new agent behavior to learn: the skill already says "pull the card on landing", and the guide now comes back with it.
- The MCP push path is unchanged — no guide injection at landing. An MCP-only agent that can also read files is pointed at `guides/<host>.md` by the skill; one that cannot, misses the guide. Accepted: prose injection is the budget risk, and the CLI is the primary surface.
- The file stays the truth. `memory show` reads the guide file directly, so an edit is visible on the next pull with no daemon involvement — the same property the card store's mtime revalidation gives cards.

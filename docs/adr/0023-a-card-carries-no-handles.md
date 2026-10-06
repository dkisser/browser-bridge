# A card carries no handles

ADR-0018 draws the trace's boundary as "the role of each addressable node, its
accessible name truncated to 40 characters, one attribute, and **a ref that is
evidence rather than a handle**". ADR-0019 draws the card's: "A card carries
predicates (role + name), never a stored ref, so every ref it hands back belongs
to the page the agent is looking at now."

Those two are consistent about the *trace* and inconsistent about the *card*, and
`MapEntry.Ref` was sitting in the gap: every site-map entry stored the ref the
call was observed at, which is the trace's ref, kept in the card, for no reader.
Nothing read it — not the renderer, which resolves predicates against the live
page, not the learner, not `bridge memory show`. It was written, serialised,
diffed by humans and never used.

**Decision:** a site-map entry carries a predicate and nothing else. The field is
gone.

The ref-as-evidence argument is sound and it is why this is not a contradiction of
ADR-0018. A ref is intrinsic to a *trace record*: the digest is a ref-keyed
structure and `Resolve` hands one back, so dropping refs there would make the
trace unusable. What does not follow is that a *card* — a long-lived, human-edited
claim about a site — should carry a copy of a fact that was true on one visit.
A card's whole contract is that it is re-resolved against whatever page the agent
is looking at now, and a stored ref is the one thing in the file that cannot be.
Keeping it is a standing invitation to the one-line mistake that breaks the
feature: someone helpfully renders `e.Ref` next to the resolved ref and the card
starts handing back handles from a page that no longer exists. A field that is
only wrong when used is not documentation, it is a trap with a comment on it.

A card is a claim about the present, and the trace is the archive. This is the
same line ADR-0018 drew when it dropped `SiteCard.Digest`, applied to the one
place that was left.

## The near-miss worth recording

`ProcedureEntry.Steps[].On` looks exactly like the field just removed — a ref
from a snapshot, stored in the card, never rendered — and it stays. It is not the
same thing for one reason: `sameSteps` compares it. A procedure's identity is
"these commands, on these controls", and dropping `On` would make
`gettext → click` on the message list indistinguishable from `gettext → click` on
the toolbar — two different procedures that would corroborate each other's
existence and clear the `ProcedureCorroboration` gate between them. `procedureLine`
deliberately does not render it, for exactly the reason `MapEntry.Ref` was
removed; it is load-bearing data, not a handle to hand out.

The distinction to carry forward: **is this value part of how the card decides
something, or is it only ever displayed?** A value that only decides how the card
*matched* stays, and must never reach an injection. A value that is only ever
displayed goes.

## Consequences

- `cards/<host>.json` loses one short string per map entry, and a card that a
  human diffs no longer shows a `@eN` that looks like something to act on. The
  card's map is now unambiguous: everything in it is a predicate, and the only
  refs in the file are the ones a `ProcedureEntry` step used for identity.
- The invariant "a card never carries a handle" is now enforced by the type
  rather than by a convention, and `internal/memory` asserts it directly.
- `ProcedureEntry.Evidence` still cites envelopes, and those are trace
  identifiers rather than page handles — they address a record, not an element.
- This amends ADR-0018's ref-as-evidence sentence in the card's case only; the
  trace boundary it draws is unchanged and remains correct.

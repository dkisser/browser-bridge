# The card is a claim about a site, and a transport error is not one

`sendError` fed the memory hook through the same `RecordResult` the extension's
own responses use, and `core.MemoryHook`'s doc defended it: *"A payload the
router synthesized (browser offline, cannot_buffer, sw_timeout) is recorded like
any other, because an agent asking a browser that is not there is exactly the
kind of thing a card should remember."* ADR-0030 §1 had already insisted the
error *code* survive, so the difference between "the browser was offline" and
"no such element" was preserved on the wire and in the trace.

It was not preserved in the card, and the card is where it mattered. A Site card
is scoped to one host and its failure tier is injected back to every future
agent as `Observed to fail here (do not repeat):`. Three codes were reaching it:

- `browser_offline`. Six clicks against a disconnected browser wrote
  `FailureEntry{Signature: "browser_offline", Command: "click", Count: 6}` into
  whatever host the tab happened to be sitting on — a card telling future agents
  that a working control fails here.
- `cannot_buffer`. The same, for a control-plane backlog.
- `sw_timeout`. Worse than misattributed: it times out against the tab's
  *previous* host, so a slow navigation made the card assert that the wrong site
  times out.

And because `len(seg.failures) > 0` is one of the two ways `applySegment` mints
a card at all, transport noise did not merely pollute existing cards — it
created them.

**Decision:** a payload the control plane synthesized for itself is recorded
through its own channel, `MemoryHook.RecordRouterError`, and is written to the
trace with `KindRouterError`. `buildSegments` pairs it — so the command stops
being pending and the cross-pass window clears — and then drops it. It reaches
`bridge memory history` and nothing else.

The test is what the record is evidence *about*, not who produced it. An
extension response is evidence about the page ("no such element here" is a fact
about the site, which is what the card is for); a router-synthesized one is
evidence about the control plane's own state. `KindRouterError` already existed
— `buildSegments` already matched it, `ADR-0020` already reserved the name —
and nothing ever wrote it. This is the first writer.

## Consequences

- The failure tier stays what ADR-0018 says it is: the one signal that cannot be
  misread. A tier that carries "the browser was not connected" is a tier an agent
  learns to discount, and discounting it costs the failures that are real.
- The trace keeps every synthesized error, with its code, so a human reading
  `memory history` for diagnostics loses nothing. ADR-0030 §1's distinction is
  now carried by the record *kind* as well as by the code.
- `core.MemoryHook` gains a method, so every implementer carries one more. That is
  the cost of the distinction being structural rather than a heuristic over the
  code's spelling — a heuristic would drift the moment the router minted a new
  code, which is the defect class this branch kept finding.
- A router error does **not** count as a call for the procedure tier either: the
  command did not execute, so it is not a step in a sequence that worked.
- Cards can no longer be minted by transport failures alone. A host whose only
  history is "the browser was offline" now has no card, which is correct: nothing
  was learned about it.

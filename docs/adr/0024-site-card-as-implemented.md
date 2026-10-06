# The site card as it was actually built

ADR-0019 states four things that did not survive contact with the
implementation. Three were wrong and one was incomplete, and all four were
corrected in place before this rule — ADRs are append-only, so the corrections
live here instead.

**Both injection points do not carry the same content.** ADR-0019 has the
`navigate` landing and the first `snapshot` each carry a full card. They carry
different halves, and the division is not tidiness — it is what each call can
honestly say. A `navigate` has no page behind it, so it can only state what is
true of the site regardless of the page: the selectors that failed there, and
the sequences that worked. The site map needs a *live* ref, and a ref
remembered from an earlier snapshot addresses nothing, so the map waits for the
`snapshot` — which is also the one call where producing a live ref is free,
because the page was being fetched anyway. Having both points carry everything
meant the landing re-resolved against the *previous visit's* digest and handed
the agent a ref that did not work. The end-to-end test is what caught it, and it
caught it because it drives real wire payloads through record → learn → inject.

**The card rides in-process, not on the wire.** ADR-0019 does not say where the
note travels. It is `ResponsePayload.SiteNote`, tagged `json:"-"`, because the
MCP tools render `data` field by field and a key added to the result JSON would
be silently dropped, and because putting it on the wire would change the
envelope ADR-0012 froze. The adapter composes the text and appends the note,
which is why the note is asked for from the executor rather than assembled once
in the router.

**Only the MCP adapter receives the card.** ADR-0019 says both adapters see it,
"since the injection is below the adapter layer", and that the CLI's output
grows by the same block. That is wrong about the CLI. The CLI is a separate
process reaching the control plane over the WebSocket protocol; it has no way to
ask for a card without a new field on the frozen envelope, and a learned,
auto-updating claim is not worth a protocol change to carry. `bridge memory show
<host>` prints the same rendering instead, so a human running the CLI has a
first-class way to see exactly what the agent was told — which is the property
the original claim was reaching for, reached a different way.

**A section header is never emitted without entries under it.** Not stated in
ADR-0019 and worth stating, because the obvious implementation gets it wrong: a
card whose map entries all belong to a different browser profile (ADR-0019's
per-browser annotation) would otherwise render an empty "here is what I know"
block. That reads as though something was withheld, which is worse than saying
nothing at all.

Amends ADR-0019. The two-halves split is also what ADR-0025 measures, and the
empty-section rule is why a card rendered for a profile that has nothing to say
is empty rather than empty-looking.

# A card's refs may be shown offline, but only labelled as offline

`bridge memory show <host> --resolve` renders a card with its site map resolved
against the last page the control plane recorded for that host. The digest is
read out of the trace — `TraceRecord.Page` already carries it, so this works
with the service stopped — and every line of the output says which page it came
from and when.

This amends ADR-0024's claim that `memory show` "prints the same rendering, so a
human running the CLI has a first-class way to see exactly what the agent was
told". That is true of the failures and the working sequences and false of the
map, because the map only renders when there is a page to resolve it against.
`--resolve` is how the third is reached.

**The obvious implementation is wrong twice over.** The tempting version is for
the CLI to read `cards/<host>.json` and call `memory.RenderCard` itself, which
needs nothing new at all. It is wrong because there is no digest: with no
`Resolver`, `RenderCard` drops the map section entirely (ADR-0024's
empty-section rule), so the command silently does nothing it was named for. And
the fix for *that* — reaching back into the trace for a digest — is where it
turns dangerous, because the result would be a rendering indistinguishable from
the live injection except for a footer nobody reads. A card's refs are only
meaningful against the page they came from, and the page in the user's browser
is not that page. An offline render that looks like the live one is worse than
no offline render, because the reader cannot tell which they are holding.

**So the provenance is stated twice, and both statements are load-bearing.** Once
in the header the command prints, and once in the site map section's own title.
The second is why `RenderOptions` grew a `PageNote`: the section header is the
only line in a card that makes a claim about the world outside the card, and it
was hardcoded to `"checked against this page"`. That string is true of every
injection — the resolver is a digest the control plane just fetched — and false
of every file-backed render, so it cannot be left alone and worked around by
string surgery afterwards. A render that must be honest about its evidence has to
be able to say what that evidence is.

**The scope is diagnostic, and that is what keeps ADR-0025 intact.** Nothing
rendered here is ever handed to an agent, so the card-is-advice-not-a-sandbox
property is untouched: no agent can act on a ref from this command, because no
agent reads it. The same reasoning rules out the alternative reading — wiring
this output onto the `navigate` CLI path — which would be a real change to how
cards reach agents and would need its own decision, not a flag on a read command.

**What this does not fix.** `bridge navigate` still shows no card, and the reason
is unchanged: the note is `ResponsePayload.SiteNote`, tagged `json:"-"`, so a
separate process cannot ask for one without a new field on the envelope
ADR-0012 froze. This command routes around that for a human reader and leaves
the actual product gap open. A CLI that an agent invokes through a skill
(ADR-0015) still gets no card.

**What it costs.** One field on an exported options struct whose only
non-default user is a human-facing command, one full read of an append-only log
that only runs when a person is looking at a card, and a rule the tests pin: the
offline output must never contain the phrase the live one uses.

Amends ADR-0024. Relies on the trace persisting `PageDigest` per ADR-0021's
typed append-only stream. The safety boundary of ADR-0018 and ADR-0025 applies
unchanged — this reads what was already recorded, and records nothing.

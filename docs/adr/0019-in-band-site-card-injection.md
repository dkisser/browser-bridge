# Site knowledge is injected in-band, on the result path

Where does a per-site learned card reach the agent? The three obvious homes all
fail, and the failures are structural rather than matters of taste:

- **The system prompt** is assembled by the agent's own runtime. The control
  plane is a separate process (`bridge serve`) reached over MCP or the CLI
  protocol; it has no handle on the prompt.
- **The skill** is an install artifact with replace semantics — ADR-0015
  `rm -rf`s `~/.agents/skills/browser-bridge/` on every upgrade, and
  `bridge [service] update` pulls the newest `install.sh`. Anything a learned
  card wrote there is gone on the next update. It is also static and
  versioned, which is the opposite of per-host-and-changing.
- **MCP tool descriptions** are one static blob per session, and they are the
  wrong *kind* of place: ADR-0003 chose them for hand-written, cross-site,
  never-changing guidance.

**Decision:** append the card to the result, in-band, at the moment the agent
lands on a host it has a card for — the `navigate` result, and the first
`snapshot` result after it. The result carries a labelled block
(`可参考的站点访问模式`) with a hard cap of 400 tokens, trimming the
procedure tier first and keeping the site map and failure list.

**The two points carry different halves, and that is not a division of labour
for its own sake — it is what each call can honestly say.** A `navigate` has no
page behind it, so it can only state what is true of the site regardless of the
page: the selectors that failed here, and the sequences that worked. The site
map needs a *live* ref, and a ref remembered from an earlier snapshot addresses
nothing; so the map waits for the `snapshot`, which is also the one call where
producing a live ref costs nothing. An earlier version of this design had both
injection points carry everything, which meant the landing re-resolved against
the *previous visit's* digest and handed the agent a ref that did not work — the
end-to-end test is what caught it.

The timing is not a preference. The control plane cannot know which host the
agent is heading for until the agent says so, so the injection point is forced
to be just after that information arrives. It is also the cheapest possible
moment: the first `snapshot` after landing is being fetched anyway, and that
same response is what a card's predicates are checked against — so verifying
that a card still matches the site costs no additional browser call.

## Relationship to ADR-0003

This narrows ADR-0003, it does not overturn it. ADR-0003's finding — that
*advice is ignorable under pressure*, proven by an agent that read the
"take a full-page snapshot" error and still guessed six selectors in a row —
is exactly why hand-written cross-site guidance was pushed into descriptions
**and** repeated in not-found errors. Those are static facts about the
protocol, and descriptions are the right home for them.

What descriptions structurally cannot carry is content that is **per-host and
changes underneath you**. A card is invalidated by a site redesign within a
week; a description is fixed when the session starts. So the two mechanisms
coexist: ADR-0003 for the protocol's permanent rules, this ADR for the learned
and perishable.

## Considered Options

- **A dedicated `site_recall` tool** the agent calls when it wants site context:
  rejected as the *only* path, because it depends on the agent remembering to
  ask — the same "relies on the agent choosing correctly under pressure"
  failure ADR-0003 documents. It remains a reasonable manual escape hatch.
- **Inject on every command while on a matching host**: rejected — the
  fingerprint check would then ride along for free, but the token cost is paid
  on every single call, and past a few hundred tokens per site the injection
  costs more context than the card saves.
- **Inject into error messages** (ADR-0003's post-failure channel): rejected as
  the only path because it never helps on the successful route, which is where
  avoiding the exploration is worth the most.

## Consequences

- **No tool is added**, so `TestToolsListMatchesTSFixture`
  (`internal/http/golden_test.go:25`) and
  `internal/http/testdata/tools_list_ts.json` stay untouched. The golden
  contract is not renegotiated for this feature.
- **The card rides in-process, on `ResponsePayload.SiteNote` (`json:"-"`), not
  on the wire.** The MCP tools render `data` field by field, so a key added to
  the result JSON would be silently dropped; and putting it on the wire would
  change the envelope ADR-0012 froze. The adapter that composes the text
  appends it instead, which is why the note is asked for from the executor
  rather than assembled once in the router.
- **Only the MCP adapter receives the card.** The CLI is a separate process
  reaching the control plane over the WebSocket protocol, so it has no way to
  ask for a card without a new field on the frozen envelope — and a learned,
  auto-updating claim is not worth a protocol change to carry. `bridge memory
  show <host>` prints the same rendering instead, so a human running the CLI
  has a first-class way to see exactly what the agent was told. (An earlier
  draft of this ADR claimed both adapters see it; that was wrong about the CLI
  and the correction is here rather than in the code.)
- Injection is additive on a result that already exists, so a card that is
  wrong is a cost, never a failure — the call still succeeds. That property is
  what makes auto-updating the card (rather than gating it behind approval)
  acceptable.
- **A section header is never emitted without entries under it.** A card whose
  map entries all belong to a different browser profile renders nothing, not an
  empty "here is what I know" block that reads as though something was withheld.

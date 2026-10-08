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
- Both adapters see the card, since the injection is below the adapter layer.
  The CLI's output grows by the same block; `docs/mcp-setup.md` needs no change
  because it summarizes tools, not result shapes.
- Injection is additive on a result that already exists, so a card that is
  wrong is a cost, never a failure — the call still succeeds. That property is
  what makes auto-updating the card (rather than gating it behind approval)
  acceptable.

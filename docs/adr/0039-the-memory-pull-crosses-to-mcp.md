# The memory pull crosses to MCP, as a mandated step and not a choice

Three ADRs in the self-learning run independently agreed that memory recall
reaches an agent two ways and stopped both from becoming a tool. ADR-0019
rejected `site_recall` "as the *only* path, because it depends on the agent
remembering to ask"; ADR-0024 recorded that the in-band injection is MCP-only;
and ADR-0027 sanctioned the CLI pull while listing, as a consequence, that "no
new tool" — `TestToolsListMatchesTheContract` and the golden fixture stay
untouched.

The MCP side now has `memory_list` and `memory_show`. This amends all three.

**The ADR-0019 objection still holds, and is why the skill moved in the same
change.** What was rejected was pull as the sole mechanism, left to the agent's
free choice under pressure — the failure ADR-0003 documents, where an agent read
a snapshot-first instruction and then guessed six selectors anyway. A tool in
`tools/list` is exactly that shape: a live option the model has to decide to
take, competing for attention against the task. What makes this work is the same
thing ADR-0027 identified for the CLI: the pull is a **checklist step in the
skill**, and the skill is the channel ADR-0003 already counts on. Both skills now
name `memory_show` in their recall step, so the tool is reached by the same
mechanism that made the CLI pull reliable. Landing the tool without the skill
change would have produced precisely the thing ADR-0019 refused.

**The asymmetry decided which adapter.** ADR-0026 closed by recording that the
CLI's workaround "leaves the actual product gap open": a card is
`ResponsePayload.SiteNote`, tagged `json:"-"`, so a separate process cannot ask
for one without a new field on the envelope ADR-0012 froze. The MCP server is an
HTTP endpoint *inside* the control plane and renders in-process with no wire
change. So the adapter that already receives cards by push is the one that can
serve them by pull, and the one that cannot is the CLI. That is the opposite of
what ADR-0024's "MCP-only" line made it look like, and it is why the pull moved
in this direction rather than the other.

**Reads only.** Writes stay behind the CLI. ADR-0028 argues that guides and
routines are human-owned because the review belongs at the moment of writing,
and that an agent writing memory under the influence of untrusted page content is
an indirect prompt-injection persistence channel — a guide rides every later
visit's context, so a poisoned one persists. A write tool on the MCP adapter
gives a model a channel to that file with the human watching a command line they
can read, and losing that is a worse trade than the ergonomics are worth. So
`memory_show` is read-only, `bridge memory rm` stays CLI, and the skill says so.

**Two things the implementation had to get right that the decision does not
imply.** The memory tools must not go through `dispatch`, because `dispatch`
calls `TakeSiteNote`, which yields a card at most once per landing — an agent
that pulled memory before its first `navigate` would spend the landing's card and
leave the browsing tools with nothing. And they take no `tab_id` and no
`timeout_ms`, which is the visible signal that they read the store rather than
the browser, and therefore never reach the extension's policy gate (ADR-0006).

**What it costs.** ADR-0027's "no new tool" consequence is now false and the
golden contract is renegotiated: `tools_list_ts.json` grows from 22 to 24 entries
and `docs/mcp-setup.md` gains two rows that `TestDocsListEveryToolTheFixturePins`
requires. That was recorded as a *benefit* when it was written — the feature did
not have to touch a published contract — and it is worth being explicit that
something traded away for convenience now has. What is preserved is the smaller
claim: no tool result shape changed, and no `CommandType` was added, so nothing
in `packages/shared` or the extension moved.

**What a review pass found, and what it says about the shape of the decision.**
Ten findings, all real, and they cluster into one thing: the pull had been
written as if it were the only place these questions get asked, and three of
them had already been answered elsewhere in the codebase.

- The tool trimmed the host with `strings.TrimSpace` while the CLI it is
  documented as equivalent to runs `memory.Host` — so an agent holding a URL
  from `pageinfo` was told a host the bridge had a card for had never been
  learned. **One rule, one place** is what `memory.Host`'s own comment says, and
  the tool was a second copy of it that had not been forked yet.
- An unreadable guide destroyed a card that had read fine, where `printGuide`
  had already decided the opposite and written down why: the card is what the
  command exists for.
- `memory_list` reported a store full of unparseable cards as "No site cards
  yet", which is the empty/broken collapse `Store.Read`'s doc comment calls a
  bug.
- The offline rendering was copied from `cli.showResolved` after this change had
  already extracted the small half of that copy. The provenance ADR-0026
  requires stated twice is exactly the kind of thing that has to change in both
  places at once, which is what a copy does not do. It is now
  `memory.RenderResolvedOffline`, shared.
- The pull's own header was a bracketed label of its own, one line above
  `[learned site patterns]` — which `RenderCard` emits into every rendering it
  produces. Two labels in one payload is a worse answer than one label and a
  sentence, and the comment claiming the distinction was the thing the second
  label undermined.

The first and last are the same failure wearing different clothes: a decision
recorded here is only as good as the *equivalences* it claims, and claiming one
is a promise to keep checking. Both surfaces now have tests that fail if the
equivalence breaks.

## Consequences

- `memory_show` resolves the site map offline against the last recorded page and
  says so in the header *and* in the map section's own title, inheriting
  ADR-0026's requirement that a file-backed render never be mistaken for the live
  injection. A test pins that the offline output never contains the live phrase.
- A missing card and a missing guide are both values, not errors, and a guide
  with no card is still served — guides are written at a human's request while
  cards are earned by traffic, so the guide can legitimately arrive first
  (ADR-0033).
- `memory_show` reads the digest through the manager's own stream handle rather
  than opening a second one over the live file, which is the mistake ADR-0036
  records. `memory.LastDigestFor` is shared with the CLI so both callers agree on
  what "most recent" means.
- Routines stay `bridge` CLI call sequences (ADR-0029). Only the *pull* moved;
  the script's own commands still pass the policy gate one at a time, which is
  the property that made shell the right Phase 0 choice.
- `docs/mcp-setup.md` says these tools read the store and take no `tab_id`,
  because an agent reading the tool table is exactly the audience that would
  otherwise try to pass one.

Amends ADR-0019 (no new tool; pull as a rejected sole path), ADR-0024 (the
MCP-only claim about who can receive a card), and ADR-0027 (the "no new tool"
consequence). Builds on ADR-0026's offline-labelling rule and ADR-0028's write
gate.

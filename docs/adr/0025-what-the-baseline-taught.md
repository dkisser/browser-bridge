# What the baseline taught, and what it tightened

ADR-0018 draws a boundary, and ADR-0024 records what the card does in practice.
Neither survived being built and measured unchanged. This is what changed, and it
is in two halves because they are two kinds of thing: a boundary that was drawn
in the wrong place, and a number that was being asked two questions at once.

The evidence for all of it is `internal/memory/bench`, a deterministic harness
(`bridge memory bench run`) that drives a synthetic mail page through the *real*
learner with two arms differing only in whether the card is read. Its scripted
agent follows ADR-0003's protocol and has no site knowledge, so what the card can
earn is the site-specific part and not the discipline. Same numbers every run,
which is what makes a change in the trend a change in the mechanism.

## The boundary, moved

**First, what ADR-0018's boundary actually draws**, stated precisely because the
consequences below are easier to argue about: what is *not* written is the body of
the page, the text of any `text` node, the page title, and anything the user typed.
What *is* written, bounded, is the role of each addressable node, its accessible
name truncated to 40 characters, one attribute, and — in the trace, not the card
(ADR-0023) — a ref. So a control labelled `Inbox (3)` or a heading labelled
`Recent activity` is kept: those are labels of addressable things, and without them
a predicate has nothing to match on. A caption paragraph is not. The distinction
is addressable structure versus page prose, and it is a judgement, not a filter
that can be re-derived later.

That judgement was right. Three specific holes in it were not.

**A URL is kept in full.** ADR-0018 says so, and gives a reason — the host is
the partition key. Correct about the host, and it does not follow that the query
string travels too. A query string is a search term, a session token or a
document id depending on the site; it is written into a card file that outlives
the visit; and *nothing in the learner reads it*, because every rule keys off the
host. A URL is now reduced to scheme, host and path, with query, fragment and
userinfo dropped, on both the `navigate` argument and the URL a snapshot
reports. An unparseable URL is dropped rather than stored raw, because an
unparseable string is the case where there is least reason to believe it is
harmless.

**A selector is structure.** It looks like it. Any attribute selector can carry a
quoted literal, and a literal is page-derived by construction:
`[data-message-subject="Standup notes"]` is a guess *about* page content, and the
guess is the content. Worse, the extension's `querySelectorByText` accepts a
bare string as a selector and matches it against every element's `textContent`,
so `get_text "some prose"` is a legal call whose selector *is* the prose — and
the extension's not-found message quotes it straight back into the card's
`FailureEntry.Hint`. Both paths landed in `cards/<host>.json` in the clear,
permanently. The harness found this by
accident: the fixture deliberately hides message subjects from the Pseudo-tree,
and the card the learner built from it contained one.

So a selector is reduced to its shape — tag, class, id, attribute names,
combinators, with quoted literals replaced — and the browser's error message is
not stored at all. Nothing ever rendered that message; it was stored because it
was available. What remains is the error *code*, which is the part that
generalises. Refs (`@e14`) pass through untouched: they are opaque handles from
DOM order, carry no page content, and are the one address form the feature rests
on. ADR-0023 splits the trace's ref from the card's, which is the same
judgement one level down.

The cost is real and it is the right way round: a card can no longer be matched
against the *exact* guess that failed, only against its shape. The next guess
will be a different value of the same attribute, and the shape is what says that
will not work here either.

**One size answered two questions.** A single `ResultSz` was both the magnitude
of the payload and the ranking signal for the site map. They are not the same
unit, and one of them is wildly misleading in the other's role: a screenshot's
payload is a base64 image megabytes long, so ranking containers by payload size
pins a `visual target` entry to the top of every site map on every site where the
agent ever took a screenshot, and trips the oversized-read threshold on a
perfectly ordinary one. `ResultSz` is now the payload magnitude and drives the
soft-failure threshold; `ContentSz` is the character count of a read's *content*
and is the only number the map ranks on, set for `gettext` and `gethtml` and zero
for everything else. A command that starts returning content under a new key has
to be added to that list on purpose rather than beginning to rank by accident.

Two smaller consequences of the same pass. A node with neither a name nor an
attribute never becomes a map entry, because resolution returns the *first*
role match and a bare `button` predicate lands on whatever button now comes
first — reporting itself valid and handing the agent a ref to the wrong control
with the card's authority behind it. And a card no longer keeps a copy of the
page: `SiteCard.Digest` held the first digest it ever saw, up to 256 labels,
was read by nothing, was never refreshed, and was the largest single block in
the file ADR-0021 calls the one a human reads, diffs and hand-edits.

## What the baseline says about the card

About two calls saved per task on a page with four plausible containers, plus
one rejected call the failure list prevents. That is the difference between
reading three containers to find the answer and reading one. It is not a large
number and should not be quoted as one. Three findings are worth more than the
number.

**A card is advice, not a sandbox.** The first harness let the card *replace* the
agent's own list of things to try, and it failed in the one situation this
feature exists to survive. The moment a site is redesigned, every container the
card names stops resolving; the card comes back holding the one entry whose name
happened to survive; and an agent that trusted it as the whole list read that
entry, found nothing, and gave up — spending fewer calls than before and
*failing*, which is strictly worse than no card. The shortlist is now prepended
to the page's own containers. This is also the only reading consistent with the
delivery mechanism: a labelled note appended to a result the agent was going to
get anyway is a hint, not a sandbox.

**"Was a card offered" is recorded, not assumed** (`KindCardShown`). A card can
be learned correctly, stored correctly, rendered correctly and still never reach
an agent, and a measurement that cannot tell those apart reports "the card did
not help" for what is really "the card was never offered". It is also the one
number an agent cannot report honestly about itself — one that ignored the card
will say it was never there — so the live benchmark reads it out of the trace
rather than asking.

**The site map helps locate content, not controls.** The benchmark's fourth task
is "find the Snooze control", and it is in the table on purpose because it is
expected to show the smallest saving (0.71 calls/run, against 2.14–3.00 for the
three content tasks). Finding a named control is something the
Pseudo-tree already answers: the toolbar is right there, with a role that says
what it is. What the tree cannot answer is *which of a page's several readable
containers holds the thing you are looking for*, because a read that succeeds on
all of them tells the tree nothing about which one mattered. That is the claim,
and that row is what keeps it from quietly growing.

And the one that decides whether the feature is safe: a redesign costs part of
the advantage (about 2.0 calls saved → 1.5) and then gives it back (→ 3.0) once
the learner has seen the new site, without ever costing correctness. That curve
is the finding, and it only exists because the harness redesigns the site
underneath a card it learned from the old one.

What the baseline establishes, over and above the number: the mechanism works end
to end on a page built to be ambiguous, the failure tier removes one specific
rejected call an agent would otherwise repeat, and a redesign costs part of the
advantage and then gives it back without ever costing correctness.

## Known gaps

- **A snapshot is capped, so a very large page is simply not learned.** A
  snapshot contributes at most `maxDigestNodes` node signatures, while the MCP
  schema lets an agent ask for a 100,000-character pseudo-tree, which is thousands
  of nodes. A call aimed past the cap degrades to "we did not learn that one"
  rather than to a wrong entry, which is the right way round — but it does mean a
  page large enough to hit the cap is a page whose structure is not in the trace
  at all, and no amount of reading will fix that afterwards.
- **The map cannot name an unnamed control.** The cost of refusing bare role
  predicates is that a per-row "mark as read" button — an icon button whose
  label lives only in its container's text — is exactly the kind of control the
  map can no longer describe. On a real mail page that is one of the three
  controls the original incident was about. An ordinal predicate ("the fourth
  button") was the alternative and was not taken: a different fragile thing
  wearing the same clothes. An agent still reaches the control by reading the
  row that owns it; what it loses is being *told* which button that is. Worth
  revisiting only against a real page, and probably as a distinct entry kind
  rather than by weakening the predicate.
- **The stream is not rotated.** A snapshot record is at most ~20KB under the
  node cap, so an ordinary day is a few megabytes, but the log grows without
  bound over months. Rotation is not free to bolt on: the learner cursor is a
  *line index* into the file, so dropping a prefix invalidates it and a rotation
  scheme has to carry the offset across. Do it as its own change, with the
  cursor format as part of it — do not reach for `tail -n` in passing.
- **The self-update signal needed its path checked.** The staleness revision was
  recorded only from a branch that a tab which had navigated never takes, so on
  the ordinary `navigate → snapshot` path the agent was told to distrust a stale
  card and the card was never marked for rebuilding. It fires from both now, and
  a cross-site navigation no longer verifies the new host's card against the
  previous host's digest — which recorded healthy cards as stale.

Amends ADR-0018's boundary and ADR-0019's claim about what the card is for. The
"one irreversible decision" and "structural, so small enough to keep forever"
consequences ADR-0018 already carried are unaffected; what was added to them
here is the node cap that qualifies the second.

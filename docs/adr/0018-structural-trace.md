# The Trace is structural: no page text, no typed input

Browser Bridge records nothing about what the agent does. A self-learning
capability needs that record as its raw material, and the obvious
implementation — log the command and its response verbatim — is a privacy
bomb: `type` carries the user's actual keystrokes (message bodies, form
fields, sometimes secrets) and `get_text` / `get_html` carry whole pages. It
would turn `$BB_HOME/data/`, which ADR-0017 justified as mode 0700 because it
holds a *token hash*, into the most sensitive directory on the machine.

**Decision:** capture at the router, reduce to structure. The observation hooks
are `internal/core/router.go` (`HandleInboundCommand` and
`HandleBrowserResponse`) — the single choke that both Inbound adapters pass
through. A record holds the command name and arguments, the outcome, and a
structural digest of the page. URLs are kept in full (the host is the partition
key and paths belong in the site map). `type`'s text argument is reduced to its
length plus its target ref. Page text and typed bodies are never written.

**The boundary this actually draws, precisely.** What is *not* written: the body
of the page, the text of any `text` node, the page title, and anything the user
typed. What *is* written, bounded: the role of each addressable node, its
accessible name truncated to 40 characters, one attribute, and a ref that is
evidence rather than a handle. So a control labelled `Inbox (3)` or a heading
labelled `Recent activity` is kept — those are labels of addressable things, and
without them a predicate has nothing to match on. A caption paragraph is not.
The distinction is *addressable structure vs page prose*, and it is a judgement,
not a filter that can be re-derived later.

The structural digest is the Pseudo-tree shape, which is exactly what ADR-0001
built it for — roles come from a small custom vocabulary and attribute
retention is chosen by us rather than by Chrome, so the digest is a stable
thing we can compare across sessions.

## Considered Options

- **Capture everything, redact later**: rejected — redaction that depends on a
  later code path is not redaction. A crash, a debug flag, or a new command
  surface leaks it in the meantime.
- **Capture everything, encrypt at rest**: keeps the data but not the problem.
  It still puts page text and user input on disk, still makes the audit story
  harder, and adds a key to manage. ADR-0016's threat model does not need
  plaintext page content; encrypting it would imply we do.
- **Capture at the MCP dispatch point** (`internal/http/tools.go:63`): rejected
  — the CLI adapter does not go through it, so the trace would cover only part
  of the traffic and the learner would be blind to half its evidence.
- **Capture extension-side** (where the real DOM lives): rejected — ADR-0006
  puts enforcement in the extension on purpose and the control plane is the
  right place for a control-plane-owned record. The extension also has no view
  of the task, so it cannot tell which calls were part of one attempt.

## Consequences

- **The one irreversible decision in the feature.** A trace that was not
  recorded cannot be reconstructed, so V2 cannot retroactively gain
  text-derived lessons; that would require a new ADR *and* a window of time
  where new traces have a different shape than old ones.
- The learner can never re-read what a page said. If a future lesson needs page
  semantics ("this list is virtualized"), it must be expressed structurally —
  which is how ADR-0003's existing habit already works: the not-found error
  appends the page's largest text *containers*, not their contents.
- A snapshot contributes at most `maxDigestNodes` node signatures. The MCP
  schema lets an agent ask for a 100,000-character pseudo-tree, which is
  thousands of nodes, and every response is written to a log that is never
  rotated (ADR-0020). A call aimed past the cap is simply not learned, which
  degrades to "we did not learn that one" rather than to a wrong entry.
- **The stream is not rotated, and that is a known gap rather than a decision.**
  With the node cap a snapshot record is at most ~20KB, so an ordinary day is a
  few megabytes, but the log grows without bound over months. Rotation is not
  free to bolt on: the learner cursor is a *line index* into this file, so
  dropping a prefix invalidates it, and a rotation scheme has to carry the
  offset across. Do that as its own change, with the cursor format as part of
  it — do not reach for `tail -n` in passing.
- **Two sizes, because the trace answers two different questions.**
  `ResultSz` is the payload magnitude and drives the soft-failure threshold;
  `ContentSz` is the character count of a read's *content* and is the only number
  the site map ranks on. They are separate fields rather than one field with two
  readings because they are not the same unit, and one of them is wildly
  misleading in the other's role: a screenshot's payload is a base64 image
  megabytes long, so ranking containers by payload size pins a `visual target`
  entry to the top of every site map on every site where the agent ever took a
  screenshot, and trips the oversized-read threshold on a perfectly ordinary one.
  `ContentSz` is set only for `gettext` and `gethtml` and is 0 for everything
  else, deliberately: a command that starts returning content under a new key has
  to be added to that list on purpose rather than beginning to rank by accident.
- **A URL is reduced to the part that identifies a site.** Scheme, host and path
  are kept; query, fragment and userinfo are dropped, on both the `navigate`
  argument and the URL a snapshot reports. A query string is a search term, a
  session token or a document id depending on the site, it is written into a card
  file that outlives the visit, and *nothing in the learner reads it* — every
  rule here keys off the host. An unparseable URL is dropped rather than stored
  raw, because an unparseable string is the case where there is least reason to
  believe it is harmless. (The `Page:` line's title was already read and thrown
  away for the same class of reason.)
- **A selector is reduced to its shape, and the browser's error message is not
  stored at all.** A selector looks structural and is not. Any attribute selector
  can carry a quoted literal, and literals are page-derived by construction:
  `[data-message-subject="Standup notes"]` is a guess *about* page content, and
  the guess is the content. Worse, `querySelectorByText` in the extension accepts
  a bare string as a selector and matches it against every element's
  `textContent`, so `get_text "some prose"` is a legal call whose selector is
  prose — and the extension's not-found message quotes it straight back, which
  landed in the card's `Hint` field. Nothing ever rendered that hint; it was
  stored because it was available. So: selectors are reduced to tag, class, id,
  attribute names and combinators with quoted literals replaced, and the message
  is gone, leaving the error *code* — which is the part that generalises. Refs
  (`@e14`) pass through untouched: they are opaque handles from DOM order and
  carry no page content, and they are the one address form the whole feature
  rests on.

  The cost is that a card can no longer be matched against the exact guess that
  failed, only against its shape. That is the better half of the deal — the next
  guess will be a different value of the same attribute, and the shape is what
  says that will not work here either.
- **A node with neither a name nor an attribute never becomes a map entry.**
  Predicates are matched by (role, name, at most one attribute) and resolution
  returns the *first* matching node, so a bare `button` predicate lands on
  whatever button now comes first — after a redesign it reports itself valid
  and hands the agent a ref to the wrong control with the card's authority
  behind it. That is worse than the entry being absent: an absent entry makes
  the agent look, and a confidently wrong one makes it not. The cost is real
  and is the known gap below.

## Known gaps

- **The map cannot describe an unnamed control.** The cost of the rule above is
  that a per-row "mark as read" button — an icon button whose label lives only
  in its container's text — is exactly the kind of control the map can no
  longer name. On a real mail page that is one of the three controls the
  original incident was about. The alternative was an ordinal predicate ("the
  fourth button"), which is a different fragile thing wearing the same clothes,
  so it was not taken. An agent still reaches the control by reading the row it
  belongs to; what it loses is being *told* which button that is. Worth
  revisiting only with a real page to test against, and probably as a distinct
  entry kind rather than by weakening the predicate.
- **The stream is not rotated.** Kept above with the cursor interaction; still
  open.
- **A card does not keep a copy of the page.** `SiteCard.Digest` held the first
  page digest the card ever saw — up to 256 control labels — and nothing read it,
  not the renderer, not the learner, not the CLI. It was also never refreshed, so
  it was a frozen picture of a page that stopped existing the moment the site was
  redesigned, and it was the largest single block in the file ADR-0021 calls "the
  one thing a human is expected to read, diff and hand-edit". Removed. The trace
  is the archive; the card is a claim about the present, and a claim should not
  carry a snapshot of a past it never updates.
- **The browser's error message is not part of a card.** See the selector entry
  above. A failure entry keeps the error *code* and the selector's *shape*.

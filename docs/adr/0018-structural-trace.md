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

# The Trace is structural: no page text, no typed input

Browser Bridge records nothing about what the agent does. A self-learning
capability needs that record as its raw material, and the obvious
implementation — log the command and its response verbatim — is a privacy
bomb: `type` carries the user's actual keystrokes (message bodies, form
fields, sometimes secrets) and `get_text` / `get_html` carry whole pages. It
would turn `$BB_HOME/data/`, which ADR-0017 justified as mode 0700 because it
holds a *token hash*, into the most sensitive directory on the machine.

**Decision:** capture at the router, reduce to structure. The observation hooks
are `internal/core/router.go:131` (`HandleInboundCommand`) and `:197`
(`HandleBrowserResponse`) — the single choke that both Inbound adapters pass
through. A record holds the command name and arguments, the outcome, and a
structural digest of the page. URLs are kept in full (the host is the partition
key and paths belong in the site map). `type`'s text argument is reduced to its
length plus its target ref. Page text and typed bodies are never written.

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

- **This is the one irreversible decision in the feature.** A trace that was
  never recorded cannot be reconstructed, so V2 cannot retroactively gain
  text-derived lessons; that would require a new ADR *and* a window of time
  where new traces have a different shape than old ones.
- The learner can never re-read what a page said. If a future lesson needs page
  semantics ("this list is virtualized"), it must be expressed structurally —
  which is how ADR-0003's existing habit already works: the not-found error
  appends the page's largest text *containers*, not their contents.
- Because the record is structural, it is small enough to keep forever, which
  is what makes the change-log and rollback story in ADR-0020 cheap.

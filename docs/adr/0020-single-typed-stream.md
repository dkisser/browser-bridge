# One typed append-only stream; cards, diffs, and the audit view are projections

The feature needs three things on disk: the raw observations a card is derived
from, a record of every card revision so a bad automatic update can be rolled
back, and the human-readable Audit trail that CONTEXT.md:75-77 has carried as
*planned, not yet implemented* since it was defined. These are three purposes
that all want the same shape — an ordered log of things that happened.

**Decision:** one append-only stream (`$BB_HOME/data/stream.jsonl`) whose
records are typed: `command`, `response`, `card_revision`, `learn_run`.
Everything else is a projection of it. The site cards under
`$BB_HOME/data/cards/<host>.json` are the learner and the reader's view; the
diff a human reviews before rolling back a bad card is the `card_revision`
records for that host; the Audit trail is a rendering of the same records for
the person rather than for the developer, which is the distinction CONTEXT.md
already draws between an Operational log and an Audit trail.

The learner holds a **cursor on disk** into the stream and re-reads from there
on every pass, rather than being fed records in memory. In-memory signals
coalesce to a single pending "there is new work" flag; they are a wakeup, never
the carrier.

## Considered Options

- **Three separate files** (trace, change log, audit): rejected — three
  writers, three cursors, three places to look when a card and the evidence
  behind it disagree. The disagreements are exactly what you need to debug.
- **Trace plus a change log, audit derived later**: rejected — same objection
  with two of the three still duplicated.
- **One file per session**: rejected — sessions are a property of the agent's
  runtime, not of the bridge. The control plane has no session or task concept
  of its own (the MCP session id is a client connection, not a user task), so
  that file boundary would be an assumption the bridge cannot actually make.

## Consequences

- **The reader must tolerate a torn last line.** A crash during an append
  leaves a partial record at the tail; the reader skips unparseable lines
  rather than failing. This is the one real hazard of a single stream, and it
  is why appends are `O_APPEND` single writes rather than buffered batches.
- **Backpressure drops the signal, never the data.** When the in-memory flag is
  already set, another producer does not enqueue; the work is already
  represented by the unread suffix of the stream. This is why the queue can be
  a single slot.
- Card writes still go through the atomic write-and-rename pattern already
  used for `data/config.json` (`internal/core/state.go:165-183`), because a
  card is read at injection time and must never be observed half-written. The
  stream is append-only and needs no such guard; the cards do.
- Satisfying the planned Audit trail is a side effect of this shape, not a
  second write path. Do not add one.

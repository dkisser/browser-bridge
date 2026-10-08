# The ceiling never fired, and the rotation had two owners

ADR-0032 decided the trace stream turns over at 16MB so that *"the disk and the
idle CPU cost of the control plane are a function of how much has been learned,
not of how long it has been running."* It named the cost it was fixing: an empty
learn pass costs 1.5ms because it calls `ReadFrom(0)` and materialises every
record.

Two things were wrong with how that was implemented, and a review of the call
sites found only the smaller one.

**The precondition could not be satisfied.** `Rotate` refuses while any record
is unlearned, because the cursor is a line number and moving the file aside
would renumber it. It expressed that as `cursorLine < lines` — and `Learn.Run`
appends its own `learn_run` record *after* it advances the cursor, so the cursor
is permanently one line short of the end. The test was unsatisfiable. Not "only
checked at startup", as the review concluded from the call sites: the check ran
and could never pass, which is why the file simply grew. The condition is now
what it always meant — no *learning* record may be unread — and the learner's
own bookkeeping is recognised as the one thing that is safe to leave behind and
free to re-read.

**The rotation had a second owner.** `New` rotates, and `New` is reached by
`bridge memory learn` and `bench record`, both of which open a second Manager
over the **live** `$BB_HOME/data` while the daemon holds its own handle.
`Rotate` closes the caller's fd, renames `stream.jsonl` to `stream.jsonl.1` and
creates a fresh empty `stream.jsonl`. The daemon's already-open fd follows the
inode to the renamed file and keeps appending there, while its own `ReadFrom`
reads the new empty file and never sees another record — learning stops for the
rest of that daemon's life, and the CLI reports nothing wrong.

## Consequences

- Rotation is gated on `Options.OwnsRotation`, set by the daemon and by nothing
  else. This is the same shape as the `OpenStream` fix: rotation is a property
  of the directory, and only the process that owns the directory may act on it.
- The check is repeated after every learn pass, not only at open, so the ceiling
  applies to a daemon that stays up for a week — which is the only process that
  accumulates enough for the ceiling to matter.
- A `learn_run` record is now formally "bookkeeping the reader may skip",
  which is what it always was. Any other kind still blocks rotation.
- `New` no longer leaks the stream's append handle when `OpenStore` fails. The
  caller treats that error as recoverable — `app.go` logs "self-learning
  disabled" and runs anyway — so the daemon used to live on with an unclosed fd.
- The review that found the second defect reported the first as a lesser version
  of itself ("only ever runs at startup"). Both are recorded here because the
  difference between "runs once" and "never runs" is the difference between a
  bounded file and an unbounded one, and only the second explains what was
  actually observed.

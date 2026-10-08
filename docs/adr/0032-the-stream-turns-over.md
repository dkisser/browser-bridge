# The stream turns over, and localhost is a site

**Amends** [ADR-0020](./0020-single-typed-stream.md) (the stream), [ADR-0019](./0019-in-band-site-card-injection.md) (the host scope key) and [ADR-0025](./0025-what-the-baseline-taught.md) (the harness's own invariant, as the audit trail it left behind).

None of these changes a decision. They are the parts of a decision that had been written as a comment, and the comments were not load-bearing.

## The stream turns over

ADR-0020 chose one append-only stream over a database, and its own header gives the two properties that make that work: a single `O_APPEND` write of a whole line cannot leave a hole, and the reader skips a torn tail rather than failing. It also said — in a comment inside `ReadFrom` — that holding the write lock while scanning "would make every browser call wait on a full-file read of a log that is never rotated."

The log was never rotated. It is the only artefact in the package with no ceiling: cards are bounded by `maxMapEntries`, failure lists by `maxFailureEntries`, procedures by `maxProcedureEntries`, and `bench.jsonl` is a human's own file. This one grows with every call, forever, and `ReadFrom` starts at byte 0 on every pass — so both the disk and the idle CPU cost of the control plane were a function of how long the daemon had been running. Measured at 40k records / 2.1MB: an empty learn pass 1.5ms, and `bridge memory history` 26ms, because it calls `ReadFrom(0)` and materialises every record.

`stream.jsonl` now rolls over to `stream.jsonl.1` at 16MB, keeping one generation, and `ReadFrom` reads both with continuous line numbering.

**The condition is the interesting part.** The cursor is a line number, so a rotation renumbers every line after the cut. Rotating with unlearned records behind the cursor would either lose them or need an offset the cursor has nowhere to keep. So rotation waits for both: the file is over the ceiling *and* the learner has consumed all of it. At that moment the cursor restarts at 0 and the next pass re-reads the retained generation from the start — which is idempotent, because rebuilding a card from the same records produces the same card. If the line count cannot be established, the file is left alone: not knowing is a reason to try again next start, not a reason to cut.

Two generations rather than a rotating set, because the audit surface a human reads is the recent one and the learner can rebuild any card it needs from the current file alone.

## `localhost` is a site

`Host` is the scope key (ADR-0019): one card per host. It dropped anything without a dot in it, on the reasoning that *"a scheme like `about:` or a stray token is not a site"*. Both halves of that are true, and the rule did not separate them: a registrable domain always has a dot, so the test also discarded `localhost` and `::1`.

The consequence was silence. A card could not exist for a locally developed site, which is exactly where someone building against this thing runs it, and nothing said so — the feature simply did not engage. Every rule in the package keys off the host, so with no host there was no card, no injection and no failure to explain.

The rule now accepts a dotless host that is a loopback name (`localhost`, `*.localhost`) or an IP literal, and still refuses everything else: `about`, `chrome` and a stray token remain out, because admitting them would mint a card per piece of junk. The test table carries both halves, since the refusal is the part that was already right.

## The harness wrote a trace no session could produce

The benchmark's own comment, on `attempt`:

> A trace the benchmark produces has to look like a session someone could have had, because "it runs the real learner over the real records" is the entire claim. Colliding identifiers are a trace no session could produce.

`recordCall` built envelope ids as `prefix + "-" + command`, so two calls of the same command in one task shared one. Over a two-repetition run, 14 of 56 ids carried more than one command record, 12 of them more than two.

It cost nothing in the numbers — measured byte-identical `observedCall` and `map` output with colliding and distinct ids, because the args ride on the response record and the learner reads a command's args before the clobbering command is recorded. The damage was to the audit trail: `CardRevision.Evidence` cites envelope ids, so an id no longer identified one call, and the records behind a review of what changed and why no longer answered "which call".

Ids are now unique per record. The invariant is checked by the reader rather than by the author, which is the only place a comment cannot be wrong.

## Three smaller things, same shape

`LearnRun.Written` carried the line count under a name that reads as cards written — the same one-name-two-units trap `ResultSz` and `OutcomeSoft` were both caught being. It is `Consumed` now, and the cards a pass wrote are the revision records, where they can be counted exactly. The capped-pass audit read `len(segments)` *after* truncating to the cap, so the one number in it you needed was the one that could not be trusted: *"capped at 64 of 64 segments; the rest are deferred."*

`bench.jsonl` was read with a `json.Decoder` that returned the first error, so one bad line anywhere made every later report fail with no way to repair it — while the stream's own reader, two packages over, deliberately skips an unparseable line and counts it. The benchmark log is machine-written by a human-invoked command, so a `SIGKILL` mid-append is a torn tail and a hand-edit is a bad line; either way a whole report was the wrong answer. It skips and warns now.

And `Manager.inflight` was bounded by *"the router's own in-flight set, and cleared as each result lands"* — which is not a bound, because three router paths drop a command without producing a result: send-to-extension fails, the MCP `sendCommand` timeout calls `RemoveRoute`, and the client disconnects. Every one strands an entry for as long as the daemon lives, and the timeout path makes it routine. The map is capped now and evicts oldest-first, which loses a record nobody was going to read instead of growing without bound.

**What these have in common.** None was a crash, a log line or a failing test. Each was a sentence in a comment that read like a guarantee, and the guarantee was somewhere else. The comments were not wrong about intent — they were wrong about enforcement, which is the one thing a comment cannot do.
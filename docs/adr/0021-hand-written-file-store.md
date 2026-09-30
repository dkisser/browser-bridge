# Memory is hand-written files, not a database

ADR-0017 wrote, in passing, that "the audit trail and agent memory (SQLite)
planned". That was a parenthetical, not a decision, and it does not survive
contact with the build. `.github/scripts/build-binaries.sh:22` sets
`CGO_ENABLED=0`, which rules out `mattn/go-sqlite3` outright. The pure-Go
alternative, `modernc.org/sqlite`, works but drags a large dependency tree into
a binary whose size ADR-0013 explicitly celebrated shrinking from 26.2MB to
17.7MB. The current dependency set is four small packages — cobra,
coder/websocket, jsonschema-go, mcp-go-sdk — and that frugality is a feature of
the module, not an accident.

**Decision:** no database, no embedded store, no third-party component. The
store is a hand-written append-only stream plus one JSON file per host, read
and written with `encoding/json` and `os`: an `O_APPEND` typed-write that closes
off a torn line before adding to it, a cursor-to-EOF read that distinguishes a
torn tail from a corrupt line, per-card read/merge/atomic-write, and a cursor
file. Card writes reuse the atomic write-and-rename pattern already in
`internal/core/state.go`.

The "roughly 200 lines" this decision originally carried was an estimate made
before writing it, and it was wrong by an order of magnitude: the package came
out at ~2,800 lines excluding tests. What the estimate got right is the shape of
the argument. Nothing here needs a query engine, and a hand-written store has no
query engine to misuse later. What the estimate missed is that durability edges
are where the lines go — torn-line recovery, cursor ordering, file permissions,
the torn-tail-versus-corrupt-line distinction — and each of those is a few lines
of code and a paragraph of comment explaining why it cannot be simplified.

The cost that mattered did not show up in lines. The binary grows by ~245KB
(17,749,778 → 17,994,546 bytes) and `go.mod` is unchanged, which is the number
this decision was actually about.

This amends ADR-0017's parenthetical; the rest of ADR-0017 — that persistent
data lives under `$BB_HOME/data/` at mode 0700 and that upgrades and
`bridge service uninstall` never touch it — is unaffected and is exactly why
this store is shaped this way.

## Considered Options

- **`modernc.org/sqlite`**: rejected on the dependency tree against ADR-0013's
  size budget. Its query power is also mostly unneeded: the only lookup the
  system performs is "give me the card for this host".
- **`mattn/go-sqlite3`**: not an option — it is cgo-only.
- **bbolt / pebble / badger**: mature, pure Go, and the wrong trade. They store
  a single opaque binary file, so cards stop being `cat`-able, `git diff`-able,
  and reviewable — which is precisely what the automatic-update-and-rollback
  story depends on (ADR-0019's consequences). Their transaction and bucket
  models are more machinery than learning and adapting them costs, against the
  one query shape that actually occurs.
- **An off-the-shelf agent memory framework** (mem0, Zep, Letta, cognee and
  kin): rejected as solving a different problem. They are semantic/vector
  recall systems over chunks of unstructured text; ours is a keyed lookup by
  host plus recency, deliberately without embeddings, and every one of them
  wants its own runtime and vector store alongside a single static binary.
- **Keeping raw text and adding an index later**: possible, but the index would
  be over a store whose query surface we can already enumerate, so it is
  speculative. Revisit if card count or stream size ever makes the filesystem
  layout the bottleneck — and take a new ADR when it does.

## Consequences

- The query surface is whatever we choose to implement, which is a ceiling as
  well as a floor. "What did the agent do on this host last Tuesday" is a scan
  of the stream; anything more is a new decision.
- The store is testable with plain `go test` against temp directories, with no
  fixtures, no migrations, and no driver.
- If the stream grows large enough that parsing every line matters, adding a
  JSON field-extraction library at that point is a triggered change, not a debt
  incurred now.

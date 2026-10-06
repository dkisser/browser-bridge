# Crystallization starts as CLI scripts; no execution runtime yet

A Routine (ADR-0028) needs an execution form. The considered options:

- **Shell sequences of the stateless CLI** — zero new code, and the security model survives untouched: every command in the script still passes the extension's policy gate (ADR-0006/0007) one at a time. The pain is dataflow: passing a value extracted in step 3 into step 7 means bash variables and jq, and LLM-authored bash quoting is a classic failure mode.
- **An embedded JS runtime in the `bridge` binary** (`bridge run script.js`, e.g. goja) — the shape both ego-lite (`ego-browser nodejs`) and tabbit (`tabbit-cli nodejs`) independently converged on: one process, one connection, real variables and control flow, intermediate page data filtered in-process so only the printed summary reaches the model. The protocol stays owned by Go — the JS API is a thin binding over `internal/ws`. Cost: a large new dependency, against the grain of ADR-0021's dependency aversion.
- **A TS SDK** — rejected. Not because of protocol drift (`packages/shared/src/types.ts` is already the contract source and would be imported, not rewritten), but because it adds a node/bun runtime requirement on the user's machine, against the single-binary positioning (ADR-0013), and adds a second client implementation to keep honest.

**Decision:** Phase 0 is shell scripts. The embedded runtime is the designated successor and is deferred until a real Routine hits bash's dataflow wall in practice — the trigger is evidence, not anticipation. In-page eval (running agent JS inside the page through the extension) stays rejected: it bypasses the command taxonomy that Approval granularity depends on (ADR-0006/0007), and no token saving is priced against that.

## Consequences

- Nothing is built in this phase: a Routine is a file the agent itself executes through its shell, and `bridge memory show <host> --json` (ADR-0027) is the only recall surface it needs.
- When the runtime does arrive, it is an ADR of its own, and the dependency case must be argued there against ADR-0021.

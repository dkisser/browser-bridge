# `$BB_HOME` gets a `data/` directory for persistent data

`~/.browser-bridge` mixed three kinds of things at its root: install artifacts
(`bin/`, `extension/`, `version` — overwritten on every upgrade), runtime
state (`logs/`, `run/`, `launchagents/` — regenerable), and the only piece of
persistent data, the pairing `config.json`, sitting loose next to them. With
the audit trail and agent memory (SQLite) planned, persistent data needed an
explicit home and an explicit preservation contract.

**Decision:** all persistent data lives under `$BB_HOME/data/` (mode 0700 —
the pairing token hash is sensitive). `config.json` moves to
`data/config.json`; the daemon migrates a pre-existing root-level
`config.json` on startup with an atomic rename (the StateManager is its only
reader/writer, so migration cannot race an installer). The preservation
contract is: install.sh / `bridge service update` only ever write `bin/`,
`extension/`, and `version`; `bridge service uninstall` keeps `data/` and
only `--purge` removes it. A BATS test installs twice with planted data
sentinels and asserts they survive, so the contract is enforced rather than
documented-only.

**Vocabulary:** the split rests on distinguishing the *Operational log*
(`logs/`, diagnostic, disposable, rotatable, read by developers) from the
*Audit trail* (`data/`, durable record of what the agent did, read by the
user). Both were colloquially "logs", which is why putting an audit log under
`data/` felt wrong; they are different things and now have different homes
(see CONTEXT.md).

**Considered options:** `state/` (rejected — the XDG state dir by definition
also holds logs, which would re-mix what we just separated); separate `data/`
+ `state/` dirs (rejected — one resident file today does not justify two
dirs); a `BB_DATA_DIR` env override (rejected as YAGNI — a symlink covers the
relocate-data use case); doing nothing (rejected — the root stays ambiguous
exactly when new data kinds are about to arrive).

**Consequences:** downgrading to a pre-ADR-0017 binary after the migration
runs means the old binary no longer finds `config.json` at the root — the
extension must be re-paired once. Downgrades are not a supported flow, so no
compatibility shim (e.g. a root-level symlink) was kept. Anything displaying
paths (the extension settings tab, `bridge service uninstall --help`) shows
the new location.

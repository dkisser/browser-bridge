# Architecture decision records

An ADR records what was decided and why **at the time**. That timestamp is the
only thing that makes a decision log worth more than a wiki page, so ADRs here are
append-only: a decision that is contradicted, narrowed or extended is recorded in a
*new* file, never by editing the old one. Editing in place destroys the record of
what was believed and when — including the parts that turned out to be wrong, which
are the parts worth reading.

That rule leaves one gap. An ADR that cannot be edited also cannot be edited to
point forward, so "this one has been amended" has nowhere to live inside the old
file. This table is that place. `CONTEXT.md` is the other: it is mutable, and it
records what is true *now*.

## The self-learning run (0017–0034)

The most recent decision chain, and the one with amendments. An ADR listed in the
**Amends** column is not wrong — it was right about what it decided, and a later
ADR records what changed since. Read the chain, not just the newest file.

| # | Decision | Amends |
|---|----------|--------|
| [0017](./0017-bb-home-data-directory.md) | `$BB_HOME` gets a `data/` directory for persistent data | superseded in part by [0021](./0021-hand-written-file-store.md) (the `(SQLite)` parenthetical) |
| [0018](./0018-structural-trace.md) | The Trace is structural: no page text, no typed input | [0023](./0023-a-card-carries-no-handles.md), [0025](./0025-what-the-baseline-taught.md), [0030](./0030-the-boundary-as-implemented.md) |
| [0019](./0019-in-band-site-card-injection.md) | Site knowledge is injected in-band, on the result path | [0024](./0024-site-card-as-implemented.md), [0025](./0025-what-the-baseline-taught.md), [0027](./0027-the-cli-recalls-by-pull.md), [0031](./0031-the-endpoint-is-untrusted.md) |
| [0020](./0020-single-typed-stream.md) | One typed append-only stream; cards, diffs and the audit view are projections | |
| [0021](./0021-hand-written-file-store.md) | Memory is hand-written files, not a database | 0017 |
| [0022](./0022-optional-model-call-for-compression.md) | The model call is optional, compresses only, and needs no SDK | [0031](./0031-the-endpoint-is-untrusted.md) |
| [0023](./0023-a-card-carries-no-handles.md) | A card carries predicates, never handles | 0018's ref-as-evidence, in the card's case; 0019 |
| [0024](./0024-site-card-as-implemented.md) | The site card as built: two halves, in-process, MCP-only | 0019 |
| [0025](./0025-what-the-baseline-taught.md) | What the baseline tightened: the record's boundary, and what a card is for | 0018, 0019, [0030](./0030-the-boundary-as-implemented.md) |
| [0026](./0026-show-a-cards-refs-offline.md) | A card's refs may be shown offline, but only labelled as offline | 0024, [0027](./0027-the-cli-recalls-by-pull.md) |
| [0027](./0027-the-cli-recalls-by-pull.md) | The CLI recalls by pull — a mandated skill step, not a tool | |
| [0028](./0028-guides-and-routines.md) | Curated guides and routines live beside the card | [0033](./0033-the-guides-read-path-rides-the-cards-pull.md) |
| [0029](./0029-crystallization-starts-as-cli-scripts.md) | Crystallization starts as CLI scripts; no execution runtime yet | |
| [0030](./0030-the-boundary-as-implemented.md) | The boundary as implemented: codes pass through, prose is classified, href is reduced | 0018, 0025 |
| [0031](./0031-the-endpoint-is-untrusted.md) | The endpoint is untrusted, and a card has to be removable | [0030](./0030-the-boundary-as-implemented.md), 0022, 0019 |
| [0032](./0032-the-stream-turns-over.md) | The stream turns over, and localhost is a site | 0020, [0019](./0019-in-band-site-card-injection.md), [0025](./0025-what-the-baseline-taught.md) |
| [0033](./0033-the-guides-read-path-rides-the-cards-pull.md) | The guide's read path rides the card's pull | [0028](./0028-guides-and-routines.md), [0034](./0034-the-landing-announces-a-guide-with-a-pointer.md) |
| [0034](./0034-the-landing-announces-a-guide-with-a-pointer.md) | The landing announces a guide with a pointer, never the prose | [0033](./0033-the-guides-read-path-rides-the-cards-pull.md) |

## The rest

| # | Decision |
|---|----------|
| [0001](./0001-snapshot-pseudo-tree.md) | Snapshot: hand-rolled DOM pseudo-tree as the page representation |
| [0002](./0002-interactive-default-filter.md) | Snapshot: interactive filter as the default |
| [0003](./0003-discovery-guidance-in-tool-descriptions.md) | Discovery guidance lives in MCP tool descriptions |
| [0004](./0004-page-text-rendered-plain-text.md) | Page text stays rendered plain text; HTML→Markdown conversion, if ever built, lives extension-side |
| [0005](./0005-service-namespace-launchd-supervision.md) | Service lifecycle lives in `bridge service`, supervised by one LaunchAgent |
| [0006](./0006-security-enforcement-in-extension.md) | Security enforcement lives in the extension |
| [0007](./0007-working-scope-not-per-command-tiers.md) | Working scope, not per-command tiers |
| [0008](./0008-fail-closed-non-blocking-denial.md) | Denials are fail-closed and non-blocking |
| [0009](./0009-control-plane-stays-off-cdp.md) | The control plane stays off `chrome.debugger` |
| [0010](./0010-side-panel-replaces-popup.md) | The human surface moves from popup to side panel |
| [0011](./0011-bridge-core-merges-ws-server-and-local-proxy.md) | Merge `ws-server` and `local-proxy` into a single `bridge-core` process |
| [0012](./0012-control-plane-and-cli-in-go.md) | Reimplement the control plane and CLI in Go |
| [0013](./0013-single-bridge-binary.md) | Merge `bridge-core` into `bridge` as a hidden `serve` subcommand |
| [0014](./0014-agent-tab-group.md) | Agent tab group |
| [0015](./0015-skill-distribution-via-installer.md) | Skill distribution via the installer |
| [0016](./0016-deferred-security-boundaries.md) | Deferred security boundaries (M1, M2) of the local single-user positioning |

# Snapshot: interactive filter as the default

ADR-0001 kept reading and acting in one representation. In practice that meant the default snapshot carried every text run and structural container, so real app pages (GitHub notifications: 15.5K chars / ~3.9K tokens tier-0) blew the ~1K-token ambition several times over, and budget fallback dropped text runs silently — agents read truncated pages as if they were complete (the Gmail incident, TODO.md #1). PinchTab's published numbers show the same split: ~800 tokens for text extraction, ~1.5K for an interactive snapshot. We decided the default snapshot is the action surface — interactive elements plus headings — and reading page content is `gettext`'s job.

## Considered Options

- **One full representation, higher budget (status quo with bigger `max_chars`)**: reading and acting stay unified, but every page pays reading-page tokens and tier fallback still mangles large pages. Rejected: the two jobs want opposite retention policies.
- **Default `interactive` filter; `full` as opt-in**: the default output matches the snapshot's stated job (find what to act on), lands ~1.5K tokens on typical app pages, and leaves `full` for exploration and reading-ish tasks that need text runs. Chosen.
- **Default `interactive` plus aggressive format tightening (dedupe text, drop img src, flatten wrappers)**: shrinks `full` too, but the wins concentrate in the mode agents would use less after this change. Deferred; revisit with measurements after the filter ships.

## Consequences

- `filter` defaults to `interactive`; `filter='full'` reproduces the ADR-0001 tree. The default budget rises to 8000 chars (interactive) while `full` keeps 3000.
- Tier-2 truncation must never be silent: full-filter output marks suppressed text runs with `text [text suppressed]`, and the stats line carries the active tier (`[nodes: 40/484 | tier: 2 | truncated: true]`).
- Non-interactive elements (plain images, bare containers) no longer receive refs in the default snapshot; agents that need them must ask for `filter='full'`.
- The skill docs and tool descriptions must steer reading tasks to `gettext` and warn about virtualized lists (Gmail), where class selectors match only mounted rows.

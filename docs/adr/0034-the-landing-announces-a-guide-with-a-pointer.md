# The landing announces a guide with a pointer, never the prose

ADR-0033 kept guides out of the landing injection for a good reason — a guide is unbounded human prose, and the injection budget is the reason the card's own rendering is trimmed at all. But it left the push path with a discovery hole: an agent navigating to a host had no way to *know* a guide existed. The skill could say "check whether `guides/<host>.md` exists", and agents do not do that — an existence check owed on every host, forever, is a standing tax that gets paid once and then silently never again. Discovery was the real requirement; transport was only ever the risky part.

**Decision:** the landing carries a one-line, fixed-shape pointer when a guide exists:

```
[site guide] this host has a curated guide: <absolute path> — read it when you need the why; ...
```

A pointer is bounded, so it does not reopen the budget question ADR-0033 settled; the prose still rides only the pull (`bridge memory show`). The same line is appended to the CLI's `navigate`/`tab:new` output, which gets no in-band injection at all — and needs no wire change to get the pointer, because the CLI can read `$BB_HOME/data` as well as the daemon can. The host comes from the landed URL in the command's own result, since navigate follows redirects, falling back to the URL that was asked for.

## Consequences

- The pointer rides the *announcement* injection only — the landing, or the snapshot that stood in for one (ADR-0019's arming). The verification render stays the bare map, or the agent hears about the same file on every read.
- A host with a guide but no card is announced too, and is deliberately *not* recorded as `card_shown`: the guide is not a card, and the baseline counts cards.
- CLI output gains a line in human mode only. `--json` output is a structured contract and stays exactly as it was.
- What agents do with the pointer is their judgment: read the file when the task needs the why, ignore it when the card suffices. The one rule the skill states — the live page wins over the guide — is printed on the pointer line itself, because that is the one place every reader is guaranteed to see it.

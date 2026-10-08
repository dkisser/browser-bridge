# A selector marker must be doing selector work

ADR-0030 §2 removed the space from `safeSelector`'s structural marker set. The
reasoning was exact: *"Every sentence of prose contains a space, so every
multi-word phrase failed the test, fell through to the structural path, and was
copied byte for byte."* The fix was right and its scope was too narrow — the
space was not the only punctuation prose and selectors share.

The test was `strings.ContainsAny(sel, ".#[]():>+~*=")`, and a period ends most
English sentences. So `get_text "Contact support."` was classified as structural
and copied verbatim into `FailureEntry.Sel` and the stream's `args.selector` — a
card file that outlives the visit. `#` (`issue #42`), `:` (`Note: see below`) and
`.` after a digit (`v1.2 released`) all pass the same test. ADR-0030 §2's own
argument applies to each of them unchanged, and the leak it closed was closed
only for prose that happened to avoid a period.

**Decision:** a marker counts only in the position a selector grammar puts it.

- `.` `#` `:` — only immediately before a name. A digit does not start one, so
  `issue #42` and `v1.2` are prose.
- `[` — only before a name or a quote, so `see [1]` is prose.
- `=` — only inside brackets, so `x = 5` is prose.
- `>` — only with a name or a separator on each side, so `div>p` and `div > p`
  both count.
- `+` `~` `*` — removed from the set entirely. Each is a real token, and each
  appears as ordinary prose punctuation (`2 + 2`, `~5 left`, a footnote's `*`).
  Every structural use of them is covered by something else: `[class*="x"]` by
  the bracket rule, `:nth-child(2n+1)` by the pseudo rule.

The space stays out, for ADR-0030 §2's reason.

## Consequences

- Prose reduces to `…` whether or not it contains punctuation. The reduction
  still teaches the actionable thing — a bare selector did not resolve here.
- A bare `a + b` or `a ~ b` selector loses its shape, the same trade ADR-0030 §2
  already accepted for descendant selectors. The ambiguity resolves toward the
  side that cannot leak, which is the rule that section established.
- `[` and `>` still admit some prose (`see [x]`, `a > b` as a comparison). The
  cost of narrowing further is real selectors losing their shape for punctuation
  that is genuinely rare, so these stay — recorded here as a known residue
  rather than left implicit.
- This narrows the set ADR-0030 §2 recorded. That ADR's decision stands; this one
  applies it to the characters it did not reach.

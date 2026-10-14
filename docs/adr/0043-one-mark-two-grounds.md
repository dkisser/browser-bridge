# One mark, two grounds

The mark has to sit on a paper README and on a dark toolbar, and those want
different files: an opaque-background asset is what social previews, app tiles,
Slack unfurls and anything else without alpha support can actually use, while a
transparent one is what the extension's own UI needs. So the brand now ships two
families — `logo.png` / `icon-{16,32,48,128}.png` transparent, and
`logo-dark.png` / `icon-dark-{16,32,48,128}.png` on the extension's dark surface.
Neither replaces the other and both are kept.

**Decision:** the dark family changes the *backdrop only*. The spectrum stays
`#007aff → #22d3ee → #fbbf24 → #ff9500`, byte for byte the same mark.

This is worth stating because the extension's dark scheme makes the opposite
suggestion. In `apps/extension/src/ui/tokens.css` the dark block redefines
`--primary: #0a84ff` and `--amber: #ff9f0a`, brighter than the light
`#007aff` / `#ff9500`, and the rule that produced those is real: UI text on a
dark surface needs more contrast, so the UI tokens brighten. A logo is not UI
text. Measured against the dark surface `#131315`, the light spectrum already
clears **4.62:1** at its darkest stop (AA body text wants 4.5:1) and 8.44:1 at
the amber end, so there is no contrast problem to solve — and solving it anyway
would mean the mark reads as two different colours depending on what is behind
it, which is the one thing a logo must not do. The dark asset is a different
file, not a different logo.

The dark surface is `#131315`, agreed by two independent sources:
`docs/design/liquid-bridge-glass.design.md` (`surface`) and the dark scheme in
`apps/extension/src/ui/tokens.css` (`--surface`). The dark PNGs are saved as
**RGB, not RGBA** — an asset whose defining property is an opaque backdrop should
not carry a permanently-255 alpha channel implying otherwise.

**Consequences:** building the dark family surfaced a defect in the light icons
that had been there since they were first cut. The viewBox carries ~7.5% optical
padding so the logo breathes on a page, and the icons were reusing it — but an
icon canvas *is* the boundary, so the padding only shrank the mark, and the
non-square viewBox inside a square viewport letterboxed the rest. The shipped
`icon-16.png` had the mark occupying 88% of the width and **66% of the height**:
in a 16px slot that is a mark 12px tall. Icons are now cut against a tight
2%-padded ink box and fill 100% of the width. `logo.png` and `logo-glass.png`
keep the generous padding, because on a page the air is the point.

Neither family is referenced by `manifest.json` yet. The natural consumer for
the dark icons is Chrome's `theme_icons`, which swaps the toolbar icon with the
browser theme; wiring that is a separate call about whether the extension wants
a dark-theme toolbar variant at all, and shipping the assets without it costs
nothing but a few KB.

**Amends** [0042](./0042-a-tapered-sweep-with-a-spectrum-fill.md), which
introduced the mark and the icon ladder without a dark-ground family.